// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

type liveConfigRestoreGuard interface {
	CreateTransactionRoot(captureID string) (string, error)
	DiscardTransactionRoot(root string) error
	RegisterLegacyJournal(path string) (*configrestore.StoreMember, error)
	Close() error
}

type beginLiveConfigRestoreFunc func(context.Context, string, string, configrestore.RegistryMutator) (liveConfigRestoreGuard, error)

var beginLiveConfigRestoreFn beginLiveConfigRestoreFunc = func(
	ctx context.Context,
	stateDir string,
	runID string,
	registry configrestore.RegistryMutator,
) (liveConfigRestoreGuard, error) {
	return configrestore.BeginLive(ctx, stateDir, runID, registry)
}

var stageConfigRestoreSetFn = func(ctx context.Context, request migration.StageRequest) (*migration.StageResult, error) {
	return migration.NewEngine().Stage(ctx, request)
}
var materializeConfigRestoreSetFn = configrestore.Materialize
var executeLiveConfigRestoreSetFn = executeLiveConfigRestoreSet

type configRestoreExecutionSession struct {
	runtime     *configRestoreRuntime
	coordinator configRestoreCoordinator
}

type configRestoreDetectionFailureProvider interface {
	DetectionFailures() []configRestoreDetectionFailure
}

type configRestoreExecutionOptions struct {
	RestoreEnabled  bool
	DryRun          bool
	RunID           string
	StateDir        string
	ManifestPath    string
	ManifestDir     string
	ExportRoot      string
	BackupDir       string
	JournalLogsDir  string
	Emitter         *events.Emitter
	Registry        configrestore.RegistryMutator
	ProcessObserver configrestore.ProcessObserver
}

type configRestoreExecutionResult struct {
	Plan         planner.ConfigPlan
	RestoreItems []restore.RestoreResult
	Results      []restore.RestoreResult
	EventResults []restore.RestoreResult
	JournalPath  string
}

type preparedConfigRestoreExecution struct {
	setIndex        int
	stage           *migration.StageResult
	materialized    *configrestore.MaterializedSet
	transactionRoot string
}

func newConfigRestoreExecutionSession(
	runtime *configRestoreRuntime,
	evidence configRestoreEvidenceSource,
) *configRestoreExecutionSession {
	return &configRestoreExecutionSession{
		runtime: runtime, coordinator: newConfigRestoreCoordinator(runtime, evidence),
	}
}

func (session *configRestoreExecutionSession) Preview(ctx context.Context) (planner.ConfigPlan, error) {
	if session == nil || session.coordinator == nil {
		return emptyConfigRestorePlan(), fmt.Errorf("config restore execution session is not initialized")
	}
	return session.coordinator.Preview(ctx)
}

