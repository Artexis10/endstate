// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestCaptureArtifactPathUsesCanonicalBundleExtension(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"captured", "optional-absent"} {
		want := filepath.Join(root, "manifests", name+manifest.BundleExt)
		if got := captureArtifactPath(root, name); got != want {
			t.Fatalf("captureArtifactPath(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestRegistryVerifierFixtureSetupFailureIsStructuredAndCleansRuntime(t *testing.T) {
	cause := errors.New("registry access denied")
	setup := &registryVerifierFixtureSetupError{verifierIndex: 2, verifierKind: "registry-key-exists", cause: cause}
	wrapped := fmt.Errorf("prepare guards: %w", setup)

	classified, ok := registryVerifierFixtureSetup(wrapped)
	if !ok || classified != setup || !errors.Is(classified, cause) {
		t.Fatalf("classified setup = %+v, %v", classified, ok)
	}
	if _, ok := registryVerifierFixtureSetup(cause); ok {
		t.Fatal("raw registry error was classified as a fixture setup failure")
	}
	failure := registryVerifierFixtureSetupFailure(wrapped)
	if failure == nil || failure.Code != CodeIsolationFailure || failure.Phase != "fixture" || failure.Coordinate != "verify[2]" || failure.Detail != "registry verifier fixture could not be materialized" {
		t.Fatalf("failure = %+v", failure)
	}
	cleaned := false
	failure, setupErr := abandonGuardPreparation(func() error {
		cleaned = true
		return nil
	}, wrapped)
	if !cleaned || setupErr != nil || failure == nil || failure.Code != CodeIsolationFailure {
		t.Fatalf("cleanup result = failure:%+v setupErr:%v cleaned:%v", failure, setupErr, cleaned)
	}

	rawCleaned := false
	failure, setupErr = abandonGuardPreparation(func() error {
		rawCleaned = true
		return nil
	}, cause)
	if !rawCleaned || failure != nil || !errors.Is(setupErr, cause) {
		t.Fatalf("raw setup result = failure:%+v setupErr:%v cleaned:%v", failure, setupErr, rawCleaned)
	}
}

func TestPrepareGuardsAndToolsMaterializesDirectoryFileExistsVerifiers(t *testing.T) {
	t.Run("Stream Deck ancestor directory", func(t *testing.T) {
		runtime, appData := streamDeckGuardRuntime(t)
		verifier := `%APPDATA%\Elgato\StreamDeck`
		runtime.Module.Verify = []modules.VerifyDef{{Type: "file-exists", Path: verifier}}
		if err := runtime.prepareGuardsAndTools(); err != nil {
			t.Fatal(err)
		}
		assertDirectoryVerifier(t, runtime, verifier, filepath.Join(appData, "Elgato", "StreamDeck"))
	})

	t.Run("exact directory target", func(t *testing.T) {
		runtime, appData := streamDeckGuardRuntime(t)
		verifier := `%APPDATA%\Elgato\StreamDeck\ProfilesV3`
		runtime.Module.Verify = []modules.VerifyDef{{Type: "file-exists", Path: verifier}}
		if err := runtime.prepareGuardsAndTools(); err != nil {
			t.Fatal(err)
		}
		assertDirectoryVerifier(t, runtime, verifier, filepath.Join(appData, "Elgato", "StreamDeck", "ProfilesV3"))
	})

	t.Run("unrelated file verifier", func(t *testing.T) {
		runtime, appData := streamDeckGuardRuntime(t)
		verifier := `%APPDATA%\Elgato\StreamDeck\verification.txt`
		runtime.Module.Verify = []modules.VerifyDef{{Type: "file-exists", Path: verifier}}
		if err := runtime.prepareGuardsAndTools(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(appData, "Elgato", "StreamDeck", "verification.txt")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("file verifier = %v, %v; want regular file", info, err)
		}
	})

	t.Run("ancestor of file fixture is directory", func(t *testing.T) {
		runtime, appData := streamDeckGuardRuntime(t)
		runtime.Plan.Targets = []FixtureTarget{{
			Coordinate: "capture.files[0]", Authored: `%APPDATA%\Elgato\StreamDeck\settings.json`,
			Resolved:    filepath.Join(appData, "Elgato", "StreamDeck", "settings.json"),
			PayloadPath: filepath.Join(appData, "Elgato", "StreamDeck", "settings.json"), Captured: "captured",
		}}
		verifier := `%APPDATA%\Elgato\StreamDeck`
		runtime.Module.Verify = []modules.VerifyDef{{Type: "file-exists", Path: verifier}}
		if err := runtime.prepareGuardsAndTools(); err != nil {
			t.Fatal(err)
		}
		assertDirectoryVerifier(t, runtime, verifier, filepath.Join(appData, "Elgato", "StreamDeck"))
		if failure := runtime.Plan.MaterializeCaptured(); failure != nil {
			t.Fatal(failure)
		}
		path := filepath.Join(appData, "Elgato", "StreamDeck", "settings.json")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("materialized file fixture = %v, %v; want regular file", info, err)
		}
	})

	t.Run("exact file target remains file verifier", func(t *testing.T) {
		runtime, appData := streamDeckGuardRuntime(t)
		path := filepath.Join(appData, "Elgato", "StreamDeck", "settings.json")
		runtime.Plan.Targets = []FixtureTarget{{
			Coordinate: "capture.files[0]", Authored: `%APPDATA%\Elgato\StreamDeck\settings.json`,
			Resolved: path,
		}}
		verifier := `%APPDATA%\Elgato\StreamDeck\settings.json`
		runtime.Module.Verify = []modules.VerifyDef{{Type: "file-exists", Path: verifier}}
		if err := runtime.prepareGuardsAndTools(); err != nil {
			t.Fatal(err)
		}
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("exact file verifier = %v, %v; want regular file", info, err)
		}
	})
}

