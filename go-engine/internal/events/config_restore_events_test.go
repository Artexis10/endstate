// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package events

import "testing"

func TestEmitConfigMigrationUsesClosedWireShape(t *testing.T) {
	emitter, buffer := captureEmitter("apply-config-test")
	emitter.EmitConfigMigration(ConfigMigrationProgress{
		CaptureID:      "capture-a",
		ConfigSetID:    "preferences",
		Stage:          ConfigMigrationEdge,
		FromGeneration: "g1",
		ToGeneration:   "g2",
		Status:         ConfigProgressCompleted,
		Message:        "migration edge validated",
	})

	event := parseEvent(t, lastLine(buffer))
	assertBaseFields(t, event, "apply-config-test", "config-migration")
	for field, want := range map[string]interface{}{
		"captureId": "capture-a", "configSetId": "preferences", "stage": "edge",
		"fromGeneration": "g1", "toGeneration": "g2", "status": "completed",
		"message": "migration edge validated",
	} {
		if event[field] != want {
			t.Errorf("%s = %#v, want %#v", field, event[field], want)
		}
	}
	if value, exists := event["reason"]; !exists || value != nil {
		t.Errorf("reason = %#v, exists=%v; want explicit null", value, exists)
	}
	if value, exists := event["remediation"]; !exists || value != nil {
		t.Errorf("remediation = %#v, exists=%v; want explicit null", value, exists)
	}
}

func TestEmitConfigMigrationCarriesFailureReasonAndRemediation(t *testing.T) {
	emitter, buffer := captureEmitter("restore-config-test")
	reason := "target_validation_failed"
	remediation := "Close the application and retry."
	emitter.EmitConfigMigration(ConfigMigrationProgress{
		CaptureID: "capture-a", ConfigSetID: "preferences",
		Stage: ConfigMigrationRollback, Status: ConfigProgressFailed,
		Reason: &reason, Message: "rollback could not be verified", Remediation: &remediation,
	})
	event := parseEvent(t, lastLine(buffer))
	if event["reason"] != reason || event["remediation"] != remediation {
		t.Fatalf("failure event = %#v", event)
	}
}

func TestEmitRestoreItemUsesRequiredNullsAndGenerationLinks(t *testing.T) {
	emitter, buffer := captureEmitter("restore-item-test")
	emitter.EmitRestoreItem(RestoreItemProgress{
		ID: "copy:settings", Module: "apps.example", Restorer: "copy",
		Source: "configs/capture-a/settings.json", Target: `C:\Users\Test\settings.json`,
		Status: RestoreItemRestoring, TargetExisted: true, Message: "restoring settings",
		CaptureID: "capture-a", ConfigSetID: "preferences", TargetInstanceID: "instance-b",
		SourceGeneration: "g1", TargetGeneration: "g2",
	})

	event := parseEvent(t, lastLine(buffer))
	assertBaseFields(t, event, "restore-item-test", "restore-item")
	for field, want := range map[string]interface{}{
		"id": "copy:settings", "module": "apps.example", "restorer": "copy",
		"status": "restoring", "targetExisted": true, "captureId": "capture-a",
		"configSetId": "preferences", "targetInstanceId": "instance-b",
		"sourceGeneration": "g1", "targetGeneration": "g2",
	} {
		if event[field] != want {
			t.Errorf("%s = %#v, want %#v", field, event[field], want)
		}
	}
	if value, exists := event["reason"]; !exists || value != nil {
		t.Errorf("reason = %#v, exists=%v; want explicit null", value, exists)
	}
	if value, exists := event["backupPath"]; !exists || value != nil {
		t.Errorf("backupPath = %#v, exists=%v; want explicit null", value, exists)
	}
}

func TestConfigRestoreEventsDisabledAreNoOp(t *testing.T) {
	emitter, buffer := captureEmitter("disabled")
	emitter.enabled = false
	emitter.EmitConfigMigration(ConfigMigrationProgress{})
	emitter.EmitRestoreItem(RestoreItemProgress{})
	if buffer.Len() != 0 {
		t.Fatalf("disabled emitter wrote %q", buffer.String())
	}
}

func TestConfigRestoreEventsRejectOpenEndedEnums(t *testing.T) {
	emitter, buffer := captureEmitter("invalid")
	emitter.EmitConfigMigration(ConfigMigrationProgress{
		Stage: ConfigMigrationStage("script"), Status: ConfigProgressCompleted,
	})
	emitter.EmitConfigMigration(ConfigMigrationProgress{
		Stage: ConfigMigrationEdge, Status: ConfigProgressStatus("running"),
	})
	emitter.EmitRestoreItem(RestoreItemProgress{Status: RestoreItemStatus("installed")})
	if buffer.Len() != 0 {
		t.Fatalf("invalid enums emitted %q", buffer.String())
	}
}
