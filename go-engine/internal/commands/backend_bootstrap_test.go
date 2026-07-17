// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestRealEnsureBackends_JSONLEventsCaptureInstallerFailureOutput(t *testing.T) {
	bs := &bootstrap.Bootstrapper{}
	bs.Detect = func(bootstrap.Backend) (bool, error) { return false, nil }
	bs.Install = func(bootstrap.Backend) error {
		if bs.InstallerStdin != os.Stdin {
			t.Fatalf("installer stdin = %v, want os.Stdin", bs.InstallerStdin)
		}
		_, _ = fmt.Fprint(bs.InstallerStdout, "raw installer stdout")
		_, _ = fmt.Fprint(bs.InstallerStderr, "raw installer stderr")
		return errors.New("installer failed")
	}
	bs.Verify = func(bootstrap.Backend) (bool, error) {
		t.Fatal("verify must not run after installer failure")
		return false, nil
	}

	emitter, eventBuf := captureBootstrapEmitter()
	withBootstrapperOn("windows", bs, func() {
		available, eerr := realEnsureBackends(
			[]bootstrap.Backend{bootstrap.BackendChocolatey},
			true,
			Consent{Granted: true, StructuredEvents: true},
			emitter,
		)
		if eerr != nil {
			t.Fatalf("realEnsureBackends error = %v", eerr)
		}
		if available[bootstrap.BackendChocolatey] {
			t.Fatal("failed installer must leave Chocolatey unavailable")
		}
	})

	lines := strings.Split(strings.TrimSpace(eventBuf.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("event lines = %q, want one structured error", eventBuf.String())
	}
	var event map[string]interface{}
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("installer output corrupted JSONL: %v; raw=%q", err, eventBuf.String())
	}
	if event["event"] != "error" || event["scope"] != "engine" {
		t.Fatalf("event = %v, want engine error", event)
	}
	message, _ := event["message"].(string)
	if !strings.Contains(message, "installer failed") || !strings.Contains(message, "raw installer stdout") || !strings.Contains(message, "raw installer stderr") {
		t.Fatalf("structured diagnostic message = %q", message)
	}
}

func TestRealEnsureBackends_JSONLEventsSuppressSuccessfulInstallerChatter(t *testing.T) {
	bs := &bootstrap.Bootstrapper{}
	bs.Detect = func(bootstrap.Backend) (bool, error) { return false, nil }
	bs.Install = func(bootstrap.Backend) error {
		_, _ = fmt.Fprint(bs.InstallerStdout, "successful installer chatter")
		return nil
	}
	bs.Verify = func(bootstrap.Backend) (bool, error) { return true, nil }

	emitter, eventBuf := captureBootstrapEmitter()
	withBootstrapperOn("windows", bs, func() {
		available, _ := realEnsureBackends(
			[]bootstrap.Backend{bootstrap.BackendChocolatey},
			true,
			Consent{Granted: true, StructuredEvents: true},
			emitter,
		)
		if !available[bootstrap.BackendChocolatey] {
			t.Fatal("successful installer and verifier must make Chocolatey available")
		}
	})
	if eventBuf.Len() != 0 {
		t.Fatalf("successful installer chatter must not enter JSONL events: %q", eventBuf.String())
	}
}

func TestRealEnsureBackends_JSONLInstallerDiagnosticIsBounded(t *testing.T) {
	bs := &bootstrap.Bootstrapper{}
	bs.Detect = func(bootstrap.Backend) (bool, error) { return false, nil }
	bs.Install = func(bootstrap.Backend) error {
		_, _ = bs.InstallerStdout.Write(bytes.Repeat([]byte{0xff, 'x'}, installerDiagnosticLimit))
		return errors.New("installer failed")
	}
	bs.Verify = func(bootstrap.Backend) (bool, error) { return false, nil }

	emitter, eventBuf := captureBootstrapEmitter()
	withBootstrapperOn("windows", bs, func() {
		_, _ = realEnsureBackends(
			[]bootstrap.Backend{bootstrap.BackendChocolatey},
			true,
			Consent{Granted: true, StructuredEvents: true},
			emitter,
		)
	})
	if eventBuf.Len() > installerDiagnosticLimit+1024 {
		t.Fatalf("bounded diagnostic event is %d bytes", eventBuf.Len())
	}
	if !strings.Contains(eventBuf.String(), "truncated") {
		t.Fatalf("bounded diagnostic must disclose truncation: %q", eventBuf.String())
	}
}

