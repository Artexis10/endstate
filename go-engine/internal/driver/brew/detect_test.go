// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// parseVersions (pure parser for `brew list --versions` output)
// ---------------------------------------------------------------------------

func TestParseVersions_SinglePackage(t *testing.T) {
	got := parseVersions("node 20.11.0\n")
	if v, ok := got["node"]; !ok || v != "20.11.0" {
		t.Errorf("expected node→20.11.0, got %+v", got)
	}
}

func TestParseVersions_MultiplePackages(t *testing.T) {
	output := "node 20.11.0\nripgrep 14.1.0\npython@3.11 3.11.7\n"
	got := parseVersions(output)
	want := map[string]string{
		"node":       "20.11.0",
		"ripgrep":    "14.1.0",
		"python@3.11": "3.11.7",
	}
	for name, ver := range want {
		if got[name] != ver {
			t.Errorf("expected %s→%s, got %q (full=%+v)", name, ver, got[name], got)
		}
	}
}

func TestParseVersions_MultipleVersionsTakesFirst(t *testing.T) {
	// brew can list several installed versions on one line; we capture the first.
	got := parseVersions("openssl@3 3.2.1 3.2.0\n")
	if got["openssl@3"] != "3.2.1" {
		t.Errorf("expected first version 3.2.1, got %q", got["openssl@3"])
	}
}

func TestParseVersions_NameOnlyLine(t *testing.T) {
	// A name with no version is still "installed" (empty version).
	got := parseVersions("somepkg\n")
	v, ok := got["somepkg"]
	if !ok || v != "" {
		t.Errorf("expected somepkg present with empty version, got %q (ok=%v)", v, ok)
	}
}

func TestParseVersions_BlankAndWhitespaceLines(t *testing.T) {
	got := parseVersions("\n  \nnode 20.11.0\n\n")
	if len(got) != 1 || got["node"] != "20.11.0" {
		t.Errorf("expected only node→20.11.0, got %+v", got)
	}
}

func TestParseVersions_Empty(t *testing.T) {
	if got := parseVersions(""); len(got) != 0 {
		t.Errorf("expected empty map, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// DetectBatch
// ---------------------------------------------------------------------------

func TestDetectBatch_FormulaeVersionsAndMisses(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list": {exitCode: 0, stdout: "node 20.11.0\nripgrep 14.1.0\n"},
	}, &calls)}

	results, err := d.DetectBatch([]string{"node", "ripgrep", "not-installed"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r := results["node"]; !r.Installed || r.Version != "20.11.0" || r.DisplayName != "node" {
		t.Errorf("node result = %+v, want installed 20.11.0", r)
	}
	if r := results["ripgrep"]; !r.Installed || r.Version != "14.1.0" {
		t.Errorf("ripgrep result = %+v, want installed 14.1.0", r)
	}
	if r := results["not-installed"]; r.Installed || r.Version != "" {
		t.Errorf("not-installed result = %+v, want absent with no version", r)
	}

	// Only one `brew list --versions` (formula) call — no --cask call when no
	// cask refs are present.
	listVersionCalls := 0
	for _, c := range calls {
		joined := strings.Join(c, " ")
		if strings.Contains(joined, "list --versions") || strings.Contains(joined, "list --cask --versions") {
			listVersionCalls++
		}
		if strings.Contains(joined, "--cask") {
			t.Errorf("no cask refs present, must not query --cask: %v", calls)
		}
	}
	if listVersionCalls != 1 {
		t.Errorf("expected exactly one list --versions call, got %d (%v)", listVersionCalls, calls)
	}
}

func TestDetectBatch_CaskVersions(t *testing.T) {
	var calls [][]string
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		// args[0] is "list" for both formula and cask version listings; the
		// scriptedCommand keys on args[0], so both share this response. We supply
		// cask-shaped output and assert the --cask flag was passed.
		"list": {exitCode: 0, stdout: "firefox 122.0\n"},
	}, &calls)}

	results, err := d.DetectBatch([]string{"cask:firefox", "cask:not-a-cask"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r := results["cask:firefox"]; !r.Installed || r.Version != "122.0" || r.DisplayName != "firefox" {
		t.Errorf("firefox cask result = %+v, want installed 122.0", r)
	}
	if r := results["cask:not-a-cask"]; r.Installed {
		t.Errorf("absent cask should be Installed=false: %+v", r)
	}
	// The cask version listing must pass --cask.
	sawCaskList := false
	for _, c := range calls {
		if strings.Contains(strings.Join(c, " "), "list --cask --versions") {
			sawCaskList = true
		}
	}
	if !sawCaskList {
		t.Errorf("expected a `brew list --cask --versions` call: %v", calls)
	}
}

func TestDetectBatch_MixedFormulaAndCask(t *testing.T) {
	var calls [][]string
	// Both formula and cask listings key on "list"; supply a superset of output
	// (both maps get the same parse, but each ref reads from the right kind).
	d := &BrewDriver{ExecCommand: scriptedCommand(map[string]scriptedResponse{
		"list": {exitCode: 0, stdout: "node 20.11.0\nfirefox 122.0\n"},
	}, &calls)}

	results, err := d.DetectBatch([]string{"node", "cask:firefox"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r := results["node"]; !r.Installed || r.Version != "20.11.0" {
		t.Errorf("node = %+v, want installed 20.11.0", r)
	}
	if r := results["cask:firefox"]; !r.Installed || r.Version != "122.0" {
		t.Errorf("firefox cask = %+v, want installed 122.0", r)
	}
	// Two listing calls: one formula, one cask.
	formula, cask := false, false
	for _, c := range calls {
		joined := strings.Join(c, " ")
		if joined == "brew list --versions" {
			formula = true
		}
		if joined == "brew list --cask --versions" {
			cask = true
		}
	}
	if !formula || !cask {
		t.Errorf("expected both formula and cask version listings; calls=%v", calls)
	}
}
