// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
)

func TestMaterializeBuildsDeterministicSnapshotRequiredActionsWithoutMutation(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	writeTestFile(t, filepath.Join(stageRoot, "copy.txt"), "replacement")
	writeTestFile(t, filepath.Join(stageRoot, "merge.json"), `{"new":2}`)
	writeTestFile(t, filepath.Join(stageRoot, "merge.ini"), "[settings]\nnew=2\n")
	writeTestFile(t, filepath.Join(stageRoot, "append.txt"), "beta\ngamma\n")

	copyTarget := filepath.Join(hostRoot, "copy.txt")
	jsonTarget := filepath.Join(hostRoot, "settings.json")
	iniTarget := filepath.Join(hostRoot, "settings.ini")
	appendTarget := filepath.Join(hostRoot, "lines.txt")
	cacheTarget := filepath.Join(hostRoot, "cache")
	writeTestFile(t, copyTarget, "original")
	writeTestFile(t, jsonTarget, `{"old":1}`)
	writeTestFile(t, iniTarget, "[settings]\nold=1\n")
	writeTestFile(t, appendTarget, "alpha\nbeta\n")
	writeTestFile(t, filepath.Join(cacheTarget, "z.tmp"), "z")
	writeTestFile(t, filepath.Join(cacheTarget, "A.tmp"), "a")
	writeTestFile(t, filepath.Join(cacheTarget, "keep.txt"), "keep")

	generation := modules.GenerationDef{
		ID: "g2",
		Restore: []modules.RestoreDef{
			{Type: "copy", Source: "copy.txt", Target: instancePath("copy.txt"), Backup: false},
			{Type: "merge-json", Source: "merge.json", Target: instancePath("settings.json")},
			{Type: "merge-ini", Source: "merge.ini", Target: instancePath("settings.ini")},
			{Type: "append", Source: "append.txt", Target: instancePath("lines.txt")},
			{Type: "delete-glob", Target: instancePath("cache"), Pattern: "*.tmp", Backup: false},
			{Type: "registry-set", Key: `HKEY_CURRENT_USER/Software/Endstate/Test`, ValueName: "Theme", ValueType: "reg_sz", Data: "dark"},
		},
		Validate: []modules.ValidationDef{
			{Type: "file-exists", Path: "copy.txt"},
			{Type: "json-parse", Path: "merge.json"},
		},
	}

	got, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  testPlan(hostRoot, generation),
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}

	wantKinds := []ActionKind{
		ActionCopy, ActionWriteFile, ActionWriteFile, ActionWriteFile,
		ActionDeleteFile, ActionDeleteFile, ActionRegistrySet,
	}
	if len(got.Actions) != len(wantKinds) {
		t.Fatalf("actions = %#v, want %d actions", got.Actions, len(wantKinds))
	}
	for index, action := range got.Actions {
		if action.Kind != wantKinds[index] {
			t.Errorf("actions[%d].Kind = %q, want %q", index, action.Kind, wantKinds[index])
		}
		if !action.SnapshotRequired {
			t.Errorf("actions[%d].SnapshotRequired = false, want true", index)
		}
	}
	if got.Actions[0].Source != filepath.Join(stageRoot, "copy.txt") || got.Actions[0].Strategy != "copy" {
		t.Errorf("copy action = %#v", got.Actions[0])
	}
	if string(got.Actions[1].DesiredContent) != "{\n  \"new\": 2,\n  \"old\": 1\n}\n" {
		t.Errorf("JSON desired content = %q", got.Actions[1].DesiredContent)
	}
	if string(got.Actions[2].DesiredContent) != "[settings]\nnew=2\nold=1" {
		t.Errorf("INI desired content = %q", got.Actions[2].DesiredContent)
	}
	if string(got.Actions[3].DesiredContent) != "alpha\nbeta\ngamma\n" {
		t.Errorf("append desired content = %q", got.Actions[3].DesiredContent)
	}
	if got.Actions[4].Target != filepath.Join(cacheTarget, "A.tmp") || got.Actions[5].Target != filepath.Join(cacheTarget, "z.tmp") {
		t.Errorf("delete targets = %q, %q", got.Actions[4].Target, got.Actions[5].Target)
	}
	registry := got.Actions[6].RegistryValue
	if registry == nil || registry.Key != `HKCU\Software\Endstate\Test` || registry.ValueType != "REG_SZ" ||
		got.Actions[6].Target != `HKCU\Software\Endstate\Test\Theme` {
		t.Errorf("registry action = %#v", got.Actions[6])
	}

	if len(got.Validations) != 2 || got.Validations[0].HostPath != copyTarget || got.Validations[1].HostPath != jsonTarget {
		t.Errorf("resolved validations = %#v", got.Validations)
	}
	assertTestFile(t, copyTarget, "original")
	assertTestFile(t, jsonTarget, `{"old":1}`)
	assertTestFile(t, appendTarget, "alpha\nbeta\n")
	if _, err := os.Stat(filepath.Join(cacheTarget, "A.tmp")); err != nil {
		t.Fatalf("delete-glob mutated target during materialization: %v", err)
	}
}

