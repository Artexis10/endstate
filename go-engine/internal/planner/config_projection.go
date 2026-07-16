// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package planner

import "fmt"

// ProjectConfigResolution creates the portable, envelope-facing result for a
// plan set. It may be called again after execution changes Reason or Status;
// presentation is always re-authored from the current machine state.
func ProjectConfigResolution(set PlanSet) ConfigResolution {
	projected := set.Resolution

	if hasSourceFacts(set.Source) {
		projected.CaptureID = set.Source.CaptureID
		projected.ModuleID = set.Source.ModuleID
		projected.ConfigSetID = set.Source.ConfigSetID
		projected.SourceGeneration = set.Source.Generation
		projected.SourceGenerationFingerprint = set.Source.GenerationFingerprint
		projected.CaptureModuleRevision = set.Source.ModuleRevision
	}
	projected.SourceInstance = nil
	projected.SourceInstanceID = ""
	if set.Source.Instance.ID != "" {
		source := set.Source.Instance
		projected.SourceInstance = &source
		projected.SourceInstanceID = source.ID
	}

	projected.TargetCandidates = make([]TargetInstance, len(set.TargetInstances))
	for index, target := range set.TargetInstances {
		target.Root = ""
		projected.TargetCandidates[index] = target
	}
	projected.MigrationPath = append([]string{}, set.Resolution.MigrationPath...)
	projected.ResolvedTargets = append([]string{}, set.Resolution.ResolvedTargets...)
	if projected.Resolution == ResolutionLegacyUnverified {
		projected.SourceInstance = nil
		projected.SourceInstanceID = ""
		projected.TargetInstanceID = ""
		projected.TargetCandidates = []TargetInstance{}
		projected.SourceGeneration = ""
		projected.SourceGenerationFingerprint = ""
		projected.TargetGeneration = ""
		projected.MigrationPath = []string{}
		projected.CaptureModuleRevision = ""
		projected.RestoreModuleRevision = ""
	}
	if projected.Reason != nil {
		reason := *projected.Reason
		projected.Reason = &reason
	}

	projected.Label = resolutionLabel(projected.Resolution)
	projected.Message, projected.Remediation = resolutionPresentation(projected)
	return projected
}

func hasSourceFacts(source SourceCapture) bool {
	return source.CaptureID != "" || source.ModuleID != "" || source.ConfigSetID != "" ||
		source.Instance.ID != "" || source.Generation != "" || source.GenerationFingerprint != "" ||
		source.ModuleRevision != ""
}

func resolutionLabel(resolution Resolution) string {
	switch resolution {
	case ResolutionDirect:
		return "Compatible"
	case ResolutionMigrate:
		return "Will be upgraded"
	case ResolutionIncompatible:
		return "Not supported"
	case ResolutionUnknown, ResolutionLegacyUnverified:
		return "Compatibility unknown"
	default:
		return "Compatibility unknown"
	}
}

func resolutionPresentation(resolution ConfigResolution) (string, *string) {
	if resolution.Reason != nil {
		if message, remediation, known := reasonPresentation(*resolution.Reason); known {
			return message, remediation
		}
	}

	switch resolution.Resolution {
	case ResolutionDirect:
		return "Settings are compatible with the selected target.", nil
	case ResolutionMigrate:
		if resolution.SourceGeneration != "" && resolution.TargetGeneration != "" {
			return fmt.Sprintf("Settings will be upgraded from %s to %s before restore.",
				resolution.SourceGeneration, resolution.TargetGeneration), nil
		}
		return "Settings will be upgraded before restore.", nil
	case ResolutionIncompatible:
		return "These settings cannot be restored to the selected target.",
			presentationRemediation("Choose a compatible target version or restore without these settings.")
	case ResolutionLegacyUnverified:
		return "These settings predate compatibility checks, so compatibility could not be verified.",
			presentationRemediation("Review the legacy settings and enable restore only if you trust this backup.")
	case ResolutionUnknown:
		return "Settings compatibility could not be verified.",
			presentationRemediation("Review the compatibility details or choose another detected target.")
	default:
		return "Settings compatibility could not be verified.",
			presentationRemediation("Review the compatibility details before restoring.")
	}
}

