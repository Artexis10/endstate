// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/state"
)

func prepareApplyHomeManager(flags ApplyFlags, mf *manifest.Manifest, r realizer.Realizer, session *configRestoreExecutionSession) (*preparedHomeManager, *envelope.Error) {
	home, envErr := prepareHomeManager(flags, mf, r)
	if envErr != nil || !flags.EnableRestore || session == nil {
		return home, envErr
	}
	if !session.runtime.hasHomeManagerSources() {
		return home, nil
	}
	if _, ok := r.(realizer.HomePreparer); !ok {
		return home, nil
	}
	if _, ok := r.(realizer.HomeActivator); !ok {
		return home, nil
	}
	if _, ok := r.(realizer.HomeObserver); !ok {
		// A backend that cannot observe ownership has no Home Manager consumer.
		return home, nil
	}
	if home == nil {
		home = &preparedHomeManager{generated: true}
		home.capturedPreparationError = home.observeOwnership(r)
	}
	home.capturedFiles = map[string]*provision.HomeFileExpectations{}
	home.prepareCaptured = func(plan *planner.ConfigPlan, staged []preparedConfigRestoreExecution) []string {
		return prepareCapturedHomeManager(flags, mf, r, session, home, plan, staged)
	}
	return home, nil
}

func closeCapturedHomeStages(home *preparedHomeManager) {
	if home == nil {
		return
	}
	for _, stage := range home.capturedStages {
		_ = stage.Close()
	}
	home.capturedStages = nil
}

func homeFilesExpectations(files []configrestore.HomeFile) (*provision.HomeFileExpectations, error) {
	result := &provision.HomeFileExpectations{Files: []provision.HomeFileExpectation{}}
	for _, file := range files {
		sum, _, err := safepath.HashRegularFile(file.Source, math.MaxInt64)
		if err != nil {
			return nil, err
		}
		result.Files = append(result.Files, provision.HomeFileExpectation{Target: filepath.ToSlash(file.Target), SHA256: fmt.Sprintf("%x", sum)})
	}
	return result, nil
}

func homeTargetsOverlap(left, right string) bool {
	leftIDs, _ := restore.FilesystemTargetIdentities(left)
	rightIDs, _ := restore.FilesystemTargetIdentities(right)
	for _, l := range leftIDs {
		for _, r := range rightIDs {
			// File claims use the existing checker's closed filesystem protocol.
			if concreteConfigRestoreClaimsOverlap("file\x00"+filepath.ToSlash(l), "file\x00"+filepath.ToSlash(r)) {
				return true
			}
		}
	}
	return false
}