func TestMaterializeMapsValidationWithinCopiedDirectory(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	writeTestFile(t, filepath.Join(stageRoot, "prefs", "settings.json"), `{}`)
	generation := modules.GenerationDef{
		ID:       "g2",
		Restore:  []modules.RestoreDef{{Type: "copy", Source: "prefs", Target: instancePath("prefs")}},
		Validate: []modules.ValidationDef{{Type: "json-parse", Path: "prefs/settings.json"}},
	}

	got, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  testPlan(hostRoot, generation),
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if got.Validations[0].HostPath != filepath.Join(hostRoot, "prefs", "settings.json") {
		t.Errorf("validation host path = %q", got.Validations[0].HostPath)
	}
}

func TestMaterializeRejectsUnmappedAndMultiplyMappedValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		restore []modules.RestoreDef
		count   int
	}{
		{
			name:    "unmapped",
			restore: []modules.RestoreDef{{Type: "copy", Source: "other.txt", Target: instancePath("other.txt")}},
			count:   0,
		},
		{
			name: "multiply mapped",
			restore: []modules.RestoreDef{
				{Type: "copy", Source: "settings.json", Target: instancePath("one.json")},
				{Type: "copy", Source: "settings.json", Target: instancePath("two.json")},
			},
			count: 2,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			stageRoot := t.TempDir()
			hostRoot := t.TempDir()
			writeTestFile(t, filepath.Join(stageRoot, "settings.json"), `{}`)
			writeTestFile(t, filepath.Join(stageRoot, "other.txt"), "other")
			generation := modules.GenerationDef{
				ID:       "g2",
				Restore:  test.restore,
				Validate: []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}},
			}

			_, err := Materialize(context.Background(), Request{
				Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
				Plan:  testPlan(hostRoot, generation),
			})
			var typed *Error
			if !errors.As(err, &typed) || typed.Code != CodeValidationMapping || typed.MappingCount != test.count {
				t.Fatalf("error = %#v, want %s with mapping count %d", err, CodeValidationMapping, test.count)
			}
		})
	}
}

func TestMaterializeRejectsIntraSetTargetOverlaps(t *testing.T) {
	for _, test := range []struct {
		name        string
		restore     []modules.RestoreDef
		windowsOnly bool
	}{
		{
			name:        "case insensitive file identity",
			windowsOnly: true,
			restore: []modules.RestoreDef{
				{Type: "copy", Source: "one", Target: instancePath("Prefs.JSON")},
				{Type: "copy", Source: "two", Target: instancePath("prefs.json")},
			},
		},
		{
			name: "parent and child",
			restore: []modules.RestoreDef{
				{Type: "copy", Source: "one", Target: instancePath("prefs")},
				{Type: "copy", Source: "two", Target: instancePath("prefs", "nested.json")},
			},
		},
		{
			name: "registry value identity",
			restore: []modules.RestoreDef{
				{Type: "registry-set", Key: `HKCU\Software\Endstate`, ValueName: "Theme", ValueType: "REG_SZ", Data: "dark"},
				{Type: "registry-set", Key: `hkey_current_user/software/endstate`, ValueName: "theme", ValueType: "REG_SZ", Data: "light"},
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.windowsOnly && runtime.GOOS != "windows" {
				t.Skip("case-insensitive host target semantics are Windows-specific")
			}
			stageRoot := t.TempDir()
			hostRoot := t.TempDir()
			writeTestFile(t, filepath.Join(stageRoot, "one"), "1")
			writeTestFile(t, filepath.Join(stageRoot, "two"), "2")
			generation := modules.GenerationDef{ID: "g2", Restore: test.restore}

			_, err := Materialize(context.Background(), Request{
				Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
				Plan:  testPlan(hostRoot, generation),
			})
			if CodeOf(err) != CodeTargetOverlap {
				t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), CodeTargetOverlap)
			}
		})
	}
}

