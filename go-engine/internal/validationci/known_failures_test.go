// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationci

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFailureFingerprintUsesExactCanonicalProjection(t *testing.T) {
	fingerprint, err := NewFailureFingerprint("artifact_contract", "capture", "configs")
	if err != nil {
		t.Fatal(err)
	}
	if fingerprint.SHA256 != "5571acd8d85bed142b71464291a9138cd8f0ab50c5774b9a667ce2e388bd5017" {
		t.Fatalf("SHA256 = %q", fingerprint.SHA256)
	}
	if fingerprint.CanonicalJSON() != `{"schemaVersion":1,"code":"artifact_contract","phase":"capture","coordinate":"configs"}` {
		t.Fatalf("canonical JSON = %q", fingerprint.CanonicalJSON())
	}
}

func TestFailureFingerprintIgnoresNonStableEvidence(t *testing.T) {
	left, err := NewFailureFingerprint("artifact_contract", "capture", "configs")
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewFailureFingerprint("artifact_contract", "capture", "configs")
	if err != nil {
		t.Fatal(err)
	}
	if !sameFingerprint(left, right) {
		t.Fatalf("stable fingerprint drift: left=%+v right=%+v", left, right)
	}
}

func TestKnownFailureLedgerRejectsNullFields(t *testing.T) {
	data := validKnownFailureLedgerJSON(t)
	data = strings.Replace(data, `"detail":"informational"`, `"detail":null`, 1)
	if _, err := ParseKnownFailureLedger([]byte(data)); err == nil {
		t.Fatal("ParseKnownFailureLedger accepted a null detail")
	}
}

func TestKnownFailureLedgerRejectsMissingFields(t *testing.T) {
	data := validKnownFailureLedgerJSON(t)
	data = strings.Replace(data, `,"detail":"informational"`, "", 1)
	if _, err := ParseKnownFailureLedger([]byte(data)); err == nil {
		t.Fatal("ParseKnownFailureLedger accepted a missing detail")
	}
}

func TestKnownFailureLedgerRejectsHostileJSON(t *testing.T) {
	valid := validKnownFailureLedgerJSON(t)
	for name, data := range map[string]string{
		"duplicate": strings.Replace(valid, `{"schemaVersion":1,`, `{"schemaVersion":1,"schemaVersion":1,`, 1),
		"unknown":   strings.Replace(valid, `{"schemaVersion":1,`, `{"schemaVersion":1,"extra":true,`, 1),
		"trailing":  valid + `{}`,
		"oversized": strings.Repeat(" ", maxKnownFailureLedgerSize+1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseKnownFailureLedger([]byte(data)); err == nil {
				t.Fatalf("ParseKnownFailureLedger accepted %s JSON", name)
			}
		})
	}
}

func TestKnownFailureLedgerRejectsCaseInsensitiveFieldAliases(t *testing.T) {
	valid := validKnownFailureLedgerJSON(t)
	for name, data := range map[string]string{
		"root canonical then alias":   strings.Replace(valid, `"schemaVersion":1,`, `"schemaVersion":1,"SCHEMAVERSION":1,`, 1),
		"root alias then canonical":   strings.Replace(valid, `"schemaVersion":1,`, `"SCHEMAVERSION":1,"schemaVersion":1,`, 1),
		"nested canonical then alias": strings.Replace(valid, `"moduleId":"apps.example",`, `"moduleId":"apps.example","MODULEID":"apps.example",`, 1),
		"nested alias then canonical": strings.Replace(valid, `"moduleId":"apps.example",`, `"MODULEID":"apps.example","moduleId":"apps.example",`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseKnownFailureLedger([]byte(data)); err == nil {
				t.Fatalf("ParseKnownFailureLedger accepted %s", name)
			}
		})
	}
}

func TestKnownFailureLedgerRejectsOmittedZeroValuedRequiredField(t *testing.T) {
	data := validKnownFailureLedgerJSON(t)
	data = strings.Replace(data, `,"shard":0,`, ",", 1)
	if _, err := ParseKnownFailureLedger([]byte(data)); err == nil {
		t.Fatal("ParseKnownFailureLedger accepted missing previousEvidence.row.shard")
	}
}

