## Why

The Hosted Backup feature (end-to-end encrypted cloud backup via Endstate Cloud) spans three repositories — `endstate` (engine), `endstate-gui`, and `substrate` (backend). No formal contract document existed to arbitrate cross-repo behavioural requirements. `docs/contracts/hosted-backup-contract.md` was written and landed; this change formally registers it in OpenSpec so it is tracked as a first-class contract alongside the other docs/contracts files.

## What Changes

- Add `docs/contracts/hosted-backup-contract.md` to OpenSpec tracking (contract already on disk, not yet committed)
- No engine code changes; no spec behaviour changes

## Capabilities

### New Capabilities
- `hosted-backup-contract`: The canonical cross-repo contract for Endstate Hosted Backup — covering the trust model, KDF parameters (Argon2id locked v1), AES-256-GCM encryption envelope, JWT/EdDSA auth format, auth and backup API surface, Cloudflare R2 storage layout, versioning policy, subscription state machine, GDPR deletion, version compatibility matrix, and schema evolution rules.

### Modified Capabilities
<!-- None — this is a docs-only change. No existing spec requirements are changing. -->

## Impact

- **`docs/contracts/hosted-backup-contract.md`** — added (the deliverable)
- **No code changes** — no engine, GUI, or substrate code is modified by this change
- **Cross-repo contract**: once formally tracked here, any future implementation PR in `endstate`, `endstate-gui`, or `substrate` that touches the hosted-backup surface must reference and remain consistent with this contract
