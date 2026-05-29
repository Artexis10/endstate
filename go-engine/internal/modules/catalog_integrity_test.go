// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"os"
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
