## ADDED Requirements

### Requirement: Complete Validation Classification

The system SHALL maintain tracked, schema-versioned validation metadata for every production module and SHALL fail matrix planning when any module is missing, duplicated, stale, invalid, or unclassified.

#### Scenario: Complete catalog is planned

- **WHEN** the validation matrix is generated for a commit
- **THEN** exactly one validation record SHALL resolve for every `modules/apps/<id>/module.jsonc`
- **AND** every record SHALL declare one or more synthetic scenarios
- **AND** every scenario kind SHALL be one of `config-roundtrip-v1`, `config-generation-v2`, `config-migration-v2`, `capture-contract`, `restore-contract`, or `install-contract`
- **AND** scenario kinds SHALL remain distinct from emitted proof-level names
- **AND** every record SHALL declare a live mode
- **AND** the aggregate SHALL report the exact catalog denominator

#### Scenario: Symmetric schema-v1 module classification

- **WHEN** a schema-v1 module declares capture targets with executable matching restoration contracts
- **THEN** it SHALL declare a `config-roundtrip-v1` scenario
- **AND** it SHALL declare non-zero minimum capture, payload/provenance, rewritten-restore, content, rebuild, verify, and revert assertions

#### Scenario: Schema-v2 module classification

- **WHEN** a schema-v2 module declares selectable generation/fingerprint alternatives or migration edges
- **THEN** its validation record SHALL declare a `config-generation-v2` scenario for every alternative and a `config-migration-v2` scenario for every migration edge
- **AND** every scenario SHALL select either literal capture/instance IDs or deterministic derivation from its fixture and a detector declared by the production module
- **AND** every executed scenario SHALL assert the exact emitted capture ID, config-set/instance identity, generation/fingerprint, validation, and migration outcome where applicable
- **AND** every target generation SHALL declare and execute production generation validation
- **AND** migration scenarios SHALL prove both the declared edge validation and target-generation validation
- **AND** a top-level app `verify` assertion SHALL be required only when the production module declares one

#### Scenario: Capture target lacks restoration contract

- **WHEN** a module declares a capture target without an executable restoration contract
- **THEN** it SHALL be blocked from `config-roundtrip-v1` and `config-roundtrip-v2`
- **AND** it SHALL remain visible in the config-eligible denominator pending repair or explicit approval as a weaker one-way contract
- **AND** it SHALL NOT be relabeled install-only, quarantined out of the denominator, or counted as config verified
- **AND** an explicitly reviewed one-way exception SHALL declare `capture-contract` with non-zero targeted capture, exact payload/provenance/content assertions and SHALL emit only `engine-contract`
- **AND** its review metadata SHALL record the `approved-one-way` decision, a stable reason code, nonblank reviewer, non-future strict review date, and nonblank evidence

#### Scenario: Restore-only contract is approved

- **WHEN** a module has a genuine reviewed restoration contract without a corresponding capture contract
- **THEN** it SHALL declare `restore-contract` with non-zero restore/content/nested-summary/revert assertions and verification where the module declares it
- **AND** it SHALL emit only `engine-contract`
- **AND** it SHALL remain outside every config-roundtrip numerator while denominator accounting shows the exclusion
- **AND** its review metadata SHALL record the `approved-one-way` decision, a stable reason code, nonblank reviewer, non-future strict review date, and nonblank evidence

#### Scenario: Intentional install-only module classification

- **WHEN** a module intentionally declares no capture and no restore operations
- **THEN** it SHALL declare an `install-contract` scenario
- **AND** it SHALL resolve at least one nonblank Winget, Chocolatey, executable, uninstall-display-name, or path-exists app reference and execute at least one production verifier
- **AND** its result SHALL NOT claim `config-roundtrip`

#### Scenario: Pull request changes eligibility metadata

