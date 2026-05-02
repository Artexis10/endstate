# Endstate Hosted Backup Contract

**Status:** Locked
**Schema Version:** 1.0
**Last Updated:** 2026-05-02

This document is the canonical specification for Endstate Hosted Backup — the optional paid tier that allows users to upload encrypted profile backups to Endstate-operated infrastructure and restore them on any machine.

This contract is referenced by three repositories:

- `endstate` (engine, Go) — implements client-side encryption, CLI commands, and JWT validation
- `endstate-gui` (Tauri/React) — wires GUI flows to engine commands
- `substrate` (Next.js/Vercel) — implements the backend API, auth, metadata, and storage orchestration

If repository code conflicts with this contract, code wins for the immediate task and a contract update must be proposed in the same change. Silent drift is forbidden.

---

## 1. Trust Model

Endstate cannot decrypt user data uploaded to Hosted Backup. This is a structural property, not a policy. Even with full access to our servers and a court order, we have no path to your data.

The user's passphrase never leaves their device. On the client, the passphrase is processed by Argon2id with a per-user salt to produce 64 bytes of derived material. The first 32 bytes are sent to the server as `serverPassword` and stored as a normal password hash. The second 32 bytes — `masterKey` — never leave the device, and are used to wrap the per-user data-encryption-key (DEK) that encrypts file contents.

The server authenticates the user without seeing material that could decrypt their data.

If a user loses both their passphrase and their recovery key, their data is unrecoverable. Endstate cannot recover it. This is the cost of the structural guarantee.

### What this design protects against

- External attacker who breaches the database, the storage backend, or both
- Insider with full operator access at Endstate
- Subpoena, court order, or compelled assistance under any legal regime

### What this design does not protect against

- Compromise of the user's device — full local access defeats end-to-end encryption
- Weak user passphrase — Argon2id raises the cost of offline attack but does not eliminate it
- Side-channel attacks on the client during active use
- Data the user has uploaded unencrypted somewhere else

### GUI / client responsibility

Recovery key generation, presentation, and verification are mandatory parts of the signup flow — not optional steps a user can skip. Any client implementing this contract (the official GUI, a hypothetical CLI signup, or a third-party client) MUST offer at least two save formats (file and printable PDF) and require explicit confirmation that the user has saved the recovery key before signup completes.

---

## 2. KDF Parameters (locked v1)

All key derivation uses Argon2id (RFC 9106) with the following parameters:

| Parameter | Value | Rationale |
|---|---|---|
| Algorithm | Argon2id | Memory-hard, side-channel-resistant; current OWASP recommendation |
| Memory | 65536 KiB (64 MiB) | OWASP 2024 minimum for password derivation |
| Iterations | 3 | OWASP 2024 minimum |
| Parallelism | 4 | Balance between desktop-class hardware utilisation and server load |
| Output length | 64 bytes | 32 bytes serverPassword + 32 bytes masterKey |
| Salt length | 16 bytes | Per-user, generated at signup, stored on server, returned at login |

The salt is treated as non-secret. The server returns it to the client during the login pre-handshake so the client can derive `serverPassword` and `masterKey` consistently across machines.

**Parameter negotiation:** The login pre-handshake response includes a `kdf` object specifying the parameters that were used at signup. The client uses these parameters, not its own defaults. This allows future upgrades without breaking existing accounts.

**Parameter floor:** The server rejects any signup using parameters weaker than the v1 values above. The client refuses to derive keys with parameters below the v1 floor regardless of server response.

---

## 3. Encryption Envelope Format

Each encrypted backup version is structured as a manifest plus chunks. Chunks are uploaded and downloaded independently to support resume.

### Manifest (encrypted JSON)

