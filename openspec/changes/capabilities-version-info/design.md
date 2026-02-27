## Context

The `capabilities` command (`engine/json-output.ps1` > `Get-CapabilitiesData`) returns a JSON object describing supported commands, features, and platform info. The GUI calls this on startup for handshake/compatibility. Currently there is no way for the GUI to determine whether the engine copy is stale (old commit) or has local modifications.

The existing `Get-EndstateVersion` and `Get-GitSha` helpers in `bin/cli.ps1` already demonstrate the pattern for safe git queries with fallback to `$null`.

## Goals / Non-Goals

**Goals:**
- Expose `gitCommit`, `gitDirty`, and `bootstrapTimestamp` in the capabilities `data` object
- Fields are nullable -- return `$null` when git is unavailable or bootstrap info is absent
- Work in both direct repo execution and bootstrapped (copied) installations
- Additive change with no schema version bump

**Non-Goals:**
- Auto-update or self-healing when staleness is detected (GUI responsibility)
- Changing the JSON envelope structure
- Adding version-info fields to any command other than capabilities

## Decisions

1. **Git queries use `$repoRoot` from existing `$script:JsonOutputRoot`** -- `Get-CapabilitiesData` already resolves the repo root via `$script:JsonOutputRoot`. Git commands will use `-C $script:JsonOutputRoot` to ensure they run against the correct repo regardless of current working directory.

2. **`bootstrapTimestamp` reads from `engine/version-info.json`** -- A lightweight JSON file (`{ "bootstrapTimestamp": "2026-02-27T10:00:00Z" }`) written by the bootstrap/install process. If the file does not exist (direct repo execution), `bootstrapTimestamp` is `$null`. This avoids coupling to any specific bootstrap mechanism.

3. **No schema version bump** -- Per the CLI JSON contract, additive optional fields are backward-compatible. GUI consumers ignore unknown fields (graceful degradation principle).

4. **Error suppression** -- Git commands use `2>$null` with try/catch to handle environments where git is not installed. `$gitDirty` defaults to `$false` on error (conservative -- assume clean if unknown).

## Risks / Trade-offs

- [Risk] Git not on PATH in bootstrapped environments -> Mitigation: All git fields return `$null`; GUI treats null as "unknown"
- [Risk] `git status --porcelain` slow on large repos -> Mitigation: Capabilities is called once at startup; latency is acceptable
- [Risk] `version-info.json` missing in dev workflow -> Mitigation: Field is nullable by design; absence is the expected dev-mode behavior