- **WHEN** a pull request changes a module from a stronger automated mode to a weaker, manual, lab, candidate, blocked, not-applicable, or quarantined mode
- **THEN** required validation selection SHALL use the union of merge-base and head classifications
- **AND** the pull request SHALL NOT evade the previously required automated lane by changing its own metadata
- **AND** head metadata SHALL NOT authorize a new package reference, installer argument, seed, comparator, timeout, or proof mode for live execution

#### Scenario: Candidate baseline remains diagnostic until trusted hosted success

- **WHEN** a candidate declares a proposed live configuration baseline
- **THEN** its policy SHALL use a production Winget install reference, a safe module-relative SHA-256-bound seed, the closed built-in `exact-bytes` comparator enum, live configuration proof mode, bounded timeouts, and no comparator artifact hash
- **AND** normal pull-request, scheduled, dispatch, hosted-denominator, and verified-count planning SHALL exclude the candidate
- **AND** candidate and local runs SHALL remain non-mutating, non-proof, and public-ineligible
- **AND** Task 9 candidate metadata alone SHALL NOT authorize execution, mutation, or proof minting
- **AND** only a later external trusted exact-ref hosted authority, rather than caller-supplied in-process catalog state, MAY mint mutation eligibility
- **AND** only a successful fresh trusted GitHub-hosted baseline MAY support a later trusted metadata change from `candidate` to `hosted`

### Requirement: Causal Task 9 Evidence Boundary

The system SHALL require a fail-closed causal evidence boundary before any future trusted hosted authority can treat a live run as eligible for proof.

#### Scenario: Process receipt is consumed

- **WHEN** the live runner invokes an engine operation
- **THEN** the immediate unexported receipt SHALL bind its typed operation, canonical executable file identity and hash, argv/environment/current-directory digests, process identity/timing/exit result, bounded stdout/stderr bytes and digests, and phase nonce/sequence
- **AND** the evidence decoder SHALL consume that receipt rather than caller-supplied or substitutable raw process bytes

#### Scenario: Raw proof framing is decoded

- **WHEN** an envelope or event record is decoded for proof
- **THEN** its stable audited raw-key inventory, bounded framing, duplicate-key rejection, and EOF/trailing-byte checks SHALL be enforced as proof schema
- **AND** local wire structs MAY use `RawMessage` before decoding official value structs
- **AND** official value structs SHALL NOT silently authorize additive fields
- **AND** the production `RebuildResult` source type SHALL NOT be changed solely to support proof decoding

#### Scenario: Exact nested topology is observed

- **WHEN** an initial apply or rebuild result is evaluated
- **THEN** initial apply SHALL contain plan, apply, and verify beneath its `apply-*` run
- **AND** rebuild SHALL contain plan, apply, restore, and verify beneath its `apply-*` run followed by a distinct `verify-*` segment
- **AND** the outer rebuild envelope run ID SHALL be distinct from both nested runs
- **AND** process success, an outer success envelope, or a merely similar event stream SHALL NOT satisfy the topology

#### Scenario: Config-restore state is inspected

- **WHEN** evidence inspects config-restore state
- **THEN** the inspector SHALL operate only on an existing root, return immutable records, require no concurrent writer or an exclusive lease, and double-scan/fail on change
- **AND** it SHALL perform no mkdir, lock, recovery, temporary cleanup, or other write
- **AND** it SHALL reject pending, temporary, link, and unknown state

#### Scenario: Pre- and post-state are cross-bound

- **WHEN** a live configuration roundtrip is evaluated
- **THEN** evidence SHALL bind the pinned ZIP identity/digests, mapping targets, config-restore state, backups, generations, relevant logs/journals, and artifact/output roots
- **AND** exactly one transaction SHALL trace to the nested apply run, capture, and config set
- **AND** every output SHALL correspond one-to-one with its event, journal entry, and physical backup
- **AND** revert SHALL bind its exact marker, digest, and bytes
- **AND** convergence SHALL require zero validation-owned persistent or target-state delta rather than whole-host immutability