func TestKnownFailureLedgerRejectsInvalidInventoryAndEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*KnownFailureLedger){
		"inventory count":        func(ledger *KnownFailureLedger) { ledger.Inventory.ModuleCount++ },
		"inventory digest":       func(ledger *KnownFailureLedger) { ledger.Inventory.SHA256 = strings.Repeat("0", 64) },
		"previous evidence":      func(ledger *KnownFailureLedger) { ledger.KnownFailures[0].PreviousEvidence.Row.ScenarioID = "other" },
		"failure hash":           func(ledger *KnownFailureLedger) { ledger.KnownFailures[0].Failure.SHA256 = strings.Repeat("0", 64) },
		"debt outside inventory": func(ledger *KnownFailureLedger) { ledger.KnownFailures[0].Identity.ModuleID = "apps.other" },
	} {
		t.Run(name, func(t *testing.T) {
			ledger := parseValidKnownFailureLedger(t)
			mutate(&ledger)
			if _, err := ParseKnownFailureLedger(marshalKnownFailureLedger(t, ledger)); err == nil {
				t.Fatalf("ParseKnownFailureLedger accepted %s", name)
			}
		})
	}
}

func TestKnownFailureLedgerRejectsAmbiguousAuthorizations(t *testing.T) {
	ledger := parseValidKnownFailureLedger(t)
	identity := ledger.Inventory.Rows[0]
	fingerprint := ledger.KnownFailures[0].Failure
	changed, err := NewFailureFingerprint("execution_failure", "restore", "settings")
	if err != nil {
		t.Fatal(err)
	}
	ledger.FailureTransitions = []FailureTransition{
		{Identity: identity, From: fingerprint, To: changed, Reason: "first"},
		{Identity: identity, From: fingerprint, To: changed, Reason: "second"},
	}
	if _, err := ParseKnownFailureLedger(marshalKnownFailureLedger(t, ledger)); err == nil {
		t.Fatal("ParseKnownFailureLedger accepted ambiguous transitions")
	}
	ledger = parseValidKnownFailureLedger(t)
	ledger.InventoryRemovals = []InventoryRemoval{{Identity: identity, Reason: "first"}, {Identity: identity, Reason: "second"}}
	if _, err := ParseKnownFailureLedger(marshalKnownFailureLedger(t, ledger)); err == nil {
		t.Fatal("ParseKnownFailureLedger accepted ambiguous removals")
	}
	ledger = parseValidKnownFailureLedger(t)
	ledger.InventoryRemovals = []InventoryRemoval{{Identity: KnownFailureIdentity{ModuleID: "apps.absent", ScenarioID: "default-v1", Kind: "config-roundtrip-v1"}, Reason: "latent removal"}}
	if _, err := ParseKnownFailureLedger(marshalKnownFailureLedger(t, ledger)); err == nil {
		t.Fatal("ParseKnownFailureLedger accepted removal outside inventory")
	}
	ledger = parseValidKnownFailureLedger(t)
	ledger.FailureTransitions = []FailureTransition{{Identity: identity, From: changed, To: fingerprint, Reason: "reversed"}}
	if _, err := ParseKnownFailureLedger(marshalKnownFailureLedger(t, ledger)); err == nil {
		t.Fatal("ParseKnownFailureLedger accepted transition whose from is not current debt")
	}
}

func TestKnownFailureIdentityRejectsDelimiterCollision(t *testing.T) {
	left := KnownFailureIdentity{ModuleID: "apps.example\x00default", ScenarioID: "v1", Kind: "config-roundtrip-v1"}
	right := KnownFailureIdentity{ModuleID: "apps.example", ScenarioID: "default\x00v1", Kind: "config-roundtrip-v1"}
	if _, err := identityKey(left); err == nil {
		t.Fatal("identityKey accepted NUL-containing left identity")
	}
	if _, err := identityKey(right); err == nil {
		t.Fatal("identityKey accepted NUL-containing right identity")
	}
}