// fakeBootstrapper builds a *bootstrap.Bootstrapper with scriptable seams and
// records install/verify calls, so realEnsureBackends is exercised hermetically
// (no real installer). present controls the detect probe.
func fakeBootstrapper(present bool, installErr error, verifyOK bool) (*bootstrap.Bootstrapper, *[]string) {
	var calls []string
	bs := &bootstrap.Bootstrapper{
		Detect: func(b bootstrap.Backend) (bool, error) { return present, nil },
		Install: func(b bootstrap.Backend) error {
			calls = append(calls, "install:"+string(b))
			return installErr
		},
		Verify: func(b bootstrap.Backend) (bool, error) {
			calls = append(calls, "verify:"+string(b))
			return verifyOK, nil
		},
	}
	return bs, &calls
}

// withBootstrapper overrides the newBootstrapperFn seam for the duration of f and
// pins the bootstrap GOOS to a Unix host (linux), so the nix-backend branch tests
// run identically on every CI OS — including the Windows runner, where nix is not
// bootstrappable and realEnsureBackends would otherwise short-circuit.
func withBootstrapper(bs *bootstrap.Bootstrapper, f func()) {
	withBootstrapperOn("linux", bs, f)
}

// withBootstrapperOn is withBootstrapper with an explicit pinned host OS (e.g.
// "darwin" to exercise the brew-bootstrappable path).
func withBootstrapperOn(goos string, bs *bootstrap.Bootstrapper, f func()) {
	origBs := newBootstrapperFn
	origGOOS := bootstrapGOOSFn
	newBootstrapperFn = func() *bootstrap.Bootstrapper { return bs }
	bootstrapGOOSFn = func() string { return goos }
	defer func() {
		newBootstrapperFn = origBs
		bootstrapGOOSFn = origGOOS
	}()
	f()
}

// captureBootstrapEmitter returns an enabled emitter writing into buf.
func captureBootstrapEmitter() (*events.Emitter, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	return events.NewEmitterWithWriter("test-run", true, buf), buf
}

// consentLines returns the parsed "consent" events emitted into buf.
func consentLines(t *testing.T, buf *bytes.Buffer) []map[string]interface{} {
	t.Helper()
	var out []map[string]interface{}
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m map[string]interface{}
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("event is not valid JSON: %v\nraw: %q", err, line)
		}
		if m["event"] == "consent" {
			out = append(out, m)
		}
	}
	return out
}

// nix is bootstrappable on both the linux dev box and darwin CI, so these tests
// are host-independent.

func TestRealEnsureBackends_PresentNoConsentNoInstall(t *testing.T) {
	bs, calls := fakeBootstrapper(true /*present*/, nil, true)
	em, buf := captureBootstrapEmitter()
	withBootstrapper(bs, func() {
		avail, eerr := realEnsureBackends([]bootstrap.Backend{bootstrap.BackendNix}, true, Consent{}, em)
		if eerr != nil {
			t.Fatalf("unexpected error: %v", eerr)
		}
		if !avail[bootstrap.BackendNix] {
			t.Fatal("present backend must be available")
		}
	})
	if len(*calls) != 0 {
		t.Fatalf("present backend must not install/verify, calls = %v", *calls)
	}
	if got := consentLines(t, buf); len(got) != 0 {
		t.Fatalf("present backend must not emit a consent event, got %d", len(got))
	}
}

func TestRealEnsureBackends_AbsentNoConsent_EmitsRequestAndSkips(t *testing.T) {
	bs, calls := fakeBootstrapper(false /*absent*/, nil, true)
	em, buf := captureBootstrapEmitter()
	withBootstrapper(bs, func() {
		avail, eerr := realEnsureBackends([]bootstrap.Backend{bootstrap.BackendNix}, true, Consent{}, em)
		if eerr != nil {
			t.Fatalf("unexpected error: %v", eerr)
		}
		if avail[bootstrap.BackendNix] {
			t.Fatal("absent + no consent must NOT be available")
		}
	})
	if len(*calls) != 0 {
		t.Fatalf("no consent must not install, calls = %v", *calls)
	}
	ev := consentLines(t, buf)
	if len(ev) != 1 {
		t.Fatalf("want exactly one consent event, got %d", len(ev))
	}
	backends, _ := ev[0]["backends"].([]interface{})
	if len(backends) != 1 || backends[0] != "nix" {
		t.Fatalf("consent backends = %v, want [nix]", ev[0]["backends"])
	}
	msg, _ := ev[0]["message"].(string)
	if msg == "" || strings.Contains(strings.ToLower(msg), "nix") {
		t.Fatalf("consent message must be present and product-neutral: %q", msg)
	}
	details, _ := ev[0]["details"].([]interface{})
	if len(details) != 1 {
		t.Fatalf("consent details must carry the installer command, got %v", ev[0]["details"])
	}
}

