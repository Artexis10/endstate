// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
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
	target := -1   // backend-native version; -1 == previous
	targetGen := 0 // engine generation number; 0 == unspecified (previous)
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
		native, nerr := strconv.Atoi(gen.Native)
		if gen.Native == "" || nerr != nil {
			return nil, envelope.NewError(envelope.ErrGenerationNotFound,
				fmt.Sprintf("Generation %d has no native rollback anchor.", n)).
				WithRemediation("Only generations committed by a native-rollback backend can be rolled back to.")
		}
		target = native
		targetGen = n
	}

	// Best-effort current native version for reporting.
	fromNative := ""
	if cur, err := r.Current(); err == nil {
		fromNative = strconv.Itoa(cur.Generation)
	}

	toNative := "previous"
	if target > 0 {
		toNative = strconv.Itoa(target)
	}

	if flags.DryRun {
		return &RollbackResult{
			DryRun:           true,
			Backend:          r.Name(),
			TargetGeneration: targetGen,
			FromNative:       fromNative,
			ToNative:         toNative,
		}, nil
	}

	if !flags.Confirm {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"rollback requires --confirm to acknowledge that it changes the installed package set").
			WithRemediation("Re-run with --confirm, or use --dry-run to preview the target.")
	}

	if err := rb.Rollback(target); err != nil {
		return nil, rollbackError(err)
	}

	// Success: append a new Provisioning Generation snapshotting the now-active
	// set so the append-only history keeps "newest == active" truthful.
	cur, _ := r.Current()
	newGen := appendRollbackGeneration(buildRunID("rollback"), r.Name(), cur)

	return &RollbackResult{
		DryRun:           false,
		Backend:          r.Name(),
		TargetGeneration: targetGen,
		FromNative:       fromNative,
		ToNative:         strconv.Itoa(cur.Generation),
		NewGeneration:    newGen,
	}, nil
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
// was newly installed) and Rollback is true. It is best-effort: a write error
// never fails the rollback, mirroring run-history persistence. Returns the
// assigned generation number, or 0 on write failure.
//
// Separation of concerns: this records package facts only — it never touches the
// config backup directory or the restore revert journal.
func appendRollbackGeneration(runID, backend string, set realizer.Set) int {
	items := make([]provision.ProvItem, 0, len(set.Elements))
	for name, e := range set.Elements {
		ref := e.AttrPath
		if ref == "" {
			ref = name
		}
		items = append(items, provision.ProvItem{ID: name, Ref: ref, Status: "present"})
	}
	g := &provision.Generation{
		RunID:     runID,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Backend:   backend,
		Items:     items,
		AddedRefs: []string{},
		Native:    strconv.Itoa(set.Generation),
		Rollback:  true,
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
