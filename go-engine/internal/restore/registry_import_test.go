// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"unicode/utf16"
)

func TestReserveRegistryImportBackupUsesUniqueImmutableActionDirectories(t *testing.T) {
	backupDir := filepath.Join(t.TempDir(), "backups", "fixed-run")
	key := `HKCU\Software\Vendor\Example`
	first, cleanupFirst, err := reserveRegistryImportBackup(legacyValidationBoundary{}, backupDir, key)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupFirst()
	if err := os.WriteFile(first, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, cleanupSecond, err := reserveRegistryImportBackup(legacyValidationBoundary{}, backupDir, key)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupSecond()
	if first == second || filepath.Base(first) != "prior.reg" || filepath.Base(second) != "prior.reg" {
		t.Fatalf("backup paths first=%q second=%q", first, second)
	}
	if err := os.WriteFile(second, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(first); err != nil || string(data) != "first" {
		t.Fatalf("first backup changed = %q, %v", data, err)
	}
}

func TestReserveRegistryImportBackupIsConcurrentAndKeyCollisionSafe(t *testing.T) {
	backupDir := filepath.Join(t.TempDir(), "backups", "fixed-run")
	key := `HKCU\Software\Vendor\Concurrent`
	const count = 16
	paths := make(chan string, count)
	errs := make(chan error, count)
	var group sync.WaitGroup
	for range count {
		group.Add(1)
		go func() {
			defer group.Done()
			path, _, err := reserveRegistryImportBackup(legacyValidationBoundary{}, backupDir, key)
			if err != nil {
				errs <- err
				return
			}
			paths <- path
		}()
	}
	group.Wait()
	close(paths)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for path := range paths {
		if seen[path] {
			t.Fatalf("duplicate concurrent reservation %q", path)
		}
		seen[path] = true
	}
	if len(seen) != count {
		t.Fatalf("reserved %d paths, want %d", len(seen), count)
	}
	first, cleanupFirst, err := reserveRegistryImportBackup(legacyValidationBoundary{}, backupDir, `HKCU\Software\Vendor\A B`)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupFirst()
	second, cleanupSecond, err := reserveRegistryImportBackup(legacyValidationBoundary{}, backupDir, `HKCU\Software\Vendor\A_B`)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanupSecond()
	if filepath.Dir(filepath.Dir(first)) == filepath.Dir(filepath.Dir(second)) {
		t.Fatalf("sanitizer-colliding keys shared backup directory: %q and %q", first, second)
	}
}

func TestRestoreRegistryImportBackupReservationsDoNotCollide(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	root := t.TempDir()
	source := filepath.Join(root, "settings.reg")
	if err := os.WriteFile(source, []byte("Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Vendor\\Example]\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalQuery, originalExport, originalImport := registryImportQueryNative, registryImportExportNative, registryImportApplyNative
	exports := 0
	registryImportQueryNative = func(string) (bool, error) { return true, nil }
	registryImportExportNative = func(_ string, path string) error {
		exports++
		return os.WriteFile(path, []byte(fmt.Sprintf("backup-%d", exports)), 0o600)
	}
	registryImportApplyNative = func(string) error { return nil }
	t.Cleanup(func() {
		registryImportQueryNative, registryImportExportNative, registryImportApplyNative = originalQuery, originalExport, originalImport
	})
	action := RestoreAction{Type: "registry-import", Source: source, Target: `HKCU\Software\Vendor\Example`, Backup: true}
	options := RestoreOptions{BackupDir: filepath.Join(root, "backups"), RunID: "fixed-run"}
	first, err := RestoreRegistryImport(action, source, options)
	if err != nil || first.Status != "restored" {
		t.Fatalf("first restore = %+v, %v", first, err)
	}
	second, err := RestoreRegistryImport(action, source, options)
	if err != nil || second.Status != "restored" {
		t.Fatalf("second restore = %+v, %v", second, err)
	}
	if first.BackupPath == second.BackupPath {
		t.Fatalf("backup paths collided: %q", first.BackupPath)
	}
	if data, err := os.ReadFile(first.BackupPath); err != nil || string(data) != "backup-1" {
		t.Fatalf("first backup = %q, %v", data, err)
	}
	if data, err := os.ReadFile(second.BackupPath); err != nil || string(data) != "backup-2" {
		t.Fatalf("second backup = %q, %v", data, err)
	}
}

func TestRestoreRegistryImportCleansReservedBackupOnExportFailure(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	root := t.TempDir()
	source := filepath.Join(root, "settings.reg")
	if err := os.WriteFile(source, []byte("Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Vendor\\Cleanup]\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalQuery, originalExport, originalImport := registryImportQueryNative, registryImportExportNative, registryImportApplyNative
	registryImportQueryNative = func(string) (bool, error) { return true, nil }
	registryImportExportNative = func(_ string, path string) error {
		if err := os.WriteFile(path, []byte("partial"), 0o600); err != nil {
			return err
		}
		return fmt.Errorf("export failed")
	}
	registryImportApplyNative = func(string) error { t.Fatal("export failure imported registry"); return nil }
	t.Cleanup(func() {
		registryImportQueryNative, registryImportExportNative, registryImportApplyNative = originalQuery, originalExport, originalImport
	})
	backupDir := filepath.Join(root, "backups")
	result, err := RestoreRegistryImport(RestoreAction{
		Type: "registry-import", Source: source, Target: `HKCU\Software\Vendor\Cleanup`, Backup: true,
	}, source, RestoreOptions{BackupDir: backupDir})
	if err != nil || result.Status != "failed" || result.BackupCreated || result.BackupPath != "" {
		t.Fatalf("export failure result = %+v, %v", result, err)
	}
	hashes, err := os.ReadDir(backupDir)
	if err != nil || len(hashes) != 1 {
		t.Fatalf("backup key directories = %v, %v", hashes, err)
	}
	actions, err := os.ReadDir(filepath.Join(backupDir, hashes[0].Name()))
	if err != nil || len(actions) != 0 {
		t.Fatalf("reserved action directory survived export failure: %v, %v", actions, err)
	}
}

func TestRestoreRegistryImportFailsClosedOnOrdinaryQueryError(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	root := t.TempDir()
	source := filepath.Join(root, "settings.reg")
	if err := os.WriteFile(source, []byte("Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Vendor\\QueryError]\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalQuery, originalExport, originalImport := registryImportQueryNative, registryImportExportNative, registryImportApplyNative
	exported, imported := false, false
	registryImportQueryNative = func(string) (bool, error) { return false, fmt.Errorf("registry access denied") }
	registryImportExportNative = func(string, string) error { exported = true; return nil }
	registryImportApplyNative = func(string) error { imported = true; return nil }
	t.Cleanup(func() {
		registryImportQueryNative, registryImportExportNative, registryImportApplyNative = originalQuery, originalExport, originalImport
	})
	result, err := RestoreRegistryImport(RestoreAction{
		Type: "registry-import", Source: source, Target: `HKCU\Software\Vendor\QueryError`, Backup: true,
	}, source, RestoreOptions{BackupDir: filepath.Join(root, "backups")})
	if err != nil || result.Status != "failed" || result.BackupCreated || result.BackupPath != "" || exported || imported {
		t.Fatalf("query failure result = %+v exported=%v imported=%v err=%v", result, exported, imported, err)
	}
}

func TestRestoreRegistryImportOrdinaryColonTargetBacksUpAndImports(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	root := t.TempDir()
	source := filepath.Join(root, "settings.reg")
	target := `HKCU\Software\Vendor:Name`
	if err := os.WriteFile(source, []byte("Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Vendor:Name]\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	originalQuery, originalExport, originalImport := registryImportQueryNative, registryImportExportNative, registryImportApplyNative
	exported, imported := false, false
	registryImportQueryNative = func(key string) (bool, error) {
		if key != target {
			t.Fatalf("query target = %q, want %q", key, target)
		}
		return true, nil
	}
	registryImportExportNative = func(key, path string) error {
		if key != target {
			t.Fatalf("export target = %q, want %q", key, target)
		}
		exported = true
		return os.WriteFile(path, []byte("backup"), 0o600)
	}
	registryImportApplyNative = func(path string) error {
		if path != source {
			t.Fatalf("import source = %q, want %q", path, source)
		}
		imported = true
		return nil
	}
	t.Cleanup(func() {
		registryImportQueryNative, registryImportExportNative, registryImportApplyNative = originalQuery, originalExport, originalImport
	})
	result, err := RestoreRegistryImport(RestoreAction{
		Type: "registry-import", Source: source, Target: target, Backup: true,
	}, source, RestoreOptions{BackupDir: filepath.Join(root, "backups")})
	if err != nil || result.Status != "restored" || !result.BackupCreated || result.BackupPath == "" || !exported || !imported {
		t.Fatalf("colon target result = %+v exported=%v imported=%v err=%v", result, exported, imported, err)
	}
}

func TestValidateRegistryImportScopeAcceptsOnlyDeclaredHKCUSubtree(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr string
	}{
		{
			name: "target and descendants",
			content: "Windows Registry Editor Version 5.00\r\n\r\n" +
				"[HKEY_CURRENT_USER\\Software\\Vendor]\r\n\"Root\"=\"ok\"\r\n\r\n" +
				"[-HKCU\\Software\\Vendor\\Old]\r\n",
		},
		{
			name:    "sibling",
			content: "Windows Registry Editor Version 5.00\n\n[HKEY_CURRENT_USER\\Software\\Other]\n",
			wantErr: "outside declared target",
		},
		{
			name:    "different hive",
			content: "Windows Registry Editor Version 5.00\n\n[HKEY_LOCAL_MACHINE\\Software\\Vendor]\n",
			wantErr: "only supports HKCU",
		},
		{
			name:    "no key sections",
			content: "Windows Registry Editor Version 5.00\n\n\"Value\"=\"orphaned\"\n",
			wantErr: "no registry key sections",
		},
		{
			name:    "malformed section",
			content: "Windows Registry Editor Version 5.00\n\n[HKEY_CURRENT_USER\\Software\\Vendor\n",
			wantErr: "malformed registry key section",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "settings.reg")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			err := ValidateRegistryImportScope(path, `HKCU\Software\Vendor`)
			if test.wantErr == "" && err != nil {
				t.Fatalf("ValidateRegistryImportScope() error = %v", err)
			}
			if test.wantErr != "" && (err == nil || !strings.Contains(err.Error(), test.wantErr)) {
				t.Fatalf("ValidateRegistryImportScope() error = %v, want %q", err, test.wantErr)
			}
		})
	}
}

func TestValidateRegistryImportScopeAcceptsUTF16LERegistryExport(t *testing.T) {
	content := "Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\Vendor\\Child]\r\n"
	encoded := utf16.Encode([]rune(content))
	data := make([]byte, 2+len(encoded)*2)
	data[0], data[1] = 0xff, 0xfe
	for index, value := range encoded {
		binary.LittleEndian.PutUint16(data[2+index*2:], value)
	}
	path := filepath.Join(t.TempDir(), "settings.reg")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRegistryImportScope(path, `HKEY_CURRENT_USER\Software\Vendor`); err != nil {
		t.Fatalf("ValidateRegistryImportScope() error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// ValidateRegistryTarget / isHKCUKey tests (platform-independent)
// ---------------------------------------------------------------------------

func TestIsHKCUKey(t *testing.T) {
	cases := []struct {
		target string
		want   bool
	}{
		{`HKCU\Software\Test`, true},
		{`hkcu\Software\Test`, true},
		{`HKEY_CURRENT_USER\Software\Test`, true},
		{`hkey_current_user\Software\Test`, true},
		{`HKLM\Software\Test`, false},
		{`HKEY_LOCAL_MACHINE\Software\Test`, false},
		{`HKCR\Software\Test`, false},
		{`HKEY_CLASSES_ROOT\Software\Test`, false},
		{`HKU\Software\Test`, false},
		{``, false},
	}

	for _, tc := range cases {
		got := isHKCUKey(tc.target)
		if got != tc.want {
			t.Errorf("isHKCUKey(%q) = %v, want %v", tc.target, got, tc.want)
		}
	}
}

func TestValidateRegistryTarget_HKCUAccepted(t *testing.T) {
	if err := ValidateRegistryTarget(`HKCU\Software\Test`); err != nil {
		t.Errorf("expected nil error for HKCU key, got: %v", err)
	}
}

func TestValidateRegistryTarget_HKEYCurrentUserAccepted(t *testing.T) {
	if err := ValidateRegistryTarget(`HKEY_CURRENT_USER\Software\Test`); err != nil {
		t.Errorf("expected nil error for HKEY_CURRENT_USER key, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// HKCU validation rejection tests (platform-independent because validation
// runs before the GOOS guard)
// ---------------------------------------------------------------------------

func TestRestoreRegistryImport_HKLMRejected(t *testing.T) {
	entry := RestoreAction{
		Type:   "registry-import",
		Source: "nonexistent.reg",
		Target: `HKLM\Software\Test`,
	}
	result, err := RestoreRegistryImport(entry, "nonexistent.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "only supports HKCU") {
		t.Errorf("expected error to contain 'only supports HKCU', got: %q", result.Error)
	}
}

func TestRestoreRegistryImport_HKCRRejected(t *testing.T) {
	entry := RestoreAction{
		Type:   "registry-import",
		Source: "nonexistent.reg",
		Target: `HKCR\Software\Test`,
	}
	result, err := RestoreRegistryImport(entry, "nonexistent.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "only supports HKCU") {
		t.Errorf("expected error to contain 'only supports HKCU', got: %q", result.Error)
	}
}

func TestRestoreRegistryImport_HKEYLocalMachineRejected(t *testing.T) {
	entry := RestoreAction{
		Type:   "registry-import",
		Source: "nonexistent.reg",
		Target: `HKEY_LOCAL_MACHINE\Software\Test`,
	}
	result, err := RestoreRegistryImport(entry, "nonexistent.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "only supports HKCU") {
		t.Errorf("expected error to contain 'only supports HKCU', got: %q", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Tests that require passing the GOOS check — Windows-only beyond this point,
// or non-Windows tests that exercise pre-GOOS-check logic only.
// ---------------------------------------------------------------------------

func TestRestoreRegistryImport_OptionalMissingSource(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	entry := RestoreAction{
		Type:     "registry-import",
		Source:   "definitely_does_not_exist_12345.reg",
		Target:   `HKCU\Software\EndstateTest\Missing`,
		Optional: true,
	}
	result, err := RestoreRegistryImport(entry, "definitely_does_not_exist_12345.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "skipped_missing_source" {
		t.Errorf("expected status=skipped_missing_source, got %q", result.Status)
	}
}

func TestRestoreRegistryImport_NonExistentSourceFails(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}
	entry := RestoreAction{
		Type:     "registry-import",
		Source:   "definitely_does_not_exist_12345.reg",
		Target:   `HKCU\Software\EndstateTest\Missing`,
		Optional: false,
	}
	result, err := RestoreRegistryImport(entry, "definitely_does_not_exist_12345.reg", RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(result.Error, "source not found") {
		t.Errorf("expected error to contain 'source not found', got: %q", result.Error)
	}
}

func TestRestoreRegistryImport_DryRun(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("registry operations require Windows")
	}

	tmp := t.TempDir()
	regFile := filepath.Join(tmp, "test.reg")

	// Write a minimal valid .reg file.
	regContent := "Windows Registry Editor Version 5.00\r\n\r\n[HKEY_CURRENT_USER\\Software\\EndstateTest\\DryRun]\r\n"
	if err := os.WriteFile(regFile, []byte(regContent), 0644); err != nil {
		t.Fatalf("failed to create temp reg file: %v", err)
	}

	entry := RestoreAction{
		Type:   "registry-import",
		Source: regFile,
		Target: `HKCU\Software\EndstateTest\DryRun`,
	}

	result, err := RestoreRegistryImport(entry, regFile, RestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error return: %v", err)
	}
	if result.Status != "restored" {
		t.Errorf("expected status=restored for dry-run, got %q", result.Status)
	}
}
