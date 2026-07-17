// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ---------------------------------------------------------------------------
// fakeBrewDriver — a driver.Driver + BatchDetector test double for the brew
// lane. It records install calls and reports presence from `installed`.
// ---------------------------------------------------------------------------

type fakeBrewDriver struct {
	installed    map[string]bool   // ref -> currently present
	versions     map[string]string // ref -> version reported by DetectBatch
	installErr   error             // returned by Install for every call (infra failure)
	installFails map[string]bool   // ref -> Install reports StatusFailed
	installCalls []string          // refs passed to Install, in order
	detectCalls  int               // DetectBatch call count

	// --- uninstall (rollback lane) ---
	uninstallResults map[string]*driver.UninstallResult // ref -> outcome (default: uninstalled)
	uninstallErr     error                              // infrastructure error (e.g. brew missing)
	uninstallCalls   []string                           // refs passed to Uninstall, in order
}

func (f *fakeBrewDriver) Name() string { return "brew" }

func (f *fakeBrewDriver) Detect(ref string) (bool, string, error) {
	if f.installed[ref] {
		return true, ref, nil
	}
	return false, "", nil
}

func (f *fakeBrewDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	f.detectCalls++
	out := make(map[string]driver.DetectResult, len(refs))
	for _, ref := range refs {
		out[ref] = driver.DetectResult{Installed: f.installed[ref], DisplayName: ref, Version: f.versions[ref]}
	}
	return out, nil
}

func (f *fakeBrewDriver) Install(ref string) (*driver.InstallResult, error) {
	f.installCalls = append(f.installCalls, ref)
	if f.installErr != nil {
		return nil, f.installErr
	}
	if f.installFails[ref] {
		return &driver.InstallResult{Status: driver.StatusFailed, Reason: driver.ReasonInstallFailed, Message: "brew exited with code 1"}, nil
	}
	if f.installed == nil {
		f.installed = map[string]bool{}
	}
	f.installed[ref] = true
	return &driver.InstallResult{Status: driver.StatusInstalled, Message: "Installed successfully"}, nil
}

// Uninstall satisfies driver.Uninstaller, recording each ref and returning the
// scripted per-ref outcome (default: uninstalled). It makes *fakeBrewDriver an
// Uninstaller so the best-effort brew rollback lane is exercised host-independently.
func (f *fakeBrewDriver) Uninstall(ref string) (*driver.UninstallResult, error) {
	f.uninstallCalls = append(f.uninstallCalls, ref)
	if f.uninstallErr != nil {
		return nil, f.uninstallErr
	}
	if r, ok := f.uninstallResults[ref]; ok {
		return r, nil
	}
	if f.installed != nil {
		delete(f.installed, ref)
	}
	return &driver.UninstallResult{Status: driver.StatusUninstalled}, nil
}

// panicBrewDriverFn is a newBrewDriverFn that fails the test if it is ever
// called — used by the non-regression test to PROVE the brew factory is not
// resolved when no app routes to brew.
func panicBrewDriverFn(t *testing.T) func() (driver.Driver, error) {
	return func() (driver.Driver, error) {
		t.Fatalf("newBrewDriverFn was called for a no-brew manifest — the brew lane must not resolve the driver")
		return nil, nil
	}
}

// withRealizerAndBrew wires both seams: the fake realizer and a specific brew
// factory. Restores both on return.
func withRealizerAndBrew(fr *fakeRealizer, brewFn func() (driver.Driver, error), f func()) {
	origRz := newRealizerFn
	origBrew := newBrewDriverFn
	newRealizerFn = func() (realizer.Realizer, error) { return fr, nil }
	newBrewDriverFn = brewFn
	defer func() {
		newRealizerFn = origRz
		newBrewDriverFn = origBrew
	}()
	f()
}

// writeTempManifest writes content to a temp .jsonc and returns its path.
func writeTempManifest(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "m.jsonc")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

// ---------------------------------------------------------------------------
// partitionRealizerLanes
// ---------------------------------------------------------------------------

