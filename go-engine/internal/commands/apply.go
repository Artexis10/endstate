// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

// stringPtr returns a pointer to s.
func stringPtr(s string) *string { return &s }

// expandVerifyPath expands Windows-style %VAR% and Go-style $VAR environment
// variables in a verify path. Uses the same expansion as the restore module.
func expandVerifyPath(p string) string {
	expanded := config.ExpandWindowsEnvVars(p)
	expanded = os.ExpandEnv(expanded)
	return expanded
}

// checkVerifyPath expands environment variables in verifyPath and checks if
// the resulting filesystem path exists.
func checkVerifyPath(verifyPath string) (expanded string, exists bool) {
	expanded = expandVerifyPath(verifyPath)
	_, err := os.Stat(expanded)
	return expanded, err == nil
}

// ApplyFlags holds the parsed CLI flags for the apply command.
type ApplyFlags struct {
	// Manifest is the path to the .jsonc manifest file.
	Manifest string
	// DryRun previews the plan without making any changes.
	DryRun bool
	// EnableRestore enables configuration restore operations during apply.
	EnableRestore bool
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
	// Export is the path to the export directory for Model B source resolution.
	Export string
	// RestoreFilter limits restore to entries matching specific module IDs
	// (comma-separated).
	RestoreFilter string
}

// ApplyResult is the data payload for the apply command JSON envelope.
// Shape matches docs/contracts/cli-json-contract.md section "Command: apply".
type ApplyResult struct {
	DryRun                  bool              `json:"dryRun"`
	Manifest                ApplyManifestRef  `json:"manifest"`
	Summary                 ApplySummary      `json:"summary"`
	Actions                 []ApplyAction     `json:"actions"`
	ConfigModuleMap         map[string]string `json:"configModuleMap,omitempty"`
	RestoreModulesAvailable []string          `json:"restoreModulesAvailable,omitempty"`
}

// ApplyManifestRef identifies the manifest used for the apply run.
type ApplyManifestRef struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Hash string `json:"hash"`
}

// ApplySummary aggregates outcome counts for the apply run.
type ApplySummary struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

// ApplyAction records the planned or executed action for a single app entry.
type ApplyAction struct {
	ID      string           `json:"id"`
	Ref     *string          `json:"ref"`
	Driver  string           `json:"driver"`
	Name    string           `json:"name,omitempty"`
	Status  string           `json:"status"`
	Reason  string           `json:"reason,omitempty"`
	Message string           `json:"message,omitempty"`
	Manual  *manifest.ManualApp `json:"manual"`
}