func TestEvaluateKnownFailureLedgerConsumesBaseInventoryRemoval(t *testing.T) {
	base := parseValidKnownFailureLedger(t)
	base.InventoryRemovals = []InventoryRemoval{{Identity: base.Inventory.Rows[0], Reason: "reviewed production removal"}}
	head := base
	head.Inventory = KnownFailureInventory{ModuleCount: 0, ScenarioCount: 0, Rows: []KnownFailureIdentity{}}
	head.Inventory.SHA256 = inventorySHA256(head.Inventory)
	head.KnownFailures = []KnownFailure{}
	result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &base, Head: &head, HeadRows: []KnownFailureRow{}})
	if result.Failure != "malformed known-failure candidate" {
		t.Fatalf("failure = %q", result.Failure)
	}
}

func TestEvaluateKnownFailureLedgerStateMachine(t *testing.T) {
	base := parseValidKnownFailureLedger(t)
	identity := base.Inventory.Rows[0]
	original := base.KnownFailures[0].Failure
	changed, err := NewFailureFingerprint("execution_failure", "restore", "settings")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		prepare func(*KnownFailureLedger, *KnownFailureLedger)
		row     KnownFailureRow
		want    string
	}{
		{"matching known debt", func(_, _ *KnownFailureLedger) {}, KnownFailureRow{Identity: identity, Failure: original}, ""},
		{"stale debt after pass", func(_, _ *KnownFailureLedger) {}, KnownFailureRow{Identity: identity, Passed: true}, "stale known-failure debt"},
		{"resolved debt", func(_, head *KnownFailureLedger) { head.KnownFailures = []KnownFailure{} }, KnownFailureRow{Identity: identity, Passed: true}, ""},
		{"removed debt while failed", func(_, head *KnownFailureLedger) { head.KnownFailures = []KnownFailure{} }, KnownFailureRow{Identity: identity, Failure: original}, "removed known-failure debt"},
		{"unreviewed fingerprint change", func(_, head *KnownFailureLedger) { head.KnownFailures[0].Failure = changed }, KnownFailureRow{Identity: identity, Failure: changed}, "unreviewed failure fingerprint transition"},
		{"consumed base transition", func(base, head *KnownFailureLedger) {
			base.FailureTransitions = []FailureTransition{{Identity: identity, From: original, To: changed, Reason: "reviewed transition"}}
			head.KnownFailures[0].Failure = changed
		}, KnownFailureRow{Identity: identity, Failure: changed}, ""},
		{"retained consumed transition is malformed", func(base, head *KnownFailureLedger) {
			transition := FailureTransition{Identity: identity, From: original, To: changed, Reason: "reviewed transition"}
			base.FailureTransitions = []FailureTransition{transition}
			head.KnownFailures[0].Failure = changed
			head.FailureTransitions = []FailureTransition{transition}
		}, KnownFailureRow{Identity: identity, Failure: changed}, "malformed known-failure candidate"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBase, gotHead := cloneKnownFailureLedger(t, base), cloneKnownFailureLedger(t, base)
			tt.prepare(&gotBase, &gotHead)
			result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &gotBase, Head: &gotHead, HeadRows: []KnownFailureRow{tt.row}})
			if result.Failure != tt.want {
				t.Fatalf("failure = %q, want %q", result.Failure, tt.want)
			}
		})
	}
}

func TestEvaluateKnownFailureLedgerRejectsNewFailureAndMissingAuthority(t *testing.T) {
	base := parseValidKnownFailureLedger(t)
	identity := base.Inventory.Rows[0]
	base.KnownFailures = []KnownFailure{}
	failure := parseValidKnownFailureLedger(t).KnownFailures[0].Failure
	head := cloneKnownFailureLedger(t, base)
	head.KnownFailures = []KnownFailure{parseValidKnownFailureLedger(t).KnownFailures[0]}
	result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &base, Head: &head, HeadRows: []KnownFailureRow{{Identity: identity, Failure: failure}}})
	if result.Failure != "new failed row" {
		t.Fatalf("failure = %q", result.Failure)
	}
	result = EvaluateKnownFailureLedger(KnownFailureComparison{Head: &base, HeadRows: []KnownFailureRow{{Identity: identity, Failure: failure}}})
	if result.Failure != "missing known-failure authority" {
		t.Fatalf("missing authority failure = %q", result.Failure)
	}
}

