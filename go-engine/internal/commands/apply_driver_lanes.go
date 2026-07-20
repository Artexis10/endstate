// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

type packageAppPlan struct {
	route       *routedDriverApp
	action      ApplyAction
	displayName string
	repin       bool
	detectErr   error
}

func packageDriverPreflight(flags ApplyFlags, mf *manifest.Manifest, emitter *events.Emitter) (map[string]driverLaneOverride, []CommandWarning) {
	chocolateyNeeded := false
	for _, app := range mf.Apps {
		if strings.EqualFold(strings.TrimSpace(app.Driver), string(bootstrap.BackendChocolatey)) && resolveAppRef(app) != "" {
			chocolateyNeeded = true
			break
		}
	}
	if !chocolateyNeeded {
		return nil, nil
	}

	available, bootstrapErr := bootstrapBackendsFn(
		[]bootstrap.Backend{bootstrap.BackendChocolatey},
		!flags.DryRun,
		bootstrapConsent(flags),
		emitter,
	)
	if bootstrapErr != nil {
		message := bootstrapErr.Message
		return map[string]driverLaneOverride{
			"chocolatey": {err: errors.New(bootstrapErr.Message), failed: true},
		}, []CommandWarning{{Code: "optional_driver_unavailable", Message: message, Driver: "chocolatey"}}
	}
	if available[bootstrap.BackendChocolatey] {
		return nil, nil
	}

	failed := flags.BootstrapBackends && !flags.NoBootstrap && !flags.DryRun
	message := "Chocolatey is unavailable; backend setup consent is required"
	if flags.NoBootstrap {
		message = "Chocolatey is unavailable and backend setup was declined"
	} else if flags.DryRun {
		message = "Chocolatey is unavailable; dry-run will not install package backends"
	} else if failed {
		message = "Chocolatey backend setup or verification failed"
	}
	return map[string]driverLaneOverride{
		"chocolatey": {err: errors.New(message), failed: failed},
	}, []CommandWarning{{Code: "optional_driver_unavailable", Message: message, Driver: "chocolatey"}}
}

