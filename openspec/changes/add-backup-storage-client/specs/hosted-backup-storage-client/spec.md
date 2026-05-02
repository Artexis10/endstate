## ADDED Requirements

### Requirement: Manifest URL Identified by chunkIndex Sentinel `-1`

In the `uploadUrls` and `urls` arrays returned by substrate's storage endpoints (contract §7), the manifest blob SHALL be addressed by the sentinel `chunkIndex` value `-1`. This is a wire-protocol flag and is independent of the AAD sentinel `0xFFFFFFFF` used inside the encrypted manifest blob (contract §3).

#### Scenario: Engine PUTs the encrypted manifest to the chunkIndex=-1 URL

- **WHEN** the engine receives an `uploadUrls` array from `POST /api/backups/:id/versions`
- **THEN** the engine SHALL identify the manifest URL by its `chunkIndex == -1` entry
- **AND** SHALL PUT the encrypted manifest blob to that URL exactly once

#### Scenario: Engine includes -1 in download requests for the manifest

- **WHEN** the engine needs to fetch a version's manifest
- **THEN** the engine SHALL include `-1` in the `chunkIndices` array of the download-URL request
- **AND** SHALL retrieve the manifest URL from the response by its `chunkIndex == -1` entry

### Requirement: Chunk Hash Verified Before Decryption

Every chunk downloaded from R2 SHALL have its SHA-256 verified against the value recorded in the manifest before any decryption attempt. This guards against a subset of supply-chain and storage-tamper threats.

#### Scenario: Hash mismatch refuses to decrypt

- **WHEN** the SHA-256 of a downloaded chunk does not match the manifest value
- **THEN** the engine SHALL refuse to decrypt the chunk
- **AND** SHALL return an integrity error to the user

### Requirement: Bounded Upload/Download Concurrency

The engine SHALL bound concurrent chunk uploads and downloads via a worker pool. The pool size defaults to 4 and is configurable via `ENDSTATE_BACKUP_CONCURRENCY` (clamped to `[1, 16]`).

#### Scenario: Default concurrency is 4

- **WHEN** `ENDSTATE_BACKUP_CONCURRENCY` is unset
- **THEN** the engine SHALL run no more than 4 chunk transfers in parallel

#### Scenario: Out-of-range values clamped

- **WHEN** `ENDSTATE_BACKUP_CONCURRENCY` is set to `0`, `-1`, or any value above `16`
- **THEN** the engine SHALL silently clamp to the valid range
- **AND** SHALL NOT abort the operation

### Requirement: Destructive Operations Require Confirmation Flag

`endstate backup delete`, `endstate backup delete-version`, and `endstate account delete` SHALL refuse to run without `--confirm`. The flag exists to prevent destructive accidents from typoed scripts; the GUI translates a confirmation dialog click into the flag.

#### Scenario: backup delete without --confirm errors

- **WHEN** `endstate backup delete --backup-id <id>` is invoked without `--confirm`
- **THEN** the engine SHALL return an error code clearly naming the missing flag
- **AND** SHALL NOT make the DELETE request

### Requirement: Push and Pull Emit Item Events

`endstate backup push` and `endstate backup pull` SHALL emit per-chunk item events when `--events jsonl` is set so the GUI can render progress.

#### Scenario: push emits per-chunk uploading → uploaded

- **WHEN** `endstate backup push --events jsonl` runs
- **THEN** the engine SHALL emit one item event with `status: "uploading"` and one with `status: "uploaded"` per chunk
- **AND** all events SHALL carry `driver: "hosted-backup"`

#### Scenario: pull emits downloading → verified → decrypted

- **WHEN** `endstate backup pull --events jsonl` runs
- **THEN** the engine SHALL emit one item event per chunk transitioning through `downloading`, `verified`, and `decrypted` statuses
- **AND** the summary event SHALL report the totals

### Requirement: backup list Mirrors Substrate Field Names

The `endstate backup list` JSON envelope's `data.backups[]` shape SHALL mirror substrate's `GET /api/backups` response field-for-field — `id`, `name`, `latestVersionId`, `versionCount`, `totalSize`, `updatedAt`. No client-side renaming.

#### Scenario: list response shape

- **WHEN** the engine receives `{ "backups": [{ "id": "x", "name": "default", ... }] }` from substrate
- **THEN** the `data` field of the engine envelope SHALL pass these values through unchanged
- **AND** SHALL NOT add wrappers, rename keys, or reorder
