// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"errors"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// removeRunner scripts a `profile remove` result and captures its args. `list`
// returns an empty set so Remove's post-op Current() read stays hermetic.
func removeRunner(stderr []byte, exit int, runErr error, captured *[]string) Runner {
	return func(args ...string) ([]byte, []byte, int, error) {
		sub := ""
		for i, a := range args {
			if a == "profile" && i+1 < len(args) {
				sub = args[i+1]
				break
			}
		}
		switch sub {
		case "remove":
			if captured != nil {
				*captured = append([]string{}, args...)
			}
			return nil, stderr, exit, runErr
		case "list":
			return []byte(`{"version":3,"elements":{}}`), nil, 0, nil
		}
		return nil, nil, 0, nil
	}
}

func TestRemove_EmitsProfileRemoveAndAdvances(t *testing.T) {
	var got []string
	gen := 0
	b := &Backend{
		Profile: "/tmp/endstate-nix-test-nonexistent",
		Run:     removeRunner(nil, 0, nil, &got),
		genFn:   func() int { v := gen; gen++; return v }, // 0 (before) -> 1 (after) = advanced
	}
	res, err := b.Remove([]string{"ripgrep", "jq"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Err != nil {
		t.Fatalf("unexpected res.Err: %+v", res.Err)
	}
	if !res.Advanced {
		t.Errorf("expected Advanced=true")
	}
	// args: profile remove --profile <p> ripgrep jq --log-format internal-json
	if len(got) < 6 || got[0] != "profile" || got[1] != "remove" || got[2] != "--profile" {
		t.Fatalf("unexpected args: %v", got)
	}
	if got[4] != "ripgrep" || got[5] != "jq" {
		t.Fatalf("expected element names after the profile path, got: %v", got)
	}
}

func TestRemove_NoOpWhenEmpty(t *testing.T) {
	var got []string
	b := &Backend{Profile: "/tmp/endstate-nix-test-nonexistent", Run: removeRunner(nil, 0, nil, &got)}
	if _, err := b.Remove(nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Fatalf("expected no `profile remove` call, got args: %v", got)
	}
}

func TestRemove_PermissionFailureIsSystemic(t *testing.T) {
	stderr := mkStderr([]struct {
		level int
		text  string
	}{{0, "error: opening lock file: permission denied"}}, nil)
	b := &Backend{
		Profile: "/tmp/endstate-nix-test-nonexistent",
		Run:     removeRunner(stderr, 1, nil, nil),
		genFn:   func() int { return 0 }, // no advance
	}
	res, _ := b.Remove([]string{"ripgrep"})
	if res.Err == nil || res.Err.Code != envelope.ErrPermissionDenied {
		t.Fatalf("want PERMISSION_DENIED, got %+v", res.Err)
	}
}

func TestRemove_SpawnFailureIsRealizerUnavailable(t *testing.T) {
	b := &Backend{
		Profile: "/tmp/endstate-nix-test-nonexistent",
		Run:     removeRunner(nil, -1, errors.New("exec: nix not found"), nil),
		genFn:   func() int { return 0 },
	}
	res, _ := b.Remove([]string{"ripgrep"})
	if res.Err == nil || res.Err.Code != envelope.ErrRealizerUnavailable {
		t.Fatalf("want REALIZER_UNAVAILABLE on spawn, got %+v", res.Err)
	}
}

func TestRemove_GenericFailureIsInstallFailed(t *testing.T) {
	stderr := mkStderr([]struct {
		level int
		text  string
	}{{0, "error: something unexpected happened"}}, nil)
	b := &Backend{
		Profile: "/tmp/endstate-nix-test-nonexistent",
		Run:     removeRunner(stderr, 1, nil, nil),
		genFn:   func() int { return 0 },
	}
	res, _ := b.Remove([]string{"ripgrep"})
	if res.Err == nil || res.Err.Code != envelope.ErrInstallFailed {
		t.Fatalf("want INSTALL_FAILED, got %+v", res.Err)
	}
}
