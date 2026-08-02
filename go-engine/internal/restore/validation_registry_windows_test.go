// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestValidationRegistrySetMapsOnlyAtNativeBoundaryAndKeepsBackupSemantic(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-regset")
	semanticKey := `HKCU\Software\Vendor\Example`
	mappedKey, err := context.MapHKCU(semanticKey)
	if err != nil {
		t.Fatal(err)
	}
	originalRead, originalWrite := registrySetReadNative, registrySetWriteNative
	var readKey, writeKey string
	registrySetReadNative = func(key, name string) (bool, string, string, bool) {
		readKey = key
		return true, "REG_SZ", "prior", true
	}
	registrySetWriteNative = func(key, name, valueType, data string) error {
		writeKey = key
		if name != "Setting" || valueType != "REG_SZ" || data != "desired" {
			t.Fatalf("native registry write = %q %q %q", name, valueType, data)
		}
		return nil
	}
	t.Cleanup(func() {
		registrySetReadNative, registrySetWriteNative = originalRead, originalWrite
	})
	backupDir := filepath.Join(context.Root(), "state", "backups", "legacy-regset-run")
	results, err := RunRestore([]RestoreAction{{
		Type: "registry-set", Key: semanticKey, ValueName: "Setting", ValueType: "REG_SZ", Data: "desired",
	}}, RestoreOptions{BackupDir: backupDir, RunID: "legacy-regset-run", ValidationContext: context}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "restored" {
		t.Fatalf("results = %+v", results)
	}
	if !strings.EqualFold(readKey, mappedKey) || !strings.EqualFold(writeKey, mappedKey) {
		t.Fatalf("native keys = read %q write %q, want %q", readKey, writeKey, mappedKey)
	}
	if results[0].Target != semanticKey+`\Setting` || !strings.HasPrefix(results[0].BackupPath, "$ENDSTATE_ROOT/") {
		t.Fatalf("semantic result = %+v", results[0])
	}
	assertNoLegacyValidationIdentity(t, context, results[0].Target, results[0].BackupPath, results[0].Error)
	backupData, err := os.ReadFile(expandPath(results[0].BackupPath))
	if err != nil {
		t.Fatal(err)
	}
	var backup registrySetBackup
	if err := json.Unmarshal(backupData, &backup); err != nil {
		t.Fatal(err)
	}
	if backup.Key != semanticKey || backup.ValueName != "Setting" || backup.PriorData != "prior" {
		t.Fatalf("semantic backup = %+v", backup)
	}
	assertNoLegacyValidationIdentity(t, context, string(backupData))
}

