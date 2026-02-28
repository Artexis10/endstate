## Why

The `endstate bootstrap` command copies a subset of repo directories to `%LOCALAPPDATA%\Endstate\bin\`, but it misses `drivers/` and `verifiers/` — two directories that engine scripts dot-source via `$PSScriptRoot\..\drivers\` and `$PSScriptRoot\..\verifiers\`. This means the bootstrapped copy silently falls back to repo-root resolution (or fails outright when no repo root is configured), causing stale code execution and mysterious mismatches between repo and PATH-invoked behavior.

## What Changes

- Copy `drivers/` and `verifiers/` directories during bootstrap (currently missing)
- Always overwrite all synced directories unconditionally (remove existence-guarded skipping for self-copy detection when running from installed location — the self-copy guard on the `.ps1` entrypoint is fine, but directory syncs should always force-overwrite)
- Report total file count copied and any copy failures at the end of bootstrap
- Ensure `capabilities --json` `gitCommit` field reflects the repo HEAD after bootstrap

## Capabilities

### New Capabilities
- `bootstrap-full-sync`: Bootstrap produces a faithful, complete copy of all engine runtime directories from the source repo, reports copy statistics, and never silently skips files.

### Modified Capabilities

## Impact

- `bin/endstate.ps1` — `Install-EndstateToPath` function (lines 825–1131): add `drivers/` and `verifiers/` copy blocks, add file-count tracking and failure reporting
- No API or envelope changes — bootstrap is a local installation command with no JSON output contract
- No dependency changes
