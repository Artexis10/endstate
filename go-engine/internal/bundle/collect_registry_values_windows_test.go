// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package bundle

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

const bScratchKey = `HKCU\Software\Endstate\BundleTest`

func cleanupBScratch(t *testing.T) {
	t.Helper()
	_ = exec.Command("reg", "delete", bScratchKey, "/f").Run()
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
