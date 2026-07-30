// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package wingetauthority

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestCommandWithStrictAuthorityUsesExactHeldExecutable(t *testing.T) {
	path, digest := strictTestExecutable(t)
	capability, err := Encode(path, digest)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	t.Setenv(StrictEnvironment, StrictValue)
	t.Setenv(AuthorityEnvironment, capability)

	command, release, err := CommandWith(exec.Command, "--version")
	if err != nil {
		t.Fatalf("CommandWith() error = %v", err)
	}
	defer release()
	if command.Path != path {
		t.Fatalf("CommandWith() path = %q, want %q", command.Path, path)
	}
	if command.Env == nil {
		t.Fatal("CommandWith() environment = nil, want private variables removed")
	}
	for _, value := range command.Env {
		if strings.HasPrefix(strings.ToUpper(value), StrictEnvironment+"=") || strings.HasPrefix(strings.ToUpper(value), AuthorityEnvironment+"=") {
			t.Fatalf("CommandWith() leaked authority environment entry %q", value)
		}
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		_ = file.Close()
		t.Fatal("held executable accepted a writable open")
	}
}

func TestCommandWithStrictAuthorityRejectsDigestMismatchBeforeBuilder(t *testing.T) {
	path, _ := strictTestExecutable(t)
	capability, err := Encode(path, [32]byte{})
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
		t.Fatal("CommandWith() error = nil, want invalid authority")
	}
	if called {
		t.Fatal("CommandWith() called builder after digest mismatch")
	}
}

func TestCommandWithStrictAuthorityRejectsReparsePathBeforeBuilder(t *testing.T) {
	path, digest := strictTestExecutable(t)
	link := filepath.Join(t.TempDir(), "winget.exe")
	if err := os.Symlink(path, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	capability, err := Encode(link, digest)
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
		t.Fatal("CommandWith() error = nil, want invalid authority")
	}
	if called {
		t.Fatal("CommandWith() called builder for reparse authority")
	}
}

func TestCommandWithStrictAuthorityIgnoresHostilePath(t *testing.T) {
	path, digest := strictTestExecutable(t)
	shim := filepath.Join(t.TempDir(), "winget.exe")
	if err := os.WriteFile(shim, []byte("hostile"), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	capability, err := Encode(path, digest)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	t.Setenv("PATH", filepath.Dir(shim)+";"+os.Getenv("PATH"))
	t.Setenv(StrictEnvironment, StrictValue)
	t.Setenv(AuthorityEnvironment, capability)

	command, release, err := CommandWith(exec.Command, "--version")
	if err != nil {
		t.Fatalf("CommandWith() error = %v", err)
	}
	defer release()
	if command.Path != path {
		t.Fatalf("CommandWith() path = %q, want trusted executable %q", command.Path, path)
	}
}

func TestCommandWithStrictAuthorityPreservesBuilderEnvironment(t *testing.T) {
	path, digest := strictTestExecutable(t)
	capability, err := Encode(path, digest)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	t.Setenv(StrictEnvironment, StrictValue)
	t.Setenv(AuthorityEnvironment, capability)

	command, release, err := CommandWith(func(name string, args ...string) *exec.Cmd {
		command := exec.Command(name, args...)
		command.Env = []string{
			"ORDINARY=value",
			strings.ToLower(StrictEnvironment) + "=private",
			strings.ToLower(AuthorityEnvironment) + "=private",
		}
		return command
	}, "--version")
	if err != nil {
		t.Fatalf("CommandWith() error = %v", err)
	}
	defer release()
	if len(command.Env) != 1 || command.Env[0] != "ORDINARY=value" {
		t.Fatalf("CommandWith() environment = %q, want builder environment without private authority", command.Env)
	}
}

func strictTestExecutable(t *testing.T) (string, [32]byte) {
	t.Helper()
	source, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable() error = %v", err)
	}
	path := filepath.Join(t.TempDir(), "winget.exe")
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := os.WriteFile(path, contents, 0700); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("UTF16PtrFromString() error = %v", err)
	}
	buffer := make([]uint16, 32768)
	count, err := windows.GetLongPathName(wide, &buffer[0], uint32(len(buffer)))
	if err != nil || count == 0 || count >= uint32(len(buffer)) {
		t.Fatalf("GetLongPathName() count=%d error=%v", count, err)
	}
	path = windows.UTF16ToString(buffer[:count])
	return path, sha256.Sum256(contents)
}
