package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// scopeTestCatalog is a small catalog covering the modules used below. Display
// names are set so entries are distinguishable from their short IDs.
func scopeTestCatalog() map[string]*modules.Module {
	return map[string]*modules.Module{
		"apps.vlc":       {ID: "apps.vlc", DisplayName: "VLC media player"},
		"apps.obsidian":  {ID: "apps.obsidian", DisplayName: "Obsidian"},
		"apps.vscodium":  {ID: "apps.vscodium", DisplayName: "VSCodium"},
		"apps.nodisplay": {ID: "apps.nodisplay"},
	}
}

func refsByID(refs []RestoreModuleRef) map[string]RestoreModuleRef {
	out := make(map[string]RestoreModuleRef, len(refs))
	for _, r := range refs {
		out[r.ID] = r
	}
	return out
}

// Tier 1: fromModule is authoritative and wins over a conflicting source path.
func TestResolveRestoreEntryModule_FromModuleTakesPrecedence(t *testing.T) {
	catalog := scopeTestCatalog()
	entry := manifest.RestoreEntry{
		Source:     "./configs/vlc/vlcrc",
		FromModule: "apps.obsidian",
	}
	if got := resolveRestoreEntryModule(entry, catalog); got != "apps.obsidian" {
		t.Errorf("expected fromModule to win, got %q", got)
	}
}

// Tier 2: legacy entries with no fromModule resolve from the source path, but
// only when the derived ID names a real catalog module.
func TestResolveRestoreEntryModule_SourcePathDerivation(t *testing.T) {
	catalog := scopeTestCatalog()

	tests := []struct {
		name   string
		source string
		want   string
	}{
		{"configs layout", "./configs/vlc/vlcrc", "apps.vlc"},
		{"payload layout", "./payload/apps/obsidian/app.json", "apps.obsidian"},
		{"directory root source", "./configs/vlc", "apps.vlc"},
		{"nested descendant", "./configs/vscodium/User/settings.json", "apps.vscodium"},
		{"backslash separators", `.\configs\vlc\vlcrc`, "apps.vlc"},
		{"no ./ prefix", "configs/vlc/vlcrc", "apps.vlc"},
		{"derived id not in catalog", "./configs/unknown-thing/file.conf", ""},
		{"unrecognized layout", "./somewhere/else/file.conf", ""},
		{"empty source", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := manifest.RestoreEntry{Source: tt.source}
			if got := resolveRestoreEntryModule(entry, catalog); got != tt.want {
				t.Errorf("source %q: expected %q, got %q", tt.source, tt.want, got)
			}
		})
	}
}

// A module is offered only when the manifest carries restore payload for it.
// This is the measured 41-offered-vs-8-real defect in miniature.
func TestScopeRestoreModules_ExcludesModulesWithoutPayload(t *testing.T) {
	catalog := scopeTestCatalog()
	mf := &manifest.Manifest{
		Restore: []manifest.RestoreEntry{
			{Source: "./configs/vlc/vlcrc", FromModule: "apps.vlc"},
		},
	}

	scope := scopeRestoreModules(mf, catalog, nil)
	if scope == nil {
		t.Fatal("expected a scope, got nil")
	}
	if len(scope.modules()) != 1 {
		t.Fatalf("expected exactly 1 module, got %d: %+v", len(scope.modules()), scope.modules())
	}
	if scope.modules()[0].ID != "apps.vlc" {
		t.Errorf("expected apps.vlc, got %q", scope.modules()[0].ID)
	}
	if len(scope.warnings()) != 0 {
		t.Errorf("expected no warnings when entries attributed, got %+v", scope.warnings())
	}
}

// entryCount counts the entries that resolved, and is never zero for a listed
// module. Ordering is deterministic by module ID.
func TestScopeRestoreModules_EntryCountAndOrdering(t *testing.T) {
	catalog := scopeTestCatalog()
	mf := &manifest.Manifest{
		Restore: []manifest.RestoreEntry{
			{Source: "./configs/vscodium/User/settings.json", FromModule: "apps.vscodium"},
			{Source: "./configs/vlc/vlcrc", FromModule: "apps.vlc"},
			{Source: "./configs/vscodium/User/keybindings.json", FromModule: "apps.vscodium"},
			{Source: "./configs/vscodium/User/snippets", FromModule: "apps.vscodium"},
		},
	}

	scope := scopeRestoreModules(mf, catalog, nil)
	byID := refsByID(scope.modules())

	if got := byID["apps.vscodium"].EntryCount; got != 3 {
		t.Errorf("expected apps.vscodium entryCount=3, got %d", got)
	}
	if got := byID["apps.vlc"].EntryCount; got != 1 {
		t.Errorf("expected apps.vlc entryCount=1, got %d", got)
	}
	for _, ref := range scope.modules() {
		if ref.EntryCount <= 0 {
			t.Errorf("module %q listed with non-positive entryCount %d", ref.ID, ref.EntryCount)
		}
	}
	// Deterministic ordering by module ID.
	mods := scope.modules()
	for i := 1; i < len(mods); i++ {
		if mods[i-1].ID > mods[i].ID {
			t.Errorf("expected ordering by module ID, got %q before %q", mods[i-1].ID, mods[i].ID)
		}
	}
}

// Display names come from the catalog, falling back to the short ID.
func TestScopeRestoreModules_DisplayNameResolution(t *testing.T) {
	catalog := scopeTestCatalog()
	mf := &manifest.Manifest{
		Restore: []manifest.RestoreEntry{
			{Source: "./configs/vlc/vlcrc", FromModule: "apps.vlc"},
			{Source: "./configs/nodisplay/x.ini", FromModule: "apps.nodisplay"},
		},
	}

	byID := refsByID(scopeRestoreModules(mf, catalog, nil).modules())
	if got := byID["apps.vlc"].DisplayName; got != "VLC media player" {
		t.Errorf("expected catalog displayName, got %q", got)
	}
	if got := byID["apps.nodisplay"].DisplayName; got != "nodisplay" {
		t.Errorf("expected short-ID fallback %q, got %q", "nodisplay", got)
	}
}