#### Scenario: Disposable lifecycle is observed

- **WHEN** a later trusted hosted authority runs a live candidate
- **THEN** it SHALL observe clean absence immediately before the install receipt and presence immediately after it
- **AND** it SHALL seed only staged hash-bound bytes on a disposable least-privilege trusted runner, use unique attempt roots and link-safe exact-leaf wipe, and bound cleanup
- **AND** it SHALL retry only after clean absence is re-proven
- **AND** UI, elevation, reboot, and lock requirements SHALL be unsupported and non-retryable
- **AND** neither candidate/local success nor cleanup success SHALL become hosted/public proof without a real fresh trusted hosted success

### Requirement: Catalog-Wide Production-Engine Scenarios

The system SHALL run every production module and every required schema-v2 alternative/migration scenario through the workflow-built `endstate` executable on every pull request using isolated deterministic fixtures and production module definitions.

#### Scenario: Schema-v1 config roundtrip succeeds

- **WHEN** a `config-roundtrip-v1` scenario runs
- **THEN** the built CLI SHALL capture the isolated app and seeded production-module targets into a recovery bundle
- **AND** the bundle SHALL contain exact expected payload, module provenance, and correctly rewritten restore entries
- **AND** `rebuild --from <bundle> --only <app-id> --confirm` SHALL restore exact captured content after every target is mutated or removed
- **AND** SHALL execute production verifiers
- **AND** nested apply, configuration-resolution, restore, and verify summaries SHALL contain zero failures and non-zero required assertions
- **AND** immediate revert SHALL recover the recorded pre-restore state
- **AND** subsequent rebuild followed by one repeat rebuild SHALL recover and converge without content drift or directory nesting

#### Scenario: Schema-v2 generation or migration roundtrip succeeds

- **WHEN** a `config-generation-v2` or `config-migration-v2` scenario runs
- **THEN** targeted capture SHALL produce the expected capture ID, config-set/instance identity, selected generation/fingerprint, and validation result
- **AND** a `config-migration-v2` scenario SHALL traverse and prove its declared source-to-target migration edge
- **AND** the scenario SHALL execute non-zero target-generation validation, plus non-zero edge validation for a migration
- **AND** it SHALL execute and prove a top-level app verifier when the production module declares one and SHALL NOT fabricate a verifier minimum when none is declared
- **AND** rebuild, nested-summary, independent content, immediate revert, recovery, and repeat-rebuild convergence assertions SHALL pass
- **AND** a successful result SHALL emit the `config-roundtrip-v2` proof level

#### Scenario: Install-only engine contract succeeds

- **WHEN** an `install-contract` scenario runs
- **THEN** the built CLI SHALL resolve the production module and expected app reference into its plan
- **AND** SHALL execute its production verifier against an isolated installed-state fixture
- **AND** the evidence SHALL report `engine-contract`
- **AND** the evidence SHALL NOT report `live-install`

#### Scenario: Reviewed capture contract succeeds

- **WHEN** a `capture-contract` scenario runs
- **THEN** the built CLI SHALL perform targeted production-module capture and prove exact selection, payload, provenance, content, and non-zero assertions
- **AND** it SHALL emit only `engine-contract` and SHALL NOT emit a config-roundtrip proof level

#### Scenario: Reviewed restore contract succeeds

- **WHEN** a `restore-contract` scenario runs
- **THEN** the built CLI SHALL perform the production restore operation and prove exact content, nested summaries, declared verification, immediate revert, and non-zero assertions
- **AND** it SHALL emit only `engine-contract` and SHALL NOT emit a config-roundtrip proof level

#### Scenario: Vacuous scenario is rejected

- **WHEN** a scenario executes zero required assertions, skips every declared operation, omits required schema-specific provenance/generation/migration evidence, or observes fewer operations than its declared minima
- **THEN** the scenario SHALL fail
- **AND** it SHALL NOT emit any passing proof level

