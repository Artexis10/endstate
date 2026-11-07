// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
)

func verifyCapturedConfigurations(flags VerifyFlags, mf *manifest.Manifest, emitter *events.Emitter) ([]VerifyItem, *envelope.Error) {
	if len(mf.ConfigCaptures) == 0 {
		return nil, nil
	}
	runtime, envErr := newConfigRestoreRuntime(configRestoreBuildRequest{
		Manifest: mf, ManifestPath: flags.Manifest, RepoRoot: resolveRepoRootFn(),
		RestoreFilter: flags.restoreFilter, RestoreTargets: flags.restoreTargets,
	})
	if envErr != nil {
		return nil, envErr
	}
	ctx := desktopSettingsContext(context.Background())
	coordinator := newConfigRestoreCoordinator(runtime, newStandaloneConfigRestoreEvidenceSource(mf.Apps))
	plan, err := coordinator.Preview(ctx)
	if err != nil {
		return nil, configRestoreInternalError(err.Error())
	}
	stageParent := ""
	if currentValidationMode != nil {
		stageParent = currentValidationMode.Root()
		if err := currentValidationMode.ValidateSandboxPath(stageParent); err != nil {
			return nil, validationRuntimeIsolationFailure("verify.config", "staging", err)
		}
	}
	boundary := newConfigRestoreHostBoundary(currentValidationMode)
	_, processObserver := newConfigRestorePlatformAdapters(currentValidationMode)
	items := make([]VerifyItem, 0, len(plan.Sets))
	for _, set := range plan.Sets {
		item := VerifyItem{Type: "config", ID: set.Source.CaptureID, Name: set.Source.ModuleID, Status: "skipped"}
		if module := runtime.catalog.modules[set.Source.ModuleID]; module != nil && module.DisplayName != "" {
			item.Name = module.DisplayName
		}
		if set.Resolution.Reason != nil {
			item.Reason = set.Resolution.Reason.String()
			item.Message = set.Resolution.Message
			if set.Resolution.Status == planner.StatusFailed || *set.Resolution.Reason == planner.ReasonPayloadIntegrityFailed {
				item.Status = "fail"
			}
		} else if set.TargetGenerationDef == nil {
			item.Reason, item.Message = "config_content_unchecked", "No supported captured content could be checked."
		} else {
			source, ok := runtime.sourceForCapture(set.Source.CaptureID)
			if !ok {
				item.Status, item.Reason, item.Message = "fail", "payload_integrity_failed", "Captured payload is unavailable."
			} else {
				item = verifyCapturedConfigSet(ctx, item, set, source, runtime.catalog.boundTypedCodec(set), runtime.processPatterns(set.Source.ModuleID), processObserver, boundary, stageParent)
			}
		}
		status := "skipped"
		switch item.Status {
		case "pass":
			status = "present"
		case "fail":
			status = "failed"
		}
		emitter.EmitItem(item.ID, "", status, item.Reason, item.Message, item.Name)
		items = append(items, item)
	}
	return items, nil
}

// boundTypedCodec returns the codec of a set that the registry binds to
// typed-roundtrip for its target generation, and nil for every other set.
func (snapshot *configCatalogSnapshot) boundTypedCodec(set planner.PlanSet) *settingscodec.TypedCodec {
	authority, ok := snapshot.currentHomeAuthority(set)
	if !ok || authority.Disposition != hmregistry.TypedRoundTrip {
		return nil
	}
	codec, _ := settingscodec.Typed(authority.Codec)
	return codec
}

