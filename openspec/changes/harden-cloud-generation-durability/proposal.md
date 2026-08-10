# Change: Harden cloud generation durability

## Why

The hosted-backup upload is not atomic, and the engine tells the user otherwise.

`upload.PushVersion` calls `storage.CreateVersion`, which makes the version row durable server-side and prunes retention, and only then starts uploading blobs. There is no finalize step. A push that dies partway — dropped connection, killed process, exhausted retry budget — leaves a version that substrate considers real: it is listed by `GET /api/backups/:id/versions`, it counts against the 1 GiB quota, it has already evicted the oldest good generation from the 5-version retention window, and `manifest.SelectLatest` will pick it as the restore target because it has the newest `createdAt`. A restore then fails, or — worse — reconstructs a truncated profile.

The remediation string the engine prints on that failure (`upload.go:187`) claims "The half-uploaded version is garbage-collected by substrate." That is false. Nothing collects it.

Two adjacent gaps in the same path:

- `download.PullVersion` fetches the encrypted manifest and decrypts it without checking it against the `manifestSha256` the API returns. Every chunk gets a SHA-256 gate before decryption; the manifest — the blob that tells the engine which chunks exist and how large they are — gets none. Its only integrity protection is the AEAD tag, evaluated after the bytes are already in the decrypt path.
- The engine never sends `X-Endstate-API-Version` on requests, only reads it on responses. Per-client negotiation of the new durability semantics needs the request header.

## What Changes

- **New**: `storage.CommitVersion(ctx, backupID, versionID)` calling `POST /api/backups/:backupId/versions/:versionId/commit` (contract §7), routed through `backup_api_base` like every sibling call.
- **New**: `upload.PushVersion` commits the version as its last step, only after every chunk and the manifest have been PUT successfully. A commit failure fails the push; the generation is never reported as protected.
- **Fixed**: the false "garbage-collected by substrate" remediation is replaced with a description of what actually happens on both backend minors.
- **New**: `download.PullVersion` and `download.LatestManifest` verify the encrypted manifest against the API-supplied `manifestSha256` before decrypting, mirroring the per-chunk gate. Mismatch refuses to decrypt and writes nothing to disk.
- **New**: the engine sends `X-Endstate-API-Version` on every request so substrate can negotiate commit semantics per client.
- **Changed**: `client.EngineSchemaMinor` 0 → 1 (contract schema 2.1).
- **Changed**: `docs/contracts/hosted-backup-contract.md` negotiates commit through the additive create-response `requiresCommit` field. When it is true, a successful commit is mandatory; absent/false preserves legacy create-is-durable behaviour.
- **New**: `docs/testing/cloud-recovery-drill.md` — a deterministic release-gate procedure over the existing Windows Sandbox harness: signup → capture → push → verify committed → wipe → recover → pull → byte-compare.
- **New**: every new backend generation starts pending; commit and bounded legacy reconciliation publish only after exact manifest/chunk size and ciphertext-digest metadata checks against object storage.
- **New**: OIDC discovery can advertise `version-create-operation-replay-v1`; capable scheduled pushes persist a stable operation ID and byte-identical encrypted spool before the first create POST, while legacy ambiguity is never automatically replayed.
- **Fixed**: every access token is signature-, issuer-, audience-, time-, and subject-verified through the discovered JWKS before the engine persists credentials.

Not breaking. A legacy substrate omits `requiresCommit` (or returns false), so
the engine retains create-is-durable behaviour. A server that requires commit
sets it true; then a 404 or any other failure is correctly fatal.

## Impact

- Affected specs: `hosted-backup-storage-client`, `hosted-backup-version-compatibility`, `verification-first`
- Affected code:
  - `go-engine/internal/backup/storage/storage.go` — `CommitVersion`
  - `go-engine/internal/backup/upload/upload.go` — commit-last ordering, honest remediation
  - `go-engine/internal/backup/download/download.go` — manifest integrity gate
  - `go-engine/internal/backup/client/version.go` — minor bump, `EngineSchemaVersion()`
  - `go-engine/internal/backup/client/client.go` — request-side version header
  - `go-engine/internal/commands/backup_orchestration_test.go`, `backup_test_fixture_test.go` — durability tests over the real httptest harness
  - `docs/contracts/hosted-backup-contract.md`, `docs/testing/cloud-recovery-drill.md`
- Cross-repo: substrate implements pending-first creation, commit and legacy reconciliation, operation replay and GC fencing, retention-at-commit, the schema-2.0 response bridge, and the 14→30 day grace-window correction.

## Out of scope

Cryptographic algorithm, KDF, nonce, associated-data, wrapped-key, and recovery-phrase redesign remain out of scope. This change wires the existing verifier and preserves the existing encrypted-envelope design; it does not invent a replacement cryptosystem.