func (session *configRestoreExecutionSession) Execute(
	ctx context.Context,
	options configRestoreExecutionOptions,
) (result configRestoreExecutionResult, envErr *envelope.Error) {
	result = configRestoreExecutionResult{
		Plan: emptyConfigRestorePlan(), RestoreItems: []restore.RestoreResult{}, Results: []restore.RestoreResult{},
		EventResults: []restore.RestoreResult{},
	}
	if session == nil || session.runtime == nil || session.coordinator == nil {
		return result, configRestoreInternalError("configuration restore session is not initialized")
	}
	if options.Emitter != nil && shouldFrameConfigRestoreEvents(session.runtime.inputs, options.RestoreEnabled) {
		options.Emitter.EmitPhase("restore")
		defer func() {
			emitConfigRestoreSummary(options.Emitter, result.EventResults)
		}()
	}
	plan, err := session.coordinator.Final(ctx, options.RestoreEnabled)
	if err != nil {
		return result, configRestoreInternalError(err.Error())
	}
	if provider, ok := session.coordinator.(configRestoreDetectionFailureProvider); ok {
		emitConfigRestoreDetectionFailures(options.Emitter, provider.DetectionFailures())
	}
	markSelectedConfigRestoreSetsPlanned(&plan)
	if !options.RestoreEnabled {
		// Coordinator implementations used by production already apply this;
		// retaining the overlay here keeps alternate prepared coordinators safe.
		overlayConfigRestoreNotEnabled(&plan)
	}
	result.Plan = plan
	legacyPreview := previewLegacyConfigRestorePlan(session.runtime.inputs, options.RestoreEnabled)
	if !options.RestoreEnabled {
		emitGenerationConfigResolutions(options.Emitter, result.Plan)
		emitGenerationConfigResolutions(options.Emitter, legacyPreview)
		result.Plan = mergeConfigRestorePlans(result.Plan, legacyPreview)
		return result, nil
	}

	if options.DryRun {
		prepared := session.stageAndMaterialize(ctx, &result.Plan, options)
		defer closePreparedConfigRestoreExecutions(prepared)
		collisions := applyUnifiedConcreteConfigRestoreCollisions(&result.Plan, prepared, session.runtime.inputs, options)
		applyLegacyConcreteConfigRestoreCollisions(&legacyPreview, collisions)
		prepared = executablePreparedConfigRestoreSets(result.Plan, prepared)
		dryRunOutcomes := make(map[int]configRestoreSetOutcome, len(prepared))
		dryRunItems := make(map[int][]restore.RestoreResult, len(prepared))
		for _, item := range prepared {
			outcome := inspectDryRunConfigRestoreSet(ctx, item.materialized, options.Registry)
			applyConfigRestoreSetOutcome(&result.Plan.Sets[item.setIndex], outcome)
			items := configRestoreResultsForSet(result.Plan.Sets[item.setIndex], item.materialized, outcome)
			dryRunOutcomes[item.setIndex] = outcome
			dryRunItems[item.setIndex] = items
			result.RestoreItems = append(result.RestoreItems, items...)
			result.EventResults = append(result.EventResults, items...)
		}
		emitGenerationConfigResolutions(options.Emitter, result.Plan)
		emitGenerationConfigResolutions(options.Emitter, legacyPreview)
		for _, item := range prepared {
			emitGenerationConfigRestoreStarts(
				options.Emitter, result.Plan.Sets[item.setIndex], item.materialized, dryRunOutcomes[item.setIndex].Prepared,
			)
			items := dryRunItems[item.setIndex]
			emitConfigRestoreItems(options.Emitter, items, result.Plan.Sets[item.setIndex].Source.ModuleID)
		}
		legacy, ordinary, eventResults, runErr := executeLegacyAndOrdinaryConfigRestores(session.runtime.inputs, options, collisions)
		if runErr != nil {
			return result, runErr
		}
		result.Plan = mergeConfigRestorePlans(result.Plan, legacy.Plan)
		result.RestoreItems = append(result.RestoreItems, legacy.RestoreItems...)
		result.RestoreItems = append(result.RestoreItems, ordinary...)
		result.EventResults = append(result.EventResults, eventResults...)
		result.Results = append(result.Results, result.RestoreItems...)
		recomputeConfigPlanSummary(&result.Plan)
		return result, nil
	}
	needsGeneration := selectedGenerationConfigRestoreExists(result.Plan)
	needsNonGeneration := selectedLegacyConfigRestoreExists(session.runtime.inputs) || len(session.runtime.inputs.ordinaryRestores) > 0
	if !needsGeneration && !needsNonGeneration {
		emitGenerationConfigResolutions(options.Emitter, result.Plan)
		result.Plan = mergeConfigRestorePlans(result.Plan, legacyPreview)
		recomputeConfigPlanSummary(&result.Plan)
		return result, nil
	}
	if beginLiveConfigRestoreFn == nil {
		emitGenerationConfigResolutions(options.Emitter, result.Plan)
		return result, configRestoreInternalError("live configuration restore lock/recovery coordinator is unavailable")
	}
	guard, err := beginLiveConfigRestoreFn(ctx, options.StateDir, options.RunID, options.Registry)
	if err != nil {
		emitGenerationConfigResolutions(options.Emitter, result.Plan)
		if errors.Is(err, configrestore.ErrRecoveryRequired) {
			return result, envelope.NewError(envelope.ErrRestoreFailed, "Configuration restore recovery is required.").
				WithDetail(map[string]string{"reason": planner.ReasonRecoveryRequired.String(), "diagnostic": err.Error()}).
				WithRemediation("Resolve the pending configuration recovery failure, then retry.")
		}
		return result, envelope.NewError(envelope.ErrRestoreFailed, "Configuration restore recovery failed.").
			WithDetail(map[string]string{"reason": "recovery_failed", "diagnostic": err.Error()}).
			WithRemediation("Resolve the pending configuration recovery failure, then retry.")
	}
	if guard == nil {
		emitGenerationConfigResolutions(options.Emitter, result.Plan)
		return result, configRestoreInternalError("live configuration restore coordinator returned a nil guard")
	}
	defer func() {
		if closeErr := guard.Close(); closeErr != nil && envErr == nil {
			envErr = configRestoreInternalError("failed to release configuration restore guard: " + closeErr.Error())
		}
	}()

	// Recovery must complete while the guard is held before materialization:
	// merge and append strategies may read the live target to derive desired bytes.
	prepared := session.stageAndMaterialize(ctx, &result.Plan, options)
	defer closePreparedConfigRestoreExecutions(prepared)
	collisions := applyUnifiedConcreteConfigRestoreCollisions(&result.Plan, prepared, session.runtime.inputs, options)
	applyLegacyConcreteConfigRestoreCollisions(&legacyPreview, collisions)
	for index := range prepared {
		item := &prepared[index]
		set := &result.Plan.Sets[item.setIndex]
		if !selectedConfigRestorePlanSet(*set) {
			continue
		}
		transactionRoot, err := guard.CreateTransactionRoot(set.Source.CaptureID)
		if err != nil {
			outcome := failedConfigRestoreSet(planner.ReasonBackupFailed, err, true, nil)
			applyConfigRestoreSetOutcome(set, outcome)
			continue
		}
		item.transactionRoot = transactionRoot
	}
	defer func() {
		var discardErrors []error
		for _, item := range prepared {
			if item.transactionRoot != "" {
				if err := guard.DiscardTransactionRoot(item.transactionRoot); err != nil {
					discardErrors = append(discardErrors, err)
				}
			}
		}
		if envErr == nil {
			if err := errors.Join(discardErrors...); err != nil {
				envErr = configRestoreInternalError("failed to discard unused configuration restore transactions: " + err.Error())
			}
		}
	}()
	prepared = executablePreparedConfigRestoreSets(result.Plan, prepared)
	emittedResolutions := make(map[int]bool, len(result.Plan.Sets))
	for index, set := range result.Plan.Sets {
		if !selectedConfigRestorePlanSet(set) {
			emitGenerationConfigResolution(options.Emitter, set)
			emittedResolutions[index] = true
		}
	}
	for _, set := range legacyPreview.Sets {
		if set.Resolution.Status != planner.StatusPlanned {
			emitGenerationConfigResolution(options.Emitter, set)
		}
	}

	failStop := false
	for _, item := range prepared {
		set := &result.Plan.Sets[item.setIndex]
		if failStop {
			markConfigRestoreRecoveryRequired(set)
			emitGenerationConfigResolution(options.Emitter, *set)
			emittedResolutions[item.setIndex] = true
			continue
		}
		readyCalled := false
		outcome := executeLiveConfigRestoreSetFn(ctx, configRestoreLiveSetRequest{
			Materialized: item.materialized, TransactionRoot: item.transactionRoot,
			Lineage: configRestoreJournalLineage(options.RunID, *set), Registry: options.Registry,
			Observer: newConfigRestoreTransactionObserver(options.Emitter, set.Source.CaptureID, set.Source.ConfigSetID),
			Ready: func(prepared *configrestore.PreparedSet) {
				emitGenerationConfigResolution(options.Emitter, *set)
				emittedResolutions[item.setIndex] = true
				emitGenerationConfigRestoreStarts(options.Emitter, *set, item.materialized, prepared)
				readyCalled = true
			},
		})
		applyConfigRestoreSetOutcome(set, outcome)
		if !readyCalled {
			emitGenerationConfigResolution(options.Emitter, *set)
			emittedResolutions[item.setIndex] = true
			emitGenerationConfigRestoreStarts(options.Emitter, *set, item.materialized, outcome.Prepared)
		}
		items := configRestoreResultsForSet(*set, item.materialized, outcome)
		result.RestoreItems = append(result.RestoreItems, items...)
		result.EventResults = append(result.EventResults, items...)
		emitConfigRestoreItems(options.Emitter, items, set.Source.ModuleID)
		if outcome.Reason != nil && *outcome.Reason == planner.ReasonJournalIntentFailed {
			for index := range result.Plan.Sets {
				if emittedResolutions[index] {
					continue
				}
				set := &result.Plan.Sets[index]
				if selectedConfigRestorePlanSet(*set) {
					markConfigRestoreRecoveryRequired(set)
				}
				emitGenerationConfigResolution(options.Emitter, *set)
				emittedResolutions[index] = true
			}
			recomputeConfigPlanSummary(&result.Plan)
			legacyPreview = overlayLegacyRecoveryRequired(legacyPreview, session.runtime.inputs)
			emitPlannedLegacyConfigResolutions(options.Emitter, legacyPreview)
			result.Plan = mergeConfigRestorePlans(result.Plan, legacyPreview)
			return result, envelope.NewError(envelope.ErrRestoreFailed, "Failed to persist configuration restore intent.").
				WithDetail(map[string]string{"reason": planner.ReasonJournalIntentFailed.String(), "diagnostic": outcome.Err.Error()}).
				WithRemediation("Review the journal storage failure, resolve its cause, and retry.")
		}
		if !outcome.CanContinue {
			failStop = true
		}
	}
	if failStop {
		for _, item := range prepared {
			set := &result.Plan.Sets[item.setIndex]
			if set.Resolution.Status == planner.StatusPlanned {
				markConfigRestoreRecoveryRequired(set)
			}
		}
	}
	for index, set := range result.Plan.Sets {
		if !emittedResolutions[index] {
			emitGenerationConfigResolution(options.Emitter, set)
		}
	}
	if failStop {
		legacyPreview = overlayLegacyRecoveryRequired(legacyPreview, session.runtime.inputs)
		emitPlannedLegacyConfigResolutions(options.Emitter, legacyPreview)
		result.Plan = mergeConfigRestorePlans(result.Plan, legacyPreview)
	} else {
		emitPlannedLegacyConfigResolutions(options.Emitter, legacyPreview)
		legacy, ordinary, eventResults, runErr := executeLegacyAndOrdinaryConfigRestores(session.runtime.inputs, options, collisions)
		if runErr != nil {
			return result, runErr
		}
		result.Plan = mergeConfigRestorePlans(result.Plan, legacy.Plan)
		result.RestoreItems = append(result.RestoreItems, legacy.RestoreItems...)
		result.RestoreItems = append(result.RestoreItems, ordinary...)
		result.EventResults = append(result.EventResults, eventResults...)
		legacyItems := append(append([]restore.RestoreResult{}, legacy.RestoreItems...), ordinary...)
		if len(legacyItems) > 0 {
			journalPath, journalErr := writeLegacyConfigRestoreJournal(options, legacyItems)
			if journalErr != nil {
				return result, envelope.NewError(envelope.ErrRestoreFailed, "Failed to write the configuration restore journal.").
					WithDetail(map[string]string{"reason": journalErr.Error()}).
					WithRemediation("Verify the Endstate journal directory is writable, then retry.")
			}
			result.JournalPath = journalPath
			if _, registerErr := guard.RegisterLegacyJournal(journalPath); registerErr != nil {
				return result, envelope.NewError(envelope.ErrRestoreFailed, "Failed to register the legacy restore journal.").
					WithDetail(map[string]string{"reason": "legacy_journal_registration_failed", "diagnostic": registerErr.Error()}).
					WithRemediation("Review the configuration restore store failure, then retry.")
			}
		}
	}
	result.Results = append(result.Results, result.RestoreItems...)
	recomputeConfigPlanSummary(&result.Plan)
	return result, nil
}

