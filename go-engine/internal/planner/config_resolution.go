// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import (
	"encoding/json"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// Resolution describes source/target configuration compatibility. It is
// deliberately independent from TerminalStatus, which describes what happened
// during one invocation.
type Resolution string

const (
	ResolutionDirect           Resolution = "direct"
	ResolutionMigrate          Resolution = "migrate"
	ResolutionIncompatible     Resolution = "incompatible"
	ResolutionUnknown          Resolution = "unknown"
	ResolutionLegacyUnverified Resolution = "legacy_unverified"
)

func (r Resolution) String() string { return string(r) }

// TerminalStatus is the final per-config-set outcome exposed in envelopes.
// In-progress staging, commit, and rollback states belong to events instead.
type TerminalStatus string

const (
	StatusPlanned        TerminalStatus = "planned"
	StatusRestored       TerminalStatus = "restored"
	StatusSkipped        TerminalStatus = "skipped"
	StatusFailed         TerminalStatus = "failed"
	StatusRolledBack     TerminalStatus = "rolled_back"
	StatusRollbackFailed TerminalStatus = "rollback_failed"
)

func (s TerminalStatus) String() string { return string(s) }

// ResolutionReason is a stable machine-readable explanation for a resolution
// or terminal execution outcome. Success is represented by a nil
// *ResolutionReason.
type ResolutionReason string

const (
	ReasonUnknownGeneration                 ResolutionReason = "unknown_generation"
	ReasonAmbiguousGeneration               ResolutionReason = "ambiguous_generation"
	ReasonDowngradeUnsupported              ResolutionReason = "downgrade_unsupported"
	ReasonMigrationPathMissing              ResolutionReason = "migration_path_missing"
	ReasonAmbiguousTargetInstance           ResolutionReason = "ambiguous_target_instance"
	ReasonTargetNotDetected                 ResolutionReason = "target_not_detected"
	ReasonMappedTargetNotDetected           ResolutionReason = "mapped_target_not_detected"
	ReasonMappedTargetIncompatible          ResolutionReason = "mapped_target_incompatible"
	ReasonTargetCollision                   ResolutionReason = "target_collision"
	ReasonPayloadIntegrityFailed            ResolutionReason = "payload_integrity_failed"
	ReasonUnsupportedModuleSchema           ResolutionReason = "unsupported_module_schema"
	ReasonCatalogModuleMissing              ResolutionReason = "catalog_module_missing"
	ReasonConfigSetMissing                  ResolutionReason = "config_set_missing"
	ReasonSourceGenerationUnknown           ResolutionReason = "source_generation_unknown"
	ReasonSourceGenerationDefinitionChanged ResolutionReason = "source_generation_definition_changed"
	ReasonAppRunning                        ResolutionReason = "app_running"
	ReasonRecoveryRequired                  ResolutionReason = "recovery_required"
	ReasonRestoreFiltered                   ResolutionReason = "restore_filtered"
	ReasonRestoreNotEnabled                 ResolutionReason = "restore_not_enabled"
	ReasonTargetDetectionFailed             ResolutionReason = "target_detection_failed"
	ReasonStagingValidationFailed           ResolutionReason = "staging_validation_failed"
	ReasonBackupFailed                      ResolutionReason = "backup_failed"
	ReasonJournalIntentFailed               ResolutionReason = "journal_intent_failed"
	ReasonCommitFailed                      ResolutionReason = "commit_failed"
	ReasonTargetValidationFailed            ResolutionReason = "target_validation_failed"
	ReasonJournalCompletionFailed           ResolutionReason = "journal_completion_failed"
	ReasonAlreadyUpToDate                   ResolutionReason = "already_up_to_date"
)

func (r ResolutionReason) String() string { return string(r) }

