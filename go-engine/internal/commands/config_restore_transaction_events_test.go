// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

func TestConfigRestoreTransactionObserverProjectsCommitValidationAndRollback(t *testing.T) {
	buffer := &bytes.Buffer{}
	emitter := events.NewEmitterWithWriter("transaction-events", true, buffer)
	observer := newConfigRestoreTransactionObserver(emitter, "capture-a", "preferences")
	observer.Observe(configrestore.TransactionObservation{
		Stage: configrestore.TransactionStageCommit, Progress: configrestore.TransactionProgressStarted,
		ActionIndex: 0, ValidationIndex: -1,
	})
	observer.Observe(configrestore.TransactionObservation{
		Stage: configrestore.TransactionStageValidation, Progress: configrestore.TransactionProgressFailed,
		ActionIndex: -1, ValidationIndex: 0, Reason: configrestore.ReasonTargetValidationFailed,
		Err: errors.New("invalid json"),
	})
	observer.Observe(configrestore.TransactionObservation{
		Stage: configrestore.TransactionStageRollback, Progress: configrestore.TransactionProgressCompleted,
		ActionIndex: 0, ValidationIndex: -1,
	})

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("events = %d, want 3: %q", len(lines), buffer.String())
	}
	decoded := make([]map[string]any, len(lines))
	for index, line := range lines {
		if err := json.Unmarshal([]byte(line), &decoded[index]); err != nil {
			t.Fatalf("event %d: %v", index, err)
		}
	}
	if decoded[0]["stage"] != "commit" || decoded[0]["status"] != "started" {
		t.Fatalf("first event = %#v", decoded[0])
	}
	if decoded[1]["stage"] != "validation" || decoded[1]["status"] != "failed" ||
		decoded[1]["reason"] != "target_validation_failed" || decoded[1]["remediation"] == nil {
		t.Fatalf("failed event = %#v", decoded[1])
	}
	if decoded[2]["stage"] != "rollback" || decoded[2]["status"] != "completed" {
		t.Fatalf("last event = %#v", decoded[2])
	}
}
