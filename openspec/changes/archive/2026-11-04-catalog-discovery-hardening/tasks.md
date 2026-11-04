# Tasks: catalog-discovery-hardening

## 1. Resolution

- [x] 1.1 Extract `walkUpFor(start, match)` in `internal/config/paths.go` so both walks share
      one implementation and are testable without faking `os.Executable`
- [x] 1.2 Add resolution step 3: nearest ancestor containing a `modules/apps` directory
- [x] 1.3 Update the doc comment to state the full four-step order
- [x] 1.4 Tests: nearest-ancestor wins; start dir counts; no match returns empty; env var wins;
      marker walk beats catalog walk; install layout resolves; a file named `apps` does not

## 2. Bootstrap

- [x] 2.1 `installCatalog(sourceRoot, installDir)` in `internal/commands/bootstrap.go` —
      copies `modules` + `payload`, replace-not-merge, same-path guard, absent source skipped
- [x] 2.2 `copyTree` and `samePath` helpers alongside the existing `copyFile`
- [x] 2.3 `BootstrapData.CatalogInstalled` reports which trees landed
- [x] 2.4 Wire into `RunBootstrap` (Windows), using `flags.RepoRoot` when supplied; skip
      absent source trees and fail on catalog copy errors
- [x] 2.5 Tests: copies both trees; refresh drops removed modules; same-path is
      non-destructive; missing source is not an error; empty source root is a no-op

## 3. Capture warning

- [x] 3.1 Emit `module_catalog_unavailable` when the root is unresolvable; fail capture
      when a resolved catalog cannot load
- [x] 3.2 Keep "catalog wired" distinct from "catalog non-empty" so a wired-but-empty catalog
      does not warn
- [x] 3.3 Tests: warns on unresolvable root; fails on load failure; silent when wired-but-empty;
      silent under `--sanitize`

## 4. Verification

- [x] 4.1 `go build ./...` and `go vet ./...` clean
- [x] 4.2 End-to-end proof: real binary in a synthetic install layout with `ENDSTATE_ROOT`
      unset — `doctor` state-dir goes from `fail`/"cannot resolve repo root" to `pass` with the
      install path
- [x] 4.3 Full suite `go test ./...` — 0 failures
- [x] 4.4 `npm run openspec:validate`

## 5. Follow-ups (not this change)

- [x] 5.1 Inspect the GUI spawn path: `src-tauri/engine-core/src/engine.rs` in
      `endstate-gui` sets `ENDSTATE_ROOT` in `build_bundled_command`. Read-only evidence
      at `ffd500c`; no installed GUI acceptance run is claimed.
- [ ] 5.2 Unix bootstrap catalog installation transfers to `add-linux-product-parity`
      task 12.6, owned by its Linux runtime bundle packet. That packet owns
      `bootstrap_unix.go` and names shared `bootstrap.go` changes. This remains unimplemented here.
- [x] 5.3 The separate shipped restore-scope fix resolves this follow-up.
      `apply.go` calls `scopeRestoreModules` in `restore_module_scope.go`; it uses
      restore-entry provenance and `ConfigModules` fallback, with the `--only` allow-list.
      Existing `restore_module_scope_test.go` and `commands_test.go` cover the boundary.
- [ ] 5.4 Consider embedding the catalog via `go:embed` so a bare binary is self-sufficient.
      `modules/` sits outside `go-engine/`'s module root, so it needs a build step — a product
      decision, not assumed here.

## Archive reconciliation — 2026-11-04

Source inspection at `d76d3e5` confirms root resolution order, catalog refresh,
and capture diagnostics. `bootstrap_windows.go` calls `installCatalog`;
`bootstrap_unix.go` does not. The synced bootstrap requirement therefore names
Windows. Task 5.2 remains with Linux task 12.6 rather than implying Unix support.

The original proposal describes catalog-load failure as a warning and catalog
copy errors as non-fatal. Current shipped code is stricter: an unreadable
resolved catalog fails capture, and a copy error fails Windows bootstrap.
Commit `50952c5` changed the capture behavior; the current regression is
`TestRunCapture_FailsWhenCatalogLoadFails`. Absent source trees still skip
without failure. The delta and task descriptions now match these shipped
boundaries; this documentation batch adds no runtime control.

Tasks 4.1–4.3 retain historical checked results from implementation. This batch
does not repeat the old synthetic-install experiment, Windows build, or full
Go suite. Task 4.4 receives fresh OpenSpec 1.14.0 strict validation.
Task 5.1 closes only the source-inspection question. Task 5.3 closes against
the later shipped restore-scope implementation; it adds no new scope algorithm.

Task 5.4 remains a deferred product decision owned by engine catalog packaging.
Its location stays this archived task until a later OpenSpec change adopts it.
Embedding is not required by this archive or assumed by Linux task 12.6.

The scoped Go recheck passes the 9 selected root-resolution cases in
`internal/config`, all 5 `TestInstallCatalog_*` cases, and all 4 capture catalog
diagnostic cases. These hermetic checks do not repeat the installed-layout E2E.