// Tier 3: when nothing resolves by fromModule or source path, the declared
// configModules are used rather than silently offering nothing.
func TestScopeRestoreModules_FallsBackToConfigModules(t *testing.T) {
	catalog := scopeTestCatalog()
	mf := &manifest.Manifest{
		Restore: []manifest.RestoreEntry{
			{Source: "./unrecognized/layout/file.conf"},
		},
		ConfigModules: []string{"apps.vlc", "apps.obsidian", "apps.not-in-catalog"},
	}

	scope := scopeRestoreModules(mf, catalog, nil)
	byID := refsByID(scope.modules())

	if len(byID) != 2 {
		t.Fatalf("expected 2 modules from configModules fallback, got %d: %+v", len(byID), scope.modules())
	}
	if _, ok := byID["apps.vlc"]; !ok {
		t.Error("expected apps.vlc from configModules fallback")
	}
	if _, ok := byID["apps.not-in-catalog"]; ok {
		t.Error("configModules fallback must not offer modules absent from the catalog")
	}
	if len(scope.warnings()) != 0 {
		t.Errorf("fallback succeeded, so no warning expected, got %+v", scope.warnings())
	}
}

// Restore entries that cannot be attributed by any tier produce a warning rather
// than an empty picker that looks like "this profile has no settings".
func TestScopeRestoreModules_WarnsWhenNothingAttributable(t *testing.T) {
	catalog := scopeTestCatalog()
	mf := &manifest.Manifest{
		Restore: []manifest.RestoreEntry{
			{Source: "./unrecognized/layout/file.conf"},
			{Source: "./configs/unknown-thing/other.conf"},
		},
	}

	scope := scopeRestoreModules(mf, catalog, nil)
	if scope == nil {
		t.Fatal("expected a scope carrying a warning, got nil")
	}
	if len(scope.modules()) != 0 {
		t.Errorf("expected no modules, got %+v", scope.modules())
	}
	if len(scope.warnings()) != 1 {
		t.Fatalf("expected exactly 1 warning, got %d: %+v", len(scope.warnings()), scope.warnings())
	}
	if code := scope.warnings()[0].Code; code != "restore_entries_unattributed" {
		t.Errorf("unexpected warning code %q", code)
	}
}

// allowedModules carries the --only selection. mf.Restore is not filtered by
// --only, so without this the offered settings would escape the subset the user
// selected.
func TestScopeRestoreModules_RespectsOnlySelection(t *testing.T) {
	catalog := scopeTestCatalog()
	mf := &manifest.Manifest{
		Restore: []manifest.RestoreEntry{
			{Source: "./configs/vlc/vlcrc", FromModule: "apps.vlc"},
			{Source: "./configs/obsidian/app.json", FromModule: "apps.obsidian"},
			{Source: "./configs/vscodium/User/settings.json", FromModule: "apps.vscodium"},
		},
	}

	scope := scopeRestoreModules(mf, catalog, map[string]bool{"apps.vlc": true})
	if scope == nil {
		t.Fatal("expected a scope, got nil")
	}
	if len(scope.modules()) != 1 || scope.modules()[0].ID != "apps.vlc" {
		t.Fatalf("expected only apps.vlc within the selection, got %+v", scope.modules())
	}
}

// A selection that excludes every module with payload yields nothing, but that
// is not the same condition as "entries could not be attributed" — so it must
// not warn.
func TestScopeRestoreModules_OnlySelectionExcludingEverythingDoesNotWarn(t *testing.T) {
	catalog := scopeTestCatalog()
	mf := &manifest.Manifest{
		Restore: []manifest.RestoreEntry{
			{Source: "./configs/vlc/vlcrc", FromModule: "apps.vlc"},
		},
	}

	scope := scopeRestoreModules(mf, catalog, map[string]bool{"apps.obsidian": true})
	if len(scope.modules()) != 0 {
		t.Errorf("expected no modules, got %+v", scope.modules())
	}
	if len(scope.warnings()) != 0 {
		t.Errorf("a subset excluding everything is not an attribution failure; got %+v", scope.warnings())
	}
}

// A manifest carrying nothing to restore offers nothing and warns about nothing:
// there is no problem to report, just no settings.
func TestScopeRestoreModules_EmptyManifestIsSilent(t *testing.T) {
	scope := scopeRestoreModules(&manifest.Manifest{}, scopeTestCatalog(), nil)
	if len(scope.modules()) != 0 {
		t.Errorf("expected no modules, got %+v", scope.modules())
	}
	if len(scope.warnings()) != 0 {
		t.Errorf("expected no warnings for a manifest with no restore entries, got %+v", scope.warnings())
	}
}

// The scope pointer is nil-safe so lane callers can pass nil unchanged.
func TestRestoreModuleScope_NilSafe(t *testing.T) {
	var scope *restoreModuleScope
	if scope.modules() != nil {
		t.Error("expected nil modules from nil scope")
	}
	if scope.warnings() != nil {
		t.Error("expected nil warnings from nil scope")
	}
	if scopeRestoreModules(nil, scopeTestCatalog(), nil) != nil {
		t.Error("expected nil scope for nil manifest")
	}
	if scopeRestoreModules(&manifest.Manifest{}, nil, nil) != nil {
		t.Error("expected nil scope for nil catalog")
	}
}
