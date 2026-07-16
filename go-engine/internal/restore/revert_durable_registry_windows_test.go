// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package restore

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func setupDurableRegistryImport(t *testing.T) (string, string, string, *Journal) {
	t.Helper()
	subkey := fmt.Sprintf(`Software\EndstateTest\DurableRevert\%d`, time.Now().UnixNano())
	target := `HKCU\` + subkey
	t.Cleanup(func() { _ = exec.Command("reg", "delete", target, "/f").Run() })

	key, _, err := registry.CreateKey(registry.CURRENT_USER, subkey, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.SetStringValue("Existing", "prior"); err != nil {
		_ = key.Close()
		t.Fatal(err)
	}
	_ = key.Close()
	child, _, err := registry.CreateKey(registry.CURRENT_USER, subkey+`\PriorChild`, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	if err := child.SetStringValue("Kept", "prior-child"); err != nil {
		_ = child.Close()
		t.Fatal(err)
	}
	_ = child.Close()

	backup := filepath.Join(t.TempDir(), "prior.reg")
	if output, err := exec.Command("reg", "export", target, backup, "/y").CombinedOutput(); err != nil {
		t.Fatalf("export backup: %v: %s", err, output)
	}

	key, err = registry.OpenKey(registry.CURRENT_USER, subkey, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.SetStringValue("Existing", "restored"); err != nil {
		_ = key.Close()
		t.Fatal(err)
	}
	if err := key.SetStringValue("Introduced", "remove-me"); err != nil {
		_ = key.Close()
		t.Fatal(err)
	}
	_ = key.Close()
	introduced, _, err := registry.CreateKey(registry.CURRENT_USER, subkey+`\IntroducedChild`, registry.ALL_ACCESS)
	if err != nil {
		t.Fatal(err)
	}
	_ = introduced.Close()

	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: target, TargetExistedBefore: true, BackupCreated: true,
		BackupPath: backup, Action: "restored", RestoreType: "registry-import",
	}}}
	return subkey, target, backup, journal
}

func TestRunRevertDurableRegistryImportReplacesKeyExactly(t *testing.T) {
	subkey, _, backup, journal := setupDurableRegistryImport(t)
	results, err := RunRevertDurable(journal, "", t.TempDir())
	if err != nil || len(results) != 1 || results[0].Action != "reverted" {
		t.Fatalf("registry results = %+v, %v", results, err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	existing, _, err := key.GetStringValue("Existing")
	if err != nil || existing != "prior" {
		_ = key.Close()
		t.Fatalf("restored existing value = %q, %v", existing, err)
	}
	if _, _, err := key.GetStringValue("Introduced"); err != registry.ErrNotExist {
		_ = key.Close()
		t.Fatalf("introduced value survived exact revert: %v", err)
	}
	_ = key.Close()
	if child, err := registry.OpenKey(registry.CURRENT_USER, subkey+`\IntroducedChild`, registry.QUERY_VALUE); err != registry.ErrNotExist {
		if err == nil {
			_ = child.Close()
		}
		t.Fatalf("introduced child survived exact revert: %v", err)
	}
	priorChild, err := registry.OpenKey(registry.CURRENT_USER, subkey+`\PriorChild`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer priorChild.Close()
	value, _, err := priorChild.GetStringValue("Kept")
	if err != nil || value != "prior-child" {
		t.Fatalf("prior child value = %q, %v", value, err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("verified backup disappeared: %v", err)
	}
}

func TestRunRevertDurableRegistryImportResumesHeldKeySwap(t *testing.T) {
	subkey, _, _, journal := setupDurableRegistryImport(t)
	originalCheckpoint := durableRevertCheckpoint
	fired := false
	durableRevertCheckpoint = func(phase string, _ int) error {
		if phase == "after_registry_target_held" && !fired {
			fired = true
			return errors.New("simulated registry delete/import crash")
		}
		return nil
	}
	t.Cleanup(func() { durableRevertCheckpoint = originalCheckpoint })

	workRoot := t.TempDir()
	if _, err := RunRevertDurable(journal, "", workRoot); err == nil || !strings.Contains(err.Error(), "simulated") {
		t.Fatalf("first registry revert error = %v", err)
	}
	if key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE); err != registry.ErrNotExist {
		if err == nil {
			_ = key.Close()
		}
		t.Fatalf("registry key after delete checkpoint = %v", err)
	}

	durableRevertCheckpoint = originalCheckpoint
	results, err := RunRevertDurable(journal, "", workRoot)
	if err != nil || len(results) != 1 || results[0].Action != "reverted" {
		t.Fatalf("registry swap retry = %+v, %v", results, err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	value, _, err := key.GetStringValue("Existing")
	if err != nil || value != "prior" {
		t.Fatalf("registry swap retry value = %q, %v", value, err)
	}
}

func TestRunRevertDurableRegistryImportFailureLeavesTargetUntouched(t *testing.T) {
	subkey, _, _, journal := setupDurableRegistryImport(t)
	originalImport := durableRegistryImportFile
	durableRegistryImportFile = func(string) error { return errors.New("simulated staging import failure") }
	t.Cleanup(func() { durableRegistryImportFile = originalImport })
	workRoot := t.TempDir()
	if _, err := RunRevertDurable(journal, "", workRoot); err == nil || !strings.Contains(err.Error(), "simulated") {
		t.Fatalf("staging import error = %v", err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	value, _, valueErr := key.GetStringValue("Existing")
	introduced, _, introducedErr := key.GetStringValue("Introduced")
	_ = key.Close()
	if valueErr != nil || value != "restored" || introducedErr != nil || introduced != "remove-me" {
		t.Fatalf("failed staging import changed target: existing=%q/%v introduced=%q/%v", value, valueErr, introduced, introducedErr)
	}

	durableRegistryImportFile = originalImport
	if _, err := RunRevertDurable(journal, "", workRoot); err != nil {
		t.Fatalf("retry after staging import failure = %v", err)
	}
}

func TestRunRevertDurableRegistryImportRejectsUserDeleteAfterReplacement(t *testing.T) {
	subkey, target, _, journal := setupDurableRegistryImport(t)
	originalCheckpoint := durableRevertCheckpoint
	durableRevertCheckpoint = func(phase string, _ int) error {
		if phase == "after_target_replaced" {
			return errors.New("simulated crash after registry replacement")
		}
		return nil
	}
	t.Cleanup(func() { durableRevertCheckpoint = originalCheckpoint })
	workRoot := t.TempDir()
	if _, err := RunRevertDurable(journal, "", workRoot); err == nil {
		t.Fatal("registry replacement unexpectedly completed")
	}
	if err := exec.Command("reg", "delete", target, "/f").Run(); err != nil {
		t.Fatal(err)
	}
	durableRevertCheckpoint = originalCheckpoint
	if _, err := RunRevertDurable(journal, "", workRoot); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("post-replacement user delete retry = %v", err)
	}
	if key, err := registry.OpenKey(registry.CURRENT_USER, subkey, registry.QUERY_VALUE); err != registry.ErrNotExist {
		if err == nil {
			_ = key.Close()
		}
		t.Fatalf("retry overwrote post-replacement user delete: %v", err)
	}
}
