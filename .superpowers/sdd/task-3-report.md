# Task 3 Report: Bounded Scheduled, Dispatch, and Release Planning

## Status

DONE

## Implementation

- Added deterministic trusted-main enumeration of current `hosted` validation records, sorted by module ID and chunked into at most 64 jobs per chunk.
- Reused the approved `LiveRow`, `LivePolicy`, quarantine, canonical-digest, and snapshot helpers. Planned jobs bind module revision, full validation digest, live policy, and canonical quarantines without aliasing catalog inputs.
- Added scheduled planning with explicit optional last-attempt state, failing/stale priority within one attempt bucket, UTC attempt-day ordering, missing-state-as-never-attempted semantics, a 64-job daily cap, and a hard 448-hosted freshness-capacity gate.
- Added exact-commit dispatch planning with exactly one selection form: sorted unique hosted IDs or one deterministic chunk index. Invalid commits, both/neither forms, empty/duplicate/unknown/non-hosted/oversized explicit selections, and negative/out-of-range chunks fail with stable error codes.
- Added structured `DispatchCapacityError` output for oversized explicit dispatch. It preserves `ErrorCode` compatibility through unwrapping and reports every deterministic remaining chunk index without launching any chunk.
- Added exact-commit release planning with deterministic 64-job chunks, a six-chunk/384-hosted cap, and no evidence-reuse or alternate-commit input surface.
- Added stable machine-readable trusted-main sources, row reasons, dispatch-selection values, and validation error codes.
- Kept workflow parsing, Actions YAML, general runner/resource policy, production sidecars, harness code, filesystem/network access, and installers out of scope.

## Files

- `go-engine/internal/validationmatrix/bounded_planner.go`
- `go-engine/internal/validationmatrix/bounded_planner_test.go`
- `.superpowers/sdd/task-3-report.md`

The approved Task 1 schema/loading contract and Task 2 synthetic/PR planner files were not changed.

## TDD Evidence

### Initial RED: missing bounded-planner API

Command:

```powershell
$env:GOCACHE='C:\tmp\endstate-go-build-task3-red'
$env:GOTMPDIR='C:\tmp\endstate-go-tmp-task3-red'
go test -count=1 ./internal/validationmatrix -run 'TestPlanHostedCatalog|TestPlanScheduled|TestPlanDispatch|TestDispatchAndRelease|TestPlanRelease'
```

Exit 1 with the expected missing-production build failure:

```text
FAIL github.com/Artexis10/endstate/go-engine/internal/validationmatrix [build failed]
internal\validationmatrix\bounded_planner_test.go:21:15: undefined: PlanHostedCatalog
internal\validationmatrix\bounded_planner_test.go:25:14: undefined: PlanHostedCatalog
internal\validationmatrix\bounded_planner_test.go:38:56: undefined: ReasonTrustedMainHosted
internal\validationmatrix\bounded_planner_test.go:38:103: undefined: PolicySourceTrustedMain
internal\validationmatrix\bounded_planner_test.go:56:16: undefined: PlanHostedCatalog
internal\validationmatrix\bounded_planner_test.go:80:14: undefined: ScheduledModuleState
internal\validationmatrix\bounded_planner_test.go:91:15: undefined: PlanScheduled
internal\validationmatrix\bounded_planner_test.go:103:17: undefined: ReasonScheduledOldestAttemptDay
internal\validationmatrix\bounded_planner_test.go:105:17: undefined: ReasonScheduledNeverAttempted
internal\validationmatrix\bounded_planner_test.go:376:50: undefined: LiveChunk
```

### Initial GREEN

Command:

```powershell
$env:GOCACHE='C:\tmp\endstate-go-build-task3-green'
$env:GOTMPDIR='C:\tmp\endstate-go-tmp-task3-green'
go test -count=1 ./internal/validationmatrix -run 'TestPlanHostedCatalog|TestPlanScheduled|TestPlanDispatch|TestDispatchAndRelease|TestPlanRelease'
```

Exit 0:

```text
ok github.com/Artexis10/endstate/go-engine/internal/validationmatrix 1.635s
```

### Review regression RED: oversized dispatch remainder reporting

The independent reviewer identified that the binding OpenSpec requires an oversized one-chunk dispatch request to report its remaining deterministic chunk indices. The regression requested structured `DispatchCapacityError` data before production was changed.

Command:

```powershell
$env:GOCACHE='C:\tmp\endstate-go-build-task3-review-red'
$env:GOTMPDIR='C:\tmp\endstate-go-tmp-task3-review-red'
go test -count=1 ./internal/validationmatrix -run '^TestPlanDispatchRejectsInvalidExplicitSelections$'
```

Exit 1:

```text
FAIL github.com/Artexis10/endstate/go-engine/internal/validationmatrix [build failed]
internal\validationmatrix\bounded_planner_test.go:242:21: undefined: DispatchCapacityError
```