func TestRealEnsureBackends_AbsentGranted_InstallsAndVerifies(t *testing.T) {
	bs, calls := fakeBootstrapper(false, nil, true /*verify ok*/)
	em, buf := captureBootstrapEmitter()
	withBootstrapper(bs, func() {
		avail, eerr := realEnsureBackends([]bootstrap.Backend{bootstrap.BackendNix}, true, Consent{Granted: true}, em)
		if eerr != nil {
			t.Fatalf("unexpected error: %v", eerr)
		}
		if !avail[bootstrap.BackendNix] {
			t.Fatal("absent + granted + verify-ok must be available")
		}
	})
	if strings.Join(*calls, ",") != "install:nix,verify:nix" {
		t.Fatalf("calls = %v, want install then verify", *calls)
	}
	if got := consentLines(t, buf); len(got) != 0 {
		t.Fatalf("granted consent must not emit a consent-request, got %d", len(got))
	}
}

func TestRealEnsureBackends_AbsentGranted_VerifyFailUnavailable(t *testing.T) {
	bs, _ := fakeBootstrapper(false, nil, false /*verify fails*/)
	em, _ := captureBootstrapEmitter()
	withBootstrapper(bs, func() {
		avail, eerr := realEnsureBackends([]bootstrap.Backend{bootstrap.BackendNix}, true, Consent{Granted: true}, em)
		if eerr != nil {
			t.Fatalf("unexpected error: %v", eerr)
		}
		if avail[bootstrap.BackendNix] {
			t.Fatal("install ok but verify fail must be UNAVAILABLE")
		}
	})
}

func TestRealEnsureBackends_AbsentGranted_InstallFailUnavailable(t *testing.T) {
	bs, _ := fakeBootstrapper(false, errors.New("install boom"), true)
	em, _ := captureBootstrapEmitter()
	withBootstrapper(bs, func() {
		avail, _ := realEnsureBackends([]bootstrap.Backend{bootstrap.BackendNix}, true, Consent{Granted: true}, em)
		if avail[bootstrap.BackendNix] {
			t.Fatal("install failure must be UNAVAILABLE")
		}
	})
}

func TestRealEnsureBackends_AbsentDenied_SkipsNoEvent(t *testing.T) {
	bs, calls := fakeBootstrapper(false, nil, true)
	em, buf := captureBootstrapEmitter()
	withBootstrapper(bs, func() {
		avail, _ := realEnsureBackends([]bootstrap.Backend{bootstrap.BackendNix}, true, Consent{Denied: true}, em)
		if avail[bootstrap.BackendNix] {
			t.Fatal("explicit --no-bootstrap must skip (unavailable)")
		}
	})
	if len(*calls) != 0 {
		t.Fatalf("denied must not install, calls = %v", *calls)
	}
	if got := consentLines(t, buf); len(got) != 0 {
		t.Fatalf("explicit decline must not re-request consent, got %d", len(got))
	}
}

func TestRealEnsureBackends_ReadOnlyAbsent_NoInstallNoConsent(t *testing.T) {
	bs, calls := fakeBootstrapper(false, nil, true)
	em, buf := captureBootstrapEmitter()
	withBootstrapper(bs, func() {
		avail, _ := realEnsureBackends([]bootstrap.Backend{bootstrap.BackendNix}, false /*read-only*/, Consent{}, em)
		if avail[bootstrap.BackendNix] {
			t.Fatal("read-only must not make an absent backend available")
		}
	})
	if len(*calls) != 0 {
		t.Fatalf("read-only must never install, calls = %v", *calls)
	}
	if got := consentLines(t, buf); len(got) != 0 {
		t.Fatalf("read-only must not request install consent, got %d", len(got))
	}
}