func TestMaterializeAllowsCaseDistinctFilesystemTargetsOnCaseSensitiveHosts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows host target identities are case-insensitive")
	}
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	writeTestFile(t, filepath.Join(stageRoot, "one"), "1")
	writeTestFile(t, filepath.Join(stageRoot, "two"), "2")
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{
		{Type: "copy", Source: "one", Target: instancePath("Prefs.JSON")},
		{Type: "copy", Source: "two", Target: instancePath("prefs.json")},
	}}

	got, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  testPlan(hostRoot, generation),
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if len(got.Actions) != 2 {
		t.Fatalf("actions = %#v, want two case-distinct targets", got.Actions)
	}
}

func TestMaterializeRejectsHostTargetLinkBeforeReadingOrPlanningMutation(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	writeTestFile(t, filepath.Join(stageRoot, "merge.json"), `{"new":2}`)
	realTarget := filepath.Join(hostRoot, "real.json")
	linkedTarget := filepath.Join(hostRoot, "linked.json")
	writeTestFile(t, realTarget, `{"old":1}`)
	if err := os.Symlink(realTarget, linkedTarget); err != nil {
		t.Skipf("creating a test symlink is unavailable: %v", err)
	}
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
		Type: "merge-json", Source: "merge.json", Target: instancePath("linked.json"),
	}}}

	_, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  testPlan(hostRoot, generation),
	})
	if CodeOf(err) != CodeUnsafePath {
		t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), CodeUnsafePath)
	}
	assertTestFile(t, realTarget, `{"old":1}`)
}

func TestMaterializeRejectsStageSourceChangedToLinkOrSpecialFile(t *testing.T) {
	t.Run("link", func(t *testing.T) {
		stageRoot := t.TempDir()
		hostRoot := t.TempDir()
		source := filepath.Join(stageRoot, "settings.json")
		outside := filepath.Join(t.TempDir(), "outside.json")
		writeTestFile(t, source, `{}`)
		writeTestFile(t, outside, `{"secret":true}`)
		stage := &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"}
		if err := os.Remove(source); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, source); err != nil {
			t.Skipf("creating a test symlink is unavailable: %v", err)
		}
		generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
			Type: "merge-json", Source: "settings.json", Target: instancePath("settings.json"),
		}}}

		_, err := Materialize(context.Background(), Request{Stage: stage, Plan: testPlan(hostRoot, generation)})
		if CodeOf(err) != CodeUnsafePath {
			t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), CodeUnsafePath)
		}
	})

	t.Run("directory where regular file is required", func(t *testing.T) {
		stageRoot := t.TempDir()
		hostRoot := t.TempDir()
		source := filepath.Join(stageRoot, "append.txt")
		writeTestFile(t, source, "line")
		stage := &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"}
		if err := os.Remove(source); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(source, 0o700); err != nil {
			t.Fatal(err)
		}
		generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
			Type: "append", Source: "append.txt", Target: instancePath("append.txt"),
		}}}

		_, err := Materialize(context.Background(), Request{Stage: stage, Plan: testPlan(hostRoot, generation)})
		if CodeOf(err) != CodeUnsupportedFileType {
			t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), CodeUnsupportedFileType)
		}
	})
}

