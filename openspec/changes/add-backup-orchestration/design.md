## Context

This change is the integration layer that turns the auth + storage + crypto scaffolds shipped in PRs #19/#22/#23 into a working end-to-end Hosted Backup pipeline. Production substrate is operational; the engine is the last unfinished piece.

The contract (`docs/contracts/hosted-backup-contract.md`) specifies almost everything. This design doc records the four ambiguities that the contract did not pin down and how we resolve them.

## Resolved questions

### Q1 — Recovery key client/server division (§6)

The contract says the server stores `Argon2id(recoveryKey, salt)` as a verifier. The engine ships `crypto.RecoveryKeyVerifier(recoveryKey, salt, params) → []byte` that produces this exact value.

**Decision:** the client computes the verifier both at signup (sent in the signup body as `recoveryKeyVerifier`) and at recovery (sent as `recoveryKeyProof`). The server stores at signup and constant-time-compares at recovery. The two values are computed identically.

The `recoveryKey` itself is the Argon2id-derived 32-byte key (not the raw mnemonic bytes). The verifier is one further Argon2id pass over `recoveryKey`, so a server breach reveals only the verifier — inverting it to the wrap key requires breaking Argon2id.

### Q2 — Profile container format

The contract is silent. We need a container that:

- Streams (no full materialisation in memory).
- Roundtrips byte-for-byte (smoke test depends on this).
- Adds zero new dependencies.

**Decision:** uncompressed POSIX tar via stdlib `archive/tar`.

Rejected alternatives: zip (random access we don't need, harder to stream); gzip-tar (non-deterministic timestamps in headers break byte-equal verification); a substrate-defined format (the contract gives us no reason to invent one).

### Q3 — Session storage of unwrapped DEK

The DEK is needed by every `backup push` and `backup pull` after login. Three options were considered:

1. In-memory only — DEK lives only for the duration of one CLI command. Re-prompts the user for the passphrase each operation.
2. Encrypted at rest with a session passphrase — requires a second secret the user has to manage; convoluted.
3. Encrypted at rest in the OS keychain alongside the refresh token — gated on the same OS user account that already gates the refresh token.

**Decision:** option 3.

The trust model is unchanged from the refresh-token storage pattern: an attacker with full OS user access defeats the encryption; everything below that is protected by the same boundary. Storing the DEK in the keychain is no weaker than storing the refresh token there. On logout and on account-delete, both entries are cleared by `SessionStore.Forget()`.

The DEK never appears on stdout, in logs, in error messages, or in any flag value. Buffers are zeroed on the way out via the existing `crypto.zeroBytes` pattern.

### Q4 — Single-version GET endpoint

Substrate exposes `GET /api/backups/:backupId/versions` (list) but no `GET /api/backups/:backupId/versions/:versionId` (single).

**Decision:** for this PR, use the list endpoint and filter by `versionId` client-side. The list response already carries the metadata (`size`, `manifestSha256`, `createdAt`) the engine needs. A follow-up substrate change can add a single-version GET if real usage shows the cost of repeated list calls is meaningful.

## Goals / Non-Goals

**Goals:**
- Complete the engine side of every Hosted Backup user journey: signup, login, push, pull, recover, logout, account delete.
- Stream phase + per-chunk + summary events on stderr matching `event-contract.md`.
- Full test coverage of happy paths and the load-bearing failure modes (wrong passphrase, chunk SHA mismatch, retry-on-5xx, refusal to overwrite).
- A byte-equal smoke test against production substrate before merge.

**Non-Goals:**
- New contract decisions. Everything is in the contract or one of the four resolutions above.
- GUI changes — those land in the next prompt against `endstate-gui`.
- Substrate-side changes — substrate is operational; any new endpoint (e.g., single-version GET) is a follow-up.
- Resume-on-failure for partially uploaded versions — for v1, a failed push leaves the half-uploaded version row for substrate's GC, and the user re-runs `backup push` for a fresh `versionId`.
- Cross-version chunk deduplication — explicitly rejected by §8 ("whole-snapshot versioning").

## Risks and trade-offs

- **Streaming the tar plaintext through chunks vs. materialising it.** A 200 MiB profile is tolerable to hold in memory (the contract's 1 GiB quota is the cap; typical profiles are much smaller). For v1 we accept the simpler "materialise then chunk" approach; a streaming chunker can replace it without changing the public API if memory pressure becomes real.
- **Concurrency on chunk PUT/GET.** Default `ENDSTATE_BACKUP_CONCURRENCY=4` (clamped 1..16). Higher concurrency speeds large pushes but risks the `RATE_LIMITED` envelope from substrate or the R2 edge. 4 is conservative.
- **Recovery file write before network call.** The signup flow writes the recovery mnemonic to disk *before* POSTing to substrate. The reverse order would mean a successful signup with the recovery file un-written if the disk write fails afterward — and the user would have no way to recover their account. The current order means a failed signup might leave a recovery file pointing at a non-existent account, but that's harmless: the file is deleted by the user, no account exists.
- **Single-version GET workaround.** Filtering the list response is O(n) where n is the number of versions per backup (capped at 5 by §8 retention policy). Performance cost is negligible.
