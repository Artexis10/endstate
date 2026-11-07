// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
	"github.com/Artexis10/endstate/go-engine/internal/state"
)

// realizerEntry is one planned manifest app on the realizer path.
type realizerEntry struct {
	app      manifest.App
	ins      realizer.Installable
	isNix    bool
	isManual bool
	name     string
}

// realizerItemName returns a stable display name for a realizer item.
func realizerItemName(app manifest.App) string {
	if app.DisplayName != "" {
		return app.DisplayName
	}
	return app.ID
}

// isSystemic reports whether a realizer error is a whole-run infrastructure
// failure that should surface as a top-level envelope error (truncating the
// per-item stream) rather than a per-item install failure.
func isSystemic(code envelope.ErrorCode) bool {
	return code == envelope.ErrRealizerUnavailable || code == envelope.ErrPermissionDenied
}

// realizerEnvelopeError builds the top-level envelope error for a systemic
// realizer failure. Raw Nix text lands ONLY in error.detail (the moat).
func realizerEnvelopeError(rerr *realizer.Error) *envelope.Error {
	msg := "The package backend is unavailable."
	rem := "Ensure the Nix daemon is running and 'nix' is on PATH."
	if rerr.Code == envelope.ErrPermissionDenied {
		msg = "Insufficient permissions to realize the package set."
		rem = "Check write permissions on the Endstate Nix profile directory."
	}
	return envelope.NewError(rerr.Code, msg).
		WithDetail(map[string]string{"subcode": rerr.Subcode, "stage": rerr.Stage, "raw": rerr.Raw}).
		WithRemediation(rem)
}

