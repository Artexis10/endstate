## Context

The current `release.yml` workflow creates a GitHub Release with auto-generated notes but attaches no binary assets. The `auto-release-on-tag` spec codifies this as intentional ("distribution via `endstate bootstrap`"). The GUI repo is now planned to download the pre-built binary at GUI build time, requiring a stable artifact URL at every release. The engine already supports ldflags version embedding via `internal/config.version` and `internal/config.schemaVersion`.

## Goals / Non-Goals

**Goals:**
- Attach `endstate.exe` (windows/amd64) and `endstate.exe.sha256` to every GitHub Release
- Embed the release version via ldflags so `endstate capabilities` reports the correct version
- Hard-fail the workflow if either artifact is missing after upload
- Produce stable, predictable download URLs the GUI repo can pin to

**Non-Goals:**
- Building for platforms other than windows/amd64 (the engine is Windows-only by design)
- Changing the release-please configuration or tagging strategy
- Modifying engine source code beyond what ldflags already support
- Publishing to package registries (winget, Chocolatey) — separate concern

## Decisions

### Decision: Extend `release.yml`, not `release-please.yml`

The release-please workflow runs on push to `main` and manages the release PR lifecycle. The actual release creation happens when `release-please` merges the release PR and pushes a tag. The existing `release.yml` already triggers on `v*` tags and creates the GitHub Release. Adding the build job there keeps artifact creation co-located with release creation and avoids a race condition between two workflows fighting over the same release.

**Alternative considered**: A separate `release-artifacts.yml` triggered on `release: published`. Rejected because it requires two workflow files and introduces a timing dependency (the release must exist before assets can be uploaded).

### Decision: `windows-latest` runner for the build job

`endstate.exe` is a Windows-only binary. Using `windows-latest` avoids cross-compilation toolchain complexity and produces a native binary without CGO or cross-compile flags.

**Alternative considered**: Cross-compile from `ubuntu-latest` with `GOOS=windows GOARCH=amd64`. Viable but adds complexity and requires explicit CGO disabling. The binary has no CGO dependencies (`golang.org/x/sys` is pure Go on windows), but keeping the runner native is safer for future CGO additions.

### Decision: `CertUtil -hashfile` for SHA-256 on Windows runner

Windows doesn't have `sha256sum` by default. `CertUtil` is available on all Windows runners without extra installation.

**Format**: `CertUtil -hashfile endstate.exe SHA256 | findstr /v "hash\|CertUtil" > endstate.exe.sha256`

Output is a bare 64-char hex string, consistent with standard `.sha256` file conventions.

**Alternative considered**: PowerShell `Get-FileHash`. Also viable, but CertUtil is more universally available and produces the expected format without string manipulation.

### Decision: Reuse `softprops/action-gh-release@v2` for asset upload

Already used in `release.yml`. Passing `tag_name` and `files` to an existing release appends assets rather than recreating the release, which is the desired behavior since the release is created in the same job.

### Decision: Post-upload verification via `gh release view`

After upload, run `gh release view $TAG --json assets` and parse with `jq` to assert both filenames are present. Fail with a non-zero exit code if either is missing. This is the hard-fail required by the constraint. The `gh` CLI is available on all GitHub-hosted runners.

**Alternative considered**: Trust the upload step's success. Rejected — silent drift (upload succeeds but asset is corrupt or missing) is the exact failure mode this change prevents.

### Decision: ldflags version embedding using `github.ref_name`

The tag name (e.g., `v1.7.7`) is available as `${{ github.ref_name }}`. Strip the `v` prefix with shell substitution `${GITHUB_REF_NAME#v}` to get the bare semver for the ldflags value.

## URL Contract

```
https://github.com/Artexis10/endstate/releases/download/v{VERSION}/endstate.exe
https://github.com/Artexis10/endstate/releases/download/v{VERSION}/endstate.exe.sha256
```

These URLs are stable as long as the GitHub repository and release tag exist. GitHub does not change asset URLs after upload.

## Risks / Trade-offs

| Risk | Mitigation |
|------|-----------|
| `windows-latest` runner image changes break build | Go 1.22 is stable; pin `go-version` in setup-go to avoid surprise upgrades |
| CertUtil output format changes | Use `findstr` filter defensively; add an integration test in a future change |
| Upload succeeds but asset is zero bytes | `gh release view` check validates presence by name, not size — acceptable for now; checksum verification by the GUI at download time catches corruption |
| Release created but artifact job fails — release has no assets | The post-upload verify step catches this; the job is not `continue-on-error` |
| Cross-repo consumers pin the wrong URL pattern | The URL contract is documented in design.md and the spec; GUI must read both |

## Migration Plan

1. Merge the `release.yml` workflow change to `main`
2. The next release-please PR merge triggers a tag → the new job runs automatically
3. To test before a real release: create a test tag `v0.0.0-test` manually and delete it after confirming assets appear
4. GUI repo update: replace the `mklink /J` junction with a `curl` download of `endstate.exe` from the release URL, verifying against `.sha256`

**Rollback**: Remove the `publish-artifacts` job from `release.yml`. Existing releases retain their assets. Future releases revert to no-artifact behavior.

## Open Questions

- Should the GUI repo pin to `latest` release or a specific version tag? (Recommendation: specific tag, resolved at GUI build time by reading the engine's `VERSION` file)
- Does the GUI build pipeline have `gh` CLI available, or should it use `curl` directly? (Out of scope for this change)
