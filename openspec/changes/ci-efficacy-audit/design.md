## Context

PR #205 adds catalog, synthetic-shard, Notepad++ engine-contract, and aggregate validation jobs on top of Endstate's legacy Go and integration CI. The repository already contains 312 Go test files and 2,647 test functions, yet it has no mutation framework or empirical defect-detection score. The first PR #205 run also shows why test volume is not proof: 102 of 362 synthetic scenarios pass while 260 expose planner/harness contract gaps, and an unrelated UTF-16LE assertion leaves the required Windows Go job red.

The audit answers one deliberately narrow question: can currently working synthetic validation detectors reject realistic product regressions that every pre-existing CI lane accepts? It is not a coverage percentage, a line-coverage exercise, or a way to make the unfinished matrix appear green.

The initial pilot is stacked on the exact PR #205 revision being evaluated. The paused hosted-live PR #206 worktree and its uncommitted state are outside this change.

## Goals / Non-Goals

**Goals:**

- Produce 30 independently reviewed, realistic, baseline-surviving product regressions without exposing new-detector outcomes before the corpus is frozen.
- Run every pre-existing PR CI lane against each candidate before it can qualify for the frozen corpus.
- Prove a new-only kill only when the unmodified detector is green and the mutated detector rejects both repetitions with the predeclared stable failure class.
- Preserve exact, compact, independently reproducible evidence for every qualification, exclusion, kill, wrong kill, survivor, infrastructure failure, and flake.
- Return an unambiguous proceed, stop-and-repair, or reject-direction decision.
- Keep mutation execution isolated, free of audited target-application installation, repository-token-free, and confined to runner-owned temporary checkouts except for exact pinned toolchain setup required by a legacy control lane.

**Non-Goals:**

- Claim that the full 362-scenario matrix is ready or green.
- Measure generic Go unit-test mutation coverage or line coverage.
- Count changes to tests, fixtures' expected outputs, validation policy, or audit code as product regressions.
- Deliberately run a mutated product binary through the protected-main hosted-live mutation authority.
- Prove GUI, installer UX, licensed application, account, hardware, or scheduled-CI behavior.
- Add a required merge check or permanent scheduled workload before the pilot proves useful.

## Decisions

### 1. Use curated differential mutations instead of a generic mutation engine

Each candidate is a reviewed patch that describes a plausible broken product change and the user-visible or invariant-level behavior it violates. The frozen mix is ten production module-data regressions, eight engine lifecycle regressions, six artifact/config correctness regressions, and six critical safety regressions. Each qualification corpus version contains at most 45 candidates because only legacy-control survivors qualify.

Candidates MUST change production Go code or production module/catalog data. Changes limited to tests, validation expectations, audit tooling, reason-code mappings, or harmless refactors are ineligible.

**Alternative considered:** an automated Go mutation tool. It provides volume but mostly measures existing unit tests, produces equivalent mutants, and does not target the module/lifecycle boundaries the new CI claims to add.

**Alternative considered:** historical bug replay only. Historical cases are preferred where available, but the known corpus is too small and uneven to supply a balanced pilot.

### 2. Separate qualification from detector execution with a committed freeze gate

Candidate metadata and patch digests are declared in four stable category queues and independently reviewed before qualification. Qualification runs only the legacy control lanes; it MUST NOT invoke a new validation detector or expose detector results.

The freeze operation selects the first valid control survivors in each category queue until it fills exactly ten module-data, eight engine-lifecycle, six artifact/config-correctness, and six critical-safety slots. The resulting 30 items are written to a frozen-corpus manifest that binds:

- reference commit and tree;
- candidate ID and category;
- patch SHA-256;
- violated behavior and criticality;
- targeted detector and expected stable failure class;
- qualification evidence identity.

The frozen manifest is committed and independently checked before the official detector run. Detector execution refuses an unfrozen, reordered, substituted, quota-incomplete, or qualification-mismatched corpus. If one qualification version does not contain enough survivors to fill every quota, the project may independently review and qualify a new candidate version only while detector results remain unopened. After freeze, no item can be excluded as equivalent or invalid without invalidating the corpus version and restarting qualification; this prevents denominator repair after results are known.

**Alternative considered:** select baseline survivors and immediately run detectors in one workflow. That is faster but allows outcome-aware selection and weakens the proof.

### 3. Define the legacy control as every pre-existing PR CI lane

Qualification reproduces all CI lanes that existed before the verified-module jobs:

- Windows `go vet ./...` and `go test ./...`;
- Ubuntu `go vet ./...` and `go test ./...`;
- macOS `go vet ./...` and `go test ./...`;
- Windows built-binary integration smoke;
- Ubuntu Nix integration smoke;
- macOS Nix integration smoke.

