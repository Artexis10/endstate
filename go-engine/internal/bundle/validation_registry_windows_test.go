// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package bundle

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
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
	if !strings.Contains(text, `[HKEY_CURRENT_USER\Software\Vendor\Example]`) || !strings.Contains(text, `[HKEY_CURRENT_USER\Software\Vendor\Example\Child]`) {
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

// TestCollectRegistryKeysFiltersSecretsBeforePublishing catches a production
// export that lands in the payload tree before its secrets are filtered, so a
// failed or skipped filter would leave secret bytes in configs/. It also
// catches an export that cannot be filtered being published anyway.
func TestCollectRegistryKeysFiltersSecretsBeforePublishing(t *testing.T) {
	for _, test := range []struct {
		name      string
		export    string
		published bool
	}{
		{
			name: "filterable export",
			export: "Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Example]\r\n\"Theme\"=\"dark\"\r\n\r\n" +
				"[HKEY_CURRENT_USER\\Software\\Example\\Secret]\r\n\"Token\"=\"SECRET-TOKEN\"\r\n",
			published: true,
		},
		{
			name:      "export the filter cannot classify",
			export:    "Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Example]\r\nToken=SECRET-TOKEN\r\n",
			published: false,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			staging := t.TempDir()
			original := runRegistryExport
			runRegistryExport = func(_, destination string) error {
				if strings.Contains(filepath.ToSlash(destination), "/configs/") {
					t.Fatalf("raw export written inside the payload tree: %s", destination)
				}
				return os.WriteFile(destination, encodeBundleRegistryUTF16(test.export), 0o600)
			}
			t.Cleanup(func() { runRegistryExport = original })
			mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{
				Key: `HKCU\Software\Example`, Dest: "settings.reg", Optional: true,
			}}}, Secrets: &modules.SecretsDef{RegistryKeys: []string{`HKCU\Software\Example\Secret`}}}

			collection, err := collectRegistryKeyCapture(mod, staging, nil)
			if err != nil {
				t.Fatalf("collectRegistryKeyCapture() error = %v", err)
			}
			payload := filepath.Join(staging, "configs", "example", "settings.reg")
			data, readErr := os.ReadFile(payload)
			if !test.published {
				if !os.IsNotExist(readErr) || len(collection.Paths) != 0 || len(collection.Conflicts) != 1 {
					t.Fatalf("unfilterable export: paths=%v conflicts=%+v readErr=%v, want nothing published and one conflict", collection.Paths, collection.Conflicts, readErr)
				}
				return
			}
			if readErr != nil {
				t.Fatal(readErr)
			}
			text := decodeBundleRegistryUTF16(t, data)
			if strings.Contains(text, "SECRET") || !strings.Contains(text, `"Theme"="dark"`) {
				t.Fatalf("published export = %q, want the setting without the secret", text)
			}
			if collection.SecretsExcluded != 1 {
				t.Fatalf("SecretsExcluded = %d, want the one filtered key", collection.SecretsExcluded)
			}
		})
	}
}

