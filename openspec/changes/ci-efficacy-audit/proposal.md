## Why

Endstate has extensive tests and a new verified-module validation pipeline, but neither test count nor a fail-closed red matrix measures whether the new CI catches realistic regressions that the legacy merge gate misses. Before expanding the matrix or finishing more hosted-live machinery, the project needs reproducible differential evidence that the direction materially improves defect detection.

## What Changes

- Add a bounded CI-efficacy audit that applies independently reviewed, realistic broken patches only to disposable checkouts.
- Before the full audit, run one final three-mutation held-out product-code preflight against the exact cross-platform Go gate, using the prior six hosted outcomes only as calibration and typed Go evidence end to end; the corpus spans `capture-contract` and `config-roundtrip-v1` in a closed production-file registry. Exclude catalog-matrix and schema-v2 mutants because Windows Go tests already execute those detectors end to end.
- Pre-screen at most 45 candidate mutations per corpus version through the complete legacy required merge gate, then fill the approved 10/8/6/6 category quotas from predeclared per-category survivor order before running any new validation detector.
- Count a mutation as a new-only kill only when every legacy check passes and the predeclared new detector rejects it with the expected stable failure class.
- Preserve already-covered mutations, correct kills, wrong kills, survivors, invalid/equivalent exclusions, and flakes as compact, schema-versioned evidence.
- Run the frozen corpus twice and invalidate the score if any detector outcome changes between runs.
- Require at least 27 of 30 correct new-only kills, all critical safety mutations killed, zero wrong kills, and zero flakes before recommending further investment in the verified-module matrix.
- Keep the pilot manual, non-required, repository-token-free, target-application-install-free, schedule-free, and non-mutating outside runner-owned temporary directories except for the exact pinned toolchain setup already required by a legacy control lane.
- Limit the first pilot to synthetic detectors that are green on the unmodified reference. Deliberate hosted-live mutation on protected `main` remains outside this pilot.
- Treat a valid held-out preflight as clearing PR #205's efficacy blocker while retaining its ordinary green-check, aggregate/shard, review, and merge criteria; the full 30-mutation moat audit is non-blocking post-merge measurement.

## Capabilities

### New Capabilities

- `ci-efficacy-audit`: Differential mutation corpus governance, legacy-control qualification, detector execution, evidence semantics, repeatability rules, and the go/no-go decision contract.

### Modified Capabilities

None.

## Impact

- **Validation tooling:** Adds an audit manifest, fixed mutation patch corpus, isolated executor, and strict evidence aggregation alongside the existing validation packages.
- **CI:** Adds an inert default-branch dispatch bootstrap followed by a manual audit workflow that uses the same runner families and commands as the legacy merge gate and targeted synthetic validation jobs without becoming a required check.
- **Repository data:** Stores reviewed mutation metadata and patch digests; mutations are never applied to the task worktree or committed product tree.
- **Operations:** Consumes bounded GitHub-hosted runner time for an explicit audit run and publishes compact evidence suitable for independent reproduction.
- **Decision making:** Produces an empirical proceed/repair/reject result for the verified-module CI direction without claiming hosted-live or GUI coverage.
