// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestValidationRestoreCopyJournalAndDurableRevertStaySemantic(t *testing.T) {
	context, originalAppData := activeLegacyRestoreValidationContext(t, "legacy-file")
	authoredTarget := `%APPDATA%\Vendor\settings.json`
	physicalTarget, err := context.ResolveHostPath(authoredTarget, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(physicalTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(physicalTarget, []byte("sandbox-prior\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	originalTarget := filepath.Join(originalAppData, "Vendor", "settings.json")
	if err := os.MkdirAll(filepath.Dir(originalTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalTarget, []byte("original-host\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-file")
	source := filepath.Join(manifestRoot, "payload", "settings.json")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("captured\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backupDir := filepath.Join(context.Root(), "state", "backups", "legacy-file-run")
	opts := RestoreOptions{
		ManifestDir:       manifestRoot,
		BackupDir:         backupDir,
		RunID:             "legacy-file-run",
		ValidationContext: context,
	}
	results, err := RunRestore([]RestoreAction{{
		Type: "copy", Source: "payload/settings.json", Target: authoredTarget, Backup: true,
	}}, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "restored" {
		t.Fatalf("restore results = %+v", results)
	}
	if results[0].Source != "payload/settings.json" || results[0].Target != authoredTarget {
		t.Fatalf("semantic result paths = source %q target %q", results[0].Source, results[0].Target)
	}
	if results[0].BackupPath == "" || !strings.HasPrefix(results[0].BackupPath, "$ENDSTATE_ROOT/") {
		t.Fatalf("semantic backup path = %q", results[0].BackupPath)
	}
	assertNoLegacyValidationIdentity(t, context, results[0].Source, results[0].Target, results[0].BackupPath, results[0].Error)
	assertFileBytes(t, physicalTarget, []byte("captured\n"))
	assertFileBytes(t, originalTarget, []byte("original-host\n"))

	logsDir := filepath.Join(context.Root(), "logs")
	if err := WriteJournalWithValidation(logsDir, opts.RunID, "manifest.jsonc", manifestRoot, "", results, context); err != nil {
		t.Fatal(err)
	}
	journalPath := filepath.Join(logsDir, "restore-journal-"+opts.RunID+".json")
	journalBytes, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	assertNoLegacyValidationIdentity(t, context, string(journalBytes))
	journal, err := ParseJournal(journalBytes)
	if err != nil {
		t.Fatal(err)
	}
	workRoot := filepath.Join(context.Root(), "state", "legacy-revert", opts.RunID)
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	reverted, err := RunRevertDurableWithValidation(journal, backupDir, workRoot, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(reverted) != 1 || reverted[0].Target != authoredTarget || reverted[0].Action != "reverted" {
		t.Fatalf("revert results = %+v", reverted)
	}
	assertNoLegacyValidationIdentity(t, context, reverted[0].Target, reverted[0].BackupUsed)
	assertFileBytes(t, physicalTarget, []byte("sandbox-prior\n"))
	assertFileBytes(t, originalTarget, []byte("original-host\n"))

	prepared, err := os.ReadFile(filepath.Join(workRoot, "entry-000000.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertNoLegacyValidationIdentity(t, context, string(prepared))
}

func TestValidationDurableRevertRejectsOutsideJournalTargetBeforeMutation(t *testing.T) {
	context, originalAppData := activeLegacyRestoreValidationContext(t, "legacy-tamper")
	originalTarget := filepath.Join(originalAppData, "Vendor", "outside.txt")
	if err := os.MkdirAll(filepath.Dir(originalTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originalTarget, []byte("protected\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	journal := &Journal{Entries: []JournalEntry{{
		TargetPath: originalTarget, Action: "restored", RestoreType: "copy", TargetExistedBefore: false,
	}}}
	workRoot := filepath.Join(context.Root(), "state", "legacy-revert", "tampered")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RunRevertDurableWithValidation(journal, "", workRoot, context); err == nil {
		t.Fatal("tampered outside target unexpectedly reached durable revert")
	}
	assertFileBytes(t, originalTarget, []byte("protected\n"))
	entries, err := os.ReadDir(workRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("tampered journal wrote durable records: %v", entries)
	}
}

func TestValidationLegacyFilesystemStrategiesUseContainedSourcesAndSemanticTargets(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-strategies")
	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-strategies")
	backupDir := filepath.Join(context.Root(), "state", "backups", "legacy-strategies-run")
	write := func(relative, content string) {
		t.Helper()
		path := filepath.Join(manifestRoot, filepath.FromSlash(relative))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("payload/dir/one.txt", "one\n")
	write("payload/settings.json", `{"new":2}`)
	write("payload/settings.ini", "[section]\nnew=2\n")
	write("payload/lines.txt", "beta\n")

	targets := []string{
		`%APPDATA%\Vendor\directory`,
		`%APPDATA%\Vendor\settings.json`,
		`%APPDATA%\Vendor\settings.ini`,
		`%APPDATA%\Vendor\lines.txt`,
		`%APPDATA%\Vendor\cache`,
	}
	seed := map[int]string{1: `{"keep":1}`, 2: "[section]\nkeep=1\n", 3: "alpha\n", 4: "stale"}
	for index, content := range seed {
		physical, err := context.ResolveHostPath(targets[index], validationmode.HostPathPolicy{})
		if err != nil {
			t.Fatal(err)
		}
		if index == 4 {
			physical = filepath.Join(physical, "stale.tmp")
		}
		if err := os.MkdirAll(filepath.Dir(physical), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(physical, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	actions := []RestoreAction{
		{Type: "copy", Source: "payload/dir", Target: targets[0], Backup: true},
		{Type: "merge-json", Source: "payload/settings.json", Target: targets[1], Backup: true},
		{Type: "merge-ini", Source: "payload/settings.ini", Target: targets[2], Backup: true},
		{Type: "append", Source: "payload/lines.txt", Target: targets[3], Backup: true},
		{Type: "delete-glob", Target: targets[4], Pattern: "*.tmp", Backup: true},
	}
	results, err := RunRestore(actions, RestoreOptions{
		ManifestDir: manifestRoot, BackupDir: backupDir, RunID: "legacy-strategies-run", ValidationContext: context,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(actions) {
		t.Fatalf("results = %+v", results)
	}
	for index, result := range results {
		if result.Status != "restored" {
			t.Fatalf("result %d = %+v", index, result)
		}
		assertNoLegacyValidationIdentity(t, context, result.Source, result.Target, result.BackupPath, result.Error)
		if index < 4 && !strings.EqualFold(result.Target, targets[index]) {
			t.Errorf("result %d target = %q, want %q", index, result.Target, targets[index])
		}
	}
	if !strings.EqualFold(results[4].Target, `%APPDATA%\Vendor\cache\stale.tmp`) {
		t.Fatalf("delete-glob semantic target = %q", results[4].Target)
	}

	directory, _ := context.ResolveHostPath(targets[0], validationmode.HostPathPolicy{})
	assertFileBytes(t, filepath.Join(directory, "one.txt"), []byte("one\n"))
	jsonTarget, _ := context.ResolveHostPath(targets[1], validationmode.HostPathPolicy{})
	assertFileBytes(t, jsonTarget, []byte("{\n  \"keep\": 1,\n  \"new\": 2\n}\n"))
	iniTarget, _ := context.ResolveHostPath(targets[2], validationmode.HostPathPolicy{})
	assertFileBytes(t, iniTarget, []byte("[section]\nkeep=1\nnew=2"))
	appendTarget, _ := context.ResolveHostPath(targets[3], validationmode.HostPathPolicy{})
	assertFileBytes(t, appendTarget, []byte("alpha\nbeta\n"))
	deletedRoot, _ := context.ResolveHostPath(targets[4], validationmode.HostPathPolicy{})
	if _, err := os.Stat(filepath.Join(deletedRoot, "stale.tmp")); !os.IsNotExist(err) {
		t.Fatalf("delete-glob target still exists: %v", err)
	}
}

func TestValidationLegacyRestoreRejectsEscapingPortableSource(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-source-tamper")
	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-source-tamper")
	if err := os.MkdirAll(manifestRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	authoredTarget := `%APPDATA%\Vendor\untouched.txt`
	results, err := RunRestore([]RestoreAction{{Type: "copy", Source: "../outside.txt", Target: authoredTarget}}, RestoreOptions{
		ManifestDir: manifestRoot, ValidationContext: context,
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "failed" {
		t.Fatalf("results = %+v", results)
	}
	assertNoLegacyValidationIdentity(t, context, results[0].Source, results[0].Target, results[0].Error)
	physical, err := context.ResolveHostPath(authoredTarget, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(physical); !os.IsNotExist(err) {
		t.Fatalf("escaping source mutated target: %v", err)
	}
}

func TestValidationLegacyOptionalMissingSourceStaysSemantic(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-missing-source")
	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-missing-source")
	if err := os.MkdirAll(manifestRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	authoredTarget := `%APPDATA%\Vendor\missing.txt`
	results, err := RunRestore([]RestoreAction{{
		Type: "copy", Source: "payload/missing.txt", Target: authoredTarget, Optional: true,
	}}, RestoreOptions{ManifestDir: manifestRoot, ValidationContext: context}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != "skipped_missing_source" ||
		results[0].Source != "payload/missing.txt" || results[0].Target != authoredTarget {
		t.Fatalf("optional missing result = %+v", results)
	}
	assertNoLegacyValidationIdentity(t, context, results[0].Source, results[0].Target, results[0].Error)
}

func TestValidationLegacyRejectsOutsideBackupBeforeFilesystemMutation(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-outside-backup")
	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-outside-backup")
	source := filepath.Join(manifestRoot, "payload", "settings.txt")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := context.ResolveHostPath(`%APPDATA%\Vendor\settings.txt`, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("prior"), 0o600); err != nil {
		t.Fatal(err)
	}
	outsideBackup := filepath.Join(t.TempDir(), "outside-backups")
	originalCopy := legacyRestoreAtomicCopyNative
	legacyRestoreAtomicCopyNative = func(string, string, os.FileMode) error {
		panic("filesystem mutation callback reached")
	}
	t.Cleanup(func() { legacyRestoreAtomicCopyNative = originalCopy })

	result, err := RestoreCopy(RestoreAction{Type: "copy", Backup: true}, source, target, RestoreOptions{
		BackupDir: outsideBackup, ValidationContext: context,
	})
	if err != nil || result.Status != "failed" {
		t.Fatalf("outside backup result = %+v, %v", result, err)
	}
	assertFileBytes(t, target, []byte("prior"))
	if _, err := os.Stat(outsideBackup); !os.IsNotExist(err) {
		t.Fatalf("outside backup directory was mutated: %v", err)
	}
	assertNoLegacyValidationIdentity(t, context, result.Error)
}

func TestValidationLegacyAllowsOutsideBackupWhenNoBackupIsUsed(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-unused-backup")
	manifestRoot := filepath.Join(context.Root(), "manifests", "legacy-unused-backup")
	if err := os.MkdirAll(filepath.Join(manifestRoot, "payload"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(manifestRoot, "payload", name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	outsideBackup := filepath.Join(t.TempDir(), "unused-outside-backups")
	tests := []struct {
		name   string
		typeID string
		source string
		body   string
	}{
		{name: "copy", typeID: "copy", source: write("copy.txt", "copy")},
		{name: "append", typeID: "append", source: write("append.txt", "append\n")},
		{name: "merge json", typeID: "merge-json", source: write("settings.json", `{"value":1}`)},
		{name: "merge ini", typeID: "merge-ini", source: write("settings.ini", "[section]\nvalue=1\n")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, err := context.ResolveHostPath(`%APPDATA%\Vendor\`+strings.ReplaceAll(test.name, " ", "-")+`.txt`, validationmode.HostPathPolicy{})
			if err != nil {
				t.Fatal(err)
			}
			result, err := RunRestore([]RestoreAction{{
				Type: test.typeID, Source: "payload/" + filepath.Base(test.source), Target: `%APPDATA%\Vendor\` + strings.ReplaceAll(test.name, " ", "-") + `.txt`, Backup: true,
			}}, RestoreOptions{ManifestDir: manifestRoot, BackupDir: outsideBackup, ValidationContext: context}, nil)
			if err != nil || len(result) != 1 || result[0].Status != "restored" {
				t.Fatalf("unused backup result = %+v, %v", result, err)
			}
			if _, err := os.Stat(target); err != nil {
				t.Fatalf("target was not restored: %v", err)
			}
		})
	}
	deleteTarget, err := context.ResolveHostPath(`%APPDATA%\Vendor\cache`, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(deleteTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	deleteResults, err := RestoreDeleteGlob(RestoreAction{Type: "delete-glob", Pattern: "*.tmp"}, deleteTarget, RestoreOptions{
		BackupDir: outsideBackup, ValidationContext: context,
	})
	if err != nil || len(deleteResults) != 1 || deleteResults[0].Status != "skipped_up_to_date" {
		t.Fatalf("unused delete-glob backup = %+v, %v", deleteResults, err)
	}
	if _, err := os.Stat(outsideBackup); !os.IsNotExist(err) {
		t.Fatalf("unused outside backup directory was mutated: %v", err)
	}
}

func TestValidationDirectoryCopyGuardsNestedMemberBeforeNativeLstat(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-nested-member")
	source := filepath.Join(context.Root(), "manifests", "legacy-nested-member", "payload", "directory")
	nestedParent := filepath.Join(source, "nested")
	nested := filepath.Join(nestedParent, "settings.txt")
	if err := os.MkdirAll(nestedParent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("desired"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := context.ResolveHostPath(`%APPDATA%\Vendor\directory`, validationmode.HostPathPolicy{})
	if err != nil {
		t.Fatal(err)
	}
	originalCheckpoint, originalLstat := legacyRestoreIOCheckpoint, legacyRestoreLstatNative
	fired, nativeReached := false, false
	legacyRestoreIOCheckpoint = func(operation, path string) {
		if operation == "copy-preflight-member-lstat" && filepath.Clean(path) == filepath.Clean(nested) && !fired {
			fired = true
			replaceValidationDirectoryWithFile(t, nestedParent)
		}
	}
	legacyRestoreLstatNative = func(path string) (os.FileInfo, error) {
		if filepath.Clean(path) == filepath.Clean(nested) {
			nativeReached = true
			panic("nested lstat callback reached")
		}
		return os.Lstat(path)
	}
	t.Cleanup(func() {
		legacyRestoreIOCheckpoint, legacyRestoreLstatNative = originalCheckpoint, originalLstat
	})
	result, err := RestoreCopy(RestoreAction{Type: "copy"}, source, target, RestoreOptions{ValidationContext: context})
	if err != nil || result.Status != "failed" || !fired || nativeReached {
		t.Fatalf("nested member result = %+v fired=%v native=%v err=%v", result, fired, nativeReached, err)
	}
	assertNoLegacyValidationIdentity(t, context, result.Error)
}

func TestValidationLegacyRechecksSourceAndTargetImmediatelyBeforeNativeIO(t *testing.T) {
	t.Run("source stat", func(t *testing.T) {
		context, _ := activeLegacyRestoreValidationContext(t, "legacy-source-recheck")
		sourceParent := filepath.Join(context.Root(), "manifests", "legacy-source-recheck", "payload")
		source := filepath.Join(sourceParent, "settings.txt")
		if err := os.MkdirAll(sourceParent, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("desired"), 0o600); err != nil {
			t.Fatal(err)
		}
		target, err := context.ResolveHostPath(`%APPDATA%\Vendor\settings.txt`, validationmode.HostPathPolicy{})
		if err != nil {
			t.Fatal(err)
		}
		originalCheckpoint, originalStat := legacyRestoreIOCheckpoint, legacyRestoreStatNative
		fired := false
		legacyRestoreIOCheckpoint = func(operation, path string) {
			if operation == "source-stat" && !fired {
				fired = true
				replaceValidationDirectoryWithFile(t, sourceParent)
			}
		}
		legacyRestoreStatNative = func(string) (os.FileInfo, error) {
			panic("source stat callback reached")
		}
		t.Cleanup(func() {
			legacyRestoreIOCheckpoint, legacyRestoreStatNative = originalCheckpoint, originalStat
		})
		result, err := RestoreCopy(RestoreAction{Type: "copy"}, source, target, RestoreOptions{ValidationContext: context})
		if err == nil || result != nil || !fired {
			t.Fatalf("source tamper result = %+v fired=%v err=%v", result, fired, err)
		}
		assertNoLegacyValidationIdentity(t, context, err.Error())
	})

	t.Run("target copy", func(t *testing.T) {
		context, _ := activeLegacyRestoreValidationContext(t, "legacy-target-recheck")
		source := filepath.Join(context.Root(), "manifests", "legacy-target-recheck", "payload", "settings.txt")
		if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(source, []byte("desired"), 0o600); err != nil {
			t.Fatal(err)
		}
		target, err := context.ResolveHostPath(`%APPDATA%\Vendor\settings.txt`, validationmode.HostPathPolicy{})
		if err != nil {
			t.Fatal(err)
		}
		targetParent := filepath.Dir(target)
		if err := os.MkdirAll(targetParent, 0o755); err != nil {
			t.Fatal(err)
		}
		originalCheckpoint, originalCopy := legacyRestoreIOCheckpoint, legacyRestoreAtomicCopyNative
		fired := false
		legacyRestoreIOCheckpoint = func(operation, path string) {
			if operation == "target-atomic-copy" && !fired {
				fired = true
				replaceValidationDirectoryWithFile(t, targetParent)
			}
		}
		legacyRestoreAtomicCopyNative = func(string, string, os.FileMode) error {
			panic("target copy callback reached")
		}
		t.Cleanup(func() {
			legacyRestoreIOCheckpoint, legacyRestoreAtomicCopyNative = originalCheckpoint, originalCopy
		})
		result, err := RestoreCopy(RestoreAction{Type: "copy"}, source, target, RestoreOptions{ValidationContext: context})
		if err != nil || result.Status != "failed" || !fired {
			t.Fatalf("target tamper result = %+v fired=%v err=%v", result, fired, err)
		}
	})
}

func TestValidationJournalAuthorizesAndRechecksPublicationPaths(t *testing.T) {
	context, _ := activeLegacyRestoreValidationContext(t, "legacy-journal-boundary")
	originalMkdir, originalWrite := legacyRestoreMkdirAllNative, legacyRestoreWriteFileNative
	legacyRestoreMkdirAllNative = func(string, os.FileMode) error { panic("journal mkdir callback reached") }
	outside := filepath.Join(t.TempDir(), "outside-logs")
	if err := WriteJournalWithValidation(outside, "outside", "manifest.jsonc", "", "", nil, context); err == nil {
		t.Fatal("outside journal directory was accepted")
	}
	if _, err := os.Stat(outside); !os.IsNotExist(err) {
		t.Fatalf("outside journal directory was mutated: %v", err)
	}
	legacyRestoreMkdirAllNative = originalMkdir

	logsDir := filepath.Join(context.Root(), "logs", "journal-boundary")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	originalCheckpoint := legacyRestoreIOCheckpoint
	fired := false
	legacyRestoreIOCheckpoint = func(operation, path string) {
		if operation == "journal-temp-write" && !fired {
			fired = true
			replaceValidationDirectoryWithFile(t, logsDir)
		}
	}
	legacyRestoreWriteFileNative = func(string, []byte, os.FileMode) error {
		panic("journal write callback reached")
	}
	t.Cleanup(func() {
		legacyRestoreIOCheckpoint = originalCheckpoint
		legacyRestoreMkdirAllNative, legacyRestoreWriteFileNative = originalMkdir, originalWrite
	})
	if err := WriteJournalWithValidation(logsDir, "tampered", "manifest.jsonc", "", "", nil, context); err == nil || !fired {
		t.Fatalf("tampered journal publication error = %v fired=%v", err, fired)
	} else {
		assertNoLegacyValidationIdentity(t, context, err.Error())
	}
}

func replaceValidationDirectoryWithFile(t *testing.T, directory string) {
	t.Helper()
	held := directory + ".held"
	if err := os.Rename(directory, held); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(directory, []byte("tampered"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Remove(directory)
		_ = os.Rename(held, directory)
	})
}

func activeLegacyRestoreValidationContext(t *testing.T, scenario string) (*validationmode.Context, string) {
	t.Helper()
	nonce := "nonce-" + scenario
	base := t.TempDir()
	root := filepath.Join(base, "endstate-validation-"+nonce)
	if err := os.MkdirAll(filepath.Join(root, ".endstate"), 0o755); err != nil {
		t.Fatal(err)
	}
	originalAppData := filepath.Join(base, "original-appdata")
	if err := os.MkdirAll(originalAppData, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", originalAppData)
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1, ScenarioID: scenario, Nonce: nonce, ModuleID: "apps.example",
		Inventory: validationmode.Inventory{
			AppID: "example", Driver: "winget", Ref: "Vendor.Example", DisplayName: "Example", InitialState: "present",
		},
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".endstate", "validation-mode.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	context, restoreEnvironment, err := validationmode.ActivateFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := restoreEnvironment(); err != nil {
			t.Errorf("restore validation environment: %v", err)
		}
	})
	return context, originalAppData
}

func assertFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func assertNoLegacyValidationIdentity(t *testing.T, context *validationmode.Context, values ...string) {
	t.Helper()
	for _, value := range values {
		if strings.Contains(strings.ToLower(value), strings.ToLower(context.Root())) ||
			strings.Contains(strings.ToLower(value), strings.ToLower(context.Descriptor().Nonce)) {
			t.Fatalf("validation identity leaked in %q", value)
		}
	}
}
