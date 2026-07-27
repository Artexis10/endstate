// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

const (
	LiveCampaignSchemaVersion = 1
	maxLiveAuthorityBodyBytes = 64 * 1024
)

type LiveCampaignMode string

const (
	LiveCampaignDiagnosticBaseline     LiveCampaignMode = "diagnostic-baseline"
	LiveCampaignScheduledQualification LiveCampaignMode = "scheduled-qualification"
)

// LiveCampaign is the reviewed, public controller record. It deliberately has
// no derived identity field: a self-hash inside this record would be circular.
type LiveCampaign struct {
	SchemaVersion          int              `json:"schemaVersion"`
	Mode                   LiveCampaignMode `json:"mode"`
	Repository             string           `json:"repository"`
	WorkflowPath           string           `json:"workflowPath"`
	Event                  string           `json:"event"`
	Ref                    string           `json:"ref"`
	ControllerCommit       string           `json:"controllerCommit"`
	TestedCheckoutCommit   string           `json:"testedCheckoutCommit"`
	RunID                  uint64           `json:"runId"`
	RunAttempt             int              `json:"runAttempt"`
	TrustedActorClass      string           `json:"trustedActorClass"`
	EngineSHA256           string           `json:"engineSha256"`
	ValidatorSHA256        string           `json:"validatorSha256"`
	GoToolchain            string           `json:"goToolchain"`
	BuildPolicy            string           `json:"buildPolicy"`
	PackageDriver          string           `json:"packageDriver"`
	PackageRef             string           `json:"packageRef"`
	PackageArguments       []string         `json:"packageArguments"`
	ModuleID               string           `json:"moduleId"`
	ModuleRevision         string           `json:"moduleRevision"`
	ValidationSourceSHA256 string           `json:"validationSourceSha256"`
	SeedSHA256             string           `json:"seedSha256"`
	ComparatorSHA256       string           `json:"comparatorSha256"`
	TargetsSHA256          string           `json:"targetsSha256"`
	ObserverSHA256         string           `json:"observerSha256"`
	WorkflowPolicySHA256   string           `json:"workflowPolicySha256"`
	PhaseNonce             string           `json:"phaseNonce"`
	ExpiresAt              time.Time        `json:"expiresAt"`
}

func ValidateLiveCampaign(campaign LiveCampaign) error {
	if campaign.SchemaVersion != LiveCampaignSchemaVersion || campaign.Repository != "Artexis10/endstate" || campaign.WorkflowPath != ".github/workflows/hosted-live.yml" || campaign.Ref != "refs/heads/main" || !liveCommitSHA(campaign.ControllerCommit) || !liveCommitSHA(campaign.TestedCheckoutCommit) || campaign.TrustedActorClass != "User" || !liveCampaignHashes(campaign) || !liveGoPatch(campaign.GoToolchain) || !strings.Contains(campaign.BuildPolicy, "-trimpath") || !strings.Contains(campaign.BuildPolicy, "-buildid=") || campaign.PackageDriver != "winget" || campaign.PackageRef != "Notepad++.Notepad++" || !validLiveModuleID(campaign.ModuleID) || campaign.ModuleID != "apps.notepad-plus-plus" || campaign.ExpiresAt.IsZero() || campaign.ExpiresAt.Location() != time.UTC {
		return fmt.Errorf("live campaign identity is invalid")
	}
	if !liveExactPackageArguments(campaign.PackageArguments, campaign.PackageRef) {
		return fmt.Errorf("live campaign package arguments are invalid")
	}
	switch campaign.Mode {
	case LiveCampaignDiagnosticBaseline:
		if campaign.Event != "workflow_dispatch" || campaign.RunID != 0 || campaign.RunAttempt != 0 || campaign.ControllerCommit != campaign.TestedCheckoutCommit {
			return fmt.Errorf("diagnostic baseline campaign is invalid")
		}
	case LiveCampaignScheduledQualification:
		if campaign.Event != "schedule" || (campaign.RunID == 0) != (campaign.RunAttempt == 0) || campaign.RunAttempt > 1 || (campaign.RunID != 0 && campaign.ControllerCommit == campaign.TestedCheckoutCommit) {
			return fmt.Errorf("scheduled qualification campaign is invalid")
		}
	default:
		return fmt.Errorf("live campaign mode is invalid")
	}
	return nil
}

func liveCampaignHashes(campaign LiveCampaign) bool {
	for _, value := range []string{campaign.EngineSHA256, campaign.ValidatorSHA256, campaign.ModuleRevision, campaign.ValidationSourceSHA256, campaign.SeedSHA256, campaign.ComparatorSHA256, campaign.TargetsSHA256, campaign.ObserverSHA256, campaign.WorkflowPolicySHA256, campaign.PhaseNonce} {
		if !lowerSHA256(value) {
			return false
		}
	}
	return true
}

func liveCommitSHA(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, character := range value {
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

func liveGoPatch(value string) bool {
	if !strings.HasPrefix(value, "go") {
		return false
	}
	var major, minor, patch int
	_, err := fmt.Sscanf(value, "go%d.%d.%d", &major, &minor, &patch)
	return err == nil && fmt.Sprintf("go%d.%d.%d", major, minor, patch) == value
}

func liveExactPackageArguments(arguments []string, ref string) bool {
	return len(arguments) == 3 && arguments[0] == "install" && arguments[1] == ref && arguments[2] == "--exact"
}

// CanonicalLiveCampaignIdentity excludes only the controller commit. The
// reviewed record remains stable when an unrelated controller commit carries it;
// runtime authority binds that controller commit separately.
func CanonicalLiveCampaignIdentity(campaign LiveCampaign) (string, error) {
	if err := ValidateLiveCampaign(campaign); err != nil {
		return "", err
	}
	canonical := campaign
	canonical.ControllerCommit = ""
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

// ProposedPinnedCampaign turns a protected-main diagnostic result into a
// sanitized record that a later controller commit must review and pin. It does
// not create mutation authority and has no run identity yet.
func (campaign LiveCampaign) ProposedPinnedCampaign() (LiveCampaign, error) {
	if campaign.Mode != LiveCampaignDiagnosticBaseline || ValidateLiveCampaign(campaign) != nil {
		return LiveCampaign{}, fmt.Errorf("live baseline campaign is invalid")
	}
	proposal := campaign
	proposal.Mode = LiveCampaignScheduledQualification
	proposal.Event = "schedule"
	proposal.RunID = 0
	proposal.RunAttempt = 0
	return proposal, nil
}

func DecodeLiveCampaignJSON(raw []byte) (LiveCampaign, error) {
	var campaign LiveCampaign
	if err := strictLiveAuthorityJSON(raw, &campaign); err != nil {
		return LiveCampaign{}, fmt.Errorf("decode live campaign: %w", err)
	}
	if err := ValidateLiveCampaign(campaign); err != nil {
		return LiveCampaign{}, err
	}
	return campaign, nil
}

func strictLiveAuthorityJSON(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > maxLiveAuthorityBodyBytes {
		return fmt.Errorf("live authority JSON size is invalid")
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
