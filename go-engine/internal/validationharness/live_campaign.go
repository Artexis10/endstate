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
	SchemaVersion          int                     `json:"schemaVersion"`
	Mode                   LiveCampaignMode        `json:"mode"`
	Repository             string                  `json:"repository"`
	WorkflowPath           string                  `json:"workflowPath"`
	Event                  string                  `json:"event"`
	Ref                    string                  `json:"ref"`
	ControllerCommit       string                  `json:"controllerCommit"`
	TestedCheckoutCommit   string                  `json:"testedCheckoutCommit"`
	RunID                  uint64                  `json:"runId"`
	RunAttempt             int                     `json:"runAttempt"`
	TrustedActorClass      string                  `json:"trustedActorClass"`
	EngineSHA256           string                  `json:"engineSha256"`
	ValidatorSHA256        string                  `json:"validatorSha256"`
	GoToolchain            string                  `json:"goToolchain"`
	BuildPolicy            string                  `json:"buildPolicy"`
	PackageDriver          string                  `json:"packageDriver"`
	PackageRef             string                  `json:"packageRef"`
	PackageArguments       []string                `json:"packageArguments"`
	Operations             []LiveCampaignOperation `json:"operations"`
	ModuleID               string                  `json:"moduleId"`
	ModuleRevision         string                  `json:"moduleRevision"`
	ValidationSourceSHA256 string                  `json:"validationSourceSha256"`
	SeedSHA256             string                  `json:"seedSha256"`
	ComparatorSHA256       string                  `json:"comparatorSha256"`
	TargetsSHA256          string                  `json:"targetsSha256"`
	ObserverSHA256         string                  `json:"observerSha256"`
	WorkflowPolicySHA256   string                  `json:"workflowPolicySha256"`
	PhaseNonce             string                  `json:"phaseNonce"`
	ExpiresAt              time.Time               `json:"expiresAt"`
}

// LiveCampaignOperation is a reviewed invocation template. It is copied into
// the private permit; callers never supply process shape to mint authority.
type LiveCampaignOperation struct {
	Sequence         uint64            `json:"sequence"`
	Operation        string            `json:"operation"`
	Executable       string            `json:"executable"`
	ExecutableSHA256 string            `json:"executableSha256"`
	Arguments        []string          `json:"arguments"`
	Directory        string            `json:"directory"`
	Environment      map[string]string `json:"environment"`
}

func ValidateLiveCampaign(campaign LiveCampaign) error {
	if campaign.SchemaVersion != LiveCampaignSchemaVersion || campaign.Repository != "Artexis10/endstate" || campaign.WorkflowPath != ".github/workflows/hosted-live.yml" || campaign.Ref != "refs/heads/main" || !liveCommitSHA(campaign.TestedCheckoutCommit) || campaign.TrustedActorClass != "User" || !liveCampaignHashes(campaign) || !liveGoPatch(campaign.GoToolchain) || !liveBuildPolicy(campaign.BuildPolicy) || campaign.PackageDriver != "winget" || campaign.PackageRef != "Notepad++.Notepad++" || !validLiveModuleID(campaign.ModuleID) || campaign.ModuleID != "apps.notepad-plus-plus" {
		return fmt.Errorf("live campaign identity is invalid")
	}
	if !liveExactPackageArguments(campaign.PackageArguments, campaign.PackageRef) {
		return fmt.Errorf("live campaign package arguments are invalid")
	}
	if !validLiveCampaignOperations(campaign.Operations, campaign.PackageRef, campaign.EngineSHA256) {
		return fmt.Errorf("live campaign operation plan is invalid")
	}
	switch campaign.Mode {
	case LiveCampaignDiagnosticBaseline:
		if campaign.Event != "workflow_dispatch" || campaign.RunID == 0 || campaign.RunAttempt != 1 || !liveCommitSHA(campaign.ControllerCommit) || campaign.ControllerCommit != campaign.TestedCheckoutCommit || !lowerSHA256(campaign.PhaseNonce) || campaign.ExpiresAt.IsZero() || campaign.ExpiresAt.Location() != time.UTC {
			return fmt.Errorf("diagnostic baseline campaign is invalid")
		}
	case LiveCampaignScheduledQualification:
		if campaign.Event != "schedule" || (campaign.RunID == 0 && (campaign.RunAttempt != 0 || campaign.ControllerCommit != "" || campaign.PhaseNonce != "" || !campaign.ExpiresAt.IsZero())) || (campaign.RunID != 0 && (campaign.RunAttempt != 1 || !liveCommitSHA(campaign.ControllerCommit) || campaign.ControllerCommit == campaign.TestedCheckoutCommit || !lowerSHA256(campaign.PhaseNonce) || campaign.ExpiresAt.IsZero() || campaign.ExpiresAt.Location() != time.UTC)) {
			return fmt.Errorf("scheduled qualification campaign is invalid")
		}
	default:
		return fmt.Errorf("live campaign mode is invalid")
	}
	return nil
}