func emitGenerationConfigResolutions(emitter *events.Emitter, plan planner.ConfigPlan) {
	for _, set := range plan.Sets {
		emitGenerationConfigResolution(emitter, set)
	}
}

var configRestoreHostPathPattern = regexp.MustCompile(`(?i)(?:[a-z]:[\\/][^\s]+|\\\\[^\s]+|~[\\/][^\s]+|(?:^|[\s(])\/[^\s]+)`)

const configRestoreDetectionDetailLimit = 1024

func emitConfigRestoreDetectionFailures(
	emitter *events.Emitter,
	failures []configRestoreDetectionFailure,
) {
	if emitter == nil {
		return
	}
	for _, failure := range normalizedConfigRestoreDetectionFailures(failures) {
		driverName := failure.Driver
		if driverName == "" {
			driverName = "unknown"
		}
		ref := failure.Ref
		if ref == "" {
			ref = "unknown"
		}
		detail := sanitizeConfigRestoreDetectionDetail(failure.Detail)
		if detail == "" {
			detail = "package driver is unavailable"
		}
		emitter.EmitError(
			"item",
			fmt.Sprintf(
				"configuration target detection failed (driver=%s, ref=%s): %s",
				driverName, ref, detail,
			),
			failure.ModuleID,
		)
	}
}

func sanitizeConfigRestoreDetectionDetail(detail string) string {
	detail = strings.Join(strings.Fields(strings.ToValidUTF8(detail, "�")), " ")
	detail = configRestoreHostPathPattern.ReplaceAllStringFunc(detail, func(match string) string {
		prefix := ""
		if strings.HasPrefix(match, " ") || strings.HasPrefix(match, "(") {
			prefix, match = match[:1], match[1:]
		}
		return prefix + "[local path]"
	})
	if len(detail) <= configRestoreDetectionDetailLimit {
		return detail
	}
	suffix := "…"
	cut := configRestoreDetectionDetailLimit - len(suffix)
	for cut > 0 && !utf8.ValidString(detail[:cut]) {
		cut--
	}
	return strings.TrimSpace(detail[:cut]) + suffix
}

func emitGenerationConfigResolution(emitter *events.Emitter, set planner.PlanSet) {
	if emitter != nil {
		emitter.EmitConfigResolution(set)
	}
}

func emitPlannedLegacyConfigResolutions(emitter *events.Emitter, plan planner.ConfigPlan) {
	for _, set := range plan.Sets {
		if set.Resolution.Status == planner.StatusPlanned {
			emitGenerationConfigResolution(emitter, set)
		}
	}
}

func shouldFrameConfigRestoreEvents(inputs configRestoreInputs, restoreEnabled bool) bool {
	return inputs.hasConfigPayloads || len(inputs.generationSources) > 0 || len(inputs.legacyLanes) > 0 ||
		(restoreEnabled && len(inputs.ordinaryRestores) > 0)
}

func markSelectedConfigRestoreSetsPlanned(plan *planner.ConfigPlan) {
	if plan == nil {
		return
	}
	for index := range plan.Sets {
		set := &plan.Sets[index]
		if set.Resolution.Status == "" && set.Resolution.Reason == nil &&
			(set.Resolution.Resolution == planner.ResolutionDirect || set.Resolution.Resolution == planner.ResolutionMigrate) {
			set.Resolution.Status = planner.StatusPlanned
			set.Resolution = planner.ProjectConfigResolution(*set)
		}
	}
	recomputeConfigPlanSummary(plan)
}

func closePreparedConfigRestoreExecutions(prepared []preparedConfigRestoreExecution) {
	for _, item := range prepared {
		_ = item.stage.Close()
	}
}

func selectedGenerationConfigRestoreExists(plan planner.ConfigPlan) bool {
	for _, set := range plan.Sets {
		if selectedConfigRestorePlanSet(set) {
			return true
		}
	}
	return false
}

