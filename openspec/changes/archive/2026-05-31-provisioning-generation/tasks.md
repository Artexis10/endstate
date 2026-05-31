> TDD: write each test RED first, then implement to green. All CI tests are hermetic (no
> real `nix`/`winget`). Host-dependent tests are made host-aware (key fixtures by
> `runtime.GOOS`, per the Phase-1 `nixApp`/`foreignRefApp` pattern). Verify on Linux:
> `cd go-engine && go test ./...`; plus `GOOS=windows go build ./...` + `go vet` clean.

## 0. Gate

- [ ] 0.1 `npm run openspec:validate` (strict) passes for this change before implementing
- [ ] 0.2 **PAUSE for maintainer review of this spec** (Gate A) before any Go is written

## 1. `internal/provision` — record + persistence (new package)

- [ ] 1.1 `provision.go` — `Generation`/`ProvItem` types, `SchemaVersion = "1.0"` const,
      `Capabilities` struct, `CapabilityReporter` + `Rollbacker` interfaces (Rollbacker
      declared only)
- [ ] 1.2 RED tests: `Dir()` resolves under `state.StateDir()` (honors `ENDSTATE_ROOT`,
      never hardcoded); `nextNumber()` on empty dir → 1, with `000003.json` present → 4
- [ ] 1.3 `store.go` + RED tests: `Write(gen)` uses `.tmp`+`os.Rename`, `MarshalIndent`,
      `0644`/`0755`; round-trips; never leaves a `.tmp` on success
- [ ] 1.4 `List()` + RED tests: newest-first, `.tmp`-excluded; missing dir → empty slice,
      no error (mirror `state.ListRunHistory`)
- [ ] 1.5 Guard test: `internal/provision` import set excludes `internal/restore`
      (separation-of-concerns enforced in code)

## 2. Write hook — realizer (nix) path

- [ ] 2.1 RED test (injected `fakeRealizer`, host-aware): a full-success apply that installs
      ≥1 ref writes one generation with `Partial=false`, `Native=ToGeneration`,
      `AddedRefs` = installed refs only, `Backend="nix"`
- [ ] 2.2 RED test: an apply that does not advance the generation writes **no** generation
- [ ] 2.3 RED test: idempotent re-run (all present, nothing to add) writes **no** generation
- [ ] 2.4 Implement at the `runApplyRealizer` success return site; reuse `runID`

## 3. Write hook — driver (winget) path

- [ ] 3.1 RED test (mock driver, host-aware): apply installing ≥1 ref writes a generation
      with `Backend="winget"`, `Native=""`, `AddedRefs` = `status=installed` only
- [ ] 3.2 RED test: partial install (some installed, some failed) → generation written with
      `Partial=true` and only the installed subset in `AddedRefs`
- [ ] 3.3 RED test: all-present / all-failed → **no** generation written
- [ ] 3.4 Implement at the driver-path success return site in `apply.go`

## 4. Capabilities reporting

- [ ] 4.1 RED + impl: nix realizer implements `provision.CapabilityReporter` →
      `{AtomicSet, NativeRollback, Transactional, BatchInstall} = true`
- [ ] 4.2 RED + impl: winget driver implements `provision.CapabilityReporter` → all false
- [ ] 4.3 RED test: discovery by type-assertion works for both backends (no Rollbacker yet)

## 5. `generations` command (read-only, list)

- [ ] 5.1 `internal/commands/generations.go` + RED tests: `RunGenerations` →
      `runGenerationsList` reads `provision.List()`; newest-first; empty list (no error)
      when none; result struct populates `data.generations`
- [ ] 5.2 `cmd/endstate/main.go` (**PROTECTED — needs go-ahead**): add `case "generations"`
      in `dispatch()` + usage line, mirroring `report`/`profile`
- [ ] 5.3 RED test: command is read-only (writes/deletes nothing)

## 6. Windows no-regression

- [ ] 6.1 `TestApply_WindowsWritesWingetGeneration` — driver path on a Windows fixture
      writes a `winget` generation; existing per-item event sequence unchanged
- [ ] 6.2 `GOOS=windows go build ./...` + `go vet ./...` clean; existing winget tests green

## 7. Contract documentation (PROTECTED — needs explicit go-ahead)

- [ ] 7.1 `docs/contracts/cli-json-contract.md` — additive `generations` command row +
      `data.generations` envelope fields

## 8. Verification

- [ ] 8.1 `cd go-engine && go test ./...` green on Linux
- [ ] 8.2 `GOOS=windows go build ./...` + `go vet ./...` clean
- [ ] 8.3 `npm run openspec:validate` (strict) passes
- [ ] 8.4 Real-nix smoke (not CI): `apply` a small manifest (e.g. `ripgrep`) on this WSL
      box → `generations` lists the committed generation; re-run is idempotent (no new
      generation)
