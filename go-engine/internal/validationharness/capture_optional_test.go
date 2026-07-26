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
	withPayload["configs/mgba/config.ini"] = append([]byte(nil), runtime.CapturePlan.Targets[0].Content...)
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
	data["configsIncluded"], data["configsSkipped"] = []any{}, []any{"mgba"}
	data["outputPath"] = "$ENDSTATE_ROOT/manifests/optional-absent.zip"
	data["manifest"].(map[string]any)["path"] = "$ENDSTATE_ROOT/manifests/optional-absent.zip"
	events[5]["path"] = "$ENDSTATE_ROOT/manifests/optional-absent.zip"
	captured := manifest.Manifest{
		Version: 1, Name: "captured", Captured: time.Now().UTC().Format(time.RFC3339),
		Apps: []manifest.App{{ID: runtime.Inventory.AppID, Refs: map[string]string{"windows": runtime.Inventory.Ref}}},
	}
	metadata := bundle.BundleMetadata{
		SchemaVersion: "1.0", CapturedAt: time.Now().UTC().Format(time.RFC3339), MachineName: "validation-host", EndstateVersion: "test", OS: "windows",
		ConfigModulesIncluded: []string{}, ConfigModulesSkipped: []string{"mgba"}, CaptureWarnings: []string{},
	}
	return mustV2JSON(t, data), events, map[string][]byte{"manifest.jsonc": mustV2JSON(t, captured), "metadata.json": mustV2JSON(t, metadata)}
}
