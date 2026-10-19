// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package bundle

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// regExportUTF16 renders reg.exe export text the way reg.exe writes it:
// UTF-16LE with a byte-order mark and CRLF line endings.
func regExportUTF16(lines ...string) []byte {
	text := strings.Join(lines, "\r\n") + "\r\n"
	units := utf16.Encode([]rune(text))
	data := make([]byte, 2+len(units)*2)
	data[0], data[1] = 0xff, 0xfe
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[2+index*2:], unit)
	}
	return data
}

func readZipEntries(t *testing.T, zipPath string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	entries := map[string][]byte{}
	for _, file := range reader.File {
		handle, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(handle)
		_ = handle.Close()
		if err != nil {
			t.Fatal(err)
		}
		entries[file.Name] = data
	}
	return entries
}

func utf16Bytes(value string) []byte {
	units := utf16.Encode([]rune(value))
	data := make([]byte, len(units)*2)
	for index, unit := range units {
		binary.LittleEndian.PutUint16(data[index*2:], unit)
	}
	return data
}

func loadShippedModule(t *testing.T, tier, directory string) *modules.Module {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "modules", tier, directory, "module.jsonc"))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := modules.ParseModuleJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	return mod
}

// TestCreateCaptureBundleFiltersCatalogSecretModules catches the failure where
// one shipped module whose registry capture overlaps its own secret
// declaration aborts the whole capture, or where the secret values it
// declares ride into the bundle. The exports below put sentinel secret bytes
// exactly where each module declares its secrets, next to ordinary settings.
func TestCreateCaptureBundleFiltersCatalogSecretModules(t *testing.T) {
	exports := map[string][]byte{
		strings.ToLower(`HKCU\Software\Piriform\CCleaner`): regExportUTF16(
			"Windows Registry Editor Version 5.00",
			"",
			`[HKEY_CURRENT_USER\Software\Piriform\CCleaner]`,
			`"name"="SENTINEL-CCLEANER-NAME"`,
			`"License"="SENTINEL-CCLEANER-LICENSE"`,
			`"WINDOW_WIDTH"="1024"`,
			`"(App)Cookies"="True"`,
		),
		strings.ToLower(`HKCU\Software\TigerVNC\vncviewer`): regExportUTF16(
			"Windows Registry Editor Version 5.00",
			"",
			`[HKEY_CURRENT_USER\Software\TigerVNC\vncviewer]`,
			`"PASSWORD"=hex:53,45,4e,54,49,4e,45,4c,2d,54,49,47,45,52,\`,
			`  56,4e,43`,
			`"FullScreen"=dword:00000001`,
		),
		strings.ToLower(`HKCU\Software\Binary Fortress Software\DisplayFusion\HotKeys`): regExportUTF16(
			"Windows Registry Editor Version 5.00",
			"",
			`[HKEY_CURRENT_USER\Software\Binary Fortress Software\DisplayFusion\HotKeys]`,
			`"HotKey_MoveWindow"="Ctrl+Alt+M"`,
		),
		strings.ToLower(`HKCU\Software\Binary Fortress Software\DisplayFusion\Taskbars`): regExportUTF16(
			"Windows Registry Editor Version 5.00",
			"",
			`[HKEY_CURRENT_USER\Software\Binary Fortress Software\DisplayFusion\Taskbars\1]`,
			`"Enabled"=dword:00000001`,
		),
		strings.ToLower(`HKCU\Software\RealVNC\vncviewer`): regExportUTF16(
			"Windows Registry Editor Version 5.00",
			"",
			`[HKEY_CURRENT_USER\Software\RealVNC\vncviewer]`,
			`"Password"="SENTINEL-REALVNC-TOP"`,
			`"Scaling"="AspectFit"`,
			"",
			`[HKEY_CURRENT_USER\Software\RealVNC\vncviewer\Connections\office]`,
			`"EncryptedPassword"=hex(3):53,45,4e,54,49,4e,45,4c`,
			`"Host"="viewer.example.invalid"`,
			"",
			`[HKEY_CURRENT_USER\Software\RealVNC\vncviewer\Password]`,
			`"Store"="SENTINEL-REALVNC-KEY"`,
		),
		strings.ToLower(`HKCU\Software\VS Revo Group`): regExportUTF16(
			"Windows Registry Editor Version 5.00",
			"",
			`[HKEY_CURRENT_USER\Software\VS Revo Group\Revo Uninstaller Pro]`,
			`"License"="SENTINEL-REVO-LICENSE"`,
			`"Notes"="first line`,
			`second line"`,
			`"View"=dword:00000002`,
			"",
			`[HKEY_CURRENT_USER\Software\VS Revo Group\Revo Uninstaller Pro\License]`,
			`"Key"="SENTINEL-REVO-KEY"`,
		),
		strings.ToLower(`HKCU\Software\TablePlus\TablePlus`): regExportUTF16(
			"Windows Registry Editor Version 5.00",
			"",
			`[HKEY_CURRENT_USER\Software\TablePlus\TablePlus]`,
			`@="default-profile"`,
			`"Say \"hi\""="ok"`,
			`"Theme"="dark"`,
			"",
			`[HKEY_CURRENT_USER\Software\TablePlus\TablePlus\WinSparkle]`,
			`"UpdateToken"="SENTINEL-TABLEPLUS-WINSPARKLE"`,
		),
	}
	originalExport := runRegistryExport
	t.Cleanup(func() { runRegistryExport = originalExport })
	runRegistryExport = func(key, destination string) error {
		data, ok := exports[strings.ToLower(key)]
		if !ok {
			return errors.New("key not present")
		}
		return os.WriteFile(destination, data, 0o600)
	}

	dir := t.TempDir()
	selected := []*modules.Module{
		loadShippedModule(t, "apps", "ccleaner"),
		loadShippedModule(t, "apps", "tigervnc"),
		loadShippedModule(t, "apps", "displayfusion"),
		loadShippedModule(t, "apps", "realvnc-viewer"),
		loadShippedModule(t, "apps", "revo-uninstaller"),
		loadShippedModule(t, "apps", "tableplus"),
		testLegacyCaptureModule(t, dir, "apps.safe-file", "settings"),
	}
	result, err := CreateCaptureBundle(testCaptureBundleRequest(t, dir, selected, nil))
	if err != nil {
		t.Fatalf("CreateCaptureBundle() error = %v, want the capture to succeed with secrets filtered", err)
	}

	entries := readZipEntries(t, filepath.Join(dir, "capture.zip"))
	for name, data := range entries {
		if bytes.Contains(data, []byte("SENTINEL")) || bytes.Contains(data, utf16Bytes("SENTINEL")) {
			t.Errorf("bundle entry %s carries secret text", name)
		}
		if bytes.Contains(data, utf16Bytes("53,45,4e,54,49,4e,45,4c")) {
			t.Errorf("bundle entry %s carries hex-encoded secret bytes", name)
		}
	}
	for payload, setting := range map[string]string{
		"configs/ccleaner/CCleaner.reg":            `"WINDOW_WIDTH"="1024"`,
		"configs/tigervnc/vncviewer.reg":           `"FullScreen"=dword:00000001`,
		"configs/displayfusion/HotKeys.reg":        `"HotKey_MoveWindow"="Ctrl+Alt+M"`,
		"configs/displayfusion/Taskbars.reg":       `"Enabled"=dword:00000001`,
		"configs/realvnc-viewer/vncviewer.reg":     `"Host"="viewer.example.invalid"`,
		"configs/revo-uninstaller/VSRevoGroup.reg": "\"Notes\"=\"first line\r\nsecond line\"",
		"configs/tableplus/Settings.reg":           `"Say \"hi\""="ok"`,
	} {
		data, ok := entries[payload]
		if !ok {
			t.Errorf("bundle lacks %s; entries = %v", payload, len(entries))
			continue
		}
		if !bytes.Contains(data, utf16Bytes(setting)) {
			t.Errorf("%s lost the ordinary setting %q", payload, setting)
		}
	}
	if _, ok := entries["configs/safe-file/settings.json"]; !ok {
		t.Error("the unrelated file module was not captured")
	}
	filteredCount := 0
	for _, module := range result.LegacyModules {
		filteredCount += module.SecretsExcluded
	}
	if filteredCount == 0 {
		t.Error("filtered secret registry items were not counted as sensitive exclusions")
	}
}

