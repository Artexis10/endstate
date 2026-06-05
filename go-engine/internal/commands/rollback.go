// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// untrackedDepsWarning is surfaced by the best-effort (winget) rollback path:
// package-manager-pulled transitive dependencies and co-installs are not recorded
// in a Provisioning Generation, so a best-effort rollback may leave them behind.
const untrackedDepsWarning = "Package-manager-pulled transitive dependencies and co-installs are not tracked and may remain installed."

// RollbackFlags holds the parsed CLI flags for the rollback command.
type RollbackFlags struct {
	// To is the engine Provisioning Generation number to roll back to. Empty
	// means roll back to the immediately previous version.
	To string
	// Confirm gates the (state-changing) rollback; without it the command refuses.
	Confirm bool
	// DryRun previews the resolved target without changing any state and without
	// requiring Confirm.
	DryRun bool
	// EnableRestore opts rollback into ALSO reverting the home-manager config
	// recorded in the target generation (realizer backends only), symmetric with
	// `apply --enable-restore`. Without it, rollback is package-only. Config
	// rollback requires an explicit --to <generation>.
	EnableRestore bool
	// Events controls streaming event output. Accepted for parity; rollback does
	// not stream a per-item sequence (it is a single whole-set operation).
	Events string
}

// RollbackResult is the data payload for the rollback command JSON envelope.
// The native (realizer) path populates FromNative/ToNative; the best-effort
// (driver/winget) path populates RemovedRefs/FailedRefs/Partial/Warning.
type RollbackResult struct {
	DryRun           bool   `json:"dryRun"`
	Backend          string `json:"backend"`
	TargetGeneration int    `json:"targetGeneration,omitempty"` // engine generation resolved from --to; 0 = previous
	// Native (realizer) rollback fields.
	FromNative    string `json:"fromNative,omitempty"` // backend-native version before rollback
	ToNative      string `json:"toNative,omitempty"`   // native version targeted (dry-run) or active after
	NewGeneration int    `json:"newGeneration,omitempty"`
	// Best-effort (driver/winget) rollback fields.
	RemovedRefs []string `json:"removedRefs,omitempty"` // refs uninstalled (or, on --dry-run, that would be uninstalled)
	FailedRefs  []string `json:"failedRefs,omitempty"`  // refs whose uninstall failed
	Partial     bool     `json:"partial,omitempty"`     // true when any targeted uninstall failed
	Warning     string   `json:"warning,omitempty"`     // best-effort caveat (untracked dependencies)

	// HomeManager carries the opt-in (--enable-restore) home-manager config
	// rollback outcome; nil when no config stage ran.
	HomeManager *RollbackHomeResult `json:"homeManager,omitempty"`
}

// RollbackHomeResult is the home-manager config-rollback portion of a rollback.
// On --dry-run it reports the target (recorded) generation that would be
// re-activated; on a real run it also reports the new (forward) generation.
type RollbackHomeResult struct {
	TargetGeneration int    `json:"targetGeneration"`          // recorded home-manager generation being reverted to
	NewGeneration    int    `json:"newGeneration,omitempty"`   // new forward generation after re-activation (0 on dry-run)
	Flake            string `json:"flake,omitempty"`           // the config that was/would be re-activated
	Config           string `json:"config,omitempty"`          // the user's declared home.nix, when the recorded config was engine-generated
	Reactivated      bool   `json:"reactivated"`               // true when the config was actually re-activated
	ViaFallback      bool   `json:"viaFallback,omitempty"`     // true when re-activated from the recorded flake because the snapshot was unavailable
}

// RunRollback reverts the installed package set to a prior Provisioning
// Generation. It is package-stage only: it never touches config restore,
// state/backups/, or the revert journal.
//
// Two strategies, both keyed off the recorded Provisioning Generations:
//   - Native (realizer, e.g. Nix): atomic `nix profile rollback` to the engine
//     generation's recorded native anchor (the user never references a Nix
//     version — the moat).
//   - Best-effort (driver, e.g. winget): uninstall the union of addedRefs of the
//     generations recorded after the target (non-atomic, failure-tolerant).
//
// Dispatch: a host with a realizer uses the native path exclusively; otherwise a
// driver that implements driver.Uninstaller uses the best-effort path; otherwise
// rollback is unsupported on this host.
func RunRollback(flags RollbackFlags) (interface{}, *envelope.Error) {
	if r, rerr := newRealizerFn(); rerr == nil {
		return runRealizerRollback(flags, r)
	}
	if d, derr := newDriverFn(); derr == nil {
		if un, ok := d.(driver.Uninstaller); ok {
			return runDriverRollback(flags, d, un)
		}
	}
	return nil, envelope.NewError(envelope.ErrRollbackUnsupported,
		"This platform's package backend does not support rollback.").
		WithRemediation("Native rollback needs a Nix backend (Linux/macOS); best-effort rollback needs an uninstall-capable driver (winget).")
}

