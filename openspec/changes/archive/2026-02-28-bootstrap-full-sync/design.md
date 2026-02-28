## Context

`Install-EndstateToPath` (bin/endstate.ps1 lines 825–1131) copies engine/, modules/, payload/, and restorers/ from the repo to `%LOCALAPPDATA%\Endstate\bin\`. Engine scripts (apply.ps1, verify.ps1, plan.ps1, json-output.ps1) dot-source `$PSScriptRoot\..\drivers\driver.ps1` and `$PSScriptRoot\..\verifiers\*.ps1`. Since `drivers/` and `verifiers/` are not bootstrapped, the installed copy relies on repo-root fallback resolution, which silently uses stale or missing files.

## Goals / Non-Goals

**Goals:**
- Bootstrap copies ALL runtime directories: engine/, modules/, payload/, restorers/, drivers/, verifiers/
- Every sync always force-overwrites (no conditional skipping)
- Bootstrap reports total files copied and any failures
- After bootstrap, PATH-invoked `endstate` behaves identically to repo-invoked `endstate`

**Non-Goals:**
- Changing the bootstrap destination layout (`%LOCALAPPDATA%\Endstate\bin\`)
- Adding JSON output mode to bootstrap command
- Modifying how engine scripts resolve dot-sourced dependencies
- Syncing bundles/, manifests/, or other repo directories that are not runtime dependencies

## Decisions

**1. Refactor directory copy into a helper function**
Each directory (engine, modules, payload, restorers) currently has ~30 lines of near-identical copy logic with source-resolution, self-copy detection, and conditional overwrite. Adding drivers/ and verifiers/ would add 60 more duplicated lines.

Decision: Extract a `Copy-BootstrapDirectory` helper that takes source name, handles self-copy detection, always force-overwrites, and returns a file count. This eliminates duplication and makes the copy-always behavior explicit.

Alternative considered: Inline the same pattern for drivers/ and verifiers/. Rejected because six copies of the same logic makes the always-overwrite invariant hard to enforce.

**2. Always overwrite, even when source == destination**
The current self-copy guard (`$resolvedSource -eq $resolvedDest → skip`) is correct for the entrypoint .ps1 (can't overwrite a running script), but for directories it causes the "stale bootstrap" problem when running `endstate bootstrap` from the installed copy.

Decision: Keep the self-copy guard only for `endstate.ps1` itself. For directories, always `Remove-Item -Recurse -Force` then `Copy-Item -Recurse -Force`, even when paths match. This ensures re-bootstrapping from the installed copy still works if the user has manually updated files.

**3. File count tracking**
Decision: Count files copied per directory using `Get-ChildItem -Recurse -File` on the destination after each copy. Report a summary line at the end: `[SYNC] Copied N files across M directories`.

## Risks / Trade-offs

- [Risk] `Remove-Item` then `Copy-Item` is not atomic — if the process crashes between remove and copy, the directory is gone → Mitigation: Bootstrap is a manual dev-time command, not a production operation. Users can re-run it.
- [Risk] Self-copy for directories when running from installed location would delete-then-copy from the same location → Mitigation: Self-copy detection still applies for directories when source and dest resolve to the same path. In that case, skip the copy (the files are already in place by definition). The key change is that we always overwrite when they're *different* — no existence check short-circuiting.
