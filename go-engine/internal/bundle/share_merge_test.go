// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// stageConfig writes a payload at configs/<rel> under root and returns the
// bundle-relative source path that a rewritten restore entry would carry.
func stageConfig(t *testing.T, root, rel, content string) string {
	t.Helper()
	full := filepath.Join(root, "configs", filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return "./configs/" + rel
}

// TestPreferMergeForShare_MergesStrictJSON verifies the main win: a recipient's
// own keys survive because the entry becomes a merge rather than a replace.
func TestPreferMergeForShare_MergesStrictJSON(t *testing.T) {
	root := t.TempDir()
	src := stageConfig(t, root, "vscode/settings.json", `{"editor.fontSize": 14}`)

	got := preferMergeForShare([]manifest.RestoreEntry{
		{Type: "copy", Source: src, Target: `%APPDATA%\Code\User\settings.json`},
	}, root)

	if got[0].Type != "merge-json" {
		t.Errorf("Type = %q, want merge-json", got[0].Type)
	}
	if !got[0].Backup {
		t.Error("share entries must force Backup so a merge is revertable")
	}
}

// TestPreferMergeForShare_JSONCStaysCopy is the one that prevents a broken
// restore. RestoreMergeJson parses with strict json.Unmarshal, which rejects
// comments — and VS Code's settings.json commonly has them. Retyping on
// filename alone would produce a bundle that fails mid-restore on the
// recipient's machine.
func TestPreferMergeForShare_JSONCStaysCopy(t *testing.T) {
	root := t.TempDir()
	src := stageConfig(t, root, "vscode/settings.json", `{
  // font size, chosen carefully
  "editor.fontSize": 14,
}`)

	got := preferMergeForShare([]manifest.RestoreEntry{
		{Type: "copy", Source: src, Target: `%APPDATA%\Code\User\settings.json`},
	}, root)

	if got[0].Type != "copy" {
		t.Errorf("Type = %q, want copy: JSONC cannot be merged by a strict JSON parser", got[0].Type)
	}
	if !got[0].Backup {
		t.Error("the copy fallback must still back up")
	}
}

// TestPreferMergeForShare_GitConfigStaysCopy guards against silent data loss.
// .gitconfig is INI-shaped, but MergeIni stores values in a map[string]string
// and so collapses duplicate keys, while git legitimately repeats them (several
// fetch refspecs, multiple insteadOf entries). Merging would drop data with no
// error.
func TestPreferMergeForShare_GitConfigStaysCopy(t *testing.T) {
	root := t.TempDir()
	src := stageConfig(t, root, "git/.gitconfig", "[user]\n\tname = Someone\n")

	for _, target := range []string{
		`%USERPROFILE%\.gitconfig`,
		`%USERPROFILE%\gitconfig`,
		`%APPDATA%\git\config`,
	} {
		got := preferMergeForShare([]manifest.RestoreEntry{
			{Type: "copy", Source: src, Target: target},
		}, root)
		if got[0].Type != "copy" {
			t.Errorf("target %s: Type = %q, want copy (merge-ini collapses git's duplicate keys)", target, got[0].Type)
		}
	}
}

// TestPreferMergeForShare_MergesPlainINI verifies real .ini targets do merge.
func TestPreferMergeForShare_MergesPlainINI(t *testing.T) {
	root := t.TempDir()
	src := stageConfig(t, root, "app/settings.ini", "[General]\nTheme=dark\n")

	got := preferMergeForShare([]manifest.RestoreEntry{
		{Type: "copy", Source: src, Target: `%APPDATA%\App\settings.ini`},
	}, root)

	if got[0].Type != "merge-ini" {
		t.Errorf("Type = %q, want merge-ini", got[0].Type)
	}
}

// TestPreferMergeForShare_LeavesNonCopyTypesAlone verifies an explicitly
// declared strategy is respected — a module author who chose append or
// registry-set knows something the sniff does not.
func TestPreferMergeForShare_LeavesNonCopyTypesAlone(t *testing.T) {
	root := t.TempDir()

	got := preferMergeForShare([]manifest.RestoreEntry{
		{Type: "append", Source: "./configs/x/a.txt", Target: `%APPDATA%\x\a.txt`},
		{Type: "registry-set", Key: `HKCU\Software\X`, ValueName: "V", Data: "1"},
	}, root)

	if got[0].Type != "append" {
		t.Errorf("declared append was retyped to %q", got[0].Type)
	}
	if got[1].Type != "registry-set" {
		t.Errorf("declared registry-set was retyped to %q", got[1].Type)
	}
	for i := range got {
		if !got[i].Backup {
			t.Errorf("entry %d: share entries must force Backup", i)
		}
	}
}

// TestPreferMergeForShare_UnreadablePayloadStaysCopy verifies an unresolvable
// source degrades to copy rather than to a merge that would fail at restore.
func TestPreferMergeForShare_UnreadablePayloadStaysCopy(t *testing.T) {
	root := t.TempDir()

	got := preferMergeForShare([]manifest.RestoreEntry{
		{Type: "copy", Source: "./configs/missing/settings.json", Target: `%APPDATA%\X\settings.json`},
	}, root)

	if got[0].Type != "copy" {
		t.Errorf("Type = %q, want copy for an unreadable payload", got[0].Type)
	}
}

// TestPreferMergeForShare_JSONArrayStaysCopy prevents a silent overwrite behind
// a merge label.
//
// DeepMerge only merges when both sides are objects; given an array source it
// replaces the target wholesale. VS Code's keybindings.json is a JSON array, so
// a "is it valid JSON" test would retype it to merge-json and then wipe the
// recipient's keybindings — the precise outcome share mode exists to avoid.
func TestPreferMergeForShare_JSONArrayStaysCopy(t *testing.T) {
	root := t.TempDir()
	src := stageConfig(t, root, "vscode/keybindings.json", `[{"key":"ctrl+b","command":"x"}]`)

	got := preferMergeForShare([]manifest.RestoreEntry{
		{Type: "copy", Source: src, Target: `%APPDATA%\Code\User\keybindings.json`},
	}, root)

	if got[0].Type != "copy" {
		t.Errorf("Type = %q, want copy: an array cannot be merged, only replaced", got[0].Type)
	}
}

// TestPreferMergeForShare_JSONScalarAndNullStayCopy covers the remaining shapes
// DeepMerge cannot merge.
func TestPreferMergeForShare_JSONScalarAndNullStayCopy(t *testing.T) {
	root := t.TempDir()

	for name, content := range map[string]string{
		"scalar": `42`,
		"string": `"hello"`,
		"null":   `null`,
	} {
		t.Run(name, func(t *testing.T) {
			src := stageConfig(t, root, name+"/f.json", content)
			got := preferMergeForShare([]manifest.RestoreEntry{
				{Type: "copy", Source: src, Target: `%APPDATA%\X\f.json`},
			}, root)
			if got[0].Type != "copy" {
				t.Errorf("Type = %q, want copy for %s payload", got[0].Type, name)
			}
		})
	}
}
