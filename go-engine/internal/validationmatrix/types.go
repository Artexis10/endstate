// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package validationmatrix loads and validates repository-owned module
// validation sidecars without duplicating production module interpretation.
package validationmatrix

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

const (
	CodeMissingSidecar          = "missing_validation_sidecar"
	CodeDuplicateSidecar        = "duplicate_validation_sidecar"
	CodeMismatchedSidecar       = "mismatched_validation_sidecar"
	CodeStaleSidecar            = "stale_validation_sidecar"
	CodeInvalidSidecar          = "invalid_validation_sidecar"
	CodeInvalidModuleCatalog    = "invalid_module_catalog"
	CodeUnknownScenarioKind     = "unknown_scenario_kind"
	CodeUnknownLiveMode         = "unknown_live_mode"
	CodeMissingAssertionMinimum = "missing_assertion_minimum"
	CodeInvalidClassification   = "invalid_module_classification"
	CodeMissingV2Scenario       = "missing_schema_v2_scenario"
	CodeDuplicateScenario       = "duplicate_validation_scenario"
	CodeInvalidQuarantine       = "invalid_quarantine"
	CodeInvalidFixture          = "invalid_fixture"
	CodeInvalidLivePolicy       = "invalid_live_policy"
	CodeInvalidOneWayReview     = "invalid_one_way_review"
	CodeInvalidSchemaV2Identity = "invalid_schema_v2_identity"
)

const CodeMissingProductionValidation = "missing_production_validation"

type ScenarioKind string

const (
	ScenarioConfigRoundtripV1  ScenarioKind = "config-roundtrip-v1"
	ScenarioConfigGenerationV2 ScenarioKind = "config-generation-v2"
	ScenarioConfigMigrationV2  ScenarioKind = "config-migration-v2"
	ScenarioCaptureContract    ScenarioKind = "capture-contract"
	ScenarioRestoreContract    ScenarioKind = "restore-contract"
	ScenarioInstallContract    ScenarioKind = "install-contract"
)

type LiveMode string

const (
	LiveHosted        LiveMode = "hosted"
	LiveCandidate     LiveMode = "candidate"
	LiveBlocked       LiveMode = "blocked"
	LiveLab           LiveMode = "lab"
	LiveManual        LiveMode = "manual"
	LiveNotApplicable LiveMode = "not-applicable"
)

type FixtureType string

const (
	FixtureAuto        FixtureType = "auto"
	FixtureDeclarative FixtureType = "declarative"
)

type IdentityMode string

const (
	IdentityLiteral            IdentityMode = "literal"
	IdentityDerivedFromFixture IdentityMode = "derived-from-fixture"
)

type ProofLevel string

const (
	ProofCatalog             ProofLevel = "catalog"
	ProofEngineContract      ProofLevel = "engine-contract"
	ProofConfigRoundtripV1   ProofLevel = "config-roundtrip-v1"
	ProofConfigRoundtripV2   ProofLevel = "config-roundtrip-v2"
	ProofLiveInstall         ProofLevel = "live-install"
	ProofLiveConfigRoundtrip ProofLevel = "live-config-roundtrip"
)

const (
	AssertionCaptured         = "captured"
	AssertionPayload          = "payload"
	AssertionProvenance       = "provenance"
	AssertionRewrittenRestore = "rewrittenRestore"
	AssertionContent          = "content"
	AssertionRebuild          = "rebuild"
	AssertionVerify           = "verify"
	AssertionNestedSummary    = "nestedSummary"
	AssertionRevert           = "revert"
	AssertionGeneration       = "generation"
	AssertionValidation       = "validation"
	AssertionMigration        = "migration"
	AssertionRestored         = "restored"
	AssertionAppReferences    = "appReferences"
)

