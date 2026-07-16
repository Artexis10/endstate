// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestStageDirectCopiesClosedWorldPayloadAndRunsFinalValidation(t *testing.T) {
	payload, tempParent := newStagePayload(t, map[string]string{
		"settings.json": `{"generation":1}`,
	})
	if err := os.Mkdir(filepath.Join(payload, "empty-directory"), 0o755); err != nil {
		t.Fatal(err)
	}
	request := directStageRequest(t, payload, tempParent, []modules.ValidationDef{
		{Type: "json-path-exists", Path: "settings.json", JSONPath: "$.generation"},
	})
	sourceBefore := snapshotStageSource(t, payload)

	staged, err := NewEngine().Stage(context.Background(), request)
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if staged.TargetGeneration != "g1" || !reflect.DeepEqual(staged.MigrationPath, []string{}) {
		t.Fatalf("stage metadata = target %q path %v", staged.TargetGeneration, staged.MigrationPath)
	}
	assertStageFile(t, staged.Root, "settings.json", `{"generation":1}`)
	if _, err := os.Lstat(filepath.Join(staged.Root, "empty-directory")); !os.IsNotExist(err) {
		t.Fatalf("undeclared empty directory was copied: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(staged.Root)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o700 {
			t.Fatalf("stage mode = %o, want 700", info.Mode().Perm())
		}
	}
	assertStageSourceUnchanged(t, payload, sourceBefore)
	root := staged.Root
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
	if err := staged.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("stage root still exists after Close: %v", err)
	}
}

func TestStageAppliesExactPinnedEdgesAndValidatesEveryOutput(t *testing.T) {
	payload, tempParent := newStagePayload(t, map[string]string{
		"settings.json": `{"generation":1}`,
	})
	request := StageRequest{
		CaptureID:        "capture-multi",
		PayloadRoot:      payload,
		PayloadManifest:  mustStageManifest(t, payload),
		SourceGeneration: "g1",
		TargetGeneration: &modules.GenerationDef{
			ID: "g3",
			Validate: []modules.ValidationDef{{
				Type: "json-path-exists", Path: "settings.json", JSONPath: "$.final",
			}},
		},
		MigrationEdges: []modules.MigrationEdgeDef{
			{
				From: "g1", To: "g2",
				Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.generation", Value: 2}},
				Validate:   []modules.ValidationDef{{Type: "json-path-exists", Path: "settings.json", JSONPath: "$.generation"}},
			},
			{
				From: "g2", To: "g3",
				Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.final", Value: true}},
				Validate:   []modules.ValidationDef{{Type: "json-path-exists", Path: "settings.json", JSONPath: "$.final"}},
			},
		},
		TempParent: tempParent,
	}
	sourceBefore := snapshotStageSource(t, payload)
	staged, err := NewEngine().Stage(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
	if !reflect.DeepEqual(staged.MigrationPath, []string{"g1", "g2", "g3"}) {
		t.Fatalf("MigrationPath = %v", staged.MigrationPath)
	}
	assertStageFile(t, staged.Root, "settings.json", "{\n  \"final\": true,\n  \"generation\": 2\n}\n")
	assertStageSourceUnchanged(t, payload, sourceBefore)
}

func TestStageMapsClosedWorldSourceIntegrityFailuresAndDoesNotCreateStage(t *testing.T) {
	tests := []struct {
		name       string
		mutate     func(t *testing.T, payload string)
		wantBundle string
	}{
		{"hash mismatch", func(t *testing.T, payload string) { writeStageFile(t, payload, "settings.json", "other") }, bundle.IntegrityPayloadHashMismatch},
		{"size mismatch", func(t *testing.T, payload string) { writeStageFile(t, payload, "settings.json", "longer-value") }, bundle.IntegrityPayloadSizeMismatch},
		{"missing", func(t *testing.T, payload string) {
			if err := os.Remove(filepath.Join(payload, "settings.json")); err != nil {
				t.Fatal(err)
			}
		}, bundle.IntegrityPayloadMissing},
		{"extra", func(t *testing.T, payload string) { writeStageFile(t, payload, "extra.json", `{}`) }, bundle.IntegrityPayloadExtra},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, tempParent := newStagePayload(t, map[string]string{"settings.json": "value"})
			request := directStageRequest(t, payload, tempParent, []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}})
			tt.mutate(t, payload)
			sourceBefore := snapshotStageSource(t, payload)
			_, err := NewEngine().Stage(context.Background(), request)
			assertStageError(t, err, CodePayloadIntegrityFailed, PhaseSourceIntegrity, -1, tt.wantBundle, "")
			assertStageSourceUnchanged(t, payload, sourceBefore)
			assertStageTempParentEmpty(t, tempParent)
		})
	}
}

