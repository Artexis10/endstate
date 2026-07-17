// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
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
	// packages ("drift") from the engine-managed set. Realizer-only; per-package
	// drivers refuse with CONVERGENCE_UNSUPPORTED.
	Prune bool
	// Confirm authorizes the destructive prune. Without it, --prune refuses
	// (unless --dry-run, which only previews).
	Confirm bool
	// Repin enables version convergence: reinstall a declared App.Version over an
	// already-installed drifted version. Supported by version-aware package
	// drivers; requires --confirm (unless --dry-run). The Nix realizer ignores it.
	Repin bool
	// BootstrapBackends (--bootstrap-backends) authorizes the engine to install an
	// absent package backend (Nix, Homebrew, or Chocolatey where supported) via
	// its official installer. Consent for the backend-bootstrap pre-step.
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
	DryRun                  bool                `json:"dryRun"`
	Manifest                ApplyManifestRef    `json:"manifest"`
	Summary                 ApplySummary        `json:"summary"`
	Actions                 []ApplyAction       `json:"actions"`
	ConfigModuleMap         map[string]string   `json:"configModuleMap,omitempty"`
	PackageModuleMap        map[string][]string `json:"packageModuleMap,omitempty"`
	Warnings                []CommandWarning    `json:"warnings,omitempty"`
	RestoreModulesAvailable []RestoreModuleRef  `json:"restoreModulesAvailable,omitempty"`
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
	ID             string              `json:"id"`
	Ref            *string             `json:"ref"`
	Driver         string              `json:"driver"`
	Name           string              `json:"name,omitempty"`
	Status         string              `json:"status"`
	Reason         string              `json:"reason,omitempty"`
	Message        string              `json:"message,omitempty"`
	Version        string              `json:"version,omitempty"` // installed/pinned version (best-effort; backend-provided)
	RebootRequired bool                `json:"rebootRequired,omitempty"`
	Manual         *manifest.ManualApp `json:"manual"`
	// WasPresent distinguishes a version convergence of an existing package from
	// a newly added package. It is internal generation/rollback provenance only.
	WasPresent bool `json:"-"`
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
	var packageModuleMap map[string][]string
	var restoreModulesAvailable []RestoreModuleRef
	var matchedModules []*modules.Module
	if catalog != nil {
		matchedModules = modules.MatchModulesForApps(catalog, mf.Apps)
		if len(matchedModules) > 0 {
			packageModuleMap = buildPackageModuleMap(matchedModules)
			configModuleMap = make(map[string]string, len(matchedModules))
			for _, mod := range matchedModules {
				if len(mod.Matches.Winget) > 0 {
					for _, wingetRef := range mod.Matches.Winget {
						configModuleMap[wingetRef] = mod.ID
					}
				} else if len(mod.Matches.Chocolatey) == 0 {
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
		// Backend preflight can emit consent. Open the mandatory first phase here;
		// EmitPhase is idempotent, so the selected realizer helper may retain phase
		// ownership for direct-call tests without producing a duplicate.
		emitter.EmitPhase("plan")
		// Two-lane split AT THE REALIZER GATE (not selectBackend, which returns
		// ErrNoBackend on darwin). Partition AFTER SynthesizeAppsFromModules so
		// synthesized manual apps land in the realizer (rest) lane. The realizer
		// receives a shallow manifest copy carrying ONLY restApps — it never sees
		// a brew/`cask:` ref.
		brewApps, unsupportedApps, restApps := partitionRealizerLanes(mf.Apps)

		// Backend-bootstrap pre-step IN FRONT of the factory gate, with ONE combined
		// consent over every backend this run needs and lacks. The realizer (Nix) is
		// needed when there is realizer-lane work (restApps) or a home-manager config
		// stage; brew is needed when an app routes to the brew lane. A live apply is
		// mutating, so an absent+consented backend is installed via its official
		// installer and verified before use; dry-run only probes availability. A
		// backend not bootstrappable on the host (brew off
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
		avail, berr := bootstrapBackendsFn(needed, !flags.DryRun, bootstrapConsent(flags), emitter)
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
			return runApplyRealizer(flags, &rzMf, rz, emitter, runID, configModuleMap, packageModuleMap, restoreModulesAvailable, brewApps, brewDrv, unsupportedApps)
		}
		return runApplyBrewOnly(flags, mf, restApps, brewApps, brewDrv, emitter, runID, configModuleMap, packageModuleMap, restoreModulesAvailable, unsupportedApps)
	}

	return runApplyDriverLanes(flags, mf, emitter, runID, configModuleMap, packageModuleMap, restoreModulesAvailable)
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
