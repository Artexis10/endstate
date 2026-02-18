# Tasks: Profile Composition — Includes with Profile Name Resolution and Exclusions

## Implementation Order

1. [x] OpenSpec change artifacts (`.openspec.yaml`, `tasks.md`)
2. [x] `engine/manifest.ps1` — `Normalize-Manifest`: Add `exclude` and `excludeConfigs` defaults
3. [x] `engine/manifest.ps1` — `Resolve-ManifestIncludes`: Profile name resolution + zip extraction
4. [x] `engine/manifest.ps1` — `Read-Manifest`: Apply `exclude`/`excludeConfigs` filtering after merge
5. [x] `engine/apply.ps1` — `Invoke-Apply`: Temp dir cleanup in finally block
6. [x] `tests/unit/ProfileComposition.Tests.ps1` — 9 unit tests
7. [x] Verification: all ProfileComposition tests pass (9/9)
8. [x] Verification: `scripts/test-unit.ps1` — no regressions (all related suites pass: Manifest 26/26, Bundle 32/32, ApplyFromPlan 22/22, ProfileContract pass)
