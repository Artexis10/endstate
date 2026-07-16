// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// untrackedDepsWarning is surfaced by best-effort package-driver rollback paths:
// package-manager-pulled transitive dependencies and co-installs are not recorded
// in a Provisioning Generation, so a best-effort rollback may leave them behind.
const untrackedDepsWarning = "Package-manager-pulled transitive dependencies and co-installs are not tracked and may remain installed."

// rollbackDriverFn is the named-driver construction seam used only after a
// rollback target has been resolved. Keeping it lazy means dry-run can preview
// recorded generations even when one of their package managers is unavailable.
var rollbackDriverFn = func(name string) (driver.Driver, error) {
	return selectDriver(runtime.GOOS, name)
}

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
// package-driver path populates RemovedRefs/FailedRefs/Partial/Warning.
type RollbackResult struct {
	DryRun           bool   `json:"dryRun"`
	Backend          string `json:"backend"`
	TargetGeneration int    `json:"targetGeneration,omitempty"` // engine generation resolved from --to; 0 = previous
	// Native (realizer) rollback fields.
	FromNative    string `json:"fromNative,omitempty"` // backend-native version before rollback
	ToNative      string `json:"toNative,omitempty"`   // native version targeted (dry-run) or active after
	NewGeneration int    `json:"newGeneration,omitempty"`
	// Best-effort package-driver rollback fields.
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
	TargetGeneration int    `json:"targetGeneration"`        // recorded home-manager generation being reverted to
	NewGeneration    int    `json:"newGeneration,omitempty"` // new forward generation after re-activation (0 on dry-run)
	Flake            string `json:"flake,omitempty"`         // the config that was/would be re-activated
	Config           string `json:"config,omitempty"`        // the user's declared home.nix, when the recorded config was engine-generated
	Reactivated      bool   `json:"reactivated"`             // true when the config was actually re-activated
	ViaFallback      bool   `json:"viaFallback,omitempty"`   // true when re-activated from the recorded flake because the snapshot was unavailable
}

// RunRollback reverts the installed package set to a prior Provisioning
// Generation. It is package-stage only: it never touches config restore,
// state/backups/, or the revert journal.
//
// Two strategies, both keyed off the recorded Provisioning Generations:
//   - Native (realizer, e.g. Nix): atomic `nix profile rollback` to the engine
//     generation's recorded native anchor (the user never references a Nix
//     version — the moat).
//   - Best-effort (per-package drivers): select generations after the target (or
//     the newest apply RunID), group addedRefs by their recorded backend, and
//     uninstall each group through that backend (non-atomic, failure-tolerant).
//
// Dispatch: a host with a realizer uses the native path exclusively; otherwise
// the best-effort path resolves history first and constructs only the recorded
// drivers it needs. This lets a named backend roll back independently of the
// platform default.
func RunRollback(flags RollbackFlags) (interface{}, *envelope.Error) {
	if r, rerr := newRealizerFn(); rerr == nil {
		return runRealizerRollback(flags, r)
	}
	return runDriverRollback(flags, nil, nil)
}