func TestPrepareGuardsAndToolsDefersInstallFileVerifierMaterialization(t *testing.T) {
	_, module, scenario := productionInstallAuthority(t, "apps.brave")
	plan, failure := compileInstallContract(module, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	runtime := fixtureScenarioRuntime(t)
	runtime.Module = module
	runtime.InstallPlan = plan
	runtime.GuardRoot = t.TempDir()
	path, err := runtime.validationContext().ResolveHostPath(module.Verify[0].Path, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("file verifier existed before prepare: %v", err)
	}
	if err := runtime.prepareGuardsAndTools(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("install file verifier was eagerly materialized: %v", err)
	}
}

func TestPrepareGuardsAndToolsNormalizesTildeHomeTargets(t *testing.T) {
	for _, authored := range []string{`~/.gitconfig`, `~\.gitconfig`} {
		t.Run(authored, func(t *testing.T) {
			runtime := fixtureScenarioRuntime(t)
			runtime.Module = &modules.Module{ID: "apps.git"}
			runtime.Plan.Targets = []FixtureTarget{{
				Coordinate: "capture.files[0]", Authored: authored,
			}}
			runtime.GuardRoot = t.TempDir()

			if err := runtime.prepareGuardsAndTools(); err != nil {
				t.Fatal(err)
			}
			if got, want := runtime.OriginalEnvironment["USERPROFILE"], filepath.Join(runtime.GuardRoot, "userprofile"); got != want {
				t.Fatalf("USERPROFILE guard root = %q, want %q", got, want)
			}
			if len(runtime.Guards) != 1 || runtime.Guards[0].Path != filepath.Join(runtime.GuardRoot, "userprofile", ".gitconfig") {
				t.Fatalf("guards = %+v, want contained .gitconfig witness", runtime.Guards)
			}
			if failure := runtime.assertGuards(); failure != nil {
				t.Fatalf("original user-profile guard changed: %+v", failure)
			}
		})
	}
}

func TestRunFreshBuiltEngineTrackedGitDefaultV1AvoidsRawHarnessIO(t *testing.T) {
	engineRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	buildRoot := t.TempDir()
	engine := filepath.Join(buildRoot, "endstate.exe")
	build := exec.Command("go", "build", "-buildvcs=false", "-o", engine, "./cmd/endstate")
	build.Dir = engineRoot
	build.Env = append(os.Environ(), "GOCACHE="+filepath.Join(buildRoot, "gocache"), "GOTELEMETRY=off")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build engine: %v\n%s", err, output)
	}

	result, err := Run(context.Background(), Request{
		EnginePath: engine, RepoRoot: filepath.Dir(engineRoot), ModuleID: "apps.git",
		ScenarioID: "default-v1", ResultPath: filepath.Join(t.TempDir(), "git-default-v1.json"),
	})
	if err != nil {
		t.Fatalf("Run returned raw harness I/O error: %v", err)
	}
	if result.ModuleID != "apps.git" || result.ScenarioID != "default-v1" {
		t.Fatalf("result identity = %q/%q, want apps.git/default-v1", result.ModuleID, result.ScenarioID)
	}
}

