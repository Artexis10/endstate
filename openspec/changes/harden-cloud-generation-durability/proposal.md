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
- **Changed**: `docs/contracts/hosted-backup-contract.md` bumps to 2.1 — commit endpoint in §7, revised durability and retention model in §8, normative 30-day grace/retention windows and purge timeline in §10, minor-compatibility matrix in §11.
- **New**: `docs/testing/cloud-recovery-drill.md` — a deterministic release-gate procedure over the existing Windows Sandbox harness: signup → capture → push → verify committed → wipe → recover → pull → byte-compare.

Not breaking. A 2.1 engine against a 2.0 substrate gets 404 on the commit route and treats the version as durable at create time, which is exactly 2.0 behaviour. That degradation path is required and tested.

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
- Cross-repo: substrate implements the commit endpoint, the per-client negotiation on the request header's minor, retention-at-commit, and the 14→30 day grace-window correction.

## Out of scope

`auth.Verify` (`go-engine/internal/backup/auth/jwt.go:50`) is a complete, tested JWT verifier with **zero production call sites** — `grep -rn "auth\.Verify"` matches only `jwt_test.go`. The engine trusts backend-issued tokens without ever validating their signature, issuer, audience, or expiry locally. That is a real finding and belongs in its own change: wiring it up touches the auth flow, the JWKS cache, and clock-skew policy, none of which this change goes near.
