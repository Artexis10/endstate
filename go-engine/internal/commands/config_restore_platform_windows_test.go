// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package commands

import (
	"bytes"
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
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
