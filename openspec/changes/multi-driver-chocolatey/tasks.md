## 1. Backend Registry and Routing Foundation

- [x] 1.0 Update locked CLI/event/profile/GUI contract shapes before landing their corresponding implementation fields
- [x] 1.1 Add red tests for Windows/macOS/Linux backend registry contents, default-driver resolution, explicit-driver resolution, and unsupported-driver rejection
- [x] 1.2 Implement the platform backend registry and route capabilities through it while preserving existing advertised backends
- [x] 1.3 Add red tests proving mixed per-package driver partitioning and no cross-driver fallback in shared planning/apply/verify helpers
- [x] 1.4 Implement generic Windows per-driver app partitioning while preserving the shipped Nix+Brew orchestration byte-for-byte

## 2. Chocolatey Driver

- [x] 2.1 Add red hermetic tests for missing-binary handling, latest install, already-present detection, batch detection, and failure classification
- [x] 2.2 Implement Chocolatey detect/install and shared command execution with configured sources left untouched
- [x] 2.3 Add red tests for exact-version install, downgrade-capable repin, uninstall without dependency removal, and reboot-required success
- [x] 2.4 Implement Chocolatey versioned install/repin, uninstall, and reboot-required fact propagation
- [x] 2.5 Add red tests for current and legacy local-list syntax, deterministic installed-package enumeration, versions, and package-ledger edge cases
- [x] 2.6 Implement Chocolatey installed-package enumeration and the shared `InstalledEnumerator` capability

## 3. Multi-Driver Command Integration

- [x] 3.1 Add red command tests for explicit Chocolatey apps across plan, apply, verify, provisioning generations, and best-effort rollback
- [x] 3.2 Route plan/apply/verify/generation/rollback through resolved registry drivers, retaining each app's selected driver in results and history
- [x] 3.3 Add red tests for globally unknown validation failure, known unsupported-host skips, absent/no-consent skips, attempted-bootstrap lane failure, and no Winget fallback
- [x] 3.4 Implement the locked validation/availability state mapping and pre-mutation command preflight

## 4. Multi-Driver Capture

- [x] 4.1 Add red capture tests for default mixed enumeration, repeatable `--driver` filtering, Chocolatey provenance, deterministic ordering, and `--pin` versions
- [x] 4.2 Refactor Winget and Brew enumeration behind `InstalledEnumerator` and implement generic capture aggregation with `(driver, ref)` update keys and deterministic ID collision suffixes
- [x] 4.3 Add red tests for optional-driver-unavailable continuation, explicit-driver-unavailable failure, authoritative deduplication, and uncertain-overlap preservation/warnings
- [x] 4.4 Implement additive structured warnings and preserve all cross-driver entries without fuzzy suppression
- [x] 4.5 Preserve existing Winget-only manifest merge, config-module mapping, display-name, bundle, and event behavior in regression tests

## 5. Driver-Aware Module Correlation

- [x] 5.1 Add red module validation/matching tests for `matches.chocolatey`, legacy `configModuleMap`, namespaced `packageModuleMap`, and capture metadata
- [x] 5.2 Implement Chocolatey module matches and driver-aware module mapping without changing existing Winget keys

## 6. Consent-Gated Chocolatey Bootstrap

- [x] 6.1 Add red tests for missing Chocolatey preflight in apply/rebuild, dry-run reporting, existing consent flags, known-path resolution, non-admin/verification failure, and no fallback
- [x] 6.2 Extend `--bootstrap-backends` / `--no-bootstrap` and the existing bootstrap abstraction with Chocolatey's official install path plus post-install executable verification
- [x] 6.3 Propagate backend-bootstrap consent through rebuild and preserve the combined GUI consent event

## 7. Contracts and Verification

- [x] 7.1 Finish CLI JSON, event, profile/manifest, and GUI integration contract examples for driver selection, warnings, reboot facts, bootstrap reuse, capabilities, and module maps
- [x] 7.2 Run focused package/command tests after each TDD slice, then `go test ./...`, `go vet ./...`, Windows build, and strict OpenSpec validation (the full suite's pre-existing registry integration tests remain blocked by sandbox registry-write denial; all feature packages pass)
- [x] 7.3 Perform an independent review/verifier pass against the approved spec and resolve all critical/important findings
- [x] 7.4 Record maintainer-side Windows smoke steps for mixed capture/rebuild, bootstrap absent/present paths, pin/repin, rollback, and source/credential non-mutation
