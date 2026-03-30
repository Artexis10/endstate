// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// ---------------------------------------------------------------------------
// convertToActions — restore filter tests
// ---------------------------------------------------------------------------

// TestConvertToActions_FilterExcludesNonMatching verifies that entries whose
// FromModule does not match the filter are excluded from the result.
func TestConvertToActions_FilterExcludesNonMatching(t *testing.T) {
	entries := []manifest.RestoreEntry{
		{Type: "copy", Source: "a", Target: "b", FromModule: "apps.vscode"},
		{Type: "copy", Source: "c", Target: "d", FromModule: "apps.git"},
	}

	actions := convertToActions(entries, "apps.vscode")

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].FromModule != "apps.vscode" {
		t.Errorf("expected FromModule=%q, got %q", "apps.vscode", actions[0].FromModule)
	}
}

// TestConvertToActions_FilterPassesMatchingModule verifies that entries whose
// FromModule matches the filter are included.
func TestConvertToActions_FilterPassesMatchingModule(t *testing.T) {
	entries := []manifest.RestoreEntry{
		{Type: "copy", Source: "a", Target: "b", FromModule: "apps.git"},
	}

	actions := convertToActions(entries, "apps.git")

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].Source != "a" {
		t.Errorf("expected Source=%q, got %q", "a", actions[0].Source)
	}
}

// TestConvertToActions_InlineEntriesAlwaysPass verifies that entries without
// FromModule (inline manifest entries) always pass the filter.
func TestConvertToActions_InlineEntriesAlwaysPass(t *testing.T) {
	entries := []manifest.RestoreEntry{
		{Type: "copy", Source: "a", Target: "b"},                           // inline, no FromModule
		{Type: "copy", Source: "c", Target: "d", FromModule: "apps.vscode"}, // module entry
	}

	actions := convertToActions(entries, "apps.git")

	// The inline entry should pass (empty FromModule), the vscode entry should be filtered out.
	if len(actions) != 1 {
		t.Fatalf("expected 1 action (inline only), got %d", len(actions))
	}
	if actions[0].Source != "a" {
		t.Errorf("expected inline entry with Source=%q, got %q", "a", actions[0].Source)
	}
}

// TestConvertToActions_EmptyFilterPassesAll verifies that an empty filter
// passes all entries regardless of FromModule.
func TestConvertToActions_EmptyFilterPassesAll(t *testing.T) {
	entries := []manifest.RestoreEntry{
		{Type: "copy", Source: "a", Target: "b", FromModule: "apps.vscode"},
		{Type: "copy", Source: "c", Target: "d", FromModule: "apps.git"},
		{Type: "copy", Source: "e", Target: "f"},
	}

	actions := convertToActions(entries, "")

	if len(actions) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(actions))
	}
}

// TestConvertToActions_ShortIDMatchesQualified verifies that a short module ID
// (e.g. "vscode") in the filter matches a qualified FromModule ("apps.vscode").
func TestConvertToActions_ShortIDMatchesQualified(t *testing.T) {
	entries := []manifest.RestoreEntry{
		{Type: "copy", Source: "a", Target: "b", FromModule: "apps.vscode"},
		{Type: "copy", Source: "c", Target: "d", FromModule: "apps.git"},
	}

	actions := convertToActions(entries, "vscode")

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].FromModule != "apps.vscode" {
		t.Errorf("expected FromModule=%q, got %q", "apps.vscode", actions[0].FromModule)
	}
}

// TestConvertToActions_MultipleFilterValues verifies that a comma-separated
// filter correctly matches multiple modules.
func TestConvertToActions_MultipleFilterValues(t *testing.T) {
	entries := []manifest.RestoreEntry{
		{Type: "copy", Source: "a", Target: "b", FromModule: "apps.vscode"},
		{Type: "copy", Source: "c", Target: "d", FromModule: "apps.git"},
		{Type: "copy", Source: "e", Target: "f", FromModule: "apps.obsidian"},
	}

	actions := convertToActions(entries, "vscode,apps.git")

	if len(actions) != 2 {
		t.Fatalf("expected 2 actions, got %d", len(actions))
	}
}

// TestConvertToActions_PropagatesFromModule verifies that FromModule is
// propagated from the manifest entry to the restore action.
func TestConvertToActions_PropagatesFromModule(t *testing.T) {
	entries := []manifest.RestoreEntry{
		{Type: "copy", Source: "a", Target: "b", FromModule: "apps.git"},
	}

	actions := convertToActions(entries, "")

	if len(actions) != 1 {
		t.Fatalf("expected 1 action, got %d", len(actions))
	}
	if actions[0].FromModule != "apps.git" {
		t.Errorf("expected FromModule=%q, got %q", "apps.git", actions[0].FromModule)
	}
}