func TestBootstrappableOn_HostMatrix(t *testing.T) {
	cases := []struct {
		goos string
		b    bootstrap.Backend
		want bool
	}{
		{"darwin", bootstrap.BackendBrew, true},
		{"linux", bootstrap.BackendBrew, false}, // brew driver is darwin-only
		{"windows", bootstrap.BackendBrew, false},
		{"darwin", bootstrap.BackendNix, true},
		{"linux", bootstrap.BackendNix, true},
		{"windows", bootstrap.BackendNix, false}, // winget ships with the OS
		{"windows", bootstrap.BackendChocolatey, true},
		{"linux", bootstrap.BackendChocolatey, false},
		{"darwin", bootstrap.BackendChocolatey, false},
	}
	for _, c := range cases {
		if got := bootstrappableOn(c.goos, c.b); got != c.want {
			t.Errorf("bootstrappableOn(%q, %q) = %v, want %v", c.goos, c.b, got, c.want)
		}
	}
}

// withBootstrapAvail overrides the bootstrapBackendsFn pre-step seam to return a
// fixed availability map, so the apply-gate wiring can be tested independently of
// the real detect/install path.
func withBootstrapAvail(avail map[bootstrap.Backend]bool, f func()) {
	orig := bootstrapBackendsFn
	bootstrapBackendsFn = func(_ []bootstrap.Backend, _ bool, _ Consent, _ *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		return avail, nil
	}
	defer func() { bootstrapBackendsFn = orig }()
	f()
}

// TestRunApply_BrewLane_DeclinedBootstrap_SkipsBrewContinuesRun proves the apply
// GATE: when the brew bootstrap pre-step reports brew unavailable (declined), the
// brew factory is NEVER resolved (panicBrewDriverFn would fail the test), the brew
// app is a visible skip, the realizer lane still installs, the run does not error,
// and exactly ONE (nix) provisioning generation is written — declined brew behaves
// like a no-brew manifest.
func TestRunApply_BrewLane_DeclinedBootstrap_SkipsBrewContinuesRun(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-declined",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	fr := &fakeRealizer{
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}},
		realizeResult: realizer.Result{Advanced: true, ToGeneration: 1, After: realizer.Set{Elements: map[string]realizer.Element{}}},
		currentSet:    realizer.Set{Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep"}}},
	}

	var result interface{}
	var eerr *envelope.Error
	// Nix available (realizer lane runs), brew declined (lane skipped).
	withBootstrapAvail(map[bootstrap.Backend]bool{bootstrap.BackendNix: true, bootstrap.BackendBrew: false}, func() {
		// panicBrewDriverFn fails the test if the gate resolves the brew factory.
		withRealizerAndBrew(fr, panicBrewDriverFn(t), func() {
			r, e := RunApply(ApplyFlags{Manifest: mfPath, Events: "jsonl", NoBootstrap: true})
			result, eerr = r, e
		})
	})
	if eerr != nil {
		t.Fatalf("declined bootstrap must not error the run: %v", eerr)
	}
	ar := result.(*ApplyResult)
	if !hasAction(ar.Actions, "hello", "brew", "skipped") {
		t.Errorf("declined brew app must be a visible skip, got %+v", ar.Actions)
	}
	if !hasAction(ar.Actions, "ripgrep", "nix", "installed") {
		t.Errorf("realizer lane must still install when brew is declined, got %+v", ar.Actions)
	}
	gens, _ := provision.List()
	if len(gens) != 1 || gens[0].Backend != "nix" {
		t.Fatalf("declined brew → exactly one nix generation, got %+v", gens)
	}
}

