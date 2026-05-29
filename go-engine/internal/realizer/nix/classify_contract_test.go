// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// This is the LOCKED ANCHOR CONTRACT TEST. Each fixture is the real error text
// emitted by Determinate Nix 3.21.0, captured by re-running the spike
// provocations against live Nix on 2026-05-29 (isolated /tmp profiles,
// --log-format internal-json). The spike's #1 lesson: reasoned anchors are
// wrong (it guessed 0/3 collision anchors right). These assertions fail loudly
// if the anchor table drifts from real output or a Nix upgrade rewords a
// message — re-harvest and re-lock when that happens.

// mkStderr builds an internal-json (`@nix {...}`) stderr blob from msg events
// (level + text) and started activity types, the way real `nix --log-format
// internal-json` emits them.
func mkStderr(msgs []struct {
	level int
	text  string
}, acts []int) []byte {
	var b strings.Builder
	for _, a := range acts {
		line, _ := json.Marshal(map[string]any{"action": "start", "type": a})
		b.WriteString("@nix ")
		b.Write(line)
		b.WriteByte('\n')
	}
	for _, m := range msgs {
		line, _ := json.Marshal(map[string]any{"action": "msg", "level": m.level, "msg": m.text})
		b.WriteString("@nix ")
		b.Write(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func TestClassify_AnchorContract(t *testing.T) {
	type msg = struct {
		level int
		text  string
	}
	cases := []struct {
		name     string
		exit     int
		advanced bool
		acts     []int // started activity types
		msgs     []msg
		wantCode envelope.ErrorCode
		wantSub  string
	}{
		{
			name: "eval-bad-attr", exit: 1, advanced: false,
			msgs:     []msg{{0, "error: flake 'flake:nixpkgs' does not provide attribute 'packages.x86_64-linux.thispackagedoesnotexist12345zz', 'legacyPackages.x86_64-linux.thispackagedoesnotexist12345zz' or 'thispackagedoesnotexist12345zz'"}},
			wantCode: envelope.ErrInstallFailed, wantSub: "eval",
		},
		{
			name: "network-bad-host", exit: 1, advanced: false,
			acts:     []int{actFileTransfer, actFileTransfer},
			msgs:     []msg{{0, "error:\n       … while fetching the input 'github:endstate-spike-nonexistent-org/nonexistent-repo-qpz'\n\n       error: unable to download 'https://api.github.com/repos/endstate-spike-nonexistent-org/nonexistent-repo-qpz/commits/HEAD': HTTP error 404"}},
			wantCode: envelope.ErrInstallFailed, wantSub: "network",
		},
		{
			name: "daemon-unavailable", exit: 1, advanced: false,
			msgs: []msg{
				{1, "warning: cannot fetch global flake registry 'https://install.determinate.systems/flake-registry/stable/flake-registry.json', will use builtin fallback registry: cannot connect to socket at '/nonexistent/spike-store-socket': No such file or directory"},
				{0, "error: opening a connection to remote store 'unix:///nonexistent/spike-store-socket' previously failed"},
			},
			wantCode: envelope.ErrRealizerUnavailable, wantSub: "daemon",
		},
		{
			// Permission failure surfaces AFTER a successful build, at the commit
			// step — structurally identical to a build/collision failure (build
			// ran, generation did not advance). The msg anchor MUST win over the
			// structural collision guess. This is the spike's YELLOW case.
			name: "permission-readonly", exit: 1, advanced: false,
			acts:     []int{actRealise, actBuilds, actCopyPathsType()},
			msgs:     []msg{{0, "error: creating symlink \"/tmp/endstate-nix-harvest/6-permission-ro/profile-1-link.tmp-16449-846930886\" -> \"/nix/store/41qh53y9gyw2h2bn8v32x1ch7pmrj4bk-profile\": Permission denied"}},
			wantCode: envelope.ErrPermissionDenied, wantSub: "permission",
		},
		{
			// Spike-sourced (collision auto-resolves at default priority, so it
			// could not be re-harvested live; this anchor is from the forced
			// equal-priority probe and is best-effort).
			name: "collision-forced", exit: 1, advanced: false,
			acts:     []int{actRealise, actBuilds},
			msgs:     []msg{{0, "error: an existing package already provides the following file:\n         /nix/store/.../bin/cp\n       The conflicting packages have a priority of 5"}},
			wantCode: envelope.ErrInstallFailed, wantSub: "collision",
		},
		{
			name: "unrecognised", exit: 1, advanced: false,
			msgs:     []msg{{0, "error: something the anchor table has never seen before"}},
			wantCode: envelope.ErrInstallFailed, wantSub: "",
		},
		{
			name: "happy", exit: 0, advanced: true,
			acts:     []int{actRealise, actBuilds, actCopyPathsType()},
			wantCode: "", wantSub: "", // nil error
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := parseInternalJSON(mkStderr(c.msgs, c.acts))
			got := classify(c.exit, p, c.advanced)
			if c.wantCode == "" {
				if got != nil {
					t.Fatalf("expected nil error, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("expected code %s, got nil", c.wantCode)
			}
			if got.Code != c.wantCode {
				t.Errorf("code: want %s, got %s", c.wantCode, got.Code)
			}
			if got.Subcode != c.wantSub {
				t.Errorf("subcode: want %q, got %q", c.wantSub, got.Subcode)
			}
			// Moat: raw text is retained for error.detail (never empty when there
			// were error messages).
			if len(c.msgs) > 0 && got.Raw == "" {
				t.Errorf("expected raw text retained for error.detail")
			}
		})
	}
}

// actCopyPathsType returns the CopyPaths activity type id (103), kept as a
// helper so the fixtures read clearly.
func actCopyPathsType() int { return 103 }

func TestClassify_SpawnFailure(t *testing.T) {
	// exitCode < 0 means the nix binary could not be spawned.
	got := classify(-1, parseInternalJSON(nil), false)
	if got == nil || got.Code != envelope.ErrRealizerUnavailable || got.Subcode != "spawn" {
		t.Fatalf("spawn failure: want REALIZER_UNAVAILABLE/spawn, got %+v", got)
	}
}