func TestMaterializeRejectsMergeAppendAndGlobSpecialHostPaths(t *testing.T) {
	for _, strategy := range []string{"merge-json", "append"} {
		t.Run(strategy+" target link", func(t *testing.T) {
			stageRoot := t.TempDir()
			hostRoot := t.TempDir()
			sourceName := "source.txt"
			sourceContent := "line"
			if strategy == "merge-json" {
				sourceName = "source.json"
				sourceContent = `{}`
			}
			writeTestFile(t, filepath.Join(stageRoot, sourceName), sourceContent)
			realTarget := filepath.Join(hostRoot, "real")
			linkedTarget := filepath.Join(hostRoot, "linked")
			writeTestFile(t, realTarget, sourceContent)
			if err := os.Symlink(realTarget, linkedTarget); err != nil {
				t.Skipf("creating a test symlink is unavailable: %v", err)
			}
			generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
				Type: strategy, Source: sourceName, Target: instancePath("linked"),
			}}}

			_, err := Materialize(context.Background(), Request{
				Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
				Plan:  testPlan(hostRoot, generation),
			})
			if CodeOf(err) != CodeUnsafePath {
				t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), CodeUnsafePath)
			}
		})

		t.Run(strategy+" target directory", func(t *testing.T) {
			stageRoot := t.TempDir()
			hostRoot := t.TempDir()
			sourceName := "source.txt"
			sourceContent := "line"
			if strategy == "merge-json" {
				sourceName = "source.json"
				sourceContent = `{}`
			}
			writeTestFile(t, filepath.Join(stageRoot, sourceName), sourceContent)
			if err := os.Mkdir(filepath.Join(hostRoot, "special"), 0o700); err != nil {
				t.Fatal(err)
			}
			generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
				Type: strategy, Source: sourceName, Target: instancePath("special"),
			}}}

			_, err := Materialize(context.Background(), Request{
				Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
				Plan:  testPlan(hostRoot, generation),
			})
			if err == nil {
				t.Fatal("Materialize() error = nil, want special host target rejection")
			}
		})
	}

	t.Run("delete-glob target link", func(t *testing.T) {
		stageRoot := t.TempDir()
		hostRoot := t.TempDir()
		realTarget := filepath.Join(hostRoot, "real-cache")
		linkedTarget := filepath.Join(hostRoot, "linked-cache")
		writeTestFile(t, filepath.Join(realTarget, "outside.tmp"), "outside")
		if err := os.Symlink(realTarget, linkedTarget); err != nil {
			t.Skipf("creating a test symlink is unavailable: %v", err)
		}
		generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
			Type: "delete-glob", Target: instancePath("linked-cache"), Pattern: "*.tmp",
		}}}

		_, err := Materialize(context.Background(), Request{
			Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
			Plan:  testPlan(hostRoot, generation),
		})
		if CodeOf(err) != CodeUnsafePath {
			t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), CodeUnsafePath)
		}
		assertTestFile(t, filepath.Join(realTarget, "outside.tmp"), "outside")
	})

	t.Run("delete-glob traversal through nested link", func(t *testing.T) {
		stageRoot := t.TempDir()
		hostRoot := t.TempDir()
		cacheTarget := filepath.Join(hostRoot, "cache")
		outside := filepath.Join(hostRoot, "outside")
		writeTestFile(t, filepath.Join(outside, "outside.tmp"), "outside")
		if err := os.MkdirAll(cacheTarget, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(cacheTarget, "linked")); err != nil {
			t.Skipf("creating a test directory symlink is unavailable: %v", err)
		}
		generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
			Type: "delete-glob", Target: instancePath("cache"), Pattern: "*/*.tmp",
		}}}

		_, err := Materialize(context.Background(), Request{
			Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
			Plan:  testPlan(hostRoot, generation),
		})
		if CodeOf(err) != CodeUnsafePath {
			t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), CodeUnsafePath)
		}
		assertTestFile(t, filepath.Join(outside, "outside.tmp"), "outside")
	})

	t.Run("delete-glob target is not a directory", func(t *testing.T) {
		stageRoot := t.TempDir()
		hostRoot := t.TempDir()
		writeTestFile(t, filepath.Join(hostRoot, "cache"), "not a directory")
		generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
			Type: "delete-glob", Target: instancePath("cache"), Pattern: "*.tmp",
		}}}

		_, err := Materialize(context.Background(), Request{
			Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
			Plan:  testPlan(hostRoot, generation),
		})
		if CodeOf(err) != CodeUnsupportedFileType {
			t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), CodeUnsupportedFileType)
		}
	})
}

