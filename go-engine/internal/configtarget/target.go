// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package configtarget holds the canonical restore-target normalization and
// overlap semantics shared by the restore planner (which fails colliding sets)
// and the capture-time collision guard (which keeps one winner before a
// collision ever reaches a manifest). Keeping a single implementation here
// guarantees the two layers agree on what "overlapping" means; historically the
// rules were replicated in internal/planner/config_collision.go and a catalog
// integrity test, and any drift between copies would let a collision slip past
// one layer while the other refused it.
package configtarget

import (
	"path/filepath"
	"strings"
)

// Kind distinguishes the two comparable restore-target domains. Overlap is only
// ever evaluated within a single kind.
type Kind uint8

const (
	// Filesystem is a host filesystem path target (copy/merge/append/delete-glob).
	Filesystem Kind = iota
	// Registry is a Windows registry value or key target (registry-set/import).
	Registry
)

// Claim is a normalized restore target used for cross-set collision checks.
type Claim struct {
	Kind      Kind
	Canonical string
}

// CanonicalFilesystem lower-cases and path.Clean-normalizes a host filesystem
// target so equivalent spellings collapse to one comparable form.
func CanonicalFilesystem(target string) string {
	normalized := filepath.ToSlash(filepath.Clean(strings.TrimSpace(target)))
	return strings.ToLower(normalized)
}

// CanonicalRegistry normalizes a registry key/value pair. valueName may be empty
// for whole-key operations.
func CanonicalRegistry(key, valueName string) string {
	key = strings.ReplaceAll(strings.TrimSpace(key), "/", `\`)
	key = strings.TrimRight(key, `\`)
	return strings.ToLower(key) + "\x00" + strings.ToLower(valueName)
}

// ClaimsOverlap reports whether two claims collide: same-kind only, registry by
// equality, filesystem by equal-or-nested paths.
func ClaimsOverlap(left, right Claim) bool {
	if left.Kind != right.Kind {
		return false
	}
	if left.Kind == Registry {
		return left.Canonical == right.Canonical
	}
	return filesystemOverlap(left.Canonical, right.Canonical)
}

func filesystemOverlap(left, right string) bool {
	if left == right {
		return true
	}
	left = strings.TrimSuffix(left, "/")
	right = strings.TrimSuffix(right, "/")
	return strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}
