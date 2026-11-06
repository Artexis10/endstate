// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

type nativeFreezeFixture struct {
	t        *testing.T
	backend  *nix.Backend
	elements map[string]any
	wrapper  string
	packages map[string]struct {
		DrvPath  string `json:"drvPath"`
		Priority int    `json:"priority"`
	}
	sourceChanged   bool
	activationBuilt bool
	activeHome      string
	activationFails bool
	priorityChanged bool
	nullAlpha       bool
	persistFails    bool
	additions       int
	removals        []string
}

// Native process and store-file boundaries are injected. Package identity, planning,
// mutation selection, provisioning persistence and capture use production code.
func newNativeFreezeFixture(t *testing.T) *nativeFreezeFixture {
	t.Helper()
	root := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", root)
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "xdg"))
	f := &nativeFreezeFixture{t: t, elements: map[string]any{}}
	artifact := filepath.Join(root, "endstate-home-files.json")
	if err := os.WriteFile(artifact, []byte(`{"files":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	previousReader := readHomeFilesArtifact
	readHomeFilesArtifact = func(path string, limit int64) ([]byte, os.FileMode, error) {
		if path == "/nix/store/home-output/endstate-home-files.json" {
			return safepath.ReadRegularFileBounded(artifact, limit)
		}
		return previousReader(path, limit)
	}
	t.Cleanup(func() { readHomeFilesArtifact = previousReader })
	f.backend = nix.New()
	f.backend.Profile = filepath.Join(root, "profile")
	f.backend.Run = f.run
	f.backend.Script = func(path string, args ...string) ([]byte, []byte, int, error) {
		if path != "/nix/store/home-output/activate" || len(args) != 0 {
			t.Fatalf("activation changed its prepared target: %s %v", path, args)
		}
		if f.activationFails {
			return nil, []byte("activation precheck failed"), 1, nil
		}
		f.activeHome = "/nix/store/home-output"
		homeProfile := filepath.Join(os.Getenv("XDG_STATE_HOME"), "nix", "profiles", "home-manager")
		if err := os.MkdirAll(filepath.Dir(homeProfile), 0700); err != nil {
			t.Fatal(err)
		}
		_ = os.Remove(homeProfile)
		if err := os.Symlink("home-manager-1-link", homeProfile); err != nil {
			t.Fatal(err)
		}
		return nil, nil, 0, nil
	}
	return f
}

func (f *nativeFreezeFixture) run(args ...string) ([]byte, []byte, int, error) {
	t := f.t
	result := func(value any) ([]byte, []byte, int, error) {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return data, nil, 0, nil
	}
	target := args[len(args)-1]
	switch args[0] {
	case "flake":
		if strings.HasPrefix(target, "path:") {
			return []byte(`{"url":` + fmt.Sprintf("%q", target) + `,"locked":{"type":"path"}}`), nil, 0, nil
		}
		return []byte(`{"url":"github:example/packages/1111111111111111111111111111111111111111","locked":{"type":"github","rev":"1111111111111111111111111111111111111111"}}`), nil, 0, nil
	case "build", "eval":
		dry := false
		for i, arg := range args {
			if arg == "--dry-run" {
				dry = true
			}
			if arg == "--output-lock-file" {
				if err := os.WriteFile(args[i+1], []byte(`{"version":7,"root":"root","nodes":{"root":{"inputs":{"nixpkgs":"nixpkgs","home-manager":"home-manager"}},"nixpkgs":{"locked":{"type":"github","rev":"1111111111111111111111111111111111111111"}},"home-manager":{"locked":{"type":"github","rev":"2222222222222222222222222222222222222222"}}}}`), 0600); err != nil {
					t.Fatal(err)
				}
			}
		}
		key, output, priority := "alpha", "dev", 7
		if strings.Contains(target, "otherAttribute") {
			key, output, priority = "beta", "out", 3
		}
		if strings.Contains(target, "homeConfigurations") || strings.HasPrefix(target, "/nix/store/home.drv") {
			key, output, priority = "home", "out", 5
		}
		frozenAlpha := target == "/nix/store/alpha.drv^dev"
		if key == "alpha" && f.sourceChanged && !frozenAlpha {
			key = "alpha-new"
		}
		drv := "/nix/store/" + key + ".drv"
		path := "/nix/store/" + key + "-" + output
		if key == "home" {
			path = "/nix/store/home-output"
		}
		if args[0] == "eval" {
			if f.priorityChanged {
				drv = "/nix/store/changed.drv"
			}
			return result(map[string]any{"drvPath": drv, "priority": priority})
		}
		if !dry {
			if frozenAlpha {
				return result([]any{map[string]any{"drvPath": drv, "outputs": map[string]string{output: path}}})
			}
			if target == "/nix/store/home.drv^out" {
				f.activationBuilt = true
				return result([]any{map[string]any{"drvPath": drv, "outputs": map[string]string{output: path}}})
			}
			// Bare store paths retain the derivation/metadata without building outputs.
			if strings.Contains(target, "^") {
				t.Fatalf("unexpected non-frozen build: %v", args)
			}
			return nil, nil, 0, nil
		}
		outputs := map[string]any{output: path}
		if key == "alpha" && f.nullAlpha {
			outputs[output] = nil
		}
		return result([]any{map[string]any{"drvPath": drv, "outputs": outputs}})
	case "store":
		data, err := os.ReadFile(filepath.Join(target, "packages.json"))
		if err != nil {
			t.Fatal(err)
		}
		var metadata struct {
			Packages map[string]struct {
				DrvPath  string `json:"drvPath"`
				Priority int    `json:"priority"`
			} `json:"packages"`
		}
		if err = json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		f.packages = metadata.Packages
		sum := sha256.Sum256(data)
		f.wrapper = fmt.Sprintf("path:/nix/store/%x-metadata", sum[:16])
		f.sourceChanged = true
		return []byte(strings.TrimPrefix(f.wrapper, "path:")), nil, 0, nil
	case "path-info":
		if f.activeHome == "" {
			return nil, nil, 1, nil
		}
		return []byte(f.activeHome + "\n"), nil, 0, nil
	case "profile":
		switch args[1] {
		case "list":
			return result(map[string]any{"version": 3, "elements": f.elements})
		case "add":
			f.additions++
			if !f.activationBuilt {
				t.Fatal("packages mutated before generated activation was frozen and built")
			}
			refs := args[4 : len(args)-2]
			if len(refs) != 2 || refs[0] != f.wrapper+"#p0^dev" || refs[1] != f.wrapper+"#p1^out" {
				t.Fatalf("profile mutation used floating source instead of prepared resolution: %v", refs)
			}
			if f.packages["p0"].DrvPath != "/nix/store/alpha.drv" || f.packages["p0"].Priority != 7 || f.packages["p1"].Priority != 3 {
				t.Fatalf("metadata changed native selection: %+v", f.packages)
			}
			f.elements["native-alpha"] = map[string]any{"attrPath": "packages." + nix.System() + ".p0", "originalUrl": f.wrapper, "url": f.wrapper, "storePaths": []string{"/nix/store/alpha-dev"}}
			f.elements["native-beta"] = map[string]any{"attrPath": "packages." + nix.System() + ".p1", "originalUrl": f.wrapper, "url": f.wrapper, "storePaths": []string{"/nix/store/beta-out"}}
			if err := os.Symlink("profile-1-link", f.backend.Profile); err != nil {
				t.Fatal(err)
			}
			// The generated source is now different too; activation must use its output.
			paths, _ := filepath.Glob(filepath.Join(os.Getenv("ENDSTATE_ROOT"), "state", "home-manager", "*", "home.nix"))
			for _, path := range paths {
				if err := os.WriteFile(path, []byte("throw \"changed\""), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if f.persistFails {
				if err := os.WriteFile(provision.Dir(), []byte("not a directory"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			return nil, nil, 0, nil
		case "remove":
			f.removals = append(f.removals, args[4:len(args)-2]...)
			return nil, nil, 0, nil
		}
	}
	t.Fatalf("unexpected native command: %v", args)
	return nil, nil, 1, nil
}

func freezeManifest(t *testing.T) (*manifest.Manifest, ApplyFlags) {
	t.Helper()
	mf := &manifest.Manifest{Version: 1, Name: "freeze", Apps: []manifest.App{{ID: "portable-alpha", Refs: map[string]string{runtime.GOOS: "path:/changing-source#renamedAttribute^dev"}}, {ID: "portable-beta", Refs: map[string]string{runtime.GOOS: "otherAttribute"}}}, HomeManager: &manifest.HomeManagerConfig{Settings: &manifest.HomeManagerSettings{Git: &manifest.GitSettings{DefaultBranch: "trunk"}}}}
	path := filepath.Join(t.TempDir(), "manifest.json")
	declaration := &manifest.Manifest{Version: 1, Name: "included", Apps: mf.Apps[1:]}
	mf.Apps = mf.Apps[:1]
	data, err := json.Marshal(declaration)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(filepath.Dir(path), "included.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	mf.Includes = []string{"included.json"}
	data, err = json.Marshal(mf)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	loaded, loadErr := loadManifest(path)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	return loaded, ApplyFlags{Manifest: path, EnableRestore: true, Prune: true, Confirm: true}
}

func TestApplyNativeFreezeSurvivesSourceChange(t *testing.T) {
	f := newNativeFreezeFixture(t)
	f.nullAlpha = true
	mf, flags := freezeManifest(t)
	raw, eerr := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "freeze", nil, nil, nil, nil, nil)
	if eerr != nil {
		t.Fatal(eerr)
	}
	if raw.(*ApplyResult).Summary.Success != 2 || len(f.removals) != 0 {
		t.Fatalf("apply/prune: %+v, removed %v", raw, f.removals)
	}
	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 1 || len(gens[0].Items) != 2 || gens[0].HomeManager == nil {
		t.Fatalf("history: %+v", gens)
	}
	home := gens[0].HomeManager
	if home.Settings != nil || home.Kind != "settings" || home.SettingsDigest == "" || home.ActivationOutput != "/nix/store/home-output" || home.Resolution.Derivation != "/nix/store/home.drv" {
		t.Fatalf("generated history lost declaration/activation identity: %+v", home)
	}
	for _, item := range gens[0].Items {
		if item.Resolution == nil || item.Resolution.WrapperURL == "" {
			t.Fatalf("package lost native identity: %+v", item)
		}
	}
	// A new backend has no in-run identity annotations: persisted evidence must work.
	fresh := nix.New()
	fresh.Profile = f.backend.Profile
	fresh.Run = f.run
	flags.EnableRestore = false
	if _, err := runApplyRealizer(flags, mf, fresh, noopEmitter(), "rerun", nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if f.additions != 1 || len(f.removals) != 0 {
		t.Fatalf("rerun changed declared packages: adds %d, removed %v", f.additions, f.removals)
	}
	current, err := fresh.Current()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := installedElement(current, realizer.Installable{ID: "portable-alpha", Ref: mf.Apps[0].Refs[runtime.GOOS]}); !ok {
		t.Fatal("persisted native identity unavailable")
	}
	cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, fresh)
	if cfg == nil || cfg.Settings == nil || cfg.Settings.Git.DefaultBranch != "trunk" || len(warnings) != 0 {
		t.Fatalf("capture declaration: %+v %v", cfg, warnings)
	}
	captured, captureErr := runCaptureRealizer(CaptureFlags{Out: filepath.Join(t.TempDir(), "capture.json")}, fresh, noopEmitter())
	if captureErr != nil {
		t.Fatal(captureErr)
	}
	capture := captured.(*CaptureResult)
	if len(capture.AppsIncluded) != 1 || capture.AppsIncluded[0].ID != "portable-beta" || len(capture.Warnings) != 1 || capture.Warnings[0].Code != "input_provenance_unavailable" {
		t.Fatalf("capture fabricated mutable package intent: %+v", capture)
	}
	inventory, err := (discovery.RealizerInventoryAdapter{Realizer: fresh}).Inventory(t.Context())
	if err != nil || len(inventory.Resolved) != 1 || inventory.Resolved[0].ID != "portable-beta" || strings.Contains(inventory.Resolved[0].Installable, "metadata") {
		t.Fatalf("discovery lost original package truth: %+v %v", inventory, err)
	}
	f.activeHome = "/nix/store/external-output"
	if cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, fresh); cfg != nil || len(warnings) != 1 {
		t.Fatalf("generation integer masked external ownership: %+v %v", cfg, warnings)
	}
	f.activeHome = "/nix/store/home-output"
	// Reload dependencies without invalidating settings for unrelated app changes.
	includePath := filepath.Join(filepath.Dir(flags.Manifest), "included.json")
	before, err := os.ReadFile(includePath)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(includePath, []byte(`{"version":1,"name":"included","apps":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, fresh); cfg == nil || len(warnings) != 0 {
		t.Fatalf("unrelated include changed the settings digest: %+v %v", cfg, warnings)
	}
	if err = os.WriteFile(includePath, []byte(`{"version":`), 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, fresh); cfg != nil || len(warnings) != 1 {
		t.Fatalf("capture ignored an unreadable dependency: %+v %v", cfg, warnings)
	}
	if err = os.WriteFile(includePath, before, 0600); err != nil {
		t.Fatal(err)
	}
	// Changing the declared settings invalidates their recorded digest.
	mf.HomeManager.Settings.Git.DefaultBranch = "changed"
	data, _ := json.Marshal(mf)
	if err := os.WriteFile(flags.Manifest, data, 0600); err != nil {
		t.Fatal(err)
	}
	if cfg, warnings := recoverHomeManagerWithWarnings(CaptureFlags{}, fresh); cfg != nil || len(warnings) != 1 {
		t.Fatalf("capture exported changed source: %+v %v", cfg, warnings)
	}
}

func TestApplyNativeFreezeRecordsPackagesAfterActivationFailure(t *testing.T) {
	f := newNativeFreezeFixture(t)
	mf, flags := freezeManifest(t)
	f.activationFails = true
	_, eerr := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "failed-home", nil, nil, nil, nil, nil)
	if eerr == nil {
		t.Fatal("activation failure was hidden")
	}
	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 1 || len(gens[0].Items) != 2 || gens[0].HomeManager != nil || gens[0].Native != "1" || !gens[0].Partial {
		t.Fatalf("committed package provenance lost or home fabricated: %+v", gens)
	}
}