func TestValidationRegistryImportRewritesNestedInputAndBackupAtNativeBoundary(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-regimport")
	semanticKey := `HKCU\Software\Vendor\Example`
	mappedKey, err := context.MapHKCU(semanticKey)
	if err != nil {
		t.Fatal(err)
	}
	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-regimport")
	source := filepath.Join(manifestRoot, "payload", "settings.reg")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	sourceText := "Windows Registry Editor Version 5.00\r\n\r\n" +
		"[HKEY_CURRENT_USER\\Software\\Vendor\\Example]\r\n\"Root\"=\"desired\"\r\n\r\n" +
		"[HKEY_CURRENT_USER\\Software\\Vendor\\Example\\Child]\r\n\"Nested\"=dword:00000002\r\n"
	if err := os.WriteFile(source, []byte(sourceText), 0o600); err != nil {
		t.Fatal(err)
	}

	originalQuery, originalExport, originalImport := registryImportQueryNative, registryImportExportNative, registryImportApplyNative
	var queryKey string
	var imported []byte
	registryImportQueryNative = func(key string) (bool, error) {
		queryKey = key
		return true, nil
	}
	registryImportExportNative = func(key, path string) error {
		if !strings.EqualFold(key, mappedKey) {
			return fmt.Errorf("unexpected export key")
		}
		physicalRoot := "HKEY_CURRENT_USER" + mappedKey[len("HKCU"):]
		physical := "Windows Registry Editor Version 5.00\r\n\r\n[" + physicalRoot + "]\r\n\"Root\"=\"prior\"\r\n\r\n[" + physicalRoot + "\\Child]\r\n\"Nested\"=dword:00000001\r\n"
		return os.WriteFile(path, []byte(physical), 0o600)
	}
	registryImportApplyNative = func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		imported = append([]byte(nil), data...)
		return nil
	}
	t.Cleanup(func() {
		registryImportQueryNative, registryImportExportNative, registryImportApplyNative = originalQuery, originalExport, originalImport
	})
	backupDir := filepath.Join(context.Root(), "state", "backups", "legacy-regimport-run")
	results, err := RunRestore([]RestoreAction{{
		Type: "registry-import", Source: "payload/settings.reg", Target: semanticKey, Backup: true,
	}}, RestoreOptions{
		ManifestDir: manifestRoot, BackupDir: backupDir, RunID: "legacy-regimport-run", ValidationContext: context,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "restored" {
		t.Fatalf("results = %+v", results)
	}
	if !strings.EqualFold(queryKey, mappedKey) {
		t.Fatalf("query key = %q, want %q", queryKey, mappedKey)
	}
	if _, err := registryfile.RewriteSubtree(imported, mappedKey, semanticKey); err != nil {
		t.Fatalf("native import was not a mapped nested subtree: %v", err)
	}
	if results[0].Source != "payload/settings.reg" || results[0].Target != semanticKey {
		t.Fatalf("semantic result = %+v", results[0])
	}
	assertNoLegacyValidationIdentity(t, context, results[0].Source, results[0].Target, results[0].BackupPath, results[0].Error)
	backupData, err := os.ReadFile(expandPath(results[0].BackupPath))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registryfile.RewriteSubtree(backupData, semanticKey, semanticKey); err != nil {
		t.Fatalf("published backup was not semantic: %v", err)
	}
	assertNoLegacyValidationIdentity(t, context, string(backupData))
}

func TestValidationRegistryImportSkipsExactMappedStateWithoutBackupOrImport(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-regimport-converged")
	semanticKey := `HKCU\Software\Vendor\Converged`
	mappedKey, err := context.MapHKCU(semanticKey)
	if err != nil {
		t.Fatal(err)
	}
	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-regimport-converged")
	source := filepath.Join(manifestRoot, "payload", "settings.reg")
	desired := validationRegistryDocument(semanticKey, "desired")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, desired, 0o600); err != nil {
		t.Fatal(err)
	}
	mappedCurrent, err := registryfile.RewriteSubtree(desired, semanticKey, mappedKey)
	if err != nil {
		t.Fatal(err)
	}
	originalQuery, originalExport, originalImport := registryImportQueryNative, registryImportExportNative, registryImportApplyNative
	queried, exported, imported := "", "", false
	registryImportQueryNative = func(key string) (bool, error) {
		queried = key
		return true, nil
	}
	registryImportExportNative = func(key, path string) error {
		exported = key
		return os.WriteFile(path, mappedCurrent, 0o600)
	}
	registryImportApplyNative = func(string) error {
		imported = true
		return nil
	}
	t.Cleanup(func() {
		registryImportQueryNative, registryImportExportNative, registryImportApplyNative = originalQuery, originalExport, originalImport
	})
	backupDir := filepath.Join(context.Root(), "state", "backups", "legacy-regimport-converged")
	results, err := RunRestore([]RestoreAction{{
		Type: "registry-import", Source: "payload/settings.reg", Target: semanticKey, Backup: true,
	}}, RestoreOptions{ManifestDir: manifestRoot, BackupDir: backupDir, ValidationContext: context}, nil)
	if err != nil || len(results) != 1 {
		t.Fatalf("restore = %+v, %v", results, err)
	}
	result := results[0]
	if result.Status != "skipped_up_to_date" || !result.TargetExistedBefore || result.BackupCreated || result.BackupPath != "" || imported {
		t.Fatalf("converged result = %+v imported=%v", result, imported)
	}
	if !strings.EqualFold(queried, mappedKey) || !strings.EqualFold(exported, mappedKey) {
		t.Fatalf("native keys query=%q export=%q want=%q", queried, exported, mappedKey)
	}
	assertNoLegacyValidationIdentity(t, context, result.Source, result.Target, result.BackupPath, result.Error)
}