Commands, runner labels, workflow source commit, timeouts, and exit status are evidence-bound. Missing, canceled, timed-out, or malformed control evidence does not qualify a mutation. A new test introduced by the evaluated branch remains part of the control when its job is not one of the new validation jobs; the audit proves what the actual contemporaneous non-validation gate catches, not a reconstructed historical test list.

The known UTF-16LE assertion defect must be repaired with its smallest restorative test-only fix before qualification. No matrix eligibility or validation contract may be weakened to make the reference green.

### 4. Score targeted green detectors, not the globally red aggregate

Before mutation execution, each targeted detector runs twice against the unmodified frozen reference. Both runs must pass with identical proof identity. A detector that is red, missing, or nondeterministic on the reference is ineligible for the pilot.

Each frozen mutation then runs only its predeclared detector twice on fresh runner-owned checkouts. This isolates the detector's signal even while unrelated matrix rows remain red. It proves the selected detector behavior but does not upgrade the global verified-module claim.

A correct new-only kill requires:

1. all qualification control lanes passed;
2. both unmodified detector baselines passed identically;
3. both mutated detector runs failed;
4. both failures reported the same predeclared stable class and coordinates; and
5. neither failure was an infrastructure, setup, aggregate, generic-crash, or unrelated failure.

Timeout counts only when timeout is the predeclared behavioral failure for that candidate. Otherwise timeout is an infrastructure failure or wrong kill.

### 5. Use a private validation-audit package and the existing validation binary

A new `internal/validationaudit` package owns strict manifest decoding, patch/corpus identity, evidence schemas, classification, and aggregation. The existing `endstate-validation` binary receives private audit subcommands for qualification evidence, detector evidence, and aggregation. Product `endstate` CLI behavior is unchanged.

The audit controller accepts only repository-tracked manifests and patches beneath one fixed audit corpus root. It rejects duplicate JSON keys, unknown fields, path traversal, symbolic links/reparse points, oversized inputs or outputs, mutable patch identities, dirty reference trees, ambiguous commits, and result paths outside a fresh runner-owned result root.

No new dependency is added. Standard Git, Go, PowerShell, existing validation binaries, and pinned GitHub actions are sufficient.

### 6. Apply mutations only to disposable exact-reference checkouts

Every lane creates a fresh descendant of `RUNNER_TEMP`, acquires the frozen reference commit without repository credentials, verifies the reference tree, applies exactly one reviewed patch, verifies the resulting tree identity, and runs with bounded environment and output. The task checkout, source PR worktree, Git configuration, audited target applications, user profile data, and repository state are never modified. Exact pinned Go, Nix, cache, and Homebrew prerequisites already exercised by a declared legacy control lane are allowed on the disposable hosted runner and are evidence-bound; they are not available to detector lanes unless their detector contract requires them.

Temporary paths are exact and validated before cleanup. Cleanup removes only the audit-owned attempt root. No broad `git clean`, recursive wildcard deletion, audited target-application installation, schedule, repository token, or write permission is allowed.

### 7. Store compact typed evidence and classify without log scraping

Per-lane evidence records schema version, audit/corpus identity, reference commit/tree, mutation and patch digest, mutated tree, runner/OS/Go identity, command contract identity, start/end UTC times, duration, exit class, and bounded stable result fields. It does not store repository paths, tokens, environment dumps, arbitrary stdout/stderr, captured user data, or free-form exception text.

The aggregate records each mutation as exactly one of:

- `already-covered`;
- `correct-new-only-kill`;
- `wrong-kill`;
- `survivor`;
- `invalid-before-freeze`;
- `infrastructure-failure`; or
- `flake`.

Raw workflow success/failure is never used as the score. The aggregator validates every expected lane, exact attempt, corpus digest, mutation identity, and stable failure coordinate before classifying.

### 8. Make the decision mechanical

The frozen denominator is exactly 30 baseline-surviving mutations, including exactly six marked critical safety regressions. The result is:

- `proceed` only when at least 27 are correct new-only kills, all six critical mutations are correct kills, wrong kills are zero, flakes are zero, and infrastructure failures are zero;
- `stop-and-repair` when any critical mutation survives, any wrong kill or flake occurs, or an otherwise promising run has a correctable infrastructure failure; or
- `reject-direction` when fewer than 27 mutations are correct new-only kills after a complete valid run.

The audit never reports a percentage for an incomplete or invalid run. Survivors and wrong kills remain first-class evidence and become the next design inputs rather than being converted into exclusions.

### 9. Keep the workflow manual and non-authorizing

