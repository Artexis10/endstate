// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package bundle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

const bScratchKey = `HKCU\Software\Endstate\BundleTest`

func cleanupBScratch(t *testing.T) {
	t.Helper()
	_ = exec.Command("reg", "delete", bScratchKey, "/f").Run()
}

// TestCollectRegistryValues_KeepsStringDataVerbatim catches capture changing a
// string's spaces, tabs or variables, so restore would write different data
// than the old PC held.
func TestCollectRegistryValues_KeepsStringDataVerbatim(t *testing.T) {
	cleanupBScratch(t)
	defer cleanupBScratch(t)
	key, _, err := registry.CreateKey(registry.CURRENT_USER, strings.TrimPrefix(bScratchKey, `HKCU\`), registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"Plain": "  alpha\t beta  ", "Expandable": "  %USERPROFILE%\t x  "}
	if err := key.SetStringValue("Plain", want["Plain"]); err != nil {
		t.Fatal(err)
	}
	if err := key.SetExpandStringValue("Expandable", want["Expandable"]); err != nil {
		t.Fatal(err)
	}
	_ = key.Close()

	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{RegistryValues: []modules.CaptureRegistryValue{
		{Key: bScratchKey, ValueName: "Plain"}, {Key: bScratchKey, ValueName: "Expandable"},
	}}}
	staging := t.TempDir()
	if _, err := CollectRegistryValues(mod, staging); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(staging, "configs", "example", "registry-values.json"))
	if err != nil {
		t.Fatal(err)
	}
	var captured []CapturedRegistryValue
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatal(err)
	}
	wantType := map[string]string{"Plain": "REG_SZ", "Expandable": "REG_EXPAND_SZ"}
	if len(captured) != 2 {
		t.Fatalf("captured = %+v", captured)
	}
	for _, value := range captured {
		if value.ValueType != wantType[value.ValueName] || value.Data != want[value.ValueName] {
			t.Errorf("%s captured as %s %q, want %s %q", value.ValueName, value.ValueType, value.Data, wantType[value.ValueName], want[value.ValueName])
		}
	}
}

// CollectRegistryValues reads only the declared named values (value-level) and
// writes them as a JSON snapshot under configs/<module>/registry-values.json.
func TestCollectRegistryValues_SnapshotsNamedValues(t *testing.T) {
	cleanupBScratch(t)
	defer cleanupBScratch(t)

	if err := exec.Command("reg", "add", bScratchKey, "/v", "AppsUseLightTheme", "/t", "REG_DWORD", "/d", "0", "/f").Run(); err != nil {
		t.Fatalf("seed value: %v", err)
	}

	mod := &modules.Module{
		ID:          "windows-settings.personalization",
		DisplayName: "Dark Mode",
		Capture: &modules.CaptureDef{
			RegistryValues: []modules.CaptureRegistryValue{
				{Key: bScratchKey, ValueName: "AppsUseLightTheme"},
				{Key: bScratchKey, ValueName: "DoesNotExist", Optional: true},
			},
		},
	}

	staging := t.TempDir()
	collected, err := CollectRegistryValues(mod, staging)
	if err != nil {
		t.Fatalf("CollectRegistryValues: %v", err)
	}
	if len(collected) != 1 {
		t.Fatalf("expected 1 collected path, got %v", collected)
	}

	snapPath := filepath.Join(staging, "configs", "windows-settings.personalization", "registry-values.json")
	data, err := os.ReadFile(snapPath)
	if err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	var captured []CapturedRegistryValue
	if err := json.Unmarshal(data, &captured); err != nil {
		t.Fatalf("parse snapshot: %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("expected 2 captured values (one present, one optional-absent), got %d", len(captured))
	}

	var present, absent *CapturedRegistryValue
	for i := range captured {
		switch captured[i].ValueName {
		case "AppsUseLightTheme":
			present = &captured[i]
		case "DoesNotExist":
			absent = &captured[i]
		}
	}
	if present == nil || !present.Existed {
		t.Fatalf("expected AppsUseLightTheme captured as existing")
	}
	if present.ValueType != "REG_DWORD" || present.Data != "0" {
		t.Errorf("expected REG_DWORD 0 (decimal-normalized), got type=%q data=%q", present.ValueType, present.Data)
	}
	if absent == nil || absent.Existed {
		t.Errorf("expected optional missing value captured with Existed=false")
	}
}

// A non-optional missing value is a hard capture error.
func TestCollectRegistryValues_RequiredMissingErrors(t *testing.T) {
	cleanupBScratch(t)
	defer cleanupBScratch(t)

	mod := &modules.Module{
		ID: "windows-settings.taskbar",
		Capture: &modules.CaptureDef{
			RegistryValues: []modules.CaptureRegistryValue{
				{Key: bScratchKey, ValueName: "NotThere"}, // not optional
			},
		},
	}
	if _, err := CollectRegistryValues(mod, t.TempDir()); err == nil {
		t.Errorf("expected error for required missing value, got nil")
	}
}
