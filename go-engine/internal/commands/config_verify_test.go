// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/discovery"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestVerifyCapturedContentReportsUnavailableOrUnsafeEvidence(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("production Linux module workflow")
	}
	for _, test := range []struct {
		name, status, reason string
		change               func(*testing.T, string, string, manifest.ConfigCapture)
	}{
		{"optional source absent", "pass", "", func(t *testing.T, _, home string, _ manifest.ConfigCapture) {
			path := filepath.Join(home, ".config", "ripgrep", "config")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("unrelated optional target\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing target", "fail", "missing", func(t *testing.T, _, home string, _ manifest.ConfigCapture) {
			if err := os.Remove(filepath.Join(home, ".ripgreprc")); err != nil {
				t.Fatal(err)
			}
		}},
		{"altered payload", "fail", "payload_integrity_failed", func(t *testing.T, path, _ string, capture manifest.ConfigCapture) {
			payload := filepath.Join(filepath.Dir(path), filepath.FromSlash(capture.PayloadRoot), capture.PayloadManifest[0].RelativePath)
			if err := os.WriteFile(payload, []byte("changed payload\n"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"missing payload", "fail", "payload_integrity_failed", func(t *testing.T, path, _ string, capture manifest.ConfigCapture) {
			payload := filepath.Join(filepath.Dir(path), filepath.FromSlash(capture.PayloadRoot), capture.PayloadManifest[0].RelativePath)
			if err := os.Remove(payload); err != nil {
				t.Fatal(err)
			}
		}},
		{"linked target", "fail", "config_read_failed", func(t *testing.T, _, home string, _ manifest.ConfigCapture) {
			target := filepath.Join(home, ".ripgreprc")
			if err := os.Rename(target, target+".outside"); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target+".outside", target); err != nil {
				t.Fatal(err)
			}
		}},
		{"ambiguous instance", "skipped", "ambiguous_target_instance", func(t *testing.T, _, home string, _ manifest.ConfigCapture) {
			catalog, _, err := loadConfigRestoreCatalogFn("")
			if err != nil {
				t.Fatal(err)
			}
			catalog["apps.ripgrep"].Config.InstanceDetectors = []modules.InstanceDetectorDef{{ID: "other", Type: "path", Glob: "${home}/*"}}
			for _, name := range []string{"one", "two"} {
				if err := os.Mkdir(filepath.Join(home, name), 0700); err != nil {
					t.Fatal(err)
				}
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifestPath, root, home, capture := configVerifyCaptureFixture(t)
			if err := os.WriteFile(filepath.Join(home, ".ripgreprc"), []byte("--hidden\n--line-number\n"), 0600); err != nil {
				t.Fatal(err)
			}
			test.change(t, manifestPath, home, capture)
			withMockDriver(&mockDriver{}, func() {
				assertConfigVerify(t, manifestPath, capture.CaptureID, test.status, test.reason)
			})
			if _, err := os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
				t.Fatalf("verify created a restore store: %v", err)
			}
		})
	}
}

func TestVerifyCapturedContentIntegrityFailureOutweighsAmbiguousTarget(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("production Linux module workflow")
	}
	path, _, home, capture := configVerifyCaptureFixture(t)
	payload := filepath.Join(filepath.Dir(path), filepath.FromSlash(capture.PayloadRoot), capture.PayloadManifest[0].RelativePath)
	if err := os.WriteFile(payload, []byte("changed payload\n"), 0600); err != nil {
		t.Fatal(err)
	}
	catalog, _, err := loadConfigRestoreCatalogFn("")
	if err != nil {
		t.Fatal(err)
	}
	catalog["apps.ripgrep"].Config.InstanceDetectors = []modules.InstanceDetectorDef{{ID: "other", Type: "path", Glob: "${home}/*"}}
	for _, name := range []string{"one", "two"} {
		if err := os.Mkdir(filepath.Join(home, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	withMockDriver(&mockDriver{}, func() {
		assertConfigVerify(t, path, capture.CaptureID, "fail", "payload_integrity_failed")
	})
}

func TestVerifyCapturedConfigSetWithAllDirectoryMembersExcludedIsSkipped(t *testing.T) {
	runtime, plan := configRestoreExecutionFixture(t, "capture-excluded")
	source := runtime.inputs.generationSources[0]
	sourceDir := filepath.Join(source.payloadRoot, "prefs")
	if err := os.Mkdir(sourceDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "owned.txt"), []byte("captured"), 0600); err != nil {
		t.Fatal(err)
	}
	var err error
	source.payloadManifest, err = bundle.BuildPayloadManifest(source.payloadRoot)
	if err != nil {
		t.Fatal(err)
	}
	set := plan.Sets[0]
	target := filepath.Join(set.TargetInstances[0].Root, "prefs")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "owned.txt"), []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	set.TargetGenerationDef.Restore = []modules.RestoreDef{{Type: "copy", Source: "prefs", Target: target, Exclude: []string{"**/owned.txt"}}}
	item := verifyCapturedConfigSet(context.Background(), VerifyItem{Type: "config", ID: set.Source.CaptureID}, set, source, nil, nil, nil, "")
	if item.Status != "skipped" || item.Reason != "config_content_unchecked" {
		t.Fatalf("excluded captured content produced a pass: %+v", item)
	}
	if got, err := os.ReadFile(filepath.Join(target, "owned.txt")); err != nil || string(got) != "different" {
		t.Fatalf("excluded target changed: %q, %v", got, err)
	}
}

func TestVerifyCapturedConfigSetFailureOutweighsUncheckedOperations(t *testing.T) {
	runtime, plan := configRestoreExecutionFixture(t, "capture-preferences")
	source := runtime.inputs.generationSources[0]
	set := plan.Sets[0]
	target := filepath.Join(set.TargetInstances[0].Root, "settings.txt")
	if err := os.WriteFile(target, []byte("different"), 0600); err != nil {
		t.Fatal(err)
	}
	oldStage, oldMaterialize := stageConfigRestoreSetFn, materializeConfigRestoreSetFn
	stageConfigRestoreSetFn = func(context.Context, migration.StageRequest) (*migration.StageResult, error) {
		return &migration.StageResult{Root: t.TempDir()}, nil
	}
	materializeConfigRestoreSetFn = func(context.Context, configrestore.Request) (*configrestore.MaterializedSet, error) {
		return &configrestore.MaterializedSet{Actions: []configrestore.Action{
			{Kind: configrestore.ActionRegistrySet},
			{Kind: configrestore.ActionWriteFile, Target: target, DesiredContent: []byte("desired")},
		}}, nil
	}
	t.Cleanup(func() { stageConfigRestoreSetFn, materializeConfigRestoreSetFn = oldStage, oldMaterialize })
	item := verifyCapturedConfigSet(context.Background(), VerifyItem{Type: "config", ID: source.source.CaptureID}, set, source, nil, nil, nil, "")
	if item.Status != "fail" || item.Reason != "config_drift" {
		t.Fatalf("known drift was hidden by unsupported operation: %+v", item)
	}
	if err := os.WriteFile(target, []byte("desired"), 0600); err != nil {
		t.Fatal(err)
	}
	item = verifyCapturedConfigSet(context.Background(), item, set, source, nil, nil, nil, "")
	if item.Status != "skipped" || item.Reason != "config_content_unchecked" {
		t.Fatalf("unchecked operation produced a pass: %+v", item)
	}
}

func TestVerifyCapturedConfigSetWithoutCapturedContentIsSkipped(t *testing.T) {
	runtime, plan := configRestoreExecutionFixture(t, "capture-empty")
	set := plan.Sets[0]
	set.TargetGenerationDef.Restore = []modules.RestoreDef{{
		Type: "copy", Source: "optional.txt", Target: filepath.Join(set.TargetInstances[0].Root, "optional.txt"), Optional: true,
	}}
	item := verifyCapturedConfigSet(context.Background(), VerifyItem{Type: "config", ID: set.Source.CaptureID}, set, runtime.inputs.generationSources[0], nil, nil, nil, "")
	if item.Status != "skipped" || item.Reason != "config_content_unchecked" {
		t.Fatalf("empty captured content produced a pass: %+v", item)
	}
}

func TestRebuildConfigVerificationRetainsSelectedTarget(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("production Linux module workflow")
	}
	manifestPath, _, home, capture := configVerifyCaptureFixture(t)
	catalog, _, err := loadConfigRestoreCatalogFn("")
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog["apps.ripgrep"]
	mod.Config.InstanceDetectors = []modules.InstanceDetectorDef{{ID: "other", Type: "path", Glob: "${home}/*"}}
	for _, name := range []string{"one", "two"} {
		if err := os.Mkdir(filepath.Join(home, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	instances, err := modules.DiscoverInstances(mod, nil, modules.DiscoveryOptions{})
	if err != nil || len(instances) != 2 {
		t.Fatalf("ambiguous target fixture: %+v, %v", instances, err)
	}
	withMockDriver(&mockDriver{}, func() {
		assertConfigVerify(t, manifestPath, capture.CaptureID, "skipped", "ambiguous_target_instance")
		raw, envErr := RunRebuild(RebuildFlags{From: manifestPath, Confirm: true, RestoreTargets: []string{capture.CaptureID + "=" + instances[0].ID}})
		if envErr != nil {
			t.Fatalf("selected-target rebuild: %+v", envErr)
		}
		result := raw.(*RebuildResult).Verify.(*VerifyResult)
		for _, item := range result.Results {
			if item.Type == "config" && item.ID == capture.CaptureID {
				if item.Status != "pass" {
					t.Fatalf("rebuild lost its selected target: %+v", item)
				}
				return
			}
		}
		t.Fatalf("rebuild omitted config verification: %+v", result)
	})
}

func TestVerifyCapturedContentDetectsDriftAndRepairRevertRestoresDrift(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("production Linux module workflow")
	}
	for _, backend := range []string{"driver", "realizer"} {
		t.Run(backend, func(t *testing.T) {
			manifestPath, root, targetHome, capture := configVerifyCaptureFixture(t)
			oldDriver, oldRealizer := newDriverFn, newRealizerFn
			newDriverFn = func() (driver.Driver, error) { return &mockDriver{}, nil }
			newRealizerFn = func() (realizer.Realizer, error) {
				if backend == "realizer" {
					return &fakeRealizer{}, nil
				}
				return nil, ErrNoRealizer
			}
			t.Cleanup(func() { newDriverFn, newRealizerFn = oldDriver, oldRealizer })
			desired := []byte("--hidden\n--line-number\n")
			target := filepath.Join(targetHome, ".ripgreprc")
			if err := os.WriteFile(target, desired, 0600); err != nil {
				t.Fatal(err)
			}
			unrelated := filepath.Join(targetHome, "unmanaged.txt")
			if err := os.WriteFile(unrelated, []byte("keep me\n"), 0600); err != nil {
				t.Fatal(err)
			}
			assertConfigVerify(t, manifestPath, capture.CaptureID, "pass", "")
			if _, err := os.Stat(filepath.Join(root, "state")); !os.IsNotExist(err) {
				t.Fatalf("verify created a restore store: %v", err)
			}
			if err := os.WriteFile(target, []byte("before first restore\n"), 0600); err != nil {
				t.Fatal(err)
			}
			first, envErr := RunRestore(RestoreFlags{Manifest: manifestPath, EnableRestore: true})
			if envErr != nil || first.(*RestoreData).ConfigResolutions[0].Status != planner.StatusRestored {
				t.Fatalf("first restore: %+v, %+v", first, envErr)
			}
			originalHistory := configVerifyFileBytes(t, filepath.Join(root, "state"))
			assertConfigVerify(t, manifestPath, capture.CaptureID, "pass", "")
			drifted := []byte("--no-hidden\n--line-number\n")
			if err := os.WriteFile(target, drifted, 0600); err != nil {
				t.Fatal(err)
			}
			beforeVerify := configVerifyFileBytes(t, root)
			assertConfigVerify(t, manifestPath, capture.CaptureID, "fail", "config_drift")
			if after := configVerifyFileBytes(t, root); !reflect.DeepEqual(beforeVerify, after) {
				t.Fatal("verify changed target bytes or restore history")
			}
			repaired, envErr := RunRestore(RestoreFlags{Manifest: manifestPath, EnableRestore: true})
			if envErr != nil || repaired.(*RestoreData).ConfigResolutions[0].Status != planner.StatusRestored {
				t.Fatalf("repair: %+v, %+v", repaired, envErr)
			}
			assertConfigVerify(t, manifestPath, capture.CaptureID, "pass", "")
			if _, envErr := RunRevert(RevertFlags{}); envErr != nil {
				t.Fatalf("repair revert: %+v", envErr)
			}
			if got, err := os.ReadFile(target); err != nil || !bytes.Equal(got, drifted) {
				t.Fatalf("repair revert bytes = %q, %v; want %q", got, err, drifted)
			}
			for path, want := range originalHistory {
				got, err := os.ReadFile(filepath.Join(root, "state", path))
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("original restore history changed at %s: %v", path, err)
				}
			}
			if got, err := os.ReadFile(unrelated); err != nil || string(got) != "keep me\n" {
				t.Fatalf("unrelated file changed: %q, %v", got, err)
			}
		})
	}
}

func configVerifyCaptureFixture(t *testing.T) (string, string, string, manifest.ConfigCapture) {
	return configVerifyCaptureFixtureWithModule(t, nil, nil)
}

// configVerifyCaptureFixtureWithModule captures the ripgrep module, optionally
// reshaped by configure, together with any independent modules that siblings
// builds from the source home.
func configVerifyCaptureFixtureWithModule(t *testing.T, configure func(*modules.Module, string), siblings func(string) []*modules.Module) (string, string, string, manifest.ConfigCapture) {
	t.Helper()
	root := t.TempDir()
	sourceHome := filepath.Join(root, "source-home")
	if err := os.MkdirAll(sourceHome, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", sourceHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(sourceHome, ".config"))
	if err := os.WriteFile(filepath.Join(sourceHome, ".ripgreprc"), []byte("--hidden\n--line-number\n"), 0600); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("../../../modules/apps/ripgrep/module.jsonc")
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if configure != nil {
		configure(mod, sourceHome)
		encoded, err := json.Marshal(mod)
		if err != nil {
			t.Fatal(err)
		}
		mod, err = modules.ParseModuleJSON(encoded)
		if err != nil {
			t.Fatal(err)
		}
	}
	authored := map[string]*modules.Module{mod.ID: mod}
	only := mod.ID
	var siblingCandidates []discovery.SettingsCandidate
	if siblings != nil {
		for _, sibling := range siblings(sourceHome) {
			authored[sibling.ID] = sibling
			only += "," + sibling.ID
			// Discovery detects registry adapters only, so the unregistered sibling is announced here.
			siblingCandidates = append(siblingCandidates, discovery.SettingsCandidate{
				ID: sibling.ID, ModuleID: sibling.ID, DisplayName: sibling.DisplayName,
				Actions: []discovery.SettingsAction{discovery.SettingsCapture, discovery.SettingsRestore},
			})
		}
	}
	catalog := modules.FilterCatalogForPlatform(authored, "linux")
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("ENDSTATE_ROOT", repo)
	outputPath := filepath.Join(root, "capture.endstate")
	func() {
		oldRoot, oldRealizer, oldDiscovery, oldCatalog := resolveRepoRootFn, newRealizerFn, discoverLinuxFn, loadCaptureModuleCatalogFn
		defer func() {
			resolveRepoRootFn, newRealizerFn, discoverLinuxFn = oldRoot, oldRealizer, oldDiscovery
			loadCaptureModuleCatalogFn = oldCatalog
		}()
		resolveRepoRootFn = func() string { return repo }
		loadCaptureModuleCatalogFn = modules.GetCatalogWithDiagnostics
		if configure != nil || siblings != nil {
			loadCaptureModuleCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
				return authored, nil, nil
			}
		}
		newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
		discoverLinuxFn = func(ctx context.Context, request discovery.Request, _ realizer.Realizer) (discovery.Result, error) {
			request.SelectedSources = []string{discovery.HomeManagerSettingsSource}
			result, err := discovery.Discover(ctx, request)
			result.Settings = append(result.Settings, siblingCandidates...)
			return result, err
		}
		withFakeGenerations(nil, errors.New("no managed profile"), func() {
			if _, err := RunCapture(CaptureFlags{Out: outputPath, Only: only}); err != nil {
				t.Fatal(err)
			}
		})
	}()
	extracted, err := bundle.ExtractBundle(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Dir(extracted)) })
	loaded, err := manifest.LoadManifest(extracted)
	if err != nil || len(loaded.ConfigCaptures) != len(authored) {
		t.Fatalf("capture manifest: %+v, %v", loaded, err)
	}
	var primary manifest.ConfigCapture
	for _, capture := range loaded.ConfigCaptures {
		if capture.ModuleID == mod.ID {
			primary = capture
		}
	}
	if loaded.HomeManager != nil || primary.SourceGeneration != "g2" {
		t.Fatal("reviewed native settings lost generation authority or received duplicate Home Manager placement")
	}
	targetHome := filepath.Join(root, "target-home")
	if err := os.MkdirAll(targetHome, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", targetHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(targetHome, ".config"))
	t.Setenv("ENDSTATE_ROOT", root)
	pathRoot := filepath.Join(root, "bin")
	if err := os.Mkdir(pathRoot, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pathRoot, "rg"), nil, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", pathRoot)
	oldRoot, oldGOOS, oldLoader := resolveRepoRootFn, captureGOOSFn, loadConfigRestoreCatalogFn
	resolveRepoRootFn = func() string { return root }
	captureGOOSFn = func() string { return "linux" }
	loadConfigRestoreCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return catalog, nil, nil
	}
	t.Cleanup(func() { resolveRepoRootFn, captureGOOSFn, loadConfigRestoreCatalogFn = oldRoot, oldGOOS, oldLoader })
	return extracted, root, targetHome, primary
}

func assertConfigVerify(t *testing.T, manifestPath, captureID, status, reason string) {
	t.Helper()
	var raw interface{}
	var envErr *envelope.Error
	output := captureStderr(t, func() {
		raw, envErr = RunVerify(VerifyFlags{Manifest: manifestPath, Events: "jsonl"})
	})
	if envErr != nil {
		t.Fatalf("verify: %+v", envErr)
	}
	result := raw.(*VerifyResult)
	for _, item := range result.Results {
		if item.Type != "config" || item.ID != captureID {
			continue
		}
		if item.Status != status || item.Reason != reason || item.Driver != "" || item.Version != "" || item.Name == "" {
			t.Fatalf("config verify item = %+v; want %s/%s", item, status, reason)
		}
		counts := VerifySummary{Total: len(result.Results)}
		for _, result := range result.Results {
			switch result.Status {
			case "pass":
				counts.Pass++
			case "fail":
				counts.Fail++
			case "skipped":
				counts.Skipped++
			}
		}
		if counts != result.Summary {
			t.Fatalf("verify summary = %+v, results require %+v", result.Summary, counts)
		}
		configEvent, summaryEvents := false, 0
		for _, event := range roundTripParseJSONL(t, output) {
			if event["event"] == "summary" {
				summaryEvents++
				if event["total"] != float64(counts.Total) || event["failed"] != float64(counts.Fail) || event["success"] != float64(counts.Pass) || event["skipped"] != float64(counts.Skipped) {
					t.Fatalf("verify summary event disagrees: %+v", event)
				}
			}
			if event["event"] == "item" && event["id"] == captureID {
				wantStatus := "skipped"
				if status == "pass" {
					wantStatus = "present"
				} else if status == "fail" {
					wantStatus = "failed"
				}
				configEvent = event["status"] == wantStatus && event["reason"] == reason && event["name"] == item.Name
			}
		}
		if !configEvent || summaryEvents != 1 {
			t.Fatalf("config event or final summary missing: %s", output)
		}
		return
	}
	t.Fatalf("verify omitted captured configuration: %+v", result)
}

func configVerifyFileBytes(t *testing.T, root string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files[relative], err = os.ReadFile(path)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}
