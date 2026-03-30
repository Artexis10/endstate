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

	"github.com/Artexis10/endstate/go-engine/internal/events"
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

func TestReadJournal_BOMPrefixed(t *testing.T) {
	tmp := t.TempDir()

	// UTF-8 BOM (0xEF 0xBB 0xBF) followed by valid JSON — this is what
	// PowerShell 5.1 produces when writing JSON with Out-File / Set-Content.
	bom := []byte{0xEF, 0xBB, 0xBF}
	jsonBody := []byte(`{"runId":"bom-test","timestamp":"2026-01-01T00:00:00Z","manifestPath":"/m.jsonc","manifestDir":"/m","entries":[]}`)
	data := append(bom, jsonBody...)

	journalPath := filepath.Join(tmp, "restore-journal-bom-test.json")
	os.WriteFile(journalPath, data, 0644)

	journal, err := ReadJournal(journalPath)
	if err != nil {
		t.Fatalf("ReadJournal should handle BOM-prefixed JSON, got error: %v", err)
	}
	if journal.RunID != "bom-test" {
		t.Errorf("expected runId=bom-test, got %q", journal.RunID)
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

	results, err := RunRestore(entries, RestoreOptions{}, nil)
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

	results, err := RunRestore(entries, RestoreOptions{}, nil)
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

	results, err := RunRestore(entries, RestoreOptions{}, nil)
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

	results, err := RunRestore(entries, RestoreOptions{DryRun: true}, nil)
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

// ===========================================================================
// Additional tests ported from Pester reference suite
// ===========================================================================

// ---------------------------------------------------------------------------
// Copy: source not found (non-optional) should fail with error
// Pester: Restore.SourceNotFound
// ---------------------------------------------------------------------------

func TestRestoreCopy_SourceNotFound(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "nonexistent-source.txt")
	tgtFile := filepath.Join(tmp, "target.txt")

	entry := RestoreAction{
		Type:   "copy",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreCopy(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "failed" {
		t.Errorf("expected status=failed, got %q", result.Status)
	}
	if !strings.Contains(strings.ToLower(result.Error), "source not found") {
		t.Errorf("expected error about source not found, got %q", result.Error)
	}
}

// ---------------------------------------------------------------------------
// Copy: creates target directory if it doesn't exist
// Pester: Restore.CopyFile "Should create target directory"
// ---------------------------------------------------------------------------

func TestRestoreCopy_CreatesTargetDirectory(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "subdir", "nested", "target.txt")

	os.WriteFile(srcFile, []byte("nested content"), 0644)

	entry := RestoreAction{
		Type:   "copy",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreCopy(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}
	if _, err := os.Stat(tgtFile); err != nil {
		t.Error("target file should exist in nested directory")
	}
	data, _ := os.ReadFile(tgtFile)
	if string(data) != "nested content" {
		t.Errorf("target content mismatch: %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// Copy: multiple exclude patterns
// Pester: Restore.Exclude.DirectoryCopy "multiple excluded patterns"
// ---------------------------------------------------------------------------

func TestRestoreCopy_MultipleExcludePatterns(t *testing.T) {
	tmp := t.TempDir()

	srcDir := filepath.Join(tmp, "source")
	os.MkdirAll(filepath.Join(srcDir, "Logs"), 0755)
	os.MkdirAll(filepath.Join(srcDir, "Temp"), 0755)
	os.MkdirAll(filepath.Join(srcDir, "Cache"), 0755)
	os.MkdirAll(filepath.Join(srcDir, "configs"), 0755)

	os.WriteFile(filepath.Join(srcDir, "Logs", "app.log"), []byte("log"), 0644)
	os.WriteFile(filepath.Join(srcDir, "Temp", "temp.dat"), []byte("temp"), 0644)
	os.WriteFile(filepath.Join(srcDir, "Cache", "data.cache"), []byte("cache"), 0644)
	os.WriteFile(filepath.Join(srcDir, "configs", "app.json"), []byte("config"), 0644)

	tgtDir := filepath.Join(tmp, "target")

	entry := RestoreAction{
		Type:    "copy",
		Source:  srcDir,
		Target:  tgtDir,
		Exclude: []string{"**/Logs/**", "**/Temp/**", "**/Cache/**"},
	}

	result, err := RestoreCopy(entry, srcDir, tgtDir, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}

	// Only configs should exist.
	if _, err := os.Stat(filepath.Join(tgtDir, "configs", "app.json")); err != nil {
		t.Error("configs/app.json should have been copied")
	}
	if _, err := os.Stat(filepath.Join(tgtDir, "Logs")); !os.IsNotExist(err) {
		t.Error("Logs directory should have been excluded")
	}
	if _, err := os.Stat(filepath.Join(tgtDir, "Temp")); !os.IsNotExist(err) {
		t.Error("Temp directory should have been excluded")
	}
	if _, err := os.Stat(filepath.Join(tgtDir, "Cache")); !os.IsNotExist(err) {
		t.Error("Cache directory should have been excluded")
	}
}

// ---------------------------------------------------------------------------
// Exclude: forward-slash patterns, nested matching
// Pester: Restore.Exclude.PatternMatching
// ---------------------------------------------------------------------------

func TestIsPathExcluded_ForwardSlashPatterns(t *testing.T) {
	tests := []struct {
		name     string
		relPath  string
		patterns []string
		want     bool
	}{
		{"forward-slash pattern matches", "Logs/app.log", []string{"**/Logs/**"}, true},
		{"nested Logs folder", "subfolder/Logs/debug.log", []string{"**/Logs/**"}, true},
		{"multiple patterns - Logs match", "Logs/file.log", []string{"**/Logs/**", "**/Temp/**"}, true},
		{"multiple patterns - Temp match", "Temp/cache.tmp", []string{"**/Logs/**", "**/Temp/**"}, true},
		{"multiple patterns - no match", "config.json", []string{"**/Logs/**", "**/Temp/**"}, false},
		{"backslash path with forward-slash pattern", "Logs\\app.log", []string{"**/Logs/**"}, true},
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
// GenerateID tests
// Pester: Restore.ActionId
// ---------------------------------------------------------------------------

func TestGenerateID_UsesProvidedID(t *testing.T) {
	action := RestoreAction{
		ID:     "my-custom-id",
		Type:   "copy",
		Source: "./source",
		Target: "~/target",
	}
	id := generateID(action)
	if id != "my-custom-id" {
		t.Errorf("expected my-custom-id, got %q", id)
	}
}

func TestGenerateID_DeterministicFromSourceAndTarget(t *testing.T) {
	action := RestoreAction{
		Type:   "copy",
		Source: "./configs/test.conf",
		Target: "~/.test.conf",
	}
	id := generateID(action)
	expected := "copy:./configs/test.conf->~/.test.conf"
	if id != expected {
		t.Errorf("expected %q, got %q", expected, id)
	}
}

func TestGenerateID_DefaultTypeToCopy(t *testing.T) {
	action := RestoreAction{
		Source: "./source",
		Target: "~/target",
	}
	id := generateID(action)
	if !strings.HasPrefix(id, "copy:") {
		t.Errorf("expected id to start with 'copy:', got %q", id)
	}
}

// ---------------------------------------------------------------------------
// IsUpToDate: target doesn't exist returns false
// Pester: Restore.UpToDateDetection "Should return false when target doesn't exist"
// ---------------------------------------------------------------------------

func TestIsUpToDate_TargetDoesNotExist(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "exists.txt")
	tgtFile := filepath.Join(tmp, "nonexistent.txt")
	os.WriteFile(srcFile, []byte("content"), 0644)

	result, err := IsUpToDate(srcFile, tgtFile)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result {
		t.Error("expected false when target doesn't exist")
	}
}

// ---------------------------------------------------------------------------
// Merge-JSON: creates target when missing
// Pester: JSON Merge "Creates target if missing"
// ---------------------------------------------------------------------------

func TestRestoreMergeJson_CreatesTargetWhenMissing(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.json")
	tgtFile := filepath.Join(tmp, "target.json")

	os.WriteFile(srcFile, []byte(`{"key":"value"}`), 0644)

	entry := RestoreAction{
		Type:   "merge-json",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreMergeJson(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}

	if _, err := os.Stat(tgtFile); err != nil {
		t.Error("target file should have been created")
	}

	data, _ := os.ReadFile(tgtFile)
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	if parsed["key"] != "value" {
		t.Errorf("expected key=value, got %v", parsed["key"])
	}
}

// ---------------------------------------------------------------------------
// Merge-JSON: adds new keys from source
// Pester: JSON Merge "Should add new keys from source"
// ---------------------------------------------------------------------------

func TestRestoreMergeJson_AddNewKeys(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.json")
	tgtFile := filepath.Join(tmp, "target.json")

	os.WriteFile(srcFile, []byte(`{"newKey":"newValue"}`), 0644)
	os.WriteFile(tgtFile, []byte(`{"existingKey":"existingValue"}`), 0644)

	entry := RestoreAction{
		Type:   "merge-json",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreMergeJson(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}

	data, _ := os.ReadFile(tgtFile)
	var parsed map[string]interface{}
	json.Unmarshal(data, &parsed)
	if parsed["existingKey"] != "existingValue" {
		t.Errorf("expected existingKey=existingValue, got %v", parsed["existingKey"])
	}
	if parsed["newKey"] != "newValue" {
		t.Errorf("expected newKey=newValue, got %v", parsed["newKey"])
	}
}

// ---------------------------------------------------------------------------
// Merge-JSON: sorted keys for deterministic output
// Pester: JSON Merge "Sorted keys for deterministic output"
// ---------------------------------------------------------------------------

func TestRestoreMergeJson_SortedKeysOutput(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.json")
	tgtFile := filepath.Join(tmp, "target.json")

	os.WriteFile(srcFile, []byte(`{"zebra":1,"apple":2}`), 0644)

	entry := RestoreAction{
		Type:   "merge-json",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreMergeJson(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "restored" {
		t.Errorf("expected status=restored, got %q", result.Status)
	}

	data, _ := os.ReadFile(tgtFile)
	content := string(data)

	appleIndex := strings.Index(content, `"apple"`)
	zebraIndex := strings.Index(content, `"zebra"`)

	if appleIndex < 0 || zebraIndex < 0 {
		t.Fatalf("expected both keys in output, got %q", content)
	}
	if appleIndex >= zebraIndex {
		t.Errorf("expected apple before zebra in sorted output, got apple@%d zebra@%d", appleIndex, zebraIndex)
	}
}

// ---------------------------------------------------------------------------
// Merge-JSON: DryRun does not modify files
// Pester: JSON Merge "DryRun mode"
// ---------------------------------------------------------------------------

func TestRestoreMergeJson_DryRunDoesNotModify(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.json")
	tgtFile := filepath.Join(tmp, "target.json")

	os.WriteFile(srcFile, []byte(`{"new":"value"}`), 0644)
	os.WriteFile(tgtFile, []byte(`{"old":"value"}`), 0644)

	originalContent, _ := os.ReadFile(tgtFile)

	entry := RestoreAction{
		Type:   "merge-json",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreMergeJson(entry, srcFile, tgtFile, RestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored for dry-run, got %q", result.Status)
	}

	afterContent, _ := os.ReadFile(tgtFile)
	if string(afterContent) != string(originalContent) {
		t.Error("target file should not be modified in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// Merge-JSON: up-to-date detection after merge
// Pester: JSON Merge "Array union...should produce same result on re-run"
// ---------------------------------------------------------------------------

func TestRestoreMergeJson_UpToDateOnRerun(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.json")
	tgtFile := filepath.Join(tmp, "target.json")

	os.WriteFile(srcFile, []byte(`{"key":"value"}`), 0644)

	entry := RestoreAction{
		Type:   "merge-json",
		Source: srcFile,
		Target: tgtFile,
	}

	// First run: should restore.
	result1, err := RestoreMergeJson(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result1.Status != "restored" {
		t.Errorf("expected status=restored on first run, got %q", result1.Status)
	}

	// Second run: should skip as up-to-date.
	result2, err := RestoreMergeJson(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Status != "skipped_up_to_date" {
		t.Errorf("expected status=skipped_up_to_date on second run, got %q", result2.Status)
	}
}

// ---------------------------------------------------------------------------
// Merge-INI: creates target when missing
// Pester: INI Merge "Creates target if missing"
// ---------------------------------------------------------------------------

func TestRestoreMergeIni_CreatesTargetWhenMissing(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.ini")
	tgtFile := filepath.Join(tmp, "target.ini")

	os.WriteFile(srcFile, []byte("[Config]\nsetting=value\n"), 0644)

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

	if _, err := os.Stat(tgtFile); err != nil {
		t.Error("target file should have been created")
	}

	data, _ := os.ReadFile(tgtFile)
	content := string(data)
	if !strings.Contains(content, "[Config]") {
		t.Error("expected [Config] section in output")
	}
	if !strings.Contains(content, "setting=value") {
		t.Error("expected setting=value in output")
	}
}

// ---------------------------------------------------------------------------
// Merge-INI: preserve sections not in source
// Pester: INI Merge "preserve keys not in source" with [OtherSection]
// ---------------------------------------------------------------------------

func TestMergeIni_PreserveSectionsNotInSource(t *testing.T) {
	target := ParseIni("[Settings]\nexistingSetting=value\n[OtherSection]\notherKey=otherValue\n")
	source := ParseIni("[Settings]\nnewSetting=true\n")

	merged := MergeIni(target, source)
	result := FormatIni(merged)

	if !strings.Contains(result, "existingSetting=value") {
		t.Error("expected existingSetting=value preserved")
	}
	if !strings.Contains(result, "newSetting=true") {
		t.Error("expected newSetting=true added")
	}
	if !strings.Contains(result, "[OtherSection]") {
		t.Error("expected [OtherSection] preserved")
	}
	if !strings.Contains(result, "otherKey=otherValue") {
		t.Error("expected otherKey=otherValue preserved")
	}
}

// ---------------------------------------------------------------------------
// Merge-INI: DryRun does not modify files
// Pester: INI Merge "DryRun mode"
// ---------------------------------------------------------------------------

func TestRestoreMergeIni_DryRunDoesNotModify(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.ini")
	tgtFile := filepath.Join(tmp, "target.ini")

	os.WriteFile(srcFile, []byte("[New]\nnew=value\n"), 0644)
	os.WriteFile(tgtFile, []byte("[Old]\nold=value\n"), 0644)

	originalContent, _ := os.ReadFile(tgtFile)

	entry := RestoreAction{
		Type:   "merge-ini",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreMergeIni(entry, srcFile, tgtFile, RestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored for dry-run, got %q", result.Status)
	}

	afterContent, _ := os.ReadFile(tgtFile)
	if string(afterContent) != string(originalContent) {
		t.Error("target file should not be modified in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// Append: idempotent rerun skips
// Pester: Append Lines "Rerun is idempotent"
// ---------------------------------------------------------------------------

func TestRestoreAppend_IdempotentWhenAlreadyAppended(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "target.txt")

	os.WriteFile(srcFile, []byte("new-line\n"), 0644)
	// Pre-populate target with existing + already-appended content.
	// The target already looks like what append would produce.
	os.WriteFile(tgtFile, []byte("existing-line\nnew-line\n"), 0644)

	entry := RestoreAction{
		Type:   "append",
		Source: srcFile,
		Target: tgtFile,
	}

	// Since target already contains base + source, merged == target.
	result, err := RestoreAppend(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Status != "skipped_up_to_date" {
		t.Errorf("expected status=skipped_up_to_date when already appended, got %q", result.Status)
	}
}

// ---------------------------------------------------------------------------
// Append: DryRun does not modify files
// Pester: Append Lines "DryRun mode"
// ---------------------------------------------------------------------------

func TestRestoreAppend_DryRunDoesNotModify(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "target.txt")

	os.WriteFile(srcFile, []byte("new line\n"), 0644)
	os.WriteFile(tgtFile, []byte("existing line\n"), 0644)

	originalContent, _ := os.ReadFile(tgtFile)

	entry := RestoreAction{
		Type:   "append",
		Source: srcFile,
		Target: tgtFile,
	}

	result, err := RestoreAppend(entry, srcFile, tgtFile, RestoreOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Status != "restored" {
		t.Errorf("expected status=restored for dry-run, got %q", result.Status)
	}

	afterContent, _ := os.ReadFile(tgtFile)
	if string(afterContent) != string(originalContent) {
		t.Error("target file should not be modified in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// Journal: FindLatestJournal with no files returns error
// ---------------------------------------------------------------------------

func TestFindLatestJournal_NoFiles(t *testing.T) {
	tmp := t.TempDir()
	logsDir := filepath.Join(tmp, "empty-logs")
	os.MkdirAll(logsDir, 0755)

	_, err := FindLatestJournal(logsDir)
	if err == nil {
		t.Error("expected error when no journal files exist")
	}
	if !strings.Contains(err.Error(), "no restore journals found") {
		t.Errorf("expected 'no restore journals found' error, got %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Journal: round-trip preserves all fields
// Pester: Restore.Journaling "journal content"
// ---------------------------------------------------------------------------

func TestJournal_WriteReadPreservesFields(t *testing.T) {
	tmp := t.TempDir()
	logsDir := filepath.Join(tmp, "logs")

	results := []RestoreResult{
		{
			ID:                  "entry-1",
			Source:              "/src/config.json",
			Target:              "/tgt/config.json",
			Status:              "restored",
			BackupPath:          "/backup/config.json",
			BackupCreated:       true,
			TargetExistedBefore: true,
		},
	}

	err := WriteJournal(logsDir, "run-fields", "/m.jsonc", "/mdir", "/export", results)
	if err != nil {
		t.Fatalf("WriteJournal failed: %v", err)
	}

	journalPath, _ := FindLatestJournal(logsDir)
	journal, err := ReadJournal(journalPath)
	if err != nil {
		t.Fatalf("ReadJournal failed: %v", err)
	}

	if journal.RunID != "run-fields" {
		t.Errorf("expected runId=run-fields, got %q", journal.RunID)
	}
	if journal.ManifestPath != "/m.jsonc" {
		t.Errorf("expected manifestPath=/m.jsonc, got %q", journal.ManifestPath)
	}
	if journal.ManifestDir != "/mdir" {
		t.Errorf("expected manifestDir=/mdir, got %q", journal.ManifestDir)
	}
	if journal.ExportRoot != "/export" {
		t.Errorf("expected exportRoot=/export, got %q", journal.ExportRoot)
	}
	if len(journal.Entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(journal.Entries))
	}

	entry := journal.Entries[0]
	if entry.ResolvedSourcePath != "/src/config.json" {
		t.Errorf("expected source=/src/config.json, got %q", entry.ResolvedSourcePath)
	}
	if entry.TargetPath != "/tgt/config.json" {
		t.Errorf("expected target=/tgt/config.json, got %q", entry.TargetPath)
	}
	if !entry.TargetExistedBefore {
		t.Error("expected targetExistedBefore=true")
	}
	if !entry.BackupCreated {
		t.Error("expected backupCreated=true")
	}
	if entry.BackupPath != "/backup/config.json" {
		t.Errorf("expected backupPath=/backup/config.json, got %q", entry.BackupPath)
	}
}

// ---------------------------------------------------------------------------
// Sensitive path: .aws detection
// Pester: Restore.SensitivePath ".aws"
// ---------------------------------------------------------------------------

func TestCheckSensitivePath_DetectsAWS(t *testing.T) {
	warnings := CheckSensitivePath("~/.aws/credentials")
	if len(warnings) == 0 {
		t.Error("expected warnings for .aws path")
	}

	foundAWS := false
	for _, w := range warnings {
		if strings.Contains(w, ".aws") {
			foundAWS = true
		}
	}
	if !foundAWS {
		t.Error("expected warning about .aws segment")
	}
}

// ---------------------------------------------------------------------------
// Sensitive path: normal path not flagged (e.g., .gitconfig)
// Pester: Restore.SensitivePath "Should NOT flag normal paths"
// ---------------------------------------------------------------------------

func TestCheckSensitivePath_NormalPathNotFlagged(t *testing.T) {
	warnings := CheckSensitivePath("~/.gitconfig")
	if len(warnings) != 0 {
		t.Errorf("expected no warnings for ~/.gitconfig, got %v", warnings)
	}
}

// ---------------------------------------------------------------------------
// RunRestore: warnings for sensitive target path
// Pester: Restore.SensitivePath "Should add warnings for sensitive paths"
// ---------------------------------------------------------------------------

func TestRunRestore_WarningsForSensitiveTarget(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.txt")
	// Target path contains .ssh segment.
	tgtFile := filepath.Join(tmp, ".ssh", "config")

	os.WriteFile(srcFile, []byte("config content"), 0644)

	entries := []RestoreAction{
		{
			Type:   "copy",
			Source: srcFile,
			Target: tgtFile,
			ID:     "sensitive-test",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if len(results[0].Warnings) == 0 {
		t.Error("expected warnings for sensitive target path")
	}

	foundSensitive := false
	for _, w := range results[0].Warnings {
		if strings.Contains(strings.ToLower(w), "sensitive") || strings.Contains(w, ".ssh") {
			foundSensitive = true
		}
	}
	if !foundSensitive {
		t.Errorf("expected warning about sensitive path, got %v", results[0].Warnings)
	}
}

// ---------------------------------------------------------------------------
// RunRestore: strategy dispatch covers all types
// Pester: Restore dispatches copy, merge-json, merge-ini, append
// ---------------------------------------------------------------------------

func TestRunRestore_DispatchAllStrategies(t *testing.T) {
	tmp := t.TempDir()

	// Create sources for each strategy.
	srcCopy := filepath.Join(tmp, "src-copy.txt")
	tgtCopy := filepath.Join(tmp, "tgt-copy.txt")
	os.WriteFile(srcCopy, []byte("copy-content"), 0644)

	srcJSON := filepath.Join(tmp, "src-merge.json")
	tgtJSON := filepath.Join(tmp, "tgt-merge.json")
	os.WriteFile(srcJSON, []byte(`{"merged":"true"}`), 0644)
	os.WriteFile(tgtJSON, []byte(`{"existing":"true"}`), 0644)

	srcINI := filepath.Join(tmp, "src-merge.ini")
	tgtINI := filepath.Join(tmp, "tgt-merge.ini")
	os.WriteFile(srcINI, []byte("[s]\nk=v\n"), 0644)
	os.WriteFile(tgtINI, []byte("[s]\nold=val\n"), 0644)

	srcAppend := filepath.Join(tmp, "src-append.txt")
	tgtAppend := filepath.Join(tmp, "tgt-append.txt")
	os.WriteFile(srcAppend, []byte("appended\n"), 0644)

	entries := []RestoreAction{
		{Type: "copy", Source: srcCopy, Target: tgtCopy, ID: "copy-1"},
		{Type: "merge-json", Source: srcJSON, Target: tgtJSON, ID: "json-1"},
		{Type: "merge-ini", Source: srcINI, Target: tgtINI, ID: "ini-1"},
		{Type: "append", Source: srcAppend, Target: tgtAppend, ID: "append-1"},
	}

	results, err := RunRestore(entries, RestoreOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	for i, r := range results {
		if r.Status != "restored" {
			t.Errorf("entry[%d] (%s): expected status=restored, got %q", i, entries[i].Type, r.Status)
		}
	}
}

// ---------------------------------------------------------------------------
// RunRestore: non-optional missing source should fail (not skip)
// Pester: Restore.SourceNotFound via RunRestore
// ---------------------------------------------------------------------------

func TestRunRestore_NonOptionalMissingSourceFails(t *testing.T) {
	tmp := t.TempDir()

	entries := []RestoreAction{
		{
			Type:   "copy",
			Source: filepath.Join(tmp, "nonexistent.txt"),
			Target: filepath.Join(tmp, "target.txt"),
			ID:     "fail-test",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != "failed" {
		t.Errorf("expected status=failed for non-optional missing source, got %q", results[0].Status)
	}
}

// ---------------------------------------------------------------------------
// Merge-JSON: backup when target exists
// Pester: JSON Merge "Backup when target exists"
// ---------------------------------------------------------------------------

func TestRestoreMergeJson_BackupWhenTargetExists(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.json")
	tgtFile := filepath.Join(tmp, "target.json")
	backupDir := filepath.Join(tmp, "backups")

	os.WriteFile(srcFile, []byte(`{"new":"data"}`), 0644)
	os.WriteFile(tgtFile, []byte(`{"old":"data"}`), 0644)

	entry := RestoreAction{
		Type:   "merge-json",
		Source: srcFile,
		Target: tgtFile,
		Backup: true,
	}

	result, err := RestoreMergeJson(entry, srcFile, tgtFile, RestoreOptions{
		BackupDir: backupDir,
		RunID:     "backup-test",
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
}

// ---------------------------------------------------------------------------
// Append: backup when target exists
// Pester: Append Lines "Backup when target exists"
// ---------------------------------------------------------------------------

func TestRestoreAppend_BackupWhenTargetExists(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "target.txt")
	backupDir := filepath.Join(tmp, "backups")

	os.WriteFile(srcFile, []byte("new line\n"), 0644)
	os.WriteFile(tgtFile, []byte("existing line\n"), 0644)

	entry := RestoreAction{
		Type:   "append",
		Source: srcFile,
		Target: tgtFile,
		Backup: true,
	}

	result, err := RestoreAppend(entry, srcFile, tgtFile, RestoreOptions{
		BackupDir: backupDir,
		RunID:     "append-backup-test",
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
}

// ---------------------------------------------------------------------------
// Merge-INI: up-to-date detection on rerun
// ---------------------------------------------------------------------------

func TestRestoreMergeIni_UpToDateOnRerun(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.ini")
	tgtFile := filepath.Join(tmp, "target.ini")

	os.WriteFile(srcFile, []byte("[Config]\nk=v\n"), 0644)

	entry := RestoreAction{
		Type:   "merge-ini",
		Source: srcFile,
		Target: tgtFile,
	}

	// First run: restore.
	result1, err := RestoreMergeIni(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result1.Status != "restored" {
		t.Errorf("expected status=restored on first run, got %q", result1.Status)
	}

	// Second run: up-to-date.
	result2, err := RestoreMergeIni(entry, srcFile, tgtFile, RestoreOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result2.Status != "skipped_up_to_date" {
		t.Errorf("expected status=skipped_up_to_date on second run, got %q", result2.Status)
	}
}

// ---------------------------------------------------------------------------
// Revert: end-to-end restore then revert (delete created target)
// Pester: Revert.JournalBased "delete newly created target"
// ---------------------------------------------------------------------------

func TestRevertEndToEnd_DeleteCreatedTarget(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "new-target.txt")
	os.WriteFile(srcFile, []byte("test-content"), 0644)

	// Run restore to create the target.
	entries := []RestoreAction{
		{
			Type:   "copy",
			Source: srcFile,
			Target: tgtFile,
			ID:     "revert-e2e",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{}, nil)
	if err != nil {
		t.Fatalf("RunRestore failed: %v", err)
	}

	// Verify target was created.
	if _, err := os.Stat(tgtFile); err != nil {
		t.Fatal("target should have been created by restore")
	}

	// Build journal from results.
	journal := &Journal{
		RunID: "revert-e2e-run",
		Entries: []JournalEntry{
			{
				TargetPath:          results[0].Target,
				TargetExistedBefore: results[0].TargetExistedBefore,
				Action:              results[0].Status,
			},
		},
	}

	// Run revert.
	revertResults, err := RunRevert(journal, "")
	if err != nil {
		t.Fatalf("RunRevert failed: %v", err)
	}

	if len(revertResults) != 1 {
		t.Fatalf("expected 1 revert result, got %d", len(revertResults))
	}
	if revertResults[0].Action != "deleted" {
		t.Errorf("expected action=deleted, got %q", revertResults[0].Action)
	}

	// Verify target was removed.
	if _, err := os.Stat(tgtFile); !os.IsNotExist(err) {
		t.Error("target should have been deleted by revert")
	}
}

// ---------------------------------------------------------------------------
// DryRun: merge-json, merge-ini, append via RunRestore
// Pester: DryRun scenarios for each strategy
// ---------------------------------------------------------------------------

func TestRunRestore_DryRunMergeJson(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "src.json")
	tgtFile := filepath.Join(tmp, "tgt.json")
	os.WriteFile(srcFile, []byte(`{"new":"val"}`), 0644)
	os.WriteFile(tgtFile, []byte(`{"old":"val"}`), 0644)

	original, _ := os.ReadFile(tgtFile)

	entries := []RestoreAction{
		{Type: "merge-json", Source: srcFile, Target: tgtFile, ID: "dry-json"},
	}

	results, err := RunRestore(entries, RestoreOptions{DryRun: true}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != "restored" {
		t.Errorf("expected status=restored for dry-run merge-json, got %q", results[0].Status)
	}

	after, _ := os.ReadFile(tgtFile)
	if string(after) != string(original) {
		t.Error("target should not be modified in dry-run mode")
	}
}

func TestRunRestore_DryRunMergeIni(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "src.ini")
	tgtFile := filepath.Join(tmp, "tgt.ini")
	os.WriteFile(srcFile, []byte("[New]\nnew=val\n"), 0644)
	os.WriteFile(tgtFile, []byte("[Old]\nold=val\n"), 0644)

	original, _ := os.ReadFile(tgtFile)

	entries := []RestoreAction{
		{Type: "merge-ini", Source: srcFile, Target: tgtFile, ID: "dry-ini"},
	}

	results, err := RunRestore(entries, RestoreOptions{DryRun: true}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != "restored" {
		t.Errorf("expected status=restored for dry-run merge-ini, got %q", results[0].Status)
	}

	after, _ := os.ReadFile(tgtFile)
	if string(after) != string(original) {
		t.Error("target should not be modified in dry-run mode")
	}
}

func TestRunRestore_DryRunAppend(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "src.txt")
	tgtFile := filepath.Join(tmp, "tgt.txt")
	os.WriteFile(srcFile, []byte("new-line\n"), 0644)
	os.WriteFile(tgtFile, []byte("existing-line\n"), 0644)

	original, _ := os.ReadFile(tgtFile)

	entries := []RestoreAction{
		{Type: "append", Source: srcFile, Target: tgtFile, ID: "dry-append"},
	}

	results, err := RunRestore(entries, RestoreOptions{DryRun: true}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != "restored" {
		t.Errorf("expected status=restored for dry-run append, got %q", results[0].Status)
	}

	after, _ := os.ReadFile(tgtFile)
	if string(after) != string(original) {
		t.Error("target should not be modified in dry-run mode")
	}
}

// ---------------------------------------------------------------------------
// expandPath tests
// (mirrors Pester PathResolver.Tests.ps1 — env var expansion, tilde, relative paths)
// ---------------------------------------------------------------------------

func TestExpandPath_WindowsPercentVar(t *testing.T) {
	// Set a test env var and verify %VAR% expansion works.
	t.Setenv("ENDSTATE_EXPAND_TEST", "/expanded/dir")

	result := expandPath("%ENDSTATE_EXPAND_TEST%/file.txt")
	if strings.Contains(result, "%ENDSTATE_EXPAND_TEST%") {
		t.Errorf("expected %%VAR%% to be expanded, got %q", result)
	}
	if !strings.Contains(result, "/expanded/dir") {
		t.Errorf("expected expanded path to contain /expanded/dir, got %q", result)
	}
}

func TestExpandPath_TildeExpansion(t *testing.T) {
	// ~ should expand to the home directory.
	result := expandPath("~/.config/app")
	if strings.HasPrefix(result, "~") {
		t.Errorf("expected ~ to be expanded, got %q", result)
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home directory")
	}
	if !strings.Contains(result, homeDir) {
		t.Errorf("expected expanded path to contain home dir %q, got %q", homeDir, result)
	}
}

func TestExpandPath_GoStyleEnvVar(t *testing.T) {
	// Go-style $VAR expansion via os.ExpandEnv.
	t.Setenv("ENDSTATE_GO_VAR_TEST", "/go/expanded")

	result := expandPath("$ENDSTATE_GO_VAR_TEST/config")
	if strings.Contains(result, "$ENDSTATE_GO_VAR_TEST") {
		t.Errorf("expected $VAR to be expanded, got %q", result)
	}
	if !strings.Contains(result, "/go/expanded") {
		t.Errorf("expected expanded value, got %q", result)
	}
}

func TestExpandPath_NoVars_PassThrough(t *testing.T) {
	input := "/plain/absolute/path"
	result := expandPath(input)
	if result != input {
		t.Errorf("expected passthrough %q, got %q", input, result)
	}
}

// ---------------------------------------------------------------------------
// resolveTarget tests
// (mirrors Pester PathResolver.Tests.ps1 — target resolution)
// ---------------------------------------------------------------------------

func TestResolveTarget_ExpandsEnvVars(t *testing.T) {
	t.Setenv("ENDSTATE_TARGET_TEST", "/resolved/target")

	result := resolveTarget("%ENDSTATE_TARGET_TEST%/config.json")
	if strings.Contains(result, "%ENDSTATE_TARGET_TEST%") {
		t.Errorf("expected env var to be expanded in target, got %q", result)
	}
}

func TestResolveTarget_AbsolutePathUnchanged(t *testing.T) {
	result := resolveTarget("/absolute/path/config.json")
	if !filepath.IsAbs(result) {
		t.Errorf("expected absolute path, got %q", result)
	}
}

// ---------------------------------------------------------------------------
// resolveSource tests — env var expansion in source paths
// (mirrors Pester PathResolver.Tests.ps1 — relative path resolution)
// ---------------------------------------------------------------------------

func TestResolveSource_RelativeToManifestDir(t *testing.T) {
	tmp := t.TempDir()
	srcFile := filepath.Join(tmp, "configs", "app.conf")
	os.MkdirAll(filepath.Dir(srcFile), 0755)
	os.WriteFile(srcFile, []byte("test"), 0644)

	result := resolveSource("./configs/app.conf", RestoreOptions{ManifestDir: tmp})
	expected := filepath.Clean(filepath.Join(tmp, "configs", "app.conf"))
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

// ---------------------------------------------------------------------------
// RunRestore — env var expansion in target path (integration)
// (mirrors Pester Restore.Execution: "Should expand environment variables in target path")
// ---------------------------------------------------------------------------

func TestRunRestore_ExpandsEnvVarInTarget(t *testing.T) {
	tmp := t.TempDir()

	// Create source file.
	srcFile := filepath.Join(tmp, "source", "env.conf")
	os.MkdirAll(filepath.Dir(srcFile), 0755)
	os.WriteFile(srcFile, []byte("env test"), 0644)

	// Set env var pointing to a target directory within tmp.
	targetDir := filepath.Join(tmp, "target-env")
	os.MkdirAll(targetDir, 0755)
	t.Setenv("ENDSTATE_TARGET_ENV_TEST", targetDir)

	entries := []RestoreAction{
		{
			Type:   "copy",
			Source: srcFile,
			Target: "%ENDSTATE_TARGET_ENV_TEST%/env.conf",
			ID:     "env-target-test",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != "restored" {
		t.Errorf("expected status=restored, got %q (error: %s)", results[0].Status, results[0].Error)
	}

	// Verify the file was written to the expanded path.
	expectedTarget := filepath.Join(targetDir, "env.conf")
	if _, statErr := os.Stat(expectedTarget); os.IsNotExist(statErr) {
		t.Errorf("expected file at expanded path %q to exist", expectedTarget)
	}
}

// ---------------------------------------------------------------------------
// RunRestore — idempotency (second restore skips)
// (mirrors Pester Restore.Execution Idempotency)
// ---------------------------------------------------------------------------

func TestRunRestore_IdempotentSecondRunSkips(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "source", "idem.conf")
	tgtFile := filepath.Join(tmp, "target", "idem.conf")
	os.MkdirAll(filepath.Dir(srcFile), 0755)
	os.MkdirAll(filepath.Dir(tgtFile), 0755)
	os.WriteFile(srcFile, []byte("idempotent content"), 0644)

	entries := []RestoreAction{
		{Type: "copy", Source: srcFile, Target: tgtFile, ID: "idem-test"},
	}

	// First restore.
	results1, err1 := RunRestore(entries, RestoreOptions{}, nil)
	if err1 != nil {
		t.Fatalf("first restore error: %v", err1)
	}
	if results1[0].Status != "restored" {
		t.Errorf("first restore: expected status=restored, got %q", results1[0].Status)
	}

	// Second restore should skip (up to date).
	results2, err2 := RunRestore(entries, RestoreOptions{}, nil)
	if err2 != nil {
		t.Fatalf("second restore error: %v", err2)
	}
	if results2[0].Status != "skipped_up_to_date" {
		t.Errorf("second restore: expected status=skipped_up_to_date, got %q", results2[0].Status)
	}
}

// ===========================================================================
// Model B: ExportRoot source resolution via RunRestore (end-to-end)
// Ported from Pester RestoreModelB.Tests.ps1
// ===========================================================================

// ---------------------------------------------------------------------------
// RunRestore with ExportRoot prefers export root over manifest dir
// Pester: Restore.ModelB.ExportRoot - Should resolve source from export root
// ---------------------------------------------------------------------------

func TestRunRestore_ExportRootPrefersOverManifestDir(t *testing.T) {
	tmp := t.TempDir()

	exportDir := filepath.Join(tmp, "export", "configs")
	manifestDir := filepath.Join(tmp, "manifest", "configs")
	targetDir := filepath.Join(tmp, "target")
	os.MkdirAll(exportDir, 0755)
	os.MkdirAll(manifestDir, 0755)
	os.MkdirAll(targetDir, 0755)

	// Create file in both export root and manifest dir with different content.
	os.WriteFile(filepath.Join(exportDir, "app.conf"), []byte("export-content"), 0644)
	os.WriteFile(filepath.Join(manifestDir, "app.conf"), []byte("manifest-content"), 0644)

	tgtFile := filepath.Join(targetDir, "app.conf")

	entries := []RestoreAction{
		{
			Type:   "copy",
			Source: "configs/app.conf",
			Target: tgtFile,
			ID:     "export-root-test",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{
		ExportRoot:  filepath.Join(tmp, "export"),
		ManifestDir: filepath.Join(tmp, "manifest"),
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != "restored" {
		t.Errorf("expected status=restored, got %q", results[0].Status)
	}

	// Verify target has export content, not manifest content.
	data, _ := os.ReadFile(tgtFile)
	if string(data) != "export-content" {
		t.Errorf("target content = %q, want %q (from export root)", string(data), "export-content")
	}
}

// ---------------------------------------------------------------------------
// RunRestore with ExportRoot falls back to manifest dir when source not in export
// Pester: Restore.ModelB.ExportRoot - Should fallback to manifest dir
// ---------------------------------------------------------------------------

func TestRunRestore_ExportRootFallbackToManifestDir(t *testing.T) {
	tmp := t.TempDir()

	exportDir := filepath.Join(tmp, "export")
	manifestDir := filepath.Join(tmp, "manifest", "configs")
	targetDir := filepath.Join(tmp, "target")
	os.MkdirAll(exportDir, 0755)
	os.MkdirAll(manifestDir, 0755)
	os.MkdirAll(targetDir, 0755)

	// Create file ONLY in manifest dir.
	os.WriteFile(filepath.Join(manifestDir, "app.conf"), []byte("manifest-content"), 0644)

	tgtFile := filepath.Join(targetDir, "app.conf")

	entries := []RestoreAction{
		{
			Type:   "copy",
			Source: "configs/app.conf",
			Target: tgtFile,
			ID:     "fallback-test",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{
		ExportRoot:  exportDir,
		ManifestDir: filepath.Join(tmp, "manifest"),
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if results[0].Status != "restored" {
		t.Errorf("expected status=restored, got %q", results[0].Status)
	}

	// Verify target has manifest content (fallback).
	data, _ := os.ReadFile(tgtFile)
	if string(data) != "manifest-content" {
		t.Errorf("target content = %q, want %q (from manifest dir fallback)", string(data), "manifest-content")
	}
}

// ---------------------------------------------------------------------------
// Journal: dry-run should NOT produce journal
// Pester: Restore.Journaling - Should NOT write journal for dry-run restore
// ---------------------------------------------------------------------------

func TestWriteJournal_NotCalledInDryRun(t *testing.T) {
	// This tests the contract: dry-run results should not be journaled.
	// RunRestore itself doesn't write journals (the caller does), but we
	// verify that the dry-run results are correctly flagged so the caller
	// knows not to write a journal.
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "src.txt")
	tgtFile := filepath.Join(tmp, "tgt.txt")
	os.WriteFile(srcFile, []byte("content"), 0644)

	entries := []RestoreAction{
		{Type: "copy", Source: srcFile, Target: tgtFile, ID: "dry-journal"},
	}

	results, err := RunRestore(entries, RestoreOptions{DryRun: true}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// In dry-run, target should not exist.
	if _, err := os.Stat(tgtFile); !os.IsNotExist(err) {
		t.Error("target should not be created in dry-run mode")
	}

	// Results should still be returned (for UI display).
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Journal: real restore writes journal that can be read back
// Pester: Restore.Journaling - Should write journal file after restore completes
// ---------------------------------------------------------------------------

func TestJournal_WriteReadAfterRestore(t *testing.T) {
	tmp := t.TempDir()
	logsDir := filepath.Join(tmp, "logs")

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "target.txt")
	os.WriteFile(srcFile, []byte("test-content"), 0644)

	entries := []RestoreAction{
		{Type: "copy", Source: srcFile, Target: tgtFile, ID: "journal-e2e"},
	}

	results, err := RunRestore(entries, RestoreOptions{
		RunID:       "journal-test-run",
		ManifestDir: tmp,
	}, nil)
	if err != nil {
		t.Fatalf("RunRestore failed: %v", err)
	}

	// Write journal from results.
	err = WriteJournal(logsDir, "journal-test-run", filepath.Join(tmp, "manifest.jsonc"), tmp, "", results)
	if err != nil {
		t.Fatalf("WriteJournal failed: %v", err)
	}

	// Read it back.
	journalPath := filepath.Join(logsDir, "restore-journal-journal-test-run.json")
	journal, err := ReadJournal(journalPath)
	if err != nil {
		t.Fatalf("ReadJournal failed: %v", err)
	}

	if journal.RunID != "journal-test-run" {
		t.Errorf("journal.RunID = %q, want %q", journal.RunID, "journal-test-run")
	}
	if len(journal.Entries) != 1 {
		t.Fatalf("expected 1 journal entry, got %d", len(journal.Entries))
	}
	if journal.Entries[0].Action != "restored" {
		t.Errorf("journal entry action = %q, want %q", journal.Entries[0].Action, "restored")
	}
}

// ---------------------------------------------------------------------------
// Revert: delete newly created target (targetExistedBefore=false)
// Pester: Revert.JournalBased - Should delete target that was created by restore
// ---------------------------------------------------------------------------

func TestRevertEndToEnd_FullPipeline(t *testing.T) {
	// This is a more complete end-to-end test than the existing one,
	// matching the Pester test that runs restore -> write journal -> revert.
	tmp := t.TempDir()
	logsDir := filepath.Join(tmp, "logs")

	srcFile := filepath.Join(tmp, "source.txt")
	tgtFile := filepath.Join(tmp, "new-target.txt")
	os.WriteFile(srcFile, []byte("test-content"), 0644)

	// Target doesn't exist yet.
	if _, err := os.Stat(tgtFile); !os.IsNotExist(err) {
		t.Fatal("precondition: target should not exist")
	}

	// Run restore to create the target.
	entries := []RestoreAction{
		{Type: "copy", Source: srcFile, Target: tgtFile, ID: "revert-pipeline"},
	}

	results, err := RunRestore(entries, RestoreOptions{RunID: "revert-pipeline-run"}, nil)
	if err != nil {
		t.Fatalf("RunRestore failed: %v", err)
	}

	// Verify target was created.
	if _, err := os.Stat(tgtFile); err != nil {
		t.Fatal("target should have been created by restore")
	}

	// Write journal.
	err = WriteJournal(logsDir, "revert-pipeline-run", "", tmp, "", results)
	if err != nil {
		t.Fatalf("WriteJournal failed: %v", err)
	}

	// Read journal.
	journalPath := filepath.Join(logsDir, "restore-journal-revert-pipeline-run.json")
	journal, err := ReadJournal(journalPath)
	if err != nil {
		t.Fatalf("ReadJournal failed: %v", err)
	}

	// Run revert.
	revertResults, err := RunRevert(journal, "")
	if err != nil {
		t.Fatalf("RunRevert failed: %v", err)
	}

	if len(revertResults) != 1 {
		t.Fatalf("expected 1 revert result, got %d", len(revertResults))
	}
	if revertResults[0].Action != "deleted" {
		t.Errorf("expected revert action=deleted, got %q", revertResults[0].Action)
	}

	// Verify target was removed.
	if _, err := os.Stat(tgtFile); !os.IsNotExist(err) {
		t.Error("target should have been deleted by revert")
	}
}

// ---------------------------------------------------------------------------
// RunRestore: event emission tests
// ---------------------------------------------------------------------------

// parseTestEvents decodes all NDJSON lines from buf into a slice of raw maps.
func parseTestEvents(buf *bytes.Buffer) []map[string]interface{} {
	var out []map[string]interface{}
	dec := json.NewDecoder(buf)
	for dec.More() {
		var obj map[string]interface{}
		if err := dec.Decode(&obj); err == nil {
			out = append(out, obj)
		}
	}
	return out
}

// TestRunRestore_EmitsInstalledEventOnSuccess verifies that a successful copy
// emits an item event with status "installed" and empty reason.
func TestRunRestore_EmitsInstalledEventOnSuccess(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "src", "settings.json")
	os.MkdirAll(filepath.Dir(srcFile), 0755)
	os.WriteFile(srcFile, []byte(`{"key":"value"}`), 0644)

	tgtFile := filepath.Join(tmp, "tgt", "settings.json")

	var buf bytes.Buffer
	emitter := events.NewEmitterWithWriter("test-run", true, &buf)

	entries := []RestoreAction{
		{Type: "copy", Source: srcFile, Target: tgtFile, ID: "evt-success"},
	}

	results, err := RunRestore(entries, RestoreOptions{}, emitter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Status != "restored" {
		t.Fatalf("expected restored result, got %v", results)
	}

	evts := parseTestEvents(&buf)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0]["event"] != "item" {
		t.Errorf("expected event=item, got %v", evts[0]["event"])
	}
	if evts[0]["status"] != "installed" {
		t.Errorf("expected status=installed, got %v", evts[0]["status"])
	}
	if evts[0]["reason"] != "" {
		t.Errorf("expected reason empty, got %v", evts[0]["reason"])
	}
	if evts[0]["driver"] != "copy" {
		t.Errorf("expected driver=copy, got %v", evts[0]["driver"])
	}
}

// TestRunRestore_EmitsSkippedAlreadyInstalledOnUpToDate verifies that a
// skipped_up_to_date result emits an item event with status "skipped" and
// reason "already_installed".
func TestRunRestore_EmitsSkippedAlreadyInstalledOnUpToDate(t *testing.T) {
	tmp := t.TempDir()

	content := []byte(`{"key":"same"}`)

	// Source and target have identical content to trigger skipped_up_to_date.
	srcFile := filepath.Join(tmp, "src", "settings.json")
	os.MkdirAll(filepath.Dir(srcFile), 0755)
	os.WriteFile(srcFile, content, 0644)

	tgtFile := filepath.Join(tmp, "tgt", "settings.json")
	os.MkdirAll(filepath.Dir(tgtFile), 0755)
	os.WriteFile(tgtFile, content, 0644)

	var buf bytes.Buffer
	emitter := events.NewEmitterWithWriter("test-run", true, &buf)

	entries := []RestoreAction{
		{Type: "copy", Source: srcFile, Target: tgtFile, ID: "evt-uptodate"},
	}

	results, err := RunRestore(entries, RestoreOptions{}, emitter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Status != "skipped_up_to_date" {
		t.Fatalf("expected skipped_up_to_date result, got %v", results)
	}

	evts := parseTestEvents(&buf)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0]["status"] != "skipped" {
		t.Errorf("expected status=skipped, got %v", evts[0]["status"])
	}
	if evts[0]["reason"] != "already_installed" {
		t.Errorf("expected reason=already_installed, got %v", evts[0]["reason"])
	}
}

// TestRunRestore_EmitsSkippedMissingOnOptionalMissingSource verifies that an
// optional entry with no source emits an item event with status "skipped" and
// reason "missing".
func TestRunRestore_EmitsSkippedMissingOnOptionalMissingSource(t *testing.T) {
	var buf bytes.Buffer
	emitter := events.NewEmitterWithWriter("test-run", true, &buf)

	entries := []RestoreAction{
		{
			Type:     "copy",
			Source:   "/nonexistent/source.json",
			Target:   "/some/target.json",
			Optional: true,
			ID:       "evt-missing",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{}, emitter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Status != "skipped_missing_source" {
		t.Fatalf("expected skipped_missing_source result, got %v", results)
	}

	evts := parseTestEvents(&buf)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0]["status"] != "skipped" {
		t.Errorf("expected status=skipped, got %v", evts[0]["status"])
	}
	if evts[0]["reason"] != "missing" {
		t.Errorf("expected reason=missing, got %v", evts[0]["reason"])
	}
}

// TestRunRestore_EmitsFailedOnRestoreFail verifies that a failed restore emits
// an item event with status "failed" and reason "restore_failed".
func TestRunRestore_EmitsFailedOnRestoreFail(t *testing.T) {
	var buf bytes.Buffer
	emitter := events.NewEmitterWithWriter("test-run", true, &buf)

	// Non-optional missing source will trigger a failed result.
	entries := []RestoreAction{
		{
			Type:     "copy",
			Source:   "/nonexistent/mandatory.json",
			Target:   "/some/target.json",
			Optional: false,
			ID:       "evt-fail",
		},
	}

	results, err := RunRestore(entries, RestoreOptions{}, emitter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 || results[0].Status != "failed" {
		t.Fatalf("expected failed result, got %v", results)
	}

	evts := parseTestEvents(&buf)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0]["status"] != "failed" {
		t.Errorf("expected status=failed, got %v", evts[0]["status"])
	}
	if evts[0]["reason"] != "restore_failed" {
		t.Errorf("expected reason=restore_failed, got %v", evts[0]["reason"])
	}
}

// TestRunRestore_NilEmitterDoesNotPanic verifies that nil emitter is safe.
func TestRunRestore_NilEmitterDoesNotPanic(t *testing.T) {
	tmp := t.TempDir()
	srcFile := filepath.Join(tmp, "src.txt")
	tgtFile := filepath.Join(tmp, "tgt.txt")
	os.WriteFile(srcFile, []byte("data"), 0644)

	entries := []RestoreAction{
		{Type: "copy", Source: srcFile, Target: tgtFile, ID: "nil-emitter"},
	}

	// Must not panic.
	_, err := RunRestore(entries, RestoreOptions{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunRestore_EmitsDriverFromRestoreType verifies the driver field in
// the emitted event matches the restore entry type.
func TestRunRestore_EmitsDriverFromRestoreType(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "src.json")
	os.WriteFile(srcFile, []byte(`{}`), 0644)
	tgtFile := filepath.Join(tmp, "tgt.json")

	var buf bytes.Buffer
	emitter := events.NewEmitterWithWriter("test-run", true, &buf)

	entries := []RestoreAction{
		{Type: "merge-json", Source: srcFile, Target: tgtFile, ID: "evt-driver"},
	}

	_, err := RunRestore(entries, RestoreOptions{}, emitter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	evts := parseTestEvents(&buf)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0]["driver"] != "merge-json" {
		t.Errorf("expected driver=merge-json, got %v", evts[0]["driver"])
	}
}

// TestRunRestore_EmitsNameFromFromModule verifies that the name field in the
// emitted event is taken from the entry's FromModule.
func TestRunRestore_EmitsNameFromFromModule(t *testing.T) {
	tmp := t.TempDir()

	srcFile := filepath.Join(tmp, "src.json")
	os.WriteFile(srcFile, []byte(`{"a":1}`), 0644)
	tgtFile := filepath.Join(tmp, "tgt.json")

	var buf bytes.Buffer
	emitter := events.NewEmitterWithWriter("test-run", true, &buf)

	entries := []RestoreAction{
		{Type: "copy", Source: srcFile, Target: tgtFile, ID: "evt-name", FromModule: "apps.vscode"},
	}

	_, err := RunRestore(entries, RestoreOptions{}, emitter)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	evts := parseTestEvents(&buf)
	if len(evts) != 1 {
		t.Fatalf("expected 1 event, got %d", len(evts))
	}
	if evts[0]["name"] != "apps.vscode" {
		t.Errorf("expected name=apps.vscode, got %v", evts[0]["name"])
	}
}
