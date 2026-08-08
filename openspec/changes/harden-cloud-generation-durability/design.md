## Context

Hosted Backup's upload path splits across two trust domains. Substrate mints a version row and a set of presigned URLs; the client then PUTs blobs straight to R2 over paths substrate never observes. That split is deliberate — it is why the server cannot read user data — but it means the server has no way to know a version is complete. Until now it assumed completeness at create time, which is the earliest possible moment and the wrong one.

The engine side of this lives in three packages: `storage` (the `/api/backups/*` wrapper), `upload` (the push pipeline), and `download` (the pull pipeline), with `client` underneath them handling retries, error mapping, and the version header.

Constraints:

- The contract is consumed by three repos. Any change here has to be implementable by a self-hoster reading only the contract document.
- Existing accounts and existing pushed versions must keep working. There is no migration window and no way to force an engine update.
- Substrate and the engine ship independently. Either can be ahead.

## Goals / Non-Goals

**Goals**

- A generation is durable if and only if it is complete.
- A failed push cannot evict a good generation from retention, consume quota, or become a restore target.
- The engine never tells the user a generation is protected when it is not.
- The manifest gets the same integrity treatment as the chunks it describes.
- A 2.1 engine works unchanged against a 2.0 substrate.

**Non-Goals**

- Resumable uploads. A failed push re-uploads from scratch; the commit boundary makes that safe, it does not make it cheap.
- Server-side blob verification. The server still never reads chunk contents — the commit is the client's assertion, not the server's proof.
- Wiring up `auth.Verify`. Real finding, separate change (see proposal).
- Client-side enforcement of grace or retention windows. The server stays authoritative.

## Decisions

**Decision: a separate commit call, not a state field on `POST .../versions`.**

The alternative was to keep one call and have the client PATCH a `status` field. A dedicated `POST .../commit` is a verb, is trivially idempotent, and reads unambiguously in the contract. It also gives substrate a single hook for the retention sweep, which is what the create call was wrongly doing before.

*Alternatives considered:* (a) server-side completeness check by HEADing every blob — the server would have to enumerate objects it deliberately does not track, and R2 eventual consistency makes the check unreliable; (b) a TTL on uncommitted versions with no explicit commit — turns a correctness property into a timing property.

**Decision: negotiate on the client's request-header minor, not on a body flag or a per-backup setting.**

The server needs to know, at create time, whether this version will be committed. A 2.0 client will never call commit, so a server that waits for one would strand every version that client creates. The `X-Endstate-API-Version` header already exists as a contract concept (§11); it was response-only. Making it bidirectional is the smallest addition that carries the information, and it generalises to future negotiations.

This does mean the engine must send the header on *every* request, not just version creation — set once in `client.Do` rather than per call site, so no future endpoint can forget it.

*Alternatives considered:* a `requiresCommit: true` field in the create body. Rejected: it lets a client lie in the other direction (claim it will commit and then not), and it does not generalise.

**Decision: a 404 on the commit route means "already durable", not "error".**

This is the graceful-degradation path and it is load-bearing, so it is worth being precise about the reasoning. A 404 here has two possible causes: the substrate does not implement the route (2.0), or the version genuinely does not exist. The second case cannot occur on the path where we call it — we just created the version and successfully PUT blobs to URLs that substrate minted for it. So 404 is unambiguous *at this call site*.

`storage.CommitVersion` returns `(acknowledged bool, err *envelope.Error)`: `(true, nil)` means the server confirmed a commit, `(false, nil)` means the server has no such route and the version was durable at create, and a non-nil error means the generation is NOT durable. Every other status — 401, 402, 5xx, transport — falls into the third case.

**Decision: commit failure fails the push.**

The upload succeeded but the generation is not protected. Reporting success would be the same lie the old remediation string told. The error carries the backend's code, names the backup and version, and points at the honest remediation.

*Alternatives considered:* retry the commit in a background goroutine and report success optimistically. Rejected outright — "verification-first" is a core invariant, and a backup tool that optimistically reports protection is worse than one that has no cloud tier.

**Decision: manifest hash verification is conditional on the server supplying a hash.**

`GET /api/backups/:id/versions` returns `manifestSha256`. When it is a non-empty value, the engine verifies and refuses to decrypt on mismatch. When it is empty — an older backend, a version absent from the listing — the check is skipped rather than failing the restore.

The asymmetry is deliberate. Making an absent hash fatal would break restore against any backend that omits the field, including during the very outage a user is most likely to be restoring after. Making a *present* hash advisory would be pointless. Verify what you are given; do not manufacture a reason to refuse.

Consequence: `PullVersion` now calls `ListVersions` unconditionally, not just when resolving "latest", because the listing is the only source of the expected hash.

## Risks / Trade-offs

- **A 2.0 engine writing to a 2.1 substrate.** The substrate honours the advertised client minor, so those versions are durable at create — correct. The engine-side "higher backend minor blocks writes" rule fires first anyway and returns `SCHEMA_INCOMPATIBLE`. Belt and braces; the block stays as defence in depth against a substrate that ignores the header.
- **An extra round trip on every push and every pull.** One POST at the end of push, one GET at the start of pull. Against a multi-megabyte chunked upload this is noise.
- **Uncommitted versions accumulate if a client repeatedly fails.** They are invisible and uncounted, so the user sees nothing; the cost is server-side storage until the cleanup job runs. Edge rate limiting is the control, and the contract says so rather than leaving it implied.
- **`PullVersion` now hard-depends on `ListVersions` succeeding**, where before an explicit `--version-id` could bypass it. `ListVersions` is read-only, so it tolerates a newer backend minor, and a pull whose listing call fails would almost certainly have failed at `download-urls` a moment later.

## Migration Plan

No data migration. Ordering between repos does not matter:

| Order | Behaviour |
|---|---|
| Substrate first | 2.0 engines keep create-is-durable (server honours their advertised minor). Nothing changes for them. |
| Engine first | 2.1 engines get 404 on commit, treat versions as durable at create. Identical to today's behaviour. |
| Both | Full commit semantics. |

Versions created before this change are already committed by definition — substrate backfills `committed_at` for existing rows, or treats `NULL` on pre-2.1 rows as committed. That is a substrate-side detail; the contract only requires that pre-existing versions stay visible.

Rollback: revert `EngineSchemaMinor` to 0. The engine stops advertising 2.1, substrate reverts it to create-is-durable semantics, and the commit call becomes a harmless no-op. No data is stranded.

## Open Questions

- Should `backup status` surface a count of uncommitted versions? It would need a new server field and only matters diagnostically. Deferred.
- Should the engine attempt a best-effort `DELETE .../versions/:id` when a push fails after create? Under 2.1 the version is already invisible, so it buys only earlier storage reclamation. Deferred — it adds a failure path to a failure path.