// RunApply executes the apply command with the provided flags.
//
// The algorithm mirrors Invoke-ApplyCore and Invoke-VerifyCore from
// bin/endstate.ps1 and follows three phases:
//
// Phase 1 — Plan
//   - Load manifest.
//   - Detect each app via winget.
//   - Build actions list: status "present" or "to_install".
//   - Emit PhaseEvent("plan"), ItemEvents, SummaryEvent("plan").
//
// Phase 2 — Apply (skipped when DryRun is true)
//   - For each "to_install" action, install via winget.
//   - Emit PhaseEvent("apply"), ItemEvents (installing → result), SummaryEvent("apply").
//
// Phase 3 — Verify (skipped when DryRun is true)
//   - Re-detect all apps with a fresh winget query.
//   - Emit PhaseEvent("verify"), ItemEvents, SummaryEvent("verify").
//
// EnableRestore is accepted but logs a no-op notice; restore is Phase 2 work.
func RunApply(flags ApplyFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("apply")
	emitter := events.NewEmitter(runID, flags.Events == "jsonl")

	// EnableRestore is handled after the install phase (before verify).

	// ----------------------------------------------------------------
	// Phase 1: Plan
	// ----------------------------------------------------------------

	mf, envelopeErr := loadManifest(flags.Manifest)
	if envelopeErr != nil {
		return nil, envelopeErr
	}

	// Resolve module catalog for configModuleMap (non-fatal if unavailable).
	var configModuleMap map[string]string
	var restoreModulesAvailable []string

	repoRoot := config.ResolveRepoRoot()
	if repoRoot != "" {
		catalog, catalogErr := loadModuleCatalogFn(repoRoot)
		if catalogErr == nil && len(catalog) > 0 {
			// Synthesize manual app entries from configModules with pathExists
			// matchers before the plan loop, so they enter the plan.
			modules.SynthesizeAppsFromModules(mf, catalog)

			matchedModules := modules.MatchModulesForApps(catalog, mf.Apps)
			if len(matchedModules) > 0 {
				configModuleMap = make(map[string]string, len(matchedModules))
				for _, mod := range matchedModules {
					for _, wingetRef := range mod.Matches.Winget {
						configModuleMap[wingetRef] = mod.ID
					}
					restoreModulesAvailable = append(restoreModulesAvailable, mod.ID)
				}
			}
		}
	}

	d := newDriverFn()

	// First event in stream MUST be a phase event per event-contract.md.
	emitter.EmitPhase("plan")

	type appPlan struct {
		app         manifest.App
		ref         string
		isManual    bool
		action      ApplyAction
		displayName string
	}

	var planEntries []appPlan
	presentCount := 0
	toInstallCount := 0

	for _, app := range mf.Apps {
		ref := resolveWindowsRef(app)
		isManual := ref == "" && app.Manual != nil && app.Manual.VerifyPath != ""

		if ref == "" && !isManual {
			continue
		}

		if isManual {
			// Manual app: check verifyPath existence.
			expanded, exists := checkVerifyPath(app.Manual.VerifyPath)

			var action ApplyAction
			action.ID = app.ID
			action.Ref = nil
			action.Driver = "manual"
			action.Name = app.DisplayName

			if exists {
				action.Status = "present"
				action.Reason = driver.ReasonAlreadyInstalled
				action.Message = fmt.Sprintf("Verified at %s", expanded)
				emitter.EmitItem(app.ID, "manual", "present", driver.ReasonAlreadyInstalled, action.Message, "")
				presentCount++
			} else {
				action.Status = "to_install"
				action.Reason = "manual_required"
				action.Message = fmt.Sprintf("Not found at %s", expanded)
				action.Manual = app.Manual
				emitter.EmitItem(app.ID, "manual", "to_install", "manual_required", action.Message, "")
				toInstallCount++
			}

			planEntries = append(planEntries, appPlan{app: app, ref: "", isManual: true, action: action})
			continue
		}

		// Winget app: detect via driver.
		installed, displayName, _ := d.Detect(ref)

		var action ApplyAction
		action.ID = app.ID
		action.Ref = stringPtr(ref)
		action.Driver = d.Name()
		action.Name = displayName

		if installed {
			action.Status = "present"
			action.Reason = driver.ReasonAlreadyInstalled
			emitter.EmitItem(ref, d.Name(), "present", driver.ReasonAlreadyInstalled, "Already installed", displayName)
			presentCount++
		} else {
			action.Status = "to_install"
			action.Reason = driver.ReasonMissing
			emitter.EmitItem(ref, d.Name(), "to_install", driver.ReasonMissing, "Will be installed", "")
			toInstallCount++
		}

		planEntries = append(planEntries, appPlan{app: app, ref: ref, action: action, displayName: displayName})
	}

	totalApps := len(planEntries)
	emitter.EmitSummary("plan", totalApps, presentCount, 0, toInstallCount)

	// ----------------------------------------------------------------
	// Phase 2: Apply  (skip when dry-run)
	// ----------------------------------------------------------------

	successCount := 0
	skippedCount := 0
	failedCount := 0

	// Initialise final action slice from plan (will be mutated below).
	finalActions := make([]ApplyAction, len(planEntries))
	for i, entry := range planEntries {
		finalActions[i] = entry.action
	}

	if !flags.DryRun {
		emitter.EmitPhase("apply")

		for i, entry := range planEntries {
			if entry.isManual {
				// Manual apps: re-check verifyPath during apply.
				if entry.action.Status == "present" {
					successCount++
				} else {
					// Not present: status "skipped", reason "manual_required".
					finalActions[i].Status = driver.StatusSkipped
					finalActions[i].Reason = "manual_required"
					emitter.EmitItem(entry.app.ID, "manual", "skipped", "manual_required", finalActions[i].Message, "")
					skippedCount++
				}
				continue
			}

			if entry.action.Status != "to_install" {
				// Already present: counts as skipped in the apply phase.
				skippedCount++
				continue
			}

			emitter.EmitItem(entry.ref, d.Name(), "installing", "", fmt.Sprintf("Installing %s", entry.ref), entry.displayName)

			result, installErr := d.Install(entry.ref)
			if installErr != nil {
				// Infrastructure failure (e.g. winget not available).
				finalActions[i].Status = driver.StatusFailed
				finalActions[i].Reason = driver.ReasonInstallFailed
				emitter.EmitItem(entry.ref, d.Name(), "failed", driver.ReasonInstallFailed, installErr.Error(), entry.displayName)
				failedCount++
				continue
			}

			finalActions[i].Status = result.Status
			finalActions[i].Reason = result.Reason

			switch result.Status {
			case driver.StatusInstalled:
				emitter.EmitItem(entry.ref, d.Name(), "installed", "", result.Message, entry.displayName)
				successCount++
			case driver.StatusPresent:
				emitter.EmitItem(entry.ref, d.Name(), "present", result.Reason, result.Message, entry.displayName)
				skippedCount++
			default:
				emitter.EmitItem(entry.ref, d.Name(), result.Status, result.Reason, result.Message, entry.displayName)
				failedCount++
			}
		}

		applyTotal := successCount + skippedCount + failedCount
		emitter.EmitSummary("apply", applyTotal, successCount, skippedCount, failedCount)

		// ----------------------------------------------------------------
		// Phase 2b: Restore  (when --enable-restore and manifest has entries)
		// ----------------------------------------------------------------

		if flags.EnableRestore && len(mf.Restore) > 0 {
			emitter.EmitPhase("restore")

			manifestDir := filepath.Dir(flags.Manifest)
			absManifestDir, _ := filepath.Abs(manifestDir)
			actions := convertToActions(mf.Restore, flags.RestoreFilter)

			exportRoot := ""
			if flags.Export != "" {
				exportRoot, _ = filepath.Abs(flags.Export)
			}

			repoRoot := config.ResolveRepoRoot()
			backupDir := ""
			if repoRoot != "" {
				backupDir = filepath.Join(repoRoot, "state", "backups", runID)
			}

			restoreOpts := restore.RestoreOptions{
				DryRun:      false, // apply is non-dry-run at this point
				BackupDir:   backupDir,
				ManifestDir: absManifestDir,
				ExportRoot:  exportRoot,
				RunID:       runID,
			}

			restoreResults, restoreErr := restore.RunRestore(actions, restoreOpts)
			if restoreErr != nil {
				emitter.EmitError("engine", "Restore failed: "+restoreErr.Error(), "")
			} else {
				restoredCnt := 0
				skippedCnt := 0
				failedCnt := 0
				for _, r := range restoreResults {
					switch r.Status {
					case "restored":
						emitter.EmitItem(r.ID, "restore", "restored", "", "Restored "+r.Target, "")
						restoredCnt++
					case "skipped_up_to_date", "skipped_missing_source":
						emitter.EmitItem(r.ID, "restore", "skipped", "", r.Status, "")
						skippedCnt++
					case "failed":
						emitter.EmitItem(r.ID, "restore", "failed", "", r.Error, "")
						failedCnt++
					}
				}
				emitter.EmitSummary("restore", len(restoreResults), restoredCnt, skippedCnt, failedCnt)

				// Write journal for restore phase.
				if repoRoot != "" {
					logsDir := filepath.Join(repoRoot, "logs")
					absManifest, _ := filepath.Abs(flags.Manifest)
					_ = restore.WriteJournal(logsDir, runID, absManifest, absManifestDir, exportRoot, restoreResults)
				}
			}
		}

		// ----------------------------------------------------------------
		// Phase 3: Verify  (fresh re-detection)
		// ----------------------------------------------------------------

		emitter.EmitPhase("verify")

		verifyPass := 0
		verifyFail := 0

		for i, entry := range planEntries {
			if entry.isManual {
				// Manual app verify: re-check verifyPath.
				expanded, exists := checkVerifyPath(entry.app.Manual.VerifyPath)
				if exists {
					emitter.EmitItem(entry.app.ID, "manual", "present", "", fmt.Sprintf("Verified at %s", expanded), "")
					verifyPass++
				} else {
					emitter.EmitItem(entry.app.ID, "manual", "failed", driver.ReasonMissing, fmt.Sprintf("Missing at %s", expanded), "")
					verifyFail++
				}
				continue
			}

			detected, verifyName, _ := d.Detect(entry.ref)
			if detected {
				emitter.EmitItem(entry.ref, d.Name(), "present", "", "Verified installed", verifyName)
				if verifyName != "" {
					finalActions[i].Name = verifyName
				}
				verifyPass++
			} else {
				emitter.EmitItem(entry.ref, d.Name(), "failed", driver.ReasonMissing, "Missing after apply", "")
				verifyFail++
			}
		}

		verifyTotal := verifyPass + verifyFail
		// Last event in stream is always a summary event per event-contract.md.
		emitter.EmitSummary("verify", verifyTotal, verifyPass, 0, verifyFail)
	}

	// Build the return summary from the apply phase counters.
	// When dry-run, we report the plan counts (present=skipped, to_install=pending).
	var outSummary ApplySummary
	outSummary.Total = totalApps
	if flags.DryRun {
		// Dry-run: no installs executed. Report plan state.
		outSummary.Success = 0
		outSummary.Skipped = presentCount  // already-present apps are effectively "skipped"
		outSummary.Failed = 0
	} else {
		outSummary.Success = successCount
		outSummary.Skipped = skippedCount
		outSummary.Failed = failedCount
	}

	return &ApplyResult{
		DryRun: flags.DryRun,
		Manifest: ApplyManifestRef{
			Path: flags.Manifest,
			Name: mf.Name,
			Hash: "", // hash computation is Phase 2 work
		},
		Summary:                 outSummary,
		Actions:                 finalActions,
		ConfigModuleMap:         configModuleMap,
		RestoreModulesAvailable: restoreModulesAvailable,
	}, nil
}

