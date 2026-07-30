// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCampaignCanonicalIdentityExcludesControllerCommitAndBindsPinnedFields(t *testing.T) {
	t.Parallel()

	campaign := liveTestCampaign()
	identity, err := CanonicalLiveCampaignIdentity(campaign)
	if err != nil {
		t.Fatal(err)
	}
	otherRun := campaign
	otherRun.ControllerCommit = strings.Repeat("c", 40)
	otherRun.RunID = 5678
	otherRun.PhaseNonce = strings.Repeat("a", 64)
	otherRun.ExpiresAt = otherRun.ExpiresAt.Add(time.Hour)
	if got, err := CanonicalLiveCampaignIdentity(otherRun); err != nil || got != identity {
		t.Fatalf("runtime-only change = %q, %v; want stable %q", got, err, identity)
	}
	for _, mutate := range []func(*LiveCampaign){
		func(value *LiveCampaign) { value.TestedCheckoutCommit = strings.Repeat("c", 40) },
		func(value *LiveCampaign) { value.EngineSHA256 = strings.Repeat("d", 64) },
		func(value *LiveCampaign) { value.ValidatorSHA256 = strings.Repeat("e", 64) },
		func(value *LiveCampaign) { value.GoToolchain = "go1.24.3" },
		func(value *LiveCampaign) { value.BuildPolicy = "-trimpath;-buildid=endstate-v2" },
		func(value *LiveCampaign) { value.ModuleRevision = strings.Repeat("f", 64) },
		func(value *LiveCampaign) { value.ComparatorSHA256 = strings.Repeat("a", 64) },
		func(value *LiveCampaign) { value.TargetsSHA256 = strings.Repeat("b", 64) },
		func(value *LiveCampaign) { value.ObserverSHA256 = strings.Repeat("c", 64) },
		func(value *LiveCampaign) { value.WorkflowPolicySHA256 = strings.Repeat("d", 64) },
	} {
		changed := campaign
		changed.PackageArguments = append([]string(nil), campaign.PackageArguments...)
		changed.Operations = append([]LiveCampaignOperation(nil), campaign.Operations...)
		mutate(&changed)
		liveTestBindEngineOperations(&changed)
		if got, err := CanonicalLiveCampaignIdentity(changed); err != nil || got == identity {
			t.Fatalf("pinned field change = %q, %v; want identity reset from %q", got, err, identity)
		}
	}
}

func TestLiveCampaignRejectsUntrustedModesAndDrift(t *testing.T) {
	t.Parallel()

	for _, mutate := range []func(*LiveCampaign){
		func(value *LiveCampaign) { value.Repository = "foreign/endstate" },
		func(value *LiveCampaign) { value.WorkflowPath = ".github/workflows/foreign.yml" },
		func(value *LiveCampaign) { value.Event = "push" },
		func(value *LiveCampaign) { value.Ref = "refs/heads/foreign" },
		func(value *LiveCampaign) { value.ControllerCommit = value.TestedCheckoutCommit },
		func(value *LiveCampaign) { value.RunAttempt = 2 },
		func(value *LiveCampaign) { value.TrustedActorClass = "Bot" },
		func(value *LiveCampaign) { value.PackageRef = "Vendor.Foreign" },
		func(value *LiveCampaign) { value.PackageArguments = []string{"install", "Vendor.Foreign"} },
		func(value *LiveCampaign) { value.ExpiresAt = time.Time{} },
		func(value *LiveCampaign) { value.BuildPolicy = "-trimpath=false;-buildid=endstate-v1" },
		func(value *LiveCampaign) { value.BuildPolicy = "-trimpath;-buildid=endstate-v1;-x" },
	} {
		candidate := liveTestCampaign()
		mutate(&candidate)
		if err := ValidateLiveCampaign(candidate); err == nil {
			t.Fatalf("ValidateLiveCampaign accepted %#v", candidate)
		}
	}

	baseline := liveTestCampaign()
	baseline.Mode = LiveCampaignDiagnosticBaseline
	baseline.Event = "workflow_dispatch"
	baseline.RunID = 4321
	baseline.RunAttempt = 1
	baseline.TestedCheckoutCommit = baseline.ControllerCommit
	if err := ValidateLiveCampaign(baseline); err != nil {
		t.Fatalf("baseline campaign rejected: %v", err)
	}
	proposal, err := baseline.ProposedPinnedCampaign()
	if err != nil {
		t.Fatal(err)
	}
	if proposal.Mode != LiveCampaignScheduledQualification || proposal.Event != "schedule" || proposal.RunID != 0 || proposal.RunAttempt != 0 || proposal.ControllerCommit != "" || proposal.PhaseNonce != "" || !proposal.ExpiresAt.IsZero() {
		t.Fatalf("proposal = %#v", proposal)
	}
	if roundTrip, err := DecodeLiveCampaignJSON(mustLiveCampaignJSON(t, proposal)); err != nil || !reflect.DeepEqual(roundTrip, proposal) {
		t.Fatalf("proposed campaign roundtrip = %#v, %v", roundTrip, err)
	}
}

