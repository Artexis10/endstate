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

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
)

// Kind distinguishes the two comparable restore-target domains. Overlap is only
// ever evaluated within a single kind.
type Kind uint8

const (
	// Filesystem is a host filesystem path target (copy/merge/append/delete-glob).
	Filesystem Kind = iota
	// Registry is a Windows registry value or key target (registry-set/import).
	Registry
	Desktop
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

// ClaimsOverlap reports whether two claims collide: registry and desktop by
// equality, filesystem by equal-or-nested paths. A filesystem claim also
// collides with a desktop claim when it covers the dconf user database.
func ClaimsOverlap(left, right Claim) bool {
	if left.Kind != right.Kind {
		return desktopDatabaseCovered(left, right) || desktopDatabaseCovered(right, left)
	}
	if left.Kind == Registry || left.Kind == Desktop {
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

// desktopDatabaseCovered reports whether a filesystem claim replaces the dconf
// user database or one of its parent directories. The session's dconf writer
// persists its cached copy on the next write, so such a restore would be undone
// and could revert unrelated keys; every GSettings value therefore conflicts
// with it. The path is host-dependent, so an unresolvable one claims nothing.
// Task 10.6 must restrict this to GSettings identities once KDE values exist;
// today every desktop claim is a GSettings value.
func desktopDatabaseCovered(desktop, filesystem Claim) bool {
	if desktop.Kind != Desktop || filesystem.Kind != Filesystem {
		return false
	}
	database, err := config.ResolveHostEnginePath(desktopsettings.UserDatabaseCoordinate)
	if err != nil {
		return false
	}
	return databaseCovered(database, filesystem.Canonical)
}

// databaseCovered is the one rule for "covers the dconf user database": the
// resolved database path and a canonical filesystem path are equal or nested.
func databaseCovered(database, canonical string) bool {
	return filesystemOverlap(CanonicalFilesystem(database), canonical)
}

// authoringEnvironment is the fixed Linux layout under which module validation
// resolves both the database and an authored path. The verdict then depends on
// the module alone, not on the validating host's home, XDG variables or OS.
var authoringEnvironment = config.PathEnvironment{GOOS: "linux", Home: "/home/authoring", Env: map[string]string{}}

// AuthoredPathCoversDesktopDatabase reports whether an authored Linux host path
// is the dconf user database or a directory above it, judged in the fixed
// authoring environment. A path that does not resolve there covers nothing.
func AuthoredPathCoversDesktopDatabase(authored string) bool {
	database, err := config.ResolveEnginePath(desktopsettings.UserDatabaseCoordinate, authoringEnvironment)
	if err != nil {
		return false
	}
	resolved, err := config.ResolveEnginePath(authored, authoringEnvironment)
	if err != nil {
		return false
	}
	return databaseCovered(database, CanonicalFilesystem(resolved))
}
