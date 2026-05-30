// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

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
type RollbackResult struct {
	DryRun           bool   `json:"dryRun"`
	Backend          string `json:"backend"`
	TargetGeneration int    `json:"targetGeneration,omitempty"` // engine generation resolved from --to; 0 = previous
	FromNative       string `json:"fromNative,omitempty"`       // backend-native version before rollback
	ToNative         string `json:"toNative,omitempty"`         // native version targeted (dry-run) or active after
	NewGeneration    int    `json:"newGeneration,omitempty"`    // appended engine generation number (0 on dry-run)
}

// RunRollback reverts the installed package set to a prior Provisioning
// Generation, for backends that advertise native rollback (the Nix realizer
// today). It is package-stage only: it never touches config restore,
// state/backups/, or the revert journal. The target is identified by engine
// generation number and mapped to the backend-native anchor (the user never
// references a Nix version directly — the moat).
func RunRollback(flags RollbackFlags) (interface{}, *envelope.Error) {
	// Acquire the host realizer. Hosts with no realizer (e.g. Windows, which uses
	// the per-package winget driver) have no native rollback in this phase.
	r, rerr := newRealizerFn()
	if rerr != nil {
		return nil, envelope.NewError(envelope.ErrRollbackUnsupported,
			"This platform's package backend does not support rollback.").
			WithRemediation("Native rollback is available on Linux/macOS via the Nix backend.")
	}

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
