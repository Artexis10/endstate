// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
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
