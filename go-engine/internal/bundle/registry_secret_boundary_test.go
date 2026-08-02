// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestResolveCaptureSecretPatternsKeepsRegistryCoordinatesOutOfFilesystemPaths(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.registry-secret")
	patterns, err := resolveCaptureSecretPatterns(context, "apps.registry-secret", []string{`HKCU:\Software\App\Token`, `%APPDATA%\App\*.token`}, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if len(patterns) != 1 || patterns[0] == `HKCU:\Software\App\Token` {
		t.Fatalf("filesystem secret patterns = %#v", patterns)
	}
}

func TestRegistryCaptureBoundaryRejectsNamedValueAtOrBelowSecret(t *testing.T) {
	mod := &modules.Module{ID: "apps.registry-secret", Capture: &modules.CaptureDef{RegistryValues: []modules.CaptureRegistryValue{{Key: `HKCU\Software\App\Secret\Child`, ValueName: "Token"}}}, Secrets: &modules.SecretsDef{RegistryKeys: []string{`HKCU\Software\App\Secret`}}}
	if err := validateRegistryCaptureBoundary(mod); !errors.Is(err, validationmode.ErrUnsafeRegistry) {
		t.Fatalf("boundary error = %v, want ErrUnsafeRegistry", err)
	}
	mod.Capture.RegistryValues[0].Key = `HKCU\Software\App`
	if err := validateRegistryCaptureBoundary(mod); err != nil {
		t.Fatalf("secret descendant below a named-value key was overrejected: %v", err)
	}
}

func TestCollectConfigFilesRejectsRegistryBoundaryBeforeStaging(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(source, []byte("settings"), 0o600); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(dir, "staging")
	mod := &modules.Module{ID: "apps.registry-secret", Capture: &modules.CaptureDef{
		Files:        []modules.CaptureFile{{Source: source, Dest: "apps/registry-secret/settings.json"}},
		RegistryKeys: []modules.CaptureRegistryKey{{Key: `HKCU\Software\App`, Dest: "settings.reg", Optional: true}},
	}, Secrets: &modules.SecretsDef{Files: []string{`HKCU\Software\App\Secret`}}}
	if _, _, err := CollectConfigFiles(mod, staging); !errors.Is(err, validationmode.ErrUnsafeRegistry) {
		t.Fatalf("CollectConfigFiles() error = %v, want ErrUnsafeRegistry", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "configs", "registry-secret", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("filesystem capture wrote before registry boundary failed: %v", err)
	}
}

func TestCapturePathMatchesSecretsIgnoresRegistryDeclarations(t *testing.T) {
	patterns := []string{`HKCU\Software\App\Secret`, `%APPDATA%\App\*.token`}
	if CapturePathMatchesSecrets(`%APPDATA%\App\settings.json`, patterns) {
		t.Fatal("registry declaration affected ordinary filesystem matching")
	}
	if !CapturePathMatchesSecrets(`%APPDATA%\App\session.token`, patterns) {
		t.Fatal("filesystem secret no longer matches")
	}
}

func TestCreateBundleDoesNotDowngradeRegistryBoundaryFailure(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.jsonc")
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"name":"registry-boundary","apps":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(source, []byte("settings"), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := &modules.Module{ID: "apps.registry-secret", Capture: &modules.CaptureDef{
		Files:        []modules.CaptureFile{{Source: source, Dest: "apps/registry-secret/settings.json"}},
		RegistryKeys: []modules.CaptureRegistryKey{{Key: `HKCU\Software\App`, Dest: "settings.reg", Optional: true}},
	}, Secrets: &modules.SecretsDef{RegistryKeys: []string{`HKCU\Software\App\Secret`}}}
	output := filepath.Join(dir, "capture.zip")
	if err := CreateBundle(manifestPath, []*modules.Module{mod}, output, "test"); !errors.Is(err, validationmode.ErrUnsafeRegistry) {
		t.Fatalf("CreateBundle() error = %v, want ErrUnsafeRegistry", err)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("CreateBundle published partial output after boundary failure: %v", err)
	}
}

func TestCreateBundleWithReportPreflightsEveryModuleBeforeIO(t *testing.T) {
	for _, test := range []struct {
		name string
		mods []*modules.Module
	}{
		{name: "safe then unsafe", mods: []*modules.Module{safeRegistryBoundaryModule("apps.safe"), unsafeRegistryBoundaryModule("apps.unsafe")}},
		{name: "unsafe then safe", mods: []*modules.Module{unsafeRegistryBoundaryModule("apps.unsafe"), safeRegistryBoundaryModule("apps.safe")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "capture.zip")
			_, err := CreateBundleWithReport(filepath.Join(dir, "missing-manifest.jsonc"), test.mods, output, "test", func(Stage) {
				t.Fatal("bundle stage ran before registry boundary preflight")
			})
			if !errors.Is(err, validationmode.ErrUnsafeRegistry) {
				t.Fatalf("CreateBundleWithReport() error = %v, want boundary failure", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("bundle output exists after boundary failure: %v", err)
			}
		})
	}
}

func TestCreateCaptureBundlePreflightsEveryModuleBeforeManifestRead(t *testing.T) {
	for _, test := range []struct {
		name string
		mods []*modules.Module
	}{
		{name: "safe then unsafe", mods: []*modules.Module{safeRegistryBoundaryModule("apps.safe"), unsafeRegistryBoundaryModule("apps.unsafe")}},
		{name: "unsafe then safe", mods: []*modules.Module{unsafeRegistryBoundaryModule("apps.unsafe"), safeRegistryBoundaryModule("apps.safe")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			output := filepath.Join(dir, "capture.zip")
			_, err := CreateCaptureBundle(CaptureBundleRequest{
				ManifestPath: filepath.Join(dir, "missing-manifest.jsonc"), OutputPath: output, Modules: test.mods,
				OnStage: func(Stage) { t.Fatal("capture stage ran before registry boundary preflight") },
			})
			if !errors.Is(err, validationmode.ErrUnsafeRegistry) {
				t.Fatalf("CreateCaptureBundle() error = %v, want boundary failure", err)
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("capture output exists after boundary failure: %v", err)
			}
		})
	}
}

func TestRegistryCaptureBoundaryPreflightsSelectedCatalogSecretModules(t *testing.T) {
	root := filepath.Join("..", "..", "..", "modules", "apps")
	selected := make([]*modules.Module, 0, 4)
	for _, name := range []string{"ccleaner", "displayfusion", "revo-uninstaller", "tableplus"} {
		data, err := os.ReadFile(filepath.Join(root, name, "module.jsonc"))
		if err != nil {
			t.Fatal(err)
		}
		mod, err := modules.ParseModuleJSON(data)
		if err != nil {
			t.Fatal(err)
		}
		selected = append(selected, mod)
	}
	if err := preflightRegistryCaptureBoundaries(selected, nil); !errors.Is(err, validationmode.ErrUnsafeRegistry) {
		t.Fatalf("selected registry secret boundary error = %v", err)
	}
}

func safeRegistryBoundaryModule(id string) *modules.Module {
	return &modules.Module{ID: id, Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: `HKCU\Software\Safe`, Dest: "safe.reg", Optional: true}}}}
}

