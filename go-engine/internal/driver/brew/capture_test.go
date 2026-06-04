// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

// scriptedByVector drives a distinct (exitCode, stdout) response per FULL brew
// arg vector (joined by spaces), so `leaves`, `list --cask`, `list --versions`,
// and `list --cask --versions` each get their OWN output. The shared
// scriptedCommand keys only on args[0] ("list"), which collides the cask listing
// with both --versions calls — masking a phantom-cask bug where formulae get
// parsed as casks too. Keying on the full vector closes that gap.
func scriptedByVector(byVector map[string]scriptedResponse) func(string, ...string) *exec.Cmd {
	return func(name string, args ...string) *exec.Cmd {
		key := strings.Join(args, " ")
		resp, ok := byVector[key]
		if !ok {
			// An unscripted vector is a test bug — surface it as a non-zero exit with
			// a marker on stderr so a missing key is visible rather than silently empty.
			resp = scriptedResponse{exitCode: 3, stderr: "UNSCRIPTED VECTOR: brew " + key}
		}
		cs := append([]string{"-test.run=TestHelperProcess", "--"}, args...)
		cmd := exec.Command(os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_WANT_HELPER_PROCESS=1",
			"FAKE_EXIT_CODE="+itoa(resp.exitCode),
			"FAKE_STDOUT="+resp.stdout,
			"FAKE_STDERR="+resp.stderr,
		)
		return cmd
	}
}

// itoa is a tiny local int→string helper (avoids importing strconv just for the
// helper-process env wiring).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// TestEnumerateInstalled_FormulaeAndCasks: `brew leaves` lists top-level formulae
// and `brew list --cask` lists casks; versions come from `brew list --versions`
// (formulae) and `brew list --cask --versions` (casks). Casks are emitted with a
// "cask:" ref; formulae bare. The fake keys on the FULL arg vector so each query
// returns a DISTINCT realistic listing, and the test asserts the EXACT enumerated
// set — including that NO phantom cask:<formula> entry leaks in.
func TestEnumerateInstalled_FormulaeAndCasks(t *testing.T) {
	d := &BrewDriver{ExecCommand: scriptedByVector(map[string]scriptedResponse{
		// Top-level formulae (bare tokens, one per line).
		"leaves": {exitCode: 0, stdout: "ripgrep\njq\n"},
		// Installed casks (cask tokens ONLY — must NOT include any formula).
		"list --cask": {exitCode: 0, stdout: "firefox\ngoogle-chrome\n"},
		// Formula versions: name + version columns (formulae only).
		"list --versions": {exitCode: 0, stdout: "ripgrep 14.1.0\njq 1.7\n"},
		// Cask versions: name + version columns (casks only).
		"list --cask --versions": {exitCode: 0, stdout: "firefox 122.0\ngoogle-chrome 121.0\n"},
	})}

	got, err := d.EnumerateInstalled()
	if err != nil {
		t.Fatalf("EnumerateInstalled error: %v", err)
	}

	byRef := map[string]InstalledApp{}
	for _, a := range got {
		byRef[a.Ref] = a
	}

	// --- Exact formulae ---
	if a, ok := byRef["ripgrep"]; !ok || a.Cask || a.Version != "14.1.0" {
		t.Errorf("ripgrep = %+v (ok=%v), want formula version 14.1.0", a, ok)
	}
	if a, ok := byRef["jq"]; !ok || a.Cask || a.Version != "1.7" {
		t.Errorf("jq = %+v (ok=%v), want formula version 1.7", a, ok)
	}

	// --- Exact casks (cask: ref, Cask=true, cask versions) ---
	if a, ok := byRef["cask:firefox"]; !ok || !a.Cask || a.Version != "122.0" {
		t.Errorf("cask:firefox = %+v (ok=%v), want cask version 122.0", a, ok)
	}
	if a, ok := byRef["cask:google-chrome"]; !ok || !a.Cask || a.Version != "121.0" {
		t.Errorf("cask:google-chrome = %+v (ok=%v), want cask version 121.0", a, ok)
	}

	// --- No PHANTOM cask: a formula must NEVER be enumerated as a cask. The old
	// args[0]-only fake returned the same listing for `list --cask` as for the
	// formula listing, so ripgrep/jq leaked in as cask:ripgrep / cask:jq. Assert
	// those phantoms are ABSENT. ---
	for _, phantom := range []string{"cask:ripgrep", "cask:jq", "cask:firefox-formula"} {
		if _, ok := byRef[phantom]; ok {
			t.Errorf("phantom cask entry %q leaked into the enumeration: %+v", phantom, got)
		}
	}
	// And no formula should be carrying a cask token's ref either.
	if _, ok := byRef["firefox"]; ok {
		t.Errorf("cask token 'firefox' must NOT be enumerated as a bare formula: %+v", got)
	}

	// --- Exact total: 2 formulae + 2 casks, nothing more. ---
	if len(got) != 4 {
		t.Errorf("enumerated %d apps, want exactly 4 (ripgrep, jq, cask:firefox, cask:google-chrome): %+v", len(got), got)
	}
}

// TestEnumerateInstalled_MissingVersionIsEmptyNotError: a formula with no version
// in the versions listing is captured with an empty version, not an error.
func TestEnumerateInstalled_MissingVersionIsEmptyNotError(t *testing.T) {
	d := &BrewDriver{ExecCommand: scriptedByVector(map[string]scriptedResponse{
		"leaves":                 {exitCode: 0, stdout: "somepkg\n"},
		"list --cask":            {exitCode: 0, stdout: ""},
		"list --versions":        {exitCode: 0, stdout: ""}, // no version listed for somepkg
		"list --cask --versions": {exitCode: 0, stdout: ""},
	})}

	got, err := d.EnumerateInstalled()
	if err != nil {
		t.Fatalf("EnumerateInstalled error: %v", err)
	}
	found := false
	for _, a := range got {
		if a.Ref == "somepkg" {
			found = true
			if a.Version != "" {
				t.Errorf("expected empty version for somepkg, got %q", a.Version)
			}
		}
	}
	if !found {
		t.Errorf("somepkg not enumerated: %+v", got)
	}
	// Exactly one app (the bare formula), no phantom cask from the empty cask list.
	if len(got) != 1 {
		t.Errorf("enumerated %d apps, want exactly 1 (somepkg): %+v", len(got), got)
	}
}

// TestEnumerateInstalled_BrewMissing: a missing brew binary surfaces as
// ErrBrewNotAvailable (callers treat it as backend-unavailable, not per-package).
func TestEnumerateInstalled_BrewMissing(t *testing.T) {
	d := &BrewDriver{ExecCommand: func(name string, args ...string) *exec.Cmd {
		return exec.Command("endstate-no-such-brew-binary-xyz")
	}}
	_, err := d.EnumerateInstalled()
	if err != ErrBrewNotAvailable {
		t.Errorf("expected ErrBrewNotAvailable, got %v", err)
	}
}

// TestEnumerateInstalled_Empty: no formulae, no casks → empty slice, no error.
func TestEnumerateInstalled_Empty(t *testing.T) {
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"leaves": {exitCode: 0, stdout: "\n"},
		"list":   {exitCode: 0, stdout: "\n"},
	}, nil)}
	got, err := d.EnumerateInstalled()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected no apps, got %+v", got)
	}
}