### Review regression GREEN

Command:

```powershell
$env:GOCACHE='C:\tmp\endstate-go-build-task3-review-green'
$env:GOTMPDIR='C:\tmp\endstate-go-tmp-task3-review-green'
go test -count=1 ./internal/validationmatrix -run '^TestPlanDispatchRejectsInvalidExplicitSelections$'
```

Exit 0:

```text
ok github.com/Artexis10/endstate/go-engine/internal/validationmatrix 1.488s
```

The regression validates and sorts 129 hosted IDs, retains the stable capacity error code, and asserts remaining chunk indices `[1, 2]`.

## Coverage Added

- Deterministic hosted enumeration and 64/64/2 chunking under reversed catalog insertion.
- Empty hosted catalog produces no jobs or chunks.
- Live job module revision, validation digest, trust policy, and quarantine snapshots do not alias input records.
- Scheduled never-attempted precedence, oldest UTC attempt-day precedence, and failing/stale/module-ID tie-breaks only within one bucket.
- Duplicate known state rejection and unknown/non-hosted state ignoring.
- Simulated seven-day coverage of 448 modules with persistent failing/stale flags and rejection at 449.
- Exact-ID dispatch success plus empty, duplicate, unknown, non-hosted, oversized, both, and neither selection failures.
- Deterministic chunk-index dispatch plus negative and out-of-range failures.
- Missing/malformed 40/64-character engine commit failures; valid 40- and 64-character lowercase hexadecimal identities.
- Release chunk sizes at 0, 64, 65, and 384; rejection at 385.

## Final Verification

Fresh focused bounded-planner tests:

```powershell
$env:GOCACHE='C:\tmp\endstate-go-build-task3-final-focused'
$env:GOTMPDIR='C:\tmp\endstate-go-tmp-task3-final-focused'
go test -count=1 ./internal/validationmatrix -run 'TestPlanHostedCatalog|TestPlanScheduled|TestPlanDispatch|TestDispatchAndRelease|TestPlanRelease'
```

Exit 0:

```text
ok github.com/Artexis10/endstate/go-engine/internal/validationmatrix 1.457s
```

Fresh full validation-matrix package and relevant module tests:

```powershell
$env:GOCACHE='C:\tmp\endstate-go-build-task3-final-package'
$env:GOTMPDIR='C:\tmp\endstate-go-tmp-task3-final-package'
go test -count=1 ./internal/validationmatrix ./internal/modules/...
```

Exit 0:

```text
ok github.com/Artexis10/endstate/go-engine/internal/validationmatrix 2.853s
ok github.com/Artexis10/endstate/go-engine/internal/modules 12.610s
```

Strict OpenSpec validation:

```powershell
npm run openspec:validate
```

Exit 0:

```text
Totals: 91 passed, 0 failed (91 items)
```

Formatting and patch verification:

```powershell
Get-ChildItem go-engine/internal/validationmatrix -Filter '*.go' | ForEach-Object { gofmt -d $_.FullName }
git diff --check
git diff --cached --check
```

All completed with exit 0 and no output.

## Independent Review

The independent reviewer found one Important issue and no Critical or Minor issues: oversized explicit dispatch did not report the binding spec's remaining deterministic chunk indices. A focused RED/GREEN added structured remainder output without changing the stable error-code contract. The same reviewer then verified only that finding and approved the fix.

## Self-Review

- Confirmed current hosted rows are sorted solely by stable module ID and use current trusted-main policy, not PR head authorization.
- Confirmed validation digest/module revision, nested trust hashes, and quarantine slices are snapshotted before caller mutation.
- Confirmed scheduled priority flags cannot cross never-attempted or UTC attempt-day buckets, so failing/stale preference cannot starve older modules.
- Confirmed every selected scheduled row remains required and no run can exceed 64 jobs; 448 exactly fits seven runs and 449 fails before planning.
- Confirmed explicit dispatch validates exact IDs before capacity evaluation, sorts them, never launches an oversized partial plan, and reports the remaining chunk indices.
- Confirmed chunk dispatch selects one and only one current deterministic hosted chunk.
- Confirmed dispatch and release plans carry exactly one required syntactically full engine identity and have no evidence-reuse surface.
- Confirmed release accepts six chunks/384 modules and fails at the seventh chunk/385th module.
- Confirmed all planner functions are pure: no workflow parsing, YAML, resource-policy generalization, filesystem, network, process, or install behavior.
- Confirmed no existing Task 1/2 wire types, functions, error codes, or planner behavior changed.

## Concerns / Deferred Work

None for this checkpoint. Workflow wiring, YAML/action policy, general runner-minute/artifact enforcement, production sidecars, and validation harnesses remain intentionally deferred to their separately scoped tasks.