// InstanceEvidence records the portable, non-secret evidence used to identify
// a source or target instance. Host-local roots are intentionally absent from
// the envelope-facing planner model.
type InstanceEvidence struct {
	Type     string `json:"type"`
	AppID    string `json:"appId,omitempty"`
	Backend  string `json:"backend,omitempty"`
	Platform string `json:"platform,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Driver   string `json:"driver,omitempty"`
}

// SourceInstance is the immutable capture-time instance identity and version
// evidence normalized for planning.
type SourceInstance struct {
	ID                string           `json:"id"`
	DetectorID        string           `json:"detectorId"`
	RawVersion        string           `json:"rawVersion"`
	NormalizedVersion string           `json:"normalizedVersion"`
	Evidence          InstanceEvidence `json:"evidence"`
}

// SourceCapture is one independently addressable captured config set. Bundle
// source facts remain distinct from the current catalog's target knowledge.
type SourceCapture struct {
	CaptureID             string         `json:"captureId"`
	ModuleID              string         `json:"moduleId"`
	ConfigSetID           string         `json:"configSetId"`
	Instance              SourceInstance `json:"sourceInstance"`
	Generation            string         `json:"sourceGeneration"`
	GenerationFingerprint string         `json:"sourceGenerationFingerprint"`
	ModuleRevision        string         `json:"captureModuleRevision"`

	// CaptureModuleSchemaVersion and PayloadIntegrityFailed are verified
	// bundle facts used during planning; they are not additional envelope data.
	CaptureModuleSchemaVersion int  `json:"-"`
	PayloadIntegrityFailed     bool `json:"-"`
}

// TargetInstance is a current-catalog target candidate for one config set.
// The target generation and module revision are current trusted knowledge,
// never values copied from a bundle-supplied module snapshot.
type TargetInstance struct {
	ID                    string           `json:"id"`
	ModuleID              string           `json:"moduleId"`
	DetectorID            string           `json:"detectorId"`
	RawVersion            string           `json:"rawVersion"`
	NormalizedVersion     string           `json:"normalizedVersion"`
	Evidence              InstanceEvidence `json:"evidence"`
	Generation            string           `json:"targetGeneration,omitempty"`
	GenerationFingerprint string           `json:"targetGenerationFingerprint,omitempty"`
	ModuleRevision        string           `json:"restoreModuleRevision"`

	// Root is host-local detection data used to expand current trusted restore
	// declarations. It must never enter a portable envelope.
	Root string `json:"-"`
}

// ConfigResolution is the stable envelope-facing result for one captured
// config set. Resolution and Status are intentionally independent axes.
type ConfigResolution struct {
	CaptureID                   string            `json:"captureId"`
	ModuleID                    string            `json:"moduleId"`
	ConfigSetID                 string            `json:"configSetId"`
	SourceInstance              *SourceInstance   `json:"sourceInstance,omitempty"`
	SourceInstanceID            string            `json:"sourceInstanceId,omitempty"`
	TargetInstanceID            string            `json:"targetInstanceId,omitempty"`
	TargetCandidates            []TargetInstance  `json:"targetCandidates"`
	SourceGeneration            string            `json:"sourceGeneration,omitempty"`
	SourceGenerationFingerprint string            `json:"sourceGenerationFingerprint,omitempty"`
	TargetGeneration            string            `json:"targetGeneration,omitempty"`
	Resolution                  Resolution        `json:"resolution"`
	Reason                      *ResolutionReason `json:"reason"`
	MigrationPath               []string          `json:"migrationPath"`
	CaptureModuleRevision       string            `json:"captureModuleRevision,omitempty"`
	RestoreModuleRevision       string            `json:"restoreModuleRevision,omitempty"`
	ResolvedTargets             []string          `json:"resolvedTargets"`
	Status                      TerminalStatus    `json:"status"`
	Label                       string            `json:"label"`
	Message                     string            `json:"message"`
	Remediation                 *string           `json:"remediation"`
}

// MarshalJSON preserves the contract that collections are [] rather than null
// even while a plan has no migration edges or resolved concrete targets.
func (r ConfigResolution) MarshalJSON() ([]byte, error) {
	type wireResolution ConfigResolution
	if r.MigrationPath == nil {
		r.MigrationPath = []string{}
	}
	if r.TargetCandidates == nil {
		r.TargetCandidates = []TargetInstance{}
	}
	if r.ResolvedTargets == nil {
		r.ResolvedTargets = []string{}
	}
	return json.Marshal(wireResolution(r))
}

// ConfigPlan groups normalized per-set planning inputs and their resolutions.
type ConfigPlan struct {
	Sets    []PlanSet               `json:"sets"`
	Summary ConfigResolutionSummary `json:"summary"`
}

// ConfigResolutionSummary reports compatibility totals and terminal execution
// accounting. Construction semantics live in SummarizeConfigResolutions.
type ConfigResolutionSummary struct {
	Total            int `json:"total"`
	Direct           int `json:"direct"`
	Migrate          int `json:"migrate"`
	Incompatible     int `json:"incompatible"`
	Unknown          int `json:"unknown"`
	LegacyUnverified int `json:"legacyUnverified"`
	Selected         int `json:"selected"`
	Skipped          int `json:"skipped"`
	Failed           int `json:"failed"`
}

// SummarizeConfigResolutions applies the locked envelope accounting rules.
// Rolled-back outcomes are selected attempts and failures even when rollback
// successfully restored pre-run state.
func SummarizeConfigResolutions(resolutions []ConfigResolution) ConfigResolutionSummary {
	summary := ConfigResolutionSummary{Total: len(resolutions)}
	for _, result := range resolutions {
		switch result.Resolution {
		case ResolutionDirect:
			summary.Direct++
		case ResolutionMigrate:
			summary.Migrate++
		case ResolutionIncompatible:
			summary.Incompatible++
		case ResolutionUnknown:
			summary.Unknown++
		case ResolutionLegacyUnverified:
			summary.LegacyUnverified++
		}

		switch result.Status {
		case StatusPlanned, StatusRestored, StatusFailed, StatusRolledBack, StatusRollbackFailed:
			summary.Selected++
		case StatusSkipped:
			summary.Skipped++
		}
		switch result.Status {
		case StatusFailed, StatusRolledBack, StatusRollbackFailed:
			summary.Failed++
		}
	}
	return summary
}

// PlanSet is one independently planned captured config set and its current
// target candidates.
type PlanSet struct {
	Source          SourceCapture    `json:"source"`
	TargetInstances []TargetInstance `json:"targetInstances"`
	Resolution      ConfigResolution `json:"resolution"`

	// TargetGenerationDef and MigrationEdges are pinned declarative data for
	// later staging. They are internal because modules remain data, not output.
	TargetGenerationDef *modules.GenerationDef     `json:"-"`
	MigrationEdges      []modules.MigrationEdgeDef `json:"-"`
}
