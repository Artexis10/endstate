// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

func TestCaptureContractAllOptionalAbsentIsExactAndCannotMintPositiveEvidence(t *testing.T) {
	runtime, _ := captureContractArtifactFixture(t)
	raw, events, entries := captureContractOptionalFixture(t, runtime)
	artifact := writeV2ArtifactZip(t, runtime.Root, "optional-absent.zip", entries)
	if failure := validateCaptureContractOptionalAbsentOutcome(raw, events, runtime, artifact); failure != nil {
		t.Fatalf("valid all-optional negative: %+v", failure)
	}

	positiveRaw, positiveEvents := captureContractEvidenceFixture(t, runtime)
	if failure := validateCaptureContractOptionalAbsentOutcome(positiveRaw, positiveEvents, runtime, artifact); failure == nil {
		t.Fatal("positive capture evidence passed as all-optional absence")
	}
	withPayload := cloneV2Entries(entries)
	withPayload[v1ArtifactPayloadPath(runtime.Module.ID, runtime.CapturePlan.Targets[0].Destination)] = append([]byte(nil), runtime.CapturePlan.Targets[0].Content...)
	payloadArtifact := writeV2ArtifactZip(t, runtime.Root, "optional-with-payload.zip", withPayload)
	payloadRaw, payloadEvents := retargetOptionalCaptureEvidence(t, raw, events, filepath.Base(payloadArtifact))
	if failure := validateCaptureContractOptionalAbsentOutcome(payloadRaw, payloadEvents, runtime, payloadArtifact); failure == nil || failure.Coordinate != "artifact" {
		t.Fatalf("optional payload failure = %+v", failure)
	}
	withDirectory := cloneV2Entries(entries)
	withDirectory["foreign/"] = []byte{}
	directoryArtifact := writeV2ArtifactZip(t, runtime.Root, "optional-with-directory.zip", withDirectory)
	directoryRaw, directoryEvents := retargetOptionalCaptureEvidence(t, raw, events, filepath.Base(directoryArtifact))
	if failure := validateCaptureContractOptionalAbsentOutcome(directoryRaw, directoryEvents, runtime, directoryArtifact); failure == nil || failure.Coordinate != "artifact" {
		t.Fatalf("optional directory-member failure = %+v", failure)
	}
	for _, field := range []string{"configModules", "verify", "restore", "legacyConfigLanes", "configCaptures"} {
		t.Run("manifest "+field, func(t *testing.T) {
			candidate := cloneV2Entries(entries)
			mutateCaptureManifest(t, candidate, func(value map[string]any) { value[field] = []any{} })
			candidateArtifact := writeV2ArtifactZip(t, runtime.Root, "optional-"+strings.ToLower(field)+".zip", candidate)
			candidateRaw, candidateEvents := retargetOptionalCaptureEvidence(t, raw, events, filepath.Base(candidateArtifact))
			if failure := validateCaptureContractOptionalAbsentOutcome(candidateRaw, candidateEvents, runtime, candidateArtifact); failure == nil || failure.Coordinate != "manifest" {
				t.Fatalf("explicit empty %s failure = %+v", field, failure)
			}
		})
	}
}

func TestCaptureContractOptionalAbsentWarningsAreExact(t *testing.T) {
	runtime, _ := captureContractArtifactFixture(t)
	raw, events, entries := captureContractOptionalFixture(t, runtime)
	artifact := writeV2ArtifactZip(t, runtime.Root, "optional-absent.zip", entries)
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
			failure := validateCaptureContractOptionalAbsentOutcome(test.mutate(t, raw), events, runtime, artifact)
			if failure == nil || failure.Code != CodeEnvelopeContract {
				t.Fatalf("failure = %+v", failure)
			}
		})
	}
}

