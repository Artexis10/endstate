# Design: Verified Module Matrix

## Context

At design time the catalog contains 357 production modules, 41 module seed scripts, and a 60-entry Windows Sandbox queue. Of those seeds, only 32 are paired with a Winget reference, and 14 modules have drift between their `curation.seed` declaration and the actual seed file. There are 354 schema-v1 modules and three schema-v2 modules; v2 currently exposes four selectable generation alternatives and one migration edge. Thirty modules are intentional install-only modules, while 43 more declare capture sources without any restoration contract and therefore cannot honestly earn config-roundtrip proof until repaired. The existing `Integration Test (windows)` job builds the real CLI, but its settings checks use a hand-written temporary manifest. Its best-effort Notepad++ pre-install normally makes `apply` exercise the already-present/skip path rather than proving that Endstate itself can install the application. The separate Sandbox harness installs and manipulates real applications, but its capture, wipe, restore, and verification stages are implemented in PowerShell rather than by the production engine; unsupported operations and zero-setting runs can currently be skipped without proving a production roundtrip.

The new system therefore needs two complementary kinds of evidence:

- deterministic production-engine evidence for the entire catalog; and
- real installed-application evidence for the subset that is safe and reliable on fresh hosted Windows runners.

Neither kind may be presented as the other.

## Goals / Non-Goals

**Goals:**

- Exercise every production module and every selectable schema-v2 generation/migration scenario through the built CLI on every pull request.
- Prove the full configuration lifecycle for every config-bearing module without mutating the runner outside an isolated test root.
- Prove real Endstate-driven installation and configuration for hosted-runner-eligible applications.
- Make missing coverage, zero-assertion passes, skips, quarantines, and infrastructure failures visible.
- Keep the blocking PR workflow below a 40-minute execution critical path, excluding GitHub queue time, with a normal target below 20 minutes.
- Keep GitHub-hosted compute at zero cost for a public repository by using only standard runners and bounded artifact storage.
- Produce small revision-bound evidence, with trusted protected-main/release publication that can back workflow badges and a compatibility page without overstating support.

**Non-Goals:**

- Claiming that all catalog applications can be installed unattended on GitHub-hosted runners.
- Using live applications where deterministic fixtures provide stronger regression isolation.
- Replacing unit tests, local Sandbox discovery, manual release testing, or GUI-repository end-to-end tests.
- Giving untrusted fork pull requests access to secrets or self-hosted machines.
- Caching or redistributing third-party installers.

## Proof Model

Each module has separate proof dimensions. A single `PASS` boolean is insufficient.

| Proof level | What it establishes | What it does not establish |
|---|---|---|
| `catalog` | Module and validation metadata parse, paths are safe, schema-specific scenarios and bundle references are valid | Runtime behavior |
| `engine-contract` | The built CLI non-vacuously executes the production module's declared capture, restore, or install/verifier contract against isolated fixtures | A bidirectional configuration roundtrip, real package installation, or application behavior |
| `config-roundtrip-v1` | The built CLI captures exact payload/provenance, rewrites restore entries, rebuilds exact content, converges on repeat, and reverts | Schema-v2 generation selection or real app behavior |
| `config-roundtrip-v2` | The built CLI proves one declared generation/fingerprint path, its validation, rebuild, and revert semantics | Other generation alternatives or migration edges unless separately evidenced |
| `live-install` | The built CLI installs the declared package on a fresh VM and the production verifier observes it | Configuration portability unless paired with `live-config-roundtrip` |
| `live-config-roundtrip` | Real install plus seeded production-module capture/restore/verify/revert succeeds | GUI orchestration or account/license/hardware flows |
| `lab` / `manual` | A documented external procedure is required | Automated CI coverage |

Public summaries report these dimensions independently. For example, `all catalog modules have current engine scenarios` and `N/M hosted-live modules passed` are valid only when the scenario denominator includes every required schema-v2 alternative/migration and asymmetric modules remain explicitly blocked. `all catalog apps work` is not a valid claim.

## Validation Metadata

Every `modules/apps/<id>/module.jsonc` has a sibling `validation.jsonc`. Keeping CI policy outside `module.jsonc` prevents runner concerns from becoming runtime module schema. A module may require more than one synthetic scenario; one sidecar is not equivalent to one test. The canonical scenario-kind enum is `config-roundtrip-v1`, `config-generation-v2`, `config-migration-v2`, `capture-contract`, `restore-contract`, and `install-contract`; scenario kinds are metadata inputs, while `catalog`, `engine-contract`, `config-roundtrip-v1`, `config-roundtrip-v2`, `live-install`, and `live-config-roundtrip` are emitted proof levels.

Illustrative shape:

```jsonc
{
  "schemaVersion": 1,
  "synthetic": {
    "scenarios": [
      {
        "id": "default-v1",
        "mode": "config-roundtrip-v1", // or config-generation-v2, config-migration-v2, capture-contract, restore-contract, install-contract
        "fixture": { "type": "auto" },
        "timeoutSeconds": 60,
        "minimumAssertions": { "captured": 1, "restored": 1, "content": 1 }
      }
    ]
  },
  "live": {
    "mode": "candidate", // hosted, candidate, blocked, lab, manual, or not-applicable
    "reasonCode": "unproven-hosted-baseline",
    "explanation": "Awaiting a trusted hosted baseline.",
    "driver": "winget",
    "ref": "Notepad++.Notepad++",
    "seed": "seed.ps1",
    "comparator": "exact-bytes",
    "proofMode": "live-config-roundtrip",
    "prTimeoutMinutes": 20,
    "scheduledTimeoutMinutes": 30,
    "runnerLabel": "windows-latest",
    "trust": { "seedSha256": "<lowercase-sha256>" }
  }
}
```