func TestMaterializeRejectsRegistryImportAndMalformedRegistrySet(t *testing.T) {
	for _, test := range []struct {
		name string
		def  modules.RestoreDef
		code Code
	}{
		{name: "whole key import", def: modules.RestoreDef{Type: "registry-import", Source: "settings.reg", Target: `HKCU\Software\Endstate`}, code: CodeUnsupportedRestore},
		{name: "wrong hive", def: modules.RestoreDef{Type: "registry-set", Key: `HKLM\Software\Endstate`, ValueName: "Theme", ValueType: "REG_SZ"}, code: CodeInvalidRegistryTarget},
		{name: "missing subkey", def: modules.RestoreDef{Type: "registry-set", Key: `HKCU`, ValueName: "Theme", ValueType: "REG_SZ"}, code: CodeInvalidRegistryTarget},
		{name: "unsupported type", def: modules.RestoreDef{Type: "registry-set", Key: `HKCU\Software\Endstate`, ValueName: "Theme", ValueType: "REG_BINARY"}, code: CodeInvalidRegistryValue},
		{name: "invalid dword", def: modules.RestoreDef{Type: "registry-set", Key: `HKCU\Software\Endstate`, ValueName: "Theme", ValueType: "REG_DWORD", Data: "not-a-number"}, code: CodeInvalidRegistryValue},
	} {
		t.Run(test.name, func(t *testing.T) {
			stageRoot := t.TempDir()
			hostRoot := t.TempDir()
			writeTestFile(t, filepath.Join(stageRoot, "settings.reg"), "data")
			generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{test.def}}
			_, err := Materialize(context.Background(), Request{
				Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
				Plan:  testPlan(hostRoot, generation),
			})
			if CodeOf(err) != test.code {
				t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), test.code)
			}
		})
	}
}

func TestMaterializeSkipsOptionalMissingSource(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
		Type: "copy", Source: "missing", Target: instancePath("missing"), Optional: true,
	}}}

	got, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"},
		Plan:  testPlan(hostRoot, generation),
	})
	if err != nil {
		t.Fatalf("Materialize() error = %v", err)
	}
	if got.Actions == nil || len(got.Actions) != 0 {
		t.Fatalf("actions = %#v, want non-nil empty slice", got.Actions)
	}
}

func TestMaterializeRejectsMismatchedPinnedInputs(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	generation := modules.GenerationDef{ID: "g2"}
	request := Request{
		Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g1"},
		Plan:  testPlan(hostRoot, generation),
	}
	_, err := Materialize(context.Background(), request)
	if CodeOf(err) != CodeInvalidRequest {
		t.Fatalf("error = %v, code = %q, want %q", err, CodeOf(err), CodeInvalidRequest)
	}
}

func testPlan(root string, generation modules.GenerationDef) planner.PlanSet {
	return planner.PlanSet{
		Source: planner.SourceCapture{CaptureID: "capture", ModuleID: "app", ConfigSetID: "preferences"},
		TargetInstances: []planner.TargetInstance{{
			ID: "target", ModuleID: "app", Root: root, Generation: generation.ID, RawVersion: "2.0",
		}},
		Resolution: planner.ConfigResolution{
			CaptureID: "capture", ModuleID: "app", ConfigSetID: "preferences",
			TargetInstanceID: "target", TargetGeneration: generation.ID,
			Resolution: planner.ResolutionDirect,
		},
		TargetGenerationDef: &generation,
	}
}

func instancePath(parts ...string) string {
	return filepath.Join(append([]string{"${instance.root}"}, parts...)...)
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertTestFile(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("%s = %q, want %q", path, got, want)
	}
}

func TestMaterializeReturnsStableActionOrderAcrossRuns(t *testing.T) {
	stageRoot := t.TempDir()
	hostRoot := t.TempDir()
	cacheTarget := filepath.Join(hostRoot, "cache")
	for _, name := range []string{"b.tmp", "A.tmp", "c.tmp"} {
		writeTestFile(t, filepath.Join(cacheTarget, name), name)
	}
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
		Type: "delete-glob", Target: instancePath("cache"), Pattern: "*.tmp",
	}}}
	request := Request{Stage: &migration.StageResult{Root: stageRoot, TargetGeneration: "g2"}, Plan: testPlan(hostRoot, generation)}

	first, err := Materialize(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Materialize(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	firstTargets := []string{first.Actions[0].Target, first.Actions[1].Target, first.Actions[2].Target}
	secondTargets := []string{second.Actions[0].Target, second.Actions[1].Target, second.Actions[2].Target}
	if !reflect.DeepEqual(firstTargets, secondTargets) {
		t.Fatalf("action order changed: %v != %v", firstTargets, secondTargets)
	}
	if !strings.EqualFold(filepath.Base(firstTargets[0]), "A.tmp") {
		t.Fatalf("first target = %q, want A.tmp", firstTargets[0])
	}
}
