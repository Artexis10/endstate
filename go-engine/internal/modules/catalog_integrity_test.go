// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// These tests guard the production config-module catalog
// (modules/apps/<id>/module.jsonc) against the failure modes that become likely
// once the catalog grows to hundreds of modules. LoadCatalog deliberately skips
// malformed/duplicate modules with only a stderr warning, so without a
// catalog-wide test a single bad file would silently vanish from the catalog.
//
// All rules here are calibrated to pass on the existing catalog; they encode
// invariants that every module — old and new — must satisfy.

// productionModulesRoot is the real catalog directory relative to this test file.
func productionModulesRoot() string {
	return filepath.Join("..", "..", "..", "modules", "apps")
}

// diskModuleDirs returns the directory names under root that contain a
// module.jsonc file.
func diskModuleDirs(t *testing.T, root string) []string {
	t.Helper()
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read modules dir %s: %v", root, err)
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, e.Name(), "module.jsonc")); err == nil {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs
}

// TestCatalogIntegrity_NoSilentSkips is the central guard for bulk module
// additions. LoadCatalog silently skips modules with invalid JSON, missing
// required fields (id/displayName/matches), or duplicate IDs — logging only to
// stderr. With hundreds of modules a single malformed file would disappear
// unnoticed. This asserts every module.jsonc on disk produces exactly one
// catalog entry.
func TestCatalogIntegrity_NoSilentSkips(t *testing.T) {
	root := productionModulesRoot()
	dirs := diskModuleDirs(t, root)
	if len(dirs) < 70 {
		t.Fatalf("found only %d module.jsonc dirs under %s — wrong path or catastrophic catalog loss?", len(dirs), root)
	}

	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	if len(catalog) != len(dirs) {
		loaded := make(map[string]bool, len(catalog))
		for _, mod := range catalog {
			loaded[filepath.Base(mod.ModuleDir)] = true
		}
		var skipped []string
		for _, d := range dirs {
			if !loaded[d] {
				skipped = append(skipped, d)
			}
		}
		t.Fatalf("catalog loaded %d modules but %d module.jsonc files exist on disk; %d silently skipped "+
			"(invalid JSON, missing required field, or duplicate id) — run the engine to see stderr warnings: %v",
			len(catalog), len(dirs), len(dirs)-len(catalog), skipped)
	}
}

var (
	driveAbsRe   = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	validSensSet = map[string]bool{"none": true, "low": true, "medium": true, "high": true}
)

// isHKCU reports whether a registry path targets the current-user hive, which is
// the only hive the engine will import (see restore/registry_import.go) and thus
// the only portable hive to capture.
func isHKCU(p string) bool {
	u := strings.ToUpper(strings.TrimSpace(p))
	switch {
	case u == "HKCU" || u == "HKEY_CURRENT_USER":
		return true
	case strings.HasPrefix(u, "HKCU\\"), strings.HasPrefix(u, "HKCU:"),
		strings.HasPrefix(u, "HKEY_CURRENT_USER\\"), strings.HasPrefix(u, "HKEY_CURRENT_USER:"):
		return true
	}
	return false
}

// validDest reports whether a capture dest is namespaced under apps/<shortID>/.
// The bundle path-rewriter relies on this prefix to map payload sources to
// captured files (CLAUDE.md landmine #2).
func validDest(dest, shortID string) bool {
	d := strings.TrimSpace(filepath.ToSlash(dest))
	prefix := "apps/" + shortID
	return d == prefix || strings.HasPrefix(d, prefix+"/")
}

