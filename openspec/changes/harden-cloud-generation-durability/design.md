## Context

Hosted Backup's upload path splits across two trust domains. Substrate mints a version row and a set of presigned URLs; the client then PUTs blobs straight to R2 without the application observing those transfers. That split is deliberate — it is why the server cannot read user data — but it means an application row alone cannot prove the objects are complete. Until now it assumed completeness at create time, which is the earliest possible moment and the wrong one.

The engine side of this lives in three packages: `storage` (the `/api/backups/*` wrapper), `upload` (the push pipeline), and `download` (the pull pipeline), with `client` underneath them handling retries, error mapping, and the version header.

Constraints:

- The contract is consumed by three repos. Any change here has to be implementable by a self-hoster reading only the contract document.
- Existing accounts and existing pushed versions must keep working. There is no migration window and no way to force an engine update.
- Substrate and the engine ship independently. Either can be ahead.

## Goals / Non-Goals

**Goals**

- A generation is durable if and only if it is complete.
- A failed push cannot evict a good generation from retention or become a restore target; pending bytes remain only as bounded quota reservations until retry or reclamation.
- The engine never tells the user a generation is protected when it is not.
- The manifest gets the same integrity treatment as the chunks it describes.
- A 2.1 engine works unchanged against a 2.0 substrate.
- The backend verifies every expected object before publication, without reading plaintext.
- Access tokens are verified locally before any credential state is persisted.
- A capability-negotiated operation identity and byte-identical encrypted spool make ambiguous create responses replay-safe.

**Non-Goals**

- Resumable uploads. A failed push re-uploads from scratch; the commit boundary makes that safe, it does not make it cheap.
- Client-side enforcement of grace or retention windows. The server stays authoritative.

## Decisions

**Decision: a separate commit call, not a state field on `POST .../versions`.**

The alternative was to keep one call and have the client PATCH a `status` field. A dedicated `POST .../commit` is a verb, is trivially idempotent, and reads unambiguously in the contract. It also gives substrate a single hook for the retention sweep, which is what the create call was wrongly doing before.

*Alternatives considered:* (a) trusting the client assertion without checking object storage — rejected because a missing or wrong-sized object could still become visible; (b) a TTL on uncommitted versions with no explicit commit — turns a correctness property into a timing property. The backend records every expected object key, size, and ciphertext digest, then HEADs those exact objects with bounded concurrency before publication. It never receives plaintext or encryption keys.

**Decision: create explicitly negotiates commit with `requiresCommit`.**

The create response tells this engine whether the version is durable at create
time. An absent or `false` value preserves legacy server behaviour and does not
call commit. A `true` value means the engine must receive a successful commit
response before it can report protection. This avoids treating an ambiguous
404 from a required commit as success.

**Decision: a required commit has no graceful-error path.**

When `requiresCommit` is true, every commit failure — including 404, 401/403,
timeout, cancellation, and rejection — leaves the generation unprotected and
fails the push. Retrying is safe only with the explicit idempotent operation
identifier; the engine never reports an optimistic background success.

**Decision: replay safety is negotiated before mutation.**

The optional OIDC discovery capability
`version-create-operation-replay-v1` is the sole authority for replaying a
create-version request. A capable engine persists a stable operation ID plus
the exact encrypted manifest and chunk bytes before its first POST. A pending
replay returns the same version and fresh checksum-bound create-only URLs; a
committed replay returns the same version with no upload work. Capability
absence uses a single legacy POST, and an ambiguous result is surfaced for
manual reconciliation rather than automatically duplicated.

**Decision: verify access tokens before persistence.**

Every login, signup, claim, recovery, and refresh result passes EdDSA
signature, issuer, audience, expiry, not-before, and subject verification via
the discovered JWKS before tokens are written to the session store. One JWKS
refetch is allowed for key rotation; unverified rotated credentials are never
persisted.

**Decision: commit failure fails the push.**

The upload succeeded but the generation is not protected. Reporting success would be the same lie the old remediation string told. The error carries the backend's code, names the backup and version, and points at the honest remediation.

*Alternatives considered:* retry the commit in a background goroutine and report success optimistically. Rejected outright — "verification-first" is a core invariant, and a backup tool that optimistically reports protection is worse than one that has no cloud tier.

**Decision: manifest hash verification is conditional on the server supplying a hash.**

`GET /api/backups/:id/versions` returns `manifestSha256`. When it is a non-empty value, the engine verifies and refuses to decrypt on mismatch. When it is empty — an older backend, a version absent from the listing — the check is skipped rather than failing the restore.

The asymmetry is deliberate. Making an absent hash fatal would break restore against any backend that omits the field, including during the very outage a user is most likely to be restoring after. Making a *present* hash advisory would be pointless. Verify what you are given; do not manufacture a reason to refuse.

Consequence: `PullVersion` now calls `ListVersions` unconditionally, not just when resolving "latest", because the listing is the only source of the expected hash.

## Risks / Trade-offs

- **A 2.0 engine writing to the compatibility backend.** Every new version starts pending. The backend responds with schema 2.0, accepts the legacy upload, and publishes it only after bounded server reconciliation verifies the manifest and chunks. Post-cutoff rows remain invisible until that verification succeeds.
- **An extra round trip on every push and every pull.** One POST at the end of push, one GET at the start of pull. Against a multi-megabyte chunked upload this is noise.
- **Uncommitted versions accumulate if a client repeatedly fails.** They are invisible but count as reserved quota, so abandoned creates cannot bypass the limit. The cleanup job reclaims them under a lease that is mutually exclusive with commit and replay URL minting.
- **`PullVersion` now hard-depends on `ListVersions` succeeding**, where before an explicit `--version-id` could bypass it. `ListVersions` is read-only, so it tolerates a newer backend minor, and a pull whose listing call fails would almost certainly have failed at `download-urls` a moment later.

## Migration Plan

The rollout is additive and deliberately staged; it is not a flag day:

| Order | Behaviour |
|---|---|
| Compatibility backend first | Migration installs pending state, the legacy visibility bridge, replay ledger, and reconciliation while responses remain schema 2.0. Old engines continue to work. |
| Engine first against an old backend | Discovery omits replay capability and create omits `requiresCommit`; the engine uses the conservative legacy one-shot path. |
| Both, capability hidden | The new engine explicitly commits, while create replay remains conservative. |
| Both, capability advertised | Stable operation replay, byte-identical restart recovery, explicit commit, and terminal committed replay are active. |

Existing rows are labelled `legacy_unverified`. Release A temporarily keeps
pre-cutoff rows visible while a newest-first reconciler verifies their exact R2
objects, publishing valid rows and quarantining definitive failures. Strict
visibility is enabled only by the guarded cutover after no unresolved legacy
rows remain.

Rollback is application-first and forward-only for schema: leave additive
columns, ledgers, and policy rows in place; keep the replay discovery capability
hidden until every write replica supports it. After strict visibility is
enabled, a pre-bridge backend must not be redeployed.

## Open Questions

- Should `backup status` surface a count of uncommitted versions? It would need a new server field and only matters diagnostically. Deferred.
- Should the engine attempt a best-effort `DELETE .../versions/:id` when a push fails after create? Under 2.1 the version is already invisible, so it buys only earlier storage reclamation. Deferred — it adds a failure path to a failure path.