func TestStageRejectsInvalidRequestShapeBeforePayloadIntegrityWork(t *testing.T) {
	validTarget := &modules.GenerationDef{ID: "g1", Validate: []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}}}
	tests := []struct {
		name string
		edit func(*StageRequest)
	}{
		{"missing capture ID", func(request *StageRequest) { request.CaptureID = "" }},
		{"relative payload root", func(request *StageRequest) { request.PayloadRoot = "relative" }},
		{"missing payload manifest", func(request *StageRequest) { request.PayloadManifest = nil }},
		{"missing source generation", func(request *StageRequest) { request.SourceGeneration = "" }},
		{"missing target generation", func(request *StageRequest) { request.TargetGeneration = nil }},
		{"empty target generation", func(request *StageRequest) { request.TargetGeneration = &modules.GenerationDef{} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, tempParent := newStagePayload(t, map[string]string{"settings.json": "tampered-after-manifest"})
			request := StageRequest{
				CaptureID: "capture-shape", PayloadRoot: payload,
				PayloadManifest:  []manifest.PayloadManifestEntry{{RelativePath: "settings.json", Size: 1, SHA256: testStageSHA256([]byte("x"))}},
				SourceGeneration: "g1", TargetGeneration: validTarget, TempParent: tempParent,
			}
			tt.edit(&request)
			sourceBefore := snapshotStageSource(t, payload)
			_, err := NewEngine().Stage(context.Background(), request)
			assertStageError(t, err, CodeInvalidStageRequest, PhaseRequestValidation, -1, "", "")
			assertStageSourceUnchanged(t, payload, sourceBefore)
			assertStageTempParentEmpty(t, tempParent)
		})
	}
}

func TestStageCleansUpOperationAndValidationFailuresWithoutMutatingSource(t *testing.T) {
	tests := []struct {
		name           string
		edges          []modules.MigrationEdgeDef
		final          []modules.ValidationDef
		wantCode       ErrorCode
		wantPhase      StagePhase
		wantEdge       int
		wantValidation configvalidate.Code
	}{
		{
			name: "unsupported operation",
			edges: []modules.MigrationEdgeDef{{
				From: "g1", To: "g2",
				Operations: []modules.MigrationOperationDef{{Type: "powershell", Path: "settings.json"}},
				Validate:   []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}},
			}},
			final:    []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}},
			wantCode: CodeUnsupportedOperation, wantPhase: PhaseEdgeOperation, wantEdge: 0,
		},
		{
			name: "intermediate validation",
			edges: []modules.MigrationEdgeDef{{
				From: "g1", To: "g2",
				Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.migrated", Value: true}},
				Validate:   []modules.ValidationDef{{Type: "json-path-exists", Path: "settings.json", JSONPath: "$.missing"}},
			}},
			final:    []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}},
			wantCode: CodeValidationFailed, wantPhase: PhaseEdgeValidation, wantEdge: 0, wantValidation: configvalidate.CodeJSONPathMissing,
		},
		{
			name: "final malformed JSON",
			edges: []modules.MigrationEdgeDef{{
				From: "g1", To: "g2",
				Operations: []modules.MigrationOperationDef{{Type: "file-copy", Source: "malformed.json", Target: "target.json"}},
				Validate:   []modules.ValidationDef{{Type: "file-exists", Path: "target.json"}},
			}},
			final:    []modules.ValidationDef{{Type: "json-parse", Path: "target.json"}},
			wantCode: CodeValidationFailed, wantPhase: PhaseTargetValidation, wantEdge: -1, wantValidation: configvalidate.CodeMalformedJSON,
		},
		{
			name: "final malformed INI",
			edges: []modules.MigrationEdgeDef{{
				From: "g1", To: "g2",
				Operations: []modules.MigrationOperationDef{{Type: "file-copy", Source: "malformed.ini", Target: "target.ini"}},
				Validate:   []modules.ValidationDef{{Type: "file-exists", Path: "target.ini"}},
			}},
			final:    []modules.ValidationDef{{Type: "ini-parse", Path: "target.ini"}},
			wantCode: CodeValidationFailed, wantPhase: PhaseTargetValidation, wantEdge: -1, wantValidation: configvalidate.CodeMalformedINI,
		},
		{
			name: "unsupported binary validation",
			edges: []modules.MigrationEdgeDef{{
				From: "g1", To: "g2",
				Operations: []modules.MigrationOperationDef{{Type: "file-copy", Source: "settings.json", Target: "target.json"}},
				Validate:   []modules.ValidationDef{{Type: "file-exists", Path: "target.json"}},
			}},
			final:    []modules.ValidationDef{{Type: "binary-parse", Path: "target.json"}},
			wantCode: CodeValidationFailed, wantPhase: PhaseTargetValidation, wantEdge: -1, wantValidation: configvalidate.CodeUnsupportedValidation,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, tempParent := newStagePayload(t, map[string]string{
				"settings.json":  `{"generation":1}`,
				"malformed.json": `{"broken":`,
				"malformed.ini":  "key:value\n",
			})
			request := StageRequest{
				CaptureID: "capture-failure", PayloadRoot: payload, PayloadManifest: mustStageManifest(t, payload),
				SourceGeneration: "g1", TargetGeneration: &modules.GenerationDef{ID: "g2", Validate: tt.final},
				MigrationEdges: tt.edges, TempParent: tempParent,
			}
			sourceBefore := snapshotStageSource(t, payload)
			_, err := NewEngine().Stage(context.Background(), request)
			assertStageError(t, err, tt.wantCode, tt.wantPhase, tt.wantEdge, "", string(tt.wantValidation))
			assertStageSourceUnchanged(t, payload, sourceBefore)
			assertStageTempParentEmpty(t, tempParent)
		})
	}
}

