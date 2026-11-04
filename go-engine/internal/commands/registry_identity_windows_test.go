// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

// TestCaptureVerifyAndRestoreKeepSiblingRegistryKeysApart catches capture,
// same-run verify and restore acting on the wrong one of two sibling keys, İ
// (U+0130) and i. Go's lower-casing merges the two names; the registry keeps
// them apart. Capture would then give one key the other's data, verify would
// check a value that this run did not restore, and restore would compare with
// the sibling's value and skip a write that is needed.
func TestCaptureVerifyAndRestoreKeepSiblingRegistryKeysApart(t *testing.T) {
	root := fmt.Sprintf(`Software\Endstate\IdentityTest-%d`, time.Now().UnixNano())
	authored, sibling := `HKCU\`+root+"\\İ", `HKCU\`+root+`\i`
	t.Cleanup(func() {
		for _, subkey := range []string{root + "\\İ", root + `\i`, root} {
			if err := registry.DeleteKey(registry.CURRENT_USER, subkey); err != nil && !errors.Is(err, registry.ErrNotExist) {
				t.Errorf("delete scratch key %s: %v", subkey, err)
			}
		}
	})
	for _, value := range []struct{ subkey, name, data string }{
		{root + "\\İ", "Setting", "authored data"},
		{root + "\\İ", "Extra", "authored extra"},
		{root + `\i`, "Setting", "sibling data"},
	} {
		key, _, err := registry.CreateKey(registry.CURRENT_USER, value.subkey, registry.SET_VALUE)
		if err != nil {
			t.Fatal(err)
		}
		err = key.SetStringValue(value.name, value.data)
		key.Close()
		if err != nil {
			t.Fatal(err)
		}
	}

	// Capture: the module captures Setting on both keys and Extra only on İ,
	// so the restore of Extra on i keeps the module's own data.
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.jsonc")
	if err := os.WriteFile(inputPath, []byte(`{"version":1,"name":"capture","apps":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	mod := &modules.Module{
		ID: "apps.example", DisplayName: "Example",
		Capture: &modules.CaptureDef{RegistryValues: []modules.CaptureRegistryValue{
			{Key: authored, ValueName: "Setting"}, {Key: sibling, ValueName: "Setting"}, {Key: authored, ValueName: "Extra"},
		}},
		Restore: []modules.RestoreDef{
			{Type: "registry-set", Key: authored, ValueName: "Setting"},
			{Type: "registry-set", Key: sibling, ValueName: "Setting"},
			{Type: "registry-set", Key: sibling, ValueName: "Extra", ValueType: "REG_SZ", Data: "module data"},
		},
	}
	zipPath := filepath.Join(dir, "capture.zip")
	if _, err := bundle.CreateCaptureBundle(bundle.CaptureBundleRequest{
		ManifestPath: inputPath, OutputPath: zipPath, EndstateVersion: "test", Modules: []*modules.Module{mod},
	}); err != nil {
		t.Fatal(err)
	}
	extracted, err := bundle.ExtractBundle(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Dir(extracted)) })
	captured, err := manifest.LoadManifest(extracted)
	if err != nil {
		t.Fatal(err)
	}
	want := map[[2]string]string{
		{authored, "Setting"}: "authored data",
		{sibling, "Setting"}:  "sibling data",
		{sibling, "Extra"}:    "module data",
	}
	if len(captured.Restore) != len(want) {
		t.Fatalf("captured restores = %+v, want %d", captured.Restore, len(want))
	}
	for _, entry := range captured.Restore {
		if wantData := want[[2]string{entry.Key, entry.ValueName}]; entry.Data != wantData {
			t.Errorf("capture gave %s %s data %q, want %q", entry.Key, entry.ValueName, entry.Data, wantData)
		}
	}

	// Restore preview: İ holds "authored data", so setting it to the sibling's
	// value is a change, not an up-to-date value.
	results, err := restore.RunRestore([]restore.RestoreAction{{
		Type: "registry-set", Key: authored, ValueName: "Setting", ValueType: "REG_SZ", Data: "sibling data",
	}}, restore.RestoreOptions{DryRun: true, BackupDir: t.TempDir()}, nil)
	if err != nil || len(results) != 1 || results[0].Status != "restored" {
		t.Fatalf("restore preview = %+v, err = %v; want status restored", results, err)
	}

	// Verify: this run restored Setting on İ only, so Setting on i is skipped.
	evaluate, skipped := scopeRestoredVerifies([]manifest.VerifyEntry{
		{Type: "registry-value-equals", Path: authored, ValueName: "Setting", FromModule: "apps.example"},
		{Type: "registry-value-equals", Path: sibling, ValueName: "Setting", FromModule: "apps.example"},
	}, restoredRegistryValues(results))
	if len(evaluate) != 1 || evaluate[0].Path != authored || len(skipped) != 1 {
		t.Fatalf("evaluate = %+v, skipped = %+v; want only Setting on İ evaluated", evaluate, skipped)
	}
}
