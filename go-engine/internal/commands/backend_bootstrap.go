// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
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
	Granted          bool
	Denied           bool
	StructuredEvents bool
}

// bootstrapConsent derives the Consent from apply flags. An explicit --no-bootstrap
// takes precedence over --bootstrap-backends (a conservative "no wins").
func bootstrapConsent(flags ApplyFlags) Consent {
	return Consent{
		Granted:          flags.BootstrapBackends && !flags.NoBootstrap,
		Denied:           flags.NoBootstrap,
		StructuredEvents: flags.Events == "jsonl",
	}
}

const installerDiagnosticLimit = 4096

type installerDiagnosticBuffer struct {
	data      []byte
	truncated bool
}

func (b *installerDiagnosticBuffer) Write(p []byte) (int, error) {
	written := len(p)
	remaining := installerDiagnosticLimit - len(b.data)
	if remaining > 0 {
		if len(p) > remaining {
			p = p[:remaining]
		}
		b.data = append(b.data, p...)
	}
	if written > remaining {
		b.truncated = true
	}
	return written, nil
}

func (b *installerDiagnosticBuffer) String() string {
	text := strings.TrimSpace(strings.ToValidUTF8(string(b.data), "?"))
	if b.truncated {
		if text != "" {
			text += "\n"
		}
		text += "[installer output truncated]"
	}
	return text
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

// bootstrapGOOSFn reports the host OS used to decide which backends are
// bootstrappable (brew → darwin, nix → linux/darwin, chocolatey → Windows). It
// defaults to runtime.GOOS and is a test seam (mirroring captureGOOSFn) so the
// realEnsureBackends branch tests run host-independently on every CI OS — without
// it, those tests short-circuit on a Windows runner where no backend is
// bootstrappable.
var bootstrapGOOSFn = func() string { return runtime.GOOS }

// resolveReadOnlyBrewDriver probes Brew without installing or requesting
// consent, then constructs the driver only when the backend is actually
// available. Plan and verify use this to distinguish an absent package manager
// from an installed manager reporting an absent package.
func resolveReadOnlyBrewDriver(needed bool, emitter *events.Emitter) driver.Driver {
	if !needed {
		return nil
	}
	available, eerr := bootstrapBackendsFn(
		[]bootstrap.Backend{bootstrap.BackendBrew},
		false,
		Consent{},
		emitter,
	)
	if eerr != nil || !available[bootstrap.BackendBrew] {
		return nil
	}
	drv, err := newBrewDriverFn()
	if err != nil {
		return nil
	}
	return drv
}

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
// A backend that is not bootstrappable on the host (e.g. brew off darwin) is
// simply absent from the returned map, so its lane falls
// back to the existing factory gate (which no-ops it) unchanged.
func realEnsureBackends(needed []bootstrap.Backend, mutating bool, consent Consent, emitter *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
	goos := bootstrapGOOSFn()
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
	// With JSONL events, stderr is a framed machine-readable stream. Keep stdin
	// attached for official installer prompts, but capture both output streams so
	// raw installer chatter cannot corrupt framing.
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
		for _, b := range absent {
			var diagnostic *installerDiagnosticBuffer
			if consent.StructuredEvents {
				diagnostic = &installerDiagnosticBuffer{}
				bs.ConfigureInstallerIO(os.Stdin, diagnostic, diagnostic)
			}
			o := bs.Provision([]bootstrap.Backend{b})[b] // each backend remains independent
			available[b] = o.Available()
			if diagnostic != nil && !o.Available() {
				structuredDetail := &installerDiagnosticBuffer{}
				if provisionErr := bs.ProvisionError(b); provisionErr != nil {
					_, _ = structuredDetail.Write([]byte(provisionErr.Error()))
				}
				if output := diagnostic.String(); output != "" {
					if len(structuredDetail.data) > 0 {
						_, _ = structuredDetail.Write([]byte("\n"))
					}
					_, _ = structuredDetail.Write([]byte(output))
				}
				if detail := structuredDetail.String(); detail != "" {
					emitter.EmitError("engine", fmt.Sprintf("%s backend setup failed: %s", b, detail), "")
				}
			}
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
// linux/darwin; Windows may bootstrap Chocolatey while Winget ships with the OS.
func bootstrappableOn(goos string, b bootstrap.Backend) bool {
	switch b {
	case bootstrap.BackendBrew:
		return goos == "darwin"
	case bootstrap.BackendNix:
		return goos == "linux" || goos == "darwin"
	case bootstrap.BackendChocolatey:
		return goos == "windows"
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
