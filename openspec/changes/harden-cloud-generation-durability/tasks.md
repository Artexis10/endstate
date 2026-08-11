## 1. Contract

- [x] 1.1 Bump `docs/contracts/hosted-backup-contract.md` to Schema Version 2.1
- [x] 1.2 Document `POST /api/backups/:backupId/versions/:versionId/commit` in §7, including idempotency, the request/response shape, and the client-minor negotiation table
- [x] 1.3 Revise §8: a version is durable only once committed; uncommitted versions are excluded from listing, quota, `versionCount`/`totalSize`, and restore selection; retention prunes at commit rather than at create
- [x] 1.4 Document the manifest SHA-256 verification obligation in §8 "Client's role"
- [x] 1.5 State the 30-day grace and 30-day cancellation-retention windows normatively in §10 and spell out the purge timeline
- [x] 1.6 Update §11 with contract version 2.1 and the explicit engine/backend minor compatibility matrix
- [x] 1.7 Add the 2026-08-08 v2.1 changelog entry, noting the change is additive per §13

## 2. Engine — commit path

- [x] 2.1 Parse additive `requiresCommit` from create and call `storage.CommitVersion` only when it is true
- [x] 2.2 Treat every required-commit failure, including 404, as unprotected; absent/false preserves create-is-durable behaviour
- [x] 2.3 Call `CommitVersion` in `upload.PushVersion` only after `putParallel` reports every chunk and the manifest uploaded
- [x] 2.4 Fail the push on commit error, carrying the backend error code and naming the backup and version in `detail`
- [x] 2.5 Replace the false "garbage-collected by substrate" remediation with `uncommittedRemediation`, describing the real behaviour on both backend minors
- [x] 2.6 Update the `upload` package doc comment so the pipeline diagram ends at the commit

## 3. Engine — manifest integrity

- [x] 3.1 Fetch the version listing unconditionally in `PullVersion` so `manifestSha256` is available for an explicit `--version-id`
- [x] 3.2 Add `manifestSHAFor` and `verifyManifestSHA256` helpers
- [x] 3.3 Gate `crypto.DecryptManifest` in `PullVersion` behind the hash check
- [x] 3.4 Apply the same gate in `LatestManifest` (the `--if-changed` peek)
- [x] 3.5 Skip verification when the backend supplies no hash; never fail a restore for an absent value

## 4. Engine — version negotiation

- [x] 4.1 Bump `client.EngineSchemaMinor` from 0 to 1 and document why in the constant's comment
- [x] 4.2 Add `client.EngineSchemaVersion()` returning `MAJOR.MINOR`
- [x] 4.3 Send `X-Endstate-API-Version` on every request from `client.Do`
- [x] 4.4 Confirm `versionMismatch` still blocks writes and warns on reads for a higher backend minor, and accepts an older backend minor on both

## 5. Tests

- [x] 5.1 Add `commitLog` and `sha256Hex` to the shared fixture in `backup_test_fixture_test.go`
- [x] 5.2 Route `POST .../versions/:vid/commit` in the `pushPullBackend` httptest mock, idempotent by default, overridable via `commitFn`
- [x] 5.3 Add `r2FailAlways`, `r2DeadURL`, and `r2WaitForManifest` controls so upload failure modes are deterministic
- [x] 5.4 Test: transport failure mid-chunk-upload sends no commit and returns no result payload
- [x] 5.5 Test: manifest uploaded but a chunk PUT exhausts its retries sends no commit
- [x] 5.6 Test: a fully successful push commits exactly once, after the blobs land; a repeated commit is tolerated
- [x] 5.7 Test: a create response without `requiresCommit` skips commit; a required-commit 404 fails the push
- [x] 5.8 Test: a commit failure fails the push with an error stating the generation is not protected
- [x] 5.9 Test: pull refuses to decrypt on a manifest SHA-256 mismatch and writes nothing to disk
- [x] 5.10 Test: pull succeeds when the advertised manifest hash matches (positive control)
- [x] 5.11 Storage-package tests: create parses `requiresCommit`; commit honours `backup_api_base` and surfaces every non-success response
- [x] 5.12 Client-package tests: an older backend minor is accepted on reads and writes; every request carries the engine schema version

## 6. Release gate

- [ ] 6.1 Write an executable, independently validated Windows Sandbox release gate (the current document remains a manual procedure)
- [ ] 6.2 Execute signup → capture → push → verify committed → wipe → recover → pull → byte-compare in a disposable Sandbox
- [ ] 6.3 Capture staging env-var setup and `HOSTED_BACKUP_TEST_EMAIL_PATTERN` evidence from that run
- [ ] 6.4 Have the release reviewer validate the numbered pass/fail criteria against raw artifacts
- [ ] 6.5 Run the manual negative drills when the upload path changes

## 7. Verification

- [ ] 7.1 `cd go-engine && go vet ./...` (focused packages pass; broad native run remains blocked by host ACL/telemetry constraints)
- [ ] 7.2 `cd go-engine && go test ./...` (focused hermetic packages pass; broad native run remains blocked by host ACL/telemetry constraints)
- [x] 7.3 `npm run openspec:validate`