func unsafeRegistryBoundaryModule(id string) *modules.Module {
	return &modules.Module{ID: id, Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: `HKCU\Software\Unsafe`, Dest: "unsafe.reg", Optional: true}}}, Secrets: &modules.SecretsDef{RegistryKeys: []string{`HKCU\Software\Unsafe\Secret`}}}
}

func TestResolveCaptureSecretPatternsRejectsInvalidRegistryCoordinatesAtSecretCoordinate(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.registry-secret")
	_, err := resolveCaptureSecretPatterns(context, "apps.registry-secret", []string{`HKCU\Software\App\*`}, validationmode.HostPathPolicy{})
	var isolation *CaptureIsolationError
	if !errors.As(err, &isolation) || isolation.Coordinate != "secrets.files[0]" || !errors.Is(err, validationmode.ErrUnsafeRegistry) {
		t.Fatalf("error = %T %v", err, err)
	}
}

func TestRegistryCaptureRejectsDeclaredSecretOverlapBeforeExport(t *testing.T) {
	mod := &modules.Module{ID: "apps.registry-secret", Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: `HKCU\Software\App`, Dest: "settings.reg", Optional: true}}}, Secrets: &modules.SecretsDef{RegistryKeys: []string{`HKCU\Software\App\Token`}}}
	if err := validateRegistryCaptureBoundary(mod); err == nil {
		t.Fatal("registry secret descendant was accepted")
	}
}

func TestRegistryCaptureAllowsSiblingSecret(t *testing.T) {
	mod := &modules.Module{ID: "apps.registry-secret", Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: `HKCU\Software\App`, Dest: "settings.reg", Optional: true}}}, Secrets: &modules.SecretsDef{Files: []string{`HKCU\Software\Other\Token`}}}
	if err := validateRegistryCaptureBoundary(mod); err != nil {
		t.Fatalf("sibling secret rejected: %v", err)
	}
}
