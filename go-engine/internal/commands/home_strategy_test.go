// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
)

type capturedHomeBackend struct {
	fakeRealizer
	t         *testing.T
	root      string
	targets   []string
	prepared  int
	activated int
	built     bool
	fail      bool
	// observeErr makes the ownership observation unreadable.
	observeErr error
	active     string
	outputs    map[string]map[string]string
	// onFreeze runs when the live combined activation is frozen.
	onFreeze func()
	// onRealize runs when the package set is installed.
	onRealize func()
	// store is the fake store that holds each built activation's files tree.
	store string
	// hiddenLeaves are built leaves that a recursive declaration expands to; evaluation
	// reports no flat target for them.
	hiddenLeaves []string
	// typedTarget is the settings file that Home Manager renders from a generated
	// typed module. renderTyped turns the staged canonical payload into the built
	// file; nil builds the payload unchanged.
	typedTarget string
	renderTyped func([]byte) []byte
	// definedOutside is what the explicit configuration's projection reports; nil means unknown.
	definedOutside []string
	// flakes records every flake that was prepared.
	flakes []string
}

func (b *capturedHomeBackend) Realize(toAdd []realizer.Installable) (realizer.Result, error) {
	if b.onRealize != nil {
		b.onRealize()
	}
	return b.fakeRealizer.Realize(toAdd)
}

func (b *capturedHomeBackend) PrepareHome(flake string, build bool) (*realizer.HomePreparation, error) {
	b.prepared++
	b.built = b.built || build
	if build && b.onFreeze != nil {
		b.onFreeze()
	}
	directory, _, _ := strings.Cut(strings.TrimPrefix(flake, "path:"), "#")
	output := filepath.Join(b.root, fmt.Sprintf("output-%d", b.prepared))
	if err := os.MkdirAll(output, 0700); err != nil {
		b.t.Fatal(err)
	}
	files := map[string]string{}
	expectations := provision.HomeFileExpectations{Files: []provision.HomeFileExpectation{}}
	targets := []string{}
	leaves := append([]string{}, b.hiddenLeaves...)
	for index, target := range b.targets {
		relative, err := filepath.Rel(os.Getenv("HOME"), target)
		if err != nil {
			b.t.Fatal(err)
		}
		name := sha256.Sum256([]byte(filepath.ToSlash(relative)))
		data, err := os.ReadFile(filepath.Join(directory, "files", fmt.Sprintf("%x", name)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			b.t.Fatal(err)
		}
		source := filepath.Join(output, fmt.Sprint(index))
		if err := os.WriteFile(source, data, 0444); err != nil {
			b.t.Fatal(err)
		}
		files[target] = source
		targets = append(targets, target)
		leaves = append(leaves, filepath.ToSlash(relative))
		sum := sha256.Sum256(data)
		expectations.Files = append(expectations.Files, provision.HomeFileExpectation{Target: filepath.ToSlash(target), SHA256: fmt.Sprintf("%x", sum)})
	}
	var rendered []byte
	if staged, _ := filepath.Glob(filepath.Join(directory, "typed", "*.toml")); b.typedTarget != "" && len(staged) == 1 {
		payload, err := os.ReadFile(staged[0])
		if err != nil {
			b.t.Fatal(err)
		}
		if b.renderTyped != nil {
			payload = b.renderTyped(payload)
		}
		relative, err := filepath.Rel(os.Getenv("HOME"), b.typedTarget)
		if err != nil {
			b.t.Fatal(err)
		}
		source := filepath.Join(output, "typed")
		if err := os.WriteFile(source, payload, 0444); err != nil {
			b.t.Fatal(err)
		}
		files[b.typedTarget] = source
		targets = append(targets, b.typedTarget)
		leaves = append(leaves, filepath.ToSlash(relative))
		rendered = payload
	}
	data, _ := json.Marshal(expectations)
	if err := os.WriteFile(filepath.Join(output, "endstate-home-files.json"), data, 0444); err != nil {
		b.t.Fatal(err)
	}
	if b.outputs == nil {
		b.outputs = map[string]map[string]string{}
	}
	b.outputs[output] = files
	if b.store != "" {
		writeBuiltActivation(b.t, b.store, output, leaves...)
		if rendered != nil {
			relative, _ := filepath.Rel(os.Getenv("HOME"), b.typedTarget)
			if err := os.WriteFile(filepath.Join(b.store, filepath.Base(output)+"-home-manager-files", filepath.FromSlash(relative)), rendered, 0o600); err != nil {
				b.t.Fatal(err)
			}
		}
	}
	b.flakes = append(b.flakes, directory)
	return &realizer.HomePreparation{Resolution: &realizer.InputResolution{Outputs: []string{"out"}, OutputPaths: map[string]string{"out": output}}, FlatTargets: targets, DefinedOutside: b.definedOutside}, nil
}
func (b *capturedHomeBackend) ActivatePreparedHome(resolution *realizer.InputResolution) (int, error) {
	b.activated++
	if b.fail {
		return 0, &realizer.Error{Code: "INSTALL_FAILED", Stage: "activation", Raw: "fixture activation failed"}
	}
	output := resolution.OutputPaths["out"]
	for target, source := range b.outputs[output] {
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			b.t.Fatal(err)
		}
		if info, err := os.Lstat(target); err == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				_ = os.Remove(target)
			} else if err := os.Rename(target, target+fmt.Sprintf(".backup-%d", b.activated)); err != nil {
				b.t.Fatal(err)
			}
		}
		if err := os.Symlink(source, target); err != nil {
			b.t.Fatal(err)
		}
	}
	b.homeObservation.Generation++
	b.active = output
	return b.homeObservation.Generation, nil
}
func (b *capturedHomeBackend) ObserveHome() (realizer.HomeObservation, error) {
	if b.observeErr != nil {
		return realizer.HomeObservation{}, b.observeErr
	}
	return realizer.HomeObservation{Present: b.homeObservation.Generation > 0, Generation: b.homeObservation.Generation, ActivationOutput: b.active}, nil
}