func TestStageRejectsTempParentAtOrInsidePayloadBeforeCreation(t *testing.T) {
	for _, inside := range []bool{false, true} {
		name := "equal"
		if inside {
			name = "inside"
		}
		t.Run(name, func(t *testing.T) {
			payload, _ := newStagePayload(t, map[string]string{"settings.json": `{}`})
			tempParent := payload
			if inside {
				tempParent = filepath.Join(payload, "stages")
				if err := os.Mkdir(tempParent, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			request := directStageRequest(t, payload, tempParent, []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}})
			sourceBefore := snapshotStageSource(t, payload)
			_, err := NewEngine().Stage(context.Background(), request)
			assertStageError(t, err, CodeUnsafeRoot, PhaseStageCreate, -1, "", "")
			assertStageSourceUnchanged(t, payload, sourceBefore)
			entries, readErr := os.ReadDir(tempParent)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if inside && len(entries) != 0 {
				t.Fatalf("inside temp parent contains %v", entries)
			}
			if !inside {
				for _, entry := range entries {
					if len(entry.Name()) >= len(".endstate-stage-") && entry.Name()[:len(".endstate-stage-")] == ".endstate-stage-" {
						t.Fatalf("stage was created inside payload: %q", entry.Name())
					}
				}
			}
		})
	}
}

func TestStageDirectAlwaysRunsFinalValidation(t *testing.T) {
	payload, tempParent := newStagePayload(t, map[string]string{"settings.ini": "[Settings]\nTheme=dark\n"})
	request := directStageRequest(t, payload, tempParent, []modules.ValidationDef{{
		Type: "ini-key-exists", Path: "settings.ini", Section: "Settings", Key: "Missing",
	}})
	sourceBefore := snapshotStageSource(t, payload)
	_, err := NewEngine().Stage(context.Background(), request)
	assertStageError(t, err, CodeValidationFailed, PhaseTargetValidation, -1, "", string(configvalidate.CodeINIKeyMissing))
	assertStageSourceUnchanged(t, payload, sourceBefore)
	assertStageTempParentEmpty(t, tempParent)
}

func TestStageRejectsMalformedPinnedEdgeSequenceAfterVerifiedCopy(t *testing.T) {
	tests := []struct {
		name   string
		edges  []modules.MigrationEdgeDef
		target string
	}{
		{"wrong first source", []modules.MigrationEdgeDef{stageTestEdge("other", "g2")}, "g2"},
		{"gap", []modules.MigrationEdgeDef{stageTestEdge("g1", "g2"), stageTestEdge("g3", "g4")}, "g4"},
		{"wrong final target", []modules.MigrationEdgeDef{stageTestEdge("g1", "g2")}, "g3"},
		{"cycle", []modules.MigrationEdgeDef{stageTestEdge("g1", "g2"), stageTestEdge("g2", "g1")}, "g1"},
		{"missing route", nil, "g2"},
		{"edge without operations", []modules.MigrationEdgeDef{{From: "g1", To: "g2", Validate: []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}}}}, "g2"},
		{"edge without validation", []modules.MigrationEdgeDef{{From: "g1", To: "g2", Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.migrated", Value: true}}}}, "g2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payload, tempParent := newStagePayload(t, map[string]string{"settings.json": `{}`})
			request := StageRequest{
				CaptureID: "capture-path", PayloadRoot: payload, PayloadManifest: mustStageManifest(t, payload),
				SourceGeneration: "g1", TargetGeneration: &modules.GenerationDef{ID: tt.target, Validate: []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}}},
				MigrationEdges: tt.edges, TempParent: tempParent,
			}
			sourceBefore := snapshotStageSource(t, payload)
			_, err := NewEngine().Stage(context.Background(), request)
			assertStageError(t, err, CodeInvalidMigrationPath, PhasePathValidation, -1, "", "")
			assertStageSourceUnchanged(t, payload, sourceBefore)
			assertStageTempParentEmpty(t, tempParent)
		})
	}
}

