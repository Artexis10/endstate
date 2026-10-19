// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
	"golang.org/x/sys/windows/registry"
)

func TestValidationConfigRestoreRegistryMapsHKCUOnlyAtNativeCall(t *testing.T) {
	validation := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", InitialState: "present",
	})
	oldRead, oldSet, oldDelete := configRestoreRegistryReadNative, configRestoreRegistrySetNative, configRestoreRegistryDeleteNative
	t.Cleanup(func() {
		configRestoreRegistryReadNative, configRestoreRegistrySetNative, configRestoreRegistryDeleteNative = oldRead, oldSet, oldDelete
	})
	called := []string{}
	configRestoreRegistryReadNative = func(_ context.Context, key, valueName string) (configrestore.RegistryReadResult, error) {
		called = append(called, "read\x00"+key+"\x00"+valueName)
		return configrestore.RegistryReadResult{}, nil
	}
	configRestoreRegistrySetNative = func(_ context.Context, key, valueName string, _ uint32, _ []byte) error {
		called = append(called, "set\x00"+key+"\x00"+valueName)
		return nil
	}
	configRestoreRegistryDeleteNative = func(_ context.Context, key, valueName string) error {
		called = append(called, "delete\x00"+key+"\x00"+valueName)
		return nil
	}

	registryAdapter, _ := newConfigRestorePlatformAdapters(validation)
	const semantic = `HKCU\Software\Vendor\App`
	mapped, err := validation.MapHKCU(semantic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registryAdapter.ReadValue(context.Background(), semantic, "Theme"); err != nil {
		t.Fatal(err)
	}
	if err := registryAdapter.SetValue(context.Background(), semantic, "Theme", registry.SZ, []byte("dark")); err != nil {
		t.Fatal(err)
	}
	if err := registryAdapter.DeleteValue(context.Background(), semantic, "Theme"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"read\x00" + mapped + "\x00Theme",
		"set\x00" + mapped + "\x00Theme",
		"delete\x00" + mapped + "\x00Theme",
	}
	if fmt.Sprint(called) != fmt.Sprint(want) {
		t.Fatalf("native calls = %#v, want %#v", called, want)
	}
	if _, err := registryAdapter.ReadValue(context.Background(), `HKLM\Software\Vendor\App`, "Theme"); err == nil {
		t.Fatal("HKLM identity reached validation registry adapter")
	}
	if len(called) != 3 {
		t.Fatalf("unsafe hive reached native callbacks: %#v", called)
	}
}

func TestRestoreConfigRestoreExecutionOptionsBindsWindowsPlatformAdapter(t *testing.T) {
	validation := validationContext(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", InitialState: "present",
	})
	originalValidation := currentValidationMode
	currentValidationMode = validation
	t.Cleanup(func() { currentValidationMode = originalValidation })

	options := restoreConfigRestoreExecutionOptions(
		RestoreFlags{Manifest: filepath.Join(validation.Root(), "manifest.jsonc")},
		"restore-platform-validation", validation.Root(), nil,
	)
	adapter, ok := options.Registry.(windowsConfigRestoreRegistry)
	if !ok || adapter.validation != validation {
		t.Fatalf("restore registry adapter = %#v, want validation context %p", options.Registry, validation)
	}
}

func TestConfigRestoreHKCUSubkeyNormalizesAcceptedPrefixAndRejectsUnsafeHives(t *testing.T) {
	got, err := configRestoreHKCUSubkey(`hKcU/Software/Endstate/Test`)
	if err != nil || got != `Software\Endstate\Test` {
		t.Fatalf("accepted subkey = %q, %v", got, err)
	}
	for _, key := range []string{`HKLM\Software\Endstate`, `HKCU`, "HKCU\\Software\x00Bad"} {
		if _, err := configRestoreHKCUSubkey(key); err == nil {
			t.Fatalf("unsafe key %q was accepted", key)
		}
	}
}