A schema-v2 scenario may use exact literal identities when the fixture owns them, or derive identities from declared fixture/detector coordinates when the canonical locator is discovered at runtime:

```jsonc
"expected": {
  "identityMode": "derived-from-fixture", // or literal with captureId + instanceId
  "detectorId": "versions",
  "configSetId": "preferences",
  "generationId": "g1",
  "fingerprint": "<lowercase-sha256>"
}
```

The derived form does not weaken the assertion: the harness independently computes the stable instance and capture identities from the contained fixture coordinates, then compares them with the exact IDs emitted by the engine. It never stores placeholder hashes in metadata.

A reviewed one-way scenario carries its approval beside the scenario rather than relying on a comment or quarantine:

```jsonc
"review": {
  "decision": "approved-one-way",
  "reasonCode": "vendor-format-is-export-only",
  "reviewer": "@module-owner",
  "reviewedOn": "2026-07-21",
  "evidence": "The vendor documents export without a compatible import path."
}
```

Rules:

- `synthetic.scenarios` is required and non-empty for every module. Symmetric schema-v1 modules use `config-roundtrip-v1`; schema-v2 modules declare a scenario for every selectable generation/fingerprint and every migration edge, always backed by non-empty target-generation validation and by edge validation for migrations. A schema-v2 scenario requires a top-level app-verifier assertion only when `Module.Verify` is non-empty. Modules intentionally containing no capture/restore entries use `install-contract`.
- A capture-bearing module cannot earn config-roundtrip proof unless every captured target has an executable restoration contract with non-zero restore and revert assertions. The current capture-only set is blocked and explicitly repaired during rollout; it cannot be relabeled install-only, waived, or removed from the denominator to manufacture a full-catalog result.
- An intentionally one-way module may receive a `capture-contract` or `restore-contract` scenario only with machine-checked `approved-one-way` review metadata containing a stable reason code, reviewer, strict non-future review date, and evidence. Other scenario kinds forbid that review object. A capture contract must prove targeted production-engine capture, exact payload/provenance/content, and non-zero capture assertions. A restore contract must prove the production restore operation, exact content, nested summaries, verification where declared, immediate revert, and non-zero restore assertions. Both emit only `engine-contract`; they remain outside the config-roundtrip numerator and denominator accounting shows the exclusion.
- Schema-v2 expected identities explicitly choose `literal` or `derived-from-fixture`. Literal identity requires exact `captureId` and `instanceId` and forbids a detector. Derived identity requires a detector declared by the production module and forbids precomputed capture/instance IDs; the harness derives those IDs from the canonical contained fixture locator and still compares the exact engine output.
- Install contracts accept any nonblank production app reference family: Winget, Chocolatey, executable, uninstall-display-name, or path-exists. They still require at least one production verifier.
- Automatic fixtures derive deterministic, non-secret sentinel content from the production capture/restore definitions. For schema-v1 auto fixtures, a destination with an extension receives a representative file fixture and an extensionless destination receives a representative directory fixture. This is deterministic fixture construction, not an inference or claim about the live application target's filesystem type. A type-sensitive shape must use a tracked SHA-256-bound declarative fixture. Format-aware fixture builders cover copy, directory copy, merge-json, merge-ini, append, and registry operations.
- A module may select a tracked custom fixture script when automatic synthesis cannot represent a dynamic path or format. Custom fixtures run only inside the same isolated roots and must declare their expected assertions.
- `live.mode: hosted` requires an unattended package reference, a reconciled real seed where configuration is claimed, a successful baseline run, and a standard-runner timeout. Unproven installable entries start as `candidate`; known automation gaps use `blocked`. A candidate may carry complete proposed `live-config-roundtrip` metadata: Winget driver/ref must exactly match the production module's Winget install reference, the safe module-relative seed is SHA-256-bound, `comparator` is the closed built-in `exact-bytes` enum, and no fake `comparatorSha256` is allowed. Candidate and local runs are non-mutating, non-proof, and public-ineligible: Task 9 metadata cannot authorize execution, mutation, or proof minting. Only a later external trusted exact-ref hosted authority may mint mutation eligibility. The compared target set is derived later from production capture/restore definitions, not duplicated in metadata. External comparator artifacts are unsupported unless a future schema adds a hash-required artifact form. `candidate`, `blocked`, `lab`, `manual`, and `not-applicable` require a stable reason code and human-readable explanation and never count as verified.
- A downgrade from `hosted` or any required config scenario, or any quarantine, is evaluated from both the merge base and pull-request head so a pull request cannot evade an existing required lane by editing its own metadata.
- Validation metadata is schema-checked, module IDs are unique, and every module has exactly one matching sidecar. Missing, duplicate, stale, or unknown entries fail the catalog gate.

## Synthetic Production-Engine Harness

The synthetic harness runs the exact `endstate` executable built for the workflow. It must not reimplement capture, bundle creation/path rewriting, generation selection, restore, verification, journaling, or revert logic in PowerShell.