// directSiblingModule is an unregistered module whose declared writer is the
// direct restore strategy, captured beside the Home Manager ripgrep set.
func directSiblingModule(t *testing.T, sourceHome string) []*modules.Module {
	t.Helper()
	if err := os.WriteFile(filepath.Join(sourceHome, ".siblingrc"), []byte("sibling=1\n"), 0600); err != nil {
		t.Fatal(err)
	}
	sibling, err := modules.ParseModuleJSON([]byte(`{"moduleSchemaVersion":3,"id":"apps.sibling","displayName":"sibling","sensitivity":"low","platforms":{"linux":{"realization":"endstate-restore","matches":{"pathExists":["${home}/.siblingrc"]},"config":{"instanceDetectors":[{"id":"default","type":"path","glob":"${home}"}],"sets":[{"id":"default","generations":[{"id":"g1","order":1,"capture":{"files":[{"source":"${home}/.siblingrc","dest":"apps/sibling/.siblingrc"}]},"restore":[{"type":"copy","source":"apps/sibling/.siblingrc","target":"${home}/.siblingrc","backup":true}]}]}]}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	return []*modules.Module{sibling}
}

type capturedFixtureOptions struct {
	// sibling captures an independent direct-restore set beside the Home Manager set.
	sibling bool
	// installMarker is a home-relative path that the ripgrep instance detector requires;
	// the target host has it only after a package install creates it.
	installMarker string
	// typed captures the reviewed Poetry typed settings beside the ripgrep set.
	typed bool
}

// typedPoetryConfig is a live Poetry file with a comment and unsorted keys, which
// capture stores in canonical form.
const typedPoetryConfig = "# project environments\n[virtualenvs]\nin-project = true\ncreate = false\n"

// poetryModule is the production Poetry module with a live settings file in the source home.
func poetryModule(t *testing.T, sourceHome string) *modules.Module {
	t.Helper()
	directory := filepath.Join(sourceHome, ".config", "pypoetry")
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "config.toml"), []byte(typedPoetryConfig), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../modules/apps/poetry/module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return mod
}

func capturedStrategyFixture(t *testing.T, options capturedFixtureOptions) (string, string, string, manifest.ConfigCapture, *capturedHomeBackend) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("trusted Linux captured Home Manager lifecycle")
	}
	var siblings func(string) []*modules.Module
	if options.sibling || options.typed {
		siblings = func(sourceHome string) []*modules.Module {
			var extra []*modules.Module
			if options.sibling {
				extra = append(extra, directSiblingModule(t, sourceHome)...)
			}
			if options.typed {
				extra = append(extra, poetryModule(t, sourceHome))
			}
			return extra
		}
	}
	path, root, home, capture := configVerifyCaptureFixtureWithModule(t, func(mod *modules.Module, sourceHome string) {
		variant := mod.Platforms["linux"]
		variant.Realization = "home-manager"
		mod.Platforms["linux"] = variant
		second := filepath.Join(sourceHome, ".config", "ripgrep", "config")
		if err := os.MkdirAll(filepath.Dir(second), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(second, []byte("--smart-case\n"), 0600); err != nil {
			t.Fatal(err)
		}
		if options.installMarker != "" {
			marker := filepath.Join(sourceHome, filepath.FromSlash(options.installMarker))
			if err := os.WriteFile(marker, nil, 0600); err != nil {
				t.Fatal(err)
			}
			variant.Config.InstanceDetectors[0].Glob = "${home}/" + options.installMarker
		}
	}, siblings)
	catalogFiles := []string{"catalog/home-manager/registry.jsonc", "catalog/packages/ripgrep.jsonc"}
	if options.typed {
		catalogFiles = append(catalogFiles, "catalog/packages/poetry.jsonc")
	}
	for _, name := range catalogFiles {
		data, err := os.ReadFile(filepath.Join("../../..", name))
		if err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	catalog, diagnostics, catalogErr := loadConfigRestoreCatalogFn(root)
	if catalogErr != nil {
		t.Fatal(catalogErr)
	}
	snapshot := newConfigCatalogSnapshot(catalog, diagnostics)
	snapshot.pinHomeAuthorities(root)
	if _, ok := snapshot.homeAuthorities["apps.ripgrep"]; !ok {
		t.Fatalf("fixture has no reviewed current Home Manager authority: %+v", snapshot.homeAuthorities)
	}
	backend := &capturedHomeBackend{t: t, root: root, store: useFakeHomeStore(t), targets: []string{filepath.Join(home, ".ripgreprc"), filepath.Join(home, ".config", "ripgrep", "config")}}
	if options.typed {
		if _, ok := snapshot.homeAuthorities["apps.poetry"]; !ok {
			t.Fatalf("fixture has no reviewed current typed authority: %+v", snapshot.homeAuthorities)
		}
		backend.typedTarget = filepath.Join(home, ".config", "pypoetry", "config.toml")
		// The explicit configuration of the fixture defines no program.
		backend.definedOutside = []string{}
	}
	old := newRealizerFn
	newRealizerFn = func() (realizer.Realizer, error) { return backend, nil }
	t.Cleanup(func() { newRealizerFn = old })
	oldMaterialize := materializeConfigRestoreSetFn
	materializeConfigRestoreSetFn = func(ctx context.Context, request configrestore.Request) (*configrestore.MaterializedSet, error) {
		if request.Plan.SettingsStrategy == "home-manager" {
			t.Fatal("Home Manager set entered direct materialization")
		}
		return oldMaterialize(ctx, request)
	}
	t.Cleanup(func() { materializeConfigRestoreSetFn = oldMaterialize })
	return path, root, home, capture, backend
}

func TestCapturedHomeManagerApplyReapplyVerifyRepairAndRollback(t *testing.T) {
	path, _, home, capture, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	flags := ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true}
	raw, err := RunApply(flags)
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*ApplyResult)
	if result.HomeManager == nil || !result.HomeManager.Activated || backend.activated != 1 || len(result.ConfigResolutions) != 1 || result.ConfigResolutions[0].Status != planner.StatusRestored {
		t.Fatalf("declared writer did not complete: preparation=%d activation=%d resolutions=%+v result=%+v", backend.prepared, backend.activated, result.ConfigResolutions, result)
	}
	for _, target := range backend.targets {
		if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("captured target did not use native-like links: %s %v", target, err)
		}
	}
	assertConfigVerify(t, path, capture.CaptureID, "pass", "")
	history, listErr := provision.List()
	if listErr != nil || len(history) != 1 {
		t.Fatalf("missing activation provenance: %+v %v", history, listErr)
	}
	prior := history[0]
	if prior.HomeManager.Resolution == nil || prior.HomeManager.FileExpectations == nil || prior.HomeManager.SettingsDigest != "" || prior.HomeManager.SourceManifest != "" {
		t.Fatalf("temporary declarations leaked into provenance: %+v", prior.HomeManager)
	}
	if _, err := RunApply(flags); err != nil {
		t.Fatal(err)
	}
	if backend.activated != 2 {
		t.Fatal("reapply over existing native leaves was refused")
	}
	target := filepath.Join(home, ".ripgreprc")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("--no-hidden\n"), 0600); err != nil {
		t.Fatal(err)
	}
	assertConfigVerify(t, path, capture.CaptureID, "fail", "config_drift")
	if _, err := RunApply(flags); err != nil {
		t.Fatal(err)
	}
	assertConfigVerify(t, path, capture.CaptureID, "pass", "")
	rollbackRaw, rollbackErr := runRealizerRollback(RollbackFlags{To: fmt.Sprint(prior.Number), Confirm: true, EnableRestore: true}, backend)
	rollback, ok := rollbackRaw.(*RollbackResult)
	if !ok || rollbackErr != nil || rollback.HomeManager == nil || !rollback.HomeManager.Reactivated || rollback.HomeManager.Verification != "verified" {
		t.Fatalf("captured activation rollback failed: %+v %+v", rollback, rollbackErr)
	}
	assertConfigVerify(t, path, capture.CaptureID, "pass", "")
}

func resolutionFor(t *testing.T, fields *ConfigResultFields, moduleID string) planner.ConfigResolution {
	t.Helper()
	if fields != nil {
		for _, resolution := range fields.ConfigResolutions {
			if resolution.ModuleID == moduleID {
				return resolution
			}
		}
	}
	t.Fatalf("no config resolution for %s in %+v", moduleID, fields)
	return planner.ConfigResolution{}
}

func assertResolution(t *testing.T, resolution planner.ConfigResolution, status planner.TerminalStatus, reason planner.ResolutionReason) {
	t.Helper()
	got := planner.ResolutionReason("")
	if resolution.Reason != nil {
		got = *resolution.Reason
	}
	if resolution.Status != status || got != reason {
		t.Fatalf("%s resolution = %s/%q, want %s/%q", resolution.ModuleID, resolution.Status, got, status, reason)
	}
}

func assertNoFile(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s exists: %v", path, err)
		}
	}
}

func assertDirectCopy(t *testing.T, path, content string) {
	t.Helper()
	info, err := os.Lstat(path)
	data, readErr := os.ReadFile(path)
	if err != nil || readErr != nil || info.Mode()&os.ModeSymlink != 0 || string(data) != content {
		t.Fatalf("%s is not the direct copy %q: %v %v", path, content, err, readErr)
	}
}

func TestCapturedHomeManagerPreviewPreparesWithoutBuildingActivatingOrClaimingContent(t *testing.T) {
	path, _, _, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, DryRun: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusPlanned, "")
	if backend.prepared != 1 || backend.built || backend.activated != 0 || result.HomeManager == nil || result.HomeManager.Activated {
		t.Fatalf("preview did not stay a no-build, no-activation preparation: prepared=%d built=%v activated=%d home=%+v", backend.prepared, backend.built, backend.activated, result.HomeManager)
	}
	assertNoFile(t, backend.targets...)
}

func TestCapturedHomeManagerStandaloneRestoreSkipsWriterAndKeepsDirectSet(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	raw, err := RunRestore(RestoreFlags{Manifest: path, EnableRestore: true})
	if err != nil {
		t.Fatal(err)
	}
	data := raw.(*RestoreData)
	hm := resolutionFor(t, data.ConfigResultFields, "apps.ripgrep")
	assertResolution(t, hm, planner.StatusSkipped, planner.ReasonSettingsStrategyUnavailable)
	if hm.Remediation == nil || !strings.Contains(*hm.Remediation, "apply") {
		t.Fatalf("standalone restore did not point to apply or rebuild: %+v", hm.Remediation)
	}
	assertResolution(t, resolutionFor(t, data.ConfigResultFields, "apps.sibling"), planner.StatusRestored, "")
	assertNoFile(t, backend.targets...)
	assertDirectCopy(t, filepath.Join(home, ".siblingrc"), "sibling=1\n")
}

func TestCapturedHomeManagerMissingNixSkipsWriterAndKeepsDirectSet(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	old := bootstrapBackendsFn
	bootstrapBackendsFn = func([]bootstrap.Backend, bool, Consent, *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		return map[bootstrap.Backend]bool{}, nil
	}
	t.Cleanup(func() { bootstrapBackendsFn = old })
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusSkipped, planner.ReasonSettingsStrategyUnavailable)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.sibling"), planner.StatusRestored, "")
	if backend.prepared != 0 || backend.activated != 0 {
		t.Fatalf("missing Nix still reached the activation backend: prepared=%d activated=%d", backend.prepared, backend.activated)
	}
	assertNoFile(t, backend.targets...)
	assertDirectCopy(t, filepath.Join(home, ".siblingrc"), "sibling=1\n")
}

func TestCapturedHomeManagerApplyFreezesBeforeDirectWriterAndKeepsDirectSetDirect(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	siblingAtFreeze := true
	backend.onFreeze = func() {
		_, err := os.Lstat(filepath.Join(home, ".siblingrc"))
		siblingAtFreeze = err == nil
	}
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusRestored, "")
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.sibling"), planner.StatusRestored, "")
	if siblingAtFreeze || backend.activated != 1 {
		t.Fatalf("combined activation was not frozen before the direct writer: siblingAtFreeze=%v activated=%d", siblingAtFreeze, backend.activated)
	}
	assertDirectCopy(t, filepath.Join(home, ".siblingrc"), "sibling=1\n")
	for _, target := range backend.targets {
		if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("Home Manager target was not linked by activation: %s %v", target, err)
		}
	}
}

// declareInManifest rewrites the extracted capture manifest with extra top-level declarations.
func declareInManifest(t *testing.T, path string, declarations map[string]any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	document := map[string]any{}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for key, value := range declarations {
		document[key] = value
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0600); err != nil {
		t.Fatal(err)
	}
}

// importModuleIn returns the generated import module that remediation names and proves it persists.
func importModuleIn(t *testing.T, remediation *string) string {
	t.Helper()
	if remediation != nil {
		for _, word := range strings.Fields(*remediation) {
			if filepath.Base(word) == nix.HomeImportModule {
				if _, err := os.Stat(word); err != nil {
					t.Fatalf("remediation names a module that does not persist: %v", err)
				}
				return word
			}
		}
	}
	t.Fatalf("remediation does not name the generated import module: %v", remediation)
	return ""
}

func TestCapturedHomeManagerRejectsDuplicateExplicitOwnerBeforeComposition(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "explicit-rc"), []byte("--explicit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	declareInManifest(t, path, map[string]any{"homeManager": map[string]any{"settings": map[string]any{"files": map[string]string{".ripgreprc": "./explicit-rc"}}}})
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err == nil || err.Code != envelope.ErrRestoreFailed {
		t.Fatalf("duplicate owners were not rejected: %+v", err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusFailed, planner.ReasonTargetCollision)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.sibling"), planner.StatusRestored, "")
	if backend.activated != 0 {
		t.Fatalf("a dropped duplicate claim still activated: %d", backend.activated)
	}
	assertNoFile(t, backend.targets...)
	assertDirectCopy(t, filepath.Join(home, ".siblingrc"), "sibling=1\n")
}

func TestCapturedHomeManagerObservedExternalOwnerRequiresImport(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	backend.homeObservation.Generation, backend.active = 7, "/nix/store/external-owner"
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*ApplyResult)
	hm := resolutionFor(t, result.ConfigResultFields, "apps.ripgrep")
	assertResolution(t, hm, planner.StatusSkipped, planner.ReasonHomeManagerImportRequired)
	if hm.Resolution != planner.ResolutionDirect {
		t.Fatalf("import requirement lost the compatibility value: %s", hm.Resolution)
	}
	if result.HomeManager == nil || result.HomeManager.Activated || result.HomeManager.OwnershipAction == nil || result.HomeManager.OwnershipAction.Owner != "external" {
		t.Fatalf("observed external owner lost its inactive ownership action: %+v", result.HomeManager)
	}
	if module := importModuleIn(t, hm.Remediation); module != result.HomeManager.OwnershipAction.ModulePath {
		t.Fatalf("remediation names %q, ownership action names %q", module, result.HomeManager.OwnershipAction.ModulePath)
	}
	if backend.activated != 0 {
		t.Fatalf("external owner was activated over: %d", backend.activated)
	}
	assertNoFile(t, backend.targets...)
	assertDirectCopy(t, filepath.Join(home, ".siblingrc"), "sibling=1\n")
}

func TestCapturedHomeManagerBesideExplicitFlakeRequiresImportWithoutBorrowingActivation(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	flake := "path:" + filepath.Join(filepath.Dir(path), "explicit-flake") + "#user"
	declareInManifest(t, path, map[string]any{"homeManager": map[string]any{"flake": flake}})
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*ApplyResult)
	hm := resolutionFor(t, result.ConfigResultFields, "apps.ripgrep")
	assertResolution(t, hm, planner.StatusSkipped, planner.ReasonHomeManagerImportRequired)
	if hm.Resolution != planner.ResolutionDirect {
		t.Fatalf("import requirement lost the compatibility value: %s", hm.Resolution)
	}
	if result.HomeManager == nil || !result.HomeManager.Activated || result.HomeManager.Flake != flake || result.HomeManager.OwnershipAction != nil || backend.activated != 1 {
		t.Fatalf("explicit activation lost its own truth: %+v activated=%d", result.HomeManager, backend.activated)
	}
	module := importModuleIn(t, hm.Remediation)
	for _, target := range hm.ResolvedTargets {
		if target == module {
			t.Fatalf("import module leaked into resolved targets: %v", hm.ResolvedTargets)
		}
	}
	assertNoFile(t, backend.targets...)
	assertDirectCopy(t, filepath.Join(home, ".siblingrc"), "sibling=1\n")
}

// declareRipgrepPackage adds the ripgrep package to the capture manifest and scripts its install.
func declareRipgrepPackage(t *testing.T, path string, backend *capturedHomeBackend) string {
	t.Helper()
	const ref = "nixpkgs#ripgrep"
	declareInManifest(t, path, map[string]any{"apps": []map[string]any{{"id": "ripgrep", "refs": map[string]string{runtime.GOOS: ref}}}})
	backend.planDiff = realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: ref}}}
	backend.realizeResult = realizer.Result{Advanced: true, FromGeneration: 1, ToGeneration: 2, After: realizer.Set{Generation: 2, Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep", AttrPath: "ripgrep"}}}}
	return ref
}

func TestCapturedHomeManagerLateActivationFailureKeepsCommittedPackagesAndDirectResults(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	ref := declareRipgrepPackage(t, path, backend)
	backend.fail = true
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err == nil || err.Code != envelope.ErrInstallFailed {
		t.Fatalf("activation failure was not reported: %+v", err)
	}
	result, ok := raw.(*ApplyResult)
	if !ok {
		t.Fatalf("late failure dropped the partial result: %T", raw)
	}
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusFailed, planner.ReasonCommitFailed)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.sibling"), planner.StatusRestored, "")
	if len(result.Actions) != 1 || result.Actions[0].Status != "installed" {
		t.Fatalf("committed package result was lost: %+v", result.Actions)
	}
	history, listErr := provision.List()
	if listErr != nil || len(history) != 1 || !history[0].Partial || len(history[0].AddedRefs) != 1 || history[0].AddedRefs[0] != ref {
		t.Fatalf("committed package provenance was lost: %+v %v", history, listErr)
	}
	assertNoFile(t, backend.targets...)
	assertDirectCopy(t, filepath.Join(home, ".siblingrc"), "sibling=1\n")
}

func TestCapturedHomeManagerUsesFinalDetectionOfPackageInstalledByThisApply(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{installMarker: ".ripgrep-installed"})
	declareRipgrepPackage(t, path, backend)
	backend.onRealize = func() {
		if err := os.WriteFile(filepath.Join(home, ".ripgrep-installed"), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusRestored, "")
	if backend.activated != 1 {
		t.Fatalf("settings for a package installed by this apply were not activated: %d", backend.activated)
	}
}

func TestCapturedHomeManagerPlacesXDGTargetInsideHomeAtItsResolvedLocation(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	custom := filepath.Join(home, "custom-config")
	t.Setenv("XDG_CONFIG_HOME", custom)
	backend.targets = []string{filepath.Join(home, ".ripgreprc"), filepath.Join(custom, "ripgrep", "config")}
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	assertResolution(t, resolutionFor(t, raw.(*ApplyResult).ConfigResultFields, "apps.ripgrep"), planner.StatusRestored, "")
	if info, err := os.Lstat(filepath.Join(custom, "ripgrep", "config")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("placement did not reach the resolved XDG target: %v", err)
	}
	assertNoFile(t, filepath.Join(home, ".config", "ripgrep", "config"))
}

func TestCapturedHomeManagerRefusesTargetOutsideHomeWithoutGuessingARoot(t *testing.T) {
	path, root, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	outside := filepath.Join(root, "xdg-outside")
	t.Setenv("XDG_CONFIG_HOME", outside)
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	assertResolution(t, resolutionFor(t, raw.(*ApplyResult).ConfigResultFields, "apps.ripgrep"), planner.StatusSkipped, planner.ReasonSettingsStrategyUnavailable)
	if backend.activated != 0 {
		t.Fatalf("an unrepresentable target was activated: %d", backend.activated)
	}
	assertNoFile(t, filepath.Join(home, ".ripgreprc"), filepath.Join(outside, "ripgrep", "config"), filepath.Join(home, ".config", "ripgrep", "config"))
}

func TestConfigRestoreExecutionNeverTreatsMissingStrategyAsDirectCopy(t *testing.T) {
	runtime, plan := configRestoreExecutionFixture(t, "capture-unpinned")
	plan.Sets[0].SettingsStrategy = ""
	guard := &recordingLiveConfigRestoreGuard{base: t.TempDir()}
	restoreExecutionSeams(t,
		func(context.Context, string, string, configrestore.RegistryMutator, configrestore.HostBoundary) (liveConfigRestoreGuard, error) {
			return guard, nil
		},
		func(context.Context, migration.StageRequest) (*migration.StageResult, error) {
			t.Fatal("a set without a trusted strategy was staged")
			return nil, nil
		},
		func(context.Context, configrestore.Request) (*configrestore.MaterializedSet, error) {
			t.Fatal("a set without a trusted strategy entered direct materialization")
			return nil, nil
		},
		func(context.Context, configRestoreLiveSetRequest) configRestoreSetOutcome {
			t.Fatal("a set without a trusted strategy was written")
			return configRestoreSetOutcome{}
		},
	)
	session := &configRestoreExecutionSession{runtime: runtime, coordinator: &staticConfigRestoreCoordinator{preview: plan, final: plan}}
	result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{RestoreEnabled: true, RunID: "apply-unpinned", StateDir: t.TempDir()})
	if envErr != nil {
		t.Fatalf("Execute() error = %+v", envErr)
	}
	assertResolution(t, result.Plan.Sets[0].Resolution, planner.StatusSkipped, planner.ReasonSettingsStrategyUnavailable)
}

func TestCapturedHomeManagerVerifyReportsReplacedTargetAsDrift(t *testing.T) {
	path, _, home, capture, _ := capturedStrategyFixture(t, capturedFixtureOptions{})
	if _, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true}); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, ".ripgreprc")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	assertConfigVerify(t, path, capture.CaptureID, "fail", "config_drift")
}

func TestCapturedHomeManagerComposedRecordKeepsExplicitDeclaration(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	if err := os.WriteFile(filepath.Join(filepath.Dir(path), "explicit-rc"), []byte("--explicit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	declareInManifest(t, path, map[string]any{"homeManager": map[string]any{"settings": map[string]any{"files": map[string]string{".explicitrc": "./explicit-rc"}}}})
	backend.targets = append(backend.targets, filepath.Join(home, ".explicitrc"))
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	assertResolution(t, resolutionFor(t, raw.(*ApplyResult).ConfigResultFields, "apps.ripgrep"), planner.StatusRestored, "")
	recovered, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, backend)
	if recovered == nil || recovered.Settings == nil || recovered.Settings.Files[".explicitrc"] != "./explicit-rc" || len(warnings) != 0 {
		t.Fatalf("explicit declaration was not recoverable after composing a captured set: %+v %+v", recovered, warnings)
	}
}

func TestCapturedHomeManagerEmitsOneFinalResolutionPerSet(t *testing.T) {
	path, _, _, capture, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	backend.fail = true
	stream := captureStderr(t, func() {
		_, _ = RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true, Events: "jsonl"})
	})
	reasons := []any{}
	for _, line := range strings.Split(stream, "\n") {
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) == nil && event["event"] == "config-resolution" && event["captureId"] == capture.CaptureID {
			reasons = append(reasons, event["reason"])
		}
	}
	if len(reasons) != 1 || reasons[0] != planner.ReasonCommitFailed.String() {
		t.Fatalf("config-resolution events for the captured set = %v, want one final commit_failed", reasons)
	}
}

func TestCapturedHomeManagerLateFailureResultListsEveryActionItsSummaryCounts(t *testing.T) {
	path, _, _, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	declareRipgrepPackage(t, path, backend)
	declareInManifest(t, path, map[string]any{"apps": []map[string]any{
		{"id": "ripgrep", "refs": map[string]string{runtime.GOOS: "nixpkgs#ripgrep"}},
		{"id": "winonly", "driver": "winget", "refs": map[string]string{"windows": "Example.WinOnly"}},
	}})
	backend.fail = true
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err == nil {
		t.Fatal("activation failure was not reported")
	}
	result := raw.(*ApplyResult)
	counted := result.Summary.Success + result.Summary.Skipped + result.Summary.Failed
	if len(result.Actions) != counted {
		t.Fatalf("result lists %d actions but its summary counts %d: %+v", len(result.Actions), counted, result.Summary)
	}
}

func TestCapturedHomeManagerExternalOwnerFoundBeforeActivationRequiresImport(t *testing.T) {
	path, _, _, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	// The owner appears after the first observation, so only the pre-activation check sees it.
	backend.onFreeze = func() { backend.homeObservation.Generation, backend.active = 7, "/nix/store/external-owner" }
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*ApplyResult)
	hm := resolutionFor(t, result.ConfigResultFields, "apps.ripgrep")
	assertResolution(t, hm, planner.StatusSkipped, planner.ReasonHomeManagerImportRequired)
	importModuleIn(t, hm.Remediation)
	if backend.activated != 0 || result.HomeManager == nil || result.HomeManager.OwnershipAction == nil {
		t.Fatalf("external owner was activated over or lost its action: activated=%d %+v", backend.activated, result.HomeManager)
	}
	assertNoFile(t, backend.targets...)
}

func TestCapturedHomeManagerRowNeverAttemptedStaysPlannedNotFailedCommit(t *testing.T) {
	path, _, _, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	// The ownership state becomes unreadable after preparation, so apply stops before activation.
	backend.onFreeze = func() { backend.observeErr = errors.New("fixture profile unreadable") }
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err == nil {
		t.Fatal("the unreadable ownership state was not reported")
	}
	result, ok := raw.(*ApplyResult)
	if !ok {
		t.Fatalf("the stop dropped the config result: %T", raw)
	}
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusPlanned, "")
	if backend.activated != 0 {
		t.Fatalf("activation ran without a readable ownership state: %d", backend.activated)
	}
	assertNoFile(t, backend.targets...)
}

func TestCapturedHomeManagerUnreadableOwnershipFailsInsteadOfClaimingNoWriter(t *testing.T) {
	path, _, home, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{sibling: true})
	backend.observeErr = errors.New("fixture profile unreadable")
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*ApplyResult)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.ripgrep"), planner.StatusFailed, planner.ReasonTargetDetectionFailed)
	assertResolution(t, resolutionFor(t, result.ConfigResultFields, "apps.sibling"), planner.StatusRestored, "")
	if backend.activated != 0 {
		t.Fatalf("activation ran without a readable ownership state: %d", backend.activated)
	}
	assertNoFile(t, backend.targets...)
	assertDirectCopy(t, filepath.Join(home, ".siblingrc"), "sibling=1\n")
}

func TestCapturedHomeManagerUnreadableRegistryFailsInsteadOfClaimingNoWriter(t *testing.T) {
	path, root, _, _, backend := capturedStrategyFixture(t, capturedFixtureOptions{})
	if err := os.WriteFile(filepath.Join(root, "catalog", "home-manager", "registry.jsonc"), []byte("{ not a registry"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := RunApply(ApplyFlags{Manifest: path, EnableRestore: true, NoBootstrap: true})
	if err != nil {
		t.Fatal(err)
	}
	assertResolution(t, resolutionFor(t, raw.(*ApplyResult).ConfigResultFields, "apps.ripgrep"), planner.StatusFailed, planner.ReasonTargetDetectionFailed)
	if backend.activated != 0 {
		t.Fatalf("activation ran without a readable registry: %d", backend.activated)
	}
	assertNoFile(t, backend.targets...)
}