#### Scenario: Rebuild outer envelope succeeds with nested failures

- **WHEN** rebuild exits zero or returns a success envelope but a nested apply, configuration-resolution, restore, or verify summary reports a failure
- **THEN** the scenario SHALL fail
- **AND** it SHALL preserve the failing nested result in sanitized evidence

#### Scenario: Scenario target escapes isolation

- **WHEN** a resolved file or registry target is outside the scenario's allowlisted temporary roots or disposable registry subtree
- **THEN** the scenario SHALL fail before mutating that target
- **AND** the evidence SHALL identify the unsafe resolved target

#### Scenario: Test-mode inventory drives exact selection

- **WHEN** a synthetic scenario invokes targeted capture
- **THEN** a fail-closed test-mode seam SHALL inject exactly one app inventory record
- **AND** the built CLI SHALL select exactly that app plus the named production module through normal matching
- **AND** test mode SHALL be identified in the envelope and evidence
- **AND** the seam SHALL NOT bypass capture planning, bundle creation, restore, verification, journaling, revert, nested summaries, or event/envelope parsing

#### Scenario: Synthetic isolation preflight runs

- **WHEN** a synthetic scenario resolves module paths or registry keys
- **THEN** APPDATA, LOCALAPPDATA, USERPROFILE, Program Files, Program Files (x86), ProgramData, Public, SystemRoot/Windows, Temp, dynamic instance roots, and HKCU SHALL resolve into declared disposable boundaries
- **AND** unresolved variables, HKLM writes without a disposable adapter, hard-coded host paths, device paths, traversal, and reparse-point escapes SHALL fail before mutation
- **AND** synthetic fixtures SHALL use a contained declarative API with package/network execution disabled
- **AND** an independent before/after guard SHALL fail any write to the original host paths/keys or repository/task roots

#### Scenario: Harness logic differs from production behavior

- **WHEN** capture, bundle path rewriting, generation/migration selection, restore, verification, journaling, or revert behavior is needed
- **THEN** the harness SHALL invoke that behavior through the built CLI
- **AND** SHALL NOT substitute a PowerShell reimplementation for a passing production-engine result

### Requirement: Production-Engine Bundle Validation

The system SHALL resolve every tracked bundle with the workflow-built engine on every pull request through `endstate catalog-plan --bundle <tracked-bundle-path> --json --events jsonl`.

#### Scenario: Bundle contract passes

- **WHEN** a tracked bundle is evaluated
- **THEN** the built engine SHALL strictly parse the bundle and resolve every ordered membership to its canonical `apps.<slug>` module and matching validation sidecar
- **AND** it SHALL emit exactly one non-skipped action per declared membership in declaration order
- **AND** each action SHALL bind the bundle identity/hash, canonical module ID, module revision/schema version, validation hash, and validation scenario count
- **AND** the action count SHALL exactly equal the declared membership count
- **AND** a second invocation with the same separately built binary and inputs SHALL have an identical stable projection
- **AND** the row SHALL earn only `catalog` proof and SHALL appear in aggregate evidence

#### Scenario: Vacuous plan is attempted

- **WHEN** an ordinary manifest plan ignores bundle membership, the engine emits zero actions for a non-empty bundle, or the harness expands membership instead of the engine
- **THEN** the bundle contract SHALL fail
- **AND** process success or a success envelope SHALL NOT count as bundle proof

#### Scenario: Bundle input is not canonical

- **WHEN** a bundle is not a regular immediate child of `<root>/bundles`, escapes through traversal, a link, or a reparse point, has an ID that differs from its filename stem, contains unknown or duplicate fields or a trailing object, or names a non-canonical module reference
- **THEN** the engine SHALL reject it before resolution

#### Scenario: Bundle membership is invalid