// runRealizerRollback performs a native, atomic rollback via a realizer that
// advertises native rollback (Nix). The target is identified by engine
// generation number and mapped to the backend-native anchor.
func runRealizerRollback(flags RollbackFlags, r realizer.Realizer) (interface{}, *envelope.Error) {
	// Discover rollback eligibility by type-assertion + advertised capability,
	// exactly like driver.BatchDetector / provision.CapabilityReporter.
	rb, ok := r.(provision.Rollbacker)
	if !ok || !nativeRollbackCapable(r) {
		return nil, envelope.NewError(envelope.ErrRollbackUnsupported,
			fmt.Sprintf("The %s backend does not support native rollback.", r.Name())).
			WithRemediation("Rollback requires a backend that advertises native rollback (e.g. Nix).")
	}

	// Resolve the native target version from the engine generation number.
	target := -1                               // backend-native version; -1 == previous
	targetGen := 0                             // engine generation number; 0 == unspecified (previous)
	hasPackageTarget := true                   // false for a config-only target generation (no native anchor)
	var targetGeneration *provision.Generation // the resolved generation (config rollback reads its HomeManager ref)
	if to := strings.TrimSpace(flags.To); to != "" {
		n, err := strconv.Atoi(to)
		if err != nil || n <= 0 {
			return nil, envelope.NewError(envelope.ErrGenerationNotFound,
				fmt.Sprintf("Invalid generation number %q.", flags.To)).
				WithRemediation("Run 'endstate generations' to list available generations.")
		}
		gen, gerr := findGeneration(n)
		if gerr != nil {
			return nil, gerr
		}
		targetGen = n
		targetGeneration = gen
		// A config-only generation (a config apply that installed/pruned nothing)
		// records no native anchor; that is not an error when --enable-restore will
		// roll its config back. The "nothing to roll back" rejection is deferred
		// until config eligibility is known.
		if native, nerr := strconv.Atoi(gen.Native); gen.Native != "" && nerr == nil {
			target = native
		} else {
			hasPackageTarget = false
		}
	}

	// Config rollback eligibility, validated BEFORE any mutation (--enable-restore
	// opts rollback into ALSO reverting the home-manager config recorded in the
	// target generation; it is realizer-only and requires an explicit --to so there
	// is a recorded config to revert to).
	var homeRef *provision.HomeGenRef
	if flags.EnableRestore {
		if targetGeneration == nil {
			return nil, envelope.NewError(envelope.ErrGenerationNotFound,
				"Home-manager config rollback requires an explicit target generation.").
				WithRemediation("Re-run with --to <generation> (see 'endstate generations'); bare rollback is package-only.")
		}
		homeRef = targetGeneration.HomeManager
		if homeRef != nil {
			if _, ok := r.(realizer.HomeRollbacker); !ok {
				return nil, envelope.NewError(envelope.ErrRollbackUnsupported,
					fmt.Sprintf("The %s backend does not support home-manager config rollback.", r.Name())).
					WithRemediation("Config rollback requires a backend that can re-activate a home-manager configuration (e.g. Nix).")
			}
		}
	}

	// Brew best-effort lane (composed with the native rollback): when an explicit
	// --to target is given, uninstall the brew apps installed AFTER it — recorded in
	// their own backend:"brew" generations, which the native nix rollback never
	// touches. Bare rollback (no --to) stays native-package-only: the nix "previous"
	// anchor is generation-relative and cannot be reconciled with interleaved brew
	// generations without an explicit boundary. Computed read-only here so it informs
	// the dry-run preview, the nothing-to-roll-back check, and the uninstall below.
	var brewRemoveRefs []string
	if strings.TrimSpace(flags.To) != "" {
		brewRemoveRefs = brewRollbackRefs(targetGen)
	}

	// Nothing to roll back: a target generation with no native anchor, no eligible
	// config rollback, AND no brew refs to uninstall. Preserves the package-only "no
	// native anchor" rejection while letting a config-only generation be reverted
	// under --enable-restore and a brew-only target uninstall its later brew refs.
	if !hasPackageTarget && homeRef == nil && len(brewRemoveRefs) == 0 {
		return nil, envelope.NewError(envelope.ErrGenerationNotFound,
			fmt.Sprintf("Generation %d has no native rollback anchor.", targetGen)).
			WithRemediation("Only generations committed by a native-rollback backend can be rolled back to.")
	}

	// Best-effort current native version for reporting.
	fromNative := ""
	if cur, err := r.Current(); err == nil {
		fromNative = strconv.Itoa(cur.Generation)
	}

	toNative := ""
	if hasPackageTarget {
		toNative = "previous"
		if target > 0 {
			toNative = strconv.Itoa(target)
		}
	}

	if flags.DryRun {
		res := &RollbackResult{
			DryRun:           true,
			Backend:          r.Name(),
			TargetGeneration: targetGen,
			FromNative:       fromNative,
			ToNative:         toNative,
		}
		if flags.EnableRestore && homeRef != nil {
			res.HomeManager = &RollbackHomeResult{
				TargetGeneration: homeRef.Generation,
				Flake:            homeRef.Flake,
				Config:           homeRef.Config,
			}
		}
		if len(brewRemoveRefs) > 0 {
			res.RemovedRefs = brewRemoveRefs // would-uninstall preview
			res.Warning = untrackedDepsWarning
		}
		return res, nil
	}

	if !flags.Confirm {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"rollback requires --confirm to acknowledge that it changes the installed package set").
			WithRemediation("Re-run with --confirm, or use --dry-run to preview the target.")
	}

	// Packages first (mirrors apply's package-then-config ordering). Skipped for a
	// config-only target generation, which has no native package anchor.
	if hasPackageTarget {
		if err := rb.Rollback(target); err != nil {
			return nil, rollbackError(err)
		}
	}

	// Config (opt-in): re-activate the home-manager config recorded in the target
	// generation. Mints a new forward home-manager generation (append-only).
	var homeResult *RollbackHomeResult
	var newHomeRef *provision.HomeGenRef
	if flags.EnableRestore && homeRef != nil {
		newGen, viaFallback, herr := rollbackHomeConfig(r, homeRef)
		if herr != nil {
			return nil, herr
		}
		newHomeRef = &provision.HomeGenRef{Flake: homeRef.Flake, Config: homeRef.Config, Generation: newGen}
		homeResult = &RollbackHomeResult{
			TargetGeneration: homeRef.Generation,
			NewGeneration:    newGen,
			Flake:            homeRef.Flake,
			Config:           homeRef.Config,
			Reactivated:      true,
			ViaFallback:      viaFallback,
		}
	}

	// Success: append a new Provisioning Generation snapshotting the now-active
	// set (and any reverted config) so the append-only history keeps
	// "newest == active" truthful. A config-only rollback records no native anchor
	// (packages were untouched), so a later package rollback treats it correctly.
	cur, _ := r.Current()
	native := ""
	if hasPackageTarget {
		native = strconv.Itoa(cur.Generation)
	}
	// Append the native rollback generation only when the native lane actually
	// rolled back packages or re-activated config. A brew-only rollback (no native
	// anchor, no config) records ONLY its backend:"brew" generation below, never an
	// empty backend:"nix" one.
	newGen := 0
	if hasPackageTarget || newHomeRef != nil {
		newGen = appendRollbackGeneration(buildRunID("rollback"), r.Name(), cur, native, newHomeRef)
	}

	// Brew best-effort uninstall lane, composed with the native rollback. Runs AFTER
	// the native package + config rollback; per-package and failure-tolerant — a brew
	// failure never unwinds the (already committed) native rollback. Records a
	// separate backend:"brew" rollback generation.
	res := &RollbackResult{
		DryRun:           false,
		Backend:          r.Name(),
		TargetGeneration: targetGen,
		FromNative:       fromNative,
		ToNative:         native,
		NewGeneration:    newGen,
		HomeManager:      homeResult,
	}
	if len(brewRemoveRefs) > 0 {
		removed, failed := runBrewRollbackLane(brewRemoveRefs)
		res.RemovedRefs = removed
		res.FailedRefs = failed
		res.Partial = len(failed) > 0
		res.Warning = untrackedDepsWarning
	}
	return res, nil
}