func TestPartitionRealizerLanes_SplitsAndPreservesOrder(t *testing.T) {
	apps := []manifest.App{
		{ID: "ripgrep", Refs: map[string]string{"darwin": "ripgrep"}},
		{ID: "chrome", Driver: "brew", Refs: map[string]string{"darwin": "cask:google-chrome"}},
		{ID: "jq", Refs: map[string]string{"darwin": "jq"}},
		{ID: "windows-only", Driver: "chocolatey", Refs: map[string]string{"darwin": "windows-only"}},
		{ID: "hello", Driver: "BREW", Refs: map[string]string{"darwin": "hello"}},
	}
	brewApps, unsupportedApps, restApps := partitionRealizerLanes(apps)
	if len(brewApps) != 2 || brewApps[0].ID != "chrome" || brewApps[1].ID != "hello" {
		t.Fatalf("brew lane = %+v, want [chrome hello] (case-insensitive)", brewApps)
	}
	if len(unsupportedApps) != 1 || unsupportedApps[0].ID != "windows-only" {
		t.Fatalf("unsupported lane = %+v, want [windows-only]", unsupportedApps)
	}
	if len(restApps) != 2 || restApps[0].ID != "ripgrep" || restApps[1].ID != "jq" {
		t.Fatalf("rest lane = %+v, want [ripgrep jq] order-preserved", restApps)
	}
}

func TestPartitionRealizerLanes_CaskRefAutoRoutesToBrew(t *testing.T) {
	apps := []manifest.App{
		{ID: "ripgrep", Refs: map[string]string{"darwin": "nixpkgs#ripgrep"}},
		// Cask ref, NO driver:"brew" → must still route to the brew lane.
		{ID: "chrome", Refs: map[string]string{"darwin": "cask:google-chrome"}},
		// Case-insensitive prefix.
		{ID: "slack", Refs: map[string]string{"darwin": "Cask:slack"}},
		{ID: "jq", Refs: map[string]string{"darwin": "jq"}},
	}
	brewApps, unsupportedApps, restApps := partitionRealizerLanes(apps)
	if len(unsupportedApps) != 0 {
		t.Fatalf("unsupported lane = %+v, want empty", unsupportedApps)
	}
	if len(brewApps) != 2 || brewApps[0].ID != "chrome" || brewApps[1].ID != "slack" {
		t.Fatalf("brew lane = %+v, want [chrome slack] (cask: auto-routes)", brewApps)
	}
	if len(restApps) != 2 || restApps[0].ID != "ripgrep" || restApps[1].ID != "jq" {
		t.Fatalf("rest lane = %+v, want [ripgrep jq] (nix refs stay)", restApps)
	}
}

func TestPartitionRealizerLanes_NoBrew(t *testing.T) {
	apps := []manifest.App{{ID: "ripgrep", Refs: map[string]string{"darwin": "ripgrep"}}}
	brewApps, unsupportedApps, restApps := partitionRealizerLanes(apps)
	if len(brewApps) != 0 {
		t.Fatalf("brew lane = %+v, want empty", brewApps)
	}
	if len(unsupportedApps) != 0 {
		t.Fatalf("unsupported lane = %+v, want empty", unsupportedApps)
	}
	if len(restApps) != 1 {
		t.Fatalf("rest lane = %+v, want one", restApps)
	}
}

// ---------------------------------------------------------------------------
// Non-regression: a no-brew apply is byte-identical to the realizer-only path.
// ---------------------------------------------------------------------------

// realizerOnlyManifestJSON is a manifest with nix apps + a manual app + a
// homeManager block but NO brew app — the realizer owns all of it.
const realizerOnlyManifestJSON = `{
  "version": 1,
  "name": "no-brew",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "manualapp", "displayName": "Manual App", "manual": { "verifyPath": "/nonexistent/path/manual" } }
  ]
}`