func validLiveCampaignOperations(operations []LiveCampaignOperation, packageRef, engineSHA256 string) bool {
	if len(operations) < 10 || len(operations) > 11 {
		return false
	}
	seen := make(map[uint64]struct{}, len(operations))
	for _, operation := range operations {
		if !liveAuthorityOperationSequence(liveOperation(operation.Operation), operation.Sequence) || !lowerSHA256(operation.ExecutableSHA256) || operation.Executable == "" || len(operation.Arguments) == 0 || len(operation.Arguments) > 64 || len(operation.Environment) > len(liveProcessEnvironmentAllowlist) {
			return false
		}
		if _, duplicate := seen[operation.Sequence]; duplicate {
			return false
		}
		seen[operation.Sequence] = struct{}{}
		for _, argument := range operation.Arguments {
			if !validLiveProcessValue(argument) {
				return false
			}
		}
		for name, value := range operation.Environment {
			if _, allowed := liveProcessEnvironmentAllowlist[name]; !allowed || !validLiveProcessEnvironmentValue(value) {
				return false
			}
		}
		if liveOperation(operation.Operation) == liveOperationWingetExactUninstall && !liveExactWingetUninstallArguments(operation.Arguments, packageRef) {
			return false
		}
		if liveCampaignEngineOperation(liveOperation(operation.Operation)) && operation.ExecutableSHA256 != engineSHA256 {
			return false
		}
	}
	_, final := seen[11]
	return final
}

func liveCampaignEngineOperation(operation liveOperation) bool {
	switch operation {
	case liveOperationEngineApply, liveOperationEngineVerify, liveOperationEngineCapture, liveOperationEngineRebuild, liveOperationEngineRevert:
		return true
	default:
		return false
	}
}

func liveExactWingetUninstallArguments(arguments []string, ref string) bool {
	return len(arguments) == 3 && arguments[0] == "uninstall" && arguments[1] == ref && arguments[2] == "--exact"
}

func liveCampaignHashes(campaign LiveCampaign) bool {
	for _, value := range []string{campaign.EngineSHA256, campaign.ValidatorSHA256, campaign.ModuleRevision, campaign.ValidationSourceSHA256, campaign.SeedSHA256, campaign.ComparatorSHA256, campaign.TargetsSHA256, campaign.ObserverSHA256, campaign.WorkflowPolicySHA256} {
		if !lowerSHA256(value) {
			return false
		}
	}
	return true
}

func liveBuildPolicy(value string) bool {
	return value == "-trimpath;-buildid=endstate-v1" || value == "-trimpath;-buildid=endstate-v2"
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
	return liveExactWingetUninstallArguments(arguments, ref)
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
	canonical.RunID = 0
	canonical.RunAttempt = 0
	canonical.PhaseNonce = ""
	canonical.ExpiresAt = time.Time{}
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
	proposal.ControllerCommit = ""
	proposal.PhaseNonce = ""
	proposal.ExpiresAt = time.Time{}
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
