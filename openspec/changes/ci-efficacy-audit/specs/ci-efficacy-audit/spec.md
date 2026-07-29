## ADDED Requirements

### Requirement: Reviewed realistic mutation candidates
The audit SHALL evaluate independently reviewed patches that represent plausible regressions in production Go code or production module/catalog data. It MUST reject candidates limited to tests, expected validation outputs, audit implementation, reason-code mappings, harmless refactors, or changes that do not violate declared behavior.

#### Scenario: Product regression candidate accepted
- **WHEN** a patch changes production behavior and identifies the violated invariant, category, criticality, targeted detector, and expected failure class
- **THEN** the audit admits it to legacy qualification after independent review and patch hashing

#### Scenario: Test-only mutation rejected
- **WHEN** a patch changes only test code or expected validation evidence
- **THEN** the audit rejects it before qualification and records the stable exclusion reason

### Requirement: Legacy-only qualification
The audit SHALL run every pre-existing PR CI lane against each candidate before exposing any new-detector result. A candidate qualifies only when Windows, Ubuntu, and macOS Go vet/test lanes, Windows built-binary integration, and Ubuntu/macOS Nix integration all produce complete passing evidence.

#### Scenario: Candidate survives the legacy gate
- **WHEN** every required legacy control lane passes for one exact candidate patch and reference commit
- **THEN** the audit marks that candidate as a control survivor eligible for deterministic freeze selection

#### Scenario: Legacy lane catches the regression
- **WHEN** any complete legacy control lane rejects a candidate for a product reason
- **THEN** the audit classifies the candidate as `already-covered` and does not include it in the frozen denominator

#### Scenario: Control evidence is incomplete
- **WHEN** a control lane is missing, malformed, canceled, timed out, identity-mismatched, or infrastructure-failed
- **THEN** the audit does not qualify or score the candidate

### Requirement: Outcome-blind frozen corpus
Each qualification corpus version SHALL contain at most 45 independently reviewed candidates divided into stable category queues. The audit SHALL select the first valid legacy-control survivors in each queue to fill exactly ten production module-data, eight engine-lifecycle, six artifact/config-correctness, and six critical-safety slots, and bind those 30 items in a committed frozen manifest before official new-detector execution.

#### Scenario: Deterministic freeze
- **WHEN** qualification evidence contains enough ordered valid survivors to fill every category quota
- **THEN** the freeze operation selects the first eligible survivors in each queue without consulting or executing any new detector

#### Scenario: Qualification version cannot fill quotas
- **WHEN** one qualification version lacks enough valid survivors in any category
- **THEN** detector execution remains forbidden and a newly reviewed candidate version MAY be qualified only while detector outcomes remain unopened

#### Scenario: Frozen item substituted
- **WHEN** a reference commit, tree, item order, patch digest, qualification identity, expected detector, or expected failure class differs from the frozen manifest
- **THEN** detector execution fails closed without scoring any mutation

#### Scenario: Post-freeze exclusion proposed
- **WHEN** an item is alleged to be invalid or equivalent after the corpus is frozen
- **THEN** the current corpus version is invalidated and qualification MUST restart rather than shrinking or replacing the denominator

### Requirement: Isolated non-mutating execution
The audit SHALL apply each exact reviewed patch only to a fresh runner-owned temporary checkout of the frozen reference. It MUST NOT modify the task checkout, persist repository credentials, install an audited target application, change host Git configuration, use secrets, or write outside the exact audit attempt and result roots. Exact pinned toolchain setup already required by a declared legacy control lane MAY run on its disposable hosted runner and MUST be evidence-bound.

#### Scenario: Mutation runs in an owned checkout
- **WHEN** a qualification or detector lane begins
- **THEN** it verifies the reference commit/tree, applies one hash-bound patch beneath `RUNNER_TEMP`, verifies the mutated tree, and runs with bounded environment, output, and time

#### Scenario: Unsafe path or mutation scope rejected
- **WHEN** a patch, checkout, result path, symbolic link/reparse point, cleanup target, or mutation write escapes its registered runner-owned root
- **THEN** the audit fails closed before executing product or validation commands

### Requirement: Green detector baseline
Every targeted new detector SHALL pass twice with identical proof identity on the unmodified frozen reference before any associated mutation can be scored.

#### Scenario: Detector is stable on the reference
- **WHEN** both unmodified detector runs pass and emit identical validated proof identity
- **THEN** mutations assigned to that detector are eligible for official execution