func prepareCapturedHomeManager(flags ApplyFlags, mf *manifest.Manifest, r realizer.Realizer, session *configRestoreExecutionSession, home *preparedHomeManager, plan *planner.ConfigPlan, staged []preparedConfigRestoreExecution) []string {
	indices := []int{}
	files := []nix.HomeFilePlacement{}
	expectations := map[string]*provision.HomeFileExpectations{}
	for _, item := range staged {
		if !item.home {
			continue
		}
		home.capturedStages = append(home.capturedStages, item.stage)
		set := &plan.Sets[item.setIndex]
		if home.capturedPreparationError != nil || session.runtime.catalog.homeAuthorityErr != nil {
			// An unreadable ownership state or registry is a failed observation, not a missing writer.
			markConfigRestoreFailure(set, planner.ReasonTargetDetectionFailed, planner.StatusFailed)
			continue
		}
		authority, supported := session.runtime.catalog.homeAuthorities[set.Source.ModuleID]
		if !supported || authority.ConfigSetID != planner.TargetConfigSetID(*set) || authority.GenerationID != set.Resolution.TargetGeneration {
			markConfigRestoreFailure(set, planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped)
			continue
		}
		if len(item.homeFiles) == 0 {
			markConfigRestoreFailure(set, planner.ReasonAlreadyUpToDate, planner.StatusSkipped)
			continue
		}
		valid := true
		for _, file := range item.homeFiles {
			if _, err := nix.HomeRelativeTarget(file.Target); err != nil {
				valid = false
			}
		}
		expected, err := homeFilesExpectations(item.homeFiles)
		if !valid || err != nil {
			markConfigRestoreFailure(set, planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped)
			continue
		}
		indices = append(indices, item.setIndex)
		expectations[set.Source.CaptureID] = expected
		for _, file := range item.homeFiles {
			files = append(files, nix.HomeFilePlacement{Source: file.Source, Target: file.Target})
		}
	}
	if len(indices) == 0 {
		return home.flatTargets
	}
	mark := func(reason planner.ResolutionReason, status planner.TerminalStatus) {
		for _, index := range indices {
			markConfigRestoreFailure(&plan.Sets[index], reason, status)
		}
	}
	advanced := mf.HomeManager != nil && mf.HomeManager.Flake != ""
	collision := false
	for left := range files {
		for right := left + 1; right < len(files); right++ {
			if homeTargetsOverlap(files[left].Target, files[right].Target) {
				collision = true
			}
		}
		for _, explicit := range home.flatTargets {
			if !advanced && home.ownershipAction == nil && homeTargetsOverlap(files[left].Target, explicit) {
				collision = true
			}
		}
	}
	if collision {
		home.forceBlocked = !advanced && home.ownershipAction == nil
		mark(planner.ReasonTargetCollision, planner.StatusFailed)
		return home.flatTargets
	}
	var settings *manifest.HomeManagerSettings
	var configPath string
	var secrets []manifest.HomeManagerSecret
	if mf.HomeManager != nil && !advanced {
		settings, configPath, secrets = mf.HomeManager.Settings, mf.HomeManager.Config, mf.HomeManager.Secrets
	}
	ref, err := nix.GenerateHomeFlakeWithPlacements(state.StateDir(), settings, filepath.Dir(flags.Manifest), files, configPath, secrets)
	if err != nil {
		if errors.Is(err, nix.ErrHomeFileCollision) {
			home.forceBlocked = true
			mark(planner.ReasonTargetCollision, planner.StatusFailed)
		} else {
			mark(planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped)
		}
		return home.flatTargets
	}
	if advanced || home.ownershipAction != nil {
		for _, index := range indices {
			markCapturedImportRequired(&plan.Sets[index], ref)
		}
		if !advanced {
			home.flake, home.generated = ref, true
			home.ownershipAction.ModulePath = generatedHomeModulePath(ref)
		}
		return home.flatTargets
	}
	prepared, err := r.(realizer.HomePreparer).PrepareHome(ref, !flags.DryRun)
	if err != nil {
		mark(planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped)
		return home.flatTargets
	}
	known := map[string]bool{}
	for _, target := range prepared.FlatTargets {
		known[filepath.Clean(filepath.FromSlash(target))] = true
	}
	for _, file := range files {
		if !known[filepath.Clean(file.Target)] {
			mark(planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped)
			return home.flatTargets
		}
	}
	var nativeExpectations *provision.HomeFileExpectations
	if !flags.DryRun {
		nativeExpectations, err = readHomeFileExpectations(prepared.Resolution)
		if err != nil {
			mark(planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped)
			return home.flatTargets
		}
		actual := map[string]string{}
		for _, file := range nativeExpectations.Files {
			actual[file.Target] = file.SHA256
		}
		for _, expected := range expectations {
			for _, file := range expected.Files {
				if actual[file.Target] != file.SHA256 {
					mark(planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped)
					return home.flatTargets
				}
			}
		}
	}
	home.flake, home.generated = ref, true
	home.resolution, home.flatTargets, home.fileExpectations = prepared.Resolution, prepared.FlatTargets, nativeExpectations
	home.capturedFiles = expectations
	return home.flatTargets
}

func finalizeCapturedHomeManager(session *configRestoreExecutionSession, home *preparedHomeManager, activated, blocked bool, emitter *events.Emitter) (*ConfigResultFields, *envelope.Error) {
	if session == nil || session.lastExecution == nil {
		return nil, nil
	}
	execution := session.lastExecution
	var envErr *envelope.Error
	if home != nil {
		for index := range execution.Plan.Sets {
			set := &execution.Plan.Sets[index]
			expected, selected := home.capturedFiles[set.Source.CaptureID]
			if !selected || !selectedConfigRestorePlanSet(*set) {
				continue
			}
			switch {
			case blocked:
				markConfigRestoreFailure(set, planner.ReasonTargetCollision, planner.StatusFailed)
			case home.ownershipAction != nil:
				// An external owner found at the pre-activation check owns the file placement.
				markCapturedImportRequired(set, home.flake)
			case !home.activationAttempted:
				// Never attempted: the row stays planned, as in the restore stage's other early returns, instead of reporting a failed write.
			case !activated:
				markConfigRestoreFailure(set, planner.ReasonCommitFailed, planner.StatusFailed)
			default:
				if reason, _ := checkHomeFileExpectations(expected); reason != "" {
					markConfigRestoreFailure(set, planner.ReasonTargetValidationFailed, planner.StatusFailed)
					envErr = envelope.NewError(envelope.ErrRestoreFailed, "Home Manager activated, but captured settings did not match their staged contents.")
				} else {
					set.Resolution.Status = planner.StatusRestored
					set.Resolution = planner.ProjectConfigResolution(*set)
				}
			}
			emitGenerationConfigResolution(emitter, *set)
		}
	}
	if !session.runtime.inputs.hasConfigPayloads {
		return nil, envErr
	}
	return NewConfigResultFields(execution.Plan.Sets, execution.RestoreItems), envErr
}

func markCapturedImportRequired(set *planner.PlanSet, flake string) {
	set.ExecutionRemediation = "Import " + generatedHomeModulePath(flake) + " into the owning Home Manager configuration, then activate it."
	markConfigRestoreFailure(set, planner.ReasonHomeManagerImportRequired, planner.StatusSkipped)
}