- **WHEN** a bundle is empty, repeats a canonical module within the bundle, references a missing module or validation sidecar, or produces an unresolved, skipped, stale, wrong-revision, or schema-incompatible action
- **THEN** the bundle contract SHALL fail
- **AND** the failing membership SHALL remain visible in command output and evidence

#### Scenario: A module appears in different bundles

- **WHEN** the same canonical module intentionally belongs to more than one tracked bundle
- **THEN** each bundle SHALL retain its membership
- **AND** the aggregate SHALL report the cross-bundle reuse without treating it as a within-bundle duplicate

#### Scenario: Module has ambiguous or non-package references

- **WHEN** a resolved module has multiple package references or only executable/path matching metadata
- **THEN** the catalog plan SHALL preserve the module as a module-resolution action
- **AND** SHALL NOT choose a package reference, synthesize an app declaration, or claim installation/verifier proof

#### Scenario: Bundle aggregate is incomplete

- **WHEN** bundle evidence is aggregated
- **THEN** the aggregate SHALL require the complete expected bundle set, total membership count, unique-module count, and cross-bundle reuse report
- **AND** missing, duplicate, partially resolved, wrong-commit, wrong-hash, or incompatible bundle rows SHALL fail aggregation

### Requirement: Live Installed-Application Evidence

The system SHALL validate every hosted-live-eligible module on a fresh standard GitHub-hosted Windows runner by allowing the production engine to install the declared package.

#### Scenario: Live install succeeds

- **WHEN** a `hosted` live module runs from an absent-package state
- **THEN** the workflow SHALL invoke production `endstate apply`
- **AND** the engine's declared package driver SHALL perform the installation
- **AND** the production verifier SHALL pass
- **AND** an independent native package-manager query plus executable/uninstall-registry/version observer SHALL agree that the package is installed
- **AND** evidence SHALL record the package reference and resolved installed version

#### Scenario: Workflow pre-installs the package

- **WHEN** a workflow installs the target package before invoking the engine apply path
- **THEN** the result SHALL NOT claim `live-install`

#### Scenario: Live config roundtrip succeeds

- **WHEN** a hosted module declares live configuration coverage
- **THEN** deterministic non-secret settings SHALL be initialized after an engine-driven install
- **AND** targeted production capture SHALL produce non-zero selected configuration in a recovery bundle
- **AND** the app and every captured target SHALL be removed and independently observed absent through native package, executable/uninstall-registry, and filesystem/registry checks
- **AND** production `rebuild` SHALL reinstall the app, restore the captured configuration, and verify the result
- **AND** nested apply, configuration-resolution, restore, and verify summaries SHALL have zero failures and non-zero relevant assertions
- **AND** the independent package observer SHALL agree with the engine after rebuild
- **AND** known non-secret seeded content SHALL match the captured payload under the declared comparator
- **AND** immediate configuration revert SHALL return settings targets to the wiped pre-restore state while the rebuilt package remains installed
- **AND** subsequent rebuild and repeat-rebuild convergence assertions SHALL pass
- **AND** evidence SHALL report both `live-install` and `live-config-roundtrip`

#### Scenario: Clean destination cannot be established

- **WHEN** uninstall or target cleanup cannot independently prove the package and every required destination absent before rebuild
- **THEN** the result SHALL NOT claim `live-install` from rebuild or `live-config-roundtrip`
- **AND** evidence SHALL identify the failed absence proof

#### Scenario: Engine and independent observer disagree

- **WHEN** the production verifier and independent package/settings observer disagree after install or rebuild
- **THEN** the live scenario SHALL fail
- **AND** neither live proof level SHALL pass

#### Scenario: Application cannot run safely on a hosted runner

- **WHEN** an application requires a Store session, account, license, reboot, interactive UI, hardware, kernel driver, unsupported silent installer, or unsafe privilege
- **THEN** its live mode SHALL be `lab` or `manual` with an explicit reason
- **AND** it SHALL remain visible in aggregate totals
- **AND** it SHALL NOT be counted as a live pass

