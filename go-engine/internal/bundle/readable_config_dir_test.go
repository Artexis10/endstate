// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"strings"
	"testing"
)

func TestReadableConfigDirNameSchemeAndSafety(t *testing.T) {
	captureID := CaptureID("apps.powertoys", "settings", "instance-a")
	name := readableConfigDirName("apps.powertoys", captureID)

	if !strings.HasPrefix(name, "powertoys-") {
		t.Fatalf("readable dir %q missing sanitized module prefix", name)
	}
	suffix := strings.TrimPrefix(name, "powertoys-")
	if len(suffix) != 8 {
		t.Fatalf("readable dir %q suffix %q is not 8 hex chars", name, suffix)
	}
	for _, r := range suffix {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("readable dir %q suffix %q has non-hex char %q", name, suffix, r)
		}
	}
	// The full opaque identity must never become the folder name.
	if name == captureID {
		t.Fatalf("readable dir still equals full identity %q", captureID)
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '-') {
			t.Fatalf("readable dir %q contains path-unsafe char %q", name, r)
		}
	}
}

func TestReadableConfigDirNameMultiInstanceNoCollision(t *testing.T) {
	// Two captures of the same module/config-set differing only by instance must
	// resolve to distinct readable directories.
	first := readableConfigDirName("apps.vscode", CaptureID("apps.vscode", "settings", "instance-a"))
	second := readableConfigDirName("apps.vscode", CaptureID("apps.vscode", "settings", "instance-b"))
	if first == second {
		t.Fatalf("multi-instance readable dirs collided: %q == %q", first, second)
	}
	if !strings.HasPrefix(first, "vscode-") || !strings.HasPrefix(second, "vscode-") {
		t.Fatalf("readable dirs %q / %q lost shared module prefix", first, second)
	}
}

func TestReadableConfigDirNameLegacyDecouplesFromIdentity(t *testing.T) {
	legacyID := LegacyCaptureID("apps.powertoys")
	name := readableConfigDirName("apps.powertoys", legacyID)
	if !strings.HasPrefix(name, "powertoys-") {
		t.Fatalf("legacy readable dir %q missing module prefix", name)
	}
	if name == legacyID || strings.HasPrefix(name, "legacy-") {
		t.Fatalf("legacy readable dir %q still leaks opaque identity", name)
	}
}

func TestSanitizeConfigDirSegment(t *testing.T) {
	cases := map[string]string{
		"apps.PowerToys":   "powertoys",
		"apps.foo bar/baz": "foo-bar-baz",
		"UPPER":            "upper",
		"  spaced  ":       "spaced",
		"weird__name!!":    "weird-name",
		"apps.a.b.c":       "a.b.c",
		"":                 "",
		"!!!":              "",
	}
	for input, want := range cases {
		if got := sanitizeConfigDirSegment(input); got != want {
			t.Errorf("sanitizeConfigDirSegment(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestReadableConfigDirNameFallsBackToIdentityWhenUnsanitizable(t *testing.T) {
	// An identifier that sanitizes to empty falls back to the opaque identity so
	// the directory stays unique and manifest-valid.
	id := CaptureID("!!!", "###", "@@@")
	if got := readableConfigDirName("!!!", id); got != id {
		t.Fatalf("unsanitizable identifier = %q, want fallback to %q", got, id)
	}
}
