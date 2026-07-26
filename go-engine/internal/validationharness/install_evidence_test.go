// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"testing"
)

func TestInstallApplyEvidencePinsExactActionOwnershipAndEvents(t *testing.T) {
	runtime := installEvidenceRuntime(t)
	payload := validInstallApplyPayload(runtime)
	events := validInstallEvents(runtime, true)
	if failure := validateInstallApplyEvidence(mustInstallJSON(t, payload), events, runtime); failure != nil {
		t.Fatalf("valid apply evidence: %+v", failure)
	}

	tests := []struct {
		name   string
		mutate func(map[string]any, []map[string]any)
	}{
		{"wrong app", func(value map[string]any, _ []map[string]any) {
			value["actions"].([]any)[0].(map[string]any)["id"] = "foreign"
		}},
		{"wrong ref", func(value map[string]any, _ []map[string]any) {
			value["actions"].([]any)[0].(map[string]any)["ref"] = "Foreign.Ref"
		}},
		{"wrong source", func(value map[string]any, _ []map[string]any) {
			value["actions"].([]any)[0].(map[string]any)["source"] = "msstore"
		}},
		{"wrong driver", func(value map[string]any, _ []map[string]any) {
			value["actions"].([]any)[0].(map[string]any)["driver"] = "chocolatey"
		}},
		{"missing ownership", func(value map[string]any, _ []map[string]any) { value["packageModuleMap"] = map[string]any{} }},
		{"fabricated config ownership", func(value map[string]any, _ []map[string]any) {
			value["configModuleMap"] = map[string]any{runtime.Inventory.Ref: runtime.InstallPlan.ModuleID}
		}},
		{"duplicate ownership", func(value map[string]any, _ []map[string]any) {
			value["packageModuleMap"].(map[string]any)["winget:Kubernetes.kubectl"] = []any{"apps.kubectl", "apps.kubectl"}
		}},
		{"foreign action", func(value map[string]any, _ []map[string]any) {
			value["actions"] = append(value["actions"].([]any), map[string]any{"id": "foreign"})
		}},
		{"install action", func(value map[string]any, _ []map[string]any) {
			value["actions"].([]any)[0].(map[string]any)["status"] = "to_install"
		}},
		{"event identity", func(_ map[string]any, events []map[string]any) { events[1]["id"] = "Foreign.Ref" }},
		{"event failure", func(_ map[string]any, events []map[string]any) {
			events[2]["success"], events[2]["failed"] = json.Number("0"), json.Number("1")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneInstallMap(t, payload)
			candidateEvents := cloneInstallEvents(t, events)
			test.mutate(candidate, candidateEvents)
			if failure := validateInstallApplyEvidence(mustInstallJSON(t, candidate), candidateEvents, runtime); failure == nil {
				t.Fatal("adversarial apply evidence was accepted")
			}
		})
	}
}

func TestInstallVerifyEvidenceRequiresExactNegativeThenPositiveMultiset(t *testing.T) {
	runtime := installEvidenceRuntime(t)
	for _, passing := range []bool{false, true} {
		payload := validInstallVerifyPayload(runtime, passing)
		events := validInstallEvents(runtime, false)
		if !passing {
			events[2]["success"], events[2]["failed"] = json.Number("1"), json.Number("1")
		}
		if failure := validateInstallVerifyEvidence(mustInstallJSON(t, payload), events, runtime, passing); failure != nil {
			t.Fatalf("valid passing=%t evidence: %+v", passing, failure)
		}

		tests := []struct {
			name   string
			mutate func(map[string]any)
		}{
			{"missing verifier", func(value map[string]any) { value["results"] = value["results"].([]any)[:1] }},
			{"duplicate verifier", func(value map[string]any) {
				value["results"] = append(value["results"].([]any), cloneInstallMap(t, value["results"].([]any)[1].(map[string]any)))
			}},
			{"foreign verifier", func(value map[string]any) { value["results"].([]any)[1].(map[string]any)["type"] = "file-exists" }},
			{"skipped package", func(value map[string]any) { value["results"].([]any)[0].(map[string]any)["status"] = "skipped" }},
			{"wrong resolution", func(value map[string]any) {
				value["results"].([]any)[1].(map[string]any)["message"] = "Command exists: C:\\host\\kubectl.exe"
			}},
		}
		for _, test := range tests {
			t.Run(test.name+"/"+map[bool]string{false: "negative", true: "positive"}[passing], func(t *testing.T) {
				candidate := cloneInstallMap(t, payload)
				test.mutate(candidate)
				if failure := validateInstallVerifyEvidence(mustInstallJSON(t, candidate), events, runtime, passing); failure == nil {
					t.Fatal("adversarial verify evidence was accepted")
				}
			})
		}
	}
}

