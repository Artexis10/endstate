// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// preferMergeForShare rewrites a share bundle's restore entries to layer the
// sender's settings onto the recipient's instead of replacing them, and forces a
// backup on every entry.
//
// Only share bundles get this. A self-rebuild wants the captured config to win
// outright — merging there would let stale local keys survive a restore that was
// meant to return the machine to a known state. Sharing is the opposite case:
// the recipient has their own settings and did not ask to lose them.
//
// The decision is made at capture time and encoded in the bundle's restore
// types, so an older engine applying a newer share bundle still merges.
//
// Retyping is deliberately conservative — a wrong merge silently corrupts a
// config file, which is worse than an honest replace-with-backup:
//
//   - copy -> merge-json only when the staged payload parses as a strict JSON
//     OBJECT. Two separate reasons, both load-bearing: RestoreMergeJson uses
//     json.Unmarshal, which rejects comments and trailing commas, so VS
//     Code-style JSONC would fail mid-restore; and DeepMerge only merges when
//     both sides are objects, replacing wholesale otherwise. A JSON array
//     payload (VS Code keybindings.json) would therefore pass a
//     "is it valid JSON" test and then silently overwrite the recipient's file
//     under a merge label — the exact outcome sharing is meant to avoid.
//   - copy -> merge-ini only for .ini targets. Notably NOT .gitconfig: MergeIni
//     stores values in a map[string]string and so collapses duplicate keys,
//     while git config legitimately repeats keys (multiple fetch refspecs,
//     insteadOf entries). Merging there would drop data.
//   - everything else keeps copy, with backup forced on.
//
// stagingRoot is the directory holding the bundle's configs/ tree, used to
// resolve each entry's rewritten ./configs/... source for sniffing.
func preferMergeForShare(entries []manifest.RestoreEntry, stagingRoot string) []manifest.RestoreEntry {
	out := make([]manifest.RestoreEntry, 0, len(entries))
	for _, entry := range entries {
		// Backup is the safety net behind every strategy below, including the
		// merges: a merge that produces an unwanted result is still revertable.
		entry.Backup = true
		if entry.Type == "" || entry.Type == "copy" {
			entry.Type = shareRestoreTypeFor(entry, stagingRoot)
		}
		out = append(out, entry)
	}
	return out
}

// shareRestoreTypeFor picks the restore type for one share-bundle entry.
func shareRestoreTypeFor(entry manifest.RestoreEntry, stagingRoot string) string {
	if mergeableINITarget(entry.Target) {
		return "merge-ini"
	}
	if stagedPayloadIsMergeableJSON(entry.Source, stagingRoot) {
		return "merge-json"
	}
	return "copy"
}

// mergeableINITarget reports whether a target is an INI file safe to merge.
//
// Extension-based, and deliberately narrow. A file being INI-shaped is not
// enough: git config is INI-shaped but relies on duplicate keys that MergeIni
// would collapse.
func mergeableINITarget(target string) bool {
	base := strings.ToLower(filepath.Base(strings.ReplaceAll(target, `\`, "/")))
	if base == ".gitconfig" || base == "gitconfig" || base == "config" {
		return false
	}
	return strings.HasSuffix(base, ".ini")
}

// stagedPayloadIsMergeableJSON reports whether the bundled payload for this
// entry is something RestoreMergeJson can genuinely merge: a strict JSON object.
//
// Unmarshalling into map[string]interface{} enforces both conditions at once —
// it rejects JSONC (comments, trailing commas) and rejects arrays and scalars,
// which DeepMerge would replace rather than merge.
//
// A source that cannot be resolved or read is reported as not mergeable, so an
// unreadable payload degrades to copy rather than to a restore that fails.
func stagedPayloadIsMergeableJSON(source, stagingRoot string) bool {
	rel := strings.TrimPrefix(strings.ReplaceAll(source, `\`, "/"), "./")
	if !strings.HasPrefix(rel, "configs/") {
		return false
	}
	data, err := os.ReadFile(filepath.Join(stagingRoot, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	var probe map[string]interface{}
	if err := json.Unmarshal(data, &probe); err != nil {
		return false
	}
	// "null" unmarshals into a nil map without error, and DeepMerge treats a nil
	// source as "keep the target" — a merge that does nothing.
	return probe != nil
}
