## ADDED Requirements

### Requirement: Generation Is Durable Only Once Committed

A backup version SHALL become durable — listed, quota-counted, and eligible as a restore target — only after the engine calls `POST /api/backups/:backupId/versions/:versionId/commit` (contract §7). Creating the version and minting its presigned URLs SHALL NOT be treated as the durability point.

The engine SHALL issue the commit only after every encrypted chunk AND the encrypted manifest have been PUT successfully. If any blob upload fails, the engine SHALL NOT issue a commit.

#### Scenario: Commit is sent after a fully successful upload

- **WHEN** `endstate backup push` uploads every chunk and the manifest without error
- **THEN** the engine SHALL send exactly one commit request for that versionId
- **AND** the commit SHALL be sent after the last blob PUT completes
- **AND** the push SHALL report success

#### Scenario: Transport failure mid-upload sends no commit

- **WHEN** a chunk PUT fails at the transport layer during `endstate backup push`
- **THEN** the engine SHALL NOT send a commit request
- **AND** the push SHALL fail
- **AND** the engine SHALL NOT return a result payload describing the generation as pushed

#### Scenario: Manifest uploaded but a chunk fails sends no commit

- **WHEN** the encrypted manifest is stored successfully but a chunk PUT exhausts its retry budget
- **THEN** the engine SHALL NOT send a commit request
- **AND** the push SHALL fail

#### Scenario: Repeated commit is tolerated

- **WHEN** the engine commits a version that is already committed
- **THEN** the backend SHALL return success
- **AND** the engine SHALL treat the version as durable

### Requirement: Commit Failure Is Never Reported As Protected

If the commit request fails for any reason other than the endpoint being absent, the engine SHALL fail the push with an actionable error naming the backup and version. The engine SHALL NOT report the generation as protected.

#### Scenario: Backend error on commit fails the push

- **WHEN** the commit request returns a non-2xx status other than 404
- **THEN** the engine SHALL return an error stating the generation is NOT protected
- **AND** the error SHALL carry the backend's error code
- **AND** the engine SHALL NOT return a push result payload

#### Scenario: Remediation describes what actually happens

- **WHEN** a push fails before or during commit
- **THEN** the remediation SHALL state that the generation was never committed and is not a restore target
- **AND** the remediation SHALL NOT claim that a half-uploaded version is garbage-collected by the backend

### Requirement: Commit Honours the Discovery-Advertised Backup API Base

The commit request SHALL be routed through `endstate_extensions.backup_api_base` from the OIDC discovery document (contract §9), exactly as every other `/api/backups/*` call is. The engine SHALL NOT construct the commit URL from the issuer when discovery advertises a relocated base.

#### Scenario: Self-hosted backup API base is used for commit

- **GIVEN** discovery advertises `backup_api_base` as `https://files.example.com/v1/backups`
- **WHEN** the engine commits a version
- **THEN** the request SHALL target `https://files.example.com/v1/backups/:backupId/versions/:versionId/commit`

### Requirement: Manifest Hash Verified Before Decryption

The encrypted manifest blob SHALL be verified against the `manifestSha256` value the API returns for the version (contract §7) before any decryption is attempted, mirroring the existing per-chunk integrity gate. On mismatch the engine SHALL refuse to decrypt and SHALL write nothing to disk.

When the backend supplies no `manifestSha256` for the version, the engine SHALL skip the check and proceed, so that restore is never blocked by an absent value.

#### Scenario: Manifest hash mismatch refuses to decrypt

- **WHEN** the SHA-256 of the downloaded manifest blob does not match the value the API advertises for that version
- **THEN** the engine SHALL return an integrity error naming the manifest
- **AND** SHALL NOT attempt to decrypt the manifest
- **AND** SHALL NOT create or populate the restore target directory

#### Scenario: Matching manifest hash restores normally

- **WHEN** the SHA-256 of the downloaded manifest blob matches the advertised value
- **THEN** the engine SHALL decrypt the manifest and complete the pull
- **AND** the restored bytes SHALL match the pushed bytes

#### Scenario: Absent manifest hash does not block restore

- **WHEN** the API returns an empty `manifestSha256` for the version
- **THEN** the engine SHALL skip manifest hash verification
- **AND** SHALL complete the pull

### Requirement: Version Listing Is Fetched For Every Pull

The engine SHALL fetch the version listing on every pull, including when an explicit `--version-id` is supplied, because the listing is the sole source of the `manifestSha256` the manifest is verified against.

#### Scenario: Explicit version id still fetches the listing

- **WHEN** `endstate backup pull --backup-id <id> --version-id <vid>` runs
- **THEN** the engine SHALL request the version listing for that backup
- **AND** SHALL use the listed `manifestSha256` for the requested version as the expected manifest hash
