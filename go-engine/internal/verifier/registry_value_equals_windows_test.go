// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package verifier

import (
	"os/exec"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

const vScratchKey = `HKCU\Software\Endstate\VerifyTest`

func cleanupVScratch(t *testing.T) {
	t.Helper()
	_ = exec.Command("reg", "delete", vScratchKey, "/f").Run()
}

func seedDword(t *testing.T, name string, data string) {
	t.Helper()
	if err := exec.Command("reg", "add", vScratchKey, "/v", name, "/t", "REG_DWORD", "/d", data, "/f").Run(); err != nil {
		t.Fatalf("failed to seed %s=%s: %v", name, data, err)
	}
}

func seedSz(t *testing.T, name, data string) {
	t.Helper()
	if err := exec.Command("reg", "add", vScratchKey, "/v", name, "/t", "REG_SZ", "/d", data, "/f").Run(); err != nil {
		t.Fatalf("failed to seed %s=%q: %v", name, data, err)
	}
}

func TestCheckRegistryValueEquals_DwordMatch(t *testing.T) {
	cleanupVScratch(t)
	defer cleanupVScratch(t)
	seedDword(t, "HideFileExt", "0")

	entry := manifest.VerifyEntry{
		Type:      "registry-value-equals",
		Path:      vScratchKey,
		ValueName: "HideFileExt",
		ValueType: "REG_DWORD",
		Data:      "0",
	}
	r := CheckRegistryValueEquals(entry)
	if !r.Pass {
		t.Errorf("expected pass for matching DWORD, got fail: %s", r.Message)
	}
}

// 0x-hex expected data must match a stored decimal DWORD.
func TestCheckRegistryValueEquals_DwordHexMatch(t *testing.T) {
	cleanupVScratch(t)
	defer cleanupVScratch(t)
	seedDword(t, "Hidden", "1")

	entry := manifest.VerifyEntry{
		Type:      "registry-value-equals",
		Path:      vScratchKey,
		ValueName: "Hidden",
		ValueType: "REG_DWORD",
		Data:      "0x1",
	}
	r := CheckRegistryValueEquals(entry)
	if !r.Pass {
		t.Errorf("expected pass for 0x1 vs stored 1, got fail: %s", r.Message)
	}
}

func TestCheckRegistryValueEquals_DwordMismatch(t *testing.T) {
	cleanupVScratch(t)
	defer cleanupVScratch(t)
	seedDword(t, "HideFileExt", "1") // not the expected 0

	entry := manifest.VerifyEntry{
		Type:      "registry-value-equals",
		Path:      vScratchKey,
		ValueName: "HideFileExt",
		ValueType: "REG_DWORD",
		Data:      "0",
	}
	r := CheckRegistryValueEquals(entry)
	if r.Pass {
		t.Errorf("expected fail for mismatched DWORD, got pass")
	}
}

func TestCheckRegistryValueEquals_TypeMismatch(t *testing.T) {
	cleanupVScratch(t)
	defer cleanupVScratch(t)
	seedDword(t, "Mode", "0")

	entry := manifest.VerifyEntry{
		Type:      "registry-value-equals",
		Path:      vScratchKey,
		ValueName: "Mode",
		ValueType: "REG_SZ", // wrong type
		Data:      "0",
	}
	r := CheckRegistryValueEquals(entry)
	if r.Pass {
		t.Errorf("expected fail for type mismatch, got pass")
	}
}

func TestCheckRegistryValueEquals_MissingValue(t *testing.T) {
	cleanupVScratch(t)
	defer cleanupVScratch(t)
	seedDword(t, "Present", "0")

	entry := manifest.VerifyEntry{
		Type:      "registry-value-equals",
		Path:      vScratchKey,
		ValueName: "Absent",
		ValueType: "REG_DWORD",
		Data:      "0",
	}
	r := CheckRegistryValueEquals(entry)
	if r.Pass {
		t.Errorf("expected fail for missing value, got pass")
	}
}

func TestCheckRegistryValueEquals_StringMatch(t *testing.T) {
	cleanupVScratch(t)
	defer cleanupVScratch(t)
	seedSz(t, "Greeting", "hello world")

	entry := manifest.VerifyEntry{
		Type:      "registry-value-equals",
		Path:      vScratchKey,
		ValueName: "Greeting",
		ValueType: "REG_SZ",
		Data:      "hello world",
	}
	r := CheckRegistryValueEquals(entry)
	if !r.Pass {
		t.Errorf("expected pass for matching string, got fail: %s", r.Message)
	}
}

// Dispatch through RunVerify must reach the registry-value-equals checker.
func TestRunVerify_DispatchesRegistryValueEquals(t *testing.T) {
	cleanupVScratch(t)
	defer cleanupVScratch(t)
	seedDword(t, "Dispatch", "0")

	entries := []manifest.VerifyEntry{
		{Type: "registry-value-equals", Path: vScratchKey, ValueName: "Dispatch", ValueType: "REG_DWORD", Data: "0"},
	}
	results := RunVerify(entries)
	if len(results) != 1 || !results[0].Pass {
		t.Errorf("expected one passing result via RunVerify, got %+v", results)
	}
}
