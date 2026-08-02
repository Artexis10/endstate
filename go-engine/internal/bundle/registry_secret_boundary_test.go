// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"errors"
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
	if err := validateRegistryCaptureSecrets(mod, mod.Capture.RegistryKeys[0].Key); err == nil {
		t.Fatal("registry secret descendant was accepted")
	}
}

func TestRegistryCaptureAllowsSiblingSecret(t *testing.T) {
	mod := &modules.Module{ID: "apps.registry-secret", Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: `HKCU\Software\App`, Dest: "settings.reg", Optional: true}}}, Secrets: &modules.SecretsDef{Files: []string{`HKCU\Software\Other\Token`}}}
	if err := validateRegistryCaptureSecrets(mod, mod.Capture.RegistryKeys[0].Key); err != nil {
		t.Fatalf("sibling secret rejected: %v", err)
	}
}
