## MODIFIED Requirements

### Requirement: Binary artifacts attached to every release
The release workflow SHALL attach binary artifacts (`endstate.exe` and `endstate.exe.sha256`) to every GitHub Release. Distribution is no longer limited to `endstate bootstrap` (git clone).

#### Scenario: Release includes binary assets
- **WHEN** a tag matching `v*` is pushed (e.g., `v1.0.0`, `v2.1.3`)
- **THEN** the release workflow SHALL execute, create a GitHub Release, and attach `endstate.exe` and `endstate.exe.sha256` as release assets

#### Scenario: Non-version tag does not trigger release
- **WHEN** a tag not matching `v*` is pushed (e.g., `test-123`)
- **THEN** the release workflow SHALL NOT execute

## REMOVED Requirements

### Requirement: No release artifacts
**Reason**: The GUI build pipeline now downloads `endstate.exe` at build time from release assets rather than building from source. Pre-built binaries must be attached to every release to support this cross-repo dependency.
**Migration**: Consumers previously using `endstate bootstrap` (git clone) may continue to do so. New consumers (GUI build pipeline) SHOULD download from `https://github.com/Artexis10/endstate/releases/download/v{VERSION}/endstate.exe` and verify against the `.sha256` file.
