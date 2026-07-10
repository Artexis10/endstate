# Design: capture-pin

## D1 — One snapshot pass, two maps

Capture's step 2 already calls `snapshot.GetDisplayNameMap()` (a `winget list` via `TakeSnapshot`) and keeps only names; `parseWingetList` parses the Version column on the same rows. A sibling `GetVersionMap()` would spawn a second `winget list` — needless cost and a second shot at the SQLite lock contention landmine.

**Decision:** swap the capture seam from `getDisplayNameMapFn = snapshot.GetDisplayNameMap` to `listInstalledFn = snapshot.TakeSnapshot` and derive both maps (`id → name`, `id → version`, empties skipped) in one pass inside `RunCapture`. The `snapshot/` package is untouched; failure posture is unchanged (non-fatal, empty maps, existing retry-on-zero-apps logic).

## D2 — `--update` semantics: preserve, refresh, never blank

The app **set** comes from `winget export` (no versions); **versions** come from the `winget list` snapshot. The two can disagree (declared-but-not-installed apps, garbled rows), so version data is evidence, not truth. Pin fidelity also inherits the display-name pass's keying caveat: versions are keyed by the tabular `winget list` Id column, so a row whose Id winget truncates cannot be matched against the export set and yields no pin — accepted under the best-effort posture.

| Scenario | Behavior |
|---|---|
| `--update`, no `--pin` | Existing `version` and `driver` survive the merge (today both are silently dropped — a non-destructive-defaults violation; the realizer merge path already preserves them). New apps get no version. |
| `--update --pin` | Start from the existing pin; overwrite only when the app's `refs.windows` resolves to a **non-empty** snapshot version (capture records ground truth). Empty/missing lookup keeps the existing pin — absence of evidence never blanks declared state. New apps get best-effort versions. |
| `--pin`, fresh capture | Conversion loop populates `version` from the snapshot map (fallback: the snapshot app's own Version field); missing → field omitted via `omitempty`; capture never fails on a missing version. |
| no `--pin`, no `--update` | Byte-identical output to today. |

## D3 — Scope

- `--pin` is a capture-**emission** flag: no changes to events, envelope shape (`appsIncluded` etc.), module matching, or bundle layout. Apply/`--repin` consume `App.Version` unchanged.
- Winget path only. The realizer (Nix/brew) path records versions unconditionally per `nix-package-version-capture`; `--pin` is accepted as a no-op there — no realizer code change.
- Sanitize keeps versions (`cleanApp` gains `driver`/`version` copy-through, matching the realizer sanitize path). Versions are not machine-identifying; a sanitized shared manifest with pins is the reproducible-setup story.

## Naming

`capture --pin` records; `apply --repin` converges. No parser collision.
