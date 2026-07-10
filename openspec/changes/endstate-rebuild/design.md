# Design: endstate-rebuild

## Load-bearing fact: bundle restore paths resolve with zero apply-side rewriting

The whole feature rests on the capture bundle already being self-describing. The resolution chain, in-repo so the "does the payload resolve?" answer never has to be re-derived:

1. **Capture stages** each config file under `configs/<module-dir>/<leaf>` (`bundle/collect.go`, `CollectConfigFiles` — `configs/` + stripped-`apps.` module dir + `filepath.Base(dest)`).
2. **CreateBundle rewrites** every module restore `source` from `./payload/apps/<id>/<path>` to `./configs/<module-dir>/<leaf>` (`bundle/create.go`, `rewriteSourcePath`), and writes those rewritten entries into the staged `manifest.jsonc`. So the manifest inside the zip already points at the `configs/` layout — not the authoring-time `payload/` layout.
3. **ExtractBundle** unzips to a temp dir and returns the path to `manifest.jsonc` inside it (`bundle/extract.go`). The extracted tree is `<tmp>/manifest.jsonc` + `<tmp>/configs/<module>/…` + `<tmp>/metadata.json`.
4. **Apply Phase 2b** resolves each restore `source` relative to `ManifestDir` = `filepath.Dir(flags.Manifest)` (`restore/restore.go`, `resolveSource`). Because `flags.Manifest` is the extracted `manifest.jsonc`, `./configs/<module>/<leaf>` resolves to `<tmp>/configs/<module>/<leaf>` — the extracted payload. **No apply-side path rewriting is required.**

`bundle.ExtractBundle`/`bundle.IsBundle` are currently dead code — no production caller. `rebuild` is the wiring that turns them on. `modules.ExpandConfigModules` also has no production caller, so there is no risk of a second, competing restore path (no double-restore): the bundle manifest's already-rewritten `restore[]` entries are the single source of restore truth.

## Refuse before mutation

The confirmation gate is evaluated **before any filesystem mutation** — before extraction, before planning, before install. Ordering in `RunRebuild`:

1. Validate `--from`: empty → `MANIFEST_VALIDATION_ERROR`; contains `://` → `NOT_SUPPORTED`; `os.Stat` miss → `MANIFEST_NOT_FOUND`. (These are read-only checks.)
2. **Confirmation gate:** a run that is neither `--dry-run` nor `--no-restore` and lacks `--confirm` returns `CONFIRMATION_REQUIRED` with remediation "Re-run with --confirm to proceed, or --dry-run to preview." Nothing has been extracted or mutated at this line.
3. Only then extract / plan / install / restore / verify.

`--dry-run` (preview only) and `--no-restore` (install without touching configuration) are the two non-destructive lanes and need no confirmation. The gate reuses the already-parsed `--confirm` flag — the same consent primitive `apply --prune`/`apply --repin` use.

## Temp-dir lifetime outlives the full pipeline by construction

For a `.zip` input, `ExtractBundle` creates the temp dir; `RunRebuild` registers `defer os.RemoveAll(filepath.Dir(manifestPath))` immediately after a successful extraction. Because the cleanup is deferred at the orchestrator scope, the extracted `configs/` payload is guaranteed to still exist through the entire install → restore → verify sequence and is removed only when `RunRebuild` returns — on the success path *and* on any mid-pipeline error path. A bare `.jsonc` input registers no cleanup (nothing was extracted). Extraction failure is self-cleaning: `ExtractBundle` removes its own temp dir before returning the error, so the `defer` is never registered for a failed extraction.

## Composition, not reimplementation

`RunRebuild` calls the existing `RunApply` and `RunVerify` with the extracted (or bare) manifest path:

- `RunApply(ApplyFlags{Manifest, DryRun, EnableRestore: !NoRestore, Events})` — apply already owns plan + install + restore (Phase 2b) + its own internal verify. Its `*envelope.Error` is propagated as-is.
- `RunVerify(VerifyFlags{Manifest, Events})` — skipped on `--dry-run`. Verify is a superset of apply's Phase 3: it re-detects apps *and* dispatches the manifest's `verify[]` block and version-drift checks, so it is the authoritative post-rebuild assertion. Verify failures live in `summary.fail`; they are **data**, not an envelope error (precedent: `schedule run` — "drift is data"). `rebuild` returns a success envelope, exit 0, even when apply/verify summaries contain failures.

## Events / runId

`rebuild` composes apply's and verify's **existing** event streams unchanged. Each sub-run opens with a `phase` event and closes with a `summary` event, so the concatenation still satisfies the event-contract ordering invariant (first event is a phase, last event is a summary). No new event types are introduced; the schema stays v1.

Known, pre-existing divergence (documented, not fixed here): apply emits under an internal `apply-<ts>` runId and verify under `verify-<ts>`, while the `rebuild` JSON envelope carries its own `rebuild-<ts>` runId. The per-file "all events share one runId" invariant already holds *within* each phase group; unifying the streamed runId with the envelope runId is a cosmetic GUI-facing follow-up and is intentionally out of scope so this change does not refactor apply's internal runId plumbing.

## Non-goals (v0)

- **URL input.** `--from https://…` (any `://`) is rejected with `NOT_SUPPORTED` and remediation "URL input is not supported; download the bundle and pass a local path." Remote fetch, integrity verification, and caching are deferred; v0 is local-file only.
- No new event types, no schema bump, no changes to `capture`, `restore`, or `revert`.
- No GUI changes (a one-click rebuild affordance is a separate `endstate-gui` change gated on `commands.rebuild`).