func TestValidationRegistryImportRestoresByteOrderingMismatch(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-regimport-ordering")
	semanticKey := `HKCU\Software\Vendor\Ordering`
	mappedKey, err := context.MapHKCU(semanticKey)
	if err != nil {
		t.Fatal(err)
	}
	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-regimport-ordering")
	source := filepath.Join(manifestRoot, "payload", "settings.reg")
	desired := []byte("Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Vendor\\Ordering]\r\n\"Alpha\"=\"1\"\r\n\"Beta\"=\"2\"\r\n")
	current := []byte("Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Vendor\\Ordering]\r\n\"Beta\"=\"2\"\r\n\"Alpha\"=\"1\"\r\n")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, desired, 0o600); err != nil {
		t.Fatal(err)
	}
	mappedCurrent, err := registryfile.RewriteSubtree(current, semanticKey, mappedKey)
	if err != nil {
		t.Fatal(err)
	}
	originalQuery, originalExport, originalImport := registryImportQueryNative, registryImportExportNative, registryImportApplyNative
	imported := false
	registryImportQueryNative = func(string) (bool, error) { return true, nil }
	registryImportExportNative = func(_ string, path string) error { return os.WriteFile(path, mappedCurrent, 0o600) }
	registryImportApplyNative = func(string) error { imported = true; return nil }
	t.Cleanup(func() {
		registryImportQueryNative, registryImportExportNative, registryImportApplyNative = originalQuery, originalExport, originalImport
	})
	result, err := RestoreRegistryImport(RestoreAction{
		Type: "registry-import", Source: "payload/settings.reg", Target: semanticKey,
	}, source, RestoreOptions{ManifestDir: manifestRoot, ValidationContext: context})
	if err != nil || result.Status != "restored" || !result.TargetExistedBefore || !imported {
		t.Fatalf("ordering mismatch result = %+v imported=%v err=%v", result, imported, err)
	}
	assertNoLegacyValidationIdentity(t, context, result.Source, result.Target, result.Error)
}

func TestValidationRegistryImportStateInspectionFailuresDoNotImport(t *testing.T) {
	for _, test := range []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{
			name: "query", setup: func(t *testing.T, _ string) {
				registryImportQueryNative = func(string) (bool, error) { return false, errors.New("registry access denied") }
				registryImportExportNative = func(string, string) error { t.Fatal("query failure exported registry"); return nil }
			},
		},
		{
			name: "export", setup: func(t *testing.T, _ string) {
				registryImportQueryNative = func(string) (bool, error) { return true, nil }
				registryImportExportNative = func(string, string) error { return errors.New("registry export denied") }
			},
		},
		{
			name: "rewrite", setup: func(t *testing.T, _ string) {
				registryImportQueryNative = func(string) (bool, error) { return true, nil }
				registryImportExportNative = func(_ string, path string) error { return os.WriteFile(path, []byte("not a registry export"), 0o600) }
			},
		},
		{
			name: "read", setup: func(t *testing.T, _ string) {
				registryImportQueryNative = func(string) (bool, error) { return true, nil }
				registryImportExportNative = func(_ string, path string) error {
					return os.WriteFile(path, validationRegistryDocument(`HKCU\Software\Vendor\Failure`, "current"), 0o600)
				}
				originalRead := legacyRestoreReadFileNative
				legacyRestoreReadFileNative = func(path string) ([]byte, error) {
					if strings.Contains(filepath.Base(path), "registry-current-") {
						return nil, errors.New("registry export read denied")
					}
					return originalRead(path)
				}
				t.Cleanup(func() { legacyRestoreReadFileNative = originalRead })
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			context, _ := activeLegacyRestoreValidationContext(t, "legacy-regimport-inspect-"+test.name)
			manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-regimport-inspect")
			source := filepath.Join(manifestRoot, "payload", "settings.reg")
			if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(source, validationRegistryDocument(`HKCU\Software\Vendor\Failure`, "desired"), 0o600); err != nil {
				t.Fatal(err)
			}
			originalQuery, originalExport, originalImport := registryImportQueryNative, registryImportExportNative, registryImportApplyNative
			imported := false
			registryImportApplyNative = func(string) error { imported = true; return nil }
			t.Cleanup(func() {
				registryImportQueryNative, registryImportExportNative, registryImportApplyNative = originalQuery, originalExport, originalImport
			})
			test.setup(t, source)
			result, err := RestoreRegistryImport(RestoreAction{
				Type: "registry-import", Source: "payload/settings.reg", Target: `HKCU\Software\Vendor\Failure`, Backup: true,
			}, source, RestoreOptions{ManifestDir: manifestRoot, BackupDir: filepath.Join(context.Root(), "state", "backups"), ValidationContext: context})
			if err != nil || result.Status != "failed" || imported || result.BackupCreated {
				t.Fatalf("%s inspection result = %+v imported=%v err=%v", test.name, result, imported, err)
			}
			assertNoLegacyValidationIdentity(t, context, result.Source, result.Target, result.BackupPath, result.Error)
		})
	}
}

