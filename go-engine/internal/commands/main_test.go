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
	code := m.Run()
	cleanup()
	os.Exit(code)
}