#### Scenario: Detector baseline is red or unstable
- **WHEN** either unmodified run fails, is incomplete, or differs from the other run
- **THEN** the detector and its assigned mutations are ineligible and the audit cannot produce a valid score

### Requirement: Differential correct-kill semantics
A mutation SHALL count as a `correct-new-only-kill` only when every legacy control passed and both fresh mutated detector runs fail with the identical predeclared stable failure class and coordinates. Generic crashes, unrelated failures, aggregate failures, and undeclared timeouts MUST NOT count as correct kills.

#### Scenario: Correct unique regression detection
- **WHEN** the control evidence is fully passing and both detector repetitions reject the mutation with the expected stable class and coordinates
- **THEN** the audit classifies the mutation as `correct-new-only-kill`

#### Scenario: Detector fails for the wrong reason
- **WHEN** the detector rejects a mutation with a different class or coordinates, crashes generically, or fails in setup or aggregation
- **THEN** the audit classifies the mutation as `wrong-kill` or `infrastructure-failure` and awards no kill credit

#### Scenario: Regression survives
- **WHEN** the complete control and both mutated detector repetitions pass
- **THEN** the audit classifies the mutation as `survivor`

### Requirement: Repeatable typed evidence
The audit SHALL emit compact, schema-versioned, typed evidence that binds the audit and corpus identities, reference and mutated trees, patch digest, runner and toolchain identity, exact command contract, timing, exit class, stable failure fields, and repetition. It MUST reject duplicate keys, unknown fields, oversized data, ambiguous JSON, path/token/environment leakage, and free-form process output.

#### Scenario: Complete evidence accepted
- **WHEN** every expected lane emits one bounded record with exact matching identities and allowed stable fields
- **THEN** the aggregator admits the record for classification

#### Scenario: Evidence is missing or foreign
- **WHEN** a record is absent, duplicated, malformed, oversized, from another attempt, corpus, commit, tree, patch, runner contract, or repetition
- **THEN** the aggregate is invalid and reports no efficacy percentage

### Requirement: Mechanical go-no-go decision
The audit SHALL score exactly 30 frozen legacy-control survivors. It SHALL return `proceed` only with at least 27 correct new-only kills, all six critical mutations correctly killed, and zero wrong kills, flakes, or infrastructure failures. It SHALL return `reject-direction` when a complete valid run has fewer than 27 correct kills, and `stop-and-repair` for a critical survivor, wrong kill, flake, or correctable infrastructure failure.

#### Scenario: Pilot proves the direction
- **WHEN** 27 or more mutations are correct kills, all critical mutations are correct kills, and no wrong kill, flake, or infrastructure failure exists
- **THEN** the audit returns `proceed` with the exact denominator and classifications

#### Scenario: Pilot rejects the direction
- **WHEN** a complete valid run has fewer than 27 correct kills
- **THEN** the audit returns `reject-direction` and preserves every survivor and wrong-kill record

#### Scenario: Pilot must stop for repair
- **WHEN** any critical mutation survives or any wrong kill, flake, or infrastructure failure occurs
- **THEN** the audit returns `stop-and-repair` without converting the condition into an exclusion

### Requirement: Manual safe audit workflow
The official pilot workflow SHALL use `workflow_dispatch` only, immutable action pins, `permissions: {}`, anonymous acquisition of the public repository at the exact reviewed commit, fixed runner labels, explicit timeouts, bounded concurrency, and failure-preserving evidence upload. Before branch execution, the same workflow filename MUST exist on the default branch as an inert bootstrap with no permissions, inputs, or repository-code execution. The operative workflow MUST create or persist no repository credential and have no pull-request trigger, push trigger, schedule, audited target-application installation, secret, GitHub write permission, or hosted-live mutation authority. Legacy control toolchain setup MUST use separately reviewed immutable pins and exact predeclared commands.

#### Scenario: Default-branch bootstrap
- **WHEN** the audit workflow is first added to the default branch
- **THEN** it exposes only an inert manual-dispatch stub that cannot execute repository code, qualify candidates, run detectors, or publish a score

#### Scenario: Official audit run
- **WHEN** a maintainer manually dispatches the exact independently reviewed audit branch after the bootstrap exists on the default branch
- **THEN** the workflow runs the exact control and detector contracts and publishes bounded qualification, detector, and aggregate evidence

#### Scenario: Workflow gains ambient authority
- **WHEN** the workflow declares a forbidden trigger, permission, credential, installation, host mutation, moving action reference, or unbounded command
- **THEN** repository workflow-contract tests reject the change

