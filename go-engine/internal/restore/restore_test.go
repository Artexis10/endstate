// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Copy strategy tests
// ---------------------------------------------------------------------------

func TestRestoreCopy_FileWithBackup(t *testing.T) {
	tmp := t.TempDir()

	// Create source file.
	srcFile := filepath.Join(tmp, "source", "config.json")
	os.MkdirAll(filepath.Dir(srcFile), 0755)
	os.WriteFile(srcFile, []byte(`{"key":"value"}`), 0644)

	// Create existing target file.
	tgtFile := filepath.Join(tmp, "target", "config.json")
	os.MkdirAll(filepath.Dir(tgtFile), 0755)
	os.WriteFile(tgtFile, []byte(`{"old":"data"}`), 0644)

	backupDir := filepath.Join(tmp, "backups")

	entry := RestoreAction{
		Type:   "copy",
		Source: srcFile,
		Target: tgtFile,
		Backup: true,
	}

	result, err := RestoreCopy(entry, srcFile, tgtFile, RestoreOptions{
		BackupDir: backupDir,
		RunID:     "test-run",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}
	if !result.BackupCreated {
		t.Error("expected BackupCreated=true")
	}
	if result.BackupPath == "" {
		t.Error("expected BackupPath to be set")
	}

	// Verify target has new content.
	data, _ := os.ReadFile(tgtFile)
	if string(data) != `{"key":"value"}` {
		t.Errorf("target content mismatch: %q", string(data))
	}

	// Verify backup exists with old content.
	backupData, readErr := os.ReadFile(result.BackupPath)
	if readErr != nil {
		t.Fatalf("cannot read backup: %v", readErr)
	}
	if string(backupData) != `{"old":"data"}` {
		t.Errorf("backup content mismatch: %q", string(backupData))
	}
}

func TestRestoreCopy_UpToDateSkip(t *testing.T) {
	tmp := t.TempDir()

	content := []byte(`{"identical":"content"}`)

	srcFile := filepath.Join(tmp, "source.json")
	tgtFile := filepath.Join(tmp, "target.json")
	os.WriteFile(srcFile, content, 0644)
	os.WriteFile(tgtFile, content, 0644)

	entry := RestoreAction{
		Type:   "copy",
		Source: srcFile,
		Target: tgtFile,
		Backup: true,
	}

	result, err := RestoreCopy(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "skipped_up_to_date" {
		t.Errorf("expected status=skipped_up_to_date, got %q", result.Status)
	}
}

func TestRestoreCopy_DirectoryCopy(t *testing.T) {
	tmp := t.TempDir()

	// Create source directory with files.
	srcDir := filepath.Join(tmp, "source")
	os.MkdirAll(filepath.Join(srcDir, "sub"), 0755)
	os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("file-a"), 0644)
	os.WriteFile(filepath.Join(srcDir, "sub", "b.txt"), []byte("file-b"), 0644)

	tgtDir := filepath.Join(tmp, "target")

	entry := RestoreAction{
		Type:   "copy",
		Source: srcDir,
		Target: tgtDir,
	}

	result, err := RestoreCopy(entry, srcDir, tgtDir, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}

	// Verify files were copied.
	data, _ := os.ReadFile(filepath.Join(tgtDir, "a.txt"))
	if string(data) != "file-a" {
		t.Errorf("expected file-a content, got %q", string(data))
	}
	data, _ = os.ReadFile(filepath.Join(tgtDir, "sub", "b.txt"))
	if string(data) != "file-b" {
		t.Errorf("expected file-b content, got %q", string(data))
	}
}

func TestRestoreCopy_ExcludeGlobs(t *testing.T) {
	tmp := t.TempDir()

	// Create source directory with files and a Logs subdirectory.
	srcDir := filepath.Join(tmp, "source")
	os.MkdirAll(filepath.Join(srcDir, "Logs"), 0755)
	os.MkdirAll(filepath.Join(srcDir, "data"), 0755)
	os.WriteFile(filepath.Join(srcDir, "config.ini"), []byte("config"), 0644)
	os.WriteFile(filepath.Join(srcDir, "Logs", "debug.log"), []byte("log"), 0644)
	os.WriteFile(filepath.Join(srcDir, "data", "important.db"), []byte("data"), 0644)

	tgtDir := filepath.Join(tmp, "target")

	entry := RestoreAction{
		Type:    "copy",
		Source:  srcDir,
		Target:  tgtDir,
		Exclude: []string{"**/Logs/**"},
	}

	result, err := RestoreCopy(entry, srcDir, tgtDir, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}

	// config.ini and data/ should exist.
	if _, err := os.Stat(filepath.Join(tgtDir, "config.ini")); err != nil {
		t.Error("config.ini should have been copied")
	}
	if _, err := os.Stat(filepath.Join(tgtDir, "data", "important.db")); err != nil {
		t.Error("data/important.db should have been copied")
	}

	// Logs should NOT exist.
	if _, err := os.Stat(filepath.Join(tgtDir, "Logs")); !os.IsNotExist(err) {
		t.Error("Logs directory should have been excluded")
	}
}

// ---------------------------------------------------------------------------
// Merge-JSON strategy tests
// ---------------------------------------------------------------------------

func TestDeepMerge_Objects(t *testing.T) {
	target := map[string]interface{}{
		"a": map[string]interface{}{
			"b": float64(1),
			"d": float64(4),
		},
	}
	source := map[string]interface{}{
		"a": map[string]interface{}{
			"b": float64(2),
			"c": float64(3),
		},
	}

	merged := DeepMerge(target, source).(map[string]interface{})
	a := merged["a"].(map[string]interface{})

	if a["b"] != float64(2) {
		t.Errorf("expected a.b=2, got %v", a["b"])
	}
	if a["c"] != float64(3) {
		t.Errorf("expected a.c=3, got %v", a["c"])
	}
	if a["d"] != float64(4) {
		t.Errorf("expected a.d=4, got %v", a["d"])
	}
}

func TestDeepMerge_ArrayReplace(t *testing.T) {
	target := map[string]interface{}{
		"items": []interface{}{"a", "b"},
	}
	source := map[string]interface{}{
		"items": []interface{}{"x", "y", "z"},
	}

	merged := DeepMerge(target, source).(map[string]interface{})
	items := merged["items"].([]interface{})

	if len(items) != 3 || items[0] != "x" || items[1] != "y" || items[2] != "z" {
		t.Errorf("expected source array to replace target, got %v", items)
	}
}

func TestDeepMerge_ScalarOverwrite(t *testing.T) {
	target := map[string]interface{}{"key": "old"}
	source := map[string]interface{}{"key": "new"}

	merged := DeepMerge(target, source).(map[string]interface{})
	if merged["key"] != "new" {
		t.Errorf("expected key=new, got %v", merged["key"])
	}
}

func TestRestoreMergeJson_FileRoundTrip(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.json")
	tgtFile := filepath.Join(tmp, "target.json")

	os.WriteFile(srcFile, []byte(`{"a":{"b":2,"c":3}}`), 0644)
	os.WriteFile(tgtFile, []byte(`{"a":{"b":1,"d":4}}`), 0644)

	entry := RestoreAction{
		Type:   "merge-json",
		Source: srcFile,
		Target: tgtFile,
		Backup: true,
	}

	backupDir := filepath.Join(tmp, "backups")
	result, err := RestoreMergeJson(entry, srcFile, tgtFile, RestoreOptions{
		BackupDir: backupDir,
		RunID:     "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}

	// Read merged file and verify.
	data, _ := os.ReadFile(tgtFile)
	var merged map[string]interface{}
	json.Unmarshal(data, &merged)

	a := merged["a"].(map[string]interface{})
	if a["b"] != float64(2) {
		t.Errorf("expected a.b=2, got %v", a["b"])
	}
	if a["c"] != float64(3) {
		t.Errorf("expected a.c=3, got %v", a["c"])
	}
	if a["d"] != float64(4) {
		t.Errorf("expected a.d=4, got %v", a["d"])
	}
}

// ---------------------------------------------------------------------------
// Merge-INI strategy tests
// ---------------------------------------------------------------------------

func TestParseIni_Basic(t *testing.T) {
	content := "[editor]\nfont=Consolas\ntheme=dark\n\n[terminal]\nshell=pwsh\n"
	ini := ParseIni(content)

	var editor *IniSection
	var terminal *IniSection
	for _, sec := range ini.Sections {
		switch sec.Name {
		case "editor":
			editor = sec
		case "terminal":
			terminal = sec
		}
	}

	if editor == nil {
		t.Fatal("expected [editor] section")
	}
	if editor.Vals["font"] != "Consolas" {
		t.Errorf("expected font=Consolas, got %q", editor.Vals["font"])
	}
	if editor.Vals["theme"] != "dark" {
		t.Errorf("expected theme=dark, got %q", editor.Vals["theme"])
	}

	if terminal == nil {
		t.Fatal("expected [terminal] section")
	}
	if terminal.Vals["shell"] != "pwsh" {
		t.Errorf("expected shell=pwsh, got %q", terminal.Vals["shell"])
	}
}

func TestMergeIni_SectionMerge(t *testing.T) {
	target := ParseIni("[editor]\ntheme=dark\n")
	source := ParseIni("[editor]\nfont=Consolas\n")

	merged := MergeIni(target, source)
	result := FormatIni(merged)

	if !strings.Contains(result, "font=Consolas") {
		t.Error("expected font=Consolas in merged output")
	}
	if !strings.Contains(result, "theme=dark") {
		t.Error("expected theme=dark preserved in merged output")
	}
}

func TestMergeIni_PreserveExistingKeys(t *testing.T) {
	target := ParseIni("[settings]\nkey1=val1\nkey2=val2\n")
	source := ParseIni("[settings]\nkey1=newval\n")

	merged := MergeIni(target, source)

	var settings *IniSection
	for _, sec := range merged.Sections {
		if sec.Name == "settings" {
			settings = sec
		}
	}

	if settings == nil {
		t.Fatal("expected [settings] section")
	}
	if settings.Vals["key1"] != "newval" {
		t.Errorf("expected key1=newval, got %q", settings.Vals["key1"])
	}
	if settings.Vals["key2"] != "val2" {
		t.Errorf("expected key2=val2 preserved, got %q", settings.Vals["key2"])
	}
}

func TestMergeIni_GlobalKeys(t *testing.T) {
	target := ParseIni("globalKey=oldValue\n[section]\nk=v\n")
	source := ParseIni("globalKey=newValue\nnewGlobal=yes\n")

	merged := MergeIni(target, source)

	var global *IniSection
	for _, sec := range merged.Sections {
		if sec.Name == "" {
			global = sec
		}
	}

	if global == nil {
		t.Fatal("expected global section")
	}
	if global.Vals["globalKey"] != "newValue" {
		t.Errorf("expected globalKey=newValue, got %q", global.Vals["globalKey"])
	}
	if global.Vals["newGlobal"] != "yes" {
		t.Errorf("expected newGlobal=yes, got %q", global.Vals["newGlobal"])
	}
}

func TestRestoreMergeIni_FileRoundTrip(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.ini")
	tgtFile := filepath.Join(tmp, "target.ini")

	os.WriteFile(srcFile, []byte("[editor]\nfont=Consolas\n"), 0644)
	os.WriteFile(tgtFile, []byte("[editor]\ntheme=dark\n"), 0644)

	entry := RestoreAction{
		Type:   "merge-ini",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreMergeIni(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}

	data, _ := os.ReadFile(tgtFile)
	content := string(data)
	if !strings.Contains(content, "font=Consolas") {
		t.Error("expected font=Consolas in merged file")
	}
	if !strings.Contains(content, "theme=dark") {
		t.Error("expected theme=dark in merged file")
	}
}

// ---------------------------------------------------------------------------
// Append strategy tests
// ---------------------------------------------------------------------------

func TestRestoreAppend_NewFile(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "target.txt")

	os.WriteFile(srcFile, []byte("line1\nline2\n"), 0644)

	entry := RestoreAction{
		Type:   "append",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreAppend(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}

	data, _ := os.ReadFile(tgtFile)
	if string(data) != "line1\nline2\n" {
		t.Errorf("target content mismatch: %q", string(data))
	}
}

func TestRestoreAppend_ExistingFile(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "target.txt")

	os.WriteFile(srcFile, []byte("new-line\n"), 0644)
	os.WriteFile(tgtFile, []byte("existing-line\n"), 0644)

	backupDir := filepath.Join(tmp, "backups")
	entry := RestoreAction{
		Type:   "append",
		Source: srcFile,
		Target: tgtFile,
		Backup: true,
	}

	result, err := RestoreAppend(entry, srcFile, tgtFile, RestoreOptions{
		BackupDir: backupDir,
		RunID:     "test",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}
	if !result.BackupCreated {
		t.Error("expected BackupCreated=true")
	}

	data, _ := os.ReadFile(tgtFile)
	if !strings.Contains(string(data), "existing-line") {
		t.Error("expected existing content preserved")
	}
	if !strings.Contains(string(data), "new-line") {
		t.Error("expected new content appended")
	}
}

// ---------------------------------------------------------------------------
// Journal tests
// ---------------------------------------------------------------------------

func TestJournal_WriteReadRoundTrip(t *testing.T) {
	tmp := t.TempDir()
	logsDir := filepath.Join(tmp, "logs")

	results := []RestoreResult{
		{
			ID:                  "test-1",
			Source:              "/src/a.json",
			Target:              "/tgt/a.json",
			Status:              "restored",
			BackupPath:          "/backup/a.json",
			BackupCreated:       true,
			TargetExistedBefore: true,
		},
		{
			ID:     "test-2",
			Source: "/src/b.json",
			Target: "/tgt/b.json",
			Status: "skipped_up_to_date",
		},
	}

	err := WriteJournal(logsDir, "run-001", "/manifest.jsonc", "/manifest-dir", "", results)
	if err != nil {
		t.Fatalf("WriteJournal failed: %v", err)
	}

	journalPath, err := FindLatestJournal(logsDir)
	if err != nil {
		t.Fatalf("FindLatestJournal failed: %v", err)
	}

	journal, err := ReadJournal(journalPath)
	if err != nil {
		t.Fatalf("ReadJournal failed: %v", err)
	}

	if journal.RunID != "run-001" {
		t.Errorf("expected runId=run-001, got %q", journal.RunID)
	}
	if len(journal.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(journal.Entries))
	}
	if journal.Entries[0].Action != "restored" {
		t.Errorf("expected first entry action=restored, got %q", journal.Entries[0].Action)
	}
	if journal.Entries[1].Action != "skipped_up_to_date" {
		t.Errorf("expected second entry action=skipped_up_to_date, got %q", journal.Entries[1].Action)
	}
}

func TestFindLatestJournal_MultipleFiles(t *testing.T) {
	tmp := t.TempDir()
	logsDir := filepath.Join(tmp, "logs")
	os.MkdirAll(logsDir, 0755)

	// Create two journal files with different timestamps.
	os.WriteFile(filepath.Join(logsDir, "restore-journal-restore-20260101-120000.json"), []byte(`{"runId":"old"}`), 0644)
	os.WriteFile(filepath.Join(logsDir, "restore-journal-restore-20260102-120000.json"), []byte(`{"runId":"new"}`), 0644)

	latest, err := FindLatestJournal(logsDir)
	if err != nil {
		t.Fatalf("FindLatestJournal failed: %v", err)
	}

	if !strings.Contains(latest, "20260102") {
		t.Errorf("expected latest journal to be 20260102, got %q", latest)
	}
}

// ---------------------------------------------------------------------------
// Revert tests
// ---------------------------------------------------------------------------

func TestRunRevert_ReverseOrder(t *testing.T) {
	tmp := t.TempDir()

	// Create targets that were "restored" by a previous run.
	targetA := filepath.Join(tmp, "a.txt")
	targetB := filepath.Join(tmp, "b.txt")
	targetC := filepath.Join(tmp, "c.txt")
	os.WriteFile(targetA, []byte("restored-a"), 0644)
	os.WriteFile(targetB, []byte("restored-b"), 0644)
	os.WriteFile(targetC, []byte("restored-c"), 0644)

	journal := &Journal{
		RunID: "test-revert",
		Entries: []JournalEntry{
			{TargetPath: targetA, TargetExistedBefore: false, Action: "restored"},
			{TargetPath: targetB, TargetExistedBefore: false, Action: "restored"},
			{TargetPath: targetC, TargetExistedBefore: false, Action: "restored"},
		},
	}

	results, err := RunRevert(journal, "")
	if err != nil {
		t.Fatalf("RunRevert failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Reverse order: C, B, A.
	if results[0].Target != targetC {
		t.Errorf("expected first revert target=C, got %q", results[0].Target)
	}
	if results[1].Target != targetB {
		t.Errorf("expected second revert target=B, got %q", results[1].Target)
	}
	if results[2].Target != targetA {
		t.Errorf("expected third revert target=A, got %q", results[2].Target)
	}

	// All should be deleted since targetExistedBefore=false.
	for _, r := range results {
		if r.Action != "deleted" {
			t.Errorf("expected action=deleted for %q, got %q", r.Target, r.Action)
		}
	}

	// Files should not exist.
	for _, f := range []string{targetA, targetB, targetC} {
		if _, err := os.Stat(f); !os.IsNotExist(err) {
			t.Errorf("expected %q to be deleted", f)
		}
	}
}

func TestRunRevert_RestoreFromBackup(t *testing.T) {
	tmp := t.TempDir()

	// Create current target (post-restore).
	target := filepath.Join(tmp, "config.json")
	os.WriteFile(target, []byte(`{"new":"content"}`), 0644)

	// Create backup of original.
	backupPath := filepath.Join(tmp, "backup", "config.json")
	os.MkdirAll(filepath.Dir(backupPath), 0755)
	os.WriteFile(backupPath, []byte(`{"original":"content"}`), 0644)

	journal := &Journal{
		RunID: "test-revert-backup",
		Entries: []JournalEntry{
			{
				TargetPath:          target,
				TargetExistedBefore: true,
				BackupCreated:       true,
				BackupPath:          backupPath,
				Action:              "restored",
			},
		},
	}

	results, err := RunRevert(journal, "")
	if err != nil {
		t.Fatalf("RunRevert failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Action != "reverted" {
		t.Errorf("expected action=reverted, got %q", results[0].Action)
	}

	// Target should have original content.
	data, _ := os.ReadFile(target)
	if string(data) != `{"original":"content"}` {
		t.Errorf("expected original content restored, got %q", string(data))
	}
}

func TestRunRevert_SkipNonRestored(t *testing.T) {
	journal := &Journal{
		RunID: "test-skip",
		Entries: []JournalEntry{
			{TargetPath: "/some/path", Action: "skipped_up_to_date"},
			{TargetPath: "/other/path", Action: "failed"},
		},
	}

	results, err := RunRevert(journal, "")
	if err != nil {
		t.Fatalf("RunRevert failed: %v", err)
	}

	for _, r := range results {
		if r.Action != "skipped" {
			t.Errorf("expected action=skipped for non-restored entry, got %q", r.Action)
		}
	}
}

// ---------------------------------------------------------------------------
// Optional entry tests
// ---------------------------------------------------------------------------

func TestRunRestore_OptionalMissingSource(t *testing.T) {
	entries := []RestoreAction{
		{
			Type:     "copy",
			Source:   "/nonexistent/source.txt",
			Target:   "/some/target.txt",
			Optional: true,
			ID:       "optional-test",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "skipped_missing_source" {
		t.Errorf("expected status=skipped_missing_source, got %q", results[0].Status)
	}
}

// ---------------------------------------------------------------------------
// Sensitive path detection tests
// ---------------------------------------------------------------------------

func TestCheckSensitivePath_DetectsSSH(t *testing.T) {
	warnings := CheckSensitivePath("C:\\Users\\test\\.ssh\\id_rsa")
	if len(warnings) == 0 {
		t.Error("expected warnings for .ssh path")
	}

	found := false
	for _, w := range warnings {
		if strings.Contains(w, ".ssh") {
			found = true
		}
	}
	if !found {
		t.Error("expected warning about .ssh segment")
	}
}

func TestCheckSensitivePath_DetectsCredentials(t *testing.T) {
	warnings := CheckSensitivePath("/home/user/.aws/credentials")
	if len(warnings) == 0 {
		t.Error("expected warnings for .aws/credentials path")
	}
}

func TestCheckSensitivePath_NoWarningForSafePath(t *testing.T) {
	warnings := CheckSensitivePath("C:\\Users\\test\\Documents\\config.json")
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for safe path, got %v", warnings)
	}
}

// ---------------------------------------------------------------------------
// RunRestore orchestrator tests
// ---------------------------------------------------------------------------

func TestRunRestore_DispatchToCorrectStrategy(t *testing.T) {
	tmp := t.TempDir()

	// Create source files for each strategy.
	srcJSON := filepath.Join(tmp, "src", "a.json")
	tgtJSON := filepath.Join(tmp, "tgt", "a.json")
	os.MkdirAll(filepath.Dir(srcJSON), 0755)
	os.MkdirAll(filepath.Dir(tgtJSON), 0755)
	os.WriteFile(srcJSON, []byte(`{"new":"data"}`), 0644)
	os.WriteFile(tgtJSON, []byte(`{"old":"data"}`), 0644)

	entries := []RestoreAction{
		{
			Type:   "merge-json",
			Source: srcJSON,
			Target: tgtJSON,
			ID:     "json-merge",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "restored" {
		t.Errorf("expected status=restored, got %q", results[0].Status)
	}

	// Verify merge happened.
	data, _ := os.ReadFile(tgtJSON)
	var merged map[string]interface{}
	json.Unmarshal(data, &merged)
	if merged["new"] != "data" {
		t.Error("expected new key from source")
	}
	if merged["old"] != "data" {
		t.Error("expected old key preserved from target")
	}
}

func TestRunRestore_UnsupportedType(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "src.txt")
	os.WriteFile(srcFile, []byte("data"), 0644)

	entries := []RestoreAction{
		{
			Type:   "unknown-strategy",
			Source: srcFile,
			Target: filepath.Join(tmp, "tgt.txt"),
			ID:     "unknown",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != "failed" {
		t.Errorf("expected status=failed for unsupported type, got %q", results[0].Status)
	}
	if !strings.Contains(results[0].Error, "unsupported") {
		t.Errorf("expected error about unsupported type, got %q", results[0].Error)
	}
}

// ---------------------------------------------------------------------------
// Backup utility tests
// ---------------------------------------------------------------------------

func TestComputeFileHash(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "test.txt")
	os.WriteFile(f, []byte("hello world"), 0644)

	hash, err := ComputeFileHash(f)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// SHA256 of "hello world" is well-known.
	expected := "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9"
	if hash != expected {
		t.Errorf("expected hash %q, got %q", expected, hash)
	}
}

func TestIsUpToDate_IdenticalFiles(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.txt")
	b := filepath.Join(tmp, "b.txt")
	os.WriteFile(a, []byte("same"), 0644)
	os.WriteFile(b, []byte("same"), 0644)

	result, err := IsUpToDate(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result {
		t.Error("expected files to be up-to-date")
	}
}

func TestIsUpToDate_DifferentFiles(t *testing.T) {
	tmp := t.TempDir()
	a := filepath.Join(tmp, "a.txt")
	b := filepath.Join(tmp, "b.txt")
	os.WriteFile(a, []byte("content-a"), 0644)
	os.WriteFile(b, []byte("content-b"), 0644)

	result, err := IsUpToDate(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expected files to NOT be up-to-date")
	}
}

// ---------------------------------------------------------------------------
// Exclude glob matching tests
// ---------------------------------------------------------------------------

func TestIsPathExcluded(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		patterns []string
		want     bool
	}{
		{"match Logs dir", "app/Logs/debug.log", []string{"**/Logs/**"}, true},
		{"match Cache dir", "data/Cache/temp.bin", []string{"**/Cache/**"}, true},
		{"no match", "config/settings.json", []string{"**/Logs/**"}, false},
		{"empty patterns", "anything/at/all", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isPathExcluded(tt.relPath, tt.patterns)
			if got != tt.want {
				t.Errorf("isPathExcluded(%q, %v) = %v, want %v", tt.relPath, tt.patterns, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DryRun tests
// ---------------------------------------------------------------------------

func TestRunRestore_DryRunDoesNotModify(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "target.txt")
	os.WriteFile(srcFile, []byte("new content"), 0644)

	entries := []RestoreAction{
		{
			Type:   "copy",
			Source: srcFile,
			Target: tgtFile,
			ID:     "dry-test",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != "restored" {
		t.Errorf("expected status=restored for dry-run, got %q", results[0].Status)
	}

	// Target should NOT have been created.
	if _, err := os.Stat(tgtFile); !os.IsNotExist(err) {
		t.Error("target should not exist in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// Source resolution tests
// ---------------------------------------------------------------------------

func TestResolveSource_ExportRootFirst(t *testing.T) {
	tmp := t.TempDir()

	// Create file in both export root and manifest dir.
	exportDir := filepath.Join(tmp, "export")
	manifestDir := filepath.Join(tmp, "manifest")
	os.MkdirAll(exportDir, 0755)
	os.MkdirAll(manifestDir, 0755)
	os.WriteFile(filepath.Join(exportDir, "config.json"), []byte("export"), 0644)
	os.WriteFile(filepath.Join(manifestDir, "config.json"), []byte("manifest"), 0644)

	resolved := resolveSource("config.json", RestoreOptions{
		ExportRoot:  exportDir,
		ManifestDir: manifestDir,
	})

	// Should resolve from export root first.
	data, _ := os.ReadFile(resolved)
	if string(data) != "export" {
		t.Errorf("expected source from export root, got content %q", string(data))
	}
}

func TestResolveSource_FallbackToManifestDir(t *testing.T) {
	tmp := t.TempDir()

	manifestDir := filepath.Join(tmp, "manifest")
	exportDir := filepath.Join(tmp, "export")
	os.MkdirAll(manifestDir, 0755)
	os.MkdirAll(exportDir, 0755)
	os.WriteFile(filepath.Join(manifestDir, "config.json"), []byte("manifest"), 0644)
	// Export dir does NOT have config.json.

	resolved := resolveSource("config.json", RestoreOptions{
		ExportRoot:  exportDir,
		ManifestDir: manifestDir,
	})

	// Should fallback to manifest dir.
	if !strings.Contains(resolved, "manifest") {
		t.Errorf("expected path in manifest dir, got %q", resolved)
	}
}
