# engine-release-artifact-publish Specification

## Purpose
Defines the requirements for attaching pre-built engine binaries and checksums to every GitHub Release, enabling cross-repo consumers (e.g., the GUI build pipeline) to download a version-pinned `endstate.exe` without building from source.

## Requirements

### Requirement: Binary artifact attached to every GitHub Release
The release workflow SHALL build `endstate.exe` for `windows/amd64` and attach it as a release asset to the GitHub Release created for each `v*` tag.

#### Scenario: Release includes endstate.exe
- **WHEN** a tag matching `v*` is pushed (e.g., `v1.7.7`)
- **THEN** the release workflow SHALL produce `endstate.exe` via `go build`
- **AND** `endstate.exe` SHALL be uploaded as an asset to the corresponding GitHub Release

#### Scenario: Binary has version embedded
- **WHEN** `endstate.exe` is built for a release tag `v{VERSION}`
- **THEN** the binary SHALL have been compiled with `-ldflags "-X github.com/Artexis10/endstate/go-engine/internal/config.version={VERSION}"`
- **AND** `endstate capabilities` (or equivalent) SHALL report `{VERSION}` as the CLI version

### Requirement: SHA-256 checksum attached to every GitHub Release
The release workflow SHALL generate `endstate.exe.sha256` containing the SHA-256 hex digest of `endstate.exe` and attach it as a release asset alongside the binary.

#### Scenario: Checksum file present on release
- **WHEN** a `v*` tag is pushed
- **THEN** the release workflow SHALL produce `endstate.exe.sha256` as a bare 64-character lowercase hex string
- **AND** `endstate.exe.sha256` SHALL be uploaded as an asset to the corresponding GitHub Release

#### Scenario: Checksum matches binary
- **WHEN** a consumer downloads both `endstate.exe` and `endstate.exe.sha256`
- **THEN** computing the SHA-256 of `endstate.exe` SHALL produce the hex string in `endstate.exe.sha256`

### Requirement: Post-upload verification hard-fails if artifacts are missing
After uploading release assets, the workflow SHALL verify that both `endstate.exe` and `endstate.exe.sha256` are present on the release and SHALL fail with a non-zero exit code if either is absent.

#### Scenario: Both artifacts present — workflow succeeds
- **WHEN** both `endstate.exe` and `endstate.exe.sha256` are successfully uploaded
- **THEN** the verification step SHALL exit 0 and the workflow SHALL succeed

#### Scenario: An artifact is missing — workflow fails
- **WHEN** either `endstate.exe` or `endstate.exe.sha256` is not present on the release after upload
- **THEN** the verification step SHALL exit non-zero and the workflow SHALL be marked as failed

### Requirement: Stable, predictable artifact download URLs
Release assets SHALL be accessible at stable URLs following the pattern:
`https://github.com/Artexis10/endstate/releases/download/v{VERSION}/{filename}`

#### Scenario: Binary URL is stable
- **WHEN** release `v{VERSION}` exists with `endstate.exe` attached
- **THEN** `https://github.com/Artexis10/endstate/releases/download/v{VERSION}/endstate.exe` SHALL return the binary with HTTP 200

#### Scenario: Checksum URL is stable
- **WHEN** release `v{VERSION}` exists with `endstate.exe.sha256` attached
- **THEN** `https://github.com/Artexis10/endstate/releases/download/v{VERSION}/endstate.exe.sha256` SHALL return the checksum file with HTTP 200

### Requirement: Artifact build runs on windows-latest
The build job SHALL run on `windows-latest` to produce a native Windows binary without cross-compilation.

#### Scenario: Runner is windows-latest
- **WHEN** the artifact publish job executes
- **THEN** the job runner SHALL be `windows-latest`