// TestCreateCaptureBundleSkipsContradictoryEntryKeepsOthers catches the
// failure where a capture entry that sits inside its own module's secret
// subtree either gets exported anyway or takes the whole capture down with
// it. The contradictory key must never reach reg export; the module's other
// key and the unrelated module must still be captured, and the warning must
// name the module.
func TestCreateCaptureBundleSkipsContradictoryEntryKeepsOthers(t *testing.T) {
	var exported []string
	originalExport := runRegistryExport
	t.Cleanup(func() { runRegistryExport = originalExport })
	runRegistryExport = func(key, destination string) error {
		exported = append(exported, key)
		return os.WriteFile(destination, regExportUTF16(
			"Windows Registry Editor Version 5.00",
			"",
			"["+strings.Replace(key, `HKCU\`, `HKEY_CURRENT_USER\`, 1)+"]",
			`"Mode"=dword:00000001`,
		), 0o600)
	}

	dir := t.TempDir()
	contradictory := &modules.Module{
		ID: "apps.contoso", DisplayName: "Contoso Notes",
		Capture: &modules.CaptureDef{RegistryKeys: []modules.CaptureRegistryKey{
			{Key: `HKCU\Software\Contoso\Notes\Vault\Cache`, Dest: "apps/contoso/vault.reg", Optional: true},
			{Key: `HKCU\Software\Contoso\Notes\View`, Dest: "apps/contoso/view.reg", Optional: true},
		}},
		Restore: []modules.RestoreDef{
			{Type: "registry-import", Source: "./payload/apps/contoso/vault.reg", Target: `HKCU\Software\Contoso\Notes\Vault\Cache`, Backup: true, Optional: true},
			{Type: "registry-import", Source: "./payload/apps/contoso/view.reg", Target: `HKCU\Software\Contoso\Notes\View`, Backup: true, Optional: true},
		},
		Secrets: &modules.SecretsDef{RegistryKeys: []string{`HKCU\Software\Contoso\Notes\Vault`}},
	}
	selected := []*modules.Module{contradictory, testLegacyCaptureModule(t, dir, "apps.safe-file", "settings")}

	result, err := CreateCaptureBundle(testCaptureBundleRequest(t, dir, selected, nil))
	if err != nil {
		t.Fatalf("CreateCaptureBundle() error = %v, want the other entries captured", err)
	}
	for _, key := range exported {
		if strings.Contains(strings.ToLower(key), `\vault`) {
			t.Fatalf("contradictory key %s was exported", key)
		}
	}
	names := zipEntryNames(t, filepath.Join(dir, "capture.zip"))
	if !containsString(names, "configs/contoso/view.reg") || !containsString(names, "configs/safe-file/settings.json") {
		t.Fatalf("bundle entries = %v, want the safe registry key and the file module", names)
	}
	if containsString(names, "configs/contoso/vault.reg") {
		t.Fatalf("bundle carries the contradictory payload: %v", names)
	}
	var contoso *LegacyModuleCaptureResult
	for index := range result.LegacyModules {
		if result.LegacyModules[index].ModuleID == "apps.contoso" {
			contoso = &result.LegacyModules[index]
		}
	}
	if contoso == nil || len(contoso.Conflicts) != 1 || contoso.Conflicts[0].Coordinate != "capture.registryKeys[0].key" {
		t.Fatalf("contoso result = %+v, want one structured conflict at capture.registryKeys[0].key", contoso)
	}
	warned := false
	for _, warning := range result.CaptureWarnings {
		if strings.Contains(warning, "Contoso Notes") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("capture warnings %q do not name the module", result.CaptureWarnings)
	}
}

// TestCreateCaptureBundleBindsRegistrySetToCapturedValues catches the
// failure where the Windows Explorer module ships its preset data instead of
// the old PC's value, or restores a value that was never set on the old PC.
// HideFileExt is 1 at capture and Hidden is absent, so the bundle must restore
// and verify exactly HideFileExt=1 and say why Hidden was left alone.
func TestCreateCaptureBundleBindsRegistrySetToCapturedValues(t *testing.T) {
	originalQuery := runRegistryQuery
	t.Cleanup(func() { runRegistryQuery = originalQuery })
	runRegistryQuery = func(key, valueName string) ([]byte, error) {
		if valueName != "HideFileExt" {
			return nil, errors.New("ERROR: The system was unable to find the specified registry key or value.")
		}
		return []byte("\r\nHKEY_CURRENT_USER\\Software\\Microsoft\\Windows\\CurrentVersion\\Explorer\\Advanced\r\n    HideFileExt    REG_DWORD    0x1\r\n\r\n"), nil
	}

	dir := t.TempDir()
	explorer := loadShippedModule(t, "windows-settings", "explorer")
	result, err := CreateCaptureBundle(testCaptureBundleRequest(t, dir, []*modules.Module{explorer}, nil))
	if err != nil {
		t.Fatal(err)
	}
	loaded, _ := loadCaptureBundle(t, filepath.Join(dir, "capture.zip"))

	if len(loaded.Restore) != 1 {
		t.Fatalf("restore entries = %+v, want only the captured HideFileExt", loaded.Restore)
	}
	restore := loaded.Restore[0]
	if restore.Type != "registry-set" || restore.ValueName != "HideFileExt" || restore.ValueType != "REG_DWORD" || restore.Data != "1" {
		t.Fatalf("restore = %+v, want registry-set HideFileExt REG_DWORD 1", restore)
	}
	if len(loaded.Verify) != 1 {
		t.Fatalf("verify entries = %+v, want only the captured HideFileExt", loaded.Verify)
	}
	verify := loaded.Verify[0]
	if verify.Type != "registry-value-equals" || verify.ValueName != "HideFileExt" || verify.ValueType != "REG_DWORD" || verify.Data != "1" {
		t.Fatalf("verify = %+v, want registry-value-equals HideFileExt REG_DWORD 1", verify)
	}
	var explorerResult *LegacyModuleCaptureResult
	for index := range result.LegacyModules {
		if result.LegacyModules[index].ModuleID == explorer.ID {
			explorerResult = &result.LegacyModules[index]
		}
	}
	if explorerResult == nil || explorerResult.Status != LegacyCaptureStatusCaptured {
		t.Fatalf("explorer result = %+v, want captured", explorerResult)
	}
	mentionsHidden := false
	for _, warning := range explorerResult.Warnings {
		if strings.Contains(warning, "Hidden") {
			mentionsHidden = true
		}
	}
	if !mentionsHidden {
		t.Fatalf("module warnings %q do not explain the absent Hidden value", explorerResult.Warnings)
	}
}
