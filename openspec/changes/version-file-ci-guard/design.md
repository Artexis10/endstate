## Context

Release-please manages version bumps via `.release-please-manifest.json` and is configured (`release-type: simple`) to update the `VERSION` file. However, the VERSION file fell behind — it read 1.5.1 while the manifest had progressed to 1.7.2. This means any dev-mode build (`go run` without ldflags) reported the wrong version.

The compile-time ldflags change (see `compile-time-version-embedding`) makes production binaries immune to this drift, but dev-mode builds still rely on the file. A CI guard ensures the drift is caught immediately.

## Goals / Non-Goals

**Goals:**
- Detect VERSION ↔ manifest drift on every push and PR to main
- Fail hard (not warn) so the drift cannot be merged
- Fix the current drift (sync VERSION to 1.7.2)

**Non-Goals:**
- Auto-fixing the VERSION file in CI (developers should fix it in their PR)
- Guarding SCHEMA_VERSION (it has no release-please manifest counterpart)

## Decisions

1. **PowerShell step on windows-latest** — `go-ci.yml` already runs on `windows-latest`, so the check uses PowerShell (`shell: pwsh`). This avoids adding a separate job or runner.

2. **Placed before Vet/Test steps** — the check is a fast pre-flight that should fail early. No point running Go tests if the version file is wrong.

3. **Fail hard, not warn** — a drifted VERSION means dev-mode builds report the wrong version. This is a correctness issue, not a style nit.

4. **Read manifest via `ConvertFrom-Json`** — the manifest is a simple JSON object `{ ".": "1.7.2" }`. PowerShell's built-in JSON parsing is sufficient; no external tools needed.

## Risks / Trade-offs

- [Risk] Release-please updates manifest but not VERSION in the same commit → CI fails on the release PR. Mitigation: the release PR itself should include the VERSION bump; if release-please is misconfigured, the CI failure surfaces it immediately (which is the desired behavior).
- [Risk] PowerShell `Get-Content -Raw` includes trailing newline → Mitigation: `.Trim()` handles this.