func (session *configRestoreExecutionSession) stageAndMaterialize(
	ctx context.Context,
	plan *planner.ConfigPlan,
	options configRestoreExecutionOptions,
) []preparedConfigRestoreExecution {
	prepared := make([]preparedConfigRestoreExecution, 0, len(plan.Sets))
	for index := range plan.Sets {
		set := &plan.Sets[index]
		if !selectedConfigRestorePlanSet(*set) {
			continue
		}
		source, ok := session.sourceForCapture(set.Source.CaptureID)
		if !ok {
			markConfigRestoreFailure(set, planner.ReasonStagingValidationFailed, planner.StatusFailed)
			continue
		}
		stage, err := stageConfigRestoreSetFn(ctx, migration.StageRequest{
			CaptureID: set.Source.CaptureID, PayloadRoot: source.payloadRoot,
			PayloadManifest: source.payloadManifest, SourceGeneration: set.Source.Generation,
			TargetGeneration: set.TargetGenerationDef, MigrationEdges: set.MigrationEdges,
			Observer: events.NewMigrationStageObserver(options.Emitter, set.Source.ConfigSetID),
		})
		if err != nil {
			reason := planner.ReasonStagingValidationFailed
			if migration.CodeOf(err) == migration.CodePayloadIntegrityFailed {
				reason = planner.ReasonPayloadIntegrityFailed
			}
			markConfigRestoreFailure(set, reason, planner.StatusFailed)
			continue
		}
		materialized, err := materializeConfigRestoreSetFn(ctx, configrestore.Request{
			Stage: stage, Plan: *set,
			ProcessPatterns: session.processPatterns(set.Source.ModuleID), ProcessObserver: options.ProcessObserver,
		})
		if err != nil {
			_ = stage.Close()
			reason := planner.ReasonStagingValidationFailed
			status := planner.StatusFailed
			switch configrestore.CodeOf(err) {
			case configrestore.CodeAppRunning:
				reason, status = planner.ReasonAppRunning, planner.StatusSkipped
			case configrestore.CodeTargetOverlap:
				reason = planner.ReasonTargetCollision
			}
			markConfigRestoreFailure(set, reason, status)
			continue
		}
		prepared = append(prepared, preparedConfigRestoreExecution{setIndex: index, stage: stage, materialized: materialized})
	}
	recomputeConfigPlanSummary(plan)
	return prepared
}

func (session *configRestoreExecutionSession) sourceForCapture(captureID string) (configRestoreSource, bool) {
	for _, source := range session.runtime.inputs.generationSources {
		if source.source.CaptureID == captureID {
			return source, true
		}
	}
	return configRestoreSource{}, false
}

func (session *configRestoreExecutionSession) processPatterns(moduleID string) []string {
	if session.runtime.catalog.resolver == nil {
		return []string{}
	}
	return session.runtime.catalog.resolver.ProcessPatterns(moduleID)
}

func selectedConfigRestorePlanSet(set planner.PlanSet) bool {
	return set.Resolution.Status == planner.StatusPlanned &&
		(set.Resolution.Resolution == planner.ResolutionDirect || set.Resolution.Resolution == planner.ResolutionMigrate) &&
		set.TargetGenerationDef != nil
}

func executablePreparedConfigRestoreSets(
	plan planner.ConfigPlan,
	prepared []preparedConfigRestoreExecution,
) []preparedConfigRestoreExecution {
	result := make([]preparedConfigRestoreExecution, 0, len(prepared))
	for _, item := range prepared {
		if selectedConfigRestorePlanSet(plan.Sets[item.setIndex]) {
			result = append(result, item)
		}
	}
	return result
}

func inspectDryRunConfigRestoreSet(
	ctx context.Context,
	set *configrestore.MaterializedSet,
	registry configrestore.RegistryMutator,
) configRestoreSetOutcome {
	root, err := os.MkdirTemp("", ".endstate-config-dry-run-")
	if err != nil {
		return failedConfigRestoreSet(planner.ReasonBackupFailed, err, true, nil)
	}
	defer os.RemoveAll(root)
	prepared, err := prepareConfigRestoreSnapshotsFn(ctx, configrestore.SnapshotRequest{
		Set: set, TransactionRoot: root, RegistryReader: registry,
	})
	if err != nil {
		return failedConfigRestoreSet(planner.ReasonBackupFailed, err, true, nil)
	}
	if preparedConfigRestoreAlreadyCurrent(prepared) {
		reason := planner.ReasonAlreadyUpToDate
		return configRestoreSetOutcome{Status: planner.StatusSkipped, Reason: &reason, CanContinue: true, Prepared: prepared}
	}
	return configRestoreSetOutcome{Status: planner.StatusPlanned, CanContinue: true, Prepared: prepared}
}

func applyConfigRestoreSetOutcome(set *planner.PlanSet, outcome configRestoreSetOutcome) {
	if set == nil {
		return
	}
	set.Resolution.Status = outcome.Status
	set.Resolution.Reason = outcome.Reason
	set.Resolution = planner.ProjectConfigResolution(*set)
}

func markConfigRestoreFailure(set *planner.PlanSet, reason planner.ResolutionReason, status planner.TerminalStatus) {
	applyConfigRestoreSetOutcome(set, configRestoreSetOutcome{Status: status, Reason: &reason, CanContinue: true})
}

func markConfigRestoreRecoveryRequired(set *planner.PlanSet) {
	markConfigRestoreFailure(set, planner.ReasonRecoveryRequired, planner.StatusSkipped)
}

func configRestoreJournalLineage(runID string, set planner.PlanSet) configrestore.JournalLineage {
	return configrestore.JournalLineage{
		RunID: runID, CaptureID: set.Source.CaptureID, ModuleID: set.Source.ModuleID,
		ConfigSetID: set.Source.ConfigSetID, TargetInstanceID: set.Resolution.TargetInstanceID,
		SourceGeneration: set.Source.Generation, TargetGeneration: set.Resolution.TargetGeneration,
		MigrationPath:               append([]string{}, set.Resolution.MigrationPath...),
		SourceGenerationFingerprint: set.Source.GenerationFingerprint,
		CaptureModuleRevision:       set.Source.ModuleRevision, RestoreModuleRevision: set.Resolution.RestoreModuleRevision,
	}
}

func configRestoreResultsForSet(
	set planner.PlanSet,
	materialized *configrestore.MaterializedSet,
	outcome configRestoreSetOutcome,
) []restore.RestoreResult {
	if materialized == nil {
		return []restore.RestoreResult{}
	}
	prepared := []configrestore.PreparedAction{}
	if outcome.Prepared != nil {
		prepared = outcome.Prepared.Actions()
	}
	results := make([]restore.RestoreResult, 0, len(materialized.Actions))
	for index, action := range materialized.Actions {
		status := "restored"
		switch outcome.Status {
		case planner.StatusSkipped:
			status = "skipped_up_to_date"
		case planner.StatusFailed, planner.StatusRolledBack, planner.StatusRollbackFailed:
			status = "failed"
		}
		item := restore.RestoreResult{
			ID:     fmt.Sprintf("config:%s:%06d", set.Source.CaptureID, index),
			Source: action.Source, Target: action.Target, Status: status, RestoreType: action.Strategy,
			CaptureID: set.Source.CaptureID, ConfigSetID: set.Source.ConfigSetID,
			TargetInstanceID: set.Resolution.TargetInstanceID,
			SourceGeneration: set.Source.Generation, TargetGeneration: set.Resolution.TargetGeneration,
		}
		if outcome.Err != nil {
			item.Error = outcome.Err.Error()
		}
		if index < len(prepared) {
			prior := prepared[index].Prior()
			item.TargetExistedBefore = prior.Kind != configrestore.StateAbsent
			item.BackupPath = prior.BackupPath
			item.BackupCreated = prior.BackupPath != ""
		}
		results = append(results, item)
	}
	return results
}