// TestRunApply_NoBrewLane_ByteIdenticalToRealizerOnly proves the two-lane wiring
// is a no-op when no app routes to brew. The COMPARISON side drives the REAL
// RunApply(...) gate in apply.go — partition + shallow-copy + newBrewLane(nil,…)
// — capturing its full JSONL event stream via the newApplyEmitterFn seam, and
// byte-compares it (timestamps normalized) against a realizer-only baseline
// produced by calling runApplyRealizer directly with nil brew. The gate side
// also installs panicBrewDriverFn so any brew-driver resolution fails the test,
// proving the no-brew gate never touches the factory. If the gate's no-brew path
// altered the stream (e.g. by passing a bogus brewApps into the lane), this test
// fails.
func TestRunApply_NoBrewLane_ByteIdenticalToRealizerOnly(t *testing.T) {
	// Replace GOOS placeholder so the nix app resolves on the test host.
	manifestJSON := replaceGOOS(realizerOnlyManifestJSON)
	mfPath := writeTempManifest(t, manifestJSON)

	newFakeRealizer := func() *fakeRealizer {
		return &fakeRealizer{
			planDiff: realizer.Diff{
				ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}},
			},
			realizeResult: realizer.Result{Advanced: true, ToGeneration: 7,
				After: realizer.Set{Elements: map[string]realizer.Element{}}},
			currentSet: realizer.Set{Elements: map[string]realizer.Element{
				"ripgrep": {Name: "ripgrep"},
			}},
		}
	}

	// Disable the module catalog / app synthesis on BOTH sides so mf.Apps and the
	// (nil) configModuleMap/restoreModulesAvailable are identical: the comparison
	// is then apples-to-apples (RunApply skips the whole catalog block when the
	// repo root is empty, exactly as the direct baseline does).
	origRepoRoot := resolveRepoRootFn
	resolveRepoRootFn = func() string { return "" }
	defer func() { resolveRepoRootFn = origRepoRoot }()

	// --- Baseline run: realizer-only, runApplyRealizer DIRECTLY with nil brew ---
	baseRoot := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", baseRoot)
	var baseBuf bytes.Buffer
	baseEmitter := events.NewEmitterWithWriter("apply-fixed", true, &baseBuf)
	frBase := newFakeRealizer()
	mfBase, eerr := loadManifest(mfPath)
	if eerr != nil {
		t.Fatalf("loadManifest: %v", eerr)
	}
	baseResult, baseErr := runApplyRealizer(ApplyFlags{Manifest: mfPath}, mfBase, frBase, baseEmitter, "apply-fixed", nil, nil, nil, nil, nil)
	if baseErr != nil {
		t.Fatalf("baseline apply error: %v", baseErr)
	}

	// --- Two-lane run via the REAL RunApply gate ---
	// Fresh state root so the baseline's provisioning generation doesn't collide.
	laneRoot := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", laneRoot)
	var laneBuf bytes.Buffer
	// The emitter seam forces a deterministic runId AND captures the stream into
	// laneBuf — the only volatile field left is the per-event timestamp.
	origEmitter := newApplyEmitterFn
	newApplyEmitterFn = func(_ string, _ bool) *events.Emitter {
		return events.NewEmitterWithWriter("apply-fixed", true, &laneBuf)
	}
	defer func() { newApplyEmitterFn = origEmitter }()

	frLane := newFakeRealizer()
	var laneResult interface{}
	// panicBrewDriverFn fails the test if the gate ever resolves the brew factory.
	withRealizerAndBrew(frLane, panicBrewDriverFn(t), func() {
		r, e := RunApply(ApplyFlags{Manifest: mfPath, Events: "jsonl"})
		if e != nil {
			t.Fatalf("two-lane RunApply error: %v", e)
		}
		laneResult = r
	})

	// --- Assert ApplyResult equality (unmarshaled JSON, OS-robust) ---
	if !jsonEqual(t, baseResult, laneResult) {
		bb, _ := json.Marshal(baseResult)
		lb, _ := json.Marshal(laneResult)
		t.Errorf("ApplyResult differs between realizer-only baseline and the RunApply gate.\n--- baseline ---\n%s\n--- gate ---\n%s", bb, lb)
	}

	// --- Assert the FULL JSONL event stream matches (timestamps normalized) ---
	// runId is fixed ("apply-fixed") in both emitters; only per-event timestamps
	// are volatile, so strip them before the byte comparison. Everything else —
	// phase order, every item event, every summary — must be identical. The gate
	// emitting an extra/altered event for a no-brew manifest fails this assertion.
	base := normalizeEventStream(t, &baseBuf)
	lane := normalizeEventStream(t, &laneBuf)
	if base != lane {
		t.Errorf("event stream differs (timestamps normalized).\n--- baseline ---\n%s\n--- two-lane gate ---\n%s", base, lane)
	}
}

