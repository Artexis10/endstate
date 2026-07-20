package commands

import (
	"regexp"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// restoreSourceModulePattern extracts a module directory name from a restore
// entry's source path. Both layouts are recognized: `./configs/<id>/...` is the
// capture/bundle layout written by internal/bundle (see rewriteSourcePath), and
// `./payload/apps/<id>/...` is the repo-relative layout used by module
// definitions. The trailing segment is optional so a directory-root source
// (`./configs/vlc`) resolves the same as a file source.
var restoreSourceModulePattern = regexp.MustCompile(`^\./(?:configs|payload/apps)/([^/]+)(?:/.*)?$`)

// resolveRestoreEntryModule maps one restore entry to a qualified module ID.
//
// Precedence (see openspec/changes/fix-setup-flow-honest-reporting/design.md,
// Decision 1):
//  1. The entry's FromModule, when non-empty. Written by capture and the bundle
//     rewrite, so it is authoritative when present.
//  2. Otherwise the ID derived from the source path prefix, but only when that
//     ID names a module in the loaded catalog. Profiles captured before
//     FromModule existed carry no provenance at all, and deriving from the path
//     is the only way they resolve; validating against the catalog keeps the
//     derivation from inventing modules.
//
// Returns "" when the entry cannot be attributed to a catalog module.
func resolveRestoreEntryModule(entry manifest.RestoreEntry, catalog map[string]*modules.Module) string {
	if entry.FromModule != "" {
		return entry.FromModule
	}
	if catalog == nil {
		return ""
	}
	matches := restoreSourceModulePattern.FindStringSubmatch(normalizeRestoreSource(entry.Source))
	if matches == nil {
		return ""
	}
	candidate := "apps." + matches[1]
	if _, ok := catalog[candidate]; !ok {
		return ""
	}
	return candidate
}

// normalizeRestoreSource makes a restore source comparable to the path pattern:
// backslashes become forward slashes and a missing "./" prefix is added, so
// `configs\vlc\vlcrc` and `./configs/vlc/vlcrc` resolve identically.
func normalizeRestoreSource(source string) string {
	normalized := strings.ReplaceAll(strings.TrimSpace(source), "\\", "/")
	if normalized == "" {
		return ""
	}
	if !strings.HasPrefix(normalized, "./") {
		normalized = "./" + strings.TrimPrefix(normalized, "/")
	}
	return normalized
}

// restoreModuleScope carries the profile-scoped restore modules plus any
// warnings produced while scoping them. It travels to the apply lanes as a
// pointer so the many lane callers that have no scope to supply keep passing
// nil, and every accessor below is nil-safe.
type restoreModuleScope struct {
	Modules  []RestoreModuleRef
	Warnings []CommandWarning
}

// modules returns the scoped module refs, or nil when no scope was supplied.
func (s *restoreModuleScope) modules() []RestoreModuleRef {
	if s == nil {
		return nil
	}
	return s.Modules
}

// warnings returns the scoping warnings, or nil when no scope was supplied.
func (s *restoreModuleScope) warnings() []CommandWarning {
	if s == nil {
		return nil
	}
	return s.Warnings
}

// scopeRestoreModules builds restoreModulesAvailable from what the manifest
// actually carries rather than from every catalog module matching the app list.
//
// A module appears only when at least one restore entry resolves to it, and its
// EntryCount is the number of entries that did — derived from the same pass, so
// membership and count cannot disagree. When no entry resolves by either tier,
// the manifest's declared ConfigModules is used as a last resort (tier 3), which
// keeps profiles whose restore entries use an unrecognized layout from silently
// offering nothing.
//
// When the manifest has restore entries but none could be attributed to a
// catalog module, the returned scope carries a warning rather than presenting an
// empty picker as if the profile simply carried no settings.
//
// allowedModules, when non-nil, restricts the result to those module IDs. It
// carries the --only selection: mf.Restore is not filtered by --only (only
// mf.Apps is), so without this the offered settings would ignore the subset the
// user actually selected. Attribution is decided before the restriction is
// applied, so a subset that excludes every module is not mistaken for a profile
// whose entries could not be attributed.
func scopeRestoreModules(mf *manifest.Manifest, catalog map[string]*modules.Module, allowedModules map[string]bool) *restoreModuleScope {
	if mf == nil || catalog == nil {
		return nil
	}

	counts := make(map[string]int)
	for _, entry := range mf.Restore {
		moduleID := resolveRestoreEntryModule(entry, catalog)
		if moduleID == "" {
			continue
		}
		if _, known := catalog[moduleID]; !known {
			continue
		}
		counts[moduleID]++
	}

	unattributed := len(counts) == 0 && len(mf.Restore) > 0

	// Tier 3: fall back to the declared configModules when no restore entry
	// resolved. Each declared module counts the entries it would contribute, but
	// with nothing attributable the only honest count is the declaration itself.
	if len(counts) == 0 {
		for _, moduleID := range mf.ConfigModules {
			if _, known := catalog[moduleID]; !known {
				continue
			}
			counts[moduleID]++
		}
		if len(counts) > 0 {
			unattributed = false
		}
	}

	if unattributed {
		return &restoreModuleScope{Warnings: []CommandWarning{{
			Code: "restore_entries_unattributed",
			Message: "This profile's saved settings could not be matched to any known app, " +
				"so no settings are offered to restore.",
		}}}
	}

	if len(counts) == 0 {
		return nil
	}

	refs := make([]RestoreModuleRef, 0, len(counts))
	for moduleID, count := range counts {
		if allowedModules != nil && !allowedModules[moduleID] {
			continue
		}
		mod, ok := catalog[moduleID]
		if !ok {
			continue
		}
		refs = append(refs, RestoreModuleRef{
			ID:          moduleID,
			DisplayName: resolveModuleDisplayName(mod),
			EntryCount:  count,
		})
	}
	if len(refs) == 0 {
		return nil
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return &restoreModuleScope{Modules: refs}
}