type configRestoreCollisionState struct {
	legacyCaptures  map[string]planner.ResolutionReason
	ordinaryActions map[int]planner.ResolutionReason
}

type ownedConfigRestoreClaim struct {
	claim       string
	kind        string
	setIndex    int
	captureID   string
	actionIndex int
}

func (claim ownedConfigRestoreClaim) ownerKey() string {
	switch claim.kind {
	case "generation":
		return fmt.Sprintf("generation:%d", claim.setIndex)
	case "legacy":
		return "legacy:" + claim.captureID
	default:
		return fmt.Sprintf("ordinary:%d", claim.actionIndex)
	}
}

func applyUnifiedConcreteConfigRestoreCollisions(
	plan *planner.ConfigPlan,
	prepared []preparedConfigRestoreExecution,
	inputs configRestoreInputs,
	options configRestoreExecutionOptions,
) configRestoreCollisionState {
	state := configRestoreCollisionState{
		legacyCaptures: make(map[string]planner.ResolutionReason), ordinaryActions: make(map[int]planner.ResolutionReason),
	}
	claims := []ownedConfigRestoreClaim{}
	for _, item := range prepared {
		for _, action := range item.materialized.Actions {
			claim := concreteConfigRestoreClaim(action)
			if claim == "" {
				continue
			}
			claims = append(claims, ownedConfigRestoreClaim{claim: claim, kind: "generation", setIndex: item.setIndex})
		}
	}
	restoreOptions := configRestoreActionOptions(options)
	for _, lane := range inputs.legacyLanes {
		if !lane.selected {
			continue
		}
		laneClaims := []ownedConfigRestoreClaim{}
		valid := true
		for _, action := range convertToActions(lane.restoreEntries, "") {
			claim, err := concreteLegacyRestoreClaim(action, restoreOptions)
			if err != nil {
				state.legacyCaptures[lane.captureID] = planner.ReasonStagingValidationFailed
				valid = false
				break
			}
			if claim != "" {
				laneClaims = append(laneClaims, ownedConfigRestoreClaim{claim: claim, kind: "legacy", captureID: lane.captureID})
			}
		}
		if valid {
			claims = append(claims, laneClaims...)
		}
	}
	for index, action := range convertToActions(inputs.ordinaryRestores, "") {
		claim, err := concreteLegacyRestoreClaim(action, restoreOptions)
		if err != nil {
			state.ordinaryActions[index] = planner.ReasonStagingValidationFailed
			continue
		}
		if claim != "" {
			claims = append(claims, ownedConfigRestoreClaim{claim: claim, kind: "ordinary", actionIndex: index})
		}
	}
	blockedGeneration := make(map[int]bool)
	for left := 0; left < len(claims); left++ {
		for right := left + 1; right < len(claims); right++ {
			if !concreteConfigRestoreClaimsOverlap(claims[left].claim, claims[right].claim) {
				continue
			}
			// Exact repeated targets within one lane are ordered and journaled as a
			// reversible state chain. Parent/child overlaps are not: changing a
			// child also changes the parent's captured tree state, so reject those
			// before any forward mutation.
			if claims[left].ownerKey() == claims[right].ownerKey() && claims[left].claim == claims[right].claim {
				continue
			}
			for _, owner := range []ownedConfigRestoreClaim{claims[left], claims[right]} {
				switch owner.kind {
				case "generation":
					blockedGeneration[owner.setIndex] = true
				case "legacy":
					state.legacyCaptures[owner.captureID] = planner.ReasonTargetCollision
				case "ordinary":
					state.ordinaryActions[owner.actionIndex] = planner.ReasonTargetCollision
				}
			}
		}
	}
	for index := range blockedGeneration {
		markConfigRestoreFailure(&plan.Sets[index], planner.ReasonTargetCollision, planner.StatusFailed)
	}
	recomputeConfigPlanSummary(plan)
	return state
}

func applyLegacyConcreteConfigRestoreCollisions(plan *planner.ConfigPlan, state configRestoreCollisionState) {
	for index := range plan.Sets {
		if reason, blocked := state.legacyCaptures[plan.Sets[index].Source.CaptureID]; blocked {
			markConfigRestoreFailure(&plan.Sets[index], reason, planner.StatusFailed)
		}
	}
	recomputeConfigPlanSummary(plan)
}

func concreteConfigRestoreClaim(action configrestore.Action) string {
	if action.Kind == configrestore.ActionRegistrySet && action.RegistryValue != nil {
		return "registry-value\x00" + normalizeConfigRestoreRegistryKey(action.RegistryValue.Key) + "\x00" + strings.ToLower(action.RegistryValue.ValueName)
	}
	if action.Target == "" {
		return ""
	}
	concrete, err := restore.ConcreteFilesystemTarget(action.Target)
	if err != nil {
		return ""
	}
	claim := filepath.ToSlash(concrete)
	return "file\x00" + strings.TrimSuffix(claim, "/")
}

func concreteConfigRestoreClaimsOverlap(left, right string) bool {
	if strings.HasPrefix(left, "registry-") || strings.HasPrefix(right, "registry-") {
		return registryConfigRestoreClaimsOverlap(left, right)
	}
	left = strings.TrimPrefix(left, "file\x00")
	right = strings.TrimPrefix(right, "file\x00")
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func concreteLegacyRestoreClaim(action restore.RestoreAction, options restore.RestoreOptions) (string, error) {
	restoreType := action.Type
	if restoreType == "" {
		restoreType = "copy"
	}
	switch restoreType {
	case "registry-set":
		if err := restore.ValidateRegistryTarget(action.Key); err != nil {
			return "", err
		}
		return "registry-value\x00" + normalizeConfigRestoreRegistryKey(action.Key) + "\x00" + strings.ToLower(action.ValueName), nil
	case "registry-import":
		descriptor := restore.DescribeAction(action, options)
		if err := restore.ValidateRegistryTarget(action.Target); err != nil {
			return "", err
		}
		if err := restore.ValidateRegistryImportScope(descriptor.Source, action.Target); err != nil {
			if !(action.Optional && os.IsNotExist(err)) {
				return "", err
			}
		}
		return "registry-key\x00" + normalizeConfigRestoreRegistryKey(action.Target), nil
	default:
		descriptor := restore.DescribeAction(action, options)
		if descriptor.Target == "" {
			return "", nil
		}
		if restoreType == "delete-glob" {
			if err := restore.ValidateDeleteGlobPattern(action.Pattern); err != nil {
				return "", err
			}
		}
		concrete, err := restore.ConcreteFilesystemTarget(descriptor.Target)
		if err != nil {
			return "", err
		}
		claim := filepath.ToSlash(concrete)
		return "file\x00" + strings.TrimSuffix(claim, "/"), nil
	}
}

func normalizeConfigRestoreRegistryKey(value string) string {
	value = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(value), "/", "\\"))
	value = strings.TrimPrefix(value, "hkey_current_user\\")
	value = strings.TrimPrefix(value, "hkcu\\")
	return strings.Trim(value, "\\")
}