### Requirement: Honest proof boundary
The audit result SHALL describe only the unique regression detection of the evaluated green synthetic detectors. It MUST NOT claim that the full matrix, hosted-live lifecycle, installer, application GUI, Endstate GUI, schedule, release, or deployment path is proven.

#### Scenario: Successful pilot is reported
- **WHEN** the audit returns `proceed`
- **THEN** the public summary identifies the exact detector set, corpus, commit, run, thresholds, survivors, and excluded proof areas without upgrading any other validation claim

### Requirement: Held-out Go-gate preflight
Before the full 30-mutation audit, the audit SHALL support one three-mutation v1 preflight that compares frozen PR #205 verified-module detectors with the contemporaneous Windows, Ubuntu, and macOS Go vet/test gate. The previous six hosted mutations SHALL be calibration-only, and the detector/audit implementation SHALL freeze before three new held-out production-Go mutations are authored. Windows integration and target-application installation MUST be absent from the v1 comparator. Catalog-matrix and schema-v2 mutants MUST be excluded because the Windows Go comparator already executes those detectors end to end.

The closed eligible file registry SHALL contain only: capture — `bundle/capture_bundle.go`, `bundle/collect.go`, `bundle/config_capture.go`, `bundle/create.go`, `bundle/module_snapshot.go`, `bundle/payload_manifest.go`; schema-v1 restore — `restore/append.go`, `restore/backup.go`, `restore/copy.go`, `restore/delete_glob.go`, `restore/merge_ini.go`, `restore/merge_json.go`, `restore/registry_import.go`, `restore/restore.go`, `restore/revert.go`, `restore/target_safety.go`. Every path is relative to `go-engine/internal/`; directory globs are forbidden.

#### Scenario: Held-out corpus remains unopened
- **WHEN** the v1 controller and detector contract have frozen
- **THEN** an independently reviewed corpus of three product regressions spans `capture-contract` and `config-roundtrip-v1` with at least one candidate in each mode, binds expected causal failures without executing a held-out mutation through the detector, and keeps every mutation-operator and violated-invariant fingerprint disjoint from v0

#### Scenario: Product-code mutation scope is enforced
- **WHEN** a v1 patch is admitted
- **THEN** it changes exactly one portable non-test production `.go` file in the closed registry, assigns the matching lifecycle, and declares one semantic fault

#### Scenario: Product reachability and review are bound
- **WHEN** a candidate is decoded
- **THEN** its manifest binds the exact production file, lifecycle, fault description, normal product entrypoint, live-reachability explanation, and immutable independent-review record digest

#### Scenario: Reviewer evaluates product causality
- **WHEN** a candidate is proposed for freeze
- **THEN** independent review establishes that the changed statement is reachable from both the normal non-validation CLI entrypoint and the selected synthetic scenario, is not validation-only adaptation, and does not alter detector result types, reason mapping, or evidence contracts

#### Scenario: Lifecycle is bound to sidecar mode
- **WHEN** a candidate is validated before execution
- **THEN** a capture-registry file requires the loaded target scenario mode `capture-contract`, a schema-v1 restore-registry file requires `config-roundtrip-v1`, and aggregation uses that verified sidecar mode rather than the manifest label

#### Scenario: Circular or shallow candidate is proposed
- **WHEN** a v1 patch changes validation, matrix, audit, workflow, CLI validation, detector, harness, test, fixture, testdata, expectation, golden output, module/catalog data, generated output, binary content, any OS-suffixed or validation-boundary file, Go build constraints, more than one production file, or any path outside the closed registry; or an added line names a candidate, target, scenario, detector, or validation/test-mode authority
- **THEN** the controller rejects it before comparator or detector execution

#### Scenario: Comparator-covered target is proposed
- **WHEN** a candidate targets Notepad++ `default-v1`, kubectl `install-v1`, mGBA `reviewed-capture-v1`, Windows Terminal `generation-preferences-g1-97631ba2d2e5`, ownCloud `generation-preferences-g1-1c4479cb88b9`, `generation-preferences-g2-899536c068d4`, or `migration-preferences-g1-to-g2`, or Studio One `generation-preferences-g1-61e9f6f3c254`
- **THEN** the controller rejects it because Windows Go tests already execute that target end to end

#### Scenario: Comparator reproduces the Go gate
- **WHEN** a held-out mutation is qualified
- **THEN** the exact patch runs against the evaluated PR #205 tree through `go vet ./...` and `go test ./...` on fixed Windows, Ubuntu, and macOS runner families using the same immutable setup action and one exact shared resolved Go patch version, while audit-only tests remain outside the evaluated checkout

