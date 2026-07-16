// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/state"
)

// stringPtr returns a pointer to s.
func stringPtr(s string) *string { return &s }

// expandVerifyPath expands Windows-style %VAR% and Go-style $VAR environment
// variables in a verify path. Uses the same expansion as the restore module.
func expandVerifyPath(p string) string {
	expanded := config.ExpandEnvVars(p)
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

// resolveModuleDisplayName returns a human-readable display name for a module.
// Resolution order: (1) DisplayName field, (2) short ID with "apps." prefix stripped.
func resolveModuleDisplayName(mod *modules.Module) string {
	if mod.DisplayName != "" {
		return mod.DisplayName
	}
	return strings.TrimPrefix(mod.ID, "apps.")
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
	// RestoreTargets contains repeatable capture-to-target mappings. Command
	// orchestration validates these against generation-aware capture IDs.
	RestoreTargets []string
	// Prune enables convergence: after install, remove installed-but-undeclared
	// packages ("drift") from the engine-managed set. Realizer-only; the winget
	// driver path refuses with CONVERGENCE_UNSUPPORTED.
	Prune bool
	// Confirm authorizes the destructive prune. Without it, --prune refuses
	// (unless --dry-run, which only previews).
	Confirm bool
	// Repin enables version convergence: reinstall a declared App.Version over an
	// already-installed drifted version. Winget-only; requires --confirm (unless
	// --dry-run). The realizer path ignores it (Nix pins via its ref).
	Repin bool
	// BootstrapBackends (--bootstrap-backends) authorizes the engine to install an
	// absent package backend (the Nix realizer / Homebrew driver) on macOS/Linux
	// via its official installer. Consent for the backend-bootstrap pre-step.
	BootstrapBackends bool
	// NoBootstrap (--no-bootstrap) forces the backend-bootstrap pre-step to skip an
	// absent backend's lane rather than install it. Takes precedence over
	// BootstrapBackends.
	NoBootstrap bool
	// Only limits the run to the comma-separated list of manifest app IDs. When
	// non-empty, filtering happens before planning so every downstream stage
	// (plan, drivers, config-module expansion, restore scoping, verify, events,
	// summary counts) sees only the selected apps. Incompatible with --prune.
	Only string

	// Prepared command-scoped restore facts are carried into alternate backend
	// paths without reloading or renormalizing the manifest/catalog.
	configRestoreRuntime  *configRestoreRuntime
	configRestoreRepoRoot string
}

// parseOnlyIDs normalises the --only value into a deduplicated set of app IDs.
// Blank entries (e.g. leading/trailing commas) are dropped. Returns nil when
// the flag is empty (feature disabled, unchanged behaviour).
func parseOnlyIDs(only string) []string {
	if only == "" {
		return nil
	}
	parts := strings.Split(only, ",")
	seen := make(map[string]bool, len(parts))
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// filterAppsByOnly returns the subset of apps whose ID is in the allowSet, and
// a slice of IDs from allowSet that matched nothing (unknown IDs).
func filterAppsByOnly(apps []manifest.App, allowSet []string) (filtered []manifest.App, unknown []string) {
	allow := make(map[string]bool, len(allowSet))
	for _, id := range allowSet {
		allow[id] = true
	}
	matched := make(map[string]bool, len(allowSet))
	for _, app := range apps {
		if allow[app.ID] {
			filtered = append(filtered, app)
			matched[app.ID] = true
		}
	}
	for _, id := range allowSet {
		if !matched[id] {
			unknown = append(unknown, id)
		}
	}
	return filtered, unknown
}

// validateOnly checks the --only flag value against the (fully-synthesized)
// manifest app set and returns the filtered []manifest.App plus a validation
// error if any IDs are unknown, the selection is empty, or --only is combined
// with --prune. Returns (nil, nil) when --only was not provided (empty string),
// indicating the feature is disabled.
//
// IMPORTANT: mf.Apps must already include synthesized apps from
// SynthesizeAppsFromModules before this function is called, so that module-
// derived manual apps are selectable/filterable just like regular apps.
func validateOnly(flags ApplyFlags, mf *manifest.Manifest) ([]manifest.App, *envelope.Error) {
	// Feature disabled: --only not provided.
	if flags.Only == "" {
		return nil, nil
	}

	// Guard: --only + --prune is invalid. Prune converges to the EXACT manifest
	// set; pruning against a deliberate subset would classify every unselected app
	// as drift. Check before parsing so the error surfaces even for blank --only.
	if flags.Prune {
		return nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			"--only and --prune cannot be combined: --prune converges to the exact manifest set, which conflicts with a deliberate subset selection").
			WithRemediation("Remove --prune when using --only, or omit --only to converge the full manifest.")
	}

	ids := parseOnlyIDs(flags.Only)

	// Empty selection after normalisation (e.g. --only "  ,  ").
	if len(ids) == 0 {
		return nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			"--only requires at least one app id; the provided value is empty after normalisation").
			WithRemediation("Provide one or more comma-separated app ids, e.g. --only git,vscode.")
	}

	filtered, unknown := filterAppsByOnly(mf.Apps, ids)

	if len(unknown) > 0 {
		return nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			fmt.Sprintf("--only references app ids not found in the manifest: %s", strings.Join(unknown, ", "))).
			WithRemediation("Check spelling and ensure the ids match the 'id' field of apps declared in the manifest.")
	}

	if len(filtered) == 0 {
		return nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			"--only produced an empty app selection; no apps would be processed").
			WithRemediation("Provide ids that match at least one app declared in the manifest.")
	}

	return filtered, nil
}

