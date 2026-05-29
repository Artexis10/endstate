// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import "testing"

func TestParseProfileList_V3Object(t *testing.T) {
	// Nix 3.x shape: version + name-keyed elements OBJECT.
	data := []byte(`{"version":3,"elements":{"ripgrep":{"attrPath":"legacyPackages.x86_64-linux.ripgrep","storePaths":["/nix/store/abc-ripgrep-15.1.0"]},"jq":{"attrPath":"legacyPackages.x86_64-linux.jq","storePaths":["/nix/store/def-jq-1.8.1"]}}}`)
	set, err := parseProfileList(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(set.Elements) != 2 {
		t.Fatalf("want 2 elements, got %d", len(set.Elements))
	}
	rg, ok := set.Elements["ripgrep"]
	if !ok {
		t.Fatalf("missing ripgrep element")
	}
	if rg.AttrPath != "legacyPackages.x86_64-linux.ripgrep" {
		t.Errorf("attrPath: got %q", rg.AttrPath)
	}
}

func TestParseProfileList_LegacyArray(t *testing.T) {
	// Older Nix shape: elements ARRAY (handled defensively).
	data := []byte(`{"version":2,"elements":[{"attrPath":"legacyPackages.x86_64-linux.jq","storePaths":["/nix/store/def-jq"]}]}`)
	set, err := parseProfileList(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := set.Elements["jq"]; !ok {
		t.Fatalf("legacy array: expected element keyed by leaf 'jq', got %+v", set.Elements)
	}
}

func TestParseProfileList_Empty(t *testing.T) {
	for _, data := range [][]byte{[]byte(`{"version":3,"elements":{}}`), []byte(""), nil} {
		set, err := parseProfileList(data)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", data, err)
		}
		if len(set.Elements) != 0 {
			t.Errorf("want empty, got %d elements", len(set.Elements))
		}
	}
}

func TestParseInternalJSON_Signals(t *testing.T) {
	type msg = struct {
		level int
		text  string
	}
	stderr := mkStderr([]msg{{0, "error: BOOM"}, {1, "warning: meh"}, {4, "debug: ignored"}}, []int{actBuild, actFileTransfer})
	p := parseInternalJSON(stderr)
	if !p.sawBuild() {
		t.Error("expected sawBuild true")
	}
	if !p.sawDownload() {
		t.Error("expected sawDownload true")
	}
	if len(p.errorMsgs) != 2 { // level<=1 only (debug excluded)
		t.Errorf("want 2 error msgs (level<=1), got %d: %v", len(p.errorMsgs), p.errorMsgs)
	}
	if !contains(p.blob, "boom") {
		t.Errorf("blob should be lowercased and contain 'boom': %q", p.blob)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
