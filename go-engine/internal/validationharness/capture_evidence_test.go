// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"strings"
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

func TestCaptureContractWarningsAreExact(t *testing.T) {
	runtime, _ := captureContractArtifactFixture(t)
	raw, events := captureContractEvidenceFixture(t, runtime)
	tests := []struct {
		name   string
		mutate func(*testing.T, []byte) []byte
	}{
		{"missing", func(t *testing.T, raw []byte) []byte {
			return mutateCaptureContractWarnings(t, raw, func(data map[string]any) { delete(data, "warnings") })
		}},
		{"null", func(t *testing.T, raw []byte) []byte {
			return mutateCaptureContractWarnings(t, raw, func(data map[string]any) { data["warnings"] = nil })
		}},
		{"empty", func(t *testing.T, raw []byte) []byte {
			return mutateCaptureContractWarnings(t, raw, func(data map[string]any) { data["warnings"] = []any{} })
		}},
		{"additional", func(t *testing.T, raw []byte) []byte {
			return mutateCaptureContractWarnings(t, raw, func(data map[string]any) {
				data["warnings"] = []any{captureContractWarning(), captureContractWarning()}
			})
		}},
		{"foreign scalar", func(t *testing.T, raw []byte) []byte {
			return mutateCaptureContractWarnings(t, raw, func(data map[string]any) { data["warnings"] = []any{"foreign"} })
		}},
		{"wrong code", func(t *testing.T, raw []byte) []byte {
			return mutateCaptureContractWarnings(t, raw, func(data map[string]any) {
				data["warnings"].([]any)[0].(map[string]any)["code"] = "foreign"
			})
		}},
		{"wrong message", func(t *testing.T, raw []byte) []byte {
			return mutateCaptureContractWarnings(t, raw, func(data map[string]any) {
				data["warnings"].([]any)[0].(map[string]any)["message"] = "foreign"
			})
		}},
		{"nested driver", func(t *testing.T, raw []byte) []byte {
			return mutateCaptureContractWarnings(t, raw, func(data map[string]any) {
				data["warnings"].([]any)[0].(map[string]any)["driver"] = "winget"
			})
		}},
		{"nested future", func(t *testing.T, raw []byte) []byte {
			return mutateCaptureContractWarnings(t, raw, func(data map[string]any) {
				data["warnings"].([]any)[0].(map[string]any)["future"] = true
			})
		}},
		{"duplicate nested field", func(t *testing.T, raw []byte) []byte {
			return []byte(strings.Replace(string(raw), `"code":"inventory_union_skipped"`, `"code":"inventory_union_skipped","code":"inventory_union_skipped"`, 1))
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := validateCaptureContractCommandEvidence(test.mutate(t, raw), events, runtime, "captured.zip")
			if failure == nil || failure.Code != CodeEnvelopeContract {
				t.Fatalf("failure = %+v", failure)
			}
		})
	}
}

func captureContractEvidenceFixture(t *testing.T, runtime *scenarioRuntime) ([]byte, []map[string]any) {
	t.Helper()
	moduleName := strings.TrimPrefix(runtime.Module.ID, "apps.")
	target := runtime.CapturePlan.Targets[0]
	payloadPath := v1ArtifactPayloadPath(runtime.Module.ID, target.Destination)
	wingetRefs, chocolateyRefs := []any{runtime.Inventory.Ref}, []any{}
	data := map[string]any{
		"appsIncluded": []any{map[string]any{"source": runtime.Inventory.Source, "name": runtime.Inventory.DisplayName, "id": runtime.Inventory.Ref, "manifestId": runtime.Inventory.AppID}},
		"configModules": []any{map[string]any{
			"displayName": runtime.Module.DisplayName, "wingetRefs": wingetRefs, "chocolateyRefs": chocolateyRefs, "appId": runtime.Inventory.AppID, "id": runtime.Module.ID,
			"paths": []any{payloadPath}, "filesCaptured": float64(1), "status": "captured",
		}},
		"configModuleMap":      map[string]any{runtime.Inventory.Ref: runtime.Module.ID},
		"packageModuleMap":     map[string]any{runtime.Inventory.Driver + ":" + runtime.Inventory.Ref: []any{runtime.Module.ID}},
		"outputPath":           "$ENDSTATE_ROOT/manifests/captured.zip",
		"outputFormat":         "zip",
		"configsIncluded":      []any{moduleName},
		"configsSkipped":       []any{},
		"configsCaptureErrors": []any{},
		"sanitized":            false,
		"isExample":            false,
		"counts": map[string]any{
			"filteredRuntimes": float64(0), "included": float64(1), "totalFound": float64(1), "sensitiveExcludedCount": float64(0), "filteredStoreApps": float64(0), "skipped": float64(0),
		},
		"captureWarnings": []any{},
		"warnings":        []any{captureContractWarning()},
		"configCapture": map[string]any{"modules": []any{map[string]any{
			"id": runtime.Module.ID, "displayName": runtime.Module.DisplayName, "entries": float64(0), "files": []any{target.Destination},
		}}},
		"manifest": map[string]any{"name": "captured", "path": "$ENDSTATE_ROOT/manifests/captured.zip"},
	}
	events := []map[string]any{
		{"event": "phase", "phase": "capture"},
		{"event": "progress", "phase": "capture", "stage": "inventory"},
		{"event": "item", "id": runtime.Inventory.Ref, "driver": runtime.Inventory.Driver, "name": runtime.Inventory.DisplayName, "status": "present", "reason": "detected", "message": "Detected " + runtime.Inventory.DisplayName},
		{"event": "progress", "phase": "capture", "stage": "settings"},
		{"event": "progress", "phase": "capture", "stage": "packaging"},
		{"event": "artifact", "phase": "capture", "kind": "manifest", "path": "$ENDSTATE_ROOT/manifests/captured.zip"},
		{"event": "summary", "phase": "capture", "total": json.Number("1"), "success": json.Number("1"), "skipped": json.Number("0"), "failed": json.Number("0")},
	}
	return mustV2JSON(t, data), events
}

func captureContractWarning() map[string]any {
	return map[string]any{
		"code":    "inventory_union_skipped",
		"message": "Installed-software inventory union skipped because a package-manager row lacks an authoritative ARP binding.",
	}
}

func mutateCaptureContractWarnings(t *testing.T, raw []byte, mutate func(map[string]any)) []byte {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	mutate(data)
	return mustV2JSON(t, data)
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
