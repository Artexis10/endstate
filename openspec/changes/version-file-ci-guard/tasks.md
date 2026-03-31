## 1. Sync VERSION

- [x] 1.1 Write "1.7.2" to the VERSION file (no trailing newline)

## 2. CI Drift Guard

- [x] 2.1 Add "Check VERSION drift" step to `.github/workflows/go-ci.yml` before Vet step

## 3. Verification

- [x] 3.1 Confirm VERSION contains "1.7.2" matching `.release-please-manifest.json`
