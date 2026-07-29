## 1. Default-branch dispatch bootstrap

- [x] 1.1 In a separate main-based branch and linked worktree, add `.github/workflows/ci-efficacy-audit.yml` as an inert `workflow_dispatch`-only stub with no inputs, `permissions: {}`, no checkout, no repository-code execution, and one bounded static job.
- [x] 1.2 Review the bootstrap workflow for forbidden triggers, permissions, credentials, moving action references, repository execution, and host mutation; validate the exact diff, commit it alone, push it, and open a focused bootstrap PR.
- [ ] 1.3 Treat merge of the bootstrap PR as an external blocking gate: after a maintainer lands it on `main`, verify GitHub recognizes the workflow filename and can dispatch the inert default-branch job before spending audit runner time.

## 2. Audit contracts and identities

- [x] 2.1 Add failing tests in `go-engine/internal/validationaudit` for strict candidate-queue and frozen-manifest decoding, including duplicate keys, unknown fields, size limits, exact lowercase commit/tree/digest identities, unique ordered IDs, the 45-candidate ceiling, and the 10/8/6/6 quotas.
- [x] 2.2 Implement the minimal schema-versioned candidate, detector, lane, qualification, and frozen-corpus types plus strict decoding needed to pass the manifest tests without adding a dependency.
- [ ] 2.3 Add failing tests for patch eligibility and identity: only production Go or production module/catalog data may change; test, expectation, audit, workflow, symlink/reparse, traversal, absolute-path, binary, oversized, duplicate, and digest-mismatched patches must be rejected.
- [ ] 2.4 Implement bounded patch inspection and SHA-256/tree binding, using exact repository-relative paths beneath one tracked audit corpus root and stable path-free rejection codes.
- [ ] 2.5 Add failing tests for strict typed attempt evidence, including audit/corpus/commit/tree/patch/lane/runner/toolchain/command/repetition/timing identity, bounded stable result fields, and rejection of missing, foreign, duplicated, oversized, free-form-log, path, token, or environment data.
- [ ] 2.6 Implement the compact evidence types and strict readers/writers with atomic create-new publication and stable path-free error classes.

## 3. Classification, freeze, and decision engine

- [ ] 3.1 Add table-driven failing tests for all qualification states: `control-survivor`, `already-covered`, invalid-before-freeze, incomplete/malformed evidence, timeout, cancellation, and infrastructure failure across every declared legacy lane.
- [ ] 3.2 Implement qualification validation that admits a candidate only when every exact legacy control lane has complete passing evidence and never reads or accepts detector evidence.
- [ ] 3.3 Add failing tests proving deterministic first-survivor selection in each stable category queue, exact quota filling, qualification-evidence binding, insufficient-survivor refusal, substitution/reordering refusal, and whole-version invalidation after freeze.
- [ ] 3.4 Implement the outcome-blind freeze operation and canonical frozen-manifest encoder; require a clean exact reference and emit an independently hashable 30-item manifest without invoking detectors.
- [ ] 3.5 Add failing tests for two identical green baselines per targeted detector, two identical mutant attempts, exact expected failure class and coordinates, and the `correct-new-only-kill`, `wrong-kill`, `survivor`, `infrastructure-failure`, and `flake` classifications.
- [ ] 3.6 Implement detector evidence validation and classification without log scraping or raw workflow status inference.
- [ ] 3.7 Add boundary tests for the mechanical decision table, then implement `proceed` only at at least 27/30 correct kills with all six critical kills and zero wrong kills/flakes/infrastructure; otherwise return the specified `stop-and-repair` or `reject-direction` result and never score an invalid run.

## 4. Isolated executor and CLI

- [ ] 4.1 Add failing hostile-path tests for an audit attempt controller that works only below a fresh registered runner-temp root, refuses dirty/ambiguous references and unsafe entries, applies exactly one digest-bound patch, verifies the mutated tree, bounds command time/output/environment, and cleans only its exact owned attempt directory.
- [ ] 4.2 Implement anonymous-HTTPS acquisition of one exact public commit into a fresh owned checkout, with no checkout action or credential helper, then verify commit/tree identity and prove with tests that `GH_TOKEN`, `GITHUB_TOKEN`, Git credential configuration, and unrelated environment never reach audited child commands.
- [ ] 4.3 Implement digest-bound single-patch application, mutated-tree verification, exact cleanup, bounded stdout/stderr discard, and explicit allowlisted command contracts for the six legacy lanes and approved targeted synthetic detectors.
- [ ] 4.4 Add private `audit-qualify`, `audit-freeze`, `audit-baseline`, `audit-detect`, and `audit-aggregate` subcommands to `go-engine/cmd/endstate-validation`, with CLI tests for exact flags, stable JSON output, refusal of foreign paths/identities, and nonzero fail-closed exits.
- [ ] 4.5 Run `go test ./internal/validationaudit ./cmd/endstate-validation ./internal/validationci -count=1`, then `go vet ./...` from `go-engine`, and preserve the exact successful commands as implementation evidence.

## 5. Restore the reference and prove detector eligibility