func TestDescribeValidationRegistryImportQueriesMappedTargetAndKeepsSemanticDescriptor(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-regimport-describe")
	semanticKey := `HKCU\Software\Vendor\Describe`
	mappedKey, err := context.MapHKCU(semanticKey)
	if err != nil {
		t.Fatal(err)
	}
	originalQuery := registryImportQueryNative
	defer func() { registryImportQueryNative = originalQuery }()
	var queried []string
	registryImportQueryNative = func(key string) (bool, error) {
		queried = append(queried, key)
		return len(queried) == 1, nil
	}
	action := RestoreAction{Type: "registry-import", Source: "payload/settings.reg", Target: semanticKey}

	exists := DescribeAction(action, RestoreOptions{ValidationContext: context})
	if !exists.TargetExisted || exists.Source != action.Source || exists.Target != semanticKey {
		t.Fatalf("existing descriptor = %+v", exists)
	}
	if len(queried) != 1 || !strings.EqualFold(queried[0], mappedKey) {
		t.Fatalf("queried = %q, want mapped key %q", queried, mappedKey)
	}
	assertNoLegacyValidationIdentity(t, context, exists.Source, exists.Target)

	absent := DescribeAction(action, RestoreOptions{ValidationContext: context})
	if absent.TargetExisted {
		t.Fatalf("absent descriptor = %+v, want targetExisted false", absent)
	}
	if len(queried) != 2 || !strings.EqualFold(queried[1], mappedKey) {
		t.Fatalf("queried = %q, want mapped key %q twice", queried, mappedKey)
	}

	registryImportQueryNative = func(string) (bool, error) {
		t.Fatal("invalid registry target reached native query")
		return false, nil
	}
	invalid := DescribeAction(RestoreAction{Type: "registry-import", Target: `HKLM\Software\Vendor\Describe`}, RestoreOptions{ValidationContext: context})
	if invalid.TargetExisted {
		t.Fatalf("invalid descriptor = %+v, want targetExisted false", invalid)
	}
}