// TestRunApply_NoBrewLane_GateNeverResolvesBrewDriver proves the apply.go GATE
// path: with a no-brew manifest, RunApply must never call newBrewDriverFn and
// must write exactly ONE nix provisioning generation.
func TestRunApply_NoBrewLane_GateNeverResolvesBrewDriver(t *testing.T) {
	manifestJSON := replaceGOOS(realizerOnlyManifestJSON)
	mfPath := writeTempManifest(t, manifestJSON)
	stateRoot := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", stateRoot)

	fr := &fakeRealizer{
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}},
		realizeResult: realizer.Result{Advanced: true, ToGeneration: 3, After: realizer.Set{Elements: map[string]realizer.Element{}}},
		currentSet:    realizer.Set{Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep"}}},
	}

	withRealizerAndBrew(fr, panicBrewDriverFn(t), func() {
		_, eerr := RunApply(ApplyFlags{Manifest: mfPath, Events: "jsonl"})
		if eerr != nil {
			t.Fatalf("RunApply error: %v", eerr)
		}
	})

	gens, err := provision.List()
	if err != nil {
		t.Fatalf("provision.List: %v", err)
	}
	if len(gens) != 1 {
		t.Fatalf("expected EXACTLY ONE provisioning generation, got %d: %+v", len(gens), gens)
	}
	if gens[0].Backend != "nix" {
		t.Errorf("the single generation must be Backend:nix, got %q", gens[0].Backend)
	}
}

// ---------------------------------------------------------------------------
// Brew apply lane behavior (interleaved with the realizer)
// ---------------------------------------------------------------------------

// TestRunApply_BrewLane_InstallsFormula: a driver:"brew" formula is installed
// via the brew driver, recorded as a separate Backend:"brew" generation, and the
// realizer lane runs alongside it. Exactly TWO generations (nix + brew).
func TestRunApply_BrewLane_InstallsFormula(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-formula",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	stateRoot := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", stateRoot)

	fr := &fakeRealizer{
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}},
		realizeResult: realizer.Result{Advanced: true, ToGeneration: 1, After: realizer.Set{Elements: map[string]realizer.Element{}}},
		currentSet:    realizer.Set{Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep"}}},
	}
	fb := &fakeBrewDriver{installed: map[string]bool{}}

	var result interface{}
	var eerr *envelope.Error
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		r, e := RunApply(ApplyFlags{Manifest: mfPath, Events: "jsonl"})
		result, eerr = r, e
	})
	if eerr != nil {
		t.Fatalf("RunApply error: %v", eerr)
	}

	// brew Install was called for the formula.
	if len(fb.installCalls) != 1 || fb.installCalls[0] != "hello" {
		t.Errorf("brew Install calls = %v, want [hello]", fb.installCalls)
	}

	ar := result.(*ApplyResult)
	if !hasAction(ar.Actions, "hello", "brew", "installed") {
		t.Errorf("expected hello installed via brew in actions, got %+v", ar.Actions)
	}
	if !hasAction(ar.Actions, "ripgrep", "nix", "installed") {
		t.Errorf("expected ripgrep installed via nix in actions, got %+v", ar.Actions)
	}

	gens, _ := provision.List()
	if len(gens) != 2 {
		t.Fatalf("expected TWO generations (nix + brew), got %d: %+v", len(gens), gens)
	}
	backends := map[string]bool{}
	for _, g := range gens {
		backends[g.Backend] = true
	}
	if !backends["nix"] || !backends["brew"] {
		t.Errorf("expected both nix and brew generations, got %+v", backends)
	}
}

// TestRunApply_BrewLane_FailureDoesNotAbortNix: a brew install failure is a
// per-item failed; the nix generation still commits and the run does not error.
func TestRunApply_BrewLane_FailureDoesNotAbortNix(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-fail",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "badpkg", "displayName": "badpkg", "driver": "brew", "refs": { "darwin": "badpkg" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	stateRoot := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", stateRoot)

	fr := &fakeRealizer{
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}},
		realizeResult: realizer.Result{Advanced: true, ToGeneration: 1, After: realizer.Set{Elements: map[string]realizer.Element{}}},
		currentSet:    realizer.Set{Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep"}}},
	}
	fb := &fakeBrewDriver{installed: map[string]bool{}, installFails: map[string]bool{"badpkg": true}}

	var result interface{}
	var eerr *envelope.Error
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		r, e := RunApply(ApplyFlags{Manifest: mfPath, Events: "jsonl"})
		result, eerr = r, e
	})
	if eerr != nil {
		t.Fatalf("RunApply must not top-level error on a per-item brew failure: %v", eerr)
	}

	ar := result.(*ApplyResult)
	if ar.Summary.Failed != 1 {
		t.Errorf("summary.failed = %d, want 1 (the brew failure)", ar.Summary.Failed)
	}
	if !hasAction(ar.Actions, "ripgrep", "nix", "installed") {
		t.Errorf("nix lane must still commit ripgrep despite the brew failure: %+v", ar.Actions)
	}

	// The nix generation must exist (committed). The brew generation is recorded
	// Partial (a failure), but writeProvisioningGeneration only records when an
	// install succeeded OR removed — a pure-failure brew lane installs nothing, so
	// no brew generation is written. Assert the nix generation is present.
	gens, _ := provision.List()
	sawNix := false
	for _, g := range gens {
		if g.Backend == "nix" {
			sawNix = true
		}
	}
	if !sawNix {
		t.Errorf("expected the committed nix generation, got %+v", gens)
	}
}