func TestStageRejectsUnsafeManifestAndOperationPaths(t *testing.T) {
	for _, unsafePath := range []string{"../settings.json", "/settings.json", `C:\settings.json`} {
		t.Run("manifest "+unsafePath, func(t *testing.T) {
			payload, tempParent := newStagePayload(t, map[string]string{"settings.json": `{}`})
			sourceBefore := snapshotStageSource(t, payload)
			request := directStageRequest(t, payload, tempParent, []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}})
			request.PayloadManifest[0].RelativePath = unsafePath
			_, err := NewEngine().Stage(context.Background(), request)
			assertStageError(t, err, CodePayloadIntegrityFailed, PhaseSourceIntegrity, -1, bundle.IntegrityUnsafePath, "")
			assertStageSourceUnchanged(t, payload, sourceBefore)
			assertStageTempParentEmpty(t, tempParent)
		})
	}

	for _, unsafePath := range []string{"../outside", "/outside", `C:\outside`} {
		t.Run("operation "+unsafePath, func(t *testing.T) {
			payload, tempParent := newStagePayload(t, map[string]string{"settings.json": `{}`})
			request := StageRequest{
				CaptureID: "capture-unsafe-operation", PayloadRoot: payload, PayloadManifest: mustStageManifest(t, payload),
				SourceGeneration: "g1", TargetGeneration: &modules.GenerationDef{ID: "g2", Validate: []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}}},
				MigrationEdges: []modules.MigrationEdgeDef{{
					From: "g1", To: "g2",
					Operations: []modules.MigrationOperationDef{{Type: "file-copy", Source: unsafePath, Target: "copied"}},
					Validate:   []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}},
				}}, TempParent: tempParent,
			}
			sourceBefore := snapshotStageSource(t, payload)
			_, err := NewEngine().Stage(context.Background(), request)
			assertStageError(t, err, CodeUnsafePath, PhaseEdgeOperation, 0, "", "")
			assertStageSourceUnchanged(t, payload, sourceBefore)
			assertStageTempParentEmpty(t, tempParent)
		})
	}
}

func TestStageRejectsLinkedPayloadWithoutChangingOutsideBytes(t *testing.T) {
	payload, tempParent := newStagePayload(t, map[string]string{"settings.json": "inside"})
	manifestEntries := mustStageManifest(t, payload)
	outsideRoot := safeMigrationTestRoot(t)
	writeStageFile(t, outsideRoot, "outside", "outside")
	outside := filepath.Join(outsideRoot, "outside")
	if err := os.Remove(filepath.Join(payload, "settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(payload, "settings.json")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating Windows reparse-point symlink requires local privilege: %v", err)
		}
		t.Fatal(err)
	}
	request := StageRequest{
		CaptureID: "capture-link", PayloadRoot: payload, PayloadManifest: manifestEntries,
		SourceGeneration: "g1", TargetGeneration: &modules.GenerationDef{ID: "g1", Validate: []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}}},
		TempParent: tempParent,
	}
	_, err := NewEngine().Stage(context.Background(), request)
	assertStageError(t, err, CodePayloadIntegrityFailed, PhaseSourceIntegrity, -1, bundle.IntegrityLinkUnsupported, "")
	data, readErr := os.ReadFile(outside)
	if readErr != nil || string(data) != "outside" {
		t.Fatalf("outside bytes changed: %q, %v", data, readErr)
	}
	assertStageTempParentEmpty(t, tempParent)
}

