// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package bundle

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestCollectRegistryKeysWithValidationMapsExportAndPublishesSemanticDocument(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	semantic := `HKCU\Software\Vendor\Example`
	physical, err := context.MapHKCU(semantic)
	if err != nil {
		t.Fatal(err)
	}
	original := runRegistryExport
	runRegistryExport = func(key, destination string) error {
		if !strings.EqualFold(key, physical) {
			t.Fatalf("reg export key = %q, want mapped key", key)
		}
		if filepath.Base(destination) == "settings.reg" {
			t.Fatal("reg export wrote directly to the published destination")
		}
		content := "Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\" + strings.TrimPrefix(physical, `HKCU\`) + "]\r\n" +
			`"Root"="unchanged"` + "\r\n\r\n[HKEY_CURRENT_USER\\" + strings.TrimPrefix(physical, `HKCU\`) + `\Child]` + "\r\n"
		return os.WriteFile(destination, encodeBundleRegistryUTF16(content), 0o600)
	}
	t.Cleanup(func() { runRegistryExport = original })

	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{
		Key: semantic, Dest: "settings.reg",
	}}}}
	staging := bundleValidationWorkRoot(t, context, "registry-key")
	paths, err := CollectRegistryKeysWithValidation(mod, staging, context)
	if err != nil {
		t.Fatalf("CollectRegistryKeysWithValidation: %v", err)
	}
	if len(paths) != 1 || paths[0] != "configs/example/settings.reg" {
		t.Fatalf("paths = %#v", paths)
	}
	data, err := os.ReadFile(filepath.Join(staging, "configs", "example", "settings.reg"))
	if err != nil {
		t.Fatal(err)
	}
	text := decodeBundleRegistryUTF16(t, data)
	if !strings.Contains(text, `[HKCU\Software\Vendor\Example]`) || !strings.Contains(text, `[HKCU\Software\Vendor\Example\Child]`) {
		t.Fatalf("semantic sections missing: %q", text)
	}
	if strings.Contains(strings.ToLower(text), strings.ToLower(context.RegistryNamespace())) {
		t.Fatalf("physical nonce namespace was published: %q", text)
	}
}

func TestCollectRegistryKeysWithValidationRejectsInjectedSection(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	semantic := `HKCU\Software\Vendor\Example`
	physical, _ := context.MapHKCU(semantic)
	original := runRegistryExport
	runRegistryExport = func(_, destination string) error {
		content := "Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\" + strings.TrimPrefix(physical, `HKCU\`) + "]\r\n" +
			"[HKEY_CURRENT_USER\\Software\\Injected]\r\n"
		return os.WriteFile(destination, encodeBundleRegistryUTF16(content), 0o600)
	}
	t.Cleanup(func() { runRegistryExport = original })
	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: semantic, Dest: "settings.reg"}}}}
	staging := bundleValidationWorkRoot(t, context, "registry-injected")
	_, err := CollectRegistryKeysWithValidation(mod, staging, context)
	var isolation *CaptureIsolationError
	if !errors.As(err, &isolation) || isolation.Coordinate != "capture.registryKeys[0].key" {
		t.Fatalf("error = %T %v", err, err)
	}
	if strings.Contains(strings.ToLower(err.Error()), strings.ToLower(context.Root())) || strings.Contains(strings.ToLower(err.Error()), strings.ToLower(context.RegistryNamespace())) {
		t.Fatalf("isolation error leaked physical authority: %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(staging, "configs", "example", "settings.reg")); !os.IsNotExist(statErr) {
		t.Fatalf("unsafe registry document was published: %v", statErr)
	}
}

func TestCollectRegistryKeysWithValidationMapsBeforeExec(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	original := runRegistryExport
	runRegistryExport = func(_, _ string) error { panic("registry export reached") }
	t.Cleanup(func() { runRegistryExport = original })
	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: `HKLM\Software\Vendor`, Dest: "settings.reg"}}}}
	if _, err := CollectRegistryKeysWithValidation(mod, bundleValidationWorkRoot(t, context, "registry-wrong-hive"), context); err == nil {
		t.Fatal("wrong hive reached registry export")
	}
}

func TestCollectRegistryValuesWithValidationReadsMappedKeyAndWritesSemanticIdentity(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	semantic := `HKCU\Software\Vendor\Example`
	physical, _ := context.MapHKCU(semantic)
	original := runRegistryQuery
	runRegistryQuery = func(key, valueName string) ([]byte, error) {
		if !strings.EqualFold(key, physical) || valueName != "Theme" {
			t.Fatalf("reg query = %q %q", key, valueName)
		}
		return []byte("    Theme    REG_DWORD    0x0000002a\r\n"), nil
	}
	t.Cleanup(func() { runRegistryQuery = original })
	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{RegistryValues: []modules.CaptureRegistryValue{{Key: semantic, ValueName: "Theme"}}}}
	staging := bundleValidationWorkRoot(t, context, "registry-value")
	paths, err := CollectRegistryValuesWithValidation(mod, staging, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 {
		t.Fatalf("paths = %#v", paths)
	}
	data, err := os.ReadFile(filepath.Join(staging, "configs", "example", "registry-values.json"))
	if err != nil {
		t.Fatal(err)
	}
	var values []CapturedRegistryValue
	if err := json.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Key != semantic || values[0].ValueType != "REG_DWORD" || values[0].Data != "42" || !values[0].Existed {
		t.Fatalf("captured values = %#v", values)
	}
	if strings.Contains(strings.ToLower(string(data)), strings.ToLower(context.RegistryNamespace())) {
		t.Fatalf("named-value JSON contains mapped identity: %s", data)
	}
}

func TestCollectRegistryValuesWithValidationMapsBeforeQuery(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	original := runRegistryQuery
	runRegistryQuery = func(_, _ string) ([]byte, error) { panic("registry query reached") }
	t.Cleanup(func() { runRegistryQuery = original })
	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{RegistryValues: []modules.CaptureRegistryValue{{Key: `HKLM\Software\Vendor`, ValueName: "Theme"}}}}
	if _, err := CollectRegistryValuesWithValidation(mod, bundleValidationWorkRoot(t, context, "registry-value-wrong-hive"), context); err == nil {
		t.Fatal("wrong hive reached registry query")
	}
}

func encodeBundleRegistryUTF16(value string) []byte {
	words := utf16.Encode([]rune(value))
	result := make([]byte, 2+len(words)*2)
	result[0], result[1] = 0xff, 0xfe
	for index, word := range words {
		binary.LittleEndian.PutUint16(result[2+index*2:], word)
	}
	return result
}

func decodeBundleRegistryUTF16(t *testing.T, value []byte) string {
	t.Helper()
	if len(value) < 2 || value[0] != 0xff || value[1] != 0xfe || (len(value)-2)%2 != 0 {
		t.Fatal("invalid UTF-16LE output")
	}
	words := make([]uint16, (len(value)-2)/2)
	for index := range words {
		words[index] = binary.LittleEndian.Uint16(value[2+index*2:])
	}
	return string(utf16.Decode(words))
}
