// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"testing"
)

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

	code := m.Run()
	cleanup()
	os.Exit(code)
}