```json
{
  "envelopeVersion": 1,
  "versionId": "<uuid>",
  "createdAt": "<ISO 8601>",
  "originalSize": 0,
  "chunkSize": 4194304,
  "chunkCount": 0,
  "chunks": [
    { "index": 0, "encryptedSize": 0, "sha256": "<hex>" }
  ],
  "kdf": {
    "algorithm": "argon2id",
    "memory": 65536,
    "iterations": 3,
    "parallelism": 4
  },
  "wrappedDEK": "<base64>"
}
```

The manifest itself is encrypted with the DEK before upload, using the same AES-256-GCM scheme as chunks. The server stores the encrypted manifest blob; chunk metadata (index, encryptedSize, sha256) is also tracked in the database for integrity checks but the manifest is the source of truth.

### Chunk format (AES-256-GCM, RFC 5116)

| Field | Size | Contents |
|---|---|---|
| `nonce` | 12 bytes | Random per chunk, generated client-side via CSPRNG |
| `ciphertext` | variable | Encrypted plaintext |
| `tag` | 16 bytes | GCM authentication tag |

Plaintext chunk size is fixed at 4 MiB except for the final chunk. Each chunk is encrypted independently with a freshly generated random nonce. The chunk index (4-byte big-endian unsigned integer) is included as Additional Authenticated Data (AAD) to bind chunks to their position and prevent reordering attacks.

### DEK wrapping

The DEK is a 32-byte random value generated client-side at signup using a CSPRNG. It is wrapped with AES-256-GCM using `masterKey` as the wrapping key. The wrapped DEK is stored in the manifest. Only the client can unwrap it.

### Algorithm choice rationale

- AES-256-GCM is hardware-accelerated on all modern CPUs (AES-NI), well-audited (NIST SP 800-38D), and the dominant choice in comparable products (Bitwarden, Filen, AWS S3 SSE-C)
- 4 MiB chunk size balances upload resume granularity against per-chunk overhead. Standard Notes uses 1 MiB; Filen uses ~5 MiB. 4 MiB is a defensible middle.
- GCM authentication tags prevent tampering; AAD binding to chunk index prevents reordering

---

## 4. JWT Format

Authentication tokens are JWTs signed with EdDSA (Ed25519) per RFC 8032 and RFC 8037.

**Why EdDSA:** Smaller signatures than RSA, no parameter choice ambiguity, and the substrate codebase already has the `@noble/ed25519` library wired and tested for license signing. Reusing this keypair pattern reduces new attack surface.

### Header

```json
{ "alg": "EdDSA", "typ": "JWT", "kid": "<key id>" }
```

### Claims

| Claim | Type | Description |
|---|---|---|
| `iss` | string | Issuer URL — `https://substratesystems.io` for Endstate Cloud, the self-host URL otherwise |
| `sub` | string | User ID (UUID) |
| `aud` | string | `endstate-backup` |
| `iat` | int | Issued-at, Unix epoch seconds |
| `exp` | int | Expiry, Unix epoch seconds — `iat + 900` (15 min) |
| `nbf` | int | Not-before, equal to `iat` |
| `jti` | string | JWT ID (UUID) for revocation lookup |
| `subscription_status` | string | One of `none`, `active`, `grace`, `cancelled` — UI hint only, server is authoritative for write authorisation |

### JWKS endpoint

`GET /api/.well-known/jwks.json` returns the public key set in standard JWKS format. The current signing key is identified by `kid`. Multiple keys may be present during rotation.

### JWT lifecycle

Access tokens expire after 15 minutes. Clients use the refresh token to obtain a new access token. JWT is never used for encryption-key derivation — encryption keys are derived solely from the user's passphrase.

---

## 5. Auth Flow

Five endpoints. All endpoints accept and return JSON. Errors use the standard envelope from `cli-json-contract.md`.

All auth endpoints rate-limited at the substrate edge. Rate limits are documented at implementation time, not in this contract.

### POST /api/auth/signup

