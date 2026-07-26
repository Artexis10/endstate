// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"testing"
)

func TestCaptureContractEnvelopeAndEventsBindExactMGBAOutcome(t *testing.T) {
	runtime, _ := captureContractArtifactFixture(t)
	raw, events := captureContractEvidenceFixture(t, runtime)
	if failure := validateCaptureContractCommandEvidence(raw, events, runtime, "captured.zip"); failure != nil {
		t.Fatalf("valid capture command evidence: %+v", failure)
	}
}

func TestCaptureContractEnvelopeAndEventsRejectVacuityForeignOwnershipAndInflation(t *testing.T) {
	runtime, _ := captureContractArtifactFixture(t)
	raw, events := captureContractEvidenceFixture(t, runtime)
	tests := []struct {
		name       string
		mutate     func(map[string]any, []map[string]any)
		coordinate string
	}{
		{"foreign envelope field", func(value map[string]any, _ []map[string]any) { value["future"] = true }, "data"},
		{"zero captured files", func(value map[string]any, _ []map[string]any) {
			value["configModules"].([]any)[0].(map[string]any)["filesCaptured"] = float64(0)
		}, "configModules"},
		{"foreign package owner", func(value map[string]any, _ []map[string]any) {
			value["packageModuleMap"] = map[string]any{"winget:mgba-emu.mgba": []any{"apps.foreign"}}
		}, "packageModuleMap"},
		{"wrong declared payload", func(value map[string]any, _ []map[string]any) {
			value["configCapture"].(map[string]any)["modules"].([]any)[0].(map[string]any)["files"] = []any{"apps/mgba/foreign.ini"}
		}, "configCapture"},
		{"missing settings stage", func(_ map[string]any, events []map[string]any) { events[3]["stage"] = "foreign" }, "events"},
		{"foreign item identity", func(_ map[string]any, events []map[string]any) { events[2]["id"] = "Foreign.mGBA" }, "item"},
		{"inflated summary", func(_ map[string]any, events []map[string]any) { events[6]["success"] = json.Number("2") }, "summary"},
		{"foreign artifact", func(_ map[string]any, events []map[string]any) {
			events[5]["path"] = "$ENDSTATE_ROOT/manifests/foreign.zip"
		}, "artifact"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var value map[string]any
			if err := json.Unmarshal(raw, &value); err != nil {
				t.Fatal(err)
			}
			candidateEvents := cloneCaptureEvents(events)
			test.mutate(value, candidateEvents)
			candidateRaw := mustV2JSON(t, value)
			if failure := validateCaptureContractCommandEvidence(candidateRaw, candidateEvents, runtime, "captured.zip"); failure == nil || failure.Coordinate != test.coordinate {
				t.Fatalf("failure = %+v, want coordinate %q", failure, test.coordinate)
			}
		})
	}
}

func captureContractEvidenceFixture(t *testing.T, runtime *scenarioRuntime) ([]byte, []map[string]any) {
	t.Helper()
	data := map[string]any{
		"appsIncluded": []any{map[string]any{"source": "winget", "name": "mGBA", "id": "mgba-emu.mgba", "manifestId": "mgba-emu-mgba"}},
		"configModules": []any{map[string]any{
			"displayName": "mGBA", "wingetRefs": []any{"mgba-emu.mgba"}, "chocolateyRefs": []any{}, "appId": "mgba", "id": "apps.mgba",
			"paths": []any{"configs/mgba/config.ini"}, "filesCaptured": float64(1), "status": "captured",
		}},
		"configModuleMap":      map[string]any{"mgba-emu.mgba": "apps.mgba"},
		"packageModuleMap":     map[string]any{"winget:mgba-emu.mgba": []any{"apps.mgba"}},
		"outputPath":           "$ENDSTATE_ROOT/manifests/captured.zip",
		"outputFormat":         "zip",
		"configsIncluded":      []any{"mgba"},
		"configsSkipped":       []any{},
		"configsCaptureErrors": []any{},
		"sanitized":            false,
		"isExample":            false,
		"counts": map[string]any{
			"filteredRuntimes": float64(0), "included": float64(1), "totalFound": float64(1), "sensitiveExcludedCount": float64(0), "filteredStoreApps": float64(0), "skipped": float64(0),
		},
		"captureWarnings": []any{},
		"configCapture": map[string]any{"modules": []any{map[string]any{
			"id": "apps.mgba", "displayName": "mGBA", "entries": float64(0), "files": []any{"apps/mgba/config.ini"},
		}}},
		"manifest": map[string]any{"name": "captured", "path": "$ENDSTATE_ROOT/manifests/captured.zip"},
	}
	events := []map[string]any{
		{"event": "phase", "phase": "capture"},
		{"event": "progress", "phase": "capture", "stage": "inventory"},
		{"event": "item", "id": "mgba-emu.mgba", "driver": "winget", "name": "mGBA", "status": "present", "reason": "detected", "message": "Detected mGBA"},
		{"event": "progress", "phase": "capture", "stage": "settings"},
		{"event": "progress", "phase": "capture", "stage": "packaging"},
		{"event": "artifact", "phase": "capture", "kind": "manifest", "path": "$ENDSTATE_ROOT/manifests/captured.zip"},
		{"event": "summary", "phase": "capture", "total": json.Number("1"), "success": json.Number("1"), "skipped": json.Number("0"), "failed": json.Number("0")},
	}
	return mustV2JSON(t, data), events
}

func cloneCaptureEvents(values []map[string]any) []map[string]any {
	result := make([]map[string]any, len(values))
	for index, event := range values {
		result[index] = make(map[string]any, len(event))
		for name, value := range event {
			result[index][name] = value
		}
	}
	return result
}