// TestRunApply_BrewLane_NonDarwinHost_VisibleSkip: when the brew driver is
// unavailable (non-darwin), a driver:"brew" app is surfaced as a skipped/filtered
// item rather than silently dropped.
func TestRunApply_BrewLane_NonDarwinHost_VisibleSkip(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-skip",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	stateRoot := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", stateRoot)

	fr := &fakeRealizer{
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}},
		realizeResult: realizer.Result{Advanced: true, ToGeneration: 1, After: realizer.Set{Elements: map[string]realizer.Element{}}},
		currentSet:    realizer.Set{Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep"}}},
	}

	var result interface{}
	var eerr *envelope.Error
	// brew factory returns ErrNoBrewDriver (the non-darwin host posture).
	withRealizerAndBrew(fr, func() (driver.Driver, error) { return nil, ErrNoBrewDriver }, func() {
		r, e := RunApply(ApplyFlags{Manifest: mfPath, Events: "jsonl"})
		result, eerr = r, e
	})
	if eerr != nil {
		t.Fatalf("RunApply error: %v", eerr)
	}

	ar := result.(*ApplyResult)
	if !hasAction(ar.Actions, "hello", "brew", "skipped") {
		t.Errorf("expected hello as a visible skipped brew item, got %+v", ar.Actions)
	}
}

// ---------------------------------------------------------------------------
// test helpers
// ---------------------------------------------------------------------------

// replaceGOOS substitutes the literal "GOOS" ref key with the current host OS so
// a fixture's nix app resolves on whatever OS the test runs on (linux/darwin/
// windows CI — the realizer functions are invoked directly by these unit tests).
func replaceGOOS(s string) string {
	return replaceAll(s, "GOOS", runtime.GOOS)
}

func replaceAll(s, old, new string) string {
	out := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return out + s
		}
		out += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// normalizeEventStream parses each JSONL event in buf, deletes the volatile
// "timestamp" field, and re-encodes deterministically so two runs can be
// compared structurally (runId is already fixed by the test's emitter).
func normalizeEventStream(t *testing.T, buf *bytes.Buffer) string {
	t.Helper()
	var out bytes.Buffer
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	for dec.More() {
		var obj map[string]interface{}
		if err := dec.Decode(&obj); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		delete(obj, "timestamp")
		enc, err := json.Marshal(obj)
		if err != nil {
			t.Fatalf("encode event: %v", err)
		}
		out.Write(enc)
		out.WriteByte('\n')
	}
	return out.String()
}

// jsonEqual marshals both values and compares the resulting JSON (unmarshaled to
// interface{}), so field order and pointer identity do not matter.
func jsonEqual(t *testing.T, a, b interface{}) bool {
	t.Helper()
	ab, err := json.Marshal(a)
	if err != nil {
		t.Fatalf("marshal a: %v", err)
	}
	bb, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal b: %v", err)
	}
	var av, bv interface{}
	_ = json.Unmarshal(ab, &av)
	_ = json.Unmarshal(bb, &bv)
	return deepEqualJSON(av, bv)
}

func deepEqualJSON(a, b interface{}) bool {
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return bytes.Equal(ab, bb)
}

// hasAction reports whether actions contains an entry with the given id, driver,
// and status.
func hasAction(actions []ApplyAction, id, drv, status string) bool {
	for _, a := range actions {
		if a.ID == id && a.Driver == drv && a.Status == status {
			return true
		}
	}
	return false
}
