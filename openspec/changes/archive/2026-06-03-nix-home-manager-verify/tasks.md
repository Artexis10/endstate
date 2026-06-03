> TDD: write each test RED first (compile failure = valid RED), then implement to green. All tests
> hermetic — no real nix. Override both `newRealizerFn` AND `newDriverFn` in command-level tests.
> Seed generations with `provision.Write` under `t.Setenv("ENDSTATE_ROOT", t.TempDir())`.

## 1. OpenSpec change and spec

- [x] 1.1 `openspec/changes/nix-home-manager-verify/`: `.openspec.yaml`, `proposal.md`, `design.md`, `tasks.md`
- [x] 1.2 `openspec/specs/nix-home-manager-verify/spec.md`: `## ADDED Requirements` with `### Requirement:`
      blocks each containing `#### Scenario:` with WHEN/THEN/AND bullets.

## 2. Reason constant

- [ ] 2.1 `internal/driver/driver.go`: add `ReasonConfigDrift = "config_drift"` alongside `ReasonVersionDrift`.

## 3. Realizer capability interface

- [ ] 3.1 `internal/realizer/realizer.go`: add optional `HomeGenerationReader { ActiveHomeGeneration() int }`
      with a doc-comment in the `Pruner` / `HomeActivator` / `HomeRollbacker` style.

## 4. Nix backend implementation

- [ ] 4.1 RED: compile assertion `var _ realizer.HomeGenerationReader = (*Backend)(nil)` in
      `internal/realizer/nix/home_manager.go` — fails until method is added.
- [ ] 4.2 Implement `(*Backend).ActiveHomeGeneration() int { return b.homeGen() }`.

## 5. Extend fakeRealizer

- [ ] 5.1 `internal/commands/apply_realizer_test.go`: add `activeHomeGen int` field and
      `ActiveHomeGeneration() int` method to `fakeRealizer` (returns `f.activeHomeGen`).

## 6. TDD: command-layer tests (RED then GREEN)

- [ ] 6.1 RED: `TestRunVerifyRealizer_HomeManager_Pass` — declared hm, active==recorded → single
      `home-manager` item with status `pass`; total summary counts include it.
- [ ] 6.2 RED: `TestRunVerifyRealizer_HomeManager_ConfigDrift` — active != recorded → `fail`,
      reason `config_drift`; `Version` and `Expected` set.
- [ ] 6.3 RED: `TestRunVerifyRealizer_HomeManager_Missing` — active==0 → `fail`, reason `missing`.
- [ ] 6.4 RED: `TestRunVerifyRealizer_HomeManager_NoRecordedGen_Missing` — no history at all, active==0
      → `fail missing`.
- [ ] 6.5 RED: `TestRunVerifyRealizer_NoHomeManager_NoHmItem` — manifest with no `homeManager` field →
      no `home-manager` item in results; existing package verify unaffected.
- [ ] 6.6 RED: `TestRunVerifyRealizer_NoHomeGenerationReader_Skips` — realizer does NOT implement
      `HomeGenerationReader` → no `home-manager` item emitted even when manifest declares hm.
- [ ] 6.7 Implement `checkHomeManagerGeneration` helper and hm check in `runVerifyRealizer`.

## 7. Verification

- [ ] 7.1 `cd go-engine && go test ./internal/commands/... ./internal/realizer/...` green.
- [ ] 7.2 `cd go-engine && go test ./...` green.
- [ ] 7.3 `GOOS=windows go build ./... && go vet ./...` clean.
- [ ] 7.4 `npm run openspec:validate` (strict) passes.