// TestRunApply_NixDeclined_BrewOnlyPath proves the PR2 declined-Nix restructuring:
// when Nix is needed but unavailable and brew is available, the realizer-lane app
// is a visible skip, the brew app STILL installs via the standalone brew-only path,
// the run does not error, and exactly ONE (brew) provisioning generation is written
// (no nix generation).
func TestRunApply_NixDeclined_BrewOnlyPath(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "nix-declined-brew-ok",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	fr := &fakeRealizer{} // never used: Nix is unavailable, brew-only path runs
	fb := &fakeBrewDriver{installed: map[string]bool{}}

	var result interface{}
	var eerr *envelope.Error
	withBootstrapAvail(map[bootstrap.Backend]bool{bootstrap.BackendNix: false, bootstrap.BackendBrew: true}, func() {
		withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
			r, e := RunApply(ApplyFlags{Manifest: mfPath, Events: "jsonl", BootstrapBackends: true})
			result, eerr = r, e
		})
	})
	if eerr != nil {
		t.Fatalf("declined Nix must not error the run: %v", eerr)
	}
	ar := result.(*ApplyResult)
	if !hasAction(ar.Actions, "ripgrep", "nix", "skipped") {
		t.Errorf("Nix-ref app must be a visible skip when Nix is unavailable, got %+v", ar.Actions)
	}
	if !hasAction(ar.Actions, "hello", "brew", "installed") {
		t.Errorf("brew app must still install on the brew-only path, got %+v", ar.Actions)
	}
	if len(fb.installCalls) != 1 || fb.installCalls[0] != "hello" {
		t.Errorf("brew Install calls = %v, want [hello]", fb.installCalls)
	}
	gens, _ := provision.List()
	if len(gens) != 1 || gens[0].Backend != "brew" {
		t.Fatalf("declined Nix + brew install → exactly one brew generation, got %+v", gens)
	}
}

// TestRunApply_NixOnlyDeclined_NoCrashAllSkipped proves a Nix-only manifest with
// Nix declined does not crash: the app is skipped, no generation is written, and
// the run returns a (successful) result rather than a top-level error.
func TestRunApply_NixOnlyDeclined_NoCrashAllSkipped(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "nix-only-declined",
  "apps": [ { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } } ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	fr := &fakeRealizer{}
	var result interface{}
	var eerr *envelope.Error
	withBootstrapAvail(map[bootstrap.Backend]bool{bootstrap.BackendNix: false}, func() {
		// No brew app → the brew factory must never resolve.
		withRealizerAndBrew(fr, panicBrewDriverFn(t), func() {
			r, e := RunApply(ApplyFlags{Manifest: mfPath, Events: "jsonl", NoBootstrap: true})
			result, eerr = r, e
		})
	})
	if eerr != nil {
		t.Fatalf("Nix-only declined must not crash: %v", eerr)
	}
	ar := result.(*ApplyResult)
	if !hasAction(ar.Actions, "ripgrep", "nix", "skipped") {
		t.Errorf("the only app must be skipped, got %+v", ar.Actions)
	}
	gens, _ := provision.List()
	if len(gens) != 0 {
		t.Fatalf("nothing installed → no provisioning generation, got %+v", gens)
	}
}

// TestRunApply_CombinedConsent_OneProbeForBothBackends proves the apply gate makes
// a SINGLE bootstrap pre-step call carrying the combined needed set (one consent),
// not one call per backend.
func TestRunApply_CombinedConsent_OneProbeForBothBackends(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "combined",
  "apps": [
    { "id": "ripgrep", "displayName": "ripgrep", "refs": { "GOOS": "nixpkgs#ripgrep" } },
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ]
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	fr := &fakeRealizer{
		planDiff:      realizer.Diff{ToAdd: []realizer.Installable{{ID: "ripgrep", Ref: "nixpkgs#ripgrep"}}},
		realizeResult: realizer.Result{Advanced: true, ToGeneration: 1, After: realizer.Set{Elements: map[string]realizer.Element{}}},
		currentSet:    realizer.Set{Elements: map[string]realizer.Element{"ripgrep": {Name: "ripgrep"}}},
	}
	fb := &fakeBrewDriver{installed: map[string]bool{}}

	var calls int
	var captured []bootstrap.Backend
	orig := bootstrapBackendsFn
	bootstrapBackendsFn = func(needed []bootstrap.Backend, _ bool, _ Consent, _ *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		calls++
		captured = needed
		return map[bootstrap.Backend]bool{bootstrap.BackendNix: true, bootstrap.BackendBrew: true}, nil
	}
	defer func() { bootstrapBackendsFn = orig }()

	withRealizerAndBrew(fr, func() (driver.Driver, error) { return fb, nil }, func() {
		if _, e := RunApply(ApplyFlags{Manifest: mfPath, Events: "jsonl", BootstrapBackends: true}); e != nil {
			t.Fatalf("RunApply error: %v", e)
		}
	})
	if calls != 1 {
		t.Fatalf("bootstrap pre-step called %d times, want exactly 1 (combined consent)", calls)
	}
	has := map[bootstrap.Backend]bool{}
	for _, b := range captured {
		has[b] = true
	}
	if !has[bootstrap.BackendNix] || !has[bootstrap.BackendBrew] {
		t.Fatalf("combined needed set = %v, want both nix and brew", captured)
	}
}