func TestWindowsConfigRestoreRegistryRoundTripsExactRawValue(t *testing.T) {
	subkey := fmt.Sprintf(`Software\Endstate\ConfigRestoreTest-%d`, time.Now().UTC().UnixNano())
	key := `HKCU\` + subkey
	valueName := "raw-binary"
	adapter := windowsConfigRestoreRegistry{}
	t.Cleanup(func() {
		_ = adapter.DeleteValue(context.Background(), key, valueName)
		if err := registry.DeleteKey(registry.CURRENT_USER, subkey); err != nil && err != registry.ErrNotExist {
			t.Errorf("cleanup registry key: %v", err)
		}
	})

	before, err := adapter.ReadValue(context.Background(), key, valueName)
	if err != nil {
		t.Fatal(err)
	}
	if before.Exists {
		t.Fatalf("fresh value unexpectedly exists: %+v", before)
	}

	want := []byte{0x00, 0xff, 0x01, 0x7f, 0x00}
	if err := adapter.SetValue(context.Background(), key, valueName, registry.BINARY, want); err != nil {
		t.Fatal(err)
	}
	afterSet, err := adapter.ReadValue(context.Background(), key, valueName)
	if err != nil {
		t.Fatal(err)
	}
	if !afterSet.Exists || afterSet.ValueType != registry.BINARY || !bytes.Equal(afterSet.Data, want) {
		t.Fatalf("round-trip = %+v, want type=%d data=%v", afterSet, registry.BINARY, want)
	}

	if err := adapter.DeleteValue(context.Background(), key, valueName); err != nil {
		t.Fatal(err)
	}
	afterDelete, err := adapter.ReadValue(context.Background(), key, valueName)
	if err != nil {
		t.Fatal(err)
	}
	if afterDelete.Exists {
		t.Fatalf("deleted value still exists: %+v", afterDelete)
	}
}

// The GUI names a Windows-settings row from the restore entry's authored
// reason. When the reason does not reach the restore-item events and the
// envelope item as label, the row shows the raw registry value name.
func TestConfigRestoreRegistrySetReasonReachesEventsAndEnvelopeAsLabel(t *testing.T) {
	subkey := fmt.Sprintf(`Software\Endstate\RestoreLabelTest-%d`, time.Now().UTC().UnixNano())
	t.Cleanup(func() {
		if err := registry.DeleteKey(registry.CURRENT_USER, subkey); err != nil && err != registry.ErrNotExist {
			t.Errorf("cleanup registry key: %v", err)
		}
	})
	reason := "Delay before a held key starts repeating"
	moduleID := "windows-settings.keyboard"
	inputs := emptyConfigRestoreInputs()
	inputs.hasConfigPayloads = true
	inputs.legacyLanes = []configRestoreLegacyLane{{
		captureID: bundle.LegacyCaptureID(moduleID), moduleID: moduleID, configSetID: "legacy", selected: true,
		restoreEntries: []manifest.RestoreEntry{{
			Type: "registry-set", Key: `HKCU\` + subkey, ValueName: "KeyboardDelay", ValueType: "REG_SZ", Data: "1",
			Reason: reason, FromModule: moduleID,
		}},
	}}
	guard := &recordingLiveConfigRestoreGuard{base: t.TempDir()}
	restoreExecutionSeams(t,
		func(context.Context, string, string, configrestore.RegistryMutator, configrestore.HostBoundary) (liveConfigRestoreGuard, error) {
			return guard, nil
		},
		stageConfigRestoreSetFn, materializeConfigRestoreSetFn, executeLiveConfigRestoreSetFn,
	)
	// The preview runs first, so the live run still finds the value absent and writes it.
	for _, preview := range []bool{true, false} {
		buffer := &bytes.Buffer{}
		session := &configRestoreExecutionSession{
			runtime:     newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot()),
			coordinator: &staticConfigRestoreCoordinator{final: emptyConfigRestorePlan()},
		}
		result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
			RestoreEnabled: true, DryRun: preview, RunID: "restore-label", StateDir: t.TempDir(),
			BackupDir: t.TempDir(), JournalLogsDir: t.TempDir(),
			Emitter: events.NewEmitterWithWriter("restore-label", true, buffer),
		})
		if envErr != nil {
			t.Fatalf("preview=%v: execute: %+v", preview, envErr)
		}
		encoded, err := json.Marshal(result.RestoreItems)
		if err != nil {
			t.Fatal(err)
		}
		var items []map[string]any
		if err := json.Unmarshal(encoded, &items); err != nil {
			t.Fatal(err)
		}
		if len(items) != 1 || items[0]["status"] != "restored" || items[0]["label"] != reason {
			t.Fatalf("preview=%v: envelope restore items = %s", preview, encoded)
		}
		restoreItemEvents := 0
		for _, line := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
			var event map[string]any
			if err := json.Unmarshal([]byte(line), &event); err != nil {
				t.Fatal(err)
			}
			if event["event"] != "restore-item" {
				continue
			}
			restoreItemEvents++
			if event["label"] != reason {
				t.Fatalf("preview=%v: restore-item event = %s", preview, line)
			}
		}
		if restoreItemEvents != 2 {
			t.Fatalf("preview=%v: %d restore-item events, want restoring and restored", preview, restoreItemEvents)
		}
	}
}