### Requirement: Bounded CI Validation

The system SHALL keep pull-request, scheduled, dispatch, and release validation bounded through deterministic sharding/chunking, parallel live selection, per-job timeouts, explicit runner-minute/artifact caps, and standard GitHub-hosted runners.

#### Scenario: Ordinary pull request runs

- **WHEN** a pull request targets the default branch
- **THEN** all synthetic module scenarios and all bundle checks SHALL run in balanced shards
- **AND** the synthetic shard timeout SHALL be at most 15 minutes
- **AND** the blocking workflow execution critical path SHALL be at most 40 minutes excluding GitHub queue delay
- **AND** the new matrix work SHALL be capped at 250 worst-case runner-minutes
- **AND** larger paid runners SHALL NOT be used

#### Scenario: Engine-affecting pull request runs

- **WHEN** a pull request changes engine behavior, module loading, capture, restore, verification, config generations, package drivers, the validation harness, or its workflow
- **THEN** the synthetic Notepad++ engine-contract canary SHALL run with a timeout of at most 25 minutes

#### Scenario: Module pull request runs

- **WHEN** a pull request changes one or more modules or validation records
- **THEN** up to three changed modules whose merge-base live mode is `hosted` SHALL run their live jobs using merge-base-controlled package/seed/comparator policy
- **AND** changed live jobs SHALL run in parallel rather than serially
- **AND** any overflow SHALL be reported as deferred/stale and SHALL NOT be represented as current live proof
- **AND** an exact-commit dispatch SHALL be available for the overflow set in deterministic chunks
- **AND** one dispatch invocation SHALL be capped at 64 live jobs, 2,880 worst-case runner-minutes, `max-parallel: 8`, and 100 MiB of uploads
- **AND** a request exceeding one chunk SHALL fail planning and report the remaining chunk indices

#### Scenario: Pull request adds or materially changes live policy

- **WHEN** head metadata introduces or changes a package reference, installer argument, seed, comparator, timeout, or live proof mode
- **THEN** that definition SHALL remain candidate/deferred on the untrusted pull request
- **AND** it SHALL first execute after trusted merge or explicit maintainer approval

#### Scenario: Nightly live matrix runs

- **WHEN** the scheduled live workflow runs
- **THEN** at most 64 modules classified `hosted` SHALL receive independent fresh Windows jobs
- **AND** matrix execution SHALL use `fail-fast: false`
- **AND** live concurrency SHALL be capped at eight jobs
- **AND** scheduled work SHALL be capped at 2,880 worst-case runner-minutes
- **AND** deterministic rotation SHALL attempt every hosted module within seven days when the hosted set exceeds 64
- **AND** every result SHALL remain bound to the engine hash it tested
- **AND** a change to the engine binary/commit, module, validation sidecar, fixture, seed/comparator, package driver/source/ref, or proof mode SHALL immediately stale every mismatched live row until that module reruns
- **AND** the final aggregator SHALL report the complete matrix and fail on required failures

#### Scenario: Release live campaign runs

- **WHEN** a release requires live evidence for its exact engine commit
- **THEN** validation SHALL run in deterministic chunks of at most 64 live jobs, 2,880 worst-case runner-minutes, `max-parallel: 8`, and 100 MiB of uploads each
- **AND** the campaign SHALL be capped at six chunks and 17,280 worst-case runner-minutes
- **AND** the planner SHALL reject a required set that cannot fit the campaign caps
- **AND** a release-wide live numerator SHALL publish only after every required exact-commit chunk completes

#### Scenario: Stable aggregate check runs

- **WHEN** any required matrix child succeeds, fails, skips, cancels, times out, is neutral, or is missing
- **THEN** an `always()` aggregate job named `Verified Module Matrix` SHALL run
- **AND** it SHALL synthesize failure for every missing or non-success required row
- **AND** this stable check name SHALL be the branch-protection gate