OS boundaries may be virtualized; engine behavior may not. Because `capture --only apps.<id>` correctly rejects module-only selection, the binary gains a fail-closed test-mode inventory seam. The seam injects exactly one detected app identity plus its package/executable/uninstall evidence, after which ordinary module matching and `capture --only <app-id>,apps.<module-id>` remain authoritative. Test mode is explicit in envelopes/evidence, is accepted only with a unique temporary `ENDSTATE_ROOT`, and may replace only inventory/detection, path resolution, registry boundaries, and package-driver side effects. It cannot bypass module matching, capture planning, bundle creation, restore, verification, journaling, revert, nested summaries, or event/envelope parsing.

The isolation layer virtualizes `%APPDATA%`, `%LOCALAPPDATA%`, `%USERPROFILE%`, `%PROGRAMFILES%`, `%PROGRAMFILES(X86)%`, `%PROGRAMDATA%`, `%PUBLIC%`, `%SYSTEMROOT%`/`%WINDIR%`, `%TEMP%`/`%TMP%`, and declared dynamic instance roots into per-scenario directories. It maps HKCU operations into `HKCU\Software\Endstate\Validation\<run-id>\...`; HKLM writes are rejected unless a disposable registry adapter can virtualize them without changing the module contract. Unresolved variables, hard-coded host paths, device paths, reparse-point escapes, and path traversal fail preflight.

Synthetic scenarios have no package/network execution and no arbitrary PowerShell fixture scripts. Automatic and custom fixtures use a contained declarative fixture API. Before and after execution, an independent guard snapshots every original host path/key named by the module plus the repository and task roots; any out-of-allowlist write fails the scenario even if engine assertions pass.

For each required config-roundtrip scenario the harness performs:

1. Load the production module and validation sidecar.
2. Create deterministic baseline fixtures at every expected capture target and record content/type/ACL-independent hashes.
3. Invoke `capture --only <injected-app-id>,apps.<module-id>` through the built CLI and assert exact selection plus the schema-specific artifact contract.
   - Schema v1: exact payload, module provenance, and rewritten restore entries.
   - Schema v2 generation: capture ID, config-set/instance identity, selected generation/fingerprint, and validation metadata.
   - Schema v2 migration: source generation, declared migration edge, migrated generation/fingerprint, and validation metadata.
4. Mutate or remove every seeded target and record the pre-restore state.
5. Invoke the built CLI `rebuild --from <bundle> --only <app-id> --confirm` path with an isolated package/detection seam that prevents network installation while preserving production planning, bundle extraction, restore, schema-specific config resolution, verification, journaling, and envelope/event handling.
6. Inspect nested apply, configuration-resolution, restore, and verify summaries rather than trusting process exit or the outer success envelope; assert exact restored content, expected operation counts, backup/journal creation, non-zero generation validation for schema v2, edge validation for migrations, and at least one app-verifier pass when the production module declares a top-level verifier, with zero nested failures.
7. Invoke the built CLI revert path immediately and assert the recorded pre-restore mutated state is recovered.
8. Invoke rebuild again to recover the captured state, then invoke one further rebuild and assert convergence with no content drift or nesting.

For a `capture-contract` module the harness performs targeted production-engine capture and proves exact selection, payload, provenance, content, and non-zero capture assertions without inventing a restore contract. For a `restore-contract` module it invokes the production restorer from a deterministic bundle and proves exact content, nested summaries, declared verification, immediate revert, and non-zero restore assertions. Both record only `engine-contract`. For an `install-contract` module the harness resolves the production module into a plan, supplies an isolated installed-state fixture at the declared verifier boundary, and invokes the built CLI verifier. It likewise records `engine-contract`, never a config-roundtrip or live-install proof level.

A scenario cannot pass vacuously. Schema-v1 roundtrips must meet their declared minimum capture, payload/provenance, rewritten-restore, content, rebuild, nested-summary, verifier, and revert assertions. Schema-v2 scenarios must additionally prove the selected generation/fingerprint, non-zero production target-generation validation, any declared migration edge plus its validation, and a top-level app verifier only when the production module declares one. Capture and restore contracts must meet their reviewed non-zero one-way assertion sets, and install contracts must resolve at least one app reference and execute at least one production verifier. Unknown operations, all-skipped operations, malformed JSON/events, success envelopes containing nested failures, or zero expected assertions fail.

## Bundle Validation

Every tracked bundle is resolved by the same built engine on every pull request through a dedicated read-only command:

```text
endstate catalog-plan --bundle <tracked-bundle-path> --json --events jsonl
```

The command is a catalog module-resolution plan, not an application installation plan. The built binary parses the bundle, resolves its ordered module memberships against the strict production module and validation-sidecar catalog, and emits one non-skipped action per membership. The harness only invokes the command and validates/aggregates its output; it must not expand bundle membership itself. Ordinary `plan` success, an empty action list, or a harness-produced module list is not bundle proof.

Bundle inputs are fail-closed. A bundle must be a regular immediate child of `<root>/bundles`, must not escape through traversal, links, or reparse points, and must use the exact bundle schema with no unknown or duplicate fields or trailing object. Its file stem and declared ID must agree. Memberships are bare canonical module slugs that resolve to `apps.<slug>`. Duplicate canonical membership within one bundle fails; intentional reuse of a module across different bundles is allowed and reported by the aggregate.

Resolution requires the canonical module and its matching validation sidecar. Each emitted action binds the bundle identity and hash to the canonical module ID, module revision/schema version, validation hash, and validation scenario count. Action order matches declaration order, the action count exactly equals the membership count, and skipped or unresolved memberships fail. Repeating the command with the same separately built binary and inputs must produce an identical stable projection.

