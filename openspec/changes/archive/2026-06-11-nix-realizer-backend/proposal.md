## Why

The `platform-backend-foundation` change made backend selection, ref resolution, capabilities, and paths platform-aware, leaving a single explicit insertion point — non-Windows hosts return `ErrNoBackend` — with **zero Windows behavior change**. This change fills that gap with a **Nix package backend** for Linux and macOS, so the engine can install packages on those hosts.

The validation spike (Determinate Nix 3.21.0, verdict **YELLOW**) confirmed the realizer model is viable: a `nix profile` generation advances **only on full success** (atomicity holds), and every observed failure class is translatable to a stable engine `error.code` via a small, contract-tested anchor map. Nix's whole-set declarative model does not fit the per-package `Driver` interface, so this change introduces a **`Realizer` interface beside `Driver`** (never shoehorned into `Driver.Install`) and wires it into the pipeline behind the existing per-item event contract.

In v1, **Nix owns packages only** — the module/restore/verify/capture layers are untouched and keep owning configuration.

## What Changes

- Add a **`Realizer` interface** (`internal/realizer`) beside `driver.Driver`: `Plan(desired) → generation diff`, `Realize(toAdd) → generation result`, `Current() → installed set`. The whole-set model is first-class; it is **not** a `Driver`.
- Add a **Nix realizer** (`internal/realizer/nix`) that shells out to `nix profile add` (the supported verb; **not** the deprecated `install` alias) with `--log-format internal-json`, and reads `nix profile list --json` (version 3, name-keyed object; legacy array tolerated). Build-tag split (`nix_other.go` / `nix_windows.go`) per repo convention.
- Add `classify(exitCode, internal-json events, generationAdvanced) → engine error.code` — the **single** source of the code. Structural signals carry the pipeline stage/subcode; a **locked anchor table** carries the top-level class for the daemon/permission/eval classes the spike proved are not structurally separable. Anchors are harvested from real pinned-Nix output and **locked behind a hermetic contract test**.
- Add a GOOS-keyed **`selectRealizer(goos)`** (linux/darwin → Nix; else `ErrNoRealizer`) and a `newRealizerFn` injection seam beside `newDriverFn`. `selectBackend` (winget) is unchanged.
- `apply` / `verify` / `plan` gain an early backend-kind fork: when a realizer is selected, a whole-set path computes one plan, performs **one atomic generation switch**, and **fans the single result back into the existing per-item event stream** (same phases, statuses, and summaries the GUI consumes). Raw Nix text appears **only** in `error.detail`.
- `capabilities` reports `drivers: ["nix"]` on Linux/macOS via `driversFor`.
- Map `App.Refs["linux"]`/`["darwin"]` to a **pinned** nixpkgs installable; bare attributes (`ripgrep`) are expanded against an engine-owned pinned revision, explicit flakerefs pass through. On the realizer path, an app with no Nix ref is **skipped** (never falls back to a winget id).
- Add additive error code **`REALIZER_UNAVAILABLE`** (the Nix analogue of `WINGET_NOT_AVAILABLE`).

## Capabilities

### New Capabilities

- `nix-package-backend`: install/detect packages on Linux and macOS through a whole-set Nix `Realizer`, classify Nix failures to stable engine error codes with raw text confined to `error.detail`, map pinned nixpkgs refs, and preserve the per-item event contract — all with no Windows behavior change.

### Modified Capabilities

(none — Windows behavior is invariant; every addition is platform-gated and additive. The foundation's `platform-backend-selection` requirements remain true: Linux/macOS simply move from "no backend" to "the Nix backend".)

## Impact

- `internal/realizer/realizer.go` (new) — `Realizer` interface + `Installable`/`Set`/`Diff`/`Result`/`Error` types.
- `internal/realizer/nix/` (new) — Nix backend (`nix profile add`/`list`), `classify`, internal-json + profile-list parsers, locked anchor contract test, real-Nix-3.21.0 fixtures under `testdata/`.
- `internal/commands/select.go` — add `selectRealizer(goos)` + `ErrNoRealizer`.
- `internal/commands/verify.go` — add `newRealizerFn` seam; early realizer fork in `RunVerify` (+ `RunPlan`).
- `internal/commands/apply.go` — ~3-line early realizer fork delegating to `runApplyRealizer`.
- `internal/commands/{apply,verify,plan}_realizer.go` (new) — whole-set fan-out keeping the originals' diffs minimal.
- `internal/commands/capabilities.go` — `driversFor` consults `selectRealizer`.
- `internal/envelope/errors.go` — add `ErrRealizerUnavailable`.
- `docs/contracts/cli-json-contract.md` — **PROTECTED**: additive `REALIZER_UNAVAILABLE` error-code row + one-line clarification that `platform.drivers` is `["nix"]` on Linux/macOS. **Flagged for explicit go-ahead at implementation time.**
- No schema version bump: `refs["linux"]`/`refs["darwin"]` keys are additive per `schema-versioning`; the `drivers` array gains the value `"nix"` (no new field).