// leafAttr returns the trailing attribute of an installable ref (after the last
// '#', then the last '.'), for matching against installed element names.
func leafAttr(s string) string {
	if i := strings.LastIndex(s, "#"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// presentInSet reports whether ref's leaf attribute matches an installed element.
func presentInSet(set realizer.Set, ref string) bool {
	for _, element := range set.Elements {
		if element.ID != "" && element.RequestedRef == ref {
			return true
		}
	}
	leaf := leafAttr(ref)
	if leaf == "" {
		return false
	}
	if _, ok := set.Elements[leaf]; ok {
		return true
	}
	for _, e := range set.Elements {
		if e.ID == "" && (e.Name == leaf || leafAttr(e.AttrPath) == leaf) {
			return true
		}
	}
	return false
}

// computeDrift returns the element names in the current set that match no desired
// ref — the installed-but-undeclared packages to prune. It compares on leaf
// attributes (the inverse of presentInSet) so e.g. "nixpkgs#ripgrep" matches an
// element named "ripgrep".
func computeDrift(current realizer.Set, desired []realizer.Installable) []string {
	var drift []string
	for name, element := range current.Elements {
		keep := false
		for _, item := range desired {
			if element.ID != "" {
				keep = realizer.Matches(element, item)
			} else {
				leaf := leafAttr(item.Ref)
				keep = leaf != "" && (leafAttr(name) == leaf || leafAttr(element.AttrPath) == leaf)
			}
			if keep {
				break
			}
		}
		if !keep {
			drift = append(drift, name)
		}
	}
	sort.Strings(drift)
	return drift
}

// requirePruner returns the realizer's optional Pruner capability, or a
// CONVERGENCE_UNSUPPORTED envelope error when the backend cannot converge.
func requirePruner(r realizer.Realizer) (realizer.Pruner, *envelope.Error) {
	if p, ok := r.(realizer.Pruner); ok {
		return p, nil
	}
	return nil, envelope.NewError(
		envelope.ErrConvergenceUnsupported,
		"this backend does not support convergence (--prune)")
}

// resolveHomeFlake resolves the home-manager flakeref the config stage activates
// this apply. There are three mutually-exclusive inputs (enforced at load):
//   - homeManager.settings — a declarative Endstate catalog the engine compiles
//     into a home.nix, then wraps into a generated flake (generated=true);
//   - homeManager.config — a path to a home.nix the engine wraps (generated=true);
//   - homeManager.flake — a direct flakeref returned unchanged (#81; generated=false).
//
// Both generated inputs reuse the wrapper's GenerateHomeFlake* and the EXISTING
// ActivateHome — no new activation/recording path. File/config paths are resolved
// relative to the manifest dir. It returns an empty flakeref (nil error) when no
// config stage applies (no --enable-restore, or no home-manager input). A
// generation failure surfaces as INSTALL_FAILED with raw text confined to
// error.detail (the moat), mirroring an activation failure.
func resolveHomeFlake(flags ApplyFlags, mf *manifest.Manifest) (flake string, generated bool, eerr *envelope.Error) {
	if !flags.EnableRestore || mf.HomeManager == nil {
		return "", false, nil
	}
	switch {
	case mf.HomeManager.Settings != nil:
		ref, gerr := nix.GenerateHomeFlakeFromSettings(state.StateDir(), mf.HomeManager.Settings, filepath.Dir(flags.Manifest), mf.HomeManager.Secrets)
		if gerr != nil {
			return "", false, envelope.NewError(
				envelope.ErrInstallFailed, "Failed to generate the home-manager configuration from settings.").
				WithDetail(map[string]string{"raw": gerr.Error()}).
				WithRemediation("Check homeManager.settings: curated keys, the raw programs block, and file sources.")
		}
		return ref, true, nil
	case mf.HomeManager.Config != "":
		cfgPath := mf.HomeManager.Config
		if !filepath.IsAbs(cfgPath) {
			cfgPath = filepath.Join(filepath.Dir(flags.Manifest), cfgPath)
		}
		ref, gerr := nix.GenerateHomeFlake(state.StateDir(), cfgPath, mf.HomeManager.Secrets)
		if gerr != nil {
			return "", false, envelope.NewError(
				envelope.ErrInstallFailed, "Failed to generate the home-manager configuration flake.").
				WithDetail(map[string]string{"raw": gerr.Error()}).
				WithRemediation("Check that homeManager.config points to a readable home.nix.")
		}
		return ref, true, nil
	case mf.HomeManager.Flake != "":
		return mf.HomeManager.Flake, false, nil
	}
	return "", false, nil
}

type preparedHomeManager struct {
	flake                    string
	generated                bool
	ownershipAction          *HomeManagerOwnershipAction
	previousHome             *provision.HomeGenRef
	resolution               *realizer.InputResolution
	flatTargets              []string
	fileExpectations         *provision.HomeFileExpectations
	prepareCaptured          func(*planner.ConfigPlan, []preparedConfigRestoreExecution) []string
	capturedStages           []*migration.StageResult
	capturedFiles            map[string]*provision.HomeFileExpectations
	capturedPreparationError error
	forceBlock               planner.ResolutionReason
	// explicitProjected and definedOutside come from the explicit configuration's
	// own preparation, never from the combined one: the combined module defines
	// the typed programs itself.
	explicitProjected   bool
	definedOutside      []string
	activationAttempted bool
	leafReads           map[*realizer.InputResolution]builtHomeLeavesRead
	leafError           error
	// checkIncomplete is set when evaluation alone cannot list every target:
	// a recursive declaration, or no parent configuration metadata.
	checkIncomplete bool
}

func prepareHomeManager(flags ApplyFlags, mf *manifest.Manifest, r realizer.Realizer) (*preparedHomeManager, *envelope.Error) {
	if _, ok := r.(realizer.HomeActivator); !ok {
		return nil, nil
	}
	flake, generated, err := resolveHomeFlake(flags, mf)
	if err != nil || flake == "" {
		return nil, err
	}
	home := &preparedHomeManager{flake: flake, generated: generated}
	if ownershipErr := home.observeOwnership(r); ownershipErr != nil {
		return nil, homeOwnershipError(ownershipErr)
	}
	if preparer, ok := r.(realizer.HomePreparer); ok && home.ownershipAction == nil {
		preparation, prepareErr := preparer.PrepareHome(flake, !flags.DryRun)
		if prepareErr != nil {
			return nil, inputResolutionError(prepareErr)
		}
		home.adoptPreparation(preparation)
		home.explicitProjected, home.definedOutside = true, preparation.DefinedOutside
		if !flags.DryRun && mf.HomeManager.Settings != nil {
			// Missing evidence would make a new generated activation unverifiable.
			// Refuse before package mutation; the caller can repair and retry apply.
			home.fileExpectations, prepareErr = readHomeFileExpectations(preparation.Resolution)
			if prepareErr != nil {
				return nil, inputResolutionError(prepareErr)
			}
		}
	}
	return home, nil
}

func (home *preparedHomeManager) adoptPreparation(preparation *realizer.HomePreparation) {
	home.resolution, home.flatTargets = preparation.Resolution, preparation.FlatTargets
	home.checkIncomplete = preparation.FlatTargets == nil || preparation.RecursiveDeclarations
}

func (home *preparedHomeManager) observeOwnership(r realizer.Realizer) error {
	action, previous, err := externalHomeManagerAction(r, home.flake, home.generated)
	if err != nil {
		if home.generated {
			return err
		}
		// A directly declared flake already has explicit activation consent.
		// Keep any known historical completion evidence without requiring ownership.
		return nil
	}
	home.ownershipAction, home.previousHome = action, previous
	return nil
}

func externalHomeManagerAction(r realizer.Realizer, flake string, generated bool) (*HomeManagerOwnershipAction, *provision.HomeGenRef, error) {
	reader, ok := r.(realizer.HomeObserver)
	if !ok {
		return nil, nil, fmt.Errorf("the backend cannot observe Home Manager ownership")
	}
	active, err := reader.ObserveHome()
	if err != nil {
		return nil, nil, err
	}
	if !active.Present {
		return nil, nil, nil
	}
	generations, err := listGenerationsFn()
	if err != nil {
		return nil, nil, err
	}
	if matching, outputHistory := matchingHomeManagerReference(generations, active.ActivationOutput, 0); outputHistory {
		if matching != nil {
			return nil, matching, nil
		}
	} else {
		for _, generation := range generations {
			if generation != nil && homeManagerOwnsActive(generation.HomeManager, active) {
				return nil, generation.HomeManager, nil
			}
		}
	}
	if !generated {
		return nil, nil, nil
	}
	return &HomeManagerOwnershipAction{
		Owner: "external", Action: "import_generated_module", Required: true,
		ActiveGeneration: active.Generation, ModulePath: generatedHomeModulePath(flake),
		Message: "An existing Home Manager configuration owns this home. Endstate generated an importable module and left the active configuration unchanged.",
	}, nil, nil
}

func homeOwnershipError(err error) *envelope.Error {
	code := realizer.ErrInputProvenanceUnresolved
	raw := err.Error()
	stage := "eval"
	if failure, ok := err.(*realizer.Error); ok {
		raw = failure.Raw
		if failure.Stage != "" {
			stage = failure.Stage
		}
		if isSystemic(failure.Code) {
			code = failure.Code
		}
	}
	if stage == "commit" {
		return envelope.NewError(code, "Home Manager activation ran, but its native identity could not be verified.").
			WithDetail(map[string]interface{}{"subcode": "home-ownership", "stage": stage, "raw": raw, "committed": true})
	}
	return envelope.NewError(code, "Home Manager ownership could not be established; settings activation stopped.").
		WithDetail(map[string]string{"subcode": "home-ownership", "stage": stage, "raw": raw})
}

func generatedHomeModulePath(flake string) string {
	location, _, _ := strings.Cut(flake, "#")
	location = strings.TrimPrefix(location, "path:")
	return filepath.Join(filepath.FromSlash(location), nix.HomeImportModule)
}

// runApplyRealizer is the whole-set apply path for a realizer backend (Nix). It
// computes one plan over the declared package set, performs ONE atomic
// generation switch, and fans the single result back into the per-item event
// stream so the event contract is preserved. Package install ONLY — config and
// verify stages keep their own concerns. Raw backend text is confined to
// error.detail.
func runApplyRealizer(flags ApplyFlags, mf *manifest.Manifest, r realizer.Realizer, emitter *events.Emitter, runID string, configModuleMap map[string]string, packageModuleMap map[string][]string, restoreScope *restoreModuleScope, brewApps []manifest.App, brewDrv driver.Driver, unsupportedApps ...[]manifest.App) (result interface{}, resultErr *envelope.Error) {
	driverName := r.Name()
	preparationCommitted := false
	if finalizer, ok := r.(realizer.PreparationFinalizer); ok {
		defer func() { finalizer.FinalizePreparation(preparationCommitted) }()
	}

	// Brew lane (driver:"brew" apps) runs interleaved INSIDE the realizer's
	// plan/apply/verify phases — one event stream, one summary per phase. Counts
	// from each brew phase merge into the matching realizer EmitSummary below.
	// brewActions are appended to the realizer actions and recorded in a SEPARATE
	// Backend:"brew" provisioning generation (after the nix one). nil/empty
	// brewApps ⇒ the lane no-ops.
	brew := newBrewLane(brewDrv, emitter, brewApps)

	// --- Phase 1: Plan ---
	emitter.EmitPhase("plan")

	var entries []realizerEntry
	var desired []realizer.Installable
	names := map[string]string{}
	for _, app := range mf.Apps {
		ref := app.Refs[runtime.GOOS] // STRICT: no fallback to a non-host (e.g. winget) ref
		name := realizerItemName(app)
		names[app.ID] = name
		switch {
		case ref != "":
			ins := realizer.Installable{ID: app.ID, Ref: ref}
			entries = append(entries, realizerEntry{app: app, ins: ins, isNix: true, name: name})
			desired = append(desired, ins)
		case app.Manual != nil && app.Manual.VerifyPath != "":
			entries = append(entries, realizerEntry{app: app, isManual: true, name: name})
		default:
			// No host package ref and not a manual app: skip (never pass a
			// foreign ref to the realizer).
			emitter.EmitItem(app.ID, driverName, "skipped", "filtered", "No linux/darwin package ref", name)
		}
	}

	if resolver, ok := r.(realizer.InputResolver); ok {
		var err error
		desired, err = resolver.Prepare(desired)
		if err != nil {
			return nil, inputResolutionError(err)
		}
		for i := range entries {
			for _, item := range desired {
				if item.ID == entries[i].ins.ID {
					entries[i].ins = item
					break
				}
			}
		}
	}
	diff, planErr := r.Plan(desired)
	if planErr != nil {
		if rerr, ok := planErr.(*realizer.Error); ok && isSystemic(rerr.Code) {
			return nil, realizerEnvelopeError(rerr)
		}
		return nil, envelope.NewError(envelope.ErrInternalError, "Failed to plan the package set.")
	}

	// Read the current set once so we can extract installed versions for
	// already-present packages (best-effort; ignored on error).
	curSet, _ := r.Current()

	present := map[string]bool{} // app.ID -> already present
	for _, ins := range diff.Present {
		present[ins.ID] = true
	}

	actions := make([]ApplyAction, 0, len(entries))
	idx := map[string]int{}
	presentCount, toInstallCount := 0, 0
	for _, e := range entries {
		switch {
		case e.isManual:
			expanded, exists, verifyPathErr := checkVerifyPathWithValidation(e.app.Manual.VerifyPath, currentValidationMode)
			if verifyPathErr != nil {
				return nil, validationRuntimeIsolationFailure("apps.manual.verifyPath", "manual-verify", verifyPathErr)
			}
			a := ApplyAction{ID: e.app.ID, Driver: "manual", Name: e.name}
			if exists {
				a.Status, a.Reason, a.Message = "present", "already_installed", fmt.Sprintf("Verified at %s", expanded)
				emitter.EmitItem(e.app.ID, "manual", "present", "already_installed", a.Message, e.name)
				presentCount++
			} else {
				a.Status, a.Reason, a.Message = "to_install", "manual_required", fmt.Sprintf("Not found at %s", expanded)
				a.Manual = e.app.Manual
				emitter.EmitItem(e.app.ID, "manual", "to_install", "manual_required", a.Message, e.name)
				toInstallCount++
			}
			idx[e.app.ID] = len(actions)
			actions = append(actions, a)
		case e.isNix:
			ref := e.ins.Ref
			a := ApplyAction{ID: e.app.ID, Ref: stringPtr(ref), Driver: driverName, Name: e.name, Resolution: e.ins.Resolution}
			if present[e.app.ID] {
				a.Status, a.Reason = "present", "already_installed"
				// Best-effort: parse the installed version from the current set.
				if el, ok := installedElement(curSet, e.ins); ok {
					a.Resolution = el.Resolution
					a.Version = nix.StorePathVersion(el.Name, el.StorePaths)
				}
				emitter.EmitItem(ref, driverName, "present", "already_installed", "Already installed", e.name)
				presentCount++
			} else {
				a.Status, a.Reason = "to_install", "missing"
				emitter.EmitItem(ref, driverName, "to_install", "missing", "Will be installed", e.name)
				toInstallCount++
			}
			idx[e.app.ID] = len(actions)
			actions = append(actions, a)
		}
	}

	// Interleave the brew plan items into THIS plan phase, then fold the brew
	// counts into the single plan summary (one summary per phase).
	brewPresent, brewToInstall := brew.planBrew()
	brewPlanSkipped := len(brew.brewActions()) - brewPresent - brewToInstall
	unsupportedActions := planUnsupportedDriverApply(firstAppSlice(unsupportedApps), emitter)
	totalApps := len(actions) + len(brew.brewActions()) + len(unsupportedActions)
	emitter.EmitSummary("plan", totalApps, presentCount+brewPresent, brewPlanSkipped+len(unsupportedActions), toInstallCount+brewToInstall)
	configSession, configSessionErr := prepareApplyConfigRestore(
		context.Background(),
		flags,
		newRealizerConfigRestoreEvidenceSourceWithBrewError(
			r,
			brewDrv,
			flags.configRestoreBrewErr,
			append(append(append([]manifest.App{}, mf.Apps...), brewApps...), firstAppSlice(unsupportedApps)...),
		),
	)
	if configSessionErr != nil {
		return nil, configSessionErr
	}
	var configFields *ConfigResultFields
	var homeBlocked planner.ResolutionReason
	preparedHome, homePreparationErr := prepareApplyHomeManager(flags, mf, r, configSession)
	if homePreparationErr != nil {
		return nil, homePreparationErr
	}

	defer closeCapturedHomeStages(preparedHome)
	if flags.DryRun {
		var configErr *envelope.Error
		configFields, homeBlocked, configErr = executePreparedApplyConfigRestore(
			context.Background(), flags, runID, emitter, configSession, preparedHome,
		)
		var previewWarnings []CommandWarning
		if preparedHome != nil && preparedHome.ownershipAction == nil && preparedHome.checkIncomplete {
			previewWarnings = append(previewWarnings, homeManagerCheckIncompleteWarning())
		}
		if configErr != nil {
			dryActions := append(append([]ApplyAction{}, actions...), brew.brewActions()...)
			dryActions = append(dryActions, unsupportedActions...)
			return &ApplyResult{
				DryRun: true, Manifest: ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
				Summary:         ApplySummary{Total: totalApps, Skipped: presentCount + brewPresent + brewPlanSkipped + len(unsupportedActions)},
				Actions:         dryActions,
				ConfigModuleMap: configModuleMap, PackageModuleMap: packageModuleMap, RestoreModulesAvailable: restoreScope.modules(),
				Warnings: previewWarnings, ConfigResultFields: configFields,
			}, configErr
		}
		// --prune --dry-run previews the prune set without mutating anything and
		// without requiring --confirm.
		var pruned []string
		if flags.Prune {
			if _, perr := requirePruner(r); perr != nil {
				return nil, perr
			}
			cur, _ := r.Current()
			pruned = computeDrift(cur, desired)
		}

		// Config stage preview (home-manager, realizer-only): generate the
		// inspectable flake and REVEAL what would activate; activate nothing.
		var hmPreview *ApplyHomeManager
		if preparedHome != nil && preparedHome.flake != "" {
			emitter.EmitPhase("config")
			if preparedHome.ownershipAction != nil {
				emitter.EmitItem(preparedHome.flake, driverName, "action_required", "external_home_manager", preparedHome.ownershipAction.Message, "home-manager")
			} else if homeBlocked != "" {
				emitter.EmitItem(preparedHome.flake, driverName, "failed", homeBlocked.String(), homeManagerBlockMessage(homeBlocked), "home-manager")
			} else {
				emitter.EmitItem(preparedHome.flake, driverName, "would_configure", "dry_run", "Would activate home-manager configuration", "home-manager")
			}
			hmPreview = &ApplyHomeManager{
				Flake: preparedHome.flake, Generated: preparedHome.generated, Activated: false,
				OwnershipAction: preparedHome.ownershipAction,
			}
		}

		// Append the brew plan actions; brew present apps add to the skipped count.
		dryActions := append(append([]ApplyAction{}, actions...), brew.brewActions()...)
		dryActions = append(dryActions, unsupportedActions...)
		return &ApplyResult{
			DryRun:                  true,
			Manifest:                ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
			Summary:                 ApplySummary{Total: totalApps, Skipped: presentCount + brewPresent + brewPlanSkipped + len(unsupportedActions)},
			Actions:                 dryActions,
			ConfigModuleMap:         configModuleMap,
			PackageModuleMap:        packageModuleMap,
			RestoreModulesAvailable: restoreScope.modules(),
			Pruned:                  pruned,
			HomeManager:             hmPreview,
			Warnings:                previewWarnings,
			ConfigResultFields:      configFields,
		}, homeManagerBlockError(homeBlocked, preparedHome.blockDiagnostic())
	}

	// --- Phase 2: Apply (one atomic generation switch over diff.ToAdd) ---
	emitter.EmitPhase("apply")
	successCount, skippedCount, failedCount := 0, 0, 0

	// Manual apps: present -> success; otherwise skipped (manual_required).
	for _, e := range entries {
		if !e.isManual {
			continue
		}
		if actions[idx[e.app.ID]].Status == "present" {
			successCount++
		} else {
			actions[idx[e.app.ID]].Status = "skipped"
			actions[idx[e.app.ID]].Reason = "manual_required"
			emitter.EmitItem(e.app.ID, "manual", "skipped", "manual_required", actions[idx[e.app.ID]].Message, e.name)
			skippedCount++
		}
	}

	// Track whether each mutating phase advanced a generation and to which
	// number, so the single Provisioning Generation below records the converged
	// set with the final advancing op's native generation.
	installAdvanced, pruneAdvanced := false, false
	installGen, pruneGen := 0, 0
	var removed []string
	var homeRef *provision.HomeGenRef
	brewFailed := 0
	defer func() {
		native := ""
		if installAdvanced {
			native = fmt.Sprint(installGen)
		}
		if pruneAdvanced {
			native = fmt.Sprint(pruneGen)
		}

		if cur, err := r.Current(); err == nil {
			// Missing inventory provenance must not erase an observed realization.
			for i := range actions {
				if el, ok := installedElement(cur, realizer.Installable{ID: actions[i].ID, Ref: derefRef(actions[i].Ref)}); ok && el.Resolution != nil {
					actions[i].Resolution = el.Resolution
				}
			}
		}
		activated := homeRef != nil && !homeRef.ActivationFailed
		if preparedHome != nil && preparedHome.prepareCaptured != nil {
			fields, contentErr := finalizeCapturedHomeManager(configSession, preparedHome, activated, homeBlocked, emitter)
			if fields != nil {
				configFields = fields
			}
			if contentErr != nil {
				resultErr = contentErr
				if homeRef != nil {
					homeRef.ActivationFailed = true
				}
			}
			if current, ok := result.(*ApplyResult); ok {
				current.ConfigResultFields = configFields
			} else if resultErr != nil && configSession.lastExecution != nil {
				// The summary counts brew and unsupported apps, so the list names them too.
				lateActions := append(append(append([]ApplyAction{}, actions...), brew.brewActions()...), unsupportedActions...)
				result = &ApplyResult{
					Manifest: ApplyManifestRef{Path: flags.Manifest, Name: mf.Name}, Actions: lateActions,
					Summary:         ApplySummary{Total: totalApps, Success: successCount, Skipped: skippedCount, Failed: failedCount},
					ConfigModuleMap: configModuleMap, PackageModuleMap: packageModuleMap, RestoreModulesAvailable: restoreScope.modules(),
					ConfigResultFields: configFields,
				}
			}
		}
		var persistErr error
		if installAdvanced || pruneAdvanced || homeRef != nil {
			persistErr = writeProvisioningGeneration(runID, driverName, actions, removed, native, failedCount > 0 || resultErr != nil, homeRef)
		}
		if err := writeProvisioningGeneration(runID, "brew", brew.brewActions(), nil, "", brewFailed > 0 || resultErr != nil, nil); err != nil && persistErr == nil {
			persistErr = err
		}
		if persistErr != nil {
			resultErr = provenanceWriteError(persistErr)
		}
	}()
	if len(diff.ToAdd) > 0 {
		for _, ins := range diff.ToAdd {
			emitter.EmitItem(ins.Ref, driverName, "installing", "", fmt.Sprintf("Installing %s", ins.Ref), names[ins.ID])
		}
		res, realizeErr := r.Realize(diff.ToAdd)
		if realizeErr != nil {
			return nil, inputResolutionError(realizeErr)
		}
		if res.Advanced {
			preparationCommitted = true
			installAdvanced = true
			installGen = res.ToGeneration
		}
		if res.Err != nil && res.Advanced {
			for _, ins := range diff.ToAdd {
				if i, ok := idx[ins.ID]; ok {
					actions[i].Status = "installed"
				}
			}
			return nil, inputResolutionError(res.Err)
		}
		if res.Err != nil {
			if isSystemic(res.Err.Code) {
				// Whole-run infra failure: top-level error, stream truncates here.
				return nil, realizerEnvelopeError(res.Err)
			}
			// Atomic install failure: nothing was installed; every to-add fails.
			msg := "Install failed"
			if res.Err.Subcode != "" {
				msg = fmt.Sprintf("Install failed (%s)", res.Err.Subcode)
			}
			for _, ins := range diff.ToAdd {
				emitter.EmitItem(ins.Ref, driverName, "failed", "install_failed", msg, names[ins.ID])
				if i, ok := idx[ins.ID]; ok {
					actions[i].Status, actions[i].Reason, actions[i].Message = "failed", "install_failed", msg
				}
				failedCount++
			}
		} else {
			for _, ins := range diff.ToAdd {
				emitter.EmitItem(ins.Ref, driverName, "installed", "", "Installed", names[ins.ID])
				if i, ok := idx[ins.ID]; ok {
					actions[i].Status, actions[i].Reason = "installed", ""
					// Best-effort: parse the installed version from the post-realize set.
					if el, ok := installedElement(res.After, ins); ok {
						actions[i].Resolution = el.Resolution
						actions[i].Version = nix.StorePathVersion(el.Name, el.StorePaths)
					}
				}
				successCount++
			}
		}
	}

	// --- Phase 2c: Convergence (prune) ---
	// After install, optionally remove installed-but-undeclared drift from the
	// engine-managed set. Realizer-only and gated behind --confirm. Element names
	// removed this run are recorded in the Provisioning Generation below.
	if flags.Prune {
		pruner, perr := requirePruner(r)
		if perr != nil {
			return nil, perr
		}
		if !flags.Confirm {
			// Refuse the destructive prune; the install phase results stand.
			return nil, envelope.NewError(
				envelope.ErrInternalError,
				"convergence (--prune) requires --confirm (it uninstalls undeclared packages)").
				WithRemediation("Re-run with --prune --confirm, or use --prune --dry-run to preview.")
		}
		cur, _ := r.Current()
		if drift := computeDrift(cur, desired); len(drift) > 0 {
			pres, removeErr := pruner.Remove(drift)
			if removeErr != nil {
				return nil, inputResolutionError(removeErr)
			}
			if pres.Advanced {
				preparationCommitted = true
				removed = drift
				pruneAdvanced = true
				pruneGen = pres.ToGeneration
			}
			if pres.Err != nil {
				if isSystemic(pres.Err.Code) {
					return nil, realizerEnvelopeError(pres.Err)
				}
				return nil, envelope.NewError(envelope.ErrInstallFailed, "Convergence (prune) failed.").
					WithDetail(map[string]string{"subcode": pres.Err.Subcode, "stage": pres.Err.Stage, "raw": pres.Err.Raw})
			}
		}
	}

	// Already-present nix packages count as skipped in the apply phase.
	skippedCount += len(diff.Present)

	// Brew install lane — runs AFTER the nix generation committed, interleaving
	// its install items into THIS apply phase. A brew failure is per-item and
	// never rolls back or aborts the nix generation. Counts fold into the single
	// apply summary.
	brewInstalled, brewSkipped, brewFailed := brew.applyBrew()
	successCount += brewInstalled
	skippedCount += brewSkipped
	failedCount += brewFailed
	skippedCount += len(unsupportedActions)

	emitter.EmitSummary("apply", successCount+skippedCount+failedCount, successCount, skippedCount, failedCount)
	var configErr *envelope.Error
	configFields, homeBlocked, configErr = executePreparedApplyConfigRestore(
		context.Background(), flags, runID, emitter, configSession, preparedHome,
	)
	if configErr != nil {

		brewActions := brew.brewActions()
		resultActions := append(append([]ApplyAction{}, actions...), brewActions...)
		resultActions = append(resultActions, unsupportedActions...)
		return &ApplyResult{
			DryRun: false, Manifest: ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
			Summary:         ApplySummary{Total: totalApps, Success: successCount, Skipped: skippedCount, Failed: failedCount},
			Actions:         resultActions,
			ConfigModuleMap: configModuleMap, PackageModuleMap: packageModuleMap, RestoreModulesAvailable: restoreScope.modules(),
			Pruned: removed, ConfigResultFields: configFields,
		}, configErr
	}

	// --- Phase 3: Verify (read current generation) ---
	emitter.EmitPhase("verify")
	cur, _ := r.Current()
	verifyPass, verifyFail := 0, 0
	for _, e := range entries {
		switch {
		case e.isManual:
			expanded, exists, verifyPathErr := checkVerifyPathWithValidation(e.app.Manual.VerifyPath, currentValidationMode)
			if verifyPathErr != nil {
				return nil, validationRuntimeIsolationFailure("apps.manual.verifyPath", "manual-verify", verifyPathErr)
			}
			if exists {
				emitter.EmitItem(e.app.ID, "manual", "present", "", fmt.Sprintf("Verified at %s", expanded), e.name)
				verifyPass++
			} else {
				emitter.EmitItem(e.app.ID, "manual", "failed", "missing", fmt.Sprintf("Missing at %s", expanded), e.name)
				verifyFail++
			}
		case e.isNix:
			if _, present := installedElement(cur, e.ins); present {
				emitter.EmitItem(e.ins.Ref, driverName, "present", "", "Verified installed", e.name)
				verifyPass++
			} else {
				emitter.EmitItem(e.ins.Ref, driverName, "failed", "missing", "Missing after apply", e.name)
				verifyFail++
			}
		}
	}
	// Brew verify lane — re-detect presence, interleaved into THIS verify phase.
	brewPass, brewVerifyFail, brewVerifySkip := brew.verifyBrew()
	verifyPass += brewPass
	verifyFail += brewVerifyFail
	verifyUnsupportedDrivers(firstAppSlice(unsupportedApps), emitter)
	verifySkipped := brewVerifySkip + len(unsupportedActions)

	emitter.EmitSummary("verify", verifyPass+verifyFail+verifySkipped, verifyPass, verifySkipped, verifyFail)

	// --- Phase 4: Config (home-manager) ---
	// Realizer-only config stage, gated by --enable-restore AND a declared
	// home-manager input — either a direct homeManager.flake (#81) or a
	// homeManager.config the engine wraps into a generated flake (resolveHomeFlake).
	// The engine owns the home-manager invocation (the user installs nothing). A
	// backend without home-manager support simply does not implement HomeActivator
	// and this stage no-ops; the winget/driver path never reaches here. On failure
	// the stage mirrors the prune precedent: a systemic failure becomes a top-level
	// envelope error, anything else INSTALL_FAILED with raw text confined to
	// error.detail (the moat).
	var homeResult *ApplyHomeManager
	if activator, ok := r.(realizer.HomeActivator); ok && preparedHome != nil && preparedHome.flake != "" {
		flake, generated := preparedHome.flake, preparedHome.generated
		if preparedHome.ownershipAction == nil {
			if ownershipErr := preparedHome.observeOwnership(r); ownershipErr != nil {
				return nil, homeOwnershipError(ownershipErr)
			}
		}
		if preparedHome.ownershipAction != nil {
			emitter.EmitPhase("config")
			emitter.EmitItem(flake, driverName, "action_required", "external_home_manager", preparedHome.ownershipAction.Message, "home-manager")
			emitter.EmitSummary("config", 1, 0, 1, 0)
			homeResult = &ApplyHomeManager{
				Flake: flake, Generated: generated, Activated: false,
				OwnershipAction: preparedHome.ownershipAction,
			}
		} else if homeBlocked != "" {
			emitter.EmitPhase("config")
			emitter.EmitItem(flake, driverName, "failed", homeBlocked.String(), homeManagerBlockMessage(homeBlocked), "home-manager")
			emitter.EmitSummary("config", 1, 0, 0, 1)
			homeResult = &ApplyHomeManager{Flake: flake, Generated: generated, Activated: false}
		} else {
			emitter.EmitPhase("config")
			emitter.EmitItem(flake, driverName, "configuring", "", "Activating home-manager configuration", "home-manager")
			preparedHome.activationAttempted = true
			var hmGen int
			var aerr error
			if preparer, ok := r.(realizer.HomePreparer); ok && preparedHome.resolution != nil {
				hmGen, aerr = preparer.ActivatePreparedHome(preparedHome.resolution)
			} else {
				hmGen, aerr = activator.ActivateHome(flake)
			}
			if hmGen > 0 {
				preparationCommitted = true
				homeRef = homeGenerationReference(flags, mf, preparedHome, hmGen)
				homeRef.ActivationFailed = aerr != nil
				if failure, ok := aerr.(*realizer.Error); ok && failure.Stage == "activation" {
					homeRef = preserveHomeManagerRetryIdentity(homeRef)
				}
			}
			if aerr != nil {
				rerr, ok := aerr.(*realizer.Error)
				if !ok || rerr == nil {
					rerr = &realizer.Error{Code: envelope.ErrInstallFailed, Raw: aerr.Error()}
				}
				if rerr.Stage == "commit" {
					preparationCommitted = true
					if homeRef == nil && preparedHome.previousHome != nil && preparedHome.previousHome.Generation > 0 {
						// Invalidate historical completion without claiming that its
						// generation is the unobserved post-state.
						copyRef := *preparedHome.previousHome
						copyRef.ActivationFailed = true
						homeRef = &copyRef
					}
				}
				emitter.EmitItem(flake, driverName, "failed", "install_failed", "Home-manager configuration activation failed", "home-manager")
				emitter.EmitSummary("config", 1, 0, 0, 1)
				if rerr.Subcode == "home-ownership" {
					return nil, homeOwnershipError(rerr)
				}
				if isSystemic(rerr.Code) {
					return nil, realizerEnvelopeError(rerr)
				}
				if rerr.Code == realizer.ErrInputProvenanceUnresolved {
					return nil, inputResolutionError(rerr)
				}
				return nil, envelope.NewError(envelope.ErrInstallFailed, "Home-manager configuration activation failed.").
					WithDetail(map[string]string{"subcode": rerr.Subcode, "stage": rerr.Stage, "raw": rerr.Raw})
			}
			homeResult = &ApplyHomeManager{Flake: flake, Generated: generated, Activated: true}
			emitter.EmitItem(flake, driverName, "configured", "", fmt.Sprintf("Activated home-manager generation %d", hmGen), "home-manager")
			emitter.EmitSummary("config", 1, 1, 0, 0)
		}
	}

	// Finalization records backend changes together, including later failures.

	brewActions := brew.brewActions()

	// Append the brew actions to the realizer actions for the result envelope.
	actions = append(actions, brewActions...)
	actions = append(actions, unsupportedActions...)

	return &ApplyResult{
		DryRun:                  false,
		Manifest:                ApplyManifestRef{Path: flags.Manifest, Name: mf.Name},
		Summary:                 ApplySummary{Total: totalApps, Success: successCount, Skipped: skippedCount, Failed: failedCount},
		Actions:                 actions,
		ConfigModuleMap:         configModuleMap,
		PackageModuleMap:        packageModuleMap,
		RestoreModulesAvailable: restoreScope.modules(),
		Pruned:                  removed,
		HomeManager:             homeResult,
		ConfigResultFields:      configFields,
	}, homeManagerBlockError(homeBlocked, preparedHome.blockDiagnostic())
}

// homeManagerBlockMessage and homeManagerBlockError report why the selected Home
// Manager activation did not run: a known overlap, or a built tree that could not
// be checked. Both name the stable reason; the raw read error stays in error.detail.
func homeManagerBlockMessage(reason planner.ResolutionReason) string {
	if reason == planner.ReasonTargetDetectionFailed {
		return "Home-manager targets could not be checked against another selected settings writer"
	}
	return "Home-manager targets overlap another selected settings writer"
}

func homeManagerBlockError(reason planner.ResolutionReason, diagnostic string) *envelope.Error {
	if reason == "" {
		return nil
	}
	message := "Selected settings writers have overlapping targets."
	detail := map[string]string{"reason": reason.String()}
	if reason == planner.ReasonTargetDetectionFailed {
		message = "The Home Manager activation could not be checked against other selected settings writers."
		if diagnostic != "" {
			detail["diagnostic"] = diagnostic
		}
	}
	return envelope.NewError(envelope.ErrRestoreFailed, message).WithDetail(detail)
}

func inputResolutionError(err error) *envelope.Error {
	code := realizer.ErrInputProvenanceUnresolved
	detail := map[string]string{"raw": err.Error()}
	if rerr, ok := err.(*realizer.Error); ok {
		code = rerr.Code
		detail = map[string]string{"raw": rerr.Raw, "stage": rerr.Stage, "subcode": rerr.Subcode}
		if rerr.Stage == "commit" {
			return envelope.NewError(code, "State committed, but its native identity could not be verified.").WithDetail(map[string]interface{}{"committed": true, "raw": rerr.Raw})
		}
	}
	return envelope.NewError(code, "The requested input could not be frozen before mutation.").WithDetail(detail)
}

func installedElement(set realizer.Set, item realizer.Installable) (realizer.Element, bool) {
	for _, element := range set.Elements {
		if realizer.Matches(element, item) {
			return element, true
		}
	}
	for key, element := range set.Elements {
		if element.ID == "" && (key == leafAttr(item.Ref) || leafAttr(element.AttrPath) == leafAttr(item.Ref)) {
			return element, true
		}
	}
	return realizer.Element{}, false
}

func homeManagerOwnsActive(home *provision.HomeGenRef, active realizer.HomeObservation) bool {
	if home == nil || !active.Present {
		return false
	}
	if !hasHomeOutputIdentity(home) {
		return home.Generation == active.Generation
	}
	return home.ActivationOutput != "" && active.ActivationOutput == home.ActivationOutput
}

func hasHomeOutputIdentity(home *provision.HomeGenRef) bool {
	return home.Kind != "" || home.ActivationOutput != ""
}

// New provenance uses the active output. Once it exists, legacy generation-only
// records cannot grant ownership to an unmatched native output. A positive
// nativeGeneration also selects that exact historical generation for retries.
func matchingHomeManagerReference(generations []*provision.Generation, activeOutput string, nativeGeneration int) (*provision.HomeGenRef, bool) {
	outputHistory := false
	for _, generation := range generations {
		if generation == nil || generation.HomeManager == nil || !hasHomeOutputIdentity(generation.HomeManager) {
			continue
		}
		outputHistory = true
		home := generation.HomeManager
		if nativeGeneration > 0 && home.Generation != nativeGeneration {
			continue
		}
		if home.ActivationOutput != "" && home.ActivationOutput == activeOutput {
			return home, true
		}
	}
	return nil, outputHistory
}

// An unchanged profile keeps its known source identity. A failed retry must not
// attribute freshly evaluated, unused inputs to that already-active output.
func preserveHomeManagerRetryIdentity(home *provision.HomeGenRef) *provision.HomeGenRef {
	if generations, err := listGenerationsFn(); err == nil {
		if previous, _ := matchingHomeManagerReference(generations, home.ActivationOutput, home.Generation); previous != nil {
			copyRef := *previous
			copyRef.ActivationFailed = home.ActivationFailed
			return &copyRef
		}
	}
	// Without exact history, only the observed native identity is proved. Losing
	// source recovery costs a warning; inventing it could restore unused inputs.
	return &provision.HomeGenRef{
		Generation:       home.Generation,
		ActivationOutput: home.ActivationOutput,
		ActivationFailed: home.ActivationFailed,
	}
}

func settingsDigest(settings *manifest.HomeManagerSettings) string {
	data, err := json.Marshal(settings)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func homeGenerationReference(flags ApplyFlags, mf *manifest.Manifest, home *preparedHomeManager, generation int) *provision.HomeGenRef {
	_, name, _ := strings.Cut(home.flake, "#")
	result := &provision.HomeGenRef{Flake: home.flake, Generation: generation, Generated: home.generated, Platform: runtime.GOOS, System: nix.System(), ConfigIdentity: name, Resolution: home.resolution, Kind: "flake"}
	result.FileExpectations = home.fileExpectations
	if home.resolution != nil && len(home.resolution.Outputs) == 1 {
		result.ActivationOutput = home.resolution.OutputPaths[home.resolution.Outputs[0]]
	}
	if home.generated {
		result.Kind = "config"
		// Captured placements are disposable: a composed record keeps only the explicit declaration, if any.
		if mf.HomeManager != nil {
			result.Config = mf.HomeManager.Config
			result.Secrets = mf.HomeManager.Secrets
		}
		if mf.HomeManager != nil && mf.HomeManager.Settings != nil {
			result.Kind = "settings"
			result.SourceManifest, _ = filepath.Abs(flags.Manifest)
			result.SettingsDigest = settingsDigest(mf.HomeManager.Settings)
		}
	}
	return result
}
