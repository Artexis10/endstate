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

## Persistence follow-up

After atomic result persistence, the matrix now revalidates the result path and
resnapshots both repository and engine boundaries before returning success. A
post-persistence reparse/path swap or authority mutation strips all aggregate
and row proof, then atomically rewrites the persisted result as failed.

## Concerns

Go emits the pre-existing inaccessible telemetry upload-token warning despite
task-owned Go cache and `GOTELEMETRY=off`; it did not affect any command exit
status. The known protected-Documents/native-registry host baselines were not
weakened or exercised by this read-only catalog matrix.

## Review fixes

The corrected harness independently joins every emitted action to the strict
validation catalog and exact pinned module/sidecar identities. Failed
catalog-plan envelopes now preserve safe structured failure records on the
expected discovered bundle row. The child environment is allowlisted, result
paths are constrained to the validation-owned temp result boundary and reject
repository/engine overlap, and any aggregate-wide failure strips catalog proof
from every row.

Additional hostile coverage rejects malformed/multiple stdout envelopes;
missing, extra, malformed, and foreign-run events; wrong revision, schema,
validation hash, or scenario count; credential leakage; invalid result paths;
and aggregate proof retention after an authority failure.

### Review-fix RED → GREEN

- RED: action identity, child-environment, and proof-stripping tests failed to
  compile because those authority helpers did not exist. GREEN: focused tests
  pass after exact catalog joins, allowlisting, and proof invalidation.
- RED: a structured nonzero catalog-plan envelope discarded its safe failure
  records. GREEN: decoder and row evidence preserve `moduleId`/`reason`.
- RED: repository/engine result-path rejection test failed to compile.
  GREEN: strict validation-owned result-path checks pass.

### Review-fix verification

- Focused hostile/matrix tests: PASS.
- Fresh-built 12-bundle acceptance: PASS in 21.83s.
- Full `go test ./internal/validationharness -count=1`: PASS in 188.532s.
- `go vet ./...` and `go test -run '^$' ./...` from `go-engine`: PASS.
- `npm run openspec:validate`: PASS, 91 passed / 0 failed.
- `git diff --check`: PASS.

## Final process and persistence follow-up

### Commits

- `86681a0` `test(validation): cover catalog process limits`
- `fe79241` `fix(validation): recheck catalog authority after persistence`
- `00b8724` `test(validation): cover catalog authority boundaries`

### RED → GREEN evidence

- Process exit: the first helper setup was intentionally rejected at the
  envelope-action boundary and was discarded; once it emitted a fully valid
  success envelope and matching JSONL segment, the existing process guard
  returned `execution_failure/catalog-plan/exit` and no decoded result. This
  confirms that a valid protocol response cannot survive a nonzero child exit;
  no production change was required because that behavior already existed.
- Post-persistence authority: `TestRunCatalogMatrixStripsPersistedProofAfterRepositoryMutation`
  drives one valid synthetic row through real `RunCatalogMatrix` control flow.
  Its hook reads the initially creditable persisted result, mutates a copied
  pinned module byte, then proves the returned aggregate, returned row, and
  atomically rewritten persisted result all have empty proof. Removing the
  final recheck makes this regression fail by leaving that observed passed file
  creditable.
- Result-path overlap: repository and engine cases now use existing regular
  directories named `endstate-validation-results` inside the valid temp-owned
  boundary and assert the stable `invalid_result_path` `repository` and
  `engine` coordinates.

### Final verification

- Focused process/persistence/aggregate catalog suite: PASS in 25.506s.
- Fresh-built 12-bundle acceptance: PASS in 29.969s (12/12 rows, 315
  memberships, 313 unique modules, and the two expected reuse entries).
- Full `go test ./internal/validationharness -count=1`: PASS in 141.251s.
- `go vet ./...`: PASS.
- `go test -run '^$' ./...`: PASS.
- `npm run openspec:validate`: PASS, 91 passed / 0 failed.
- `git diff --check`: rerun after this report update before the report commit.

The Go tool still emits the pre-existing inaccessible telemetry upload-token
warning even with `GOTELEMETRY=off`; every verification command above exited
successfully.

## Final whole-task review fixes

### Fix commit

`be6545f` `fix(validation): harden catalog matrix review boundaries`

### RED → GREEN evidence

- RED: duplicate bundle membership returned a bare resolver error. GREEN:
  `apps.foo` now reaches the resolver result, CLI failure data, and failed
  matrix row as `duplicate_membership` without host paths.
- RED: a duplicate top-level `runId`, nested action identity/failure field, or
  JSONL event key was accepted by Go's last-key-wins decoder. GREEN: recursive
  token-walk validation rejects duplicate keys before every envelope, nested
  data, or event typed decode.
- RED: a missing engine could persist a failure result below the requested
  repository before repository/engine overlap safety was established. GREEN:
  no result file is written and no proof is emitted.
- RED: a result path crossing a symlink or Windows junction inside
  `%TEMP%\endstate-validation-results` passed because only the immediate
  parent was checked. GREEN: every root-chain component plus the leaf is
  checked before and after atomic replacement; the portable symlink test runs
  where permitted and the Windows junction regression passes.
- README now inventories `catalog-plan` as a catalog-only, read-only command
  without implying installation or normal manifest composition.

### Final verification

- Focused catalogplan/commands/hostile matrix suite: PASS. The fresh-built
  production matrix passed `12/12` bundles with `315` memberships, `313`
  unique modules, exactly two reuse entries, and two plans per row in 30.38s.
- `go test ./internal/validationharness -count=1`: PASS in 151.512s.
- `go vet ./...`: PASS.
- `go test -run '^$' ./...`: PASS.
- `npm run openspec:validate`: PASS, 91 passed / 0 failed.
- `git diff --check`: PASS before the documentation/report commit.