// verifyCapturedConfigSet compares one captured set with its target. A non-nil
// typed codec compares a Home Manager set by decoded value.
func verifyCapturedConfigSet(ctx context.Context, item VerifyItem, set planner.PlanSet, source configRestoreSource, typed *settingscodec.TypedCodec, patterns []string, observer configrestore.ProcessObserver, boundary configrestore.HostBoundary, stageParent string) VerifyItem {
	stage, err := stageConfigRestoreSetFn(ctx, migration.StageRequest{
		CaptureID: set.Source.CaptureID, PayloadRoot: source.payloadRoot, PayloadManifest: source.payloadManifest,
		SourceGeneration: set.Source.Generation, TargetGeneration: set.TargetGenerationDef,
		MigrationEdges: set.MigrationEdges, Mapping: set.Mapping, TempParent: stageParent,
	})
	if err != nil {
		item.Status, item.Reason, item.Message = "fail", "staging_validation_failed", err.Error()
		if migration.CodeOf(err) == migration.CodePayloadIntegrityFailed {
			item.Reason = "payload_integrity_failed"
		}
		return item
	}
	defer stage.Close()
	if set.SettingsStrategy == modules.RealizationHomeManager {
		files, err := configrestore.ResolveHomeFiles(ctx, configrestore.Request{Stage: stage, Plan: set, Boundary: boundary, ProcessPatterns: patterns, ProcessObserver: observer})
		if err != nil {
			return configVerifyReadFailure(item, err)
		}
		if typed != nil && len(files) == 1 {
			return verifyTypedHomeFile(item, typed, files[0])
		}
		expected, err := homeFilesExpectations(files)
		if err != nil {
			item.Status, item.Reason, item.Message = "fail", "config_read_failed", err.Error()
			return item
		}
		if len(expected.Files) == 0 {
			item.Status, item.Reason, item.Message = "skipped", "config_content_unchecked", "No captured file requires a content check."
			return item
		}
		reason, message := checkHomeFileExpectations(expected)
		item.Status, item.Reason, item.Message = "pass", reason, "Target configuration matches captured desired content."
		if reason != "" {
			item.Status, item.Message = "fail", message
		}
		return item
	}
	materialized, err := materializeConfigRestoreSetFn(ctx, configrestore.Request{
		Stage: stage, Plan: set, Boundary: boundary, ProcessPatterns: patterns, ProcessObserver: observer,
	})
	if err != nil {
		return configVerifyReadFailure(item, err)
	}
	comparison, err := configrestore.CompareMaterialized(ctx, materialized)
	switch {
	case err != nil:
		item.Status, item.Reason, item.Message = "fail", "config_read_failed", err.Error()
	case comparison.Missing:
		item.Status, item.Reason, item.Message = "fail", driver.ReasonMissing, "Captured configuration is missing from its target."
	case comparison.Drifted:
		item.Status, item.Reason, item.Message = "fail", driver.ReasonConfigDrift, "Target configuration differs from captured desired content."
	case comparison.Checked == 0 || comparison.Unsupported > 0:
		item.Status, item.Reason = "skipped", "config_content_unchecked"
		item.Message = fmt.Sprintf("Checked %d filesystem operations; %d unsupported operations remain unchecked.", comparison.Checked, comparison.Unsupported)
	default:
		item.Status, item.Reason = "pass", ""
		item.Message = "Target configuration matches captured desired content."
	}
	return item
}

// verifyTypedHomeFile compares the captured value with the live target by the
// codec's decoding. Home Manager renders the value in its own layout, so the live
// bytes differ from the staged canonical encoding by design (decision 30).
func verifyTypedHomeFile(item VerifyItem, codec *settingscodec.TypedCodec, file configrestore.HomeFile) VerifyItem {
	staged, _, err := safepath.ReadRegularFileBounded(file.Source, settingscodec.MaxContentSize)
	if err != nil {
		item.Status, item.Reason, item.Message = "fail", "config_read_failed", err.Error()
		return item
	}
	want, err := codec.Decode(staged)
	if err != nil {
		item.Status, item.Reason, item.Message = "fail", "staging_validation_failed", err.Error()
		return item
	}
	if len(want) == 0 {
		// Apply renders nothing for an empty value, so the target holds no captured content.
		item.Status, item.Reason, item.Message = "skipped", "config_content_unchecked", "The captured settings value is empty, so no content is checked."
		return item
	}
	live, err := readDeclaredHomeFile(file.Target, settingscodec.MaxContentSize)
	if err != nil {
		item.Status = "fail"
		item.Reason, item.Message = homeFileReadFailure(file.Target, err)
		return item
	}
	if got, err := codec.Decode(live); err != nil || !codec.Equal(got, want) {
		item.Status, item.Reason, item.Message = "fail", driver.ReasonConfigDrift, "Declared Home Manager settings differ from the captured value: "+file.Target
		return item
	}
	item.Status, item.Reason, item.Message = "pass", "", "Target configuration matches captured desired content."
	return item
}

// configVerifyReadFailure maps a failed staged-set resolution to verify's
// vocabulary for both settings writers: a real read failure or a replaced
// target fails; an unsupported declaration or a running app is skipped.
func configVerifyReadFailure(item VerifyItem, err error) VerifyItem {
	item.Status, item.Reason, item.Message = "fail", "config_read_failed", err.Error()
	switch configrestore.CodeOf(err) {
	case configrestore.CodeAppRunning:
		item.Status, item.Reason = "skipped", planner.ReasonAppRunning.String()
	case configrestore.CodeUnsupportedRestore:
		item.Status, item.Reason = "skipped", "config_content_unchecked"
	case configrestore.CodeTargetStateUnsupported:
		// A target that no longer holds a file has lost the captured content: drift, not an unchecked set.
		item.Status, item.Reason, item.Message = "fail", driver.ReasonConfigDrift, "Target configuration differs from captured desired content."
	}
	return item
}

func summarizeVerifyItems(items []VerifyItem) VerifySummary {
	summary := VerifySummary{Total: len(items)}
	for _, item := range items {
		switch item.Status {
		case "pass":
			summary.Pass++
		case "fail":
			summary.Fail++
		case "skipped":
			summary.Skipped++
		}
	}
	return summary
}
