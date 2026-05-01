## Why

The endstate-gui repo currently builds the engine from source using an `mklink /J` junction to the engine source tree, creating a tight coupling that breaks when the GUI is built outside the monorepo context. Publishing `endstate.exe` and a `.sha256` checksum as GitHub Release assets gives the GUI a stable, version-pinned download URL that decouples GUI builds from engine source availability.

## What Changes

- The `release.yml` workflow gains a second job (`publish-artifacts`) that runs on `windows-latest` after the GitHub Release is created by the tag push
- The job builds `endstate.exe` for `windows/amd64` with version embedded via ldflags
- The job generates `endstate.exe.sha256` (CertUtil-compatible hex digest)
- Both files are uploaded to the GitHub Release as assets using `softprops/action-gh-release@v2`
- A post-upload verification step fetches the release asset list and hard-fails (exit 1) if either file is missing
- The `auto-release-on-tag` spec requirement "No release artifacts" is replaced with "Binary artifacts attached to every release"

## Capabilities

### New Capabilities

- `engine-release-artifact-publish`: Attach `endstate.exe` and `endstate.exe.sha256` to every GitHub Release, at stable predictable URLs, with hard-fail verification that both are present.

### Modified Capabilities

- `auto-release-on-tag`: Requirement "No release artifacts" changes to "Binary artifacts attached to every release". The distribution mechanism shifts from `endstate bootstrap` (git clone) to pre-built binary download.

## Impact

- **`.github/workflows/release.yml`**: Add `publish-artifacts` job with build, checksum, upload, and verify steps
- **`openspec/specs/auto-release-on-tag/spec.md`**: Update "No release artifacts" requirement to reflect binary publishing
- No engine code changes; no release-please config changes
- GUI repo: can pin to `https://github.com/Artexis10/endstate/releases/download/v{VERSION}/endstate.exe` at build time