**Request:**
```json
{
  "email": "user@example.com",
  "serverPassword": "<base64, 32 bytes>",
  "salt": "<base64, 16 bytes>",
  "kdfParams": { "algorithm": "argon2id", "memory": 65536, "iterations": 3, "parallelism": 4 },
  "wrappedDEK": "<base64>",
  "recoveryKeyVerifier": "<base64>",
  "recoveryKeyWrappedDEK": "<base64>"
}
```

**Response:**
```json
{ "userId": "<uuid>", "accessToken": "<jwt>", "refreshToken": "<opaque>" }
```

The server stores `Argon2id(serverPassword, server_salt)`, the user's `salt`, the `kdfParams`, the `wrappedDEK`, and the recovery key materials. The server never sees the user's passphrase or `masterKey`.

### POST /api/auth/login (step 1: pre-handshake)

**Request:** `{ "email": "user@example.com" }`

**Response:** `{ "salt": "<base64>", "kdfParams": {...} }`

Lets the client derive the same `serverPassword` and `masterKey` it derived at signup. This step leaks "this email exists" to anyone who hits the endpoint — acceptable trade-off, matches the disclosure made by every comparable service. Mitigated by edge rate-limiting.

### POST /api/auth/login (step 2: complete)

**Request:** `{ "email": "user@example.com", "serverPassword": "<base64>" }`

**Response:** `{ "userId": "<uuid>", "accessToken": "<jwt>", "refreshToken": "<opaque>", "wrappedDEK": "<base64>" }`

The server verifies `serverPassword` against the stored hash and returns the wrapped DEK so the client can unwrap it with `masterKey`.

### POST /api/auth/refresh

**Request:** `{ "refreshToken": "<opaque>" }`

**Response:** `{ "accessToken": "<jwt>", "refreshToken": "<opaque>" }`

Sliding window: each refresh issues a new refresh token; the old one is invalidated. Maximum lifetime of a single refresh chain is 30 days.

### POST /api/auth/logout

**Request:** `{ "refreshToken": "<opaque>" }`

**Response:** `{ "ok": true }`

Invalidates the refresh token. Access tokens expire on their own; the server does not maintain an access-token blocklist.

### POST /api/auth/recover

See Section 6.

---

## 6. Recovery Key

Generated client-side at signup. Presented to the user once for them to record. Endstate never stores the recovery key in plaintext.

**The recovery key is a second independent unlock path, not a second factor.** Normal sign-in on any machine requires only email and passphrase — the recovery key is not used. The recovery key is a safety net for the case where the user forgets their passphrase.

### Generation

32 bytes from a CSPRNG, encoded as a 24-word BIP39 mnemonic for human readability and transcription error-detection.

### Storage

The recovery key is processed client-side by Argon2id (same parameters as the passphrase KDF) to produce a 32-byte `recoveryKey`. A second wrapping of the DEK with `recoveryKey` — `recoveryKeyWrappedDEK` — is stored on the server alongside `wrappedDEK`. The server stores `Argon2id(recoveryKey, salt)` as a verifier (`recoveryKeyVerifier`).

### Normal sign-in flow

See Section 5. Recovery key is not involved.

### Recovery flow (passphrase forgotten, recovery key in hand)

1. User initiates recovery, enters their recovery key (typed mnemonic or pasted from saved file)
2. Client derives `recoveryKey` via Argon2id
3. Client proves possession to server via `POST /api/auth/recover` with `{ email, recoveryKeyProof }`
4. Server returns `recoveryKeyWrappedDEK`
5. Client unwraps DEK with `recoveryKey`
6. User is prompted to set a new passphrase
7. Client derives new `serverPassword` and `masterKey`, re-wraps the DEK as new `wrappedDEK`, uploads it via `POST /api/auth/recover/finalize`
8. Server updates the password hash and the wrappedDEK in a single transaction

### What the recovery key does not do

It does not allow the server, an attacker who breaches the server, or anyone other than the holder of the recovery key to decrypt the DEK. Both `wrappedDEK` and `recoveryKeyWrappedDEK` are useless without the corresponding passphrase or recovery key.

### What happens if both are lost