func TestCollectRegistryValuesRejectsSecretBeforeQueryOrOutput(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	original := runRegistryQuery
	runRegistryQuery = func(_, _ string) ([]byte, error) { panic("registry query reached") }
	t.Cleanup(func() { runRegistryQuery = original })
	staging := bundleValidationWorkRoot(t, context, "registry-value-secret")
	mod := &modules.Module{ID: "apps.example", Capture: &modules.CaptureDef{RegistryValues: []modules.CaptureRegistryValue{{Key: `HKCU\Software\Example\Secret\Child`, ValueName: "Theme"}}}, Secrets: &modules.SecretsDef{Files: []string{`HKCU\Software\Example\Secret`}}}
	if _, err := CollectRegistryValuesWithValidation(mod, staging, context); !errors.Is(err, validationmode.ErrUnsafeRegistry) {
		t.Fatalf("CollectRegistryValuesWithValidation() error = %v, want ErrUnsafeRegistry", err)
	}
	if _, err := os.Stat(filepath.Join(staging, "configs", "example", "registry-values.json")); !os.IsNotExist(err) {
		t.Fatalf("registry values output exists after boundary failure: %v", err)
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

// TestCollectRegistryKeysFiltersRealExportInValidationNamespace catches a
// filter that only works on hand-written fixtures. It runs real reg.exe export
// and import against the disposable validation namespace under
// HKCU\Software\Endstate, seeds a setting next to a secret value and a secret
// subkey, captures, restores the payload into a cleared key, and reads the
// registry back.
func TestCollectRegistryKeysFiltersRealExportInValidationNamespace(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	semantic := `HKCU\Software\Vendor\Example`
	physical, err := context.MapHKCU(semantic)
	if err != nil {
		t.Fatal(err)
	}
	namespace := context.RegistryNamespace()
	scratch := strings.ToLower(`HKCU\Software\Endstate\`)
	if !strings.HasPrefix(strings.ToLower(physical), scratch) || !strings.HasPrefix(strings.ToLower(namespace), scratch) {
		t.Fatalf("mapped key %s leaves the Endstate scratch namespace", physical)
	}
	t.Cleanup(func() { _ = exec.Command("reg", "delete", namespace, "/f").Run() })
	reg := func(args ...string) {
		t.Helper()
		if output, err := exec.Command("reg", args...).CombinedOutput(); err != nil {
			t.Fatalf("reg %v: %v\n%s", args, err, output)
		}
	}
	reg("add", physical, "/v", "Theme", "/t", "REG_SZ", "/d", "dark", "/f")
	reg("add", physical, "/v", "Token", "/t", "REG_SZ", "/d", "SECRET-TOKEN", "/f")
	reg("add", physical+`\Secret`, "/v", "Key", "/t", "REG_SZ", "/d", "SECRET-SUBKEY", "/f")

	mod := &modules.Module{ID: "apps.example",
		Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{{Key: semantic, Dest: "settings.reg"}}},
		Secrets: &modules.SecretsDef{RegistryValues: []modules.RegistrySecretValue{
			{Key: semantic, ValueName: "Token"},
			{Key: semantic + `\Secret`, Subkeys: true},
		}},
	}
	staging := bundleValidationWorkRoot(t, context, "registry-real")
	collection, err := collectRegistryKeyCapture(mod, staging, context)
	if err != nil {
		t.Fatalf("collectRegistryKeyCapture() error = %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(staging, "configs", "example", "settings.reg"))
	if err != nil {
		t.Fatal(err)
	}
	text := decodeBundleRegistryUTF16(t, payload)
	if strings.Contains(text, "SECRET") || !strings.Contains(text, `"Theme"="dark"`) {
		t.Fatalf("captured payload = %q, want the setting without secrets", text)
	}
	if collection.SecretsExcluded != 2 {
		t.Fatalf("SecretsExcluded = %d, want the secret value and the secret subkey value", collection.SecretsExcluded)
	}

	// Restore into a cleared key exactly as validation-mode registry import
	// does: map the semantic payload into the namespace, then reg import.
	reg("delete", physical, "/f")
	mapped, err := registryfile.RewriteSubtree(payload, semantic, physical)
	if err != nil {
		t.Fatal(err)
	}
	importFile := filepath.Join(t.TempDir(), "restore.reg")
	if err := os.WriteFile(importFile, mapped, 0o600); err != nil {
		t.Fatal(err)
	}
	reg("import", importFile)

	if output, err := exec.Command("reg", "query", physical, "/v", "Theme").Output(); err != nil || !strings.Contains(string(output), "dark") {
		t.Fatalf("restored Theme = %q, %v; want dark", output, err)
	}
	if err := exec.Command("reg", "query", physical, "/v", "Token").Run(); err == nil {
		t.Fatal("restore recreated the secret value")
	}
	if err := exec.Command("reg", "query", physical+`\Secret`, "/v", "Key").Run(); err == nil {
		t.Fatal("restore recreated the secret subkey value")
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