func reasonPresentation(reason ResolutionReason) (string, *string, bool) {
	message := ""
	remediation := ""
	switch reason {
	case ReasonUnknownGeneration:
		message = "The target version does not match a known configuration generation."
		remediation = "Install or select a supported target version, or update the module catalog."
	case ReasonAmbiguousGeneration:
		message = "The target version matches more than one configuration generation."
		remediation = "Update the module catalog so the target version matches exactly one generation."
	case ReasonDowngradeUnsupported:
		message = "Settings cannot be restored to an older configuration generation."
		remediation = "Choose a target with the same or a newer supported configuration generation."
	case ReasonMigrationPathMissing:
		message = "No supported migration path exists between the source and target generations."
		remediation = "Choose a compatible target version or add a reviewed forward migration to the module catalog."
	case ReasonAmbiguousTargetInstance:
		message = "More than one compatible target instance was detected."
		remediation = "Select a target instance explicitly."
	case ReasonTargetNotDetected:
		message = "No target instance was detected for these settings."
		remediation = "Install the application or select a detected target, then retry."
	case ReasonMappedTargetNotDetected:
		message = "The selected target instance is no longer detected."
		remediation = "Refresh detection and select an available target instance."
	case ReasonMappedTargetIncompatible:
		message = "The selected target instance is not compatible with these settings."
		remediation = "Select a compatible target instance."
	case ReasonTargetCollision:
		message = "This config set overlaps another selected restore target."
		remediation = "Restore only one of the colliding config sets or change the target selection."
	case ReasonPayloadIntegrityFailed:
		message = "The captured settings payload failed integrity verification."
		remediation = "Create a new backup or use an untampered backup."
	case ReasonUnsupportedModuleSchema:
		message = "The current engine cannot safely interpret this module schema."
		remediation = "Update Endstate or use a module schema supported by this engine."
	case ReasonCatalogModuleMissing:
		message = "The current module catalog does not contain this application module."
		remediation = "Install or update the module catalog, then retry."
	case ReasonConfigSetMissing:
		message = "The current module no longer defines this config set."
		remediation = "Update the module catalog or restore without this config set."
	case ReasonSourceGenerationUnknown:
		message = "The current module does not recognize the captured configuration generation."
		remediation = "Update the module catalog with the captured generation history."
	case ReasonSourceGenerationDefinitionChanged:
		message = "The captured generation fingerprint is not accepted by the current module."
		remediation = "Use a catalog that accepts this historical fingerprint or create a new backup."
	case ReasonAppRunning:
		message = "The application must be closed before these settings can be restored."
		remediation = "Close the application and retry."
	case ReasonRecoveryRequired:
		message = "A previous config restore requires recovery before new changes can begin."
		remediation = "Review the recovery failure, restore a safe state, and retry."
	case ReasonRestoreFiltered:
		message = "This config set was excluded by the restore filter."
		remediation = "Change the restore filter to include this module and retry."
	case ReasonRestoreNotEnabled:
		message = "Settings restore is not enabled for this invocation."
		remediation = "Enable settings restore and retry."
	case ReasonTargetDetectionFailed:
		message = "Target detection failed, so compatibility could not be determined."
		remediation = "Review the detection failure, resolve its cause, and retry."
	case ReasonStagingValidationFailed:
		message = "The staged settings failed validation before target changes began."
		remediation = "Review the staging validation failure, resolve its cause, and retry."
	case ReasonBackupFailed:
		message = "Required target backups could not be created."
		remediation = "Review the backup failure, resolve its cause, and retry."
	case ReasonJournalIntentFailed:
		message = "The restore journal could not be written before target changes began."
		remediation = "Review the journal storage failure, resolve its cause, and retry."
	case ReasonCommitFailed:
		message = "The settings transaction failed while writing the target configuration."
		remediation = "Review the target write failure, resolve its cause, and retry."
	case ReasonTargetValidationFailed:
		message = "The restored target configuration failed validation."
		remediation = "Review the target validation failure before retrying."
	case ReasonJournalCompletionFailed:
		message = "The restore journal could not record transaction completion."
		remediation = "Review the journal storage failure before retrying."
	case ReasonAlreadyUpToDate:
		return "The target already has the desired settings.", nil, true
	default:
		return "", nil, false
	}
	return message, presentationRemediation(remediation), true
}

func presentationRemediation(value string) *string { return &value }