func TestApplyNativeFreezeRefusesChangedPriorityTargetBeforeMutation(t *testing.T) {
	f := newNativeFreezeFixture(t)
	mf, flags := freezeManifest(t)
	f.priorityChanged = true
	_, eerr := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "changed-priority", nil, nil, nil, nil, nil)
	if eerr == nil || string(eerr.Code) != "INPUT_PROVENANCE_UNRESOLVED" || f.additions != 0 || len(f.removals) != 0 {
		t.Fatalf("changed native target was not refused before mutation: %v", eerr)
	}
	gens, err := provision.List()
	if err != nil || len(gens) != 0 {
		t.Fatalf("uncommitted operation wrote provenance: %+v %v", gens, err)
	}
}

func TestApplyNativeFreezeReportsCommittedProvenanceFailure(t *testing.T) {
	f := newNativeFreezeFixture(t)
	mf, flags := freezeManifest(t)
	f.persistFails = true
	_, eerr := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "unwritten", nil, nil, nil, nil, nil)
	if eerr == nil || string(eerr.Code) != "PROVENANCE_WRITE_FAILED" || f.additions != 1 {
		t.Fatalf("committed state was reported as complete success: %v", eerr)
	}
}

func TestApplyNativeFreezeDryRunDoesNotBuildActivation(t *testing.T) {
	f := newNativeFreezeFixture(t)
	mf, flags := freezeManifest(t)
	flags.DryRun = true
	raw, err := runApplyRealizer(flags, mf, f.backend, noopEmitter(), "dry-freeze", nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !raw.(*ApplyResult).DryRun || f.activationBuilt || f.additions != 0 || f.activeHome != "" {
		t.Fatalf("dry run mutated user state: %+v", raw)
	}
	roots, _ := filepath.Glob(filepath.Join(filepath.Dir(f.backend.Profile), "frozen-roots-*"))
	if len(roots) != 0 {
		t.Fatalf("uncommitted native roots leaked: %v", roots)
	}
}

type frozenHomeRollbackFixture struct {
	fakeRealizer
	restored *realizer.InputResolution
}

func (*frozenHomeRollbackFixture) PrepareHome(string, bool) (*realizer.InputResolution, error) {
	panic("rollback must not evaluate a source")
}
func (f *frozenHomeRollbackFixture) ActivatePreparedHome(r *realizer.InputResolution) (int, error) {
	f.restored = r
	return 8, nil
}
func (*frozenHomeRollbackFixture) ActiveHomeOutput() string { return "/nix/store/old-output" }

func TestRollbackGeneratedHomeUsesRecordedFrozenActivation(t *testing.T) {
	r := &frozenHomeRollbackFixture{}
	resolution := &realizer.InputResolution{Derivation: "/nix/store/old-home.drv", Outputs: []string{"out"}, OutputPaths: map[string]string{"out": "/nix/store/old-output"}}
	home := &provision.HomeGenRef{Kind: "settings", Generated: true, Flake: "path:/latest/generated#profile", Resolution: resolution, ActivationOutput: "/nix/store/old-output", Generation: 2}
	generation, _, err := rollbackHomeConfig(r, home)
	if err != nil || generation != 8 || r.restored != resolution || r.activateCalls != 0 {
		t.Fatalf("rollback used mutable generated source: %d %v %+v", generation, err, r.restored)
	}
	home.Resolution = nil
	r.restored = nil
	if _, _, err := rollbackHomeConfig(r, home); err == nil || r.activateCalls != 0 || r.restored != nil {
		t.Fatal("unavailable frozen history fell back to the latest generated source")
	}
}

func TestRollbackRecordsNativeCommitWhenLaterHomeRollbackFails(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	target := &provision.Generation{Backend: "nix", Native: "7", HomeManager: &provision.HomeGenRef{Flake: "github:example/config#profile", Generation: 3}}
	if err := provision.Write(target); err != nil {
		t.Fatal(err)
	}
	r := &fakeRealizer{caps: provision.Capabilities{NativeRollback: true}, currentSet: realizer.Set{Generation: 11, Elements: map[string]realizer.Element{}}, homeRollbackErr: &realizer.Error{Code: envelope.ErrInstallFailed, Raw: "activation precheck failed"}}
	_, eerr := runRealizerRollback(RollbackFlags{To: fmt.Sprint(target.Number), Confirm: true, EnableRestore: true}, r)
	if eerr == nil || r.rollbackCalls != 1 {
		t.Fatalf("rollback failure was hidden or packages were not committed: %v", eerr)
	}
	gens, err := provision.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(gens) != 2 || !gens[0].Rollback || !gens[0].Partial || gens[0].Native != "11" || gens[0].HomeManager != nil {
		t.Fatalf("native rollback commit lost or home generation fabricated: %+v", gens)
	}
}
