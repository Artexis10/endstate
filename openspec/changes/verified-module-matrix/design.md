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

- Claiming that all 357 applications can be installed unattended on GitHub-hosted runners.
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

Public summaries report these dimensions independently. For example, `357/357 modules have current engine scenarios` and `N/M hosted-live modules passed` are valid only when the scenario denominator includes every required schema-v2 alternative/migration and asymmetric modules remain explicitly blocked. `357 apps work` is not a valid claim.

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
    "mode": "hosted", // hosted, candidate, blocked, lab, manual, or not-applicable
    "driver": "winget",
    "ref": "Notepad++.Notepad++",
    "seed": "seed.ps1",
    "timeoutMinutes": 20
  }
}
```

Rules:

- `synthetic.scenarios` is required and non-empty for every module. Symmetric schema-v1 modules use `config-roundtrip-v1`; schema-v2 modules declare a scenario for every selectable generation/fingerprint and every migration edge; modules intentionally containing no capture/restore entries use `install-contract`.
- A capture-bearing module cannot earn config-roundtrip proof unless every captured target has an executable restoration contract with non-zero restore and revert assertions. The current capture-only set is blocked and explicitly repaired during rollout; it cannot be relabeled install-only, waived, or removed from the denominator to manufacture `357/357`.
- An intentionally one-way module may receive a `capture-contract` or `restore-contract` scenario only after review. A capture contract must prove targeted production-engine capture, exact payload/provenance/content, and non-zero capture assertions. A restore contract must prove the production restore operation, exact content, nested summaries, verification where declared, immediate revert, and non-zero restore assertions. Both emit only `engine-contract`; they remain outside the config-roundtrip numerator and denominator accounting shows the exclusion.
- Automatic fixtures derive deterministic, non-secret sentinel content from the production capture/restore definitions. Format-aware fixture builders cover copy, directory copy, merge-json, merge-ini, append, and registry operations.
- A module may select a tracked custom fixture script when automatic synthesis cannot represent a dynamic path or format. Custom fixtures run only inside the same isolated roots and must declare their expected assertions.
- `live.mode: hosted` requires an unattended package reference, a reconciled real seed where configuration is claimed, a successful baseline run, and a standard-runner timeout. Unproven installable entries start as `candidate`; known automation gaps use `blocked`. `candidate`, `blocked`, `lab`, `manual`, and `not-applicable` require a stable reason code and human-readable explanation and never count as verified.
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
6. Inspect nested apply, configuration-resolution, restore, and verify summaries rather than trusting process exit or the outer success envelope; assert exact restored content, expected operation counts, backup/journal creation, and at least one verifier pass with zero nested failures.
7. Invoke the built CLI revert path immediately and assert the recorded pre-restore mutated state is recovered.
8. Invoke rebuild again to recover the captured state, then invoke one further rebuild and assert convergence with no content drift or nesting.

For a `capture-contract` module the harness performs targeted production-engine capture and proves exact selection, payload, provenance, content, and non-zero capture assertions without inventing a restore contract. For a `restore-contract` module it invokes the production restorer from a deterministic bundle and proves exact content, nested summaries, declared verification, immediate revert, and non-zero restore assertions. Both record only `engine-contract`. For an `install-contract` module the harness resolves the production module into a plan, supplies an isolated installed-state fixture at the declared verifier boundary, and invokes the built CLI verifier. It likewise records `engine-contract`, never a config-roundtrip or live-install proof level.

A scenario cannot pass vacuously. Schema-v1 roundtrips must meet their declared minimum capture, payload/provenance, rewritten-restore, content, rebuild, nested-summary, and revert assertions. Schema-v2 scenarios must additionally prove the selected generation/fingerprint, validation, and any declared migration edge. Capture and restore contracts must meet their reviewed non-zero one-way assertion sets, and install contracts must resolve at least one app reference and execute at least one production verifier. Unknown operations, all-skipped operations, malformed JSON/events, success envelopes containing nested failures, or zero expected assertions fail.

## Bundle Validation

Every tracked bundle is resolved with the same built engine on every pull request. The gate asserts that:

- every referenced module exists and has validation metadata;
- resolution is deterministic and contains no unintended duplicates;
- the engine can produce a dry-run plan for the bundle; and
- bundle coverage is included in the aggregate evidence.

This is a production-engine bundle contract check, not a real installation of the entire bundle.

## Live Hosted-Runner Harness

Live jobs run directly on fresh standard `windows-latest` GitHub-hosted VMs. Windows Sandbox is not nested inside the runner.

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
10. Emit evidence and allow the hosted VM to be discarded.

Install-only modules can earn `live-install` without a configuration roundtrip. Modules requiring a Store session, license/account, reboot, unsupported silent installer, interactive UI, hardware, kernel driver, or unsafe privilege remain visible as `lab` or `manual` rather than silently skipped. An engine verifier and the independent observer must agree; either one failing withholds the proof level.

### Pull-request trust boundary

A pull request cannot authorize the installer or seed that its own untrusted code will execute. Live PR selection uses a merge-base-controlled allowlist of package source/driver/ref, installer arguments, seed hash, proof mode, comparator, and timeout. A new or materially changed live definition remains `candidate/deferred` on the PR and first runs after trusted merge or explicit maintainer approval; head metadata cannot self-promote it to `hosted`.

Actions are pinned to immutable commit SHAs, checkout uses `persist-credentials: false`, installer subprocesses receive no GitHub token, and live workflows use `pull_request` rather than `pull_request_target`. This does not make untrusted code trusted; it prevents the validation metadata from expanding the external code executed with the hosted Windows runner's elevated account.

## CI Selection and Scheduling

The matrix planner produces bounded JSON matrices rather than one hand-maintained workflow entry per app.

### Pull requests

- Existing Go tests and vet remain blocking.
- The built engine is uploaded once with one-day retention and reused by all validation jobs. All third-party Actions are pinned to immutable commit SHAs.
- All module synthetic scenarios and all bundle checks run in 8 balanced Windows shards. Shard count is configurable up to 16 without changing module metadata.
- Engine-affecting changes run the live Notepad++ canary.
- Up to three changed modules run their live lane when merge-base-controlled metadata says `hosted`; head metadata may retain or weaken selection but cannot authorize a new installer/seed. A larger bulk change is capped on the PR path; overflow modules are reported as `deferred/stale`, never as verified, and can be exercised for the exact commit through bounded deterministic dispatch chunks after trusted merge.
- Validation-harness, workflow, schema, or matrix-planner changes run the live Notepad++ canary plus deliberately good and deliberately broken synthetic fixtures.
- Pull requests from forks use `pull_request`, read-only repository permissions, `persist-credentials: false`, no secrets, no token in installer subprocess environments, and standard GitHub-hosted runners only.
- One stable aggregate job named `Verified Module Matrix` runs under `always()` and converts every missing, skipped, cancelled, neutral, timed-out, or malformed required row into failure. This—not dynamic child job names—is the branch-protection check.

### Scheduled and release runs

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
