// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package wingetauthority

import (
	"crypto/sha256"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	digest := sha256.Sum256([]byte("winget"))
	encoded, err := Encode(`C:\\Program Files\\WindowsApps\\winget.exe`, digest)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	if !strings.HasPrefix(encoded, "v1:") {
		t.Fatalf("Encode() = %q, want versioned value", encoded)
	}

	path, gotDigest, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if path != `C:\\Program Files\\WindowsApps\\winget.exe` || gotDigest != digest {
		t.Fatalf("Decode() = (%q, %x), want (%q, %x)", path, gotDigest, `C:\\Program Files\\WindowsApps\\winget.exe`, digest)
	}
}

func TestRequireHostedAllowsOnlyCompleteValidAuthority(t *testing.T) {
	digest := sha256.Sum256([]byte("winget"))
	capability, err := Encode(`C:\\trusted\\winget.exe`, digest)
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	for _, environment := range []map[string]string{{StrictEnvironment: StrictValue, AuthorityEnvironment: capability}} {
		if err := RequireHosted(environment); err != nil {
			t.Fatalf("RequireHosted(%v) error = %v", environment, err)
		}
	}
	for _, environment := range []map[string]string{
		nil,
		{},
		{StrictEnvironment: StrictValue},
		{AuthorityEnvironment: capability},
		{StrictEnvironment: "wrong", AuthorityEnvironment: capability},
		{StrictEnvironment: StrictValue, AuthorityEnvironment: "malformed"},
		{StrictEnvironment: StrictValue, strings.ToLower(StrictEnvironment): StrictValue, AuthorityEnvironment: capability},
	} {
		if err := RequireHosted(environment); err == nil {
			t.Fatalf("RequireHosted(%v) error = nil, want invalid authority", environment)
		}
	}
}

func TestEncodeRejectsControlAndOversizedPaths(t *testing.T) {
	for _, path := range []string{"", "C:\\trusted\\win\x00get.exe", "C:\\trusted\\win\u0085get.exe", strings.Repeat("x", maxCapabilityLength)} {
		if _, err := Encode(path, [32]byte{}); err == nil {
			t.Fatalf("Encode(%q) error = nil, want invalid authority", path)
		}
	}
}

func TestCommandWithUsesAmbientWingetOnlyWithoutAuthority(t *testing.T) {
	clearEnvironment(t, StrictEnvironment)
	clearEnvironment(t, AuthorityEnvironment)

	var gotName string
	command, release, err := CommandWith(func(name string, args ...string) *exec.Cmd {
		gotName = name
		return &exec.Cmd{Path: name, Args: append([]string{name}, args...)}
	}, "--version")
	if err != nil {
		t.Fatalf("CommandWith() error = %v", err)
	}
	defer release()
	if gotName != "winget" || command.Path != "winget" {
		t.Fatalf("CommandWith() builder name = %q, path = %q; want ambient winget", gotName, command.Path)
	}
	if command.Env != nil {
		t.Fatalf("CommandWith() environment = %v, want nil", command.Env)
	}
}

func clearEnvironment(t *testing.T, key string) {
	t.Helper()
	value, present := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%q): %v", key, err)
	}
	t.Cleanup(func() {
		if present {
			_ = os.Setenv(key, value)
			return
		}
		_ = os.Unsetenv(key)
	})
}

func TestDecodeRejectsMalformedCapability(t *testing.T) {
	for _, value := range []string{
		"",
		"v2:Qw:0000000000000000000000000000000000000000000000000000000000000000",
		"v1:not?base64:0000000000000000000000000000000000000000000000000000000000000000",
		"v1:Qw:ABC",
		"v1:Zh:0000000000000000000000000000000000000000000000000000000000000000",
		"v1:Qw:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		"v1:Qw:0000000000000000000000000000000000000000000000000000000000000000:extra",
	} {
		t.Run(value, func(t *testing.T) {
			if _, _, err := Decode(value); err == nil {
				t.Fatal("Decode() error = nil, want invalid capability")
			}
		})
	}
}

func TestAuthorityEnvironmentHandlingIsCaseInsensitive(t *testing.T) {
	value, found, invalid := environmentValue(map[string]string{
		strings.ToLower(StrictEnvironment): StrictValue,
	}, StrictEnvironment)
	if !found || invalid || value != StrictValue {
		t.Fatalf("environmentValue() = (%q, %v, %v), want (%q, true, false)", value, found, invalid, StrictValue)
	}
	_, found, invalid = environmentValue(map[string]string{
		StrictEnvironment:                  StrictValue,
		strings.ToLower(StrictEnvironment): StrictValue,
	}, StrictEnvironment)
	if found || !invalid {
		t.Fatalf("environmentValue() duplicate = (found=%v, invalid=%v), want (false, true)", found, invalid)
	}
	filtered := withoutAuthority([]string{
		"ordinary=value",
		strings.ToLower(StrictEnvironment) + "=private",
		strings.ToLower(AuthorityEnvironment) + "=private",
	})
	if len(filtered) != 1 || filtered[0] != "ordinary=value" {
		t.Fatalf("withoutAuthority() = %v, want ordinary environment only", filtered)
	}
}
