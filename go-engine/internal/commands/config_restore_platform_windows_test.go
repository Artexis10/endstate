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

	"golang.org/x/sys/windows/registry"
)

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
