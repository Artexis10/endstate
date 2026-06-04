// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// partitionBrewLane splits a synthesized app set into the brew lane (apps that
// declare driver:"brew", case-insensitively) and the rest (everything the Nix
// realizer owns). Order is preserved within each lane so the event stream and
// recorded actions stay deterministic. The realizer is always handed restApps —
// it NEVER sees a brew/`cask:` ref.
func partitionBrewLane(apps []manifest.App) (brewApps, restApps []manifest.App) {
	for _, app := range apps {
		if strings.EqualFold(app.Driver, "brew") {
			brewApps = append(brewApps, app)
		} else {
			restApps = append(restApps, app)
		}
	}
	return brewApps, restApps
}

// brewLane is the interleaved, best-effort brew install lane that runs ALONGSIDE
// the realizer's atomic generation in a single apply. It interleaves per-item
// events into the realizer's already-open plan/apply/verify phases (the realizer
// emits the single phase + summary events; the brew counts merge into those
// summaries — the brew lane never opens its own phase or summary). Each phase
// method returns the (present, toInstall|installed|skipped, failed) counts to
// fold into the matching realizer summary.
//
// Sequencing/isolation: planBrew runs in the plan phase; applyBrew runs in the
// apply phase AFTER the realizer committed its generation, so a brew failure is
// a per-item `failed` that never rolls back or aborts the nix generation;
// verifyBrew runs in the verify phase. The recorded brewActions are appended to
// the realizer actions and written to a SEPARATE Backend:"brew" provisioning
// generation.
//
// A nil drv (non-darwin host: newBrewDriverFn → ErrNoBrewDriver) surfaces every
// brew app as a visible `skipped`/`filtered` item rather than silently dropping
// it (parity with the realizer's manual-app handling).
type brewLane struct {
	drv     driver.Driver
	emitter *events.Emitter
	apps    []manifest.App

	// per-app planning state carried plan→apply→verify (index-aligned with apps).
	refs      []string
	names     []string
	installed []bool
	actions   []ApplyAction
}

// newBrewLane prepares a brewLane over the partitioned brewApps. It does no I/O.
func newBrewLane(drv driver.Driver, emitter *events.Emitter, apps []manifest.App) *brewLane {
	return &brewLane{drv: drv, emitter: emitter, apps: apps}
}

// active reports whether the lane has any work (apps present). Phase methods are
// safe to call on an inactive lane (they no-op, returning zero counts).
func (l *brewLane) active() bool { return l != nil && len(l.apps) > 0 }

// planBrew emits the brew plan items (detect presence) and records the initial
// actions. Returns (present, toInstall, 0). When drv is nil every app is a
// visible skip (present=0, toInstall=0) and a skipped action is recorded.
func (l *brewLane) planBrew() (present, toInstall int) {
	if !l.active() {
		return 0, 0
	}
	l.names = make([]string, len(l.apps))
	l.refs = make([]string, len(l.apps))
	l.installed = make([]bool, len(l.apps))
	l.actions = make([]ApplyAction, len(l.apps))

	if l.drv == nil {
		for i, app := range l.apps {
			ref := app.Refs["darwin"]
			name := brewItemName(app, ref)
			l.refs[i], l.names[i] = ref, name
			l.actions[i] = ApplyAction{ID: app.ID, Ref: refPtrOrNil(ref), Driver: "brew", Name: name,
				Status: "skipped", Reason: "filtered", Message: "brew driver unavailable on this host"}
			l.emitter.EmitItem(brewEventID(app.ID, ref), "brew", "skipped", "filtered", l.actions[i].Message, name)
		}
		return 0, 0
	}

	refs := make([]string, 0, len(l.apps))
	for _, app := range l.apps {
		if ref := app.Refs["darwin"]; ref != "" {
			refs = append(refs, ref)
		}
	}
	batch := brewDetectBatch(l.drv, refs)

	for i, app := range l.apps {
		ref := app.Refs["darwin"]
		res := batch[ref]
		name := brewItemName(app, ref)
		if res.DisplayName != "" {
			name = res.DisplayName
		}
		l.refs[i], l.names[i], l.installed[i] = ref, name, res.Installed
		a := ApplyAction{ID: app.ID, Ref: refPtrOrNil(ref), Driver: "brew", Name: name, Version: res.Version}
		if res.Installed {
			a.Status, a.Reason = "present", driver.ReasonAlreadyInstalled
			l.emitter.EmitItem(ref, "brew", "present", driver.ReasonAlreadyInstalled, "Already installed", name)
			present++
		} else {
			a.Status, a.Reason = "to_install", driver.ReasonMissing
			l.emitter.EmitItem(ref, "brew", "to_install", driver.ReasonMissing, "Will be installed", name)
			toInstall++
		}
		l.actions[i] = a
	}
	return present, toInstall
}

