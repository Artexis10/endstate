## Why

The VERSION file drifted from the release-please manifest (1.5.1 vs 1.7.2) and nobody noticed until a production build investigation revealed stale version reporting. Without ldflags, dev-mode `go run` builds were silently using the wrong version. There is no automated check preventing this drift from recurring.

## What Changes

- Sync the VERSION file to the current correct value (1.7.2)
- Add a CI step to `.github/workflows/go-ci.yml` that fails when VERSION and `.release-please-manifest.json` disagree

## Impact

- `.github/workflows/go-ci.yml` — gains a pre-flight "Check VERSION drift" step
- `VERSION` — synced from 1.5.1 to 1.7.2
- No schema version bump (operational/CI change, not a behavioral change)
