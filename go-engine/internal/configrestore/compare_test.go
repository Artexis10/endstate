// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestCompareMaterializedCopyChecksExactBytesAndDirectoryOverlay(t *testing.T) {
	stage, target := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(stage, "prefs", "owned.txt"), "desired\r\n")
	writeTestFile(t, filepath.Join(stage, "prefs", "ignore.log"), "captured excluded")
	writeTestFile(t, filepath.Join(target, "prefs", "owned.txt"), "desired\r\n")
	writeTestFile(t, filepath.Join(target, "prefs", "ignore.log"), "local excluded")
	writeTestFile(t, filepath.Join(target, "prefs", "unrelated.txt"), "local only")
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{
		Type: "copy", Source: "prefs", Target: instancePath("prefs"), Exclude: []string{"**/ignore.log"},
	}}}
	materialized, err := Materialize(context.Background(), Request{
		Stage: &migration.StageResult{Root: stage, TargetGeneration: "g2"}, Plan: testPlan(target, generation),
	})
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := CompareMaterialized(context.Background(), materialized)
	if err != nil || comparison.Checked != 1 || comparison.Drifted || comparison.Unsupported != 0 {
		t.Fatalf("directory overlay comparison = %+v, %v", comparison, err)
	}
	writeTestFile(t, filepath.Join(target, "prefs", "owned.txt"), "desired\n")
	comparison, err = CompareMaterialized(context.Background(), materialized)
	if err != nil || !comparison.Drifted || comparison.Missing {
		t.Fatalf("line-ending drift comparison = %+v, %v", comparison, err)
	}
	assertTestFile(t, filepath.Join(target, "prefs", "owned.txt"), "desired\n")
	assertTestFile(t, filepath.Join(target, "prefs", "ignore.log"), "local excluded")
	assertTestFile(t, filepath.Join(target, "prefs", "unrelated.txt"), "local only")
}

func TestCompareMaterializedEmptyRegularFileStillParticipates(t *testing.T) {
	for _, test := range []struct{ name, source, member string }{
		{"file", "empty.txt", "empty.txt"},
		{"directory", "prefs", "prefs/empty.txt"},
	} {
		t.Run(test.name, func(t *testing.T) {
			stage, target := t.TempDir(), t.TempDir()
			writeTestFile(t, filepath.Join(stage, test.member), "")
			writeTestFile(t, filepath.Join(target, test.member), "changed")
			generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{Type: "copy", Source: test.source, Target: instancePath(test.source)}}}
			materialized, err := Materialize(context.Background(), Request{
				Stage: &migration.StageResult{Root: stage, TargetGeneration: "g2"}, Plan: testPlan(target, generation),
			})
			if err != nil {
				t.Fatal(err)
			}
			comparison, err := CompareMaterialized(context.Background(), materialized)
			if err != nil || comparison.Checked != 1 || !comparison.Drifted {
				t.Fatalf("empty captured file was unchecked: %+v, %v", comparison, err)
			}
			writeTestFile(t, filepath.Join(target, test.member), "")
			comparison, err = CompareMaterialized(context.Background(), materialized)
			if err != nil || comparison.Checked != 1 || comparison.Drifted {
				t.Fatalf("matching empty file comparison = %+v, %v", comparison, err)
			}
		})
	}
}

func TestCompareMaterializedMergeAndAppendKeepCurrentUnrelatedValues(t *testing.T) {
	for _, test := range []struct{ strategy, source, current, changed string }{
		{"merge-json", `{"owned":"desired"}`, "{\n  \"owned\": \"desired\",\n  \"user\": \"first\"\n}\n", "{\n  \"owned\": \"desired\",\n  \"user\": \"second\"\n}\n"},
		{"merge-ini", "[settings]\nowned=desired", "[settings]\nowned=desired\nuser=first", "[settings]\nowned=desired\nuser=second"},
		{"append", "owned\n", "owned\nfirst\n", "owned\nsecond\n"},
	} {
		t.Run(test.strategy, func(t *testing.T) {
			stage, target := t.TempDir(), t.TempDir()
			writeTestFile(t, filepath.Join(stage, "settings"), test.source)
			path := filepath.Join(target, "settings")
			writeTestFile(t, path, test.current)
			generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{{Type: test.strategy, Source: "settings", Target: instancePath("settings")}}}
			for _, current := range []string{test.current, test.changed} {
				writeTestFile(t, path, current)
				materialized, err := Materialize(context.Background(), Request{
					Stage: &migration.StageResult{Root: stage, TargetGeneration: "g2"}, Plan: testPlan(target, generation),
				})
				if err != nil {
					t.Fatal(err)
				}
				comparison, err := CompareMaterialized(context.Background(), materialized)
				if err != nil || comparison.Checked != 1 || comparison.Drifted || comparison.Unsupported != 0 {
					t.Fatalf("current unrelated value comparison = %+v, %v", comparison, err)
				}
				assertTestFile(t, path, current)
			}
		})
	}
}

func TestCompareMaterializedDeleteChecksPresenceWithoutDeleting(t *testing.T) {
	target := filepath.Join(t.TempDir(), "cache.tmp")
	set := &MaterializedSet{Actions: []Action{{Kind: ActionDeleteFile, Target: target}}}
	comparison, err := CompareMaterialized(context.Background(), set)
	if err != nil || comparison.Checked != 1 || comparison.Drifted {
		t.Fatalf("absent delete target comparison = %+v, %v", comparison, err)
	}
	writeTestFile(t, target, "keep until repair")
	comparison, err = CompareMaterialized(context.Background(), set)
	if err != nil || !comparison.Drifted || comparison.Missing {
		t.Fatalf("present delete target comparison = %+v, %v", comparison, err)
	}
	assertTestFile(t, target, "keep until repair")
}

func TestCompareMaterializedResolvesAndEnforcesHostBoundary(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "settings")
	writeTestFile(t, target, "desired")
	boundary := &recordingHostBoundary{root: root, resolved: map[string]string{"${home}/settings": target, "payload/settings": target}}
	set := &MaterializedSet{boundary: boundary, Actions: []Action{{Kind: ActionWriteFile, Source: "payload/settings", Target: "${home}/settings", DesiredContent: []byte("desired")}}}
	comparison, err := CompareMaterialized(context.Background(), set)
	if err != nil || comparison.Checked != 1 || comparison.Drifted {
		t.Fatalf("semantic target comparison = %+v, %v", comparison, err)
	}
	boundary.rejectPath = target
	if _, err := CompareMaterialized(context.Background(), set); err == nil {
		t.Fatal("comparison read through a rejected host boundary")
	}
	assertTestFile(t, target, "desired")
}

func TestCompareMaterializedRejectsLinkedTarget(t *testing.T) {
	root := t.TempDir()
	outside, target := filepath.Join(root, "outside"), filepath.Join(root, "linked")
	writeTestFile(t, outside, "untouched")
	if err := os.Symlink(outside, target); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := CompareMaterialized(context.Background(), &MaterializedSet{Actions: []Action{{Kind: ActionWriteFile, Target: target, DesiredContent: []byte("untouched")}}}); err == nil {
		t.Fatal("linked target was accepted as matching content")
	}
	assertTestFile(t, outside, "untouched")
}
