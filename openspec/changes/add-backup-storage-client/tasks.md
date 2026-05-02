## 1. Manifest package

- [ ] 1.1 Create `internal/backup/manifest/` with the encrypted-manifest envelope struct (`envelopeVersion`, `versionId`, `chunks`, `wrappedDEK`, `kdf`, `originalSize`, `chunkSize`, `chunkCount`, `createdAt`)
- [ ] 1.2 `Marshal(m)` / `Unmarshal(b)` round-trip the JSON shape from contract §3
- [ ] 1.3 `SelectLatestVersion(versions)` returns the newest-by-`createdAt` (tie-break by `versionId` lex order)

## 2. Upload package

- [ ] 2.1 Create `internal/backup/upload/` with `UploadVersion(ctx, in, dek, urls)` — splits plaintext into 4 MiB chunks, calls `crypto.EncryptChunk` per chunk, PUTs to presigned URL by `chunkIndex`
- [ ] 2.2 Manifest URL handled separately: `crypto.EncryptManifest`, then PUT to the URL with `chunkIndex == -1`
- [ ] 2.3 Bounded concurrency via `backup.Concurrency()` (default 4, env override)
- [ ] 2.4 Per-chunk progress emitted as item events (`driver: "hosted-backup"`, `id: <versionId>/chunks/<n>`, status `uploading` → `uploaded`)
- [ ] 2.5 Returns crypto.ErrNotImplemented from any encrypt call → orchestration tested with a fake-crypto test double

## 3. Download package

- [ ] 3.1 Create `internal/backup/download/` with `DownloadVersion(ctx, dek, urls, manifest, out)` — fetches manifest URL (chunkIndex=-1), parses, then fetches each chunk
- [ ] 3.2 Per-chunk SHA-256 verified against the manifest before decrypt; mismatch → integrity error
- [ ] 3.3 Bounded concurrency via `backup.Concurrency()`
- [ ] 3.4 Per-chunk progress emitted as item events (`status: downloading` → `verified` → `decrypted`)

## 4. Storage API client calls

- [ ] 4.1 `internal/backup/storage/` (or extend `client/`) with typed wrappers for `POST /api/backups`, `GET /api/backups`, `GET /api/backups/:id/versions`, `POST /api/backups/:id/versions`, `DELETE /api/backups/:id`, `DELETE /api/backups/:id/versions/:vid`, `POST /api/backups/:id/versions/:vid/download-urls`
- [ ] 4.2 All write endpoints use `ReadOnly: false`; reads use `ReadOnly: true` so version-mismatch tolerance kicks in
- [ ] 4.3 Manifest URL is identified in `uploadUrls`/`urls` arrays by `chunkIndex == -1`; this is a wire-protocol flag distinct from the AAD sentinel `0xFFFFFFFF`

## 5. Command handlers

- [ ] 5.1 `runBackupList` — `GET /api/backups`; envelope per plan §"Envelope shapes"
- [ ] 5.2 `runBackupVersions` — `GET /api/backups/:id/versions`
- [ ] 5.3 `runBackupPush` — load profile, encrypt, request URLs, upload, finalize
- [ ] 5.4 `runBackupPull` — request URLs, download, verify, decrypt, write profile
- [ ] 5.5 `runBackupDelete` — requires `--confirm`; calls `DELETE /api/backups/:id`
- [ ] 5.6 `runBackupDeleteVersion` — requires `--confirm`; calls `DELETE /api/backups/:id/versions/:vid`
- [ ] 5.7 `runBackupRecover` — reads recovery key + new passphrase from stdin; orchestrates the `/api/auth/recover` + `/api/auth/recover/finalize` flow (crypto stub blocks)
- [ ] 5.8 `runAccountDelete` — requires `--confirm`; calls `DELETE /api/account`; clears local session

## 6. Status extension

- [ ] 6.1 `runBackupStatus` populates `lastBackupAt` from the most recent backup version's `createdAt` when signed in (single extra call: `GET /api/backups`)

## 7. Tests

- [ ] 7.1 `manifest_test.go` covers round-trip Marshal/Unmarshal, version selection
- [ ] 7.2 `upload_test.go` exercises the orchestration with a fake-crypto double + httptest presigned URL receiver; covers manifest URL handling (`chunkIndex=-1`) and SHA-256 surfacing in metadata
- [ ] 7.3 `download_test.go` exercises manifest fetch + per-chunk fetch + SHA-256 mismatch rejection
- [ ] 7.4 Each command handler test asserts envelope shape (success and error paths) using httptest backend
- [ ] 7.5 `--confirm`-required commands fail without the flag with a clear message

## 8. Documentation

- [ ] 8.1 README "Hosted Backup" section enumerates the full command surface
- [ ] 8.2 Changelog entry: PR 2 of three
