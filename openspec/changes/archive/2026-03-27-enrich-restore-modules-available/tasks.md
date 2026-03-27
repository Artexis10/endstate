## 1. Type and Helper Changes

- [x] 1.1 Add `RestoreModuleRef` struct with `id` and `displayName` JSON fields to `go-engine/internal/commands/apply.go`
- [x] 1.2 Change `ApplyResult.RestoreModulesAvailable` field type from `[]string` to `[]RestoreModuleRef`
- [x] 1.3 Add `resolveModuleDisplayName` helper function with fallback chain: `DisplayName` field -> short ID (strip `apps.` prefix)

## 2. Wiring Changes

- [x] 2.1 Update the module-matching loop in `RunApply` to build `RestoreModuleRef` objects instead of appending raw ID strings
- [x] 2.2 Replace `config.ResolveRepoRoot()` with `resolveRepoRootFn()` in the catalog-loading block of `RunApply` for testability

## 3. OpenSpec Spec

- [x] 3.1 Create `openspec/specs/restore-modules-display-name/spec.md` defining the enriched shape, resolution order, and invariants

## 4. Tests

- [x] 4.1 Add integration test: `TestRunApply_RestoreModulesAvailable_DisplayNames` — verifies enriched objects with display names from mocked catalog
- [x] 4.2 Add integration test: `TestRunApply_RestoreModulesAvailable_FallbackToShortID` — verifies fallback when `displayName` is empty
- [x] 4.3 Add unit test: `TestResolveModuleDisplayName` — table-driven test covering displayName present, empty, prefix stripping, and non-apps prefix

## 5. Verification

- [x] 5.1 Run `go build ./...` — verify zero compile errors
- [x] 5.2 Run `go test ./internal/commands/` — verify all existing tests pass (no regressions)
- [x] 5.3 Run new tests with `-v` flag — verify all 6 new test cases pass
