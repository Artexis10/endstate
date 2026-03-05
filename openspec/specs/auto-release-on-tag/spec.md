# auto-release-on-tag Specification

## Purpose
TBD - created by archiving change auto-release-on-tag. Update Purpose after archive.
## Requirements
### Requirement: Release triggered on version tag push
The system SHALL create a GitHub Release when a tag matching `v*` is pushed to the repository.

#### Scenario: Tag push triggers release
- **WHEN** a tag matching `v*` is pushed (e.g., `v1.0.0`, `v2.1.3`)
- **THEN** the release workflow SHALL execute and create a GitHub Release

#### Scenario: Non-version tag does not trigger release
- **WHEN** a tag not matching `v*` is pushed (e.g., `test-123`)
- **THEN** the release workflow SHALL NOT execute

### Requirement: Release notes extracted from changelog
The workflow SHALL extract the release body from `CHANGELOG.md` by matching the `## [x.y.z]` heading corresponding to the tag version (with `v` prefix stripped).

#### Scenario: Changelog section exists for version
- **WHEN** a tag `v1.2.3` is pushed
- **AND** `CHANGELOG.md` contains a `## [1.2.3]` section
- **THEN** the release body SHALL contain the text between that heading and the next `## [` heading (or EOF)

#### Scenario: Changelog section missing for version
- **WHEN** a tag `v1.2.3` is pushed
- **AND** `CHANGELOG.md` does not contain a `## [1.2.3]` section
- **THEN** the release body SHALL fall back to "See CHANGELOG.md"

### Requirement: Release metadata
The GitHub Release SHALL use the tag name as both the release name and tag reference, SHALL NOT be marked as draft or prerelease, and SHALL be marked as the latest release.

#### Scenario: Release metadata is correct
- **WHEN** tag `v1.0.0` triggers the workflow
- **THEN** the release name SHALL be `v1.0.0`
- **AND** draft SHALL be `false`
- **AND** prerelease SHALL be `false`
- **AND** make_latest SHALL be `true`

### Requirement: Lightweight runner
The release workflow SHALL run on `ubuntu-latest` since no Windows-specific operations are required.

#### Scenario: Workflow runs on Ubuntu
- **WHEN** the release workflow executes
- **THEN** the runner SHALL be `ubuntu-latest`

### Requirement: No release artifacts
The release SHALL NOT attach zip archives or other build artifacts. Distribution is handled via `endstate bootstrap` (git clone).

#### Scenario: No files attached to release
- **WHEN** a GitHub Release is created
- **THEN** no binary assets SHALL be attached to the release