func TestInstallEvidenceCanonicalizesSemanticManifestSeparators(t *testing.T) {
	runtime := installEvidenceRuntime(t)
	apply := validInstallApplyPayload(runtime)
	apply["manifest"].(map[string]any)["path"] = `$ENDSTATE_ROOT\manifests\install-v1.jsonc`
	if failure := validateInstallApplyEvidence(mustInstallJSON(t, apply), validInstallEvents(runtime, true), runtime); failure != nil {
		t.Fatalf("Windows semantic apply path: %+v", failure)
	}
	verify := validInstallVerifyPayload(runtime, true)
	verify["manifest"].(map[string]any)["path"] = `$ENDSTATE_ROOT\manifests\install-v1.jsonc`
	verify["results"].([]any)[1].(map[string]any)["message"] = `Command exists: $ENDSTATE_ROOT\state\validation-tools\kubectl.exe`
	if failure := validateInstallVerifyEvidence(mustInstallJSON(t, verify), validInstallEvents(runtime, false), runtime, true); failure != nil {
		t.Fatalf("Windows semantic verify path: %+v", failure)
	}
}

func TestInstallEventDecoderClosesApplyAndExpectedNegativeVerifyShapes(t *testing.T) {
	apply := installEventWire("apply-run", "plan", 1, 1, 0)
	if _, failure := decodeEvents([]byte(apply), "apply", "apply-envelope"); failure != nil {
		t.Fatalf("valid apply events: %+v", failure)
	}
	failedApply := installEventWire("apply-run", "plan", 1, 0, 1)
	if _, failure := decodeEvents([]byte(failedApply), "apply", "apply-envelope"); failure == nil {
		t.Fatal("failed apply event summary was accepted")
	}
	negativeVerify := installEventWire("verify-run", "verify", 2, 1, 1)
	if _, failure := decodeEvents([]byte(negativeVerify), "verify", "verify-envelope"); failure == nil {
		t.Fatal("ordinary passing decoder accepted a nested verifier failure")
	}
	if _, failure := decodeExpectedVerifyFailureEvents([]byte(negativeVerify), "verify", "verify-envelope"); failure != nil {
		t.Fatalf("exact negative verifier events: %+v", failure)
	}
	if _, failure := decodeExpectedVerifyFailureEvents([]byte(apply), "apply", "apply-envelope"); failure == nil {
		t.Fatal("expected-failure policy escaped verify command")
	}
}

func TestInstallOuterSuccessCannotMaskUnexpectedNestedVerifyFailure(t *testing.T) {
	runtime := installEvidenceRuntime(t)
	data := validInstallVerifyPayload(runtime, false)
	envelope := map[string]any{
		"schemaVersion": "1.0", "cliVersion": "0.1.0", "command": "verify", "runId": "verify-run",
		"timestampUtc": "2026-07-26T12:00:00Z", "success": true,
		"testMode": map[string]any{"active": true, "scenarioId": runtime.InstallPlan.ScenarioID, "moduleId": runtime.InstallPlan.ModuleID},
		"data":     data, "error": nil,
	}
	decoded, failure := decodeEnvelope(mustInstallJSON(t, envelope), "verify", runtime.InstallPlan.ModuleID, runtime.InstallPlan.ScenarioID)
	if failure != nil {
		t.Fatalf("outer success envelope should decode before nested policy: %+v", failure)
	}
	events := validInstallEvents(runtime, false)
	events[2]["success"], events[2]["failed"] = json.Number("1"), json.Number("1")
	if failure := validateInstallVerifyEvidence(decoded.Data, events, runtime, true); failure == nil {
		t.Fatal("outer success masked a nested command verifier failure")
	}
}