// RestoreModuleRef identifies a config module available for restore, including
// a human-readable display name resolved from the module catalog.
type RestoreModuleRef struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// ApplyResult is the data payload for the apply command JSON envelope.
// Shape matches docs/contracts/cli-json-contract.md section "Command: apply".
type ApplyResult struct {
	DryRun                  bool               `json:"dryRun"`
	Manifest                ApplyManifestRef   `json:"manifest"`
	Summary                 ApplySummary       `json:"summary"`
	Actions                 []ApplyAction      `json:"actions"`
	ConfigModuleMap         map[string]string  `json:"configModuleMap,omitempty"`
	RestoreModulesAvailable []RestoreModuleRef `json:"restoreModulesAvailable,omitempty"`
	// Pruned lists the engine-managed element names removed by --prune
	// convergence (or, on --dry-run, that would be removed). Omitted when empty.
	Pruned []string `json:"pruned,omitempty"`
	// HomeManager reports the home-manager configuration stage (realizer-only,
	// --enable-restore). It carries the flakeref the engine activated (or, on
	// --dry-run, WOULD activate) and whether that flakeref was engine-generated
	// from a homeManager.config (vs a direct homeManager.flake). Omitted when no
	// config stage ran.
	HomeManager *ApplyHomeManager `json:"homeManager,omitempty"`
	*ConfigResultFields
}