type ValidationRecord struct {
	SchemaVersion  int             `json:"schemaVersion"`
	ModuleID       string          `json:"moduleId"`
	ModuleRevision string          `json:"moduleRevision"`
	Synthetic      SyntheticPolicy `json:"synthetic"`
	Live           LivePolicy      `json:"live"`
	Quarantines    []Quarantine    `json:"quarantines,omitempty"`

	FilePath string `json:"-"`
}

type SyntheticPolicy struct {
	Scenarios []Scenario `json:"scenarios"`
}

type Scenario struct {
	ID                string               `json:"id"`
	Mode              ScenarioKind         `json:"mode"`
	Fixture           Fixture              `json:"fixture"`
	TimeoutSeconds    int                  `json:"timeoutSeconds"`
	MinimumAssertions map[string]int       `json:"minimumAssertions"`
	Expected          *SchemaV2Expectation `json:"expected,omitempty"`
	Review            *OneWayReview        `json:"review,omitempty"`
}

type OneWayReview struct {
	Decision   string `json:"decision"`
	ReasonCode string `json:"reasonCode"`
	Reviewer   string `json:"reviewer"`
	ReviewedOn string `json:"reviewedOn"`
	Evidence   string `json:"evidence"`
}

type Fixture struct {
	Type   FixtureType `json:"type"`
	Path   string      `json:"path,omitempty"`
	SHA256 string      `json:"sha256,omitempty"`
}

type SchemaV2Expectation struct {
	IdentityMode  IdentityMode `json:"identityMode"`
	DetectorID    string       `json:"detectorId,omitempty"`
	CaptureID     string       `json:"captureId,omitempty"`
	ConfigSetID   string       `json:"configSetId"`
	InstanceID    string       `json:"instanceId,omitempty"`
	GenerationID  string       `json:"generationId"`
	Fingerprint   string       `json:"fingerprint"`
	MigrationFrom string       `json:"migrationFrom,omitempty"`
	MigrationTo   string       `json:"migrationTo,omitempty"`

	detectorIDSet bool
	captureIDSet  bool
	instanceIDSet bool
}

func (expectation *SchemaV2Expectation) UnmarshalJSON(data []byte) error {
	type wire SchemaV2Expectation
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded wire
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	*expectation = SchemaV2Expectation(decoded)
	_, expectation.detectorIDSet = fields["detectorId"]
	_, expectation.captureIDSet = fields["captureId"]
	_, expectation.instanceIDSet = fields["instanceId"]
	return nil
}

type LivePolicy struct {
	Mode                    LiveMode     `json:"mode"`
	Driver                  string       `json:"driver,omitempty"`
	Ref                     string       `json:"ref,omitempty"`
	Seed                    string       `json:"seed,omitempty"`
	Comparator              string       `json:"comparator,omitempty"`
	ProofMode               ProofLevel   `json:"proofMode,omitempty"`
	PRTimeoutMinutes        int          `json:"prTimeoutMinutes,omitempty"`
	ScheduledTimeoutMinutes int          `json:"scheduledTimeoutMinutes,omitempty"`
	RunnerLabel             string       `json:"runnerLabel,omitempty"`
	Trust                   *TrustHashes `json:"trust,omitempty"`
	ReasonCode              string       `json:"reasonCode,omitempty"`
	Explanation             string       `json:"explanation,omitempty"`
}

type TrustHashes struct {
	SeedSHA256       string `json:"seedSha256,omitempty"`
	ComparatorSHA256 string `json:"comparatorSha256,omitempty"`
}

type Quarantine struct {
	ProofLevel         ProofLevel `json:"proofLevel"`
	OS                 string     `json:"os"`
	RunnerImage        string     `json:"runnerImage"`
	FailureFingerprint string     `json:"failureFingerprint"`
	IssueURL           string     `json:"issueUrl"`
	ReasonCode         string     `json:"reasonCode"`
	Owner              string     `json:"owner"`
	ExpiresOn          string     `json:"expiresOn"`
}

type Catalog struct {
	Modules map[string]*modules.Module
	Records map[string]ValidationRecord
}
