// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bootstrap"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// presentBootstrapFn is the default backend-bootstrap pre-step fake: it reports
// every needed backend as present/available, so the real detect/install/verify
// path never runs under `go test` and the factory gate (newRealizerFn /
// newBrewDriverFn) still decides resolution exactly as before this change. Tests
// that exercise decline/skip/install wiring override bootstrapBackendsFn locally.
func presentBootstrapFn(needed []bootstrap.Backend, _ bool, _ Consent, _ *events.Emitter) (map[bootstrap.Backend]bool, *envelope.Error) {
	avail := make(map[bootstrap.Backend]bool, len(needed))
	for _, b := range needed {
		avail[b] = true
	}
	return avail, nil
}

// TestMain keeps the commands package tests hermetic. Several tests drive a
// successful apply, which records a Provisioning Generation under the resolved
// state directory. Without an explicit ENDSTATE_ROOT those writes would land in
// ./state under the package directory; routing them to a throwaway directory
// keeps the working tree clean. Tests that set their own ENDSTATE_ROOT via
// t.Setenv still override this per-test.
func TestMain(m *testing.M) {
	cleanup := func() {}
	if os.Getenv("ENDSTATE_ROOT") == "" {
		if dir, err := os.MkdirTemp("", "endstate-commands-test-*"); err == nil {
			_ = os.Setenv("ENDSTATE_ROOT", dir)
			cleanup = func() { _ = os.RemoveAll(dir) }
		}
	}
	// The brew driver lane (apply/capture/verify/plan) is darwin-only and resolves
	// the REAL Homebrew driver via newBrewDriverFn when the host is darwin. On a
	// macOS CI runner that would shell out to real `brew` — e.g. capture's
	// EnumerateInstalled returns the runner's ~47 preinstalled formulae/casks,
	// breaking every realizer-capture count assertion. Pin the default to the
	// fail-if-resolved fake so the brew lane is inert by default; tests that
	// actually exercise brew override it locally (withCaptureRealizerAndBrew /
	// withRealizerAndBrew / the brew apply fakes). This keeps every other test
	// host-independent WITHOUT touching captureGOOSFn, which also keys the emitted
	// host ref (a.Refs[runtime.GOOS]) and must stay the real host OS.
	newBrewDriverFn = failBrewDriverFn

	// Default the backend-bootstrap pre-step to a present/available no-op so the
	// real installer is never shelled in tests and existing command tests stay
	// byte-identical (the factory gate still decides resolution). Tests that
	// exercise the bootstrap wiring override bootstrapBackendsFn locally.
	bootstrapBackendsFn = presentBootstrapFn

	// Capture always publishes a bundle now, even when no config module matches.
	// Keep the default catalog empty so package-capture tests never depend on the
	// checkout's live module catalog; tests that exercise matching replace this
	// seam explicitly.
	loadCaptureModuleCatalogFn = func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error) {
		return map[string]*modules.Module{}, []modules.CatalogDiagnostic{}, nil
	}

	code := m.Run()
	cleanup()
	os.Exit(code)
}
