//go:build windows

// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestValidationModeSessionRegistryRegistrationClosesAtSeal(t *testing.T) {
	changeValidationTestWorkingDirectory(t, t.TempDir())
	context := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", InitialState: "absent",
	})
	session, err := ActivateValidationMode(context)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Restore() })
	originalKey := `HKCU\Software\EndstateValidationSessionTests\Original`
	if err := session.registerOriginalRegistryProtection(
		"capture.registryKeys[0].key", "settings-key",
		validationmode.ProtectedRegistry{Key: originalKey, WholeKey: true},
	); err != nil {
		t.Fatalf("register before seal: %v", err)
	}
	session.sealIsolation()
	lateKey := `HKCU\Software\EndstateValidationSessionTests\Late`
	if err := session.registerOriginalRegistryProtection(
		"restore[0].key", "late-key",
		validationmode.ProtectedRegistry{Key: lateKey, WholeKey: true},
	); !errors.Is(err, validationmode.ErrUnsafeRegistry) {
		t.Fatalf("late registration error = %v, want unsafe registry", err)
	}
	isolationErr := session.IsolationError()
	if !errors.Is(isolationErr, validationmode.ErrUnsafeRegistry) || !strings.Contains(isolationErr.Error(), "coordinate=restore[0].key") {
		t.Fatalf("late registration finding = %v", isolationErr)
	}
	for _, forbidden := range []string{originalKey, lateKey, "registry-value-secret", context.Root()} {
		if strings.Contains(strings.ToLower(isolationErr.Error()), strings.ToLower(forbidden)) {
			t.Fatalf("registration error leaked registry sentinel %q: %v", forbidden, isolationErr)
		}
	}
}