func runApplyDriverLanes(
	flags ApplyFlags,
	mf *manifest.Manifest,
	emitter *events.Emitter,
	runID string,
	configModuleMap map[string]string,
	packageModuleMap map[string][]string,
	restoreScope *restoreModuleScope,
) (interface{}, *envelope.Error) {
	if flags.Prune {
		return nil, envelope.NewError(
			envelope.ErrConvergenceUnsupported,
			"convergence (--prune) is not supported on this backend").
			WithRemediation("Run on a host with the Nix realizer (Linux/macOS) to use --prune.")
	}

	// The event contract locks the first streamed event to the plan phase. Backend
	// preflight may emit a consent request, so establish the phase before probing.
	emitter.EmitPhase("plan")
	overrides, warnings := packageDriverPreflight(flags, mf, emitter)
	lanes, routed, routeErr := resolvePackageDriverLanesWithOverrides(mf.Apps, overrides)
	if routeErr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, routeErr.Error())
	}
	warnings = append(warnings, possibleDuplicatePackageWarnings(routed)...)
	warnings = append(warnings, restoreScope.warnings()...)
	routedByIndex := make(map[int]*routedDriverApp, len(routed))
	for _, route := range routed {
		routedByIndex[route.index] = route
	}

	detections := detectPackageDriverLanes(lanes)
	planEntries := make([]packageAppPlan, 0, len(routed))
	presentCount, toInstallCount, planSkipped, planFailed := 0, 0, 0, 0

	for appIndex, app := range mf.Apps {
		route, ok := routedByIndex[appIndex]
		if !ok {
			continue
		}
		entry := packageAppPlan{route: route}
		if route.isManual {
			expanded, exists := checkVerifyPath(app.Manual.VerifyPath)
			entry.action = ApplyAction{ID: app.ID, Driver: "manual", Name: app.DisplayName}
			if exists {
				entry.action.Status = driver.StatusPresent
				entry.action.Reason = driver.ReasonAlreadyInstalled
				entry.action.Message = fmt.Sprintf("Verified at %s", expanded)
				emitter.EmitItem(app.ID, "manual", driver.StatusPresent, driver.ReasonAlreadyInstalled, entry.action.Message, app.DisplayName)
				presentCount++
			} else {
				entry.action.Status = "to_install"
				entry.action.Reason = "manual_required"
				entry.action.Message = fmt.Sprintf("Not found at %s", expanded)
				entry.action.Manual = app.Manual
				emitter.EmitItem(app.ID, "manual", "to_install", "manual_required", entry.action.Message, app.DisplayName)
				toInstallCount++
			}
			planEntries = append(planEntries, entry)
			continue
		}

		entry.action = ApplyAction{
			ID:     app.ID,
			Ref:    stringPtr(route.ref),
			Driver: route.driverName,
			Name:   resolveItemDisplayName("", app, route.ref),
		}
		entry.displayName = entry.action.Name
		if route.err != nil || route.drv == nil {
			entry.action.Status = driver.StatusSkipped
			entry.action.Reason = driver.ReasonFiltered
			entry.action.Message = route.err.Error()
			if route.failed {
				entry.action.Status = driver.StatusFailed
				entry.action.Reason = driver.ReasonInstallFailed
				planFailed++
				emitter.EmitItem(route.ref, route.driverName, driver.StatusFailed, driver.ReasonInstallFailed, entry.action.Message, entry.displayName)
			} else {
				planSkipped++
				emitter.EmitItem(route.ref, route.driverName, driver.StatusSkipped, driver.ReasonFiltered, entry.action.Message, entry.displayName)
			}
			planEntries = append(planEntries, entry)
			continue
		}

		detection := detections[route.driverName]
		var installed bool
		var displayName, version string
		if detection.err != nil {
			entry.detectErr = detection.err
		} else if detection.batchUsed {
			if result, ok := detection.results[route.ref]; ok {
				installed, displayName, version = result.Installed, result.DisplayName, result.Version
			}
		} else {
			installed, displayName, entry.detectErr = route.drv.Detect(route.ref)
		}
		if displayName != "" {
			entry.displayName = displayName
			entry.action.Name = displayName
		}
		entry.action.Version = version

		if entry.detectErr != nil {
			entry.action.Status = driver.StatusFailed
			entry.action.Reason = driver.ReasonInstallFailed
			entry.action.Message = entry.detectErr.Error()
			planFailed++
			emitter.EmitItem(route.ref, route.driverName, driver.StatusFailed, driver.ReasonInstallFailed, entry.action.Message, entry.displayName)
		} else if installed {
			entry.action.Status = driver.StatusPresent
			entry.action.Reason = driver.ReasonAlreadyInstalled
			if flags.Repin && app.Version != "" && version != "" && strings.TrimSpace(version) != strings.TrimSpace(app.Version) {
				entry.repin = true
				entry.action.Reason = driver.ReasonVersionDrift
				entry.action.Message = fmt.Sprintf("Version drift: installed %s, want %s", version, app.Version)
				emitter.EmitItem(route.ref, route.driverName, driver.StatusPresent, driver.ReasonVersionDrift, entry.action.Message, entry.displayName)
			} else {
				entry.action.Message = "Already installed"
				emitter.EmitItem(route.ref, route.driverName, driver.StatusPresent, driver.ReasonAlreadyInstalled, entry.action.Message, entry.displayName)
			}
			presentCount++
		} else {
			entry.action.Status = "to_install"
			entry.action.Reason = driver.ReasonMissing
			entry.action.Message = "Will be installed"
			toInstallCount++
			emitter.EmitItem(route.ref, route.driverName, "to_install", driver.ReasonMissing, entry.action.Message, entry.displayName)
		}
		planEntries = append(planEntries, entry)
	}

	totalApps := len(planEntries)
	emitter.EmitSummary("plan", totalApps, presentCount, planSkipped, toInstallCount+planFailed)
	finalActions := make([]ApplyAction, len(planEntries))
	for i := range planEntries {
		finalActions[i] = planEntries[i].action
		finalActions[i].WasPresent = planEntries[i].repin
	}
	configSession, configSessionErr := prepareApplyConfigRestore(
		context.Background(),
		flags,
		newDriverLaneConfigRestoreEvidenceSource(lanes),
	)
	if configSessionErr != nil {
		return nil, configSessionErr
	}
	var configFields *ConfigResultFields
	if flags.DryRun {
		var configErr *envelope.Error
		configFields, configErr = executePreparedApplyConfigRestore(
			context.Background(), flags, runID, emitter, configSession,
		)
		if configErr != nil {
			return &ApplyResult{
				DryRun:   true,
				Manifest: ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
				Summary:  ApplySummary{Total: totalApps, Skipped: presentCount + planSkipped, Failed: planFailed},
				Actions:  finalActions, ConfigModuleMap: configModuleMap, PackageModuleMap: packageModuleMap,
				Warnings: warnings, RestoreModulesAvailable: restoreScope.modules(), ConfigResultFields: configFields,
			}, configErr
		}
	}

	successCount, skippedCount, failedCount := 0, 0, 0
	if !flags.DryRun {
		emitter.EmitPhase("apply")
		for i, entry := range planEntries {
			route := entry.route
			if route.isManual {
				if entry.action.Status == driver.StatusPresent {
					successCount++
				} else {
					finalActions[i].Status = driver.StatusSkipped
					finalActions[i].Reason = "manual_required"
					emitter.EmitItem(entry.route.app.ID, "manual", driver.StatusSkipped, "manual_required", finalActions[i].Message, entry.route.app.DisplayName)
					skippedCount++
				}
				continue
			}
			if entry.action.Status == driver.StatusSkipped {
				emitter.EmitItem(route.ref, route.driverName, driver.StatusSkipped, entry.action.Reason, entry.action.Message, entry.displayName)
				skippedCount++
				continue
			}
			if entry.action.Status == driver.StatusFailed {
				emitter.EmitItem(route.ref, route.driverName, driver.StatusFailed, entry.action.Reason, entry.action.Message, entry.displayName)
				failedCount++
				continue
			}

			if entry.repin {
				vi, ok := route.drv.(driver.VersionedInstaller)
				if !flags.Confirm || !ok {
					skippedCount++
					continue
				}
				emitter.EmitItem(route.ref, route.driverName, "installing", "", fmt.Sprintf("Re-pinning %s to %s", route.ref, route.app.Version), entry.displayName)
				result, err := vi.ReinstallVersion(route.ref, route.app.Version)
				if err != nil {
					finalActions[i].Status, finalActions[i].Reason, finalActions[i].Message = driver.StatusFailed, driver.ReasonInstallFailed, err.Error()
					emitter.EmitItem(route.ref, route.driverName, driver.StatusFailed, driver.ReasonInstallFailed, err.Error(), entry.displayName)
					failedCount++
					continue
				}
				finalActions[i].Status = result.Status
				finalActions[i].Reason = result.Reason
				finalActions[i].Message = result.Message
				finalActions[i].RebootRequired = result.RebootRequired
				if result.Status == driver.StatusInstalled {
					finalActions[i].Reason = ""
					finalActions[i].Version = route.app.Version
					emitter.EmitItemWithReboot(route.ref, route.driverName, driver.StatusInstalled, "", result.Message, entry.displayName, result.RebootRequired)
					successCount++
				} else {
					emitter.EmitItemWithReboot(route.ref, route.driverName, result.Status, result.Reason, result.Message, entry.displayName, result.RebootRequired)
					failedCount++
				}
				continue
			}

			if entry.action.Status != "to_install" {
				skippedCount++
				continue
			}
			emitter.EmitItem(route.ref, route.driverName, "installing", "", fmt.Sprintf("Installing %s", route.ref), entry.displayName)
			pinned := route.app.Version != ""
			var result *driver.InstallResult
			var installErr error
			if vi, ok := route.drv.(driver.VersionedInstaller); ok && pinned {
				result, installErr = vi.InstallVersion(route.ref, route.app.Version)
			} else {
				result, installErr = route.drv.Install(route.ref)
			}
			if installErr != nil {
				finalActions[i].Status, finalActions[i].Reason, finalActions[i].Message = driver.StatusFailed, driver.ReasonInstallFailed, installErr.Error()
				emitter.EmitItem(route.ref, route.driverName, driver.StatusFailed, driver.ReasonInstallFailed, installErr.Error(), entry.displayName)
				failedCount++
				continue
			}
			finalActions[i].Status = result.Status
			finalActions[i].Reason = result.Reason
			finalActions[i].Message = result.Message
			finalActions[i].RebootRequired = result.RebootRequired
			switch result.Status {
			case driver.StatusInstalled:
				finalActions[i].Reason = ""
				if pinned {
					finalActions[i].Version = route.app.Version
				}
				emitter.EmitItemWithReboot(route.ref, route.driverName, driver.StatusInstalled, "", result.Message, entry.displayName, result.RebootRequired)
				successCount++
			case driver.StatusPresent:
				emitter.EmitItemWithReboot(route.ref, route.driverName, driver.StatusPresent, result.Reason, result.Message, entry.displayName, result.RebootRequired)
				skippedCount++
			default:
				emitter.EmitItemWithReboot(route.ref, route.driverName, result.Status, result.Reason, result.Message, entry.displayName, result.RebootRequired)
				failedCount++
			}
		}

		emitter.EmitSummary("apply", successCount+skippedCount+failedCount, successCount, skippedCount, failedCount)

		if flags.Repin && !flags.Confirm {
			return nil, envelope.NewError(
				envelope.ErrInternalError,
				"version convergence (--repin) requires --confirm (it reinstalls drifted packages)").
				WithRemediation("Re-run with --repin --confirm, or use --repin --dry-run to preview.")
		}

		var configErr *envelope.Error
		configFields, configErr = executePreparedApplyConfigRestore(
			context.Background(), flags, runID, emitter, configSession,
		)
		if configErr != nil {
			// Package mutation already committed. Preserve backend rollback facts
			// even when the transactional configuration stage refuses or fails.
			writePackageDriverGenerations(runID, lanes, finalActions)
			return &ApplyResult{
				DryRun:   false,
				Manifest: ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
				Summary:  ApplySummary{Total: totalApps, Success: successCount, Skipped: skippedCount, Failed: failedCount},
				Actions:  finalActions, ConfigModuleMap: configModuleMap, PackageModuleMap: packageModuleMap,
				Warnings: warnings, RestoreModulesAvailable: restoreScope.modules(), ConfigResultFields: configFields,
			}, configErr
		}

		emitter.EmitPhase("verify")
		verifyDetections := detectPackageDriverLanes(lanes)
		verifyPass, verifyFail, verifySkip := 0, 0, 0
		for i, entry := range planEntries {
			route := entry.route
			if route.isManual {
				expanded, exists := checkVerifyPath(route.app.Manual.VerifyPath)
				if exists {
					emitter.EmitItem(route.app.ID, "manual", driver.StatusPresent, "", fmt.Sprintf("Verified at %s", expanded), route.app.DisplayName)
					verifyPass++
				} else {
					emitter.EmitItem(route.app.ID, "manual", driver.StatusFailed, driver.ReasonMissing, fmt.Sprintf("Missing at %s", expanded), route.app.DisplayName)
					verifyFail++
				}
				continue
			}
			if route.err != nil || route.drv == nil {
				if route.failed {
					emitter.EmitItem(route.ref, route.driverName, driver.StatusFailed, driver.ReasonInstallFailed, route.err.Error(), entry.displayName)
					verifyFail++
				} else {
					emitter.EmitItem(route.ref, route.driverName, driver.StatusSkipped, driver.ReasonFiltered, route.err.Error(), entry.displayName)
					verifySkip++
				}
				continue
			}

			detection := verifyDetections[route.driverName]
			var detected bool
			var verifyName, verifyVersion string
			var detectErr error
			if detection.err != nil {
				detectErr = detection.err
			} else if detection.batchUsed {
				if result, ok := detection.results[route.ref]; ok {
					detected, verifyName, verifyVersion = result.Installed, result.DisplayName, result.Version
				}
			} else {
				detected, verifyName, detectErr = route.drv.Detect(route.ref)
			}
			if detectErr != nil {
				emitter.EmitItem(route.ref, route.driverName, driver.StatusFailed, driver.ReasonInstallFailed, detectErr.Error(), entry.displayName)
				verifyFail++
			} else if got, want := strings.TrimSpace(verifyVersion), strings.TrimSpace(route.app.Version); detected && want != "" && got != "" && got != want {
				message := fmt.Sprintf("installed %s, want %s", got, want)
				emitter.EmitItem(route.ref, route.driverName, driver.StatusFailed, driver.ReasonVersionDrift, message, resolveItemDisplayName(verifyName, route.app, route.ref))
				if verifyName != "" {
					finalActions[i].Name = verifyName
				}
				finalActions[i].Version = got
				finalActions[i].Reason = driver.ReasonVersionDrift
				finalActions[i].Message = message
				verifyFail++
			} else if detected {
				emitter.EmitItem(route.ref, route.driverName, driver.StatusPresent, "", "Verified installed", resolveItemDisplayName(verifyName, route.app, route.ref))
				if verifyName != "" {
					finalActions[i].Name = verifyName
				}
				if verifyVersion != "" {
					finalActions[i].Version = verifyVersion
				}
				verifyPass++
			} else {
				emitter.EmitItem(route.ref, route.driverName, driver.StatusFailed, driver.ReasonMissing, "Missing after apply", entry.displayName)
				verifyFail++
			}
		}
		emitter.EmitSummary("verify", verifyPass+verifyFail+verifySkip, verifyPass, verifySkip, verifyFail)
		writePackageDriverGenerations(runID, lanes, finalActions)
	}

	outSummary := ApplySummary{Total: totalApps}
	if flags.DryRun {
		outSummary.Skipped = presentCount + planSkipped
		outSummary.Failed = planFailed
	} else {
		outSummary.Success, outSummary.Skipped, outSummary.Failed = successCount, skippedCount, failedCount
	}
	return &ApplyResult{
		DryRun: flags.DryRun,
		Manifest: ApplyManifestRef{
			Path: flags.Manifest,
			Name: mf.Name,
		},
		Summary:                 outSummary,
		Actions:                 finalActions,
		ConfigModuleMap:         configModuleMap,
		PackageModuleMap:        packageModuleMap,
		Warnings:                warnings,
		RestoreModulesAvailable: restoreScope.modules(),
		ConfigResultFields:      configFields,
	}, nil
}

func writePackageDriverGenerations(runID string, lanes []packageDriverLane, actions []ApplyAction) {
	for _, lane := range lanes {
		laneActions := make([]ApplyAction, 0, len(lane.apps))
		partial := false
		for _, action := range actions {
			if action.Driver != lane.name {
				continue
			}
			laneActions = append(laneActions, action)
			partial = partial || action.Status == driver.StatusFailed || action.Reason == driver.ReasonVersionDrift
		}
		writeProvisioningGeneration(runID, lane.name, laneActions, nil, "", partial, nil)
	}
}
