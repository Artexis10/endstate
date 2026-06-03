// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"path/filepath"
	"strings"
)

// knownOutputSuffixes is the set of Nix output names that appear as trailing
// dash-separated tokens in a store-path base name and are NOT part of the
// version. A token that matches one of these terminates the version scan.
var knownOutputSuffixes = map[string]bool{
	"bin":     true,
	"dev":     true,
	"doc":     true,
	"lib":     true,
	"man":     true,
	"etc":     true,
	"info":    true,
	"out":     true,
	"small":   true,
	"debug":   true,
	"light":   true,
	"full":    true,
	"wrapped": true,
	"static":  true,
	"shared":  true,
	"data":    true,
	"locale":  true,
}

// StorePathVersion extracts the package version from an element's Nix store
// paths. It prefers a store path whose base starts with the exact element name
// (i.e. no output suffix appended to the name), falling back to the first
// path that yields a non-empty version. Returns "" when no version can be
// parsed; never returns an error or panics.
//
// Store-path format: /nix/store/<32-hex-hash>-<name>-<version>[-output]
// Examples:
//
//	/nix/store/2rwsbbpn5p76jf35rv7cb9qlhpxnp83p-ripgrep-14.1.0
//	/nix/store/0b4y5p2qadabfmjmr2zczbzjfvpjydrq-ripgrep-14.1.0-man
func StorePathVersion(name string, storePaths []string) string {
	if name == "" || len(storePaths) == 0 {
		return ""
	}

	// Two-pass: first pass collects exact-name matches (path base starts with
	// hash-name-version, no output suffix); second pass uses any path that
	// yields a version as a fallback.
	var fallback string
	for _, sp := range storePaths {
		ver := parseStorePathVersion(name, sp)
		if ver == "" {
			continue
		}
		// Check whether this is an exact-name path (no output suffix appended
		// to name): the base, after stripping hash, starts with "<name>-<ver>"
		// and the version scan consumed the entire remainder without a suffix.
		if isExactNamePath(name, sp) {
			return ver
		}
		if fallback == "" {
			fallback = ver
		}
	}
	return fallback
}

// isExactNamePath reports whether the store path is the "base" output for the
// element — i.e. the base name after stripping the hash prefix starts exactly
// with "<name>-" and the remainder after the version tokens contains no known
// output suffix. This distinguishes "/nix/store/<h>-ripgrep-14.1.0" from
// "/nix/store/<h>-ripgrep-14.1.0-bin".
func isExactNamePath(name, storePath string) bool {
	base := filepath.Base(storePath)
	rest, ok := stripHashPrefix(base)
	if !ok {
		return false
	}
	prefix := name + "-"
	if !strings.HasPrefix(rest, prefix) {
		return false
	}
	afterName := rest[len(prefix):]
	// Tokenize and check: the last token should NOT be a known output suffix.
	tokens := strings.Split(afterName, "-")
	if len(tokens) == 0 {
		return false
	}
	last := tokens[len(tokens)-1]
	return !knownOutputSuffixes[last]
}

// parseStorePathVersion extracts the version from a single store path, given
// the element name. Returns "" when the path does not follow the expected
// format or does not contain the given name prefix.
func parseStorePathVersion(name, storePath string) string {
	base := filepath.Base(storePath)

	// Strip the 32-char lowercase hex hash and the following dash.
	rest, ok := stripHashPrefix(base)
	if !ok {
		return ""
	}

	// Strip the element name and the following dash.
	prefix := name + "-"
	if !strings.HasPrefix(rest, prefix) {
		return ""
	}
	versionPart := rest[len(prefix):]
	if versionPart == "" {
		return ""
	}

	return extractVersion(versionPart)
}

// stripHashPrefix strips the leading "<32-char-nix-base32>-" prefix from a
// store-path base name. Nix store hashes are 32 lowercase base32 characters
// ([0-9a-z]); they are NOT hex. Returns the remainder and true on success;
// "", false when the prefix is absent or malformed.
func stripHashPrefix(base string) (string, bool) {
	const hashLen = 32
	if len(base) < hashLen+1 {
		return "", false
	}
	hash := base[:hashLen]
	if base[hashLen] != '-' {
		return "", false
	}
	for _, c := range hash {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z')) {
			return "", false
		}
	}
	return base[hashLen+1:], true
}

// extractVersion scans the token string (everything after "<hash>-<name>-")
// and collects version tokens. A token that is a known output suffix
// terminates the scan. A token that starts with a digit is always part of
// the version. A token that does not start with a digit and is a known output
// suffix terminates; otherwise it is included (handles pre-release labels like
// "alpha", "beta", "rc1").
func extractVersion(s string) string {
	if s == "" {
		return ""
	}
	tokens := strings.Split(s, "-")
	var versionTokens []string
	for _, tok := range tokens {
		if tok == "" {
			continue
		}
		if knownOutputSuffixes[tok] {
			// Stop at a known output suffix.
			break
		}
		versionTokens = append(versionTokens, tok)
	}
	return strings.Join(versionTokens, "-")
}
