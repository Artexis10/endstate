// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestWindowsHostedLiveEvidenceRootRejectsForeignMatchingDirectory(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("RUNNER_TEMP", parent)
	campaign := liveTestCampaign()
	definition := hostedLiveDefinitionForTest(t)
	id, err := CanonicalLiveCampaignIdentity(campaign)
	if err != nil {
		t.Fatalf("campaign identity: %v", err)
	}
	if err := os.Mkdir(filepath.Join(parent, hostedLiveEvidenceResultRootName(id, campaign.RunID, campaign.RunAttempt)), 0o700); err != nil {
		t.Fatalf("create foreign matching root: %v", err)
	}
	if _, err := newHostedLiveEvidenceResultRoot(campaign, definition); err == nil {
		t.Fatal("newHostedLiveEvidenceResultRoot() accepted a foreign matching root")
	}
}

func TestWindowsHostedLiveEvidenceRootPersistsOnlyRegisteredStableCapability(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("RUNNER_TEMP", parent)
	campaign := liveTestCampaign()
	definition := hostedLiveDefinitionForTest(t)
	root, err := newHostedLiveEvidenceResultRoot(campaign, definition)
	if err != nil {
		t.Fatalf("newHostedLiveEvidenceResultRoot() error = %v", err)
	}
	evidence := hostedLiveEvidenceForCampaign(t, campaign, definition)
	if err := persistHostedLiveEvidence(root, evidence); err != nil {
		t.Fatalf("persistHostedLiveEvidence() error = %v", err)
	}
	stored, err := os.ReadFile(filepath.Join(root.path, hostedLiveEvidenceFilename))
	if err != nil {
		t.Fatalf("read evidence: %v", err)
	}
	want, err := encodeHostedLiveEvidence(evidence)
	if err != nil || !reflect.DeepEqual(stored, want) {
		t.Fatalf("stored evidence = %q, %v; want %q", stored, err, want)
	}
	if err := persistHostedLiveEvidence(hostedLiveEvidenceResultRoot{}, evidence); err == nil {
		t.Fatal("persistHostedLiveEvidence() accepted a forged zero capability")
	}
}

func TestWindowsHostedLiveEvidenceRootRejectsSwapAndCampaignMismatch(t *testing.T) {
	parent := t.TempDir()
	t.Setenv("RUNNER_TEMP", parent)
	campaign := liveTestCampaign()
	definition := hostedLiveDefinitionForTest(t)
	root, err := newHostedLiveEvidenceResultRoot(campaign, definition)
	if err != nil {
		t.Fatalf("newHostedLiveEvidenceResultRoot() error = %v", err)
	}
	evidence := hostedLiveEvidenceForCampaign(t, campaign, definition)
	evidence.Inputs.TargetsSHA256 = strings.Repeat("9", 64)
	if err := persistHostedLiveEvidence(root, evidence); err == nil {
		t.Fatal("persistHostedLiveEvidence() accepted campaign-mismatched evidence")
	}
	evidence = hostedLiveEvidenceForCampaign(t, campaign, definition)
	evidence.Inputs.DefinitionSHA256 = strings.Repeat("9", 64)
	if err := persistHostedLiveEvidence(root, evidence); err == nil {
		t.Fatal("persistHostedLiveEvidence() accepted definition-mismatched evidence")
	}

	swappedCampaign := liveTestCampaign()
	swappedCampaign.RunID = 4321
	swappedRoot, err := newHostedLiveEvidenceResultRoot(swappedCampaign, definition)
	if err != nil {
		t.Fatalf("new swapped root: %v", err)
	}
	swappedEvidence := hostedLiveEvidenceForCampaign(t, swappedCampaign, definition)
	windowsHostedLiveEvidenceRootBeforePersist = func(path string) {
		if err := os.Rename(path, path+"-replaced"); err != nil {
			t.Fatalf("move registered root: %v", err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatalf("replace root: %v", err)
		}
	}
	t.Cleanup(func() { windowsHostedLiveEvidenceRootBeforePersist = nil })
	if err := persistHostedLiveEvidence(swappedRoot, swappedEvidence); err == nil {
		t.Fatal("persistHostedLiveEvidence() accepted a swapped root")
	}
}

func hostedLiveDefinitionForTest(t *testing.T) LiveDefinition {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	definition, err := CompileLiveDefinition(repo, "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	return definition
}

func hostedLiveEvidenceForCampaign(t *testing.T, campaign LiveCampaign, definition LiveDefinition) hostedLiveEvidence {
	t.Helper()
	evidence := hostedLiveEvidenceForTest()
	campaignID, err := CanonicalLiveCampaignIdentity(campaign)
	if err != nil {
		t.Fatal(err)
	}
	evidence.Campaign = campaignID
	evidence.Run = hostedLiveEvidenceRun{ID: campaign.RunID, Attempt: campaign.RunAttempt, Event: campaign.Event, Ref: campaign.Ref, TrustedCommit: campaign.ControllerCommit}
	evidence.Engine = hostedLiveEvidenceEngine{Commit: campaign.TestedCheckoutCommit, Version: evidence.Engine.Version, SHA256: campaign.EngineSHA256, ValidatorSHA256: campaign.ValidatorSHA256}
	evidence.Inputs.DefinitionSHA256 = canonicalLiveDefinitionSHA256(t, definition)
	evidence.Inputs.ModuleSHA256 = campaign.ModuleRevision
	evidence.Inputs.ValidationSourceSHA256 = campaign.ValidationSourceSHA256
	evidence.Inputs.SeedSHA256 = campaign.SeedSHA256
	evidence.Inputs.ComparatorSHA256 = campaign.ComparatorSHA256
	evidence.Inputs.TargetsSHA256 = campaign.TargetsSHA256
	evidence.Inputs.ObserverSHA256 = campaign.ObserverSHA256
	evidence.Inputs.WorkflowSHA256 = campaign.WorkflowPolicySHA256
	return evidence
}