// A Brew-only package manifest still needs Nix when the opt-in Home Manager
// config stage is active. The combined bootstrap probe must therefore include
// both backends instead of incorrectly taking the Brew-only path.
func TestRunApply_BrewOnlyWithHomeManagerRestoreNeedsNixAndBrew(t *testing.T) {
	manifestJSON := replaceGOOS(`{
  "version": 1, "name": "brew-with-home-manager",
  "apps": [
    { "id": "hello", "displayName": "hello", "driver": "brew", "refs": { "darwin": "hello" } }
  ],
  "homeManager": { "flake": "github:example/dotfiles#me" }
}`)
	mfPath := writeTempManifest(t, manifestJSON)
	t.Setenv("ENDSTATE_ROOT", t.TempDir())

	var captured []bootstrap.Backend
	orig := bootstrapBackendsFn
	bootstrapBackendsFn = func(needed []bootstrap.Backend, _ bool, _ Consent, _ *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
		captured = append([]bootstrap.Backend(nil), needed...)
		return map[bootstrap.Backend]bool{}, nil
	}
	defer func() { bootstrapBackendsFn = orig }()

	withRealizerAndBrew(&fakeRealizer{}, panicBrewDriverFn(t), func() {
		if _, eerr := RunApply(ApplyFlags{Manifest: mfPath, EnableRestore: true, NoBootstrap: true}); eerr != nil {
			t.Fatalf("RunApply error: %v", eerr)
		}
	})

	want := map[bootstrap.Backend]bool{bootstrap.BackendNix: true, bootstrap.BackendBrew: true}
	if len(captured) != len(want) {
		t.Fatalf("needed backends = %v, want nix and brew", captured)
	}
	for _, backend := range captured {
		if !want[backend] {
			t.Fatalf("needed backends = %v, unexpected %q", captured, backend)
		}
	}
}

// TestRealEnsureBackends_CombinedConsentOneEventBothBackends validates the real
// pre-step emits a SINGLE consent event covering both absent backends. It pins the
// host to darwin (both nix and brew bootstrappable) so it runs on every CI OS.
func TestRealEnsureBackends_CombinedConsentOneEventBothBackends(t *testing.T) {
	bs := &bootstrap.Bootstrapper{
		Detect:  func(b bootstrap.Backend) (bool, error) { return false, nil }, // both absent
		Install: func(b bootstrap.Backend) error { return nil },
		Verify:  func(b bootstrap.Backend) (bool, error) { return true, nil },
	}
	em, buf := captureBootstrapEmitter()
	withBootstrapperOn("darwin", bs, func() {
		_, _ = realEnsureBackends([]bootstrap.Backend{bootstrap.BackendNix, bootstrap.BackendBrew}, true, Consent{}, em)
	})
	ev := consentLines(t, buf)
	if len(ev) != 1 {
		t.Fatalf("combined consent must emit exactly one event, got %d", len(ev))
	}
	backends, _ := ev[0]["backends"].([]interface{})
	if len(backends) != 2 {
		t.Fatalf("combined consent event backends = %v, want both", ev[0]["backends"])
	}
}

func TestBootstrapConsent_FlagMapping(t *testing.T) {
	if c := bootstrapConsent(ApplyFlags{BootstrapBackends: true}); !c.Granted || c.Denied {
		t.Fatalf("--bootstrap-backends → %+v, want Granted", c)
	}
	if c := bootstrapConsent(ApplyFlags{NoBootstrap: true}); c.Granted || !c.Denied {
		t.Fatalf("--no-bootstrap → %+v, want Denied", c)
	}
	// Conflicting flags: --no-bootstrap wins (conservative "no").
	if c := bootstrapConsent(ApplyFlags{BootstrapBackends: true, NoBootstrap: true}); c.Granted || !c.Denied {
		t.Fatalf("both flags → %+v, want Denied wins", c)
	}
	if c := bootstrapConsent(ApplyFlags{}); c.Granted || c.Denied {
		t.Fatalf("no flag → %+v, want neither", c)
	}
	if c := bootstrapConsent(ApplyFlags{Events: "jsonl"}); !c.StructuredEvents {
		t.Fatalf("--events jsonl → %+v, want StructuredEvents", c)
	}
}
