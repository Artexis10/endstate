// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package verifier

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func verifierValidationContext(t *testing.T) *validationmode.Context {
	t.Helper()
	root, err := os.MkdirTemp(os.TempDir(), "endstate-validation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	nonce := strings.TrimPrefix(filepath.Base(root), "endstate-validation-")
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1,
		ScenarioID:    "verifier-validation",
		Nonce:         nonce,
		ModuleID:      "apps.verifier",
		Inventory: validationmode.Inventory{
			AppID: "verifier", Driver: "winget", Ref: "Vendor.Verifier",
			DisplayName: "Verifier", InitialState: "present",
		},
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".endstate"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".endstate", "validation-mode.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	context, err := validationmode.LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	return context
}

func TestValidationFileVerifierUsesMappedStateAndSemanticResult(t *testing.T) {
	context := verifierValidationContext(t)
	appData, ok := context.VirtualRoot("APPDATA")
	if !ok {
		t.Fatal("APPDATA virtual root missing")
	}
	if err := os.MkdirAll(filepath.Join(appData, "Vendor"), 0o700); err != nil {
		t.Fatal(err)
	}
	mapped, err := context.ResolveHostPath(`%APPDATA%\Vendor\settings.json`, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(mapped), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mapped, []byte("sandbox-sentinel"), 0o600); err != nil {
		t.Fatal(err)
	}

	results, runErr := RunVerifyWithValidation([]manifest.VerifyEntry{{
		Type: "file-exists", Path: `%APPDATA%\Vendor\settings.json`,
	}}, context)
	if runErr != nil || len(results) != 1 || !results[0].Pass {
		t.Fatalf("RunVerifyWithValidation() = (%+v, %v)", results, runErr)
	}
	if results[0].Path != `%APPDATA%\Vendor\settings.json` || !strings.Contains(results[0].Message, `%APPDATA%\Vendor\settings.json`) {
		t.Fatalf("result lost semantic identity: %+v", results[0])
	}
	serialized, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{context.Root(), context.Descriptor().Nonce, "sandbox-sentinel"} {
		if strings.Contains(strings.ToLower(string(serialized)), strings.ToLower(forbidden)) {
			t.Fatalf("result leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestValidationFileVerifierRejectsOutsideBeforeNativeStat(t *testing.T) {
	context := verifierValidationContext(t)
	originalStat := fileStatNative
	fileStatNative = func(string) (os.FileInfo, error) {
		panic("native stat reached")
	}
	t.Cleanup(func() { fileStatNative = originalStat })

	results, runErr := RunVerifyWithValidation([]manifest.VerifyEntry{{
		Type: "file-exists", Path: filepath.Join(t.TempDir(), "outside.txt"),
	}}, context)
	if !errors.Is(runErr, validationmode.ErrUnsafePath) {
		t.Fatalf("error = %v, want ErrUnsafePath", runErr)
	}
	if len(results) != 1 || results[0].Pass || results[0].Path == "" {
		t.Fatalf("safe failure result = %+v", results)
	}
	serialized, err := json.Marshal(results)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(serialized)), strings.ToLower(context.Root())) ||
		strings.Contains(strings.ToLower(string(serialized)), strings.ToLower(context.Descriptor().Nonce)) {
		t.Fatalf("failure result leaked validation authority: %s", serialized)
	}
}

func TestValidationFileVerifierOrdinaryMissingIsAssertionNotIsolation(t *testing.T) {
	context := verifierValidationContext(t)
	appData, ok := context.VirtualRoot("APPDATA")
	if !ok {
		t.Fatal("APPDATA virtual root missing")
	}
	if err := os.MkdirAll(filepath.Join(appData, "Vendor"), 0o700); err != nil {
		t.Fatal(err)
	}
	results, runErr := RunVerifyWithValidation([]manifest.VerifyEntry{{
		Type: "file-exists", Path: `%APPDATA%\Vendor\missing.json`,
	}}, context)
	if runErr != nil {
		t.Fatalf("ordinary assertion returned isolation error: %v", runErr)
	}
	if len(results) != 1 || results[0].Pass || !strings.Contains(strings.ToLower(results[0].Message), "not found") {
		t.Fatalf("ordinary missing result = %+v", results)
	}
}
