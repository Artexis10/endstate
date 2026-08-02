# Change: Verified Module Matrix

## Why

Endstate's current CI proves that the built CLI can execute representative commands, but its configuration checks use a hand-written temporary manifest and do not prove that every production module survives a real capture-bundle/rebuild/revert cycle. Its best-effort Notepad++ pre-install normally exercises the already-present path rather than proving an Endstate-driven installation. The Windows Sandbox harness covers selected applications, yet it reimplements module behavior in PowerShell and cannot run reliably on standard GitHub-hosted runners because nested virtualization is not a supported dependency.

That leaves two gaps at the moment Endstate is being published and demonstrated:

1. A catalog change can be structurally valid while breaking the production engine path for one or more modules.
2. A green check cannot yet be translated into a precise public claim about which modules were exercised synthetically and which were proven against a real installed application.

Endstate needs a catalog-wide, production-engine validation system whose PR path is fast enough to remain blocking and whose broader live-install path can run unattended on public-repository CI.

## What Changes

- Add tracked validation metadata for every module, including all schema-specific deterministic engine scenarios, live-run eligibility, package reference, fixture strategy, resource budget, and any blocked/manual/lab-only reason.
- Add a catalog-wide synthetic matrix that runs the built `endstate` executable against production module definitions and isolated fixtures.
- Require symmetric schema-v1 modules to prove payload provenance and rewritten restore entries through targeted capture bundle -> mutation -> rebuild/restore -> verify -> revert, and schema-v2 modules to prove every selectable generation/fingerprint plus every migration edge.
- Put repair of capture-only modules in scope; they cannot be relabeled or quarantined into a false config-roundtrip pass.
- Require install-only modules to prove production module resolution, planning, and verification against isolated installed-state fixtures without claiming a real installation occurred.
- Add a live Windows matrix that lets the production engine install eligible applications on fresh standard GitHub-hosted runners, captures a targeted recovery bundle, removes the app and settings, then proves the production `rebuild` path can reinstall, restore, and verify them.
- Run all synthetic module scenarios and all bundle-resolution checks on every pull request, a live Notepad++ canary on engine-affecting pull requests, changed live-eligible modules on module pull requests, and the complete live-eligible set on a schedule.
- Emit compact, schema-versioned evidence and an honest public summary that distinguishes engine-contract, config-roundtrip, live-install, manual, blocked, stale, quarantined, and not-applicable states.
- Bound the blocking PR critical path with sharding, concurrency limits, standard-runner-only policy, per-job timeouts, and failure-only artifact uploads.
- Track the initial synthetic failure debt as protected-main-owned, exact structured evidence without counting a known failure as proof, permitting a pull request to mint new debt, or allowing the full catalog denominator to shrink silently.
- Keep the catalog-wide matrix maintainable through presence-aware sidecar defaults and a deterministic check-only revision synchronizer while preserving every scenario, fixture, independent verifier, and proof identity.

## Capabilities

### New Capabilities

- `verified-module-matrix`: Catalog-wide production-engine validation, live installed-app evidence, CI selection policy, runtime/cost controls, and public proof semantics.

### Modified Capabilities

- `sandbox-validation`: Legacy direct-copy Sandbox results are explicitly classified as curation/harness evidence and cannot claim production-engine proof.

The existing `sandbox-validation` capability remains available for local discovery and Windows Sandbox investigations. Its results do not satisfy the new production-engine evidence levels unless it invokes the built engine through the new harness.

## Impact

- **Catalog:** `modules/apps/*/validation.jsonc` becomes required alongside every `module.jsonc`.
- **Catalog maintenance:** Uniform validation boilerplate may be omitted only when it resolves deterministically from the production module before validation and digesting; explicit invalid values remain errors.
- **Engine test tooling:** Adds isolated production-binary harnesses and deterministic fixture/path virtualization.
- **CI:** Adds generated synthetic and live matrices under `.github/workflows/` while preserving the required `Go Tests` check name.
- **Evidence:** Adds compact PR evidence plus trusted protected-main/release publication suitable for public compatibility reporting.
- **Documentation:** Updates validation guidance and clearly defines what each proof level does and does not establish.
- **GUI:** GUI end-to-end proof is outside this change and requires a separate downstream OpenSpec in the GUI repository; the module matrix must not imply GUI coverage.

## Non-Goals

- Running every real application on every pull request.
- Treating a synthetic fixture as proof that an installer, application launch, account flow, license, hardware integration, or GUI works.
- Making Windows Sandbox or a self-hosted runner a dependency of untrusted pull-request checks.
- Uploading installers, captured application payloads, credentials, or user data as CI artifacts.
- Automatically committing generated compatibility results back to the repository.
