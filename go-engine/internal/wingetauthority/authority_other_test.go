// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package wingetauthority

import (
	"crypto/sha256"
	"os/exec"
	"testing"
)

func TestCommandWithRejectsStrictAuthorityOutsideWindows(t *testing.T) {
	capability, err := Encode(`/trusted/winget`, sha256.Sum256([]byte("winget")))
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	t.Setenv(StrictEnvironment, StrictValue)
	t.Setenv(AuthorityEnvironment, capability)
	called := false

	_, _, err = CommandWith(func(string, ...string) *exec.Cmd {
		called = true
		return exec.Command("winget")
	})
	if err == nil {
		t.Fatal("CommandWith() error = nil, want strict-mode rejection")
	}
	if called {
		t.Fatal("CommandWith() called builder in unsupported strict mode")
	}
}
