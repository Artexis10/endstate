// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"os/exec"
	"testing"
)

// TestEnumerateInstalled_FormulaeAndCasks: `brew leaves` lists top-level formulae
// and `brew list --cask` lists casks; versions come from `brew list --versions`
// (formulae) and `brew list --cask --versions` (casks). Casks are emitted with a
// "cask:" ref; formulae bare. A missing version is "" (not a failure).
func TestEnumerateInstalled_FormulaeAndCasks(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		// "leaves" → top-level formulae (one per line).
		"leaves": {exitCode: 0, stdout: "ripgrep\njq\n"},
		// "list" → both `--cask` (cask listing) and `--versions` share args[0]=="list".
		// scriptedCommand keys only on args[0], so this single response is reused for
		// the cask listing, the formula --versions, and the cask --versions calls.
		// We supply a superset of names+versions; each query reads the names it needs.
		"list": {exitCode: 0, stdout: "ripgrep 14.1.0\njq 1.7\nfirefox 122.0\n"},
	}, &calls)}

	got, err := d.EnumerateInstalled()
	if err != nil {
		t.Fatalf("EnumerateInstalled error: %v", err)
	}

	byRef := map[string]InstalledApp{}
	for _, a := range got {
		byRef[a.Ref] = a
	}

	if a, ok := byRef["ripgrep"]; !ok || a.Cask || a.Version != "14.1.0" {
		t.Errorf("ripgrep = %+v, want formula version 14.1.0", a)
	}
	if a, ok := byRef["jq"]; !ok || a.Cask || a.Version != "1.7" {
		t.Errorf("jq = %+v, want formula version 1.7", a)
	}
	// A cask must carry the cask: ref and Cask=true.
	ca, ok := byRef["cask:firefox"]
	if !ok || !ca.Cask {
		t.Errorf("firefox cask missing or not marked cask: %+v (all=%+v)", ca, got)
	}
}

// TestEnumerateInstalled_MissingVersionIsEmptyNotError: a formula with no version
// in the versions listing is captured with an empty version, not an error.
func TestEnumerateInstalled_MissingVersionIsEmptyNotError(t *testing.T) {
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"leaves": {exitCode: 0, stdout: "somepkg\n"},
		"list":   {exitCode: 0, stdout: ""}, // no versions listed
	}, nil)}

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