#### Scenario: Detector reproduces the PR construction boundary
- **WHEN** an unmodified baseline or held-out detector attempt runs
- **THEN** the controller builds `cmd/endstate` and `cmd/endstate-validation` from the same exact evaluated or mutated checkout, invokes the co-built validation CLI for the target, and validates its typed result without using the controller's linked validation harness as the product oracle

#### Scenario: Mutant validator is replaced by a frozen oracle
- **WHEN** a detector attempt builds only the engine or classifies an in-process frozen-harness result instead of the co-built validation binary
- **THEN** the attempt is invalid infrastructure evidence and receives no efficacy credit

#### Scenario: Proof machinery changes after freeze
- **WHEN** the corpus/dispatch commit differs from the proof-machinery freeze outside the registered v1 corpus root or changes detector, controller, workflow, or command-contract bytes
- **THEN** the controller refuses every comparator and detector attempt before product execution

#### Scenario: Candidate fails before the claimed invariant
- **WHEN** a candidate does not compile, is not parseable, schema-valid, revision-consistent, admitted to its exact target, or fails only at a compile, schema, revision, selection, fixture, admission, aggregate, envelope, or other shallow guard
- **THEN** it is ineligible for correct-kill credit even if that shallow failure was predeclared

#### Scenario: Creditable domain failure is classified
- **WHEN** an admitted detector attempt fails after setup
- **THEN** correct-kill credit is possible only for `artifact_contract` in `capture` or `rebuild`; `content_mismatch` or `event_contract` in `capture`, `rebuild`, `verify`, or `revert`; or `revert_failure` in `revert`

#### Scenario: Fixture or unknown failure is reported
- **WHEN** an attempt reports `unsupported_fixture`, phase `fixture`, or any class/phase pair outside the closed credit table
- **THEN** it is uncreditable regardless of preregistration and cannot be classified as a correct kill

### Requirement: Typed v1 evidence authority
The v1 workflow SHALL delegate acquisition, patch application, bounded command execution, detector invocation, evidence publication, and aggregation to typed Go code. Workflow shell MUST NOT compose or classify JSON. Repetition authority SHALL bind the source commit/tree, mutated tree, patch, runner/toolchain, command contract, and detector contract; a binary digest MAY be diagnostic but MUST NOT define source-authority equality.

#### Scenario: Infrastructure cannot become a kill
- **WHEN** acquisition, setup, tool discovery, process launch, timeout, cancellation, output, evidence, or artifact handling fails
- **THEN** the attempt is infrastructure failure and receives no product-rejection or kill credit

#### Scenario: Repeated causal rejection is creditable
- **WHEN** both fresh detector attempts bind identical source authority and reject the mutation with the exact predeclared class, phase, coordinate, and optional reason
- **THEN** the detector portion is eligible for correct-kill classification even when diagnostic binary hashes differ

### Requirement: Mechanical v1 decision and versioning
V1 SHALL report `meaningful-signal` only with three correct new-only kills spanning both verified `capture-contract` and `config-roundtrip-v1` modes, and zero survivors, wrong kills, flakes, or infrastructure failures. A complete comparator rejection SHALL be `already-covered` and make v1 `insufficient-signal`; incomplete, malformed, or infrastructure evidence SHALL be inconclusive. The earliest created workflow run for the exact reviewed dispatch commit SHALL be authoritative. A second dispatch or any rerun invalidates that experiment version; repairing proof machinery requires a new experiment version and a new held-out corpus.

#### Scenario: V1 proves bounded unique detection
- **WHEN** all three Go comparator lanes pass every mutation and both detector repetitions reject every mutation for its exact expected reason
- **THEN** v1 reports `meaningful-signal` for the two evaluated module scenario modes without a coverage percentage or broader readiness claim

#### Scenario: V1 machinery fails
- **WHEN** the official dispatch has incomplete, malformed, flaky, wrong-reason, survivor, or infrastructure evidence
- **THEN** v1 cannot be repaired or rerun in place, and any new attempt uses a reviewed version and new held-out corpus

#### Scenario: A later run looks better
- **WHEN** the exact dispatch commit has another workflow run or an attempt greater than one
- **THEN** the v1 version is invalid rather than selecting the later result, and the verifier reports the complete run history

#### Scenario: V1 reports meaningful signal
- **WHEN** v1 produces a valid `meaningful-signal` result
- **THEN** it clears only PR #205's efficacy blocker while required checks, aggregate/shard wiring, independent review, and ordinary merge criteria remain mandatory
