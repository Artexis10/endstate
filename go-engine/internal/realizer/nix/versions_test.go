// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import "testing"

// TestStorePathVersion covers the realistic store-path shapes emitted by
// `nix profile list --json`. The parser is pure (no I/O) and best-effort:
// an unparseable path must return "" and never panic or error.

// Simple case: single store path, version immediately after hash+name.
func TestStorePathVersion_Simple(t *testing.T) {
	got := StorePathVersion("ripgrep", []string{
		"/nix/store/2rwsbbpn5p76jf35rv7cb9qlhpxnp83p-ripgrep-14.1.0",
	})
	if got != "14.1.0" {
		t.Errorf("simple: got %q, want 14.1.0", got)
	}
}

// Output suffix -bin: version is before the known suffix.
func TestStorePathVersion_BinSuffix(t *testing.T) {
	got := StorePathVersion("ripgrep", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-ripgrep-14.1.0-bin",
	})
	if got != "14.1.0" {
		t.Errorf("bin suffix: got %q, want 14.1.0", got)
	}
}

// Output suffix -man: version is before the known suffix.
func TestStorePathVersion_ManSuffix(t *testing.T) {
	got := StorePathVersion("ripgrep", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-ripgrep-14.1.0-man",
	})
	if got != "14.1.0" {
		t.Errorf("man suffix: got %q, want 14.1.0", got)
	}
}

// Output suffix -dev.
func TestStorePathVersion_DevSuffix(t *testing.T) {
	got := StorePathVersion("openssl", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-openssl-3.3.1-dev",
	})
	if got != "3.3.1" {
		t.Errorf("dev suffix: got %q, want 3.3.1", got)
	}
}

// Output suffix -doc.
func TestStorePathVersion_DocSuffix(t *testing.T) {
	got := StorePathVersion("openssl", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-openssl-3.3.1-doc",
	})
	if got != "3.3.1" {
		t.Errorf("doc suffix: got %q, want 3.3.1", got)
	}
}

// Multi-segment version with a non-output trailing segment (like git's windows cross-version).
func TestStorePathVersion_MultiSegment(t *testing.T) {
	// git has versions like "2.43.0" — just confirm parsing works on a longer version.
	got := StorePathVersion("git", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-git-2.43.0",
	})
	if got != "2.43.0" {
		t.Errorf("multi-segment: got %q, want 2.43.0", got)
	}
}

// Date-based version: tokens are numeric, all treated as version parts.
func TestStorePathVersion_DateVersion(t *testing.T) {
	got := StorePathVersion("some-package", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-some-package-2025-04-01",
	})
	if got != "2025-04-01" {
		t.Errorf("date version: got %q, want 2025-04-01", got)
	}
}

// Multi-path: prefers the exact-name path (no output suffix) over a -bin path.
func TestStorePathVersion_MultiPath_PrefersExactName(t *testing.T) {
	got := StorePathVersion("ripgrep", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-ripgrep-14.1.0-bin",
		"/nix/store/2rwsbbpn5p76jf35rv7cb9qlhpxnp83p-ripgrep-14.1.0",
	})
	if got != "14.1.0" {
		t.Errorf("multi-path prefer base: got %q, want 14.1.0", got)
	}
}

// Multi-path: when only a suffixed path exists, still extracts the version.
func TestStorePathVersion_MultiPath_OnlySuffixed(t *testing.T) {
	got := StorePathVersion("ripgrep", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-ripgrep-14.1.0-bin",
		"/nix/store/def1234567890abcdef1234567890abc-ripgrep-14.1.0-man",
	})
	if got != "14.1.0" {
		t.Errorf("multi-path only suffixed: got %q, want 14.1.0", got)
	}
}

// Name mismatch: no path contains the element name — fallback to first parseable.
func TestStorePathVersion_NameMismatch_Fallback(t *testing.T) {
	// Element name "foo" but store path has "bar-1.2.3"; should fall back.
	got := StorePathVersion("foo", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-bar-1.2.3",
	})
	// Fallback: strip hash, then find first version-like token after the first dash.
	// Since name "foo" is not a prefix, the fallback path is taken.
	// The parser should return "" since it can't strip the "foo-" prefix from "bar-1.2.3".
	if got != "" {
		t.Errorf("name mismatch: got %q, want empty (no match)", got)
	}
}

// Unparseable: hash is not 32 hex chars.
func TestStorePathVersion_InvalidHash_ReturnsEmpty(t *testing.T) {
	got := StorePathVersion("ripgrep", []string{
		"/nix/store/tooshort-ripgrep-14.1.0",
	})
	if got != "" {
		t.Errorf("invalid hash: got %q, want empty", got)
	}
}

// Empty store paths → empty version, no panic.
func TestStorePathVersion_EmptyPaths_ReturnsEmpty(t *testing.T) {
	got := StorePathVersion("ripgrep", []string{})
	if got != "" {
		t.Errorf("empty paths: got %q, want empty", got)
	}
}

// Nil store paths → empty version, no panic.
func TestStorePathVersion_NilPaths_ReturnsEmpty(t *testing.T) {
	got := StorePathVersion("ripgrep", nil)
	if got != "" {
		t.Errorf("nil paths: got %q, want empty", got)
	}
}

// Empty name → fallback parsing (name prefix can't match); should not panic.
func TestStorePathVersion_EmptyName_ReturnsEmpty(t *testing.T) {
	got := StorePathVersion("", []string{
		"/nix/store/2rwsbbpn5p76jf35rv7cb9qlhpxnp83p-ripgrep-14.1.0",
	})
	// Empty name means we can't strip a name prefix; returns "".
	if got != "" {
		t.Errorf("empty name: got %q, want empty", got)
	}
}

// jq: single version with no suffix.
func TestStorePathVersion_Jq(t *testing.T) {
	got := StorePathVersion("jq", []string{
		"/nix/store/def1234567890abcdef1234567890abc-jq-1.8.1",
	})
	if got != "1.8.1" {
		t.Errorf("jq: got %q, want 1.8.1", got)
	}
}

// A version with a -lib suffix (common for libraries).
func TestStorePathVersion_LibSuffix(t *testing.T) {
	got := StorePathVersion("openssl", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-openssl-3.3.1-lib",
	})
	if got != "3.3.1" {
		t.Errorf("lib suffix: got %q, want 3.3.1", got)
	}
}

// Package with a hyphenated name: "fd-find" should still work.
func TestStorePathVersion_HyphenatedName(t *testing.T) {
	got := StorePathVersion("fd-find", []string{
		"/nix/store/abc1234567890abcdef1234567890abc-fd-find-10.2.0",
	})
	if got != "10.2.0" {
		t.Errorf("hyphenated name: got %q, want 10.2.0", got)
	}
}
