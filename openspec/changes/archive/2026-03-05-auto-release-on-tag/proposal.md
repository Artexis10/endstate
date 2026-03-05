## Why

The previous release workflow ran unit tests, stamped a VERSION.txt, built a zip artifact, and created a GitHub Release — all on `windows-latest`. This was over-engineered: tests already run on every push/PR via CI, version stamping is unnecessary for a PowerShell project distributed via git clone/bootstrap, and the zip artifact was unused. The workflow needs to be simplified to just create a GitHub Release with changelog notes when a version tag is pushed.

## What Changes

- Remove the `test` job from the release workflow (tests already run in CI)
- Remove version stamping, release packaging, and zip artifact creation
- Switch runner from `windows-latest` to `ubuntu-latest` (no Windows-specific steps remain)
- Extract the matching changelog section from `CHANGELOG.md` as the release body
- Upgrade `softprops/action-gh-release` from v1 to v2
- Use explicit `body_path` instead of `generate_release_notes`
- Set `make_latest: true` and remove prerelease detection logic

## Capabilities

### New Capabilities
- `auto-release-on-tag`: Automatic GitHub Release creation from changelog when a `v*` tag is pushed

### Modified Capabilities

## Impact

- `.github/workflows/release.yml` — complete rewrite (simplified)
- No code changes to engine, drivers, or CLI
- No dependency changes
- Release artifacts (zip) will no longer be attached to GitHub Releases