The engine does not synthesize application declarations from module matchers, choose among multiple package references, or treat an executable-only module as installable. Package and verifier policy remain the authority of the later synthetic and live scenarios. Bundle rows therefore earn only `catalog` proof. They do not earn `engine-contract`, config-roundtrip, live-install, GUI, workflow, or public-proof status, and general bundle composition inside ordinary user manifests is outside this change.

The pull-request matrix runs every tracked bundle twice and the aggregate requires the complete expected bundle set, total membership count, unique-module count, and explicit cross-bundle reuse report. Missing, duplicate, stale, partially resolved, wrong-revision, or schema-incompatible rows fail aggregation.

## Live Hosted-Runner Harness

"Hosted-live" means a real application lifecycle executed on a fresh standard GitHub-hosted Windows VM whose exact runner label and architecture are bound into authority and evidence. The initial Notepad++ lane uses the standard `windows-11-arm` image and native ARM64 engine and validator binaries. It does not mean hosted backups, remote backup storage, or a simulated package state. The recovery bundle, backup records, journal, and restored settings exist only inside the disposable runner unless sanitized compact evidence is uploaded. Windows Sandbox is not nested inside the runner.

A module is hosted-safe only when its exact production package reference supports unattended install and removal; the application needs no account, paid license, interactive UI, reboot, hardware, or kernel driver; the engine verifier can be reconciled with a stable independent package/version observer; its declared configuration targets can be seeded with deterministic non-secret bytes and cleaned within a closed observer boundary; and the full lifecycle fits the 45-minute scheduled ceiling on a standard runner. That boundary enumerates the expected package identities, executables and uninstall-registry keys, declared configuration filesystem/registry targets, allowed services, drivers, and scheduled tasks (normally empty), and Windows pending-reboot indicators. Admission requires before/after observation for every category and rejects an unexpected delta inside the boundary. Launching or clicking the application's GUI is not required for this engine proof. A real package, executable/version observation, production configuration targets, and exact restored bytes are required. The proof claims absence and cleanup only over that declared boundary, not whole-machine cleanliness. Modules outside it remain explicit `candidate`, `blocked`, `lab`, or `manual` rows rather than being silently skipped.

Each job handles exactly one module:

1. Check out the tested commit and download the engine artifact built once by the workflow.
2. Independently confirm the target package is absent using the native package manager plus executable/uninstall-registry evidence, or record and safely remove a preinstalled copy and prove absence before continuing.
3. Generate a one-module manifest from the production module metadata.
4. Invoke the production engine `apply` path and let its declared package driver perform the initial install. Workflow-level pre-installation is forbidden for a `live-install` claim.
5. Reconcile the production verifier with an independent observer: exact native package-manager query plus executable/uninstall-registry/version evidence. Record the resolved installed version, then run the trusted deterministic non-secret seed when configuration proof is claimed.
6. Invoke targeted production capture to create the recovery bundle and require non-zero selected config evidence.
7. Stop the app, uninstall it on the disposable runner, wipe every captured target, and independently prove the package and every destination are absent. If a clean destination cannot be established, withhold rebuild proof.
8. Invoke `endstate rebuild --from <bundle> --only <app-id> --confirm` so the production engine reinstalls the package, restores configuration, and verifies the result.
9. Inspect nested apply, configuration-resolution, restore, and verify summaries; require zero failures and non-zero relevant operation counts. Reconcile the reinstalled package with the independent observer and compare only known non-secret seeded settings with the captured payload under the declared comparator. Run configuration revert immediately and prove the settings targets return to their wiped pre-restore state while the rebuilt package remains installed, then rebuild twice to prove configuration recovery and convergence.
10. Perform unconditional bounded uninstall/target cleanup, prove final absence and equality over the declared observer boundary, emit sanitized compact evidence, and allow the hosted VM to be discarded. Cleanup failure or an unexpected boundary delta invalidates the proof rather than producing a green result.

Install-only modules can earn `live-install` without a configuration roundtrip. Modules requiring a Store session, license/account, reboot, unsupported silent installer, interactive UI, hardware, kernel driver, or unsafe privilege remain visible as `lab` or `manual` rather than silently skipped. An engine verifier and the independent observer must agree; either one failing withholds the proof level.

### Pull-request trust boundary

A pull request cannot authorize the installer or seed that its own untrusted code will execute. Live PR selection uses a merge-base-controlled allowlist of package source/driver/ref, installer arguments, seed hash, proof mode, comparator, and timeout. Normal PR, scheduled, dispatch, hosted-denominator, and verified-count planners select only `hosted` rows. Candidate metadata remains non-authorizing: Task 9 stores only validated, hash-bound, configuration-roundtrip-only proposed metadata and no in-process catalog API may turn it into an execution, mutation, or proof-minting authority. Local candidate execution is likewise non-mutating and non-proof. A later external trusted exact-ref hosted authority—not caller-supplied in-process catalog state—may mint mutation eligibility. Only a successful fresh trusted GitHub-hosted baseline may later support a trusted metadata change from `candidate` to `hosted`. A new or materially changed live definition remains `candidate/deferred` on the PR and first runs after trusted merge or explicit maintainer approval; head metadata cannot self-promote it to `hosted`.

Actions are pinned to immutable commit SHAs, checkout uses `persist-credentials: false`, installer subprocesses receive no GitHub token, and live workflows use `pull_request` rather than `pull_request_target`. This does not make untrusted code trusted; it prevents the validation metadata from expanding the external code executed with the hosted Windows runner's elevated account.

### Runner capability probe