### Requirement: Honest Evidence and Aggregation

The system SHALL emit schema-versioned evidence for every attempted scenario and SHALL publish exact, separate proof-level counts without combining different engine commits into one exact-commit aggregate.

#### Scenario: Evidence is emitted

- **WHEN** a scenario completes
- **THEN** its evidence SHALL include workflow run/attempt and event/ref, timestamp, artifact digest, engine commit/version/binary hash, canonical module revision/hash, seed hash, validation hash, fixture hash, attempted proof levels, runner image version/OS, phase timings, assertion counts, and status
- **AND** live evidence SHALL include package driver/version, declared reference, and resolved installed version
- **AND** evidence SHALL contain counts and local comparator outcomes rather than captured configuration payloads
- **AND** published hashes SHALL cover only known non-secret deterministic sentinels

#### Scenario: Aggregate is built

- **WHEN** per-scenario evidence is aggregated
- **THEN** missing, duplicate, wrong-commit-within-the-requested-batch, or schema-incompatible evidence SHALL fail aggregation
- **AND** an exact-commit aggregate SHALL include only evidence with the requested engine hash
- **AND** a public latest-row ledger MAY contain rows from different protected-main engine hashes but SHALL NOT represent them as one exact-commit numerator
- **AND** every proof dimension SHALL report both `passed / eligible` and `eligible / catalog`
- **AND** required scenario counts SHALL expose every schema-v2 alternative and migration edge
- **AND** candidate, blocked, deferred, stale, quarantined, pass-after-retry, lab/manual, not-applicable, and missing rows SHALL remain explicit and SHALL NOT shrink a denominator

#### Scenario: Public claim is rendered

- **WHEN** a job summary, badge, or compatibility page presents validation results
- **THEN** it SHALL preserve the evidence proof-level labels and tested commit identity
- **AND** synthetic results SHALL NOT be worded as proof that all real applications install or run
- **AND** module results SHALL NOT be worded as GUI end-to-end proof

#### Scenario: Compatibility status is published

- **WHEN** compatibility evidence is made durable or drives a public badge
- **THEN** only a protected-main scheduled or release job that does not execute pull-request code SHALL publish it
- **AND** the trusted status SHALL identify the exact workflow run and engine/module/seed/fixture revisions
- **AND** automated live evidence with a mismatched engine binary/commit, module, validation sidecar, fixture, seed/comparator, package driver/source/ref, or proof mode, or evidence older than seven days, SHALL render stale
- **AND** the publisher SHALL reconstruct each module's last ten scheduled attempts from trusted 90-day compact evidence
- **AND** only complete exact-commit release evidence within the bounded campaign SHALL be attached to the matching GitHub Release
- **AND** pull-request artifacts SHALL remain ephemeral test signals rather than public verification

### Requirement: Failure and Quarantine Integrity

The system SHALL distinguish product/assertion failures from recognized transient infrastructure failures and SHALL never hide required failures behind skips or unbounded retries.

#### Scenario: Assertion failure occurs

- **WHEN** schema, safety, engine, content, operation-count, verifier, idempotence, generation, or revert assertions fail
- **THEN** the scenario SHALL fail without automatic retry

#### Scenario: Recognized transient infrastructure failure occurs

- **WHEN** a classified Winget source or network failure occurs before assertions
- **THEN** the harness MAY repair the source and retry once
- **AND** both attempts SHALL remain visible in evidence
- **AND** a successful retry SHALL be reported as `PASS_AFTER_RETRY`, not `PASS`
- **AND** an exhausted retry SHALL fail the job

#### Scenario: Module is quarantined

