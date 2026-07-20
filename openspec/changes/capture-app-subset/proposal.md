# Proposal: capture-app-subset

## Why

Selectivity is asymmetric. `apply --only <ids>` limits a run to chosen apps and is advertised in the capabilities handshake. Capture has no equivalent: the only lever is `--sanitize`, which attaches *no* config modules at all. There is no way to capture part of a machine.

That gap blocks the product's most distinctive use case — handing a curated setup to someone else. `winget export` already moves app lists; what makes Endstate different is that it moves *settings*. Sharing those settings requires choosing which ones, because nobody wants to hand over their whole machine.

## What Changes

- **`capture --only <id[,id,...]>`**, mirroring `apply --only` in name and shape so a selection reads the same on both sides.
- **A namespaced token grammar**, because capture selects across two kinds of thing: a bare token is a detected app id (`git-git`), an `apps.`-prefixed token is a config module id (`apps.vscode`). Bare is *always* an app — accepting bare module ids would make `vscode` ambiguous. This is a deliberate asymmetry with `--restore-filter`, which does accept short module ids.
- **Ref-only module matching under a selection.** A module attaches only when a selected app matches it by package reference, or when it is named outright.
- **`rebuild --only`** propagates the selection to apply, so a recipient can take part of a shared bundle.
- **Capability advertisement** for both, plus removal of `--filter` from `restore`'s advertised flags — `parseArgs` never handled it.

### The substantive part: pathExists containment

`matcher.go` checks `matches.pathExists` against the filesystem *without consulting the app list*, and **141 of 357 catalog modules declare it**. Filtering apps alone would therefore still bundle configs for most installed apps regardless of what was selected — a payload leak precisely in the flow where the artifact goes to another person.

`modules.MatchModulesForAppsSelective` matches by ref only; `MatchModulesForApps` is unchanged and remains the default for unfiltered runs.

Scoping is applied by **narrowing the catalog before planning** (`scopeCatalogToSelection`), not by filtering each module tier. Planning has two tiers with different discovery rules: legacy (schema v1) modules match against the app list, while generation-aware (schema v2) modules are discovered by detector eligibility *"even without an application match, so path-only instances remain discoverable."* That second rule is exactly what a selection must override. Narrowing the input constrains both tiers at once and leaves their internal logic untouched.

### Filter placement

The filter runs **after** deterministic ID assignment — the dedup suffixing there produces the ids a user actually sees and selects by — and **before** the `--update` merge. The ordering is load-bearing: `capture --only git-git --update` must ADD `git-git` to an existing manifest, not truncate that manifest to it. Filtering the newly discovered set pre-merge gives the former; post-merge would silently destroy the rest.

## Capabilities

### New Capabilities
- `capture-app-subset`: capture accepts an explicit app/module selection, and config modules attach only for selected apps or named modules.

### Modified Capabilities
<!-- none — apply --only is unchanged; rebuild gains propagation, covered by the new capability -->

## Impact

- `go-engine/internal/modules/matcher.go` — `MatchModulesForAppsSelective`; existing matcher becomes a wrapper over a shared core, behavior identical.
- `go-engine/internal/commands/capture.go` — `CaptureFlags.Only`, `captureSelection`, `parseCaptureOnly`, `validateCaptureOnly`, filter placement, counts.
- `go-engine/internal/commands/capture_config.go` — `scopeCatalogToSelection`; `captureSelectionError` so a mistyped token surfaces as `MANIFEST_VALIDATION_ERROR` rather than being flattened into `CAPTURE_FAILED` by the finalize path's plain-error return.
- `go-engine/internal/commands/capture_realizer.go` — same app filter after both capture lanes contribute.
- `go-engine/internal/commands/rebuild.go` — `RebuildFlags.Only` + propagation.
- `go-engine/cmd/endstate/main.go`, `capabilities.go`, `docs/contracts/cli-json-contract.md` — PROTECTED areas; modified under explicit instruction.
- Backward-compatible: omitting `--only` leaves every path byte-identical. No schema bump.

## Risk

`totalFound` stays pre-filter while deselected apps count as `skipped`, so `totalFound == included + skipped` still holds. A client that reads `skipped` as "the engine filtered these out" will now also see user-deselected apps there. If that distinction matters to the GUI, an additive `counts.notSelected` is a 1.x-compatible follow-up.

## Known gap

`data.appsIncluded[].id` carries the package *reference* (`Git.Git`) while `--only` matches the manifest app *id* (`git-git`), so capture output does not show the token a user must pass. Documented in the contract; clients should source ids from the written manifest. Worth closing before the flag is surfaced in a UI.
