// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

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

// homeTargetClaims returns the file claims of one concrete target, with the
// aliases of its live ancestors.
func homeTargetClaims(target string) []string {
	identities, err := restore.FilesystemTargetIdentities(target)
	if err != nil {
		// Unavailable filesystem identity supplies no known overlapping claim.
		return nil
	}
	claims := make([]string, 0, len(identities))
	for _, identity := range identities {
		// File claims use the existing checker's closed filesystem protocol.
		claims = append(claims, "file\x00"+strings.TrimSuffix(filepath.ToSlash(identity), "/"))
	}
	return claims
}

func homeTargetsOverlap(left, right string) bool {
	rightClaims := homeTargetClaims(right)
	for _, claim := range homeTargetClaims(left) {
		if anyClaimOverlaps(rightClaims, claim) {
			return true
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
	// Captured placements compose with an explicit configuration into one activation.
	composes := !advanced && home.ownershipAction == nil
	var explicitClaims []string
	if composes {
		if flags.DryRun || home.resolution == nil {
			for _, explicit := range home.flatTargets {
				explicitClaims = append(explicitClaims, homeTargetClaims(explicit)...)
			}
		} else {
			// Home Manager's overlap rule can drop a placement inside an explicit
			// recursive root, so compare with the built leaves before composing.
			var err error
			if explicitClaims, err = home.builtLeafClaims(); err != nil {
				home.forceBlock = planner.ReasonTargetDetectionFailed
				mark(planner.ReasonTargetDetectionFailed, planner.StatusFailed)
				return home.flatTargets
			}
		}
	}
	collision := false
	for left := range files {
		for right := left + 1; right < len(files); right++ {
			if homeTargetsOverlap(files[left].Target, files[right].Target) {
				collision = true
			}
		}
		for _, claim := range homeTargetClaims(files[left].Target) {
			if anyClaimOverlaps(explicitClaims, claim) {
				collision = true
			}
		}
	}
	if collision {
		if composes {
			home.forceBlock = planner.ReasonTargetCollision
		}
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
			home.forceBlock = planner.ReasonTargetCollision
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
	targets := make([]string, 0, len(files))
	for _, file := range files {
		targets = append(targets, file.Target)
	}
	if flags.DryRun {
		known := map[string]bool{}
		for _, target := range prepared.FlatTargets {
			known[filepath.Clean(filepath.FromSlash(target))] = true
		}
		for _, target := range targets {
			if !known[filepath.Clean(target)] {
				mark(planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped)
				return home.flatTargets
			}
		}
	} else {
		// The built leaves replace the evaluated targets, which omit recursive declarations.
		leaves, err := home.builtLeaves(prepared.Resolution)
		if err != nil {
			home.forceBlock = planner.ReasonTargetDetectionFailed
			mark(planner.ReasonTargetDetectionFailed, planner.StatusFailed)
			return home.flatTargets
		}
		if len(leaves.missingTargets(targets)) > 0 {
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
	home.flake, home.generated, home.fileExpectations = ref, true, nativeExpectations
	home.adoptPreparation(prepared)
	home.capturedFiles = expectations
	return home.flatTargets
}

func finalizeCapturedHomeManager(session *configRestoreExecutionSession, home *preparedHomeManager, activated bool, blocked planner.ResolutionReason, emitter *events.Emitter) (*ConfigResultFields, *envelope.Error) {
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
			case blocked != "":
				markConfigRestoreFailure(set, blocked, planner.StatusFailed)
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
