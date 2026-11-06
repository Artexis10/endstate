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

func TestResolveHomeFilesKeepsLeafPolicyAndOptionalAbsenceSeparateFromDirect(t *testing.T) {
	stage, host := t.TempDir(), t.TempDir()
	writeTestFile(t, filepath.Join(stage, "prefs"), "desired")
	referent := filepath.Join(host, "referent")
	writeTestFile(t, referent, "prior")
	target := filepath.Join(host, "prefs")
	if err := os.Symlink(referent, target); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{
		{Type: "copy", Source: "prefs", Target: instancePath("prefs")},
		{Type: "copy", Source: "absent", Target: instancePath("absent"), Optional: true},
	}}
	request := Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g2"}, Plan: testPlan(host, generation)}
	files, err := ResolveHomeFiles(context.Background(), request)
	if err != nil || len(files) != 1 || files[0].Target != target || files[0].Source != filepath.Join(stage, "prefs") || files[0].TargetExpression != instancePath("prefs") {
		t.Fatalf("resolved HM placement lost its trusted scope: %+v %v", files, err)
	}
	assertTestFile(t, referent, "prior")
	if _, err := Materialize(context.Background(), request); CodeOf(err) != CodeUnsafePath {
		t.Fatalf("HM leaf policy weakened direct protection: %v", err)
	}
}

func TestResolveHomeFilesRejectsUnsupportedSemanticsWithoutDirectFallback(t *testing.T) {
	for _, kind := range []string{"merge-json", "directory"} {
		t.Run(kind, func(t *testing.T) {
			stage, host := t.TempDir(), t.TempDir()
			definition := modules.RestoreDef{Type: "copy", Source: "prefs", Target: instancePath("prefs")}
			if kind == "directory" {
				if err := os.Mkdir(filepath.Join(stage, "prefs"), 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				definition.Type = kind
				writeTestFile(t, filepath.Join(stage, "prefs"), "{}")
			}
			generation := modules.GenerationDef{ID: "g2", Restore: []modules.RestoreDef{definition}}
			_, err := ResolveHomeFiles(context.Background(), Request{Stage: &migration.StageResult{Root: stage, TargetGeneration: "g2"}, Plan: testPlan(host, generation)})
			if CodeOf(err) != CodeUnsupportedRestore {
				t.Fatalf("unsupported semantics entered HM placement: %v", err)
			}
			if _, err := os.Stat(filepath.Join(host, "prefs")); !os.IsNotExist(err) {
				t.Fatalf("unsupported source changed target: %v", err)
			}
		})
	}
}