func registryConfigRestoreClaimsOverlap(left, right string) bool {
	type registryClaim struct{ kind, key, value string }
	parse := func(value string) registryClaim {
		parts := strings.Split(value, "\x00")
		claim := registryClaim{kind: parts[0]}
		if len(parts) > 1 {
			claim.key = parts[1]
		}
		if len(parts) > 2 {
			claim.value = parts[2]
		}
		return claim
	}
	l, r := parse(left), parse(right)
	if l.kind == "registry-value" && r.kind == "registry-value" {
		return l.key == r.key && l.value == r.value
	}
	if l.kind == "registry-key" && r.kind == "registry-key" {
		return l.key == r.key || strings.HasPrefix(l.key, r.key+"\\") || strings.HasPrefix(r.key, l.key+"\\")
	}
	if l.kind == "registry-value" {
		l, r = r, l
	}
	return l.kind == "registry-key" && r.kind == "registry-value" &&
		(r.key == l.key || strings.HasPrefix(r.key, l.key+"\\"))
}

func configRestoreInternalError(reason string) *envelope.Error {
	return envelope.NewError(envelope.ErrInternalError, "Configuration restore failed.").
		WithDetail(map[string]string{"reason": reason}).
		WithRemediation("Review the configuration restore error and retry.")
}

func previewLegacyConfigRestorePlan(inputs configRestoreInputs, restoreEnabled bool) planner.ConfigPlan {
	plan := planner.ConfigPlan{Sets: []planner.PlanSet{}}
	lanes := append([]configRestoreLegacyLane(nil), inputs.legacyLanes...)
	sort.Slice(lanes, func(left, right int) bool { return lanes[left].captureID < lanes[right].captureID })
	for _, lane := range lanes {
		status := planner.StatusPlanned
		var reason *planner.ResolutionReason
		switch {
		case !lane.selected:
			status = planner.StatusSkipped
			reason = legacyConfigReason(planner.ReasonRestoreFiltered)
		case !restoreEnabled:
			status = planner.StatusSkipped
			reason = legacyConfigReason(planner.ReasonRestoreNotEnabled)
		}
		set := planner.PlanSet{
			Source:          planner.SourceCapture{CaptureID: lane.captureID, ModuleID: lane.moduleID, ConfigSetID: "legacy"},
			TargetInstances: []planner.TargetInstance{},
			Resolution: planner.ConfigResolution{
				Resolution: planner.ResolutionLegacyUnverified, Reason: reason,
				MigrationPath: []string{}, ResolvedTargets: []string{}, Status: status,
			},
		}
		set.Resolution = planner.ProjectConfigResolution(set)
		plan.Sets = append(plan.Sets, set)
	}
	recomputeConfigPlanSummary(&plan)
	return plan
}

func executeLegacyAndOrdinaryConfigRestores(
	inputs configRestoreInputs,
	options configRestoreExecutionOptions,
	collisions configRestoreCollisionState,
) (configRestoreLegacyProjection, []restore.RestoreResult, []restore.RestoreResult, *envelope.Error) {
	execution := configRestoreLegacyExecution{
		DryRun: options.DryRun, ResultsByCaptureID: make(map[string][]restore.RestoreResult),
		BlockedReasons: make(map[string]planner.ResolutionReason),
	}
	restoreOptions := configRestoreActionOptions(options)
	eventResults := []restore.RestoreResult{}
	for _, lane := range inputs.legacyLanes {
		if !lane.selected {
			continue
		}
		if reason, blocked := collisions.legacyCaptures[lane.captureID]; blocked {
			execution.BlockedReasons[lane.captureID] = reason
			continue
		}
		lineage := configRestoreEventLineage{
			ModuleID: lane.moduleID, CaptureID: lane.captureID, ConfigSetID: lane.configSetID,
		}
		results := []restore.RestoreResult{}
		for _, action := range convertToActions(lane.restoreEntries, "") {
			descriptor := restore.DescribeAction(action, restoreOptions)
			emitRestoreActionStarts(options.Emitter, []restore.RestoreAction{action}, restoreOptions, lineage)
			actionResults, err := restore.RunRestore([]restore.RestoreAction{action}, restoreOptions, nil)
			if err != nil {
				return configRestoreLegacyProjection{}, nil, nil, envelope.NewError(envelope.ErrRestoreFailed, err.Error())
			}
			linked := linkLegacyRestoreItems(lane, actionResults)
			results = append(results, linked...)
			eventResult := configRestoreEventResultForAction(descriptor, linked)
			eventResults = append(eventResults, eventResult)
			emitConfigRestoreItems(options.Emitter, []restore.RestoreResult{eventResult}, lane.moduleID)
		}
		execution.ResultsByCaptureID[lane.captureID] = results
	}
	legacy, err := projectLegacyConfigRestores(inputs, true, execution)
	if err != nil {
		return configRestoreLegacyProjection{}, nil, nil, configRestoreInternalError(err.Error())
	}
	ordinary := []restore.RestoreResult{}
	if len(inputs.ordinaryRestores) > 0 {
		for actionIndex, action := range convertToActions(inputs.ordinaryRestores, "") {
			descriptor := restore.DescribeAction(action, restoreOptions)
			emitRestoreActionStarts(options.Emitter, []restore.RestoreAction{action}, restoreOptions, configRestoreEventLineage{})
			if reason, blocked := collisions.ordinaryActions[actionIndex]; blocked {
				result := restore.RestoreResult{
					ID: descriptor.ID, Source: descriptor.Source, Target: descriptor.Target,
					Status: "failed", Error: reason.String(), RestoreType: descriptor.RestoreType,
					TargetExistedBefore: descriptor.TargetExisted,
				}
				ordinary = append(ordinary, result)
				eventResults = append(eventResults, result)
				emitConfigRestoreItems(options.Emitter, []restore.RestoreResult{result}, "")
				continue
			}
			results, runErr := restore.RunRestore([]restore.RestoreAction{action}, restoreOptions, nil)
			if runErr != nil {
				return configRestoreLegacyProjection{}, nil, nil, envelope.NewError(envelope.ErrRestoreFailed, runErr.Error())
			}
			ordinary = append(ordinary, results...)
			eventResult := configRestoreEventResultForAction(descriptor, results)
			eventResults = append(eventResults, eventResult)
			emitConfigRestoreItems(options.Emitter, []restore.RestoreResult{eventResult}, "")
		}
	}
	return legacy, ordinary, eventResults, nil
}

