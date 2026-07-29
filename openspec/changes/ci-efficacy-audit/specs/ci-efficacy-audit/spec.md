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
