## Why

A self-hosted issuer or API endpoint can return a 307 or 308 redirect. Go's
default client follows it and replays a POST body, which could send passwords,
recovery proofs, encrypted backup data, or bearer credentials to Endstate Cloud
or another origin.

## What Changes

- Default backup API and OIDC HTTP clients follow redirects only within the
  original origin (scheme, hostname, effective port).
- Cross-origin redirects, including HTTPS-to-HTTP downgrades, fail without
  exposing request query values in the returned error.
- Same-origin redirects remain supported.

## Capabilities

### Modified Capabilities

- `hosted-backup-auth-client`: redirect handling preserves request-body and
  credential origin boundaries for self-hosted deployments.

## Impact

- `go-engine/internal/backup/client/`
- `go-engine/internal/backup/oidc/`
- `docs/contracts/hosted-backup-contract.md` §9