func TestOptionalCaptureManifestAppIsExact(t *testing.T) {
	runtime, _ := captureContractArtifactFixture(t)
	_, _, entries := captureContractOptionalFixture(t, runtime)
	tests := []struct {
		name   string
		mutate func(map[string][]byte)
	}{
		{"missing app display name", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { delete(value["apps"].([]any)[0].(map[string]any), "displayName") })
		}},
		{"blank app display name", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { value["apps"].([]any)[0].(map[string]any)["displayName"] = "" })
		}},
		{"wrong app display name", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { value["apps"].([]any)[0].(map[string]any)["displayName"] = "Foreign" })
		}},
		{"underscored app display name", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) {
				app := value["apps"].([]any)[0].(map[string]any)
				delete(app, "displayName")
				app["display_name"] = runtime.Inventory.DisplayName
			})
		}},
		{"foreign app field", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { value["apps"].([]any)[0].(map[string]any)["future"] = true })
		}},
		{"duplicate app display name", func(values map[string][]byte) {
			values["manifest.jsonc"] = []byte(strings.Replace(string(values["manifest.jsonc"]), `"displayName":"mGBA"`, `"displayName":"mGBA","displayName":"mGBA"`, 1))
		}},
		{"wrong app id", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) { value["apps"].([]any)[0].(map[string]any)["id"] = "foreign" })
		}},
		{"wrong app ref", func(values map[string][]byte) {
			mutateCaptureManifest(t, values, func(value map[string]any) {
				value["apps"].([]any)[0].(map[string]any)["refs"] = map[string]any{"windows": "Foreign.mGBA"}
			})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneV2Entries(entries)
			test.mutate(candidate)
			if failure := validateOptionalCaptureManifest(runtime, candidate["manifest.jsonc"]); failure == nil {
				t.Fatal("foreign optional-absence app projection passed")
			}
		})
	}
}

func retargetOptionalCaptureEvidence(t *testing.T, raw []byte, events []map[string]any, artifactBase string) ([]byte, []map[string]any) {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	want := "$ENDSTATE_ROOT/manifests/" + artifactBase
	value["outputPath"] = want
	value["manifest"].(map[string]any)["path"] = want
	candidateEvents := cloneCaptureEvents(events)
	candidateEvents[5]["path"] = want
	return mustV2JSON(t, value), candidateEvents
}

func captureContractOptionalFixture(t *testing.T, runtime *scenarioRuntime) ([]byte, []map[string]any, map[string][]byte) {
	t.Helper()
	raw, events := captureContractEvidenceFixture(t, runtime)
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	module := data["configModules"].([]any)[0].(map[string]any)
	module["paths"], module["filesCaptured"], module["status"] = []any{}, float64(0), "skipped"
	moduleName := strings.TrimPrefix(runtime.Module.ID, "apps.")
	data["configsIncluded"], data["configsSkipped"] = []any{}, []any{moduleName}
	data["outputPath"] = "$ENDSTATE_ROOT/manifests/optional-absent.zip"
	data["manifest"].(map[string]any)["path"] = "$ENDSTATE_ROOT/manifests/optional-absent.zip"
	events[5]["path"] = "$ENDSTATE_ROOT/manifests/optional-absent.zip"
	captured := manifest.Manifest{
		Version: 1, Name: "captured", Captured: time.Now().UTC().Format(time.RFC3339),
		Apps: []manifest.App{{ID: runtime.Inventory.AppID, DisplayName: runtime.Inventory.DisplayName, Refs: map[string]string{"windows": runtime.Inventory.Ref}}},
	}
	metadata := bundle.BundleMetadata{
		SchemaVersion: "1.0", CapturedAt: time.Now().UTC().Format(time.RFC3339), MachineName: "validation-host", EndstateVersion: "test", OS: "windows",
		ConfigModulesIncluded: []string{}, ConfigModulesSkipped: []string{moduleName}, CaptureWarnings: []string{},
	}
	return mustV2JSON(t, data), events, map[string][]byte{"manifest.jsonc": mustV2JSON(t, captured), "metadata.json": mustV2JSON(t, metadata)}
}