Data is unrecoverable. The user's account remains, but blobs are inaccessible. The user can re-subscribe and back up new data; previous data cannot be recovered.

---

## 7. API Surface

All endpoints accept and return JSON. Errors use the standard envelope from `cli-json-contract.md`:

```json
{
  "success": false,
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable message",
    "detail": {},
    "remediation": "What to do",
    "docsKey": "errors/error-code"
  }
}
```

All write endpoints require `Authorization: Bearer <accessToken>`. Auth endpoints (signup/login/refresh/logout/recover) require no token except `refresh` and `logout`.

All endpoints rate-limited at the substrate edge. Rate limits documented per-endpoint at implementation time.

### Auth endpoints (Section 5)

- `POST /api/auth/signup`
- `POST /api/auth/login` (two-step)
- `POST /api/auth/refresh`
- `POST /api/auth/logout`
- `POST /api/auth/recover` and `POST /api/auth/recover/finalize` (Section 6)

### Account endpoints

- `GET /api/account/me` → `{ userId, email, subscriptionStatus, createdAt }`
- `DELETE /api/account` → triggers GDPR deletion (Section 12)

### Backup metadata endpoints

- `GET /api/backups` → list user's backups: `{ backups: [{ id, name, latestVersionId, versionCount, totalSize, updatedAt }] }`
- `POST /api/backups` → create a new backup: `{ name }` → `{ backupId }`
- `DELETE /api/backups/:backupId` → permanently delete a backup and all its versions
- `GET /api/backups/:backupId/versions` → list versions: `{ versions: [{ versionId, createdAt, size, manifestSha256 }] }`
- `POST /api/backups/:backupId/versions` → create a new version: `{ encryptedManifest, chunkMetadata: [{ index, encryptedSize, sha256 }] }` → `{ versionId, uploadUrls: [{ chunkIndex, presignedUrl, expiresAt }] }`
- `DELETE /api/backups/:backupId/versions/:versionId` → soft-delete a version (purged after 7 days)

### Blob storage endpoints

- `POST /api/backups/:backupId/versions/:versionId/download-urls` → request presigned download URLs for a set of chunk indices: `{ chunkIndices: [int] }` → `{ urls: [{ chunkIndex, presignedUrl, expiresAt }] }`

### OIDC discovery

- `GET /api/.well-known/openid-configuration` (Section 9)
- `GET /api/.well-known/jwks.json` (Section 4)

### Subscription state

- `POST /api/webhooks/paddle` → Paddle webhook receiver, raw-body HMAC verification (Section 10). Not user-facing.

### Ownership enforcement

All `/api/backups/*` endpoints are scoped to the authenticated user. The server enforces ownership on every request — `userId` from the JWT must match the `userId` on the backup row. Cross-user access returns 404, not 403, to avoid leaking the existence of other users' backups.

---

## 8. Storage Layout

Cloudflare R2, EU jurisdiction.

### Bucket structure

```
users/<userId>/
  backups/<backupId>/
    versions/<versionId>/
      manifest                    # encrypted JSON (Section 3)
      chunks/<chunkIndex>         # encrypted chunk (Section 3)
```

All paths are opaque to the server. Filenames are UUIDs and integer chunk indices; no plaintext profile names appear in object keys.

### Server's role

Mints presigned URLs (PUT for upload, GET for download) scoped to a single object key with a short TTL (5 minutes). Records metadata in Postgres: `backupId`, `versionId`, `chunkIndex`, `objectKey`, `size`, `sha256`, `createdAt`. Server never reads chunk contents.

### Client's role

Uploads/downloads chunks directly to R2 via presigned URLs. Verifies SHA-256 of each chunk on download against the manifest before decrypting. Refuses to decrypt any chunk whose hash does not match.

### Versioning model (v1)

**Whole-snapshot versioning.** Each `POST /api/backups/:backupId/versions` creates a complete new copy of the backup. No chunk-level deduplication across versions. Storage cost grows linearly with version count. This is a deliberate v1 simplification; content-addressed deduplication is a possible v2 optimisation if real usage demands it.

