// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"context"
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

type recordingStageObserver struct {
	progress []StageProgress
}

func (o *recordingStageObserver) ObserveStageProgress(progress StageProgress) {
	o.progress = append(o.progress, progress)
}

func TestStageProgressReportsOrderedPinnedEdgesAndValidation(t *testing.T) {
	payload, tempParent := newStagePayload(t, map[string]string{"settings.json": `{"generation":1}`})
	observer := &recordingStageObserver{}
	request := StageRequest{
		CaptureID: "capture-progress", PayloadRoot: payload, PayloadManifest: mustStageManifest(t, payload),
		SourceGeneration: "g1", TempParent: tempParent, Observer: observer,
		TargetGeneration: &modules.GenerationDef{ID: "g3", Validate: []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}}},
		MigrationEdges: []modules.MigrationEdgeDef{
			{
				From: "g1", To: "g2",
				Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.generation", Value: 2}},
				Validate:   []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}},
			},
			{
				From: "g2", To: "g3",
				Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.generation", Value: 3}},
				Validate:   []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}},
			},
		},
	}

	result, err := NewEngine().Stage(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer result.Close()

	want := []StageProgress{
		{CaptureID: "capture-progress", Stage: ProgressStaging, Status: ProgressStarted, EdgeIndex: -1},
		{CaptureID: "capture-progress", Stage: ProgressStaging, Status: ProgressCompleted, EdgeIndex: -1},
		{CaptureID: "capture-progress", Stage: ProgressEdge, Status: ProgressStarted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
		{CaptureID: "capture-progress", Stage: ProgressEdge, Status: ProgressCompleted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
		{CaptureID: "capture-progress", Stage: ProgressValidation, Status: ProgressStarted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
		{CaptureID: "capture-progress", Stage: ProgressValidation, Status: ProgressCompleted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
		{CaptureID: "capture-progress", Stage: ProgressEdge, Status: ProgressStarted, EdgeIndex: 1, FromGeneration: "g2", ToGeneration: "g3"},
		{CaptureID: "capture-progress", Stage: ProgressEdge, Status: ProgressCompleted, EdgeIndex: 1, FromGeneration: "g2", ToGeneration: "g3"},
		{CaptureID: "capture-progress", Stage: ProgressValidation, Status: ProgressStarted, EdgeIndex: 1, FromGeneration: "g2", ToGeneration: "g3"},
		{CaptureID: "capture-progress", Stage: ProgressValidation, Status: ProgressCompleted, EdgeIndex: 1, FromGeneration: "g2", ToGeneration: "g3"},
		{CaptureID: "capture-progress", Stage: ProgressValidation, Status: ProgressStarted, EdgeIndex: -1, ToGeneration: "g3"},
		{CaptureID: "capture-progress", Stage: ProgressValidation, Status: ProgressCompleted, EdgeIndex: -1, ToGeneration: "g3"},
	}
	if !reflect.DeepEqual(observer.progress, want) {
		t.Fatalf("progress =\n%+v\nwant =\n%+v", observer.progress, want)
	}
}

func TestStageProgressReportsFailureAtActiveBoundary(t *testing.T) {
	payload, tempParent := newStagePayload(t, map[string]string{"settings.json": `{"generation":1}`})
	observer := &recordingStageObserver{}
	request := StageRequest{
		CaptureID: "capture-failure", PayloadRoot: payload, PayloadManifest: mustStageManifest(t, payload),
		SourceGeneration: "g1", TempParent: tempParent, Observer: observer,
		TargetGeneration: &modules.GenerationDef{ID: "g2", Validate: []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}}},
		MigrationEdges: []modules.MigrationEdgeDef{{
			From: "g1", To: "g2",
			Operations: []modules.MigrationOperationDef{{Type: "json-set", Path: "settings.json", JSONPath: "$.generation", Value: 2}},
			Validate:   []modules.ValidationDef{{Type: "json-path-exists", Path: "settings.json", JSONPath: "$.missing"}},
		}},
	}

	_, err := NewEngine().Stage(context.Background(), request)
	if CodeOf(err) != CodeValidationFailed {
		t.Fatalf("error = %v", err)
	}
	last := observer.progress[len(observer.progress)-1]
	if last.Stage != ProgressValidation || last.Status != ProgressFailed || last.EdgeIndex != 0 || last.Code != CodeValidationFailed {
		t.Fatalf("last progress = %+v", last)
	}
	if got := observer.progress[len(observer.progress)-2].Status; got != ProgressStarted {
		t.Fatalf("event before failure = %q, want started", got)
	}
}

func TestStageProgressReportsSourceIntegrityAsStagingFailure(t *testing.T) {
	payload, tempParent := newStagePayload(t, map[string]string{"settings.json": `{}`})
	observer := &recordingStageObserver{}
	request := directStageRequest(t, payload, tempParent, []modules.ValidationDef{{Type: "json-parse", Path: "settings.json"}})
	request.Observer = observer
	writeStageFile(t, payload, "settings.json", `{"tampered":true}`)

	_, err := NewEngine().Stage(context.Background(), request)
	if CodeOf(err) != CodePayloadIntegrityFailed {
		t.Fatalf("error = %v", err)
	}
	want := []StageProgress{
		{CaptureID: request.CaptureID, Stage: ProgressStaging, Status: ProgressStarted, EdgeIndex: -1},
		{CaptureID: request.CaptureID, Stage: ProgressStaging, Status: ProgressFailed, EdgeIndex: -1, Code: CodePayloadIntegrityFailed},
	}
	if !reflect.DeepEqual(observer.progress, want) {
		t.Fatalf("progress = %+v, want %+v", observer.progress, want)
	}
}