func configRestoreActionOptions(options configRestoreExecutionOptions) restore.RestoreOptions {
	return restore.RestoreOptions{
		DryRun: options.DryRun, BackupDir: options.BackupDir, ManifestDir: options.ManifestDir,
		ExportRoot: options.ExportRoot, RunID: options.RunID,
	}
}

type configRestoreEventLineage struct {
	ModuleID         string
	CaptureID        string
	ConfigSetID      string
	TargetInstanceID string
	SourceGeneration string
	TargetGeneration string
}

func emitRestoreActionStarts(
	emitter *events.Emitter,
	actions []restore.RestoreAction,
	options restore.RestoreOptions,
	lineage configRestoreEventLineage,
) {
	if emitter == nil {
		return
	}
	for _, action := range actions {
		descriptor := restore.DescribeAction(action, options)
		moduleID := lineage.ModuleID
		if moduleID == "" {
			moduleID = action.FromModule
		}
		emitter.EmitRestoreItem(events.RestoreItemProgress{
			ID: descriptor.ID, Module: moduleID, Restorer: descriptor.RestoreType,
			Source: descriptor.Source, Target: descriptor.Target, Status: events.RestoreItemRestoring,
			TargetExisted: descriptor.TargetExisted, Message: "restoring settings",
			CaptureID: lineage.CaptureID, ConfigSetID: lineage.ConfigSetID,
			TargetInstanceID: lineage.TargetInstanceID, SourceGeneration: lineage.SourceGeneration,
			TargetGeneration: lineage.TargetGeneration,
		})
	}
}

func emitGenerationConfigRestoreStarts(
	emitter *events.Emitter,
	set planner.PlanSet,
	materialized *configrestore.MaterializedSet,
	prepared *configrestore.PreparedSet,
) {
	if emitter == nil || materialized == nil {
		return
	}
	preparedActions := prepared.Actions()
	for index, action := range materialized.Actions {
		targetExisted := false
		if index < len(preparedActions) {
			targetExisted = preparedActions[index].Prior().Kind != configrestore.StateAbsent
		} else if action.Target != "" {
			_, err := os.Stat(action.Target)
			targetExisted = err == nil
		}
		emitter.EmitRestoreItem(events.RestoreItemProgress{
			ID:     fmt.Sprintf("config:%s:%06d", set.Source.CaptureID, index),
			Module: set.Source.ModuleID, Restorer: action.Strategy,
			Source: action.Source, Target: action.Target, Status: events.RestoreItemRestoring,
			TargetExisted: targetExisted, Message: "restoring settings",
			CaptureID: set.Source.CaptureID, ConfigSetID: set.Source.ConfigSetID,
			TargetInstanceID: set.Resolution.TargetInstanceID,
			SourceGeneration: set.Source.Generation, TargetGeneration: set.Resolution.TargetGeneration,
		})
	}
}

func emitConfigRestoreItems(emitter *events.Emitter, results []restore.RestoreResult, moduleID string) {
	if emitter == nil {
		return
	}
	for _, result := range results {
		restorer := result.RestoreType
		if restorer == "" {
			restorer = "copy"
		}
		status := events.RestoreItemRestored
		var reason *string
		message := "settings restored"
		switch result.Status {
		case "skipped_up_to_date":
			status = events.RestoreItemSkippedUpToDate
			value := planner.ReasonAlreadyUpToDate.String()
			reason, message = &value, "target settings are already current"
		case "skipped_missing_source":
			status = events.RestoreItemSkippedMissingSource
			value := "source_missing"
			reason, message = &value, "optional settings source is missing"
		case "failed":
			status = events.RestoreItemFailed
			value := "restore_failed"
			reason, message = &value, result.Error
		}
		var backupPath *string
		if result.BackupPath != "" {
			value := result.BackupPath
			backupPath = &value
		}
		emitter.EmitRestoreItem(events.RestoreItemProgress{
			ID: result.ID, Module: moduleID, Restorer: restorer,
			Source: result.Source, Target: result.Target, Status: status, Reason: reason,
			BackupPath: backupPath, TargetExisted: result.TargetExistedBefore, Message: message,
			CaptureID: result.CaptureID, ConfigSetID: result.ConfigSetID,
			TargetInstanceID: result.TargetInstanceID, SourceGeneration: result.SourceGeneration,
			TargetGeneration: result.TargetGeneration,
		})
	}
}

func configRestoreEventResultForAction(
	descriptor restore.ActionDescriptor,
	results []restore.RestoreResult,
) restore.RestoreResult {
	eventResult := restore.RestoreResult{
		ID: descriptor.ID, Source: descriptor.Source, Target: descriptor.Target,
		RestoreType: descriptor.RestoreType, TargetExistedBefore: descriptor.TargetExisted,
		Status: "failed", Error: "restore action produced no result",
	}
	for index, result := range results {
		if index == 0 {
			eventResult.CaptureID = result.CaptureID
			eventResult.ConfigSetID = result.ConfigSetID
			eventResult.TargetInstanceID = result.TargetInstanceID
			eventResult.SourceGeneration = result.SourceGeneration
			eventResult.TargetGeneration = result.TargetGeneration
			eventResult.Status = result.Status
			eventResult.Error = result.Error
			eventResult.BackupPath = result.BackupPath
			eventResult.BackupCreated = result.BackupCreated
		}
		eventResult.TargetExistedBefore = eventResult.TargetExistedBefore || result.TargetExistedBefore
		eventResult.BackupCreated = eventResult.BackupCreated || result.BackupCreated
		if restoreEventStatusRank(result.Status) > restoreEventStatusRank(eventResult.Status) {
			eventResult.Status = result.Status
			eventResult.Error = result.Error
		}
		if eventResult.BackupPath != result.BackupPath {
			eventResult.BackupPath = ""
		}
	}
	return eventResult
}

func restoreEventStatusRank(status string) int {
	switch status {
	case "failed":
		return 4
	case "restored":
		return 3
	case "skipped_missing_source":
		return 2
	case "skipped_up_to_date":
		return 1
	default:
		return 0
	}
}

func selectedLegacyConfigRestoreExists(inputs configRestoreInputs) bool {
	for _, lane := range inputs.legacyLanes {
		if lane.selected {
			return true
		}
	}
	return false
}

