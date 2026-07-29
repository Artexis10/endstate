# Task 3 report — production-Go proof boundary

## Scope

Committed proof-machinery change: `fa42b57` (`refactor(validationpilot): enforce production-go proof boundary`).

Converted the v1 proof machinery from the superseded six-case catalog/module
design to exactly three production-Go candidates. The controller now accepts
only one allowlisted `go-engine/internal/` product file per patch, binds each
candidate to lifecycle metadata, target exclusion, a review-record digest, and
the mode loaded from the target's production validation sidecar.

Detector attempts now build both `cmd/endstate` and `cmd/endstate-validation`
from the same evaluated checkout, record both binary SHA-256 values only as
diagnostics, invoke the co-built validation CLI, and decode its typed JSON
result. The controller no longer calls the linked validation harness as the
mutant oracle. Comparator failures classify as `already-covered`; the closed
class/phase table gates correct-kill credit.

## TDD record

1. RED: added production-candidate metadata, target-exclusion, comparator, and
   credit-table tests. Focused `validationpilot` test failed at compile time
   because `Lifecycle`, production-file/review metadata, and production-Go
   lifecycle constants were absent. GREEN: added the closed candidate model,
   registries, classification, and three-case proof helper; focused test passed.
2. RED: added the one registered production-Go patch-file test. Focused
   `validationaudit` test failed because `V1PatchRequest.ProductionFile` was
   absent. GREEN: restricted patch parsing to a single allowlisted production
   Go path; focused test passed.
3. RED: added verified-sidecar-mode aggregation coverage. The focused test
   initially classified mismatched sidecar evidence as a correct kill. GREEN:
   lifecycle/sidecar mismatches now become infrastructure evidence; focused
   test passed.

## Final verification

Executed from `go-engine` with task-owned `GOCACHE` and `GOMODCACHE` under
`C:\tmp` and `GOTELEMETRY=off`:

```text
go test ./internal/validationpilot ./internal/validationaudit ./cmd/endstate-validation-pilot ./internal/validationci -count=1
ok github.com/Artexis10/endstate/go-engine/internal/validationpilot
ok github.com/Artexis10/endstate/go-engine/internal/validationaudit
ok github.com/Artexis10/endstate/go-engine/cmd/endstate-validation-pilot
ok github.com/Artexis10/endstate/go-engine/internal/validationci

go vet ./internal/validationpilot ./internal/validationaudit ./cmd/endstate-validation-pilot ./internal/validationci
exit 0

git diff --check
exit 0
```

The installed Go tool still emitted an access-denied telemetry uploader warning
after successful commands despite `GOTELEMETRY=off`; it did not change their
exit status or verification result.

## Self-review

Reviewed the complete task diff. It is limited to the owned v1 controller,
patch authority, and tests. It does not author corpus candidates, execute a
detector, mutate GitHub configuration, or change the workflow contract.

## Review repair

Committed repair: `23555f8` (`fix(validationpilot): harden external proof evidence`).

Implemented the reviewed production-Go boundary repairs:

- detector invocations now own a fresh `validation-result/result.json` beneath
  the attempt root, pass it through `--result`, and require strict byte and
  semantic agreement with bounded stdout;
- external result decoding rejects duplicate/unknown JSON keys, foreign or
  vacuous result identities, unbounded collections, invalid statuses, and
  free-form failure detail before classification;
- production-Go candidates cannot declare a causal reason because the external
  result has no stable reason field;
- patch scope now rejects added build constraints, test-mode authority, and
  candidate/module/scenario/detector observer dependence while retaining the
  one-file registry; and
- a typed, affirmative per-candidate review record is decoded and byte-hash
  bound to the candidate before attempts.

### Repair RED/GREEN

RED: strict external-result tests did not compile because no decoder existed.
GREEN: added strict decoding and causal-reason rejection; focused tests passed.
The full required package test and vet commands below passed after the repair.

```text
go test ./internal/validationpilot ./internal/validationaudit ./cmd/endstate-validation-pilot ./cmd/endstate-validation ./internal/validationci -count=1
ok validationpilot
ok validationaudit
ok endstate-validation-pilot
ok endstate-validation
ok validationci

go vet ./internal/validationpilot ./internal/validationaudit ./cmd/endstate-validation-pilot ./cmd/endstate-validation ./internal/validationci
exit 0
```

## Second review repair

Corrected the attempt-owned result path to use the prepared child TEMP tree,
bound the canonical `--result` leaf in the detector contract digest, restored
exact stdout/result-file byte equality, and accept bounded diagnostic failure
detail without promoting it into causal evidence. Patch checks now cover the
validation-only observer identifiers named in the re-review, and review records
bind the operator/invariant fingerprints while rejecting unsafe leaves.

Verification repeated with the full required package test and vet commands;
all five packages passed and `git diff --check` exited zero.

## Finalization after `ab04cbb`

### RED/GREEN evidence

RED started with the new raw-output boundary test. The focused package build
failed because `V1ChildResult` had no raw-byte field, proving the controller
could not preserve canonical CLI bytes independently from trimmed command
values. The same focused test source defines the empty-detail failed-result
contract; the preceding implementation accepted that vacuous shape. GREEN adds
`RawValue`, preserves it in the real process runner, compares only it with the
persisted result, and rejects empty failed-result detail. The focused matrix
then passed:

```text
go test ./internal/validationpilot ./internal/validationaudit ./cmd/endstate-validation -count=1
ok github.com/Artexis10/endstate/go-engine/internal/validationpilot
ok github.com/Artexis10/endstate/go-engine/internal/validationaudit
ok github.com/Artexis10/endstate/go-engine/cmd/endstate-validation
```

The final matrix now exercises the actual controller command shape and
attempt-TEMP result leaf; canonical raw stdout versus persisted bytes; missing
and disagreeing result files; the production CLI parser plus harness result
path; strict valid and hostile external results; every named patch observer
authority/build tag/candidate identity plus an ordinary allowed production
edit; and missing, tampered, foreign, fingerprint, negative, duplicate-key,
unknown-key, and linked review records. The observer/review checks were
already present in `ab04cbb`; their new adversarial tests were green without
further production changes.

### Final verification

Executed from `go-engine` with `GOTELEMETRY=off`,
`GOCACHE=C:\tmp\endstate-task3-gocache`, and
`GOMODCACHE=C:\tmp\endstate-task3-gomodcache`:

```text
go test ./internal/validationpilot ./internal/validationaudit ./cmd/endstate-validation-pilot ./cmd/endstate-validation ./internal/validationci -count=1
ok github.com/Artexis10/endstate/go-engine/internal/validationpilot
ok github.com/Artexis10/endstate/go-engine/internal/validationaudit
ok github.com/Artexis10/endstate/go-engine/cmd/endstate-validation-pilot
ok github.com/Artexis10/endstate/go-engine/cmd/endstate-validation
ok github.com/Artexis10/endstate/go-engine/internal/validationci

go vet ./internal/validationpilot ./internal/validationaudit ./cmd/endstate-validation-pilot ./cmd/endstate-validation ./internal/validationci
exit 0

git diff --check
exit 0
```

As in the prior repair, the installed Go tool wrote an access-denied telemetry
uploader warning despite `GOTELEMETRY=off`; both required Go commands exited
zero.