func TestValidationRegistryRejectsUnsafeIdentityBeforeNativeCallbacks(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-regreject")
	originalRead, originalWrite := registrySetReadNative, registrySetWriteNative
	originalQuery, originalExport, originalImport := registryImportQueryNative, registryImportExportNative, registryImportApplyNative
	panicNative := func() { panic("native registry callback reached") }
	registrySetReadNative = func(string, string) (bool, string, string, bool) { panicNative(); return false, "", "", false }
	registrySetWriteNative = func(string, string, string, string) error { panicNative(); return nil }
	registryImportQueryNative = func(string) (bool, error) { panicNative(); return false, nil }
	registryImportExportNative = func(string, string) error { panicNative(); return nil }
	registryImportApplyNative = func(string) error { panicNative(); return nil }
	t.Cleanup(func() {
		registrySetReadNative, registrySetWriteNative = originalRead, originalWrite
		registryImportQueryNative, registryImportExportNative, registryImportApplyNative = originalQuery, originalExport, originalImport
	})

	setResults, err := RunRestore([]RestoreAction{{
		Type: "registry-set", Key: `HKLM\Software\Vendor`, ValueName: "Setting", ValueType: "REG_SZ", Data: "x",
	}}, RestoreOptions{ValidationContext: context}, nil)
	if err != nil || len(setResults) != 1 || setResults[0].Status != "failed" {
		t.Fatalf("unsafe registry-set = %+v, %v", setResults, err)
	}

	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-regreject")
	source := filepath.Join(manifestRoot, "payload", "settings.reg")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	data := "Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Vendor\\Expected]\r\n\"Good\"=\"1\"\r\n\r\n[HKEY_CURRENT_USER\\Software\\Injected]\r\n\"Bad\"=\"1\"\r\n"
	if err := os.WriteFile(source, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	importResults, err := RunRestore([]RestoreAction{{
		Type: "registry-import", Source: "payload/settings.reg", Target: `HKCU\Software\Vendor\Expected`,
	}}, RestoreOptions{ManifestDir: manifestRoot, ValidationContext: context}, nil)
	if err != nil || len(importResults) != 1 || importResults[0].Status != "failed" {
		t.Fatalf("unsafe registry-import = %+v, %v", importResults, err)
	}
	assertNoLegacyValidationIdentity(t, context, setResults[0].Error, importResults[0].Error)
}

func TestValidationRegistrySetRejectsOutsideBackupBeforeNativeCallbacks(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-regset-outside-backup")
	originalRead, originalWrite := registrySetReadNative, registrySetWriteNative
	readCalled := false
	registrySetReadNative = func(string, string) (bool, string, string, bool) {
		readCalled = true
		return false, "", "", false
	}
	registrySetWriteNative = func(string, string, string, string) error {
		panic("registry write callback reached")
	}
	t.Cleanup(func() { registrySetReadNative, registrySetWriteNative = originalRead, originalWrite })
	outsideBackup := filepath.Join(t.TempDir(), "outside-backups")
	result, err := RestoreRegistrySet(RestoreAction{
		Type: "registry-set", Key: `HKCU\Software\Vendor\Example`, ValueName: "Setting", ValueType: "REG_SZ", Data: "desired",
	}, RestoreOptions{BackupDir: outsideBackup, ValidationContext: context})
	if err != nil || result.Status != "failed" {
		t.Fatalf("outside registry backup result = %+v, %v", result, err)
	}
	if !readCalled {
		t.Fatal("registry-set did not classify whether a backup was needed")
	}
	if _, err := os.Stat(outsideBackup); !os.IsNotExist(err) {
		t.Fatalf("outside registry backup directory was mutated: %v", err)
	}
	assertNoLegacyValidationIdentity(t, context, result.Error)
}

func TestValidationRegistrySetDryRunDoesNotAuthorizeUnusedDefaultBackup(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-regset-dry-run")
	originalRead, originalWrite := registrySetReadNative, registrySetWriteNative
	registrySetReadNative = func(string, string) (bool, string, string, bool) {
		return false, "", "", false
	}
	registrySetWriteNative = func(string, string, string, string) error {
		panic("registry write callback reached")
	}
	t.Cleanup(func() { registrySetReadNative, registrySetWriteNative = originalRead, originalWrite })
	result, err := RestoreRegistrySet(RestoreAction{
		Type: "registry-set", Key: `HKCU\Software\Vendor\Example`, ValueName: "Setting", ValueType: "REG_SZ", Data: "desired",
	}, RestoreOptions{DryRun: true, BackupDir: filepath.Join(t.TempDir(), "unused-outside-backup"), ValidationContext: context})
	if err != nil || result.Status != "restored" || result.BackupCreated {
		t.Fatalf("registry-set dry-run result = %+v, %v", result, err)
	}
}

func TestValidationRegistryImportAllowsOutsideBackupWhenUnused(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-regimport-unused-backup")
	outsideBackup := filepath.Join(t.TempDir(), "unused-outside-backups")
	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-regimport-unused-backup")
	if err := os.MkdirAll(filepath.Join(manifestRoot, "payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalQuery, originalImport := registryImportQueryNative, registryImportApplyNative
	registryImportQueryNative = func(string) (bool, error) { return false, nil }
	registryImportApplyNative = func(string) error { panic("dry-run import callback reached") }
	t.Cleanup(func() { registryImportQueryNative, registryImportApplyNative = originalQuery, originalImport })

	optional, err := RunRestore([]RestoreAction{{
		Type: "registry-import", Source: "payload/missing.reg", Target: `HKCU\Software\Vendor\Optional`, Backup: true, Optional: true,
	}}, RestoreOptions{ManifestDir: manifestRoot, BackupDir: outsideBackup, ValidationContext: context}, nil)
	if err != nil || len(optional) != 1 || optional[0].Status != "skipped_missing_source" {
		t.Fatalf("optional registry import = %+v, %v", optional, err)
	}
	source := filepath.Join(manifestRoot, "payload", "settings.reg")
	if err := os.WriteFile(source, validationRegistryDocument(`HKCU\Software\Vendor\Absent`, "desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	absent, err := RunRestore([]RestoreAction{{
		Type: "registry-import", Source: "payload/settings.reg", Target: `HKCU\Software\Vendor\Absent`, Backup: true,
	}}, RestoreOptions{ManifestDir: manifestRoot, BackupDir: outsideBackup, DryRun: true, ValidationContext: context}, nil)
	if err != nil || len(absent) != 1 || absent[0].Status != "restored" || absent[0].BackupCreated {
		t.Fatalf("absent registry import = %+v, %v", absent, err)
	}
	if _, err := os.Stat(outsideBackup); !os.IsNotExist(err) {
		t.Fatalf("unused outside registry backup was mutated: %v", err)
	}
}

func TestValidationDurableRegistrySetRevertMapsOnlyAtNativeBoundary(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-durable-regset")
	semanticKey := `HKCU\Software\Vendor\DurableSet`
	mappedKey, err := context.MapHKCU(semanticKey)
	if err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(context.Root(), "state", "backups", "legacy-durable-regset")
	physicalBackup, err := writeRegistrySetBackup(registrySetBackup{
		Key: semanticKey, ValueName: "Setting", Existed: true, PriorType: "REG_SZ", PriorData: "prior",
	}, RestoreOptions{BackupDir: backupDir})
	if err != nil {
		t.Fatal(err)
	}
	semanticBackup, err := context.DisplayPath(physicalBackup)
	if err != nil {
		t.Fatal(err)
	}

	originalRead, originalWrite, originalDelete := registrySetReadNative, registrySetWriteNative, registrySetDeleteNative
	current := "restored"
	registrySetReadNative = func(key, name string) (bool, string, string, bool) {
		if !strings.EqualFold(key, mappedKey) || name != "Setting" {
			t.Fatalf("native registry read = %q %q", key, name)
		}
		return true, "REG_SZ", current, true
	}
	registrySetWriteNative = func(key, name, valueType, data string) error {
		if !strings.EqualFold(key, mappedKey) || name != "Setting" || valueType != "REG_SZ" {
			t.Fatalf("native registry write = %q %q %q", key, name, valueType)
		}
		current = data
		return nil
	}
	registrySetDeleteNative = func(string, string) error { t.Fatal("unexpected registry delete"); return nil }
	t.Cleanup(func() {
		registrySetReadNative, registrySetWriteNative, registrySetDeleteNative = originalRead, originalWrite, originalDelete
	})
	workRoot := filepath.Join(context.Root(), "state", "legacy-revert", "durable-regset")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: semanticKey + `\Setting`, TargetExistedBefore: true, BackupCreated: true,
		BackupPath: semanticBackup, Action: "restored", RestoreType: "registry-set",
	}}}
	results, err := RunRevertDurableWithValidation(journal, backupDir, workRoot, context)
	if err != nil || len(results) != 1 || results[0].Action != "reverted" || current != "prior" {
		t.Fatalf("durable registry-set revert = %+v current=%q err=%v", results, current, err)
	}
	prepared, err := os.ReadFile(filepath.Join(workRoot, "entry-000000.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertNoLegacyValidationIdentity(t, context, string(prepared), results[0].Target, results[0].BackupUsed)
}

func TestValidationDurableRegistryImportCrashRecoveryKeepsScratchSemantic(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-durable-regimport")
	semanticKey := `HKCU\Software\Vendor\DurableImport`
	mappedKey, err := context.MapHKCU(semanticKey)
	if err != nil {
		t.Fatal(err)
	}
	semanticCurrent := validationRegistryDocument(semanticKey, "restored")
	semanticPrior := validationRegistryDocument(semanticKey, "prior")
	physicalCurrent, err := registryfile.RewriteSubtree(semanticCurrent, semanticKey, mappedKey)
	if err != nil {
		t.Fatal(err)
	}
	registryState := map[string][]byte{strings.ToLower(mappedKey): physicalCurrent}

	originalExists, originalExport := durableRegistryKeyExistsNative, durableRegistryExportNative
	originalDelete, originalRename, originalImport := durableRegistryDeleteNative, durableRegistryRenameNative, durableRegistryImportFile
	durableRegistryKeyExistsNative = func(key string) (bool, error) {
		_, ok := registryState[strings.ToLower(key)]
		return ok, nil
	}
	durableRegistryExportNative = func(key, path string) error {
		data, ok := registryState[strings.ToLower(key)]
		if !ok {
			return fmt.Errorf("missing registry key")
		}
		return os.WriteFile(path, data, 0o600)
	}
	durableRegistryDeleteNative = func(key string) error {
		delete(registryState, strings.ToLower(key))
		return nil
	}
	durableRegistryRenameNative = func(source, destination string) error {
		data, ok := registryState[strings.ToLower(source)]
		if !ok {
			return fmt.Errorf("missing rename source")
		}
		rewritten, err := registryfile.RewriteSubtree(data, source, destination)
		if err != nil {
			return err
		}
		registryState[strings.ToLower(destination)] = rewritten
		delete(registryState, strings.ToLower(source))
		return nil
	}
	durableRegistryImportFile = func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		root, err := validationRegistryDocumentRoot(data)
		if err != nil {
			return err
		}
		registryState[strings.ToLower(root)] = append([]byte(nil), data...)
		return nil
	}
	t.Cleanup(func() {
		durableRegistryKeyExistsNative, durableRegistryExportNative = originalExists, originalExport
		durableRegistryDeleteNative, durableRegistryRenameNative, durableRegistryImportFile = originalDelete, originalRename, originalImport
	})

	backupDir := filepath.Join(context.Root(), "state", "backups", "legacy-durable-regimport")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	physicalBackup := filepath.Join(backupDir, "prior.reg")
	if err := os.WriteFile(physicalBackup, semanticPrior, 0o600); err != nil {
		t.Fatal(err)
	}
	semanticBackup, err := context.DisplayPath(physicalBackup)
	if err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: semanticKey, TargetExistedBefore: true, BackupCreated: true,
		BackupPath: semanticBackup, Action: "restored", RestoreType: "registry-import",
	}}}
	workRoot := filepath.Join(context.Root(), "state", "legacy-revert", "durable-regimport")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	originalCheckpoint := durableRevertCheckpoint
	fired := false
	durableRevertCheckpoint = func(phase string, _ int) error {
		if phase == "after_registry_target_held" && !fired {
			fired = true
			return errors.New("simulated validation registry crash")
		}
		return nil
	}
	t.Cleanup(func() { durableRevertCheckpoint = originalCheckpoint })
	if _, err := RunRevertDurableWithValidation(journal, backupDir, workRoot, context); err == nil {
		t.Fatal("durable registry crash checkpoint did not fire")
	}
	prepared, err := os.ReadFile(filepath.Join(workRoot, "entry-000000.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertNoLegacyValidationIdentity(t, context, string(prepared))
	var preparedRecord durableLegacyRevertPrepared
	if err := json.Unmarshal(prepared, &preparedRecord); err != nil {
		t.Fatal(err)
	}
	if preparedRecord.Target != semanticKey || !strings.Contains(preparedRecord.StagePath, ".endstate-revert-") ||
		!strings.HasPrefix(preparedRecord.StagePath, `HKCU\Software\Vendor\`) {
		t.Fatalf("prepared registry identities are not semantic: %s", prepared)
	}
	durableRevertCheckpoint = originalCheckpoint
	results, err := RunRevertDurableWithValidation(journal, backupDir, workRoot, context)
	if err != nil || len(results) != 1 || results[0].Action != "reverted" {
		t.Fatalf("durable registry recovery = %+v, %v", results, err)
	}
	final, ok := registryState[strings.ToLower(mappedKey)]
	if !ok {
		t.Fatal("mapped registry target was not restored")
	}
	semanticFinal, err := registryfile.RewriteSubtree(final, mappedKey, semanticKey)
	if err != nil {
		t.Fatal(err)
	}
	text, err := decodeRegistryImport(semanticFinal)
	if err != nil || !strings.Contains(text, `"Setting"="prior"`) {
		t.Fatalf("final registry state = %q, %v", text, err)
	}
	assertNoLegacyValidationIdentity(t, context, results[0].Target, results[0].BackupUsed)
}

func TestValidationDurableRegistryRejectsHKLMBeforeNativeCallbacks(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-durable-regreject")
	originalExists, originalExport := durableRegistryKeyExistsNative, durableRegistryExportNative
	originalDelete, originalRename, originalImport := durableRegistryDeleteNative, durableRegistryRenameNative, durableRegistryImportFile
	panicNative := func() { panic("durable native registry callback reached") }
	durableRegistryKeyExistsNative = func(string) (bool, error) { panicNative(); return false, nil }
	durableRegistryExportNative = func(string, string) error { panicNative(); return nil }
	durableRegistryDeleteNative = func(string) error { panicNative(); return nil }
	durableRegistryRenameNative = func(string, string) error { panicNative(); return nil }
	durableRegistryImportFile = func(string) error { panicNative(); return nil }
	t.Cleanup(func() {
		durableRegistryKeyExistsNative, durableRegistryExportNative = originalExists, originalExport
		durableRegistryDeleteNative, durableRegistryRenameNative, durableRegistryImportFile = originalDelete, originalRename, originalImport
	})
	workRoot := filepath.Join(context.Root(), "state", "legacy-revert", "durable-regreject")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: `HKLM\Software\Vendor\Injected`, TargetExistedBefore: false,
		Action: "restored", RestoreType: "registry-import",
	}}}
	if _, err := RunRevertDurableWithValidation(journal, "", workRoot, context); err == nil {
		t.Fatal("tampered HKLM durable journal was accepted")
	}
	entries, err := os.ReadDir(workRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("tampered durable registry journal wrote records: %v", entries)
	}
}

func validationRegistryDocument(key, value string) []byte {
	return []byte("Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER" + key[len("HKCU"):] + "]\r\n\"Setting\"=\"" + value + "\"\r\n")
}

func validationRegistryDocumentRoot(data []byte) (string, error) {
	text, err := decodeRegistryImport(data)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			return validationmode.NormalizeHKCU(line[1 : len(line)-1])
		}
	}
	return "", fmt.Errorf("registry document has no root")
}
