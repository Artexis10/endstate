## Context

The Spec → Planner → Drivers → Verifiers pipeline is platform-agnostic above the driver layer, but selection and the surrounding ref/capabilities/path logic are hardcoded to Windows + winget. The validation spike (Determinate Nix 3.21.0) confirmed the Nix realizer model is viable (atomicity holds; error surface translatable via a tested anchor map — YELLOW). This change is the platform-agnostic foundation that the Nix backend slots into. It mirrors the repo's established per-OS split (`registry_windows.go`/`registry_other.go`, `keychain_windows.go`/`keychain_other.go`).

## Goals / Non-Goals

**Goals:**
- GOOS-keyed backend selection with a single, explicit insertion point for future backends.
- Platform-keyed ref resolution, dynamic capabilities, XDG paths, platform-aware env expansion.
- **Zero behavior change on Windows** — provable by regression tests.

**Non-Goals:**
- No Nix / realizer implementation (next change).
- No new `Realizer`/`Backend` interface yet — introduced in the Nix change where the whole-set declarative model actually needs it; introducing it here would be a speculative, unused abstraction.
- No per-app `App.Driver` backend override (a later decision); selection is purely GOOS-based.
- No verifier/restore changes (separate change) and no bootstrap/release changes.

## Decisions

1. **`SelectBackend(goos string) (driver.Driver, error)`** in `internal/driver/` — `windows` → `winget.New()`; default → a sentinel `ErrNoBackend`. The existing `Driver` interface is kept; selection is the only new seam. Call sites (`commands/verify.go`, `apply.go`, `plan.go`) pass `runtime.GOOS`.

2. **`resolveRef` prefers `App.Refs[runtime.GOOS]`, else the first non-empty ref** — byte-identical to today's `resolveWindowsRef` on a Windows host with `refs.windows`, so no Windows behavior changes.

3. **Capabilities are dynamic** — `os` from `runtime.GOOS`; `drivers` from what `SelectBackend` can provide on this host. On Windows this still yields `windows` / `["winget"]`. Additive contract fields only.

4. **Paths** — `ProfileDir()` returns the existing `Documents\Endstate\Profiles` on Windows, and `$XDG_DATA_HOME`/`~/.local/share/endstate/profiles` on Linux. Env expansion dispatches: `%VAR%` (Windows) vs `os.ExpandEnv` `$VAR` (other), via one `ExpandEnvVars` entry point that the four current `ExpandWindowsEnvVars` callers route through.

5. **Build-tag split where OS-specific code is unavoidable** — follow the existing `*_windows.go` / `*_other.go` convention rather than scattering `runtime.GOOS` branches.

## Risks / Trade-offs

- **[Risk] A behavior change leaks onto Windows** → Mitigation: the Windows code path is literally unchanged; add regression tests asserting `capabilities` reports `os=windows` + `drivers=["winget"]`, `resolveRef` picks the same ref, and `ProfileDir` is unchanged.
- **[Risk] `resolveRef` fallback changes selection for multi-ref manifests** → Mitigation: fallback order is identical to `resolveWindowsRef`; covered by a test with a multi-ref app.
- **[Risk] Non-Windows `ErrNoBackend` is confusing in isolation** → Accepted: it is explicit and temporary; the Nix change fills it. Surfaced clearly rather than silently no-op.