GitHub accepts `workflow_dispatch` only when the workflow file exists on the default branch. A first, main-based bootstrap change therefore adds the final audit workflow filename with `workflow_dispatch`, no inputs, no permissions, and one inert job that executes no repository code. The bootstrap cannot qualify, detect, or publish a score.

After the bootstrap is on `main`, the reviewed audit branch replaces the inert job with the fixed audit implementation. A maintainer dispatches that exact branch ref only after independent review. The operative audit workflow uses `workflow_dispatch` only, pinned immutable actions, `permissions: {}`, anonymous HTTPS acquisition of the public repository at the exact reviewed commit, fixed runner labels, explicit job timeouts, bounded concurrency, and failure-preserving evidence uploads. No repository credential is created, persisted, or exposed to audited child commands. It has no schedule, pull-request trigger, push trigger, audited target-application installation, secret, GitHub write permission, or hosted-live mutation authority. Legacy control toolchain setup is allowed only through separately reviewed immutable action pins and exact predeclared commands.

The official pilot result binds one exact reviewed workflow commit and actual GitHub Actions run. Local reproduction is additional evidence, not a substitute for the actual run.

### 10. Use one held-out Go-only preflight before finalizing PR #205

The v0 six-mutation run is retained as calibration evidence, not rescored after
its outcomes are known. V1 freezes the detector, typed audit machinery, and
final operative workflow, then authors three new held-out module-lifecycle
product-code mutations across `capture-contract` and `config-roundtrip-v1`,
with at least one in each authored scenario mode. Held-out operators and
violated invariants must be fingerprinted and disjoint from every v0 calibration
case; changing only the affected application or fault spelling is not held-out
evidence.
Its comparator is the contemporaneous Windows, Ubuntu, and macOS Go vet/test
gate with the same pinned setup action and requested Go line. Notepad++
integration is a separate proof lane and is not represented as missing or
failed comparator evidence.

Each candidate changes exactly one portable production `.go` file from the
closed lifecycle/file registry in `pilot-v1.md`; directory globs are forbidden.
The registry excludes validation-boundary and OS-suffixed files. Tests,
fixtures, testdata,
expected/golden output, modules, bundles, validation/matrix/audit/workflow code,
CLI validation entry points, generated files, binaries, and paths outside that
allowlist are forbidden. The manifest declares one semantic fault, family,
operator fingerprint, violated invariant, target, and expected downstream
causal failure. Ordinary Go vet/test on all three runner families screens the
candidate before any detector result is opened.

Only portable and Windows-specific production files used by the detector build
are eligible. Added lines may not mention candidate, module, bundle, scenario,
or detector identifiers, inspect validation/test-mode state, change build
constraints, or add a validation-specific branch. The fault must be a context-
independent deletion, substitution, or boundary change to existing product
logic.

The manifest also binds lifecycle, exact production file, fault description,
normal product entrypoint, live-reachability explanation, and an immutable
independent-review record digest. The controller enforces the closed
path/lifecycle and record-shape rules. Independent review establishes that the
changed statement is reachable both from a normal non-validation CLI path and
the selected synthetic scenario, is not validation-only adaptation, and does
not alter detector result types, reason mapping, or evidence contracts.

A frozen target-exclusion registry rejects the eight production scenarios that
Windows Go tests already run end to end: Notepad++ default, kubectl install,
mGBA reviewed capture, Windows Terminal generation, ownCloud generation g1/g2
and migration, and Studio One generation. These targets cannot be held out.

Lifecycle claims bind to the loaded production sidecar, not candidate metadata:
capture-registry files require `capture-contract`; schema-v1 restore-registry
files require `config-roundtrip-v1`. Aggregation uses that verified mode and
requires at least one candidate in each. Schema-v2 mutants are excluded because
the Windows Go comparator already runs all five production generation scenarios
and the sole production migration scenario end to end.

Workflow shell does not assemble or classify evidence. One Go controller binds
the evaluated PR tree, frozen proof-machinery commit, later patch-corpus
commit, dispatch commit, mutated tree, patch, one exact shared resolved
toolchain, runner image, command contract, detector contract, repetitions, and
structured outcomes. It rejects any post-freeze change outside the registered
corpus root. An engine binary digest is diagnostic rather than repetition
authority. Setup, tool, timeout, output, evidence, and artifact failures remain
infrastructure and cannot receive kill credit. Candidates must compile and pass
parsing, schema, revision, and detector-admission guards before only a
downstream domain failure can receive credit.