Before any hosted-live workflow receives checkout, token, installation, seed, cleanup, or scheduling authority, a separate `.github/workflows/hosted-live-runner-probe.yml` measures the runner prerequisite on every pull request. The probe uses one standard `windows-11-arm` job and an unfiltered `pull_request` trigger so stacked pull requests exercise the same image. It declares no permissions, calls no Action, checks out no repository content, uploads nothing, and exposes no dispatch, push, `pull_request_target`, or schedule trigger.

The single inline PowerShell step is observation-only. It requires an ARM64 runner; exactly one current-user, non-framework, non-resource `Microsoft.DesktopAppInstaller_8wekyb3d8bbwe` package with an ARM64 full-name identity; an install root that is the package-named direct child of the system `Program Files\WindowsApps` directory; and a regular non-reparse `winget.exe` at the exact package root. It records the package name, family, full name, publisher ID, version, architecture, install root, canonical executable path, executable size and SHA-256, and Authenticode signer/status. Trust requires a valid Authenticode result. The executable is invoked directly, never through `PATH` or an App Execution Alias, for bounded `--version` and `--info` checks only; both must exit zero with well-formed non-empty output. Source queries are excluded because they may initialize or update source state.

Missing, ambiguous, wrong-architecture, path-substituted, reparse-backed, unsigned/untrusted, unusable, or oversized results fail explicitly as unsupported capability. The probe never installs, registers, repairs, or updates App Installer. A green probe is prerequisite evidence only: it cannot mint a live mutation permit, hosted/public proof, promotion, or a qualification streak. If it fails, hosted-live mutation work stops until a separately reviewed bootstrap design pins an immutable official App Installer artifact and SHA-256; moving `aka.ms` URLs are never an authority.

The approved stacked-PR probe passed at exact head `674fb00bad2c3e514edd7261fb44873ceb4725ee` in workflow run `30382635430`, job `90353993826`. The `windows-11-arm` image reported an ARM64 `Microsoft.DesktopAppInstaller_1.29.279.0_arm64__8wekyb3d8bbwe` package and a valid Microsoft-signed direct `winget.exe` with SHA-256 `58bd916e39894268f8049dd1658bf391f6c82a196359c8b461347e703643f4b5`; direct bounded `--version` and `--info` checks both exited zero. This closes only the runner prerequisite. No App Installer bootstrap is needed for the initial lane, and the result is not hosted-live application evidence.

### Nested production Winget authority

The trusted AppX binding must reach every Winget process started inside the production engine. Binding only the harness observer and cleanup operations is insufficient because production apply, verify, capture, and rebuild also install, detect, list, detail, and export through Winget. Hosted execution therefore supplies both a private versioned hosted-strict marker and a private versioned child capability containing the exact canonical AppX executable path and expected SHA-256. Both values are derived by the trusted parent and included in the sealed engine-operation environment and process receipt; the parent rejects every hosted engine request that omits either one, including total omission. Neither value has public CLI, manifest, module, or candidate-metadata ingress.

One shared production launcher owns every Winget spawn used by the package driver, snapshot/capture enumeration, and supporting production checks. When the hosted-strict marker is present it requires the complete capability, rejects relative or non-canonical paths, links/reparse points, malformed digests, and digest drift, opens and hashes the exact executable while denying write/delete replacement, invokes only that absolute path while the binding remains held, and never falls back to `PATH`, an App Execution Alias, or another executable. A marker/capability mismatch fails before process start. Ordinary non-hosted commands retain their existing ambient Winget behavior only when the hosted-strict marker is absent; a capability without the marker is invalid rather than an alternate execution mode.

The parent cross-binds the capability to the independently trusted AppX package identity and engine request, and hosted regressions put a hostile `winget` first on `PATH` while proving install/detect/list/details/export cannot reach it. No mutating hosted workflow is added until this production bridge and its focused authority tests pass independent review.

### Task 9 pre-proof evidence boundary

The independent observer is approved only as an observation component. It is neither an execution authority nor hosted proof. The remaining Task 9 evidence layer is deliberately fail-closed and must be complete before any candidate can be selected for mutation by a later trusted hosted authority.

Every engine invocation has an immediate, unexported causal receipt. The receipt binds the typed operation; canonical executable file identity and SHA-256; argv, environment, and working-directory digests; process identity, start/end timing, and exit result; bounded stdout/stderr bytes plus their digests; and the phase nonce and sequence. Evidence decoding consumes that receipt directly. It must not accept caller-supplied or later-substituted raw process bytes as an equivalent source.

The JSON/event decoder treats its stable, audited raw-key inventories and bounded framing as proof schema. It rejects duplicate keys, trailing records/bytes, truncated input, unexpected EOF, unknown fields that could alter authorization, and any value outside the bounded frame. Local wire structures may use `json.RawMessage` to audit the raw object and then decode the official value structures, but the official structures must not silently authorize additive fields. This requirement does not justify changing the production `RebuildResult` source type solely for proof.

Nested event topology is exact. The initial apply emits plan, apply, and verify under its `apply-*` run. A rebuild emits plan, apply, restore, and verify under its `apply-*` run, followed by its distinct `verify-*` segment. The outer rebuild envelope has a separate run ID and must not be mistaken for either nested run. Process success, an outer success envelope, or a structurally similar event stream is insufficient.

Configuration-restore inspection is a separate genuinely read-only operation over an existing root. It creates no directory, lock, recovery record, temporary path, cleanup action, or other write; returns immutable records; rejects pending, temporary, link, and unknown state; scans twice and fails on any change; and requires a no-concurrent-writer or exclusive-lease precondition. It is not permitted to reuse a mutating recovery/cleanup path merely because the expected result is observation.