// applyBrew installs the to_install brew apps (best-effort, per-package) and
// returns (installed, skipped, failed). A present app counts skipped; an install
// failure is a per-item failed that never aborts the nix generation. Must be
// called after planBrew. When drv is nil it folds the visible-skip count.
func (l *brewLane) applyBrew() (installed, skipped, failed int) {
	if !l.active() {
		return 0, 0, 0
	}
	if l.drv == nil {
		return 0, len(l.apps), 0 // each unavailable-host app counts as skipped
	}
	for i, app := range l.apps {
		ref := l.refs[i]
		name := l.names[i]
		_ = app
		if l.installed[i] {
			skipped++
			continue
		}
		l.emitter.EmitItem(ref, "brew", "installing", "", fmt.Sprintf("Installing %s", ref), name)
		result, installErr := l.drv.Install(ref)
		if installErr != nil {
			l.actions[i].Status = driver.StatusFailed
			l.actions[i].Reason = driver.ReasonInstallFailed
			l.actions[i].Message = installErr.Error()
			l.emitter.EmitItem(ref, "brew", "failed", driver.ReasonInstallFailed, installErr.Error(), name)
			failed++
			continue
		}
		l.actions[i].Status = result.Status
		l.actions[i].Reason = result.Reason
		l.actions[i].Message = result.Message
		switch result.Status {
		case driver.StatusInstalled:
			l.emitter.EmitItem(ref, "brew", "installed", "", result.Message, name)
			installed++
		case driver.StatusPresent:
			l.emitter.EmitItem(ref, "brew", "present", result.Reason, result.Message, name)
			skipped++
		default:
			l.emitter.EmitItem(ref, "brew", result.Status, result.Reason, result.Message, name)
			failed++
		}
	}
	return installed, skipped, failed
}

// verifyBrew re-detects presence and returns (pass, fail). Must be called after
// planBrew. When drv is nil it folds the visible-skip count into skipped (no
// pass/fail — those apps were never attempted).
func (l *brewLane) verifyBrew() (pass, fail, skipped int) {
	if !l.active() {
		return 0, 0, 0
	}
	if l.drv == nil {
		return 0, 0, len(l.apps)
	}
	refs := make([]string, 0, len(l.apps))
	for _, ref := range l.refs {
		if ref != "" {
			refs = append(refs, ref)
		}
	}
	batch := brewDetectBatch(l.drv, refs)
	for i := range l.apps {
		ref := l.refs[i]
		name := l.names[i]
		if batch[ref].Installed {
			l.emitter.EmitItem(ref, "brew", "present", "", "Verified installed", name)
			pass++
		} else {
			l.emitter.EmitItem(ref, "brew", "failed", driver.ReasonMissing, "Missing after apply", name)
			fail++
		}
	}
	return pass, fail, 0
}

// brewActions returns the recorded brew ApplyActions (empty before planBrew).
func (l *brewLane) brewActions() []ApplyAction {
	if l == nil {
		return nil
	}
	return l.actions
}

// brewDetectBatch runs the driver's BatchDetector if available, else falls back
// to per-ref Detect. An infrastructure error yields an empty map (every app
// reads as absent), keeping the lane best-effort.
func brewDetectBatch(d driver.Driver, refs []string) map[string]driver.DetectResult {
	out := make(map[string]driver.DetectResult, len(refs))
	if len(refs) == 0 {
		return out
	}
	if bd, ok := d.(driver.BatchDetector); ok {
		if res, err := bd.DetectBatch(refs); err == nil {
			return res
		}
	}
	for _, ref := range refs {
		installed, name, derr := d.Detect(ref)
		if derr != nil {
			out[ref] = driver.DetectResult{}
			continue
		}
		out[ref] = driver.DetectResult{Installed: installed, DisplayName: name}
	}
	return out
}

// brewItemName resolves a stable display name for a brew app event/action.
func brewItemName(app manifest.App, ref string) string {
	if app.DisplayName != "" {
		return app.DisplayName
	}
	if ref != "" {
		return ref
	}
	return app.ID
}

// brewEventID returns the ref for an event id when present, else the app ID
// (so an unavailable-host skip with no darwin ref still has a usable id).
func brewEventID(id, ref string) string {
	if ref != "" {
		return ref
	}
	return id
}

// refPtrOrNil returns &ref when ref is non-empty, else nil (matching the
// ApplyAction.Ref convention where a refless entry carries a nil pointer).
func refPtrOrNil(ref string) *string {
	if ref == "" {
		return nil
	}
	return &ref
}