- **WHEN** a required automated module is quarantined
- **THEN** its metadata SHALL include proof level, OS/runner image, failure fingerprint, issue URL, reason code, owner, and expiry date
- **AND** it SHALL continue running observationally, surface recovery, remain visible, and SHALL NOT count as pass
- **AND** a quarantine introduced by pull-request head SHALL NOT relax merge-base blocking policy without trusted approval
- **AND** an expired quarantine SHALL fail validation

#### Scenario: Module exceeds flake budget

- **WHEN** a module requires infrastructure retry in more than two of its last ten scheduled attempts
- **THEN** it SHALL be marked flaky with an issue reference
- **AND** it SHALL NOT be presented as cleanly verified until its retry rate returns within budget
- **AND** one scheduled attempt SHALL mean one planned module row in one distinct scheduled workflow run for the proof-level/stable-runner-lane key
- **AND** exact runner image versions SHALL be recorded without resetting that stable history key
- **AND** GitHub workflow reruns SHALL remain part of that scheduled attempt rather than adding denominator attempts
- **AND** any assertion/product failure in any rerun SHALL make the scheduled attempt `FAIL`
- **AND** otherwise any earlier infrastructure failure, cancellation, timeout, workflow rerun, or recognized transient retry SHALL limit a later success to `PASS_AFTER_RETRY`
- **AND** only a first-run success without retry SHALL count as clean `PASS`

#### Scenario: Required result is skipped

- **WHEN** a required scenario is skipped, absent, cancelled, neutral, timed out, or marked `continue-on-error`
- **THEN** the aggregate SHALL fail

### Requirement: Cost, Artifact, and Trust Boundaries

The system SHALL use public-repository standard hosted compute and minimize stored artifacts without weakening proof or exposing untrusted code to privileged infrastructure.

#### Scenario: Pull request from a fork runs

- **WHEN** untrusted fork code is validated
- **THEN** it SHALL run under the `pull_request` trust model on standard GitHub-hosted runners
- **AND** repository permissions SHALL be read-only
- **AND** checkout SHALL disable persisted credentials
- **AND** installer subprocesses SHALL receive no GitHub token
- **AND** no secrets or self-hosted runners SHALL be available
- **AND** live package/seed/comparator policy SHALL come from the trusted merge base
- **AND** `pull_request_target` SHALL NOT execute untrusted code
- **AND** third-party Actions SHALL be pinned to immutable commit SHAs

#### Scenario: Validation artifacts are retained

- **WHEN** validation uploads artifacts
- **THEN** the shared engine artifact SHALL have one-day retention
- **AND** each compact evidence record SHALL be at most 64 KiB
- **AND** pull-request compact evidence SHALL have retention of at most seven days
- **AND** scheduled-live compact evidence SHALL have 90-day retention to preserve at least ten scheduled attempts under the seven-day rotation SLA
- **AND** detailed failure diagnostics SHALL have retention of at most three days
- **AND** failure diagnostics SHALL be at most 1 MiB per module and total validation uploads SHALL be at most 100 MiB per workflow run
- **AND** installers and captured configuration payloads SHALL NOT be cached or uploaded

#### Scenario: Repository retained storage is budgeted

- **WHEN** a validation workflow plans or uploads retained artifacts/status assets
- **THEN** validation-owned retained bytes plus in-progress reservations SHALL stay below 1 GiB and preserve at least 25 percent headroom below the included repository/account storage allowance, whichever limit is stricter
- **AND** a serialized budget step SHALL prune expired engine/diagnostic/PR artifacts and compact scheduled history to the verified last ten attempts per module/stable lane before deleting superseded raw evidence
- **AND** optional diagnostics SHALL be omitted before required compact proof
- **AND** planning or upload SHALL fail when required proof cannot fit without exceeding the rolling cap

#### Scenario: Repository owner configures a spending guard

- **WHEN** the module matrix is enabled
- **THEN** documentation SHALL instruct the owner to set an Actions budget that stops usage at zero dollars
- **AND** workflows SHALL reject larger-runner labels that could bypass the standard public-repository compute allowance