func TestStageHonorsCancellationBeforeCopyEachEdgeAndFinalValidation(t *testing.T) {
	checkpoints := []struct {
		name  string
		phase StagePhase
		index int
	}{
		{"before copy", PhaseStageCopy, -1},
		{"before second edge", PhaseEdgeOperation, 1},
		{"before final validation", PhaseTargetValidation, -1},
	}
	for _, checkpoint := range checkpoints {
		t.Run(checkpoint.name, func(t *testing.T) {
			payload, tempParent := newStagePayload(t, map[string]string{"settings.json": `{}`})
			request := StageRequest{
				CaptureID: "capture-cancel", PayloadRoot: payload, PayloadManifest: mustStageManifest(t, payload),
				SourceGeneration: "g1", TargetGeneration: &modules.GenerationDef{ID: "g3", Validate: []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}}},
				MigrationEdges: []modules.MigrationEdgeDef{
					{
						From: "g1", To: "g2",
						Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.first", Value: true}},
						Validate:   []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}},
					},
					{
						From: "g2", To: "g3",
						Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.second", Value: true}},
						Validate:   []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}},
					},
				}, TempParent: tempParent,
			}
			sourceBefore := snapshotStageSource(t, payload)
			ctx, cancel := context.WithCancel(context.Background())
			engine := NewEngine()
			engine.stageCheckpoint = func(_ context.Context, phase StagePhase, index int) error {
				if phase == checkpoint.phase && index == checkpoint.index {
					cancel()
					return ctx.Err()
				}
				return nil
			}
			_, err := engine.Stage(ctx, request)
			assertStageError(t, err, CodeCanceled, checkpoint.phase, checkpoint.index, "", "")
			assertStageSourceUnchanged(t, payload, sourceBefore)
			assertStageTempParentEmpty(t, tempParent)
		})
	}
}

func directStageRequest(t *testing.T, payload, tempParent string, validation []modules.ValidationDef) StageRequest {
	t.Helper()
	return StageRequest{
		CaptureID: "capture-direct", PayloadRoot: payload, PayloadManifest: mustStageManifest(t, payload),
		SourceGeneration: "g1", TargetGeneration: &modules.GenerationDef{ID: "g1", Validate: validation},
		MigrationEdges: []modules.MigrationEdgeDef{}, TempParent: tempParent,
	}
}

func stageTestEdge(from, to string) modules.MigrationEdgeDef {
	return modules.MigrationEdgeDef{
		From: from,
		To:   to,
		Operations: []modules.MigrationOperationDef{{
			Type: "json-set", Path: "settings.json", JSONPath: "$.last", Value: to,
		}},
		Validate: []modules.ValidationDef{{Type: "file-exists", Path: "settings.json"}},
	}
}

func newStagePayload(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	root := safeMigrationTestRoot(t)
	payload := filepath.Join(root, "payload")
	tempParent := filepath.Join(root, "stages")
	if err := os.Mkdir(payload, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tempParent, 0o755); err != nil {
		t.Fatal(err)
	}
	for relative, content := range files {
		writeStageFile(t, payload, relative, content)
	}
	return payload, tempParent
}

func writeStageFile(t *testing.T, root, relative, content string) {
	t.Helper()
	hostPath := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(hostPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hostPath, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustStageManifest(t *testing.T, root string) []manifest.PayloadManifestEntry {
	t.Helper()
	entries, err := bundle.BuildPayloadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func testStageSHA256(data []byte) string {
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func snapshotStageSource(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(hostPath string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(hostPath)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, hostPath)
		if err != nil {
			return err
		}
		hash := sha256.Sum256(data)
		snapshot[filepath.ToSlash(relative)] = hex.EncodeToString(hash[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func assertStageSourceUnchanged(t *testing.T, root string, before map[string]string) {
	t.Helper()
	if after := snapshotStageSource(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("source payload changed: after=%v before=%v", after, before)
	}
}

func assertStageFile(t *testing.T, root, relative, want string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("stage file %q = %q, want %q", relative, data, want)
	}
}

func assertStageTempParentEmpty(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("stage temp parent contains %v", entries)
	}
}

func assertStageError(t *testing.T, err error, code ErrorCode, phase StagePhase, edgeIndex int, integrityCode, validationCode string) {
	t.Helper()
	if CodeOf(err) != code {
		t.Fatalf("Stage error = %v, code = %q, want %q", err, CodeOf(err), code)
	}
	var typed *Error
	if !errors.As(err, &typed) {
		t.Fatalf("Stage error type = %T, want *Error", err)
	}
	if typed.Phase != phase || typed.EdgeIndex != edgeIndex || typed.IntegrityCode != integrityCode || typed.ValidationCode != validationCode {
		t.Fatalf("Stage error context = phase %q edge %d integrity %q validation %q", typed.Phase, typed.EdgeIndex, typed.IntegrityCode, typed.ValidationCode)
	}
}