The evidence layer cross-binds exact pre- and post-state: the captured pinned ZIP identity and digests; mapping targets; config-restore state; backups; generations; relevant logs and journals; and artifact/output roots. One and only one transaction must trace to the nested apply run, capture, and config set. Each output maps one-to-one to its event, journal entry, and physical backup. Revert binds its exact marker, digest, and bytes. Convergence means zero validation-owned persistent or target-state delta after the required repeat operation, not a claim that the whole hosted machine is immutable.

Live lifecycle proof is also bounded. It observes clean absence immediately before the install receipt and presence immediately after it; seeds only staged hash-bound bytes on a disposable least-privilege trusted runner; uses unique attempt roots and link-safe exact-leaf wipe; bounds cleanup; and retries only after clean absence is re-proven. UI, elevation, reboot, and lock requirements are unsupported conditions, not retryable infrastructure failures. Neither a passing local candidate nor successful cleanup is hosted/public proof; promotion remains a later action requiring a real fresh trusted hosted success.

## CI Selection and Scheduling

The matrix planner produces bounded JSON matrices rather than one hand-maintained workflow entry per app.

### Initial Notepad++ rollout

The first real-app lane is a dedicated `.github/workflows/hosted-live.yml` workflow, separate from the fast synthetic `go-ci.yml` matrix and its stable aggregate. Its initial stage is manual-only `workflow_dispatch` on trusted `main`, uses exact `windows-11-arm` execution, and runs only the fixed `Notepad++.Notepad++` candidate. It has no pull-request, push, `pull_request_target`, or schedule trigger and cannot mutate from untrusted code. The engine and validator are native ARM64 binaries built from and attested to the exact clean tested checkout; the workflow file digest, runner label, runner/process architecture, repository, ref, commit, run, and attempt are independently bound rather than accepted as caller assertions.

The external exact-ref authority verifies repository, workflow, event, ref, checkout commit, run, attempt, and trusted actor class before minting a single-use private mutation permit. The permit also binds the engine binary hash; package driver, reference, and arguments; module/validation/seed hashes; comparator and target set; observer boundary; protected workflow/policy digest; phase nonce; and expiry. Those inputs come from the protected qualification record or merge-base allowlist, not caller-supplied candidate metadata. The workflow has one non-cancelling concurrency group, a 45-minute safety timeout, no automatic retry during qualification, and uploads only sanitized versioned evidence of at most 64 KiB with 90-day retention.

The first successful protected-main manual run establishes a diagnostic baseline, not a merge gate or qualification attempt. Only after that evidence is inspected may a separate reviewed controller change pin the exact successful identity and add a schedule trigger. Scheduled attempts then keep testing the pinned campaign even while unrelated commits move `main`; changing any pinned field starts a new streak. Promotion requires ten consecutive scheduled first-attempt clean passes for that identity, and each counting attempt—including build, setup, lifecycle, evidence, and cleanup—must finish within the later 25-minute pull-request ceiling. Manual dispatches, workflow reruns, slow passes, and `PASS_AFTER_RETRY` results do not advance or repair the streak. Promotion is a separate reviewed sidecar/workflow pull request and authorizes only Notepad++. After its merge, a fresh trusted-main run of the promoted `hosted` identity must pass within 25 minutes before the live canary becomes a required matrix row consumed by the stable `Verified Module Matrix` aggregate. Branch protection requires only that stable aggregate, never the conditional canary job name. Fork pull requests remain synthetic-only. This staged gate prevents a new, slow, or flaky external package lane from blocking development before its reliability is measured.

### Pull requests

- Existing Go tests and vet remain blocking.
- The built engine is uploaded once with one-day retention and reused by all validation jobs. All third-party Actions are pinned to immutable commit SHAs.
- All module synthetic scenarios and all bundle checks run in 8 balanced Windows shards. Shard count is configurable up to 16 without changing module metadata.
- After the initial qualification and explicit promotion, engine-affecting same-repository changes run the live Notepad++ canary.
- Up to three changed modules run their live lane when merge-base-controlled metadata says `hosted`; head metadata may retain or weaken selection but cannot authorize a new installer/seed. A larger bulk change is capped on the PR path; overflow modules are reported as `deferred/stale`, never as verified, and can be exercised for the exact commit through bounded deterministic dispatch chunks after trusted merge.
- After canary promotion, validation-harness, workflow, schema, or matrix-planner changes in same-repository pull requests run the live Notepad++ canary plus deliberately good and deliberately broken synthetic fixtures; before promotion they run the synthetic fixtures only.
- Pull requests from forks run synthetic validation only under `pull_request`, with read-only repository permissions, `persist-credentials: false`, no secrets, and standard GitHub-hosted runners only. They do not receive a live mutation permit.
- One stable aggregate job named `Verified Module Matrix` runs under `always()` and converts every missing, skipped, cancelled, neutral, timed-out, or malformed required row into failure. This—not dynamic child job names—is the branch-protection check.

### Scheduled and release runs

