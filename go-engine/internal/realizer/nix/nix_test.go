// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"errors"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestResolveInstallable(t *testing.T) {
	b := &Backend{Pin: "nixpkgs"}
	cases := map[string]string{
		"ripgrep":                           "nixpkgs#ripgrep",                   // bare attr -> pinned
		"nixpkgs#jq":                        "nixpkgs#jq",                        // already has '#'
		"github:NixOS/nixpkgs/abc123#hello": "github:NixOS/nixpkgs/abc123#hello", // flakeref verbatim
	}
	for in, want := range cases {
		if got := b.ResolveInstallable(in); got != want {
			t.Errorf("ResolveInstallable(%q): want %q, got %q", in, want, got)
		}
	}
}

func TestIsPresent(t *testing.T) {
	set := realizer.Set{Elements: map[string]realizer.Element{
		"ripgrep": {Name: "ripgrep", AttrPath: "legacyPackages.x86_64-linux.ripgrep"},
	}}
	if !isPresent(set, "nixpkgs#ripgrep") {
		t.Error("nixpkgs#ripgrep should be present (leaf match)")
	}
	if !isPresent(set, "ripgrep") {
		t.Error("bare ripgrep should be present")
	}
	if isPresent(set, "nixpkgs#jq") {
		t.Error("jq should NOT be present")
	}
}

// fakeRun dispatches on the `profile <sub>` subcommand.
func fakeRun(addStderr []byte, addExit int, addErr error, listJSON []byte) Runner {
	return func(args ...string) ([]byte, []byte, int, error) {
		sub := ""
		for i, a := range args {
			if a == "profile" && i+1 < len(args) {
				sub = args[i+1]
				break
			}
		}
		switch sub {
		case "add":
			return nil, addStderr, addExit, addErr
		case "list":
			return listJSON, nil, 0, nil
		}
		return nil, nil, 0, nil
	}
}

func TestCurrent_InjectedRunner(t *testing.T) {
	b := &Backend{Profile: "/tmp/endstate-nix-test-nonexistent", Run: fakeRun(nil, 0, nil, []byte(`{"version":3,"elements":{"ripgrep":{"attrPath":"x.ripgrep","storePaths":["/nix/store/a-ripgrep"]}}}`))}
	set, err := b.Current()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := set.Elements["ripgrep"]; !ok {
		t.Fatalf("expected ripgrep in current set, got %+v", set.Elements)
	}
}

func TestPlan_InjectedRunner(t *testing.T) {
	b := &Backend{Profile: "/tmp/endstate-nix-test-nonexistent", Pin: "nixpkgs", Run: fakeRun(nil, 0, nil, []byte(`{"version":3,"elements":{"ripgrep":{"attrPath":"x.ripgrep"}}}`))}
	diff, err := b.Plan([]realizer.Installable{{ID: "ripgrep", Ref: "ripgrep"}, {ID: "jq", Ref: "jq"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(diff.Present) != 1 || diff.Present[0].ID != "ripgrep" {
		t.Errorf("want ripgrep present, got %+v", diff.Present)
	}
	if len(diff.ToAdd) != 1 || diff.ToAdd[0].ID != "jq" {
		t.Errorf("want jq to-add, got %+v", diff.ToAdd)
	}
}

func TestRealize_Success(t *testing.T) {
	gen := 0
	b := &Backend{
		Profile: "/tmp/endstate-nix-test-nonexistent",
		Pin:     "nixpkgs",
		Run:     fakeRun(nil, 0, nil, []byte(`{"version":3,"elements":{}}`)),
		genFn:   func() int { v := gen; gen++; return v }, // 0 (before) -> 1 (after) = advanced
	}
	res, _ := b.Realize([]realizer.Installable{{ID: "jq", Ref: "jq"}})
	if !res.Advanced {
		t.Errorf("expected Advanced=true")
	}
	if res.Err != nil {
		t.Errorf("expected nil Err, got %+v", res.Err)
	}
}

func TestRealize_InstallFailure(t *testing.T) {
	evalStderr := mkStderr([]struct {
		level int
		text  string
	}{{0, "error: flake 'flake:nixpkgs' does not provide attribute 'bogus'"}}, nil)
	b := &Backend{
		Profile: "/tmp/endstate-nix-test-nonexistent",
		Pin:     "nixpkgs",
		Run:     fakeRun(evalStderr, 1, nil, []byte(`{"version":3,"elements":{}}`)),
		genFn:   func() int { return 0 }, // no advance
	}
	res, _ := b.Realize([]realizer.Installable{{ID: "bogus", Ref: "bogus"}})
	if res.Advanced {
		t.Errorf("expected Advanced=false")
	}
	if res.Err == nil || res.Err.Code != envelope.ErrInstallFailed || res.Err.Subcode != "eval" {
		t.Fatalf("want INSTALL_FAILED/eval, got %+v", res.Err)
	}
}

func TestRealize_Spawn(t *testing.T) {
	b := &Backend{
		Profile: "/tmp/endstate-nix-test-nonexistent",
		Pin:     "nixpkgs",
		Run:     fakeRun(nil, -1, errors.New("exec: nix not found"), nil),
		genFn:   func() int { return 0 },
	}
	res, _ := b.Realize([]realizer.Installable{{ID: "jq", Ref: "jq"}})
	if res.Err == nil || res.Err.Code != envelope.ErrRealizerUnavailable {
		t.Fatalf("want REALIZER_UNAVAILABLE on spawn failure, got %+v", res.Err)
	}
}

func TestRealize_EmptyToAdd(t *testing.T) {
	b := &Backend{Profile: "/tmp/endstate-nix-test-nonexistent", Run: fakeRun(nil, 0, nil, []byte(`{"version":3,"elements":{}}`))}
	res, _ := b.Realize(nil)
	if res.Err != nil {
		t.Errorf("empty to-add should be a no-op, got %+v", res.Err)
	}
}