func TestEvaluateKnownFailureLedgerRejectsHeadAddedTransitionSelfBlessing(t *testing.T) {
	base := parseValidKnownFailureLedger(t)
	head := cloneKnownFailureLedger(t, base)
	identity := base.Inventory.Rows[0]
	original := base.KnownFailures[0].Failure
	changed, err := NewFailureFingerprint("execution_failure", "restore", "settings")
	if err != nil {
		t.Fatal(err)
	}
	head.KnownFailures[0].Failure = changed
	head.FailureTransitions = []FailureTransition{{Identity: identity, From: original, To: changed, Reason: "head cannot bless itself"}}
	result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &base, Head: &head, HeadRows: []KnownFailureRow{{Identity: identity, Failure: changed}}})
	if result.Failure != "malformed known-failure candidate" {
		t.Fatalf("failure = %q", result.Failure)
	}
}

func TestEvaluateKnownFailureLedgerRejectsInvalidHeadAddedFutureTransition(t *testing.T) {
	base := parseValidKnownFailureLedger(t)
	head := cloneKnownFailureLedger(t, base)
	identity := base.Inventory.Rows[0]
	changed, err := NewFailureFingerprint("execution_failure", "restore", "settings")
	if err != nil {
		t.Fatal(err)
	}
	head.FailureTransitions = []FailureTransition{{Identity: identity, From: changed, To: base.KnownFailures[0].Failure, Reason: "invalid future transition"}}
	result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &base, Head: &head, HeadRows: []KnownFailureRow{{Identity: identity, Failure: base.KnownFailures[0].Failure}}})
	if result.Failure != "malformed known-failure candidate" {
		t.Fatalf("failure = %q", result.Failure)
	}
}

func TestEvaluateKnownFailureLedgerPinsInventoryDenominator(t *testing.T) {
	base := parseValidKnownFailureLedger(t)
	base.KnownFailures = []KnownFailure{}
	head := cloneKnownFailureLedger(t, base)
	head.Inventory = KnownFailureInventory{ModuleCount: 0, ScenarioCount: 0, Rows: []KnownFailureIdentity{}}
	head.Inventory.SHA256 = inventorySHA256(head.Inventory)
	result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &base, Head: &head})
	if result.Failure != "unauthorized row removal" {
		t.Fatalf("failure = %q", result.Failure)
	}
}

func TestEvaluateKnownFailureLedgerAllowsPassingInventoryAddition(t *testing.T) {
	base := parseValidKnownFailureLedger(t)
	base.KnownFailures = []KnownFailure{}
	head := cloneKnownFailureLedger(t, base)
	addition := KnownFailureIdentity{ModuleID: "apps.new", ScenarioID: "default-v1", Kind: "config-roundtrip-v1"}
	head.Inventory.Rows = append(head.Inventory.Rows, addition)
	head.Inventory.ModuleCount = 2
	head.Inventory.ScenarioCount = 2
	head.Inventory.SHA256 = inventorySHA256(head.Inventory)
	result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &base, Head: &head, HeadRows: []KnownFailureRow{{Identity: base.Inventory.Rows[0], Passed: true}, {Identity: addition, Passed: true}}})
	if result.Failure != "" || len(result.InventoryAdditions) != 1 || len(result.Passed) != 2 {
		t.Fatalf("result = %+v", result)
	}
}

func TestEvaluateKnownFailureLedgerRejectsFailingInventoryAddition(t *testing.T) {
	base := parseValidKnownFailureLedger(t)
	base.KnownFailures = []KnownFailure{}
	head := cloneKnownFailureLedger(t, base)
	addition := KnownFailureIdentity{ModuleID: "apps.new", ScenarioID: "default-v1", Kind: "config-roundtrip-v1"}
	head.Inventory.Rows = append(head.Inventory.Rows, addition)
	head.Inventory.ModuleCount = 2
	head.Inventory.ScenarioCount = 2
	head.Inventory.SHA256 = inventorySHA256(head.Inventory)
	failure := parseValidKnownFailureLedger(t).KnownFailures[0].Failure
	result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &base, Head: &head, HeadRows: []KnownFailureRow{{Identity: base.Inventory.Rows[0], Passed: true}, {Identity: addition, Failure: failure}}})
	if result.Failure != "new failed row" {
		t.Fatalf("failure = %q", result.Failure)
	}
}