### Versioning policy

- **Last 5 versions per backup retained.** Configurable per backup via metadata (future).
- Older versions are garbage-collected by a scheduled substrate cron job.
- Garbage collection is "soft" for 7 days — version row marked `deleted_at`, blobs purged from R2 after the 7-day window — to allow for accidental-deletion recovery.
- After purge, blobs are unrecoverable.

### Storage quota (v1)

**1 GiB per active subscriber.** Enforced server-side at version creation. Quota check uses the sum of `size` across non-deleted versions. Quota exceeded → version creation fails with `STORAGE_QUOTA_EXCEEDED`. Calibrated against realistic profile sizes (apps + configs typically <200 MB); intended as a backstop against pathological cases, not a feature limit. May be raised post-launch based on real usage data.

### Why client uses presigned URLs (not direct R2 credentials)

Direct R2 credentials in the client would mean every user can list every other user's bucket prefix. Presigned URLs scoped per-object are the standard pattern (used by AWS S3 Transfer Acceleration, Backblaze B2, Cloudflare R2 documentation examples).

---

## 9. OIDC Discovery and Self-Host Contract

The substrate backend exposes standard OIDC discovery endpoints. Self-hosters running their own substrate-equivalent backend (or any OIDC-compliant issuer with appropriate endpoints) are supported without engine code changes.

### Engine configuration (two environment variables)