func TestDolphinDirectoryFixtureUsesNestedFileVerifierAsPayloadWitness(t *testing.T) {
	mod, err := modules.ParseModuleJSON([]byte(`{
  "id": "apps.dolphin-emulator", "displayName": "Dolphin", "sensitivity": "low",
  "matches": {"winget": ["DolphinEmulator.Dolphin"]},
  "verify": [{"type": "file-exists", "path": "%APPDATA%\\Dolphin Emulator\\Config\\Dolphin.ini"}],
  "restore": [{"type": "copy", "source": "./payload/apps/dolphin-emulator/appdata-Config", "target": "%APPDATA%\\Dolphin Emulator\\Config", "backup": true, "optional": true}],
  "capture": {"files": [{"source": "%APPDATA%\\Dolphin Emulator\\Config", "dest": "apps/dolphin-emulator/appdata-Config", "optional": true}], "excludeGlobs": []}
}`))
	if err != nil {
		t.Fatal(err)
	}
	scenario := fixtureScenario()
	scenario.Fixture.Type = validationmatrix.FixtureDeclarative
	definitions, failure := compileFixtureDefinitions(mod, scenario)
	if failure != nil {
		t.Fatal(failure)
	}
	definitions.Entries[0].Kind = fixtureKindDirectory
	plan, failure := compileFixturePlan(fixtureValidationContext(t, mod.ID, scenario.ID), mod, scenario, definitions)
	if failure != nil {
		t.Fatal(failure)
	}
	target := plan.Targets[0]
	wantPayload := filepath.Join(target.Resolved, "Dolphin.ini")
	if target.PayloadPath != wantPayload {
		t.Fatalf("payload path = %q, want nested verifier %q", target.PayloadPath, wantPayload)
	}

	authorityRoot, err := os.MkdirTemp(filepath.Dir(plan.context.Root()), "endstate-validation-authority-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(authorityRoot) })
	childWorkingDir := filepath.Join(authorityRoot, "child")
	if err := os.Mkdir(childWorkingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	runtime := &scenarioRuntime{
		Module: mod, Scenario: scenario, Plan: plan, Root: plan.context.Root(),
		AuthorityRoot: authorityRoot, ChildWorkingDir: childWorkingDir, GuardRoot: filepath.Join(authorityRoot, "guards"),
	}
	if err := runtime.prepareGuardsAndTools(); err != nil {
		t.Fatal(err)
	}
	if failure := runtime.Plan.MaterializeCaptured(); failure != nil {
		t.Fatal(failure)
	}
	verifierPath, err := runtime.validationContext().ResolveHostPath(mod.Verify[0].Path, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(verifierPath)
	if err != nil || !info.Mode().IsRegular() {
		t.Fatalf("nested verifier after materialization = %v, %v; want regular file", info, err)
	}
	if failure := runtime.Plan.CompareCaptured(); failure != nil {
		t.Fatal(failure)
	}
	payloadName, ok := targetArtifactPayloadName(mod.ID, target)
	if !ok {
		t.Fatal("nested verifier payload has no artifact name")
	}
	entries := map[string][]byte{strings.ToLower(payloadName): []byte(target.Captured)}
	if failure := validateArtifactConfigPayloadSet(runtime, entries); failure != nil {
		t.Fatalf("artifact payload membership = %+v", failure)
	}
	entries["configs/apps/dolphin-emulator/appdata-Config/unexpected.ini"] = []byte("unexpected")
	if failure := validateArtifactConfigPayloadSet(runtime, entries); failure == nil {
		t.Fatal("artifact accepted an unexpected directory member")
	}

	if failure := runtime.Plan.Mutate(); failure != nil {
		t.Fatal(failure)
	}
	backup := filepath.Join(runtime.Root, "state", "backups", "dolphin")
	if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(backup, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(filepath.Join(backup, "Dolphin.ini")), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backup, "Dolphin.ini"), []byte(target.Mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	if failure := validateMutatedBackup(runtime, target, backup); failure != nil {
		t.Fatalf("rebuild backup = %+v", failure)
	}
}

func streamDeckGuardRuntime(t *testing.T) (*scenarioRuntime, string) {
	t.Helper()
	runtime := fixtureScenarioRuntime(t)
	appData, ok := runtime.validationContext().VirtualRoot("APPDATA")
	if !ok {
		t.Fatal("APPDATA validation root is absent")
	}
	runtime.Module = &modules.Module{ID: "apps.stream-deck"}
	runtime.Plan.Targets = []FixtureTarget{
		{Coordinate: "capture.files[0]", Authored: `%APPDATA%\Elgato\StreamDeck\ProfilesV3`, Resolved: filepath.Join(appData, "Elgato", "StreamDeck", "ProfilesV3"), Directory: true},
		{Coordinate: "capture.files[1]", Authored: `%APPDATA%\Elgato\StreamDeck\BackupV3`, Resolved: filepath.Join(appData, "Elgato", "StreamDeck", "BackupV3"), Directory: true},
		{Coordinate: "capture.files[2]", Authored: `%APPDATA%\Elgato\StreamDeck\Backup`, Resolved: filepath.Join(appData, "Elgato", "StreamDeck", "Backup"), Directory: true},
	}
	runtime.GuardRoot = t.TempDir()
	return runtime, appData
}

func assertDirectoryVerifier(t *testing.T, runtime *scenarioRuntime, verifier, wantPath string) {
	t.Helper()
	path, err := runtime.validationContext().ResolveHostPath(verifier, validationmode.HostPathPolicy{})
	if err != nil || path != wantPath {
		t.Fatalf("resolve verifier = %q, %v; want %q, nil", path, err, wantPath)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		t.Fatalf("directory verifier = %v, %v; want directory", info, err)
	}
}