// brewRollbackRefs returns the de-duplicated union of the AddedRefs of every
// backend:"brew" Provisioning Generation numbered greater than targetGen — the
// brew packages a best-effort rollback to targetGen must uninstall. nix and winget
// generations are ignored (the native lane owns those). Order follows provision.List.
func brewRollbackRefs(targetGen int) []string {
	gens, err := provision.List()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var refs []string
	for _, g := range gens {
		if g.Backend != "brew" || g.Number <= targetGen {
			continue
		}
		for _, ref := range g.AddedRefs {
			if ref != "" && !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

// runBrewRollbackLane uninstalls the given brew refs best-effort via the host's
// brew driver and records a backend:"brew" rollback generation. It mirrors
// runDriverRollback's per-ref, failure-tolerant loop, scoped to brew and composed
// into the native rollback. Because the native rollback has ALREADY committed when
// this runs, an unavailable brew driver or a per-ref infrastructure error is
// tolerated (the refs are reported failed) rather than aborting — the native
// rollback cannot be unwound. Cask uninstalls are non-destructive (the brew driver
// never passes --zap). A generation is recorded only when at least one ref was
// actually removed (an all-failed lane records none, matching the winget path's
// "nothing removed → no generation").
func runBrewRollbackLane(refs []string) (removed, failed []string) {
	d, derr := newBrewDriverFn()
	if derr != nil {
		return nil, refs
	}
	un, ok := d.(driver.Uninstaller)
	if !ok {
		return nil, refs
	}
	for _, ref := range refs {
		res, uerr := un.Uninstall(ref)
		if uerr != nil || res == nil {
			failed = append(failed, ref)
			continue
		}
		switch res.Status {
		case driver.StatusUninstalled, driver.StatusAbsent:
			removed = append(removed, ref)
		default:
			failed = append(failed, ref)
		}
	}
	if len(removed) > 0 {
		appendRollbackGenerationRemoved(buildRunID("rollback"), "brew", removed, len(failed) > 0)
	}
	return removed, failed
}

// rollbackHomeConfig re-activates the home-manager config recorded by the target
// generation via the realizer's HomeRollbacker, returning the new (forward)
// home-manager generation. When the recorded generation's snapshot is no longer
// available it falls back to re-activating a directly-referenced flake (faithful
// for a pinned flake); for an engine-generated wrapper (Config set) the state-dir
// flake now holds the LATEST config, not the target's, so it refuses rather than
// activate the wrong config (non-destructive). The caller has already confirmed r
// is a HomeRollbacker. viaFallback reports whether the fallback path ran.
func rollbackHomeConfig(r realizer.Realizer, homeRef *provision.HomeGenRef) (newGen int, viaFallback bool, eerr *envelope.Error) {
	hr := r.(realizer.HomeRollbacker)
	gen, herr := hr.RollbackHome(homeRef.Generation)
	if herr == nil {
		return gen, false, nil
	}
	if !errors.Is(herr, realizer.ErrHomeSnapshotMissing) {
		return 0, false, homeRollbackError(herr)
	}

	// Snapshot garbage-collected. Fall back only when faithful.
	if homeRef.Config != "" || homeRef.Flake == "" {
		// Engine-generated wrapper (or nothing to fall back to): the state-dir flake
		// holds the latest config, not the target's — refuse rather than mislead.
		return 0, false, envelope.NewError(envelope.ErrRollbackFailed,
			"The home-manager configuration snapshot for the target generation is unavailable (garbage-collected).").
			WithRemediation("Re-apply the desired home.nix with 'endstate apply --enable-restore'.")
	}
	activator, ok := r.(realizer.HomeActivator)
	if !ok {
		return 0, false, envelope.NewError(envelope.ErrRollbackFailed,
			"The home-manager configuration snapshot is unavailable and cannot be re-activated.").
			WithRemediation("Re-apply the desired configuration with 'endstate apply --enable-restore'.")
	}
	gen, aerr := activator.ActivateHome(homeRef.Flake)
	if aerr != nil {
		return 0, false, homeRollbackError(aerr)
	}
	return gen, true, nil
}

// homeRollbackError maps a home-manager config-rollback failure to an envelope
// error, mirroring rollbackError: systemic classes reuse the realizer envelope
// error; otherwise the (already-classified) code is surfaced with raw backend
// text confined to error.detail (the moat). A generic INSTALL_FAILED from the
// fallback ActivateHome is remapped to ROLLBACK_FAILED so the error names the verb.
func homeRollbackError(err error) *envelope.Error {
	rerr, ok := err.(*realizer.Error)
	if !ok {
		return envelope.NewError(envelope.ErrRollbackFailed, "Home-manager configuration rollback failed.").
			WithDetail(map[string]string{"raw": err.Error()})
	}
	if isSystemic(rerr.Code) {
		return realizerEnvelopeError(rerr)
	}
	code := rerr.Code
	if code == envelope.ErrInstallFailed {
		code = envelope.ErrRollbackFailed
	}
	return envelope.NewError(code, "Home-manager configuration rollback failed.").
		WithDetail(map[string]string{"subcode": rerr.Subcode, "stage": rerr.Stage, "raw": rerr.Raw}).
		WithRemediation("Run 'endstate generations' to inspect available rollback targets.")
}

// nativeRollbackCapable reports whether r advertises native rollback. A backend
// that does not report capabilities is treated as not rollback-capable.
func nativeRollbackCapable(r realizer.Realizer) bool {
	cr, ok := r.(provision.CapabilityReporter)
	if !ok {
		return false
	}
	return cr.Capabilities().NativeRollback
}

// findGeneration returns the recorded Provisioning Generation numbered n, or a
// GENERATION_NOT_FOUND envelope error when none exists.
func findGeneration(n int) (*provision.Generation, *envelope.Error) {
	gens, err := provision.List()
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, err.Error())
	}
	for _, g := range gens {
		if g.Number == n {
			return g, nil
		}
	}
	return nil, envelope.NewError(envelope.ErrGenerationNotFound,
		fmt.Sprintf("No Provisioning Generation numbered %d.", n)).
		WithRemediation("Run 'endstate generations' to list available generations.")
}

// appendRollbackGeneration writes a new Provisioning Generation snapshotting the
// now-active package set after a successful rollback. AddedRefs is empty (nothing
// was newly installed) and Rollback is true. When home is non-nil it also records
// the home-manager config re-activated by the rollback (a flakeref + generation
// number — still a provisioning fact, not config file contents), so the
// append-only history's newest record reflects the active config too. It is
// best-effort: a write error never fails the rollback, mirroring run-history
// persistence. Returns the assigned generation number, or 0 on write failure.
//
// Separation of concerns: this records package facts plus the engine-owned
// home-manager config reference — it never touches the config backup directory or
// the restore revert journal.
func appendRollbackGeneration(runID, backend string, set realizer.Set, native string, home *provision.HomeGenRef) int {
	items := make([]provision.ProvItem, 0, len(set.Elements))
	for name, e := range set.Elements {
		ref := e.AttrPath
		if ref == "" {
			ref = name
		}
		items = append(items, provision.ProvItem{ID: name, Ref: ref, Status: "present"})
	}
	g := &provision.Generation{
		RunID:       runID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Backend:     backend,
		Items:       items,
		AddedRefs:   []string{},
		Native:      native,
		Rollback:    true,
		HomeManager: home,
	}
	if err := provision.Write(g); err != nil {
		return 0
	}
	return g.Number
}

// runDriverRollback performs a best-effort rollback on a driver that can
// uninstall (winget). It reverts by uninstalling the union of the added
// references of every Provisioning Generation recorded after the target. It is
// non-atomic and failure-tolerant: per-package failures are reported and the run
// is marked partial, never aborting mid-way. Package-stage only — it touches the
// driver and internal/provision, never restore/backups/the revert journal.
func runDriverRollback(flags RollbackFlags, d driver.Driver, un driver.Uninstaller) (interface{}, *envelope.Error) {
	backend := d.Name()
	gens, err := provision.List()
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, err.Error())
	}

	maxNum := 0
	for _, g := range gens {
		if g.Number > maxNum {
			maxNum = g.Number
		}
	}

	// Resolve the target generation number.
	targetGen := maxNum - 1 // bare rollback reverts the most recent generation
	if to := strings.TrimSpace(flags.To); to != "" {
		n, perr := strconv.Atoi(to)
		if perr != nil || n <= 0 {
			return nil, envelope.NewError(envelope.ErrGenerationNotFound,
				fmt.Sprintf("Invalid generation number %q.", flags.To)).
				WithRemediation("Run 'endstate generations' to list available generations.")
		}
		if _, gerr := findGeneration(n); gerr != nil {
			return nil, gerr
		}
		targetGen = n
	}

	// removeRefs = union of addedRefs of every generation numbered > targetGen.
	seen := map[string]bool{}
	var removeRefs []string
	for _, g := range gens {
		if g.Number <= targetGen {
			continue
		}
		for _, ref := range g.AddedRefs {
			if ref != "" && !seen[ref] {
				seen[ref] = true
				removeRefs = append(removeRefs, ref)
			}
		}
	}

	// Nothing recorded after the target → already at/before it; a no-op success.
	if len(removeRefs) == 0 {
		return &RollbackResult{DryRun: flags.DryRun, Backend: backend, TargetGeneration: targetGen}, nil
	}

	if flags.DryRun {
		return &RollbackResult{
			DryRun:           true,
			Backend:          backend,
			TargetGeneration: targetGen,
			RemovedRefs:      removeRefs, // would-remove preview
			Warning:          untrackedDepsWarning,
		}, nil
	}

	if !flags.Confirm {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"rollback requires --confirm to acknowledge that it uninstalls packages").
			WithRemediation("Re-run with --confirm, or use --dry-run to preview what would be removed.")
	}

	// Uninstall each ref independently; never abort on the first failure.
	var removed, failed []string
	for _, ref := range removeRefs {
		res, uerr := un.Uninstall(ref)
		if uerr != nil {
			// Infrastructure failure (e.g. the winget binary is missing) — whole-run.
			return nil, envelope.NewError(envelope.ErrWingetNotAvailable,
				"The package backend is unavailable.").
				WithDetail(map[string]string{"raw": uerr.Error()}).
				WithRemediation("Ensure winget is installed and on PATH.")
		}
		switch res.Status {
		case driver.StatusUninstalled, driver.StatusAbsent:
			removed = append(removed, ref)
		default:
			failed = append(failed, ref)
		}
	}

	// Every targeted uninstall failed → a top-level failure.
	if len(removed) == 0 {
		return nil, envelope.NewError(envelope.ErrRollbackFailed,
			"Rollback failed: no packages could be uninstalled.").
			WithDetail(map[string]string{"failed": strings.Join(failed, ", ")}).
			WithRemediation("Another installed package may depend on them; inspect the failed packages.")
	}

	// Append a rollback-marked generation recording what was removed.
	newGen := appendRollbackGenerationRemoved(buildRunID("rollback"), backend, removed, len(failed) > 0)

	return &RollbackResult{
		DryRun:           false,
		Backend:          backend,
		TargetGeneration: targetGen,
		RemovedRefs:      removed,
		FailedRefs:       failed,
		Partial:          len(failed) > 0,
		NewGeneration:    newGen,
		Warning:          untrackedDepsWarning,
	}, nil
}