- [ ] 5.1 Reproduce the required Windows Go failure, add a focused regression test for semantic inspection of UTF-16LE registry exports, and make the smallest test-helper-only fix in `go-engine/cmd/endstate/main_validation_e2e_windows_test.go` without changing product or validation eligibility.
- [ ] 5.2 Run the affected Windows test and the complete Windows `go vet ./...` and `go test ./...` control locally or on a dedicated PR run; record the exact green commit before candidate qualification.
- [ ] 5.3 Declare only currently working synthetic detectors with stable failure class/coordinate contracts, and run each twice on the unmodified exact reference; exclude any red, missing, or identity-unstable detector before corpus authoring continues.

## 6. Candidate corpus and legacy-only qualification

- [ ] 6.1 Create the versioned `validation/ci-efficacy/v1/candidates.json` skeleton and stable category queues, including the common identity, behavior, realism, criticality, detector, expected failure, touched-path, and patch-digest fields.
- [ ] 6.2 Author and locally apply-check the production module-data queue patches, preserving declared order and changing no test, expectation, audit, or workflow file.
- [ ] 6.3 Author and locally apply-check the engine-lifecycle queue patches under the same eligibility constraints.
- [ ] 6.4 Author and locally apply-check the artifact/config-correctness and critical-safety queue patches under the same eligibility constraints, keeping the complete candidate version at or below 45 items.
- [ ] 6.5 Have an independent reviewer inspect every candidate and patch for realism, non-equivalence, production impact, queue ordering, detector independence, and ineligible changes; correct the corpus only before qualification and commit the reviewed version.
- [ ] 6.6 Replace the branch copy of `.github/workflows/ci-efficacy-audit.yml` with the qualification stage: manual-only, fixed runner families, bounded sharding/concurrency/timeouts, immutable action pins, `permissions: {}`, anonymous exact-commit acquisition, exact legacy commands, no detector invocation, and failure-preserving typed artifact upload.
- [ ] 6.7 Extend `go-engine/internal/validationci/workflow_contract_test.go` to reject forbidden triggers, permissions, credentials, moving references, audited target-app installation, host/repository mutation, detector commands in qualification, unsafe cleanup, unbounded jobs, and evidence omissions; run the targeted workflow-contract tests.
- [ ] 6.8 Before dispatch, document the exact candidate/shard/job count and maximum runner-minute envelope, verify the remote branch head equals the independently reviewed commit, freeze that remote ref until completion, and abort rather than exceed the declared corpus or workflow bounds.
- [ ] 6.9 After independent workflow review, commit and push the exact qualification implementation, dispatch that reviewed frozen branch head only after the default-branch stub gate is open, and inspect every actual legacy-control shard and typed evidence artifact for completeness.
- [ ] 6.10 Run the freeze command solely over the accepted qualification artifacts; if any category cannot fill its quota, stop without detector execution and create a newly reviewed candidate version, otherwise commit the exact canonical 30-item frozen manifest and its qualification/run identity.
- [ ] 6.11 Independently verify the frozen manifest's reference commit/tree, candidate order, quotas, patch digests, qualification evidence identities, detector assignments, expected coordinates, and absence of opened detector results before authorizing the detector stage.

## 7. Official repeated detector audit

- [ ] 7.1 Replace the branch workflow with the detector stage bound to the committed frozen manifest: run every targeted unmodified baseline twice first, gate all mutant jobs on identical green baseline evidence, then run each frozen mutant twice in fresh owned checkouts and upload only bounded typed evidence.
- [ ] 7.2 Extend workflow-contract tests to reject legacy/corpus drift, missing baseline dependencies, fewer than two repetitions, aggregate-only failure matching, raw-log classification, credentials/secrets/write permissions, audited target-app installation, unsafe cleanup, moving pins, and non-manual triggers.
- [ ] 7.3 Run targeted package/CLI/workflow tests plus `go vet ./...` and `go test ./...`; obtain independent code/security review and an independent verifier pass, fix findings with focused tests, and commit and push the exact reviewed detector-stage revision.
- [ ] 7.4 Manually dispatch the exact reviewed detector-stage branch revision, inspect both baseline and mutation repetitions plus artifacts rather than relying on check colors, and refuse scoring on any missing, foreign, flaky, wrong-reason, or infrastructure result.
- [ ] 7.5 Run the strict aggregator over the actual qualification and detector evidence, publish the exact run/workflow/commit/corpus identities and all 30 classifications, and apply the mechanical `proceed`, `stop-and-repair`, or `reject-direction` decision without denominator repair.

## 8. Closeout and honest claim

- [ ] 8.1 Have an independent verifier reproduce a representative candidate from each category in fresh local disposable checkouts, compare its typed result with the hosted artifact, and inspect the actual GitHub run's triggers, permissions, pins, runner labels, commands, attempts, and cleanup behavior.
- [ ] 8.2 Record the result in the Endstate validation decision/handoff notes with the exact evidence links, thresholds, survivors/wrong kills, and explicit exclusions for the globally red matrix, hosted-live lifecycle, installer, GUI, schedule, release, and deployment readiness.
- [ ] 8.3 If the result is `proceed`, propose only the next detector-scoped investment; if `stop-and-repair`, repair and rerun the same frozen corpus; if `reject-direction`, restore the inert workflow stub or remove the operative branch workflow while retaining the contract and evidence.