// runRealizerRollback performs a native, atomic rollback via a realizer that
// advertises native rollback (Nix). The target is identified by engine
// generation number and mapped to the backend-native anchor.
func runRealizerRollback(flags RollbackFlags, r realizer.Realizer) (interface{}, *envelope.Error) {
	// Native rollback capability is checked only after history routing decides a
	// native package lane is selected. Brew-only runs do not require it.
	rb, hasNativeRollbacker := r.(provision.Rollbacker)

	// Resolve the native target version from the engine generation number.
	target := -1                               // backend-native version; -1 == previous
	targetGen := 0                             // engine generation number; 0 == unspecified (previous)
	hasPackageTarget := true                   // false for a config-only target generation (no native anchor)
	var targetGeneration *provision.Generation // the resolved generation (config rollback reads its HomeManager ref)
	var bareRunGenerations []*provision.Generation
	explicitTarget := strings.TrimSpace(flags.To) != ""
	if to := strings.TrimSpace(flags.To); explicitTarget {
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
	} else {
		gens, err := provision.List()
		if err != nil {
			return nil, envelope.NewError(envelope.ErrInternalError, err.Error())
		}
		bareRunGenerations = newestNonRollbackRun(gens)
		if len(bareRunGenerations) > 0 {
			hasPackageTarget = false
			targetGen = bareRunGenerations[0].Number - 1
			for _, g := range bareRunGenerations {
				if g.Number-1 < targetGen {
					targetGen = g.Number - 1
				}
				if strings.EqualFold(strings.TrimSpace(g.Backend), strings.TrimSpace(r.Name())) {
					native := strings.TrimSpace(g.Native)
					if _, err := strconv.Atoi(native); native != "" && err == nil {
						hasPackageTarget = true
					}
				}
			}
		}
	}

	if hasPackageTarget && (!hasNativeRollbacker || !nativeRollbackCapable(r)) {
		return nil, envelope.NewError(envelope.ErrRollbackUnsupported,
			fmt.Sprintf("The %s backend does not support native rollback.", r.Name())).
			WithRemediation("Rollback requires a backend that advertises native rollback (e.g. Nix).")
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

	// Brew best-effort lane (composed with native rollback). Explicit rollback
	// removes Brew additions after the numbered boundary. Bare rollback removes
	// only Brew additions from the newest non-rollback runId group, matching the
	// native "previous" step when that apply also committed a Nix generation.
	var brewRemoveRefs []string
	if explicitTarget {
		brewRemoveRefs = brewRollbackRefs(targetGen)
	} else {
		brewRemoveRefs = brewRollbackRefsFromGenerations(bareRunGenerations)
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
	if hasPackageTarget {
		if cur, err := r.Current(); err == nil {
			fromNative = strconv.Itoa(cur.Generation)
		}
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
			Backend:          realizerRollbackBackend(r.Name(), hasPackageTarget, brewRemoveRefs),
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
	rollbackRunID := buildRunID("rollback")
	newGen := 0
	if hasPackageTarget || newHomeRef != nil {
		newGen = appendRollbackGeneration(rollbackRunID, r.Name(), cur, native, newHomeRef)
	}

	// Brew best-effort uninstall lane, composed with the native rollback. Runs AFTER
	// the native package + config rollback; per-package and failure-tolerant — a brew
	// failure never unwinds the (already committed) native rollback. Records a
	// separate backend:"brew" rollback generation.
	res := &RollbackResult{
		DryRun:           false,
		Backend:          realizerRollbackBackend(r.Name(), hasPackageTarget, brewRemoveRefs),
		TargetGeneration: targetGen,
		FromNative:       fromNative,
		ToNative:         native,
		NewGeneration:    newGen,
		HomeManager:      homeResult,
	}
	if len(brewRemoveRefs) > 0 {
		removed, failed, brewGen := runBrewRollbackLane(rollbackRunID, brewRemoveRefs)
		if len(removed) == 0 && len(failed) > 0 && !hasPackageTarget && newHomeRef == nil {
			return nil, envelope.NewError(envelope.ErrRollbackFailed,
				"Rollback failed: no packages could be uninstalled.").
				WithDetail(map[string]string{"failed": strings.Join(failed, ", ")}).
				WithRemediation("Another installed package may depend on them; inspect the failed packages.")
		}
		res.RemovedRefs = removed
		res.FailedRefs = failed
		res.Partial = len(failed) > 0
		res.Warning = untrackedDepsWarning
		if brewGen > res.NewGeneration {
			res.NewGeneration = brewGen
		}
	}
	return res, nil
}

// newestNonRollbackRun selects the newest apply as one rollback unit. A
// non-empty runId selects every non-rollback generation sharing it; a legacy
// empty runId falls back to the single newest generation.
func newestNonRollbackRun(gens []*provision.Generation) []*provision.Generation {
	var newest *provision.Generation
	for _, g := range gens {
		if !g.Rollback {
			newest = g
			break
		}
	}
	if newest == nil {
		return nil
	}
	selected := []*provision.Generation{newest}
	if newest.RunID == "" {
		return selected
	}
	for _, g := range gens {
		if g == newest || g.Rollback || g.RunID != newest.RunID {
			continue
		}
		selected = append(selected, g)
	}
	return selected
}

func brewRollbackRefsFromGenerations(gens []*provision.Generation) []string {
	seen := map[string]bool{}
	var refs []string
	for _, g := range gens {
		if !strings.EqualFold(strings.TrimSpace(g.Backend), "brew") {
			continue
		}
		for _, ref := range g.AddedRefs {
			if ref == "" || seen[ref] {
				continue
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	return refs
}

func realizerRollbackBackend(realizerName string, nativeSelected bool, brewRefs []string) string {
	if len(brewRefs) == 0 {
		return realizerName
	}
	if nativeSelected {
		return "mixed"
	}
	return "brew"
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
// with native rollback when that lane is selected. Because any native rollback has
// already committed when this runs, an unavailable brew driver or a per-ref
// infrastructure error is tolerated (the refs are reported failed) rather than
// aborting. Cask uninstalls are non-destructive (the brew driver
// never passes --zap). A generation is recorded only when at least one ref was
// actually removed (an all-failed lane records none, matching the winget path's
// "nothing removed → no generation").
func runBrewRollbackLane(runID string, refs []string) (removed, failed []string, newGeneration int) {
	d, derr := newBrewDriverFn()
	if derr != nil {
		return nil, refs, 0
	}
	un, ok := d.(driver.Uninstaller)
	if !ok {
		return nil, refs, 0
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
		newGeneration = appendRollbackGenerationRemoved(runID, "brew", removed, len(failed) > 0)
	}
	return removed, failed, newGeneration
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

type rollbackDriverGroup struct {
	backend string
	refs    []string
}

// runDriverRollback performs a best-effort rollback using each selected
// generation's recorded backend. d/un are the already-constructed platform
// default from the legacy dispatch path; named non-default backends are resolved
// lazily through rollbackDriverFn and are never substituted.
func runDriverRollback(flags RollbackFlags, d driver.Driver, un driver.Uninstaller) (interface{}, *envelope.Error) {
	gens, err := provision.List()
	if err != nil {
		return nil, envelope.NewError(envelope.ErrInternalError, err.Error())
	}

	// Resolve the selected non-rollback generations. Explicit rollback selects
	// every later generation; bare rollback selects the complete newest apply run
	// by RunID, with a single-generation fallback for legacy empty RunIDs.
	targetGen := 0
	var selected []*provision.Generation
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
		for _, g := range gens {
			if !g.Rollback && g.Number > targetGen {
				selected = append(selected, g)
			}
		}
	} else {
		var newest *provision.Generation
		for _, g := range gens {
			if !g.Rollback {
				newest = g
				break
			}
		}
		if newest != nil {
			selected = append(selected, newest)
			if newest.RunID != "" {
				for _, g := range gens {
					if g == newest || g.Rollback || g.RunID != newest.RunID {
						continue
					}
					selected = append(selected, g)
				}
			}
			targetGen = newest.Number - 1
			for _, g := range selected {
				if g.Number-1 < targetGen {
					targetGen = g.Number - 1
				}
			}
		}
	}

	groups := groupRollbackGenerations(selected)
	backend := rollbackResultBackend(groups, d)
	removeRefs := flattenRollbackGroupRefs(groups)

	// Nothing recorded after the target → already at/before it; a no-op success.
	if len(removeRefs) == 0 {
		// Preserve the legacy no-history/default-driver gate for mutating commands,
		// but only after history and the rollback target have been resolved. Dry-run
		// remains history-only and never constructs a package driver.
		if !flags.DryRun && d == nil {
			defaultDriver, derr := newDriverFn()
			if derr != nil {
				return nil, rollbackUnsupported()
			}
			defaultUninstaller, ok := defaultDriver.(driver.Uninstaller)
			if !ok {
				return nil, rollbackUnsupported()
			}
			d, un = defaultDriver, defaultUninstaller
			backend = rollbackResultBackend(groups, d)
		}
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

	// Resolve and process each backend independently. A failed or unavailable
	// backend contributes failed refs while later groups continue. The sole legacy
	// exception is a single Winget lane whose infrastructure disappears: preserve
	// WINGET_NOT_AVAILABLE for that established wire behavior.
	var removed, failed []string
	runID := buildRunID("rollback")
	newGen := 0
	for _, group := range groups {
		groupUninstaller, rerr := resolveRollbackUninstaller(group.backend, d, un)
		if rerr != nil {
			if len(groups) == 1 && group.backend == "winget" {
				return nil, wingetRollbackUnavailable(rerr)
			}
			failed = append(failed, group.refs...)
			continue
		}

		var groupRemoved, groupFailed []string
		for _, ref := range group.refs {
			res, uerr := groupUninstaller.Uninstall(ref)
			if uerr != nil {
				if len(groups) == 1 && group.backend == "winget" {
					return nil, wingetRollbackUnavailable(uerr)
				}
				groupFailed = append(groupFailed, ref)
				continue
			}
			if res == nil {
				groupFailed = append(groupFailed, ref)
				continue
			}
			switch res.Status {
			case driver.StatusUninstalled, driver.StatusAbsent:
				groupRemoved = append(groupRemoved, ref)
			default:
				groupFailed = append(groupFailed, ref)
			}
		}

		removed = append(removed, groupRemoved...)
		failed = append(failed, groupFailed...)
		if len(groupRemoved) > 0 {
			newGen = appendRollbackGenerationRemoved(runID, group.backend, groupRemoved, len(groupFailed) > 0)
		}
	}

	// Every targeted uninstall failed → a top-level failure.
	if len(removed) == 0 {
		return nil, envelope.NewError(envelope.ErrRollbackFailed,
			"Rollback failed: no packages could be uninstalled.").
			WithDetail(map[string]string{"failed": strings.Join(failed, ", ")}).
			WithRemediation("Another installed package may depend on them; inspect the failed packages.")
	}

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

// groupRollbackGenerations groups selected generations by their authoritative
// recorded backend. provision.List is newest-first, so first-seen group order is
// newest-generation-first and ref order is deterministic. De-duplication is
// deliberately scoped to one backend; equal refs owned by different managers
// remain distinct rollback operations.
func groupRollbackGenerations(gens []*provision.Generation) []rollbackDriverGroup {
	indices := map[string]int{}
	seen := map[string]map[string]bool{}
	var groups []rollbackDriverGroup
	for _, g := range gens {
		backend := strings.ToLower(strings.TrimSpace(g.Backend))
		idx, ok := indices[backend]
		if !ok {
			idx = len(groups)
			indices[backend] = idx
			seen[backend] = map[string]bool{}
			groups = append(groups, rollbackDriverGroup{backend: backend})
		}
		for _, ref := range g.AddedRefs {
			if ref == "" || seen[backend][ref] {
				continue
			}
			seen[backend][ref] = true
			groups[idx].refs = append(groups[idx].refs, ref)
		}
	}

	// Generations with no additions do not require a backend lane.
	filtered := groups[:0]
	for _, group := range groups {
		if len(group.refs) > 0 {
			filtered = append(filtered, group)
		}
	}
	return filtered
}

func flattenRollbackGroupRefs(groups []rollbackDriverGroup) []string {
	var refs []string
	for _, group := range groups {
		refs = append(refs, group.refs...)
	}
	return refs
}

func rollbackResultBackend(groups []rollbackDriverGroup, d driver.Driver) string {
	if len(groups) == 1 {
		return groups[0].backend
	}
	if len(groups) > 1 {
		return "mixed"
	}
	if d != nil {
		return d.Name()
	}
	return ""
}

func resolveRollbackUninstaller(backend string, d driver.Driver, un driver.Uninstaller) (driver.Uninstaller, error) {
	if d == nil && (backend == "" || strings.EqualFold(backend, "winget")) {
		defaultDriver, err := newDriverFn()
		if err != nil {
			return nil, err
		}
		defaultUninstaller, ok := defaultDriver.(driver.Uninstaller)
		if !ok {
			return nil, fmt.Errorf("recorded backend %q does not support uninstall", backend)
		}
		d, un = defaultDriver, defaultUninstaller
	}
	if d != nil && (backend == "" || strings.EqualFold(strings.TrimSpace(d.Name()), backend)) {
		if un == nil {
			return nil, fmt.Errorf("recorded backend %q does not support uninstall", backend)
		}
		return un, nil
	}
	resolved, err := rollbackDriverFn(backend)
	if err != nil {
		return nil, err
	}
	resolvedUninstaller, ok := resolved.(driver.Uninstaller)
	if !ok {
		return nil, fmt.Errorf("recorded backend %q does not support uninstall", backend)
	}
	return resolvedUninstaller, nil
}

func rollbackUnsupported() *envelope.Error {
	return envelope.NewError(envelope.ErrRollbackUnsupported,
		"This platform's package backend does not support rollback.").
		WithRemediation("Native rollback needs a Nix backend (Linux/macOS); best-effort rollback needs a supported uninstall-capable package driver (Winget, Chocolatey, or Brew).")
}

func wingetRollbackUnavailable(err error) *envelope.Error {
	return envelope.NewError(envelope.ErrWingetNotAvailable,
		"The package backend is unavailable.").
		WithDetail(map[string]string{"raw": err.Error()}).
		WithRemediation("Ensure winget is installed and on PATH.")
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