// ApplyHomeManager surfaces the home-manager configuration stage in the apply
// result. For a homeManager.config input, Flake is the engine-generated,
// inspectable wrapper flakeref (`<dir>#<name>`) and Generated is true; for a
// direct homeManager.flake it is that flakeref and Generated is false. Activated
// is false on --dry-run (revealed but not activated).
type ApplyHomeManager struct {
	Flake     string `json:"flake"`
	Generated bool   `json:"generated"`
	Activated bool   `json:"activated"`
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
	ID      string              `json:"id"`
	Ref     *string             `json:"ref"`
	Driver  string              `json:"driver"`
	Name    string              `json:"name,omitempty"`
	Status  string              `json:"status"`
	Reason  string              `json:"reason,omitempty"`
	Message string              `json:"message,omitempty"`
	Version string              `json:"version,omitempty"` // installed/pinned version (best-effort; winget path)
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
// EnableRestore opts into configuration restore after package installation and
// immediately before verification.
func RunApply(flags ApplyFlags) (interface{}, *envelope.Error) {
	runID := buildRunID("apply")
	emitter := newApplyEmitterFn(runID, flags.Events == "jsonl")

	// EnableRestore is handled after the install phase (before verify).

	// ----------------------------------------------------------------
	// Phase 1: Plan
	// ----------------------------------------------------------------

	mf, envelopeErr := loadManifest(flags.Manifest)
	if envelopeErr != nil {
		return nil, envelopeErr
	}
	repoRoot := resolveRepoRootFn()
	configRuntime, configInputErr := newConfigRestoreRuntime(configRestoreBuildRequest{
		Manifest:       mf,
		ManifestPath:   flags.Manifest,
		RepoRoot:       repoRoot,
		RestoreFilter:  flags.RestoreFilter,
		RestoreTargets: flags.RestoreTargets,
	})
	if configInputErr != nil {
		return nil, configInputErr
	}
	flags.configRestoreRuntime = configRuntime
	flags.configRestoreRepoRoot = repoRoot

	// Load the module catalog once; it is used for synthesis (pre-filter) and
	// for module matching (post-filter). Non-fatal if unavailable.
	var catalog map[string]*modules.Module
	if len(configRuntime.inputs.generationSources) > 0 {
		catalog = configRuntime.catalog.ModuleCatalog()
	} else if repoRoot != "" {
		if cat, catalogErr := loadModuleCatalogFn(repoRoot); catalogErr == nil && len(cat) > 0 {
			catalog = cat
		}
	}

	// Phase 1a: Synthesis — MUST run before --only so that module-derived manual
	// apps (pathExists synthesized entries) are part of the selectable/filterable
	// app set. validateOnly then sees the fully-expanded list.
	if catalog != nil {
		modules.SynthesizeAppsFromModules(mf, catalog)
	}

	// Phase 1b: --only filter — validate and filter against the fully-expanded
	// app list (including synthesized apps). validateOnly returns the filtered
	// []manifest.App directly — no second filterAppsByOnly call needed.
	if flags.Only != "" {
		filtered, onlyErr := validateOnly(flags, mf)
		if onlyErr != nil {
			return nil, onlyErr
		}
		mfCopy := *mf
		mfCopy.Apps = filtered
		mf = &mfCopy
	}

	// Phase 1c: Module matching — runs AFTER --only so configModuleMap and
	// restoreModulesAvailable are scoped to the filtered app set.
	var configModuleMap map[string]string
	var restoreModulesAvailable []RestoreModuleRef
	var matchedModules []*modules.Module
	if catalog != nil {
		matchedModules = modules.MatchModulesForApps(catalog, mf.Apps)
		if len(matchedModules) > 0 {
			configModuleMap = make(map[string]string, len(matchedModules))
			for _, mod := range matchedModules {
				if len(mod.Matches.Winget) > 0 {
					for _, wingetRef := range mod.Matches.Winget {
						configModuleMap[wingetRef] = mod.ID
					}
				} else {
					// pathExists-only modules: key by short app ID so the GUI can match.
					shortID := strings.TrimPrefix(mod.ID, "apps.")
					configModuleMap[shortID] = mod.ID
				}
				restoreModulesAvailable = append(restoreModulesAvailable, RestoreModuleRef{
					ID:          mod.ID,
					DisplayName: resolveModuleDisplayName(mod),
				})
			}
		}
	}
	if flags.Only != "" {
		scopeConfigRestoreRuntimeForOnly(configRuntime, matchedModules)
	}

	// Platform realizer path (whole-set, e.g. Nix on linux/darwin). When a
	// realizer backend is available, take the whole-set apply path that fans one
	// atomic generation switch into the per-item event stream. On Windows
	// newRealizerFn returns ErrNoRealizer, so control falls through to the winget
	// driver loop below, byte-identical to prior behavior.
	if rz, rerr := newRealizerFn(); rerr == nil {
		// Two-lane split AT THE REALIZER GATE (not selectBackend, which returns
		// ErrNoBackend on darwin). Partition AFTER SynthesizeAppsFromModules so
		// synthesized manual apps land in the realizer (rest) lane. The realizer
		// receives a shallow manifest copy carrying ONLY restApps — it never sees
		// a brew/`cask:` ref.
		brewApps, restApps := partitionBrewLane(mf.Apps)

		// Backend-bootstrap pre-step IN FRONT of the factory gate, with ONE combined
		// consent over every backend this run needs and lacks. The realizer (Nix) is
		// needed when there is realizer-lane work (restApps) or a home-manager config
		// stage; brew is needed when an app routes to the brew lane. apply is mutating,
		// so an absent+consented backend is installed via its official installer and
		// verified before use. A backend not bootstrappable on the host (brew off
		// darwin, anything on Windows) is filtered out by the pre-step and resolves via
		// the existing factory gate (which no-ops it) unchanged.
		nixNeeded := len(restApps) > 0 || configStageApplies(flags, mf)
		brewNeeded := len(brewApps) > 0
		needed := make([]bootstrap.Backend, 0, 2)
		if nixNeeded {
			needed = append(needed, bootstrap.BackendNix)
		}
		if brewNeeded {
			needed = append(needed, bootstrap.BackendBrew)
		}
		avail, berr := bootstrapBackendsFn(needed, true, bootstrapConsent(flags), emitter)
		if berr != nil {
			return nil, berr
		}

		// Brew driver resolves only when brew is needed AND available; absent+declined
		// (or a non-darwin host) leaves brewDrv nil → the existing visible-skip path.
		var brewDrv driver.Driver
		if brewNeeded && avail[bootstrap.BackendBrew] {
			if d, derr := newBrewDriverFn(); derr == nil {
				brewDrv = d
			}
		}

		// Route on realizer availability. When Nix is needed AND available, the
		// whole-set realizer path runs (byte-identical to today; brew interleaves).
		// When Nix is NOT needed, or is needed but unavailable (declined/failed), the
		// realizer lane is skipped and the brew lane runs standalone — so a declined
		// Nix with a consented brew still installs the brew apps, and the run never
		// aborts with a half-done apply.
		if nixNeeded && avail[bootstrap.BackendNix] {
			rzMf := *mf
			rzMf.Apps = restApps
			return runApplyRealizer(flags, &rzMf, rz, emitter, runID, configModuleMap, restoreModulesAvailable, brewApps, brewDrv)
		}
		return runApplyBrewOnly(flags, mf, restApps, brewApps, brewDrv, emitter, runID, configModuleMap, restoreModulesAvailable)
	}

	// Convergence (--prune) is realizer-only. The driver path (winget) operates on
	// the shared system, where removing undeclared packages is unsafe, so refuse
	// and change nothing.
	if flags.Prune {
		return nil, envelope.NewError(
			envelope.ErrConvergenceUnsupported,
			"convergence (--prune) is not supported on this backend").
			WithRemediation("Run on a host with the Nix realizer (Linux/macOS) to use --prune.")
	}

	d, derr := newDriverFn()
	if derr != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, derr.Error())
	}

	// First event in stream MUST be a phase event per event-contract.md.
	emitter.EmitPhase("plan")

	type appPlan struct {
		app         manifest.App
		ref         string
		isManual    bool
		action      ApplyAction
		displayName string
		repin       bool // --repin: installed-but-drifted from the declared version
	}

	// Batch-detect all winget apps in one call for performance.
	var wingetRefs []string
	for _, app := range mf.Apps {
		ref := resolveAppRef(app)
		if ref != "" {
			wingetRefs = append(wingetRefs, ref)
		}
	}

	var batchResults map[string]driver.DetectResult
	if bd, ok := d.(driver.BatchDetector); ok && len(wingetRefs) > 0 {
		batchResults, _ = bd.DetectBatch(wingetRefs)
		// Ignore error — fall back to per-ref Detect if batch fails.
	}

	var planEntries []appPlan
	presentCount := 0
	toInstallCount := 0

	for _, app := range mf.Apps {
		ref := resolveAppRef(app)
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
				emitter.EmitItem(app.ID, "manual", "present", driver.ReasonAlreadyInstalled, action.Message, app.DisplayName)
				presentCount++
			} else {
				action.Status = "to_install"
				action.Reason = "manual_required"
				action.Message = fmt.Sprintf("Not found at %s", expanded)
				action.Manual = app.Manual
				emitter.EmitItem(app.ID, "manual", "to_install", "manual_required", action.Message, app.DisplayName)
				toInstallCount++
			}

			planEntries = append(planEntries, appPlan{app: app, ref: "", isManual: true, action: action})
			continue
		}

		// Winget app: detect via driver (use batch results if available).
		var installed bool
		var displayName string
		var version string // best-effort installed version captured from the batch
		if br, ok := batchResults[ref]; ok {
			installed = br.Installed
			displayName = br.DisplayName
			version = br.Version
		} else {
			installed, displayName, _ = d.Detect(ref)
		}

		// Ensure a display name is always available for events and the envelope.
		itemName := resolveItemDisplayName(displayName, app, ref)

		var action ApplyAction
		action.ID = app.ID
		action.Ref = stringPtr(ref)
		action.Driver = d.Name()
		action.Name = itemName
		action.Version = version // captured installed version for present packages

		repin := false
		if installed {
			action.Status = "present"
			if flags.Repin && app.Version != "" && version != "" &&
				strings.TrimSpace(version) != strings.TrimSpace(app.Version) {
				// Declared version has drifted from the installed one: mark for
				// re-pin convergence (reinstalled in the apply loop, --confirm-gated).
				repin = true
				action.Reason = driver.ReasonVersionDrift
				action.Message = fmt.Sprintf("Version drift: installed %s, want %s", version, app.Version)
				emitter.EmitItem(ref, d.Name(), "present", driver.ReasonVersionDrift, action.Message, itemName)
			} else {
				action.Reason = driver.ReasonAlreadyInstalled
				emitter.EmitItem(ref, d.Name(), "present", driver.ReasonAlreadyInstalled, "Already installed", itemName)
			}
			presentCount++
		} else {
			action.Status = "to_install"
			action.Reason = driver.ReasonMissing
			emitter.EmitItem(ref, d.Name(), "to_install", driver.ReasonMissing, "Will be installed", itemName)
			toInstallCount++
		}

		planEntries = append(planEntries, appPlan{app: app, ref: ref, action: action, displayName: itemName, repin: repin})
	}

	totalApps := len(planEntries)
	emitter.EmitSummary("plan", totalApps, presentCount, 0, toInstallCount)

	configSession := newConfigRestoreExecutionSession(
		configRuntime,
		newDriverConfigRestoreEvidenceSource(d, mf.Apps),
	)
	if _, previewErr := configSession.Preview(context.Background()); previewErr != nil {
		return nil, configRestoreInternalError(previewErr.Error())
	}
	var configFields *ConfigResultFields
	if flags.DryRun {
		configExecution, executeErr := configSession.Execute(
			context.Background(),
			applyConfigRestoreExecutionOptions(flags, runID, repoRoot, emitter),
		)
		if executeErr != nil {
			return nil, executeErr
		}
		if configRuntime.inputs.hasConfigPayloads {
			configFields = NewConfigResultFields(configExecution.Plan.Sets, configExecution.RestoreItems)
		}
	}

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
					emitter.EmitItem(entry.app.ID, "manual", "skipped", "manual_required", finalActions[i].Message, entry.app.DisplayName)
					skippedCount++
				}
				continue
			}

			// Version convergence (--repin): a present app whose installed version
			// drifted from its declared App.Version. Reinstall the declared version
			// (force), gated by --confirm; without confirmation the drifted app is
			// left present and the run refuses after the apply phase.
			if entry.repin {
				vi, ok := d.(driver.VersionedInstaller)
				if !flags.Confirm || !ok {
					skippedCount++
					continue
				}
				emitter.EmitItem(entry.ref, d.Name(), "installing", "", fmt.Sprintf("Re-pinning %s to %s", entry.ref, entry.app.Version), entry.displayName)
				result, rerr := vi.ReinstallVersion(entry.ref, entry.app.Version)
				if rerr != nil {
					finalActions[i].Status = driver.StatusFailed
					finalActions[i].Reason = driver.ReasonInstallFailed
					emitter.EmitItem(entry.ref, d.Name(), "failed", driver.ReasonInstallFailed, rerr.Error(), entry.displayName)
					failedCount++
					continue
				}
				if result.Status == driver.StatusInstalled {
					finalActions[i].Status, finalActions[i].Reason = "installed", ""
					finalActions[i].Version = entry.app.Version // converged version is now committed
					emitter.EmitItem(entry.ref, d.Name(), "installed", "", result.Message, entry.displayName)
					successCount++
				} else {
					finalActions[i].Status, finalActions[i].Reason = result.Status, result.Reason
					emitter.EmitItem(entry.ref, d.Name(), result.Status, result.Reason, result.Message, entry.displayName)
					failedCount++
				}
				continue
			}

			if entry.action.Status != "to_install" {
				// Already present: counts as skipped in the apply phase.
				skippedCount++
				continue
			}

			emitter.EmitItem(entry.ref, d.Name(), "installing", "", fmt.Sprintf("Installing %s", entry.ref), entry.displayName)

			// Honor a declared version (pin-on-install) when the driver supports
			// versioned installation; otherwise install the latest, as before.
			pinned := entry.app.Version != ""
			var result *driver.InstallResult
			var installErr error
			if vi, ok := d.(driver.VersionedInstaller); ok && pinned {
				result, installErr = vi.InstallVersion(entry.ref, entry.app.Version)
			} else {
				result, installErr = d.Install(entry.ref)
			}
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
				if pinned {
					// The pinned version is now the committed version; record it
					// (winget exposes no version on install for the unpinned path).
					finalActions[i].Version = entry.app.Version
				}
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

		// Record a Provisioning Generation for the install stage (best-effort,
		// install-only). Written only when >=1 package was installed this run;
		// Partial when any attempted install failed. Never touches restore state.
		// Driver (winget) path: home-manager is realizer-only, so no config record.
		writeProvisioningGeneration(runID, d.Name(), finalActions, nil, "", failedCount > 0, nil)

		// Version convergence (--repin) is destructive (reinstall / possible
		// downgrade), so it requires --confirm. Refuse without it — the install
		// phase above stands; only the drifted re-pins were withheld. (--dry-run
		// previews and never reaches here.)
		if flags.Repin && !flags.Confirm {
			return nil, envelope.NewError(
				envelope.ErrInternalError,
				"version convergence (--repin) requires --confirm (it reinstalls drifted packages)").
				WithRemediation("Re-run with --repin --confirm, or use --repin --dry-run to preview.")
		}

		// ----------------------------------------------------------------
		// Phase 2b: Restore. Final target detection replaces the preview after
		// installation and immediately precedes configuration mutation.
		// ----------------------------------------------------------------

		if configRuntime.inputs.hasConfigPayloads || (flags.EnableRestore && len(mf.Restore) > 0) {
			emitter.EmitPhase("restore")
			configExecution, executeErr := configSession.Execute(
				context.Background(),
				applyConfigRestoreExecutionOptions(flags, runID, repoRoot, emitter),
			)
			if executeErr != nil {
				return nil, executeErr
			}
			emitConfigRestoreSummary(emitter, configExecution.Results)
			if configRuntime.inputs.hasConfigPayloads {
				configFields = NewConfigResultFields(configExecution.Plan.Sets, configExecution.RestoreItems)
			}
		}

		// ----------------------------------------------------------------
		// Phase 3: Verify  (fresh re-detection)
		// ----------------------------------------------------------------

		emitter.EmitPhase("verify")

		// Batch-detect for verify phase (fresh snapshot after installs).
		var verifyBatchResults map[string]driver.DetectResult
		if bd, ok := d.(driver.BatchDetector); ok {
			var verifyRefs []string
			for _, entry := range planEntries {
				if !entry.isManual && entry.ref != "" {
					verifyRefs = append(verifyRefs, entry.ref)
				}
			}
			if len(verifyRefs) > 0 {
				verifyBatchResults, _ = bd.DetectBatch(verifyRefs)
			}
		}

		verifyPass := 0
		verifyFail := 0

		for i, entry := range planEntries {
			if entry.isManual {
				// Manual app verify: re-check verifyPath.
				expanded, exists := checkVerifyPath(entry.app.Manual.VerifyPath)
				if exists {
					emitter.EmitItem(entry.app.ID, "manual", "present", "", fmt.Sprintf("Verified at %s", expanded), entry.app.DisplayName)
					verifyPass++
				} else {
					emitter.EmitItem(entry.app.ID, "manual", "failed", driver.ReasonMissing, fmt.Sprintf("Missing at %s", expanded), entry.app.DisplayName)
					verifyFail++
				}
				continue
			}

			var detected bool
			var verifyName string
			if br, ok := verifyBatchResults[entry.ref]; ok {
				detected = br.Installed
				verifyName = br.DisplayName
			} else {
				detected, verifyName, _ = d.Detect(entry.ref)
			}
			if detected {
				emitter.EmitItem(entry.ref, d.Name(), "present", "", "Verified installed", resolveItemDisplayName(verifyName, entry.app, entry.ref))
				if verifyName != "" {
					finalActions[i].Name = verifyName
				}
				verifyPass++
			} else {
				emitter.EmitItem(entry.ref, d.Name(), "failed", driver.ReasonMissing, "Missing after apply", resolveItemDisplayName(entry.displayName, entry.app, entry.ref))
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
		outSummary.Skipped = presentCount // already-present apps are effectively "skipped"
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
		ConfigResultFields:      configFields,
	}, nil
}