// TestCatalogIntegrity_ModuleInvariants enforces structural and safety
// invariants on every loaded module:
//   - id == "apps.<directory>"
//   - sensitivity ∈ {none, low, medium, high}
//   - registry-import restore targets and captured registry keys are HKCU-only
//     (the only restorable hive)
//   - capture dests are namespaced under apps/<shortID>/
//   - no hardcoded absolute drive paths in restore/capture sources or targets
func TestCatalogIntegrity_ModuleInvariants(t *testing.T) {
	root := productionModulesRoot()
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	for id, mod := range catalog {
		dir := filepath.Base(mod.ModuleDir)
		shortID := strings.TrimPrefix(mod.ID, "apps.")

		if mod.ID != "apps."+dir {
			t.Errorf("%s: id %q does not match its directory (expected %q)", dir, mod.ID, "apps."+dir)
		}
		if !validSensSet[mod.Sensitivity] {
			t.Errorf("%s: sensitivity %q not in {none, low, medium, high}", id, mod.Sensitivity)
		}

		for i, r := range mod.Restore {
			if r.Type == "registry-import" && !isHKCU(r.Target) {
				t.Errorf("%s: restore[%d] registry-import target %q is not HKCU; the engine rejects non-HKCU imports", id, i, r.Target)
			}
			if driveAbsRe.MatchString(strings.TrimSpace(r.Source)) {
				t.Errorf("%s: restore[%d] source %q is a hardcoded absolute path (use ./payload/... or env vars)", id, i, r.Source)
			}
			if driveAbsRe.MatchString(strings.TrimSpace(r.Target)) {
				t.Errorf("%s: restore[%d] target %q is a hardcoded absolute path (use %%VAR%%, ~, or HKCU)", id, i, r.Target)
			}
		}

		if mod.Capture != nil {
			for i, f := range mod.Capture.Files {
				if driveAbsRe.MatchString(strings.TrimSpace(f.Source)) {
					t.Errorf("%s: capture.files[%d] source %q is a hardcoded absolute path", id, i, f.Source)
				}
				if !validDest(f.Dest, shortID) {
					t.Errorf("%s: capture.files[%d] dest %q must be namespaced under %q", id, i, f.Dest, "apps/"+shortID+"/")
				}
			}
			for i, k := range mod.Capture.RegistryKeys {
				if !isHKCU(k.Key) {
					t.Errorf("%s: capture.registryKeys[%d] key %q is not HKCU; only HKCU is restorable via registry-import", id, i, k.Key)
				}
				if !validDest(k.Dest, shortID) {
					t.Errorf("%s: capture.registryKeys[%d] dest %q must be namespaced under %q", id, i, k.Dest, "apps/"+shortID+"/")
				}
			}
		}
	}
}

// overlapAllowlistKey normalizes a module-ID pair into a stable, order-independent
// allowlist key.
func overlapAllowlistKey(a, b string) [2]string {
	if a <= b {
		return [2]string{a, b}
	}
	return [2]string{b, a}
}

// knownRestoreOverlapAllowlist enumerates module-ID pairs that are permitted to
// declare overlapping restore targets. Two kinds of entries belong here:
//
//  1. Intentional shared-ownership designs (none today).
//  2. GRANDFATHERED pre-existing overlaps that predate this invariant and are
//     tracked for a follow-up ownership partition. These are latent bugs of the
//     same class the invariant guards against (overlapping targets make the
//     restore planner fail BOTH sets with ReasonTargetCollision — see
//     internal/planner/config_collision.go), not deliberate designs. They are
//     allowlisted only so this guard can block NEW regressions immediately while
//     the known cases are triaged separately.
//
// Key entries via overlapAllowlistKey. Removing an entry (by partitioning the
// overlap in the catalog) is always safe and preferred.
var knownRestoreOverlapAllowlist = map[[2]string]string{
	// Betterbird is a Thunderbird fork that reuses the same
	// %APPDATA%\Thunderbird profile tree, so both modules target the same
	// prefs/filters/extensions. Follow-up: decide single ownership of the
	// shared Thunderbird profile (or make Betterbird non-owning).
	overlapAllowlistKey("apps.betterbird", "apps.thunderbird"): "pre-existing: Betterbird reuses the Thunderbird profile dir; pending ownership partition",
	// Gpg4win bundles Kleopatra (its matches include kleopatra.exe), so both
	// modules restore %APPDATA%\gnupg\kleopatra\kleopatrarc and
	// %APPDATA%\kleopatra\kleopatrarc. Follow-up: fold Kleopatra config into the
	// Gpg4win bundle owner or gate double-matching.
	overlapAllowlistKey("apps.gpg4win", "apps.kleopatra"): "pre-existing: Gpg4win bundles Kleopatra; overlapping kleopatrarc targets pending partition",
	// IntelliJ IDEA Community and Ultimate both declare the identical
	// %APPDATA%\JetBrains\IntelliJIdea* config glob. Follow-up: give each edition
	// an edition-specific config path or a single shared owner.
	overlapAllowlistKey("apps.intellij-idea", "apps.intellij-idea-ultimate"): "pre-existing: IDEA Community/Ultimate share the JetBrains IntelliJIdea* config glob; pending partition",
}