// appendRollbackGenerationRemoved writes a rollback-marked Provisioning
// Generation recording the refs a best-effort rollback uninstalled. AddedRefs is
// empty; RemovedRefs carries what was removed; Partial is set when any targeted
// uninstall failed. Best-effort: a write error never fails the rollback. Returns
// the assigned generation number, or 0 on write failure.
func appendRollbackGenerationRemoved(runID, backend string, removed []string, partial bool) int {
	g := &provision.Generation{
		RunID:       runID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Backend:     backend,
		Items:       []provision.ProvItem{},
		AddedRefs:   []string{},
		RemovedRefs: removed,
		Rollback:    true,
		Partial:     partial,
	}
	if err := provision.Write(g); err != nil {
		return 0
	}
	return g.Number
}

// rollbackError maps a backend rollback failure to an envelope error. Systemic
// infrastructure failures (REALIZER_UNAVAILABLE / PERMISSION_DENIED) reuse the
// realizer envelope error; otherwise the (already-classified) ROLLBACK_FAILED
// code is surfaced with raw backend text confined to error.detail (the moat).
func rollbackError(err error) *envelope.Error {
	rerr, ok := err.(*realizer.Error)
	if !ok {
		return envelope.NewError(envelope.ErrRollbackFailed, "Rollback failed.").
			WithDetail(map[string]string{"raw": err.Error()})
	}
	if isSystemic(rerr.Code) {
		return realizerEnvelopeError(rerr)
	}
	return envelope.NewError(rerr.Code, "Rollback failed.").
		WithDetail(map[string]string{"subcode": rerr.Subcode, "stage": rerr.Stage, "raw": rerr.Raw}).
		WithRemediation("Run 'endstate generations' to inspect available rollback targets.")
}
