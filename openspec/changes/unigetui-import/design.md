# Design: unigetui-import

## Context

UniGetUI serializes backups and bundles with the same C# models (`SerializableBundle`: `export_version` (currently 3), `packages[]`, `incompatible_packages_info`, `incompatible_packages[]`; `SerializablePackage`: `Id`, `Name`, `Version`, `Source`, `ManagerName`, optional `InstallationOptions` incl. a `Version` pin, optional `Updates`). Files are JSON with a `.ubundle` extension; local periodic backups are named `<COMPUTERNAME> installed packages <timestamp>.ubundle`; the v3.3.0 cloud backup syncs the same JSON to a private GitHub Gist. Real backups show `winget` as the dominant source with chocolatey/npm/pip/PSGallery entries alongside, and Microsoft Store / "Local PC" apps relegated to `incompatible_packages`.

Endstate has no import/convert command today; manifests are JSONC loaded via `manifest.LoadManifest`. The engine's app entry shape (`App{ID, Refs, Version, DisplayName}`) maps cleanly onto winget-source packages.

## Goals / Non-Goals

**Goals:**
- One command turns a `.ubundle` into a valid, human-readable JSONC manifest a user can immediately `plan`/`apply`, with module-catalog matching then lighting up config restore for known apps.
- Total transparency about fidelity: every package that doesn't map is listed with a reason.
- Zero side effects: import never installs, never touches the network, and is deterministic.

**Non-Goals (v0):**
- No import of UniGetUI's own settings; no chocolatey/scoop install lanes (winget is Endstate's Windows driver); no `.ubundle` export; no direct Gist download (users export/download the file themselves); no YAML/XML bundle variants (UniGetUI's canonical interchange is JSON).

## Decisions

1. **Command shape `import --from unigetui --path <file> [--out <path>] [--pin] [--json]`** — a generic `import` verb with a `--from` source keeps the door open for `--from dsc` later under the same `manifest-import` capability. *Alternative:* `import-unigetui` dedicated verb — rejected; verb proliferation in a hand-rolled dispatcher.
2. **Strict-but-forward-compatible parsing.** `export_version` other than 3 produces a warning, not a failure, provided required fields parse; unknown JSON fields are ignored (Go default). Rationale: UniGetUI bumps the version on additive changes; hard-failing would break on their next release.
3. **Mapping policy.** `Source == "winget"` (case-insensitive; `ManagerName` as fallback discriminator) → app entry: slugged `ID` (lowercased last segment of the winget Id, deduplicated), `refs.windows = Id`, `displayName = Name`. Version pinning is opt-in via `--pin` (bundle `Version` is "installed at backup time", which self-updating apps churn); a per-package `InstallationOptions.Version` pin is authored intent and therefore wins over the observed `Version` when `--pin` is set. *Alternative:* always pin — rejected; contradicts the version-drift-noise posture established for `capture` pinning.
4. **Skip transparency as a first-class output.** The command result (text and `--json` envelope) contains `imported[]`, `skipped[]` (each with manager + reason), and `incompatible[]` passed through from the bundle. No silent drops — this mirrors the "explicit about what is NOT moved" positioning rule from the market brief.
5. **Output is JSONC with a generated header comment** naming the source file and tool version, written to `--out` (default `manifests/local/imported-unigetui.jsonc` — gitignored per repo conventions). Output loads via `manifest.LoadManifest` round-trip as a validity gate before writing.
6. **Package layout**: parser and mapper in `internal/importer` (pure, hermetic, fixture-tested); command glue in `internal/commands/import.go`; dispatcher entry in `cmd/endstate/main.go` (protected area — explicit instruction 2026-07-10).

## Risks / Trade-offs

- [UniGetUI schema drift (v4+)] → warn-don't-fail on version mismatch; parser tolerates unknown fields; fixture from a real-world gist backup locks current behavior.
- [Winget IDs in bundle not present in winget catalog anymore] → import doesn't resolve against winget (pure transform); `plan`/`apply` surface unavailable packages through existing paths.
- [Slug collisions (two packages → same app id)] → deterministic de-dup suffixing; collision listed in the summary.
- [Users expect settings to import too] → the summary explicitly states that config restore comes from Endstate's module catalog matching, not from the bundle — managing exactly the expectation the UniGetUI thread will create.

## Migration Plan

Purely additive; rollback = remove the dispatcher entry. No schema or contract changes.

## Open Questions

- Whether `--from dsc` lands in the same release train (separate change; this design only reserves the flag shape).
- Whether to accept `unigetui://` deep-link payloads or Gist URLs later — deferred until the interop conversation with the UniGetUI project happens.
