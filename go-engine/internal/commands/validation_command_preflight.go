// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func validationManifestFromCapturePreparation(prepared *captureConfigPreparation, apps []manifest.App) (*manifest.Manifest, error) {
	if prepared == nil {
		return &manifest.Manifest{Version: 1, Apps: append([]manifest.App(nil), apps...)}, nil
	}
	return bundle.ProjectCapturePlanningManifest(apps, prepared.Planning.LegacyModules, prepared.Planning.GenerationPlans)
}

func validationRuntimeIsolationFailure(coordinate, target string, err error) *envelope.Error {
	if currentValidationSession != nil {
		reason := isolationReasonUnsafePath
		if errors.Is(err, validationmode.ErrGuardBudget) {
			reason = isolationReasonGuardBudget
		} else if errors.Is(err, validationmode.ErrUnsafeRegistry) {
			reason = isolationReasonUnsafeRegistry
		}
		_ = currentValidationSession.recordIsolationFinding(coordinate, target, reason)
	}
	return validationCommandIsolationError()
}

func validationCapturePlanningFacts(prepared *captureConfigPreparation) ([]validationProductionConfigPlan, []modules.ConfigInstance) {
	if prepared == nil {
		return nil, nil
	}
	plans := make([]validationProductionConfigPlan, 0, len(prepared.Planning.GenerationPlans))
	instances := make([]modules.ConfigInstance, 0, len(prepared.Planning.GenerationPlans))
	seen := map[string]struct{}{}
	for _, plan := range prepared.Planning.GenerationPlans {
		plans = append(plans, validationProductionConfigPlan{
			SetID: plan.Set.ID, GenerationID: plan.Generation.ID, Instance: plan.Instance,
		})
		if _, exists := seen[plan.Instance.ID]; !exists {
			seen[plan.Instance.ID] = struct{}{}
			instances = append(instances, plan.Instance)
		}
	}
	return plans, instances
}

func preflightActiveValidationCommand(input validationProductionModulePreflight) *envelope.Error {
	if currentValidationMode == nil {
		return nil
	}
	if currentValidationSession == nil {
		return validationCommandIsolationError()
	}
	input.Context = currentValidationMode
	input.Session = currentValidationSession
	if err := preflightValidationProductionModule(input); err != nil {
		return validationCommandIsolationError()
	}
	if err := currentValidationSession.gateMutation(); err != nil {
		return validationCommandIsolationError()
	}
	return nil
}

func gateActiveValidationMutation() *envelope.Error {
	if currentValidationMode == nil {
		return nil
	}
	if currentValidationSession == nil || currentValidationSession.gateMutation() != nil {
		return validationCommandIsolationError()
	}
	return nil
}

func validationCommandIsolationError() *envelope.Error {
	return envelope.NewError(envelope.ErrInternalError, "Validation-mode isolation preflight failed.")
}

func validationManifestPortableRoot(manifestPath string) string {
	root, err := filepath.Abs(filepath.Dir(manifestPath))
	if err != nil {
		return filepath.Clean(filepath.Dir(manifestPath))
	}
	return filepath.Clean(root)
}

func validationConfigPlansFromManifest(value *manifest.Manifest) []validationProductionConfigPlan {
	if value == nil {
		return nil
	}
	plans := make([]validationProductionConfigPlan, 0, len(value.ConfigCaptures))
	for _, capture := range value.ConfigCaptures {
		instance := modules.ConfigInstance{
			ID: capture.SourceInstance.ID, ModuleID: capture.ModuleID,
			DetectorID: capture.SourceInstance.DetectorID,
			Version: modules.VersionEvidence{
				Raw: capture.SourceInstance.RawVersion, Normalized: capture.SourceInstance.NormalizedVersion,
			},
		}
		if evidence := capture.SourceInstance.Evidence; evidence != nil {
			instance.Evidence = modules.InstanceEvidence{
				Type: evidence.Type, AppID: evidence.AppID, Backend: evidence.Backend,
				Platform: evidence.Platform, Ref: evidence.Ref, Driver: evidence.Driver,
			}
		}
		plans = append(plans, validationProductionConfigPlan{
			SetID: capture.ConfigSetID, GenerationID: capture.SourceGeneration, Instance: instance,
		})
	}
	return plans
}

func validationDiscoverCommandInstances(selected []*modules.Module, apps []manifest.App) ([]modules.ConfigInstance, error) {
	if currentValidationMode == nil {
		return nil, nil
	}
	descriptor := currentValidationMode.Descriptor()
	detectionApps := append([]manifest.App(nil), apps...)
	for index := range detectionApps {
		app := &detectionApps[index]
		if app.ID == descriptor.Inventory.AppID {
			app.Installed = descriptor.Inventory.InitialState == "present"
			app.InstalledVersion = descriptor.Inventory.Version
			app.Backend = descriptor.Inventory.Driver
		}
	}
	instances := []modules.ConfigInstance{}
	for _, mod := range selected {
		if mod == nil || mod.Config == nil {
			continue
		}
		discovered, err := modules.DiscoverInstances(mod, capturePackageEvidence(mod, detectionApps), modules.DiscoveryOptions{})
		if err != nil {
			return nil, err
		}
		instances = append(instances, discovered...)
	}
	return instances, nil
}

func validationSandboxTarget(coordinate, value string) validationProductionSandboxTarget {
	absolute, err := filepath.Abs(value)
	if err != nil {
		absolute = value
	}
	return validationProductionSandboxTarget{Coordinate: coordinate, Path: filepath.Clean(absolute)}
}

func preflightActiveValidationSandboxPaths(targets ...validationProductionSandboxTarget) *envelope.Error {
	if currentValidationMode == nil {
		return nil
	}
	if currentValidationSession == nil {
		return validationCommandIsolationError()
	}
	for _, target := range targets {
		if err := currentValidationMode.ValidateSandboxPath(target.Path); err != nil {
			return validationPreflightFailureEnvelope(currentValidationSession, target.Coordinate, "materialized-path")
		}
	}
	return nil
}

func validationSelectedRestoreModules(runtime *configRestoreRuntime, catalog map[string]*modules.Module) []*modules.Module {
	if runtime == nil {
		return nil
	}
	ids := map[string]struct{}{}
	for _, source := range runtime.inputs.generationSources {
		if source.selected {
			ids[source.source.ModuleID] = struct{}{}
		}
	}
	for _, lane := range runtime.inputs.legacyLanes {
		if lane.selected {
			ids[lane.moduleID] = struct{}{}
		}
	}
	selected := make([]*modules.Module, 0, len(ids))
	for id := range ids {
		if mod := catalog[id]; mod != nil {
			selected = append(selected, mod)
		}
	}
	return selected
}

func validationPreflightFailureEnvelope(session *ValidationModeSession, coordinate, target string) *envelope.Error {
	if session != nil {
		_ = session.recordIsolationFinding(coordinate, target, isolationReasonUnsafePath)
	}
	return validationCommandIsolationError()
}