- During initial qualification the dedicated nightly/manual workflow selects only Notepad++. Notepad++ promotion authorizes only Notepad++. Each additional module needs its own pinned candidate campaign, ten consecutive scheduled first-attempt clean passes within its declared PR timeout, a separate reviewed promotion, and a fresh post-promotion trusted-main pass before it may enter hosted rotation or pull-request selection.
- A nightly workflow selects at most 64 `hosted` live modules with `fail-fast: false` and `max-parallel: 8`. When the hosted set exceeds 64, deterministic rotation plus priority for stale/failing modules guarantees every hosted entry is attempted within a seven-day attempt SLA. Each row remains bound to the full proof identity it actually tested; the public compatibility page is a heterogeneous row ledger, not an exact-current-commit aggregate. A change to the engine binary/commit, module, validation sidecar, fixture, seed/comparator, package driver/source/ref, or proof mode immediately marks every mismatched live row stale until that module reruns.
- Release validation uses deterministic exact-commit chunks of at most 64 live jobs. Each chunk is capped at 2,880 worst-case runner-minutes, `max-parallel: 8`, and 100 MiB of uploads; the whole release campaign is capped at six chunks and 17,280 worst-case runner-minutes. A release-wide live numerator is published only after every required chunk for that engine hash completes, and the planner rejects a campaign that cannot fit those limits. Evidence from another engine commit is never reused as release proof.
- Lab/manual modules remain in the summary with their reason and last independently recorded result, but stale manual evidence is never converted into a green automated check.
- A `workflow_dispatch` run targets exact module IDs or one deterministic chunk index for one engine commit. One invocation may plan at most 64 live jobs, uses `max-parallel: 8`, and is capped at 2,880 worst-case runner-minutes and 100 MiB of uploads. Requests above the cap are rejected with the remaining chunk plan rather than silently launching an unbounded matrix.

## Runtime and Cost Budget

- Repository workflows use standard GitHub-hosted labels only. Larger runner labels are rejected by a policy check.
- A synthetic shard has `timeout-minutes: 15`. Every pull-request live job, including Notepad++, has `timeout-minutes` at most 25. Scheduled live jobs default to 30 and may declare at most 45 with justification; the 45-minute allowance is forbidden on the PR path.
- The blocking pull-request DAG has a 40-minute hard execution budget excluding GitHub queue delay and targets a sub-20-minute critical path. Changed live modules run in parallel, never serially behind one another.
- Pull-request live work uses the Notepad++ sentinel plus at most three changed-module jobs in parallel. The new matrix work is capped at 250 worst-case runner-minutes per PR, including planning/build/aggregation allowances. Every scheduled, dispatch, or release chunk is capped at 64 live jobs, 2,880 worst-case runner-minutes, `max-parallel: 8`, and 100 MiB of uploads; an exact-commit release campaign is additionally capped at six chunks and 17,280 worst-case runner-minutes. Plans exceeding a declared cap fail before spawning jobs.
- Matrix chunks remain below GitHub's per-workflow matrix limit. The planner splits larger future catalogs deterministically.
- Installers and captured payloads are never cached. Go dependencies may use the normal setup-go cache.
- The engine artifact uses one-day retention. Each evidence record is capped at 64 KiB, detailed failure diagnostics at 1 MiB per module, and all uploaded validation artifacts at 100 MiB per workflow run. Compact PR evidence uses at most seven-day retention. Compact scheduled live evidence uses 90-day retention so the trusted publisher can reconstruct at least ten scheduled attempts under the seven-day rotation SLA. Failure diagnostics use at most three-day retention.
- Validation-owned retained artifacts/status assets have a repository-wide rolling cap of 1 GiB and must also preserve at least 25% headroom below the repository/account included-storage allowance, whichever is stricter. A serialized artifact-budget step inventories retained bytes plus reservations for in-progress validation runs before upload. It deletes expired engine/diagnostic/PR artifacts, compacts scheduled history to the last ten attempts per module/lane, and removes superseded raw scheduled evidence only after the compact ledger is verified. Optional diagnostics are dropped first; if required compact proof cannot fit, upload/planning fails rather than exceeding the cap or discarding required history.
- A repository Actions budget with stop-on-limit at zero dollars is documented as an owner setup step. CI configuration alone cannot enforce account billing settings.
- Under GitHub's current terms, standard GitHub-hosted runner compute is free for this public repository. That does not cover larger runners, paid third-party packages/services, or storage beyond included allowances; the standard-runner policy, compact evidence caps, retained-storage guard, and zero-dollar stop-on-limit setting keep this design inside the intended free tier.
- Runtime acceptance uses repeated cold standard-runner measurements including build, artifact transfer, planning, matrix execution, and aggregation; the p95 PR critical path must satisfy the 40-minute budget before branch protection is enabled.

The scheduled live matrix may consume substantial total runner time, but it does not extend the pull-request critical path. Its wall-clock time, runner-minutes, job count, artifact bytes, and freshness are all planner-enforced rather than merely observed.

## Failure, Retry, and Quarantine Policy

- Assertion, schema, safety, content, engine, and verifier failures are never automatically retried.
- A recognized transient Winget source/network failure may be retried once inside the same job after source repair. Both attempts remain in evidence and a successful retry is `PASS_AFTER_RETRY`, never a clean `PASS`.
- Unknown failures and exhausted infrastructure retries fail the job; `continue-on-error` is not used for proof lanes.
- Quarantine is scoped to proof level, OS/runner image, and failure fingerprint and requires an issue URL, stable reason code, owner, and expiry date. It continues running observationally, surfaces recovery, remains visible, and is not counted as pass. A head-added quarantine cannot relax the current PR; merge-base policy remains blocking until the quarantine is approved on protected main. Expired quarantine fails validation. `candidate`, `blocked`, `deferred`, and `stale` are likewise never counted as pass.
- A module whose transient retry rate exceeds two of its last ten scheduled attempts is marked flaky, opens/links an issue, and cannot be presented as cleanly verified until it stays within budget. One attempt means one planned module row in a distinct scheduled workflow run for a proof-level/stable runner-lane key such as `windows-standard-hosted`; the exact runner image version is recorded but does not reset history. GitHub `run_attempt` reruns do not add denominator attempts and all reruns remain attached to that scheduled attempt. Any assertion/product failure in any rerun makes the scheduled attempt `FAIL`; otherwise any earlier infrastructure failure, cancellation, timeout, workflow rerun, or in-job transient retry limits a later success to `PASS_AFTER_RETRY`. Only a first-run success with no retry is clean `PASS`.
- Scheduled matrices continue after individual failures so the aggregate shows the complete state. The final aggregator fails when any required result is missing or failed, except a quarantine already approved on protected main may be non-blocking while remaining outside every pass numerator.