Detector attempts must mirror PR #205 rather than a frozen in-process oracle.
The controller builds both `cmd/endstate` and `cmd/endstate-validation` from the
same mutated evaluated checkout, invokes the co-built validation CLI for the
exact target, and validates its typed result. Detector/harness source and the
CLI command contract remain frozen, while any shared product dependency changed
by the mutant is co-built into both binaries exactly as it is in the actual CI.
Calling the controller's own linked `validationharness` for mutant outcomes is
forbidden.

Kill credit uses a closed class/phase table: `artifact_contract` in `capture`
or `rebuild`; `content_mismatch` or `event_contract` in `capture`, `rebuild`,
`verify`, or `revert`; and `revert_failure` in `revert`. `unsupported_fixture`,
every `fixture` phase, and every class/phase pair outside that table are
categorically uncreditable.

Catalog-planner mutants are excluded because the Windows Go comparator already
builds the mutated engine and runs the same full production catalog matrix as
the proposed detector. Catalog CI can provide focused diagnostics but cannot
demonstrate unique detection against this comparator. Schema-v2 mutants are
also excluded because the comparator runs all six production generation and
migration scenarios end to end.

V1 succeeds only at three correct new-only kills spanning both
`capture-contract` and `config-roundtrip-v1`, with exact causal failure identity
twice and zero survivors, wrong kills, flakes, or infrastructure failures. It is
dispatched once; the earliest run for the exact SHA is authoritative, and any
second dispatch or rerun invalidates the version. A broken harness requires a
new version and held-out corpus. A valid v1 result clears only the efficacy
blocker; PR #205 still requires its green required checks, aggregate/shard
wiring verification, independent review, and merge criteria. The 30-mutation
audit remains a non-blocking post-merge measurement rather than a new merge
delay. A complete comparator rejection is classified `already-covered` and
makes v1 `insufficient-signal`; only missing, malformed, or infrastructure
evidence makes it inconclusive.

## Risks / Trade-offs

- **[Curated mutations can reflect reviewer bias]** → Freeze the taxonomy, declared ordering, rationale, patches, and expected detector classes before detector execution; use historical regressions where available and an independent reviewer for corpus realism.
- **[Legacy qualification is runner-expensive]** → Bound the pilot, shard fixed candidates, fail closed on missing evidence, and avoid expanding beyond 30 frozen survivors until the result justifies it.
- **[The global matrix remains red]** → Run only detectors proven green twice on the reference and keep the claim explicitly detector-scoped.
- **[A detector can fail for the wrong reason]** → Require exact stable failure class and coordinates twice; generic crash, timeout, or aggregate failure is a wrong kill or infrastructure failure.
- **[Equivalent mutations can inflate or destabilize the denominator]** → Resolve equivalence before freeze. Discovery after freeze invalidates and versions the entire corpus instead of silently shrinking it.
- **[The audit implementation could manufacture evidence]** → Use strict typed evidence, immutable action pins, exact commit/tree/patch binding, independent code review, and an independent verifier that reproduces a sample and inspects the actual workflow run.
- **[Cross-platform control results can differ legitimately]** → Bind runner OS and exact control command; any required lane failure means the candidate is already covered or unqualified, never a new-only kill.
- **[A passing synthetic pilot could be overclaimed]** → The aggregate and documentation explicitly deny full-matrix, hosted-live, installer, GUI, and release-readiness claims.
- **[A detector-specific mutant could make the result circular]** → Freeze the detector first, forbid all detector/harness/test changes, require a plausible product defect in one allowlisted production file, and have an independent reviewer approve realism and expected causality without opening detector results.

## Migration Plan

1. Land the inert, permissionless `workflow_dispatch` bootstrap from a main-based task branch so GitHub can address the audit workflow by name. It executes no repository code and changes no required check.
2. Land the audit contract and implementation on an isolated stacked draft branch without changing required checks.
3. Repair only the known UTF-16LE control-test defect and prove each selected detector green on the unmodified reference.
4. Commit and independently review the four ordered candidate queues, bounded to 45 candidates per qualification version.
5. Run legacy-only qualification and publish qualification evidence.
6. Commit the deterministic quota-filled frozen manifest without executing detectors.
7. Independently verify the freeze identity, then manually dispatch the reviewed audit branch and run the official detector audit twice at one exact commit.
8. Publish the aggregate and actual workflow links; choose proceed, stop-and-repair, or reject-direction mechanically.
9. If the audit machinery is unsafe or unhelpful, restore the inert stub or remove the workflow and retain the spec and evidence as the decision record. No product runtime rollback is required because the audit does not alter product behavior or required CI.

## Open Questions

None. Scope, corpus size, critical threshold, overall threshold, repetition count, and hosted-live exclusion were explicitly approved before this design was written.