| Variable | Default (Endstate Cloud) | Self-host example |
|---|---|---|
| `ENDSTATE_OIDC_ISSUER_URL` | `https://substratesystems.io` | `https://my-endstate.example.com` |
| `ENDSTATE_OIDC_AUDIENCE` | `endstate-backup` | `endstate-backup` (or any value matching the self-hoster's issuer) |

The engine fetches `${ENDSTATE_OIDC_ISSUER_URL}/.well-known/openid-configuration` on startup, caches it for 1 hour, and uses the discovered endpoints for auth and JWKS validation.

### Required OIDC discovery fields

```json
{
  "issuer": "https://substratesystems.io",
  "jwks_uri": "https://substratesystems.io/api/.well-known/jwks.json",
  "id_token_signing_alg_values_supported": ["EdDSA"],
  "endstate_extensions": {
    "auth_signup_endpoint": "https://substratesystems.io/api/auth/signup",
    "auth_login_endpoint": "https://substratesystems.io/api/auth/login",
    "auth_refresh_endpoint": "https://substratesystems.io/api/auth/refresh",
    "auth_logout_endpoint": "https://substratesystems.io/api/auth/logout",
    "auth_recover_endpoint": "https://substratesystems.io/api/auth/recover",
    "backup_api_base": "https://substratesystems.io/api/backups",
    "supported_kdf_algorithms": ["argon2id"],
    "supported_envelope_versions": [1],
    "min_kdf_params": { "memory": 65536, "iterations": 3, "parallelism": 4 }
  }
}
```

The `endstate_extensions` block is non-standard but namespaced. Anyone implementing a self-host backend implements these extension fields. The engine refuses to talk to a backend that does not advertise them or advertises incompatible KDF / envelope minimums.

### Storage backend

Self-hosters can use any S3-compatible object store (R2, S3, MinIO, Backblaze B2, Wasabi). The substrate backend's storage interface is documented as S3-compatible and the storage backend is configured server-side, not client-side. The engine never sees storage credentials.

### Self-host scope (v1)

- Self-hosters can run their own substrate-equivalent backend (any implementation of this contract) pointing at their own object store
- Self-hosters can swap in any OIDC-compliant issuer for auth, as long as the issuer advertises the required `endstate_extensions`
- Self-hosters configure the engine via two environment variables
- This contract document is published publicly (the protocol is open)
- Substrate's specific implementation source is not required to be public — the protocol is the spec, not the implementation
- A polished `docker-compose` self-host bundle is a v1.x deliverable, not v2.0

---

## 10. Subscription State Machine

Subscription state is authoritative on the substrate backend. The JWT carries `subscription_status` as a hint claim, refreshed each token mint (max staleness 15 minutes). Server checks the database row, not the JWT, for any write authorisation.

### States

| State | Meaning | Backup write | Backup read / restore |
|---|---|---|---|
| `none` | Never subscribed, or fully cancelled past retention | Blocked | Blocked (no data to read) |
| `active` | Subscription paid, current | Allowed | Allowed |
| `grace` | Payment failed, in 30-day grace window | Blocked | Allowed |
| `cancelled` | User cancelled, in 30-day retention window | Blocked | Allowed |

### Transitions (Paddle-driven)

| Paddle event | Transition | Notes |
|---|---|---|
| `subscription.created` (first-time) | `none → active` | |
| `subscription.activated` (after grace recovery) | `grace → active` | Card succeeded after past_due |
| `subscription.past_due` (payment failed) | `active → grace`, set `grace_started_at` | |
| `subscription.canceled` (user-initiated) | `active → cancelled`, set `cancel_started_at` | Note Paddle spelling: "canceled" |
| `subscription.canceled` (failed payment, grace expired) | `grace → cancelled` | |
| Internal: 30 days in `cancelled` | `cancelled → none`, schedule blob purge | |

### Restore-during-grace rationale

A subscription lapse is the worst time to lock users out of their own data. Card declines, expired cards, billing email going to spam — all common. Allowing read/restore during grace is the kindest UX and the one users most need at exactly the moment their card needs attention.

### Purge timeline

Blobs are purged 30 days after entering `cancelled`. The user's account remains. They can re-subscribe at any time, but data from before purge is gone. This is documented in Terms.

### Webhook reliability

- Paddle retries webhooks on any non-2xx response
- The webhook handler is idempotent on `event_id` (Paddle's deduplication key)
- Out-of-order delivery is handled — `subscription.activated` arriving before `subscription.created` is rare but possible; the handler reconciles on `subscription_id`, not on event order

### Webhook signature verification

HMAC-SHA256 over the raw request body, using the `Paddle-Signature` header. Substrate already implements this correctly for the license webhook (`src/lib/license/paddle.ts`). The hosted-backup webhook reuses that verification utility.

---

## 11. Version Compatibility Matrix

Three independent version axes, with explicit compatibility checks at every boundary.

| Axis | Owner | Format | Source of truth |
|---|---|---|---|
| `apiSchemaVersion` | Substrate | `MAJOR.MINOR` | This contract |
| `engineVersion` | Engine | `MAJOR.MINOR.PATCH` (semver) | `engine/VERSION.txt` |
| `guiVersion` | GUI | `MAJOR.MINOR.PATCH` (semver) | `endstate-gui/package.json` |

**Contract version:** Currently `1.0`. Changes per the rules in Section 13.

### Compatibility check at each boundary

1. **Engine ↔ Backend.** Engine fetches `/api/.well-known/openid-configuration` on startup. Backend includes `X-Endstate-API-Version: 1.0` on every response. Engine refuses to make backup-write calls if the backend's `apiSchemaVersion` major version does not match the engine's expected major. Restore (read-only) is permitted across minor mismatches but warned in logs.

2. **GUI ↔ Engine.** Existing pattern — `endstate capabilities --json` includes `cliVersion` and `schemaVersion`. GUI checks compatibility on startup. Hosted-backup commands gated behind `engineVersion >= 2.0.0` (the version that introduces the `backup` subcommand).

3. **GUI ↔ Backend.** GUI does not talk to the backend directly. All backend calls go through the engine. The GUI's only check is "does the engine I'm bundled with support hosted backup?"

### Breaking-change protocol (post-1.0)

When `apiSchemaVersion` major bumps, the substrate backend supports the old major version for at least 90 days alongside the new major. Engines released during that window are bumped to the new major. Engines released before the bump continue working. After 90 days, old major support is dropped — engines that have not been updated will receive a clear `SCHEMA_INCOMPATIBLE` error and a remediation pointing at the auto-updater.

### Pre-1.0 / closed-beta exception

During the closed-beta period (before public 1.0 release of Hosted Backup), breaking changes to this contract are allowed without the 90-day overlap window. Any user affected during this period is consulted directly. After public 1.0 release, the breaking-change protocol above applies in full.

---

## 12. GDPR Account Deletion

A user can delete their account at any time. Deletion is hard-delete by default; no soft-delete grace. The cryptographic guarantee means any retained data is useless to us anyway, but explicit hard-delete is the principled posture.

### Endpoint

`DELETE /api/account` with the user's current access token.

### What gets deleted

- All rows in `users`, `sessions`, `subscriptions`, `backups`, `backup_versions` for the userId
- All R2 objects under `users/<userId>/`
- Active Paddle subscription cancelled

### What is retained

- An audit log entry: `{ deletedAt, userIdHash, reason: "user_request" }`. The `userIdHash` is `SHA-256(userId)`, not the original UUID — sufficient for "did this user delete?" queries from the user themselves without retaining identifying information
- Paddle's own transaction records, which Paddle retains independently per their own retention policy. Endstate cannot delete data from Paddle.

### What the user sees

Confirmation dialog with explicit warning: *"This deletes your account, your subscription, and all backed-up data. This cannot be undone."* On confirmation, account deletion is immediate. The user is signed out. Re-signup with the same email is allowed; previous data is unrecoverable.

### Active subscription

If the user has an active subscription at deletion, the subscription is cancelled. No prorated refund — the user has chosen to delete; their billing relationship ends. This is documented in Terms.

### Timing

Account deletion is synchronous from the user's perspective (returns 200 once Postgres rows are deleted and Paddle is notified). R2 object purging is asynchronous, completes within 24 hours, scheduled job. The substrate backend marks the user's R2 prefix for deletion and a cron job runs the actual deletes.

---

## 13. Schema Evolution

### Additive (no schema bump)

- New optional fields in request/response shapes
- New optional manifest fields
- New error codes
- New endpoints
- New `endstate_extensions` discovery fields
- New subscription states (must default to least-permissive behaviour for older clients)

### Breaking (schema bump required)

- Field removal or rename
- Type changes
- Semantic changes to existing field meaning
- KDF parameter floor changes (e.g., raising memory minimum from 64 MiB to 128 MiB)
- Encryption envelope version changes
- Auth flow shape changes
- Subscription state semantic changes

A schema bump triggers the breaking-change protocol from Section 11.

---

## 14. References

### Endstate documents

- `PRINCIPLES.md` — the seven public commitments
- `docs/ai/PROJECT_SHADOW.md` — architectural truth
- `docs/contracts/cli-json-contract.md` — error envelope conventions
- `docs/contracts/event-contract.md` — event ordering and JSONL format
- `docs/contracts/profile-contract.md` — profile manifest validity rules
- `docs/contracts/gui-integration-contract.md` — GUI ↔ engine contract
- `docs/contracts/config-portability-contract.md` — export/restore primitive

### External standards

- **RFC 9106** — Argon2 specification
- **RFC 8032** — Edwards-curve digital signatures (Ed25519)
- **RFC 8037** — JOSE EdDSA
- **RFC 5116** — AEAD (AES-GCM ciphertext format)
- **NIST SP 800-38D** — AES-GCM
- **OWASP Cryptographic Storage Cheat Sheet** — current Argon2id parameter recommendations
- **OpenID Connect Core 1.0** — OIDC discovery format

### Reference implementations

- **Bitwarden** — closest at-scale reference for split-output Argon2 auth
- **Filen.io** — closest architectural reference (Windows-first hosted backup with self-host option)
- **Standard Notes** — chunked envelope format reference