func TestEvaluateKnownFailureLedgerAcceptsConsumedInventoryRemoval(t *testing.T) {
	base := parseValidKnownFailureLedger(t)
	base.InventoryRemovals = []InventoryRemoval{{Identity: base.Inventory.Rows[0], Reason: "reviewed production removal"}}
	head := cloneKnownFailureLedger(t, base)
	head.Inventory = KnownFailureInventory{ModuleCount: 0, ScenarioCount: 0, Rows: []KnownFailureIdentity{}}
	head.Inventory.SHA256 = inventorySHA256(head.Inventory)
	head.KnownFailures = []KnownFailure{}
	head.InventoryRemovals = []InventoryRemoval{}
	result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &base, Head: &head})
	if result.Failure != "" || len(result.InventoryRemovals) != 1 {
		t.Fatalf("result = %+v", result)
	}
}

func TestEvaluateKnownFailureLedgerReportsHonestKnownDebtCounts(t *testing.T) {
	ledger := parseValidKnownFailureLedger(t)
	row := KnownFailureRow{Identity: ledger.Inventory.Rows[0], Failure: ledger.KnownFailures[0].Failure}
	result := EvaluateKnownFailureLedger(KnownFailureComparison{Base: &ledger, Head: &ledger, HeadRows: []KnownFailureRow{row}})
	if result.Failure != "" || len(result.Passed) != 0 || len(result.KnownDebt) != 1 || len(result.ResolvedDebt) != 0 {
		t.Fatalf("result = %+v", result)
	}
}

func validKnownFailureLedgerJSON(t *testing.T) string {
	t.Helper()
	identity := KnownFailureIdentity{ModuleID: "apps.example", ScenarioID: "default-v1", Kind: "config-roundtrip-v1"}
	failure, err := NewFailureFingerprint("artifact_contract", "capture", "configs")
	if err != nil {
		t.Fatal(err)
	}
	inventory := KnownFailureInventory{ModuleCount: 1, ScenarioCount: 1, Rows: []KnownFailureIdentity{identity}}
	inventory.SHA256 = inventorySHA256(inventory)
	ledger := KnownFailureLedger{
		SchemaVersion: SchemaVersion,
		Inventory:     inventory,
		KnownFailures: []KnownFailure{{
			Identity: identity,
			PreviousEvidence: PreviousEvidence{
				Commit: strings.Repeat("a", 40), EngineSHA256: strings.Repeat("b", 64), RepositoryHash: strings.Repeat("c", 64),
				Row: LedgerRowID{ModuleID: identity.ModuleID, ModuleRevision: strings.Repeat("d", 64), ScenarioID: identity.ScenarioID, ScenarioKind: identity.Kind, ScenarioDigest: strings.Repeat("e", 64), RowSHA256: strings.Repeat("f", 64)},
			},
			Failure: failure, Detail: "informational",
		}},
		FailureTransitions: []FailureTransition{},
		InventoryRemovals:  []InventoryRemoval{},
	}
	data, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func parseValidKnownFailureLedger(t *testing.T) KnownFailureLedger {
	t.Helper()
	ledger, err := ParseKnownFailureLedger([]byte(validKnownFailureLedgerJSON(t)))
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}

func marshalKnownFailureLedger(t *testing.T, ledger KnownFailureLedger) []byte {
	t.Helper()
	data, err := json.Marshal(ledger)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func cloneKnownFailureLedger(t *testing.T, ledger KnownFailureLedger) KnownFailureLedger {
	t.Helper()
	clone, err := ParseKnownFailureLedger(marshalKnownFailureLedger(t, ledger))
	if err != nil {
		t.Fatal(err)
	}
	return clone
}