func overlayLegacyRecoveryRequired(plan planner.ConfigPlan, inputs configRestoreInputs) planner.ConfigPlan {
	selected := make(map[string]bool, len(inputs.legacyLanes))
	for _, lane := range inputs.legacyLanes {
		selected[lane.captureID] = lane.selected
	}
	for index := range plan.Sets {
		if selected[plan.Sets[index].Source.CaptureID] {
			if plan.Sets[index].Resolution.Status == planner.StatusPlanned {
				markConfigRestoreRecoveryRequired(&plan.Sets[index])
			}
		}
	}
	recomputeConfigPlanSummary(&plan)
	return plan
}

func mergeConfigRestorePlans(left, right planner.ConfigPlan) planner.ConfigPlan {
	merged := planner.ConfigPlan{Sets: append(planner.CloneConfigPlan(left).Sets, planner.CloneConfigPlan(right).Sets...)}
	sort.Slice(merged.Sets, func(left, right int) bool {
		return merged.Sets[left].Source.CaptureID < merged.Sets[right].Source.CaptureID
	})
	recomputeConfigPlanSummary(&merged)
	return merged
}

func writeLegacyConfigRestoreJournal(
	options configRestoreExecutionOptions,
	results []restore.RestoreResult,
) (string, error) {
	if options.DryRun || len(results) == 0 {
		return "", nil
	}
	logsDir := options.JournalLogsDir
	if logsDir == "" {
		logsDir = "logs"
	}
	logsDir, err := filepath.Abs(logsDir)
	if err != nil {
		return "", err
	}
	logsDir = filepath.Clean(logsDir)
	absManifest := options.ManifestPath
	if absManifest != "" {
		absManifest, err = filepath.Abs(absManifest)
		if err != nil {
			return "", err
		}
	}
	journalPath, err := publishLegacyConfigRestoreJournal(
		logsDir, options.RunID, absManifest, options.ManifestDir, options.ExportRoot, results,
	)
	if err != nil {
		return "", err
	}
	info, statErr := os.Lstat(journalPath)
	if statErr != nil {
		return "", statErr
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("restore journal %q is not a regular file", journalPath)
	}
	return journalPath, nil
}

func publishLegacyConfigRestoreJournal(
	logsDir string,
	runID string,
	manifestPath string,
	manifestDir string,
	exportRoot string,
	results []restore.RestoreResult,
) (string, error) {
	if _, err := ensureDurableConfigRestoreDirectory(logsDir, 0o755); err != nil {
		return "", err
	}
	stagingDir, err := os.MkdirTemp(logsDir, ".legacy-journal-staging-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(stagingDir)
	if err := restore.WriteJournal(stagingDir, runID, manifestPath, manifestDir, exportRoot, results); err != nil {
		return "", err
	}
	staged := filepath.Join(stagingDir, fmt.Sprintf("restore-journal-%s.json", runID))
	stagedFile, err := os.OpenFile(staged, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	if err := stagedFile.Sync(); err != nil {
		_ = stagedFile.Close()
		return "", err
	}
	if err := stagedFile.Close(); err != nil {
		return "", err
	}
	stagedBytes, err := os.ReadFile(staged)
	if err != nil {
		return "", err
	}
	sequence, err := nextLegacyConfigRestoreJournalSequence(logsDir, runID)
	if err != nil {
		return "", err
	}
	for attempt := 0; attempt < 16; attempt++ {
		name := fmt.Sprintf("restore-journal-%s.json", runID)
		if attempt > 0 {
			var suffix [8]byte
			if _, err := rand.Read(suffix[:]); err != nil {
				return "", err
			}
			name = fmt.Sprintf(
				"restore-journal-%s~%020d-%s.json",
				runID, sequence+int64(attempt-1), hex.EncodeToString(suffix[:]),
			)
		}
		destination := filepath.Join(logsDir, name)
		if err := os.Link(staged, destination); err == nil {
			syncErr := syncConfigRestoreDirectory(logsDir)
			if syncErr != nil {
				return "", errors.Join(
					configrestore.ErrPublicationAmbiguous,
					fmt.Errorf("restore journal %q may be published but directory durability failed: %w", destination, syncErr),
				)
			}
			publishedBytes, readErr := os.ReadFile(destination)
			if readErr != nil || !bytes.Equal(stagedBytes, publishedBytes) {
				if readErr != nil {
					return "", errors.Join(
						configrestore.ErrPublicationAmbiguous,
						fmt.Errorf("restore journal %q was published but readback failed: %w", destination, readErr),
					)
				}
				return "", errors.Join(
					configrestore.ErrPublicationAmbiguous,
					fmt.Errorf("published restore journal %q bytes differ from staged bytes", destination),
				)
			}
			return destination, nil
		} else if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("could not allocate a collision-safe restore journal path")
}

// ensureDurableConfigRestoreDirectory creates a missing directory chain one
// component at a time and syncs each parent immediately after linking its new
// child. MkdirAll alone can leave an otherwise-written journal unreachable
// after a crash when more than the leaf directory was newly created.
func ensureDurableConfigRestoreDirectory(path string, permissions os.FileMode) (bool, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	absolute = filepath.Clean(absolute)
	missing := []string{}
	for current := absolute; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return false, fmt.Errorf("configuration journal path %q is not a safe directory", current)
			}
			break
		}
		if !os.IsNotExist(statErr) {
			return false, statErr
		}
		missing = append(missing, current)
		parent := filepath.Dir(current)
		if parent == current {
			return false, fmt.Errorf("configuration journal directory root %q does not exist", current)
		}
	}

	created := false
	for index := len(missing) - 1; index >= 0; index-- {
		directory := missing[index]
		if mkdirErr := os.Mkdir(directory, permissions); mkdirErr != nil {
			if !os.IsExist(mkdirErr) {
				return created, mkdirErr
			}
			info, statErr := os.Lstat(directory)
			if statErr != nil {
				return created, statErr
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return created, fmt.Errorf("configuration journal path %q is not a safe directory", directory)
			}
			continue
		}
		created = true
		if syncErr := syncConfigRestoreDirectory(filepath.Dir(directory)); syncErr != nil {
			return created, fmt.Errorf("sync parent of configuration journal directory %q: %w", directory, syncErr)
		}
	}
	return created, nil
}

func nextLegacyConfigRestoreJournalSequence(logsDir, runID string) (int64, error) {
	sequence := time.Now().UTC().UnixNano()
	prefix := fmt.Sprintf("restore-journal-%s~", runID)
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) {
			continue
		}
		tail := strings.TrimPrefix(name, prefix)
		separator := strings.IndexByte(tail, '-')
		if separator <= 0 {
			continue
		}
		value, parseErr := strconv.ParseInt(tail[:separator], 10, 64)
		if parseErr == nil && value >= sequence {
			sequence = value + 1
		}
	}
	return sequence, nil
}
