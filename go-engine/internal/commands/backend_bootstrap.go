// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"runtime"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
)

// This file wires the engine-installed-backend bootstrap (internal/bootstrap)
// as a pre-step in front of the backend factory gate. It is unrelated to the
// `endstate bootstrap` self-install command in bootstrap.go.

// Consent carries the user's backend-bootstrap decision, resolved from the CLI
// flags. Granted is --bootstrap-backends (authorize installing absent backends);
// Denied is --no-bootstrap (force skip). Neither set means "not yet answered":
// the engine emits a consent-request event and defaults to skipping the lane.
type Consent struct {
	Granted bool
	Denied  bool
}

// bootstrapConsent derives the Consent from apply flags. An explicit --no-bootstrap
// takes precedence over --bootstrap-backends (a conservative "no wins").
func bootstrapConsent(flags ApplyFlags) Consent {
	return Consent{Granted: flags.BootstrapBackends && !flags.NoBootstrap, Denied: flags.NoBootstrap}
}

// bootstrapBackendsFn is the injectable pre-step seam run in front of the backend
// factory gate (newRealizerFn / newBrewDriverFn). It detects each needed backend
// and, on the mutating apply command, drives consent → official install → verify;
// it returns which backends are available to resolve. The commands package
// TestMain defaults it to a present/available no-op so existing tests are
// byte-identical; production wires realEnsureBackends.
var bootstrapBackendsFn = realEnsureBackends

// newBootstrapperFn constructs the bootstrap mechanism realEnsureBackends drives.
// It defaults to the real detect/install/verify seams; tests override it with a
// fake Bootstrapper so the absent → consent → install → verify branches are
// exercised hermetically (the real installer is never shelled in `go test`).
var newBootstrapperFn = bootstrap.New

// realEnsureBackends is the production pre-step. For each needed backend that is
// bootstrappable on the host OS it runs detect → (consent → install → verify):
//   - present → available (no prompt, no install);
//   - absent + read-only command (mutating=false) → not available, NO install,
//     NO consent request (you do not install a package manager to read state);
//   - absent + apply + consent granted → install via the OFFICIAL installer, then
//     verify; available only if the verify probe passes;
//   - absent + apply + no consent → emit ONE combined consent-request event and
//     default to skipping (not available); explicit --no-bootstrap skips silently.
//
// A backend that is not bootstrappable on the host (e.g. brew off darwin, or any
// backend on Windows) is simply absent from the returned map, so its lane falls
// back to the existing factory gate (which no-ops it) unchanged.
func realEnsureBackends(needed []bootstrap.Backend, mutating bool, consent Consent, emitter *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
	goos := runtime.GOOS
	available := map[bootstrap.Backend]bool{}

	relevant := make([]bootstrap.Backend, 0, len(needed))
	for _, b := range needed {
		if bootstrappableOn(goos, b) {
			relevant = append(relevant, b)
		}
	}
	if len(relevant) == 0 {
		return available, nil
	}

	bs := newBootstrapperFn()
	absent, present := bs.Probe(relevant)
	for _, b := range present {
		available[b] = true // present and working → use directly, never re-install
	}
	if len(absent) == 0 {
		return available, nil
	}

	// Read-only commands never install: report the absent backends as unavailable
	// without prompting. Their lanes fall back to detect/skip in the existing path.
	if !mutating {
		for _, b := range absent {
			available[b] = false
		}
		return available, nil
	}

	switch {
	case consent.Granted:
		outcomes := bs.Provision(absent) // install + verify each, independently
		for b, o := range outcomes {
			available[b] = o.Available()
		}
	case consent.Denied:
		// Explicit skip: the user declined up front. Lane(s) skipped, run continues.
		for _, b := range absent {
			available[b] = false
		}
	default:
		// Not yet answered: emit ONE combined consent-request and default to skip.
		emitter.EmitConsent(backendStrings(absent), bootstrap.ConsentMessage(absent), installerCommands(absent))
		for _, b := range absent {
			available[b] = false
		}
	}
	return available, nil
}

// bootstrappableOn reports whether a backend can be bootstrapped (and used) on the
// given host OS. The Homebrew driver is darwin-only; the Nix realizer is
// linux/darwin; Windows bootstraps nothing (winget ships with the OS).
func bootstrappableOn(goos string, b bootstrap.Backend) bool {
	switch b {
	case bootstrap.BackendBrew:
		return goos == "darwin"
	case bootstrap.BackendNix:
		return goos == "linux" || goos == "darwin"
	default:
		return false
	}
}

// backendStrings renders backend identifiers for the consent event's structured
// backends field.
func backendStrings(bs []bootstrap.Backend) []string {
	out := make([]string, len(bs))
	for i, b := range bs {
		out[i] = string(b)
	}
	return out
}

// installerCommands renders the exact, inspectable installer commands for the
// consent event's details field (the "what will run" affordance).
func installerCommands(bs []bootstrap.Backend) []string {
	out := make([]string, 0, len(bs))
	for _, b := range bs {
		if cmd := bootstrap.InstallerCommand(b); cmd != "" {
			out = append(out, cmd)
		}
	}
	return out
}