func installEventWire(runID, phase string, total, success, failed int) string {
	reason, message := "", "Verified installed"
	if phase == "plan" {
		reason, message = "already_installed", "Already installed"
	}
	return fmt.Sprintf("{\"version\":1,\"runId\":%q,\"timestamp\":\"2026-07-26T12:00:00Z\",\"event\":\"phase\",\"phase\":%q}\n", runID, phase) +
		fmt.Sprintf("{\"version\":1,\"runId\":%q,\"timestamp\":\"2026-07-26T12:00:01Z\",\"event\":\"item\",\"id\":\"Kubernetes.kubectl\",\"driver\":\"winget\",\"name\":\"kubectl\",\"status\":\"present\",\"reason\":%q,\"message\":%q}\n", runID, reason, message) +
		fmt.Sprintf("{\"version\":1,\"runId\":%q,\"timestamp\":\"2026-07-26T12:00:02Z\",\"event\":\"summary\",\"phase\":%q,\"total\":%d,\"success\":%d,\"skipped\":0,\"failed\":%d}\n", runID, phase, total, success, failed)
}

func installEvidenceRuntime(t *testing.T) *scenarioRuntime {
	t.Helper()
	plan := productionKubectlInstallPlan(t)
	return &scenarioRuntime{InstallPlan: plan, Inventory: plan.Inventory}
}

func validInstallApplyPayload(runtime *scenarioRuntime) map[string]any {
	return map[string]any{
		"dryRun":   true,
		"manifest": map[string]any{"path": "$ENDSTATE_ROOT/manifests/install-v1.jsonc", "name": "Endstate validation apps.kubectl", "hash": ""},
		"summary":  map[string]any{"total": 1, "success": 0, "skipped": 1, "failed": 0},
		"actions": []any{map[string]any{
			"id": runtime.Inventory.AppID, "ref": runtime.Inventory.Ref, "driver": runtime.Inventory.Driver,
			"source": runtime.Inventory.Source, "name": runtime.Inventory.DisplayName, "status": "present",
			"reason": "already_installed", "message": "Already installed", "manual": nil,
		}},
		"packageModuleMap": map[string]any{"winget:" + runtime.Inventory.Ref: []any{runtime.InstallPlan.ModuleID}},
	}
}

func validInstallVerifyPayload(runtime *scenarioRuntime, passing bool) map[string]any {
	commandStatus, commandMessage := "fail", "Command not found: kubectl"
	pass, fail := 1, 1
	if passing {
		commandStatus = "pass"
		commandMessage = "Command exists: $ENDSTATE_ROOT/state/validation-tools/kubectl.exe"
		pass, fail = 2, 0
	}
	return map[string]any{
		"manifest": map[string]any{"path": "$ENDSTATE_ROOT/manifests/install-v1.jsonc", "name": "Endstate validation apps.kubectl"},
		"summary":  map[string]any{"total": 2, "pass": pass, "fail": fail},
		"results": []any{
			map[string]any{"type": "app", "id": runtime.Inventory.AppID, "ref": runtime.Inventory.Ref, "driver": runtime.Inventory.Driver, "name": runtime.Inventory.DisplayName, "status": "pass"},
			map[string]any{"type": "command-exists", "status": commandStatus, "message": commandMessage},
		},
	}
}

func validInstallEvents(runtime *scenarioRuntime, apply bool) []map[string]any {
	phase := "verify"
	reason, message := "", "Verified installed"
	total := json.Number("2")
	if apply {
		phase = "plan"
		reason, message = "already_installed", "Already installed"
		total = json.Number("1")
	}
	return []map[string]any{
		{"event": "phase", "phase": phase},
		{"event": "item", "id": runtime.Inventory.Ref, "driver": runtime.Inventory.Driver, "name": runtime.Inventory.DisplayName, "status": "present", "reason": reason, "message": message},
		{"event": "summary", "phase": phase, "total": total, "success": total, "skipped": json.Number("0"), "failed": json.Number("0")},
	}
}

func mustInstallJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func cloneInstallMap(t *testing.T, value map[string]any) map[string]any {
	t.Helper()
	var clone map[string]any
	if err := json.Unmarshal(mustInstallJSON(t, value), &clone); err != nil {
		t.Fatal(err)
	}
	return clone
}

func cloneInstallEvents(t *testing.T, value []map[string]any) []map[string]any {
	t.Helper()
	var clone []map[string]any
	raw := mustInstallJSON(t, value)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&clone); err != nil {
		t.Fatal(err)
	}
	return clone
}
