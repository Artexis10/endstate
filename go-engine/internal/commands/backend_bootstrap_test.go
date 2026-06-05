// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

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

// withBootstrapper overrides the newBootstrapperFn seam for the duration of f.
func withBootstrapper(bs *bootstrap.Bootstrapper, f func()) {
	orig := newBootstrapperFn
	newBootstrapperFn = func() *bootstrap.Bootstrapper { return bs }
	defer func() { newBootstrapperFn = orig }()
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
	withBootstrapAvail(map[bootstrap.Backend]bool{bootstrap.BackendBrew: false}, func() {
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
}