// restoreTargetKind distinguishes filesystem targets from registry targets so
// overlap is only ever compared within a kind (mirrors the planner).
type restoreTargetKind uint8

const (
	fsRestoreTarget restoreTargetKind = iota
	registryRestoreTarget
)

// restoreTargetClaim is a single normalized restore target declared by a module.
type restoreTargetClaim struct {
	kind      restoreTargetKind
	canonical string
	display   string // original, human-readable target for failure messages
}

// homePrefixes are the env-var-style home-directory prefixes used across the
// catalog. They are normalized to a single sentinel so that, e.g., "~/.bashrc"
// and "%USERPROFILE%\\.bashrc" are recognized as the same target even though the
// catalog test cannot expand env vars against a real host.
var homePrefixes = []string{"%userprofile%/", "$home/", "${home}/", "~/"}

// canonicalCatalogFilesystemTarget mirrors the overlap semantics of
// internal/planner/config_collision.go (canonicalFilesystemTarget): it lower-cases
// and path.Clean-normalizes the target. The planner runs on host-expanded paths;
// this catalog-level test cannot expand env vars, so it first collapses the
// known home-directory prefixes to a single "<home>" sentinel. It is a local
// replica rather than a direct call because the planner imports this package, so
// importing planner here would create an import cycle.
func canonicalCatalogFilesystemTarget(raw string) string {
	s := strings.ToLower(strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/")))
	if s == "~" {
		return "<home>"
	}
	for _, p := range homePrefixes {
		if strings.HasPrefix(s, p) {
			s = "<home>/" + s[len(p):]
			break
		}
	}
	return path.Clean(s)
}

// canonicalCatalogRegistryTarget mirrors internal/planner/config_collision.go
// (canonicalRegistryTarget). valueName may be empty for whole-key operations.
func canonicalCatalogRegistryTarget(key, valueName string) string {
	key = strings.ReplaceAll(strings.TrimSpace(key), "/", `\`)
	key = strings.TrimRight(key, `\`)
	return strings.ToLower(key) + "\x00" + strings.ToLower(valueName)
}

// restoreClaimFor converts one RestoreDef into a normalized claim. It returns
// false for restore types that declare no comparable target.
func restoreClaimFor(r RestoreDef) (restoreTargetClaim, bool) {
	switch r.Type {
	case "copy", "merge-json", "merge-ini", "append", "delete-glob":
		if strings.TrimSpace(r.Target) == "" {
			return restoreTargetClaim{}, false
		}
		return restoreTargetClaim{
			kind:      fsRestoreTarget,
			canonical: canonicalCatalogFilesystemTarget(r.Target),
			display:   r.Target,
		}, true
	case "registry-set":
		return restoreTargetClaim{
			kind:      registryRestoreTarget,
			canonical: canonicalCatalogRegistryTarget(r.Key, r.ValueName),
			display:   strings.TrimRight(r.Key, `\/`) + `\` + r.ValueName,
		}, true
	case "registry-import":
		if strings.TrimSpace(r.Target) == "" {
			return restoreTargetClaim{}, false
		}
		return restoreTargetClaim{
			kind:      registryRestoreTarget,
			canonical: canonicalCatalogRegistryTarget(r.Target, ""),
			display:   r.Target,
		}, true
	default:
		return restoreTargetClaim{}, false
	}
}

// moduleRestoreClaims gathers the unique restore-target claims a module declares,
// across both the schema-v1 top-level restore block and any schema-v2 config-set
// generations. Generations within one module legitimately re-target the same
// paths (they are version alternatives), so claims are de-duplicated per module;
// only cross-module overlap is a collision.
func moduleRestoreClaims(mod *Module) []restoreTargetClaim {
	seen := make(map[[2]string]bool)
	var claims []restoreTargetClaim
	add := func(r RestoreDef) {
		claim, ok := restoreClaimFor(r)
		if !ok {
			return
		}
		key := [2]string{string(rune(claim.kind)), claim.canonical}
		if seen[key] {
			return
		}
		seen[key] = true
		claims = append(claims, claim)
	}
	for _, r := range mod.Restore {
		add(r)
	}
	if mod.Config != nil {
		for _, set := range mod.Config.Sets {
			for _, gen := range set.Generations {
				for _, r := range gen.Restore {
					add(r)
				}
			}
		}
	}
	return claims
}

// restoreClaimsOverlap mirrors targetClaimsOverlap/filesystemTargetsOverlap from
// internal/planner/config_collision.go: same-kind only; registry compares by
// equality, filesystem by equal-or-nested paths.
func restoreClaimsOverlap(a, b restoreTargetClaim) bool {
	if a.kind != b.kind {
		return false
	}
	if a.kind == registryRestoreTarget {
		return a.canonical == b.canonical
	}
	left := strings.TrimSuffix(a.canonical, "/")
	right := strings.TrimSuffix(b.canonical, "/")
	if left == right {
		return true
	}
	return strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

// TestCatalogIntegrity_NoOverlappingRestoreTargets guards against two modules in
// the catalog declaring overlapping restore targets (equal or nested filesystem
// paths, or the same registry value). The restore planner fails BOTH colliding
// sets with ReasonTargetCollision (internal/planner/config_collision.go), so a
// real capture that selects two overlapping modules silently restores NEITHER.
// This invariant keeps restore ownership partitioned at catalog-authoring time.
func TestCatalogIntegrity_NoOverlappingRestoreTargets(t *testing.T) {
	root := productionModulesRoot()
	catalog, err := LoadCatalog(root)
	if err != nil {
		t.Fatalf("LoadCatalog: %v", err)
	}

	ids := make([]string, 0, len(catalog))
	for id := range catalog {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	claimsByModule := make(map[string][]restoreTargetClaim, len(ids))
	for _, id := range ids {
		claimsByModule[id] = moduleRestoreClaims(catalog[id])
	}

	type collision struct {
		left, right, target string
	}
	var collisions []collision
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			left, right := ids[i], ids[j]
			if _, allowed := knownRestoreOverlapAllowlist[overlapAllowlistKey(left, right)]; allowed {
				continue
			}
			for _, lc := range claimsByModule[left] {
				for _, rc := range claimsByModule[right] {
					if restoreClaimsOverlap(lc, rc) {
						collisions = append(collisions, collision{left: left, right: right, target: lc.display})
					}
				}
			}
		}
	}

	if len(collisions) > 0 {
		sort.Slice(collisions, func(a, b int) bool {
			if collisions[a].left != collisions[b].left {
				return collisions[a].left < collisions[b].left
			}
			if collisions[a].right != collisions[b].right {
				return collisions[a].right < collisions[b].right
			}
			return collisions[a].target < collisions[b].target
		})
		var b strings.Builder
		b.WriteString("catalog declares overlapping restore targets; the restore planner fails BOTH sets " +
			"with ReasonTargetCollision (internal/planner/config_collision.go). Partition ownership so each " +
			"target has exactly one owning module:\n")
		for _, c := range collisions {
			b.WriteString("  " + c.left + " <-> " + c.right + " both restore: " + c.target + "\n")
		}
		t.Fatal(b.String())
	}
}