## Evidence and Public Claims

Every scenario emits a compact schema-versioned result containing at least:

- workflow run ID/attempt, event/ref, timestamp, and artifact digest;
- engine commit, version, and binary hash;
- module ID, canonical module revision/hash, seed hash, validation metadata hash, and fixture hash;
- proof levels attempted and their individual `PASS`, `PASS_AFTER_RETRY`, `FAIL`, `BLOCKED`, or `TIMEOUT` status;
- runner image version, OS, architecture, package driver/version, declared package ref, and resolved installed version when live;
- phase timings, operation/assertion counts, local comparator outcomes, and failure classification;
- retry, quarantine, manual/lab reason, freshness, and expiry metadata where applicable.

Each workflow/chunk aggregator rejects duplicate, missing, wrong-commit, or schema-incompatible results. An exact-commit aggregate includes only rows with that engine hash. The public compatibility ledger may contain latest per-module rows from different protected-main proof identities, but never combines them into an exact-commit numerator; every row displays its tested engine/module/validation/fixture/seed/comparator/package-policy/proof-mode identity and immediately becomes stale when any component changes. For every proof dimension the ledger publishes both `passed / eligible` and `eligible / catalog`; candidate, blocked, deferred, stale, quarantined, retry-pass, lab/manual, not-applicable, and missing rows stay explicit and never shrink a denominator. Config proof additionally reports required scenario count so schema-v2 alternatives and migration edges cannot disappear behind a module count.

PR artifacts and job summaries are ephemeral test signals, not durable public verification. Only protected-main scheduled runs and releases may publish compatibility status. The trusted publishing job reconstructs the rolling ten-attempt history from 90-day scheduled compact evidence and deploys the latest provenance-bound row ledger to GitHub Pages without executing PR code. Release evidence is attached to the matching GitHub Release only after its bounded exact-commit campaign is complete. The page preserves proof-level labels and every proof-identity component; any identity mismatch or age beyond the seven-day automated-live TTL renders the row `stale`. Manual/lab evidence has its own explicit review date and TTL and never becomes automated proof. Badges link to the trusted published status or exact workflow run and cannot imply that a heterogeneous ledger is an exact-current-commit aggregate.

## Security and Data Handling

- Workflows use minimum permissions (`contents: read`); a separate protected-main publishing job receives only the additional Pages/release permission it needs and never executes pull-request code.
- No `pull_request_target` execution of pull-request code.
- Untrusted pull requests never run on self-hosted machines and receive no secrets.
- Synthetic fixtures use the contained declarative API. Live seeds come only from the merge-base/trusted allowlist and must be deterministic and contain no credentials, tokens, licenses, personal paths, or user data.
- Evidence contains counts, identities, and outcomes, not captured configuration payloads. Published hashes cover only known non-secret deterministic sentinels; app-generated content is compared locally and published as a boolean outcome rather than an unsalted content hash.
- Live installers come from the production package manager/source declared by the module; CI does not upload or redistribute them.

## Rollout

1. Land schema, planner, sidecars, fail-closed test-mode inventory/isolation seam, and harness self-tests without making the new checks required.
2. Repair capture-only modules or explicitly approve genuinely one-way contracts, cover every schema-v2 alternative/migration, then make the all-module synthetic/bundle matrix blocking once the full catalog has non-vacuous results within the runtime budget.
3. Replace the current best-effort Notepad++ pre-install behavior with an engine-driven live canary and make it blocking after repeated green runs.
4. Enable changed-module live PR selection.
5. Enable scheduled hosted-live coverage under the freshness/resource caps, then expose its trusted protected-main/release badge and compatibility feed.
6. Require the stable `Verified Module Matrix` aggregate check in branch protection only after repeated cold-run p95 acceptance and missing-job failure tests pass.

No phase may publish a stronger claim than the evidence implemented in that phase.

## Risks / Trade-offs

- **Catalog-wide sidecars add maintenance:** The schema validator and generated defaults make omissions obvious; module-specific exceptions remain reviewable beside the module.
- **Automatic fixtures can miss app-specific semantics:** They prove engine/module mechanics, while live seeds provide real-path evidence where feasible.
- **Winget and upstream CDNs are flaky:** Fresh VMs, limited parallelism, classified one-time infrastructure retry, and non-blocking nightly continuation preserve signal without hiding failures.
- **Hosted runner images drift:** Evidence records the exact image/app versions; the canary catches image-wide breakage before interpreting broad failures as module regressions.
- **Forty-minute hard cap may expose heavy modules:** Heavy live cases move to nightly/lab rather than making ordinary pull requests wait for hours.
- **The old Sandbox harness can imply stronger coverage than it has:** Documentation and result labels reserve production proof levels for the built-engine harness.
