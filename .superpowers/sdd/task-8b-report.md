# Task 8B report — built-engine tracked-bundle matrix

## Status

DONE

## Commit

`feat(validation): add built catalog matrix` (the handoff identifies the final
commit hash).

## Files

- `go-engine/internal/validationharness/catalog_matrix.go`
- `go-engine/internal/validationharness/catalog_matrix_test.go`
- `go-engine/internal/validationharness/catalog_matrix_windows_test.go`
- `go-engine/internal/commands/catalogplan.go`
- `go-engine/cmd/endstate/main.go`
- `openspec/changes/verified-module-matrix/tasks.md`

## Delivered contract

`RunCatalogMatrix` enumerates immediate regular `bundles/*.jsonc` children,
runs one canonical built executable twice per bundle with `ENDSTATE_ROOT` set
to the tested repository, and validates only the `catalog-plan` process
boundary. It uses bounded output and a 60-second child timeout, removes test
mode and credential variables from the child environment, snapshots the
repository and engine before/after, strictly decodes one envelope and one
closed JSONL segment, checks envelope/event run-ID equality, validates the
catalog-only action projection and input bundle byte identity, and rejects
nondeterministic second projections.

Rows record bundle identity, action projection, two child plans, assertion
counts, timings, and only `catalog` proof on success. The deterministic
aggregate requires every discovered row, reports module reuse rather than
rejecting cross-bundle reuse, and grants `catalog` only to a complete pass.
Optional result persistence is atomic and restricted to an existing
`endstate-validation-results` directory.

Task 8A's catalog event emitter now receives the already-created envelope
run ID from the CLI shell. The real binary previously emitted a different
event run ID, which Task 8B correctly rejected; this minimal plumbing makes
the public command satisfy the required identity boundary.

## RED → GREEN evidence

1. RED: `TestRunCatalogMatrixRejectsMissingEngine` did not compile because
   `RunCatalogMatrix` and `CatalogMatrixRequest` were undefined.
   GREEN: it passes after the matrix request/result and validation entrypoint
   were added.
2. RED: the first fresh-built production 12-bundle acceptance produced
   `12 attempted / 0 passed`: Task 8A event run IDs omitted the hostname that
   envelope run IDs include.
   GREEN: envelope run ID plumbing made the same fresh-built acceptance pass.
3. RED: the acceptance required per-row timings and failed because rows had
   no phase timings.
   GREEN: first/second plan timings are now recorded and acceptance passes.
4. RED: `TestValidateCatalogPlanBundleIdentityRejectsForeignHash` did not
   compile because the input-byte identity guard was absent.
   GREEN: it passes after hashing and binding the immediate tracked bundle.
5. RED: the acceptance required non-vacuous per-row assertion counts and
   failed because `AssertionCounts` was absent.
   GREEN: rows now require two catalog plans and an action count equal to
   membership count.

## Cold real-binary acceptance

`go test ./internal/validationharness -run '^TestRunCatalogMatrixFreshBuiltEngineProductionBundles$' -count=1 -v`

- PASS in 19.86 seconds.
- Builds `./cmd/endstate` once, then executes 24 catalog-plan children.
- `12 attempted / 12 passed / 0 failed`.
- `315 memberships / 313 unique modules`.
- Reuse exactly `apps.msi-afterburner` and `apps.powertoys`, each in
  `core-utilities` and `system-utilities`.
- Every row has exactly two plans and only `catalog` proof.

## Verification

- Focused matrix and catalog CLI suite: PASS.
- Full `go test ./internal/validationharness -count=1`: PASS in 138.428s.
- `go vet ./...`: PASS.
- `go test -run '^$' ./...`: PASS.
- `npm run openspec:validate`: PASS, 91 passed / 0 failed.
- `git diff --check`: PASS.

## Concerns

Go emits the pre-existing inaccessible telemetry upload-token warning despite
task-owned Go cache and `GOTELEMETRY=off`; it did not affect any command exit
status. The known protected-Documents/native-registry host baselines were not
weakened or exercised by this read-only catalog matrix.