func scopeConfigRestoreRuntimeForOnly(runtime *configRestoreRuntime, matched []*modules.Module) {
	if runtime == nil {
		return
	}
	allowed := make(map[string]struct{}, len(matched))
	for _, module := range matched {
		if module != nil {
			allowed[module.ID] = struct{}{}
		}
	}
	for index := range runtime.inputs.generationSources {
		source := &runtime.inputs.generationSources[index]
		_, included := allowed[source.source.ModuleID]
		source.selected = source.selected && included
		if !source.selected {
			delete(runtime.inputs.targetMappings, source.source.CaptureID)
		}
	}
	for index := range runtime.inputs.legacyLanes {
		lane := &runtime.inputs.legacyLanes[index]
		_, included := allowed[lane.moduleID]
		lane.selected = lane.selected && included
	}
}

func applyConfigRestoreExecutionOptions(
	flags ApplyFlags,
	runID string,
	repoRoot string,
	emitter *events.Emitter,
) configRestoreExecutionOptions {
	manifestDir, _ := filepath.Abs(filepath.Dir(flags.Manifest))
	exportRoot := ""
	if flags.Export != "" {
		exportRoot, _ = filepath.Abs(flags.Export)
	}
	stateDir := state.StateDir()
	if repoRoot != "" {
		stateDir = filepath.Join(repoRoot, "state")
	}
	stateDir, _ = filepath.Abs(stateDir)
	logsDir := ""
	if repoRoot != "" {
		logsDir = filepath.Join(repoRoot, "logs")
	}
	options := configRestoreExecutionOptions{
		RestoreEnabled: flags.EnableRestore,
		DryRun:         flags.DryRun,
		RunID:          runID,
		StateDir:       stateDir,
		ManifestPath:   flags.Manifest,
		ManifestDir:    manifestDir,
		ExportRoot:     exportRoot,
		BackupDir:      filepath.Join(stateDir, "backups", runID),
		JournalLogsDir: logsDir,
	}
	options.Registry, options.ProcessObserver = newConfigRestorePlatformAdapters()
	options.Emitter = emitter
	return options
}

func emitConfigRestoreSummary(emitter *events.Emitter, results []restore.RestoreResult) {
	restored, skipped, failed := 0, 0, 0
	for _, result := range results {
		switch result.Status {
		case "restored":
			restored++
		case "skipped_up_to_date", "skipped_missing_source":
			skipped++
		case "failed":
			failed++
		}
	}
	emitter.EmitSummary("restore", len(results), restored, skipped, failed)
}