func TestLiveCampaignRequiresExactUnattendedWingetUninstall(t *testing.T) {
	campaign := liveTestCampaign()
	campaign.PackageArguments = []string{"uninstall", "--id", "Notepad++.Notepad++", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"}
	for index := range campaign.Operations {
		if campaign.Operations[index].Operation == string(liveOperationWingetExactUninstall) {
			campaign.Operations[index].Arguments = append([]string(nil), campaign.PackageArguments...)
		}
	}
	if err := ValidateLiveCampaign(campaign); err != nil {
		t.Fatalf("ValidateLiveCampaign() error = %v", err)
	}
	for _, arguments := range [][]string{
		{"uninstall", "Notepad++.Notepad++", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"},
		{"uninstall", "--id", "Notepad++.Notepad++", "--source", "winget", "--exact", "--accept-source-agreements", "--disable-interactivity"},
		{"uninstall", "--id", "Notepad++.Notepad++", "--exact", "--source", "winget", "--accept-source-agreements"},
	} {
		candidate := campaign
		candidate.PackageArguments = arguments
		if err := ValidateLiveCampaign(candidate); err == nil {
			t.Fatalf("ValidateLiveCampaign accepted %#v", arguments)
		}
	}
}

func TestValidateLiveCampaignRequiresClosedRuntimeTemplates(t *testing.T) {
	campaign := liveTestCampaign()
	if err := ValidateLiveCampaign(campaign); err != nil {
		t.Fatalf("ValidateLiveCampaign(valid templates) error = %v", err)
	}
	for _, test := range []struct {
		name   string
		mutate func(*LiveCampaign)
	}{
		{"literal executable", func(value *LiveCampaign) { value.Operations[2].Executable = `C:\\runner\\endstate.exe` }},
		{"duplicate root token", func(value *LiveCampaign) {
			value.Operations[2].Arguments = append(value.Operations[2].Arguments, "$ENDSTATE_ROOT")
		}},
		{"root token in executable", func(value *LiveCampaign) { value.Operations[2].Executable = "$ENDSTATE_ROOT\\endstate.exe" }},
		{"winget token outside executable", func(value *LiveCampaign) { value.Operations[2].Arguments[0] = "$WINGET" }},
		{"checkout traversal", func(value *LiveCampaign) { value.Operations[2].Executable = "$CHECKOUT_ROOT\\..\\endstate.exe" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			candidate := liveTestCampaign()
			test.mutate(&candidate)
			if err := ValidateLiveCampaign(candidate); err == nil {
				t.Fatal("ValidateLiveCampaign accepted an unclosed runtime template")
			}
		})
	}
}

func liveTestCampaign() LiveCampaign {
	return LiveCampaign{
		SchemaVersion: LiveCampaignSchemaVersion, Mode: LiveCampaignScheduledQualification,
		Repository: "Artexis10/endstate", WorkflowPath: ".github/workflows/hosted-live.yml", Event: "schedule", Ref: "refs/heads/main",
		ControllerCommit: strings.Repeat("a", 40), TestedCheckoutCommit: strings.Repeat("b", 40), RunID: 1234, RunAttempt: 1, TrustedActorClass: "User",
		EngineSHA256: strings.Repeat("c", 64), ValidatorSHA256: strings.Repeat("d", 64), GoToolchain: "go1.24.2", BuildPolicy: "-trimpath;-buildid=endstate-v1",
		PackageDriver: "winget", PackageRef: "Notepad++.Notepad++", PackageArguments: []string{"uninstall", "--id", "Notepad++.Notepad++", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"}, Operations: liveTestCampaignOperations(),
		ModuleID: "apps.notepad-plus-plus", ModuleRevision: strings.Repeat("e", 64), ValidationSourceSHA256: strings.Repeat("f", 64), SeedSHA256: strings.Repeat("a", 64),
		ComparatorSHA256: strings.Repeat("b", 64), TargetsSHA256: strings.Repeat("c", 64), ObserverSHA256: strings.Repeat("d", 64), WorkflowPolicySHA256: strings.Repeat("e", 64), PhaseNonce: strings.Repeat("f", 64),
		ExpiresAt: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

func liveTestCampaignOperations() []LiveCampaignOperation {
	operations := make([]LiveCampaignOperation, 0, 15)
	for sequence, operation := range map[uint64]liveOperation{1: liveOperationWingetExactUninstall, 2: liveOperationDeclaredTargetWipe, 3: liveOperationEngineApply, 4: liveOperationEngineVerify, 5: liveOperationHashBoundSeed, 6: liveOperationEngineCapture, 7: liveOperationWingetExactUninstall, 8: liveOperationDeclaredTargetWipe, 9: liveOperationEngineRebuild, 10: liveOperationEngineRevert, 11: liveOperationEngineRebuild, 12: liveOperationEngineRebuild, 13: liveOperationWingetExactUninstall, 14: liveOperationDeclaredTargetWipe, 15: liveOperationAttemptRootCleanup} {
		entry := LiveCampaignOperation{Sequence: sequence, Operation: string(operation)}
		if operation == liveOperationWingetExactUninstall {
			entry.Executable, entry.ExecutableSHA256 = liveTemplateWinget, liveTemplateWinget
			entry.Arguments = []string{"uninstall", "--id", "Notepad++.Notepad++", "--exact", "--source", "winget", "--accept-source-agreements", "--disable-interactivity"}
		} else if operation != liveOperationDeclaredTargetWipe && operation != liveOperationAttemptRootCleanup {
			digest := strings.Repeat("c", 64)
			entry.Executable, entry.Arguments, entry.ExecutableSHA256 = liveTemplateOperation(operation, digest)
			if operation == liveOperationHashBoundSeed {
				entry.Directory = liveTemplateEndstateRoot + `\seed`
			} else {
				entry.Directory = liveTemplateCheckoutRoot + `\go-engine`
				entry.Environment = map[string]string{"ENDSTATE_ROOT": liveTemplateEndstateRoot}
			}
		}
		operations = append(operations, entry)
	}
	sort.Slice(operations, func(left, right int) bool { return operations[left].Sequence < operations[right].Sequence })
	return operations
}
