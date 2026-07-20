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
- [x] 2.4 Wire into `RunBootstrap` (windows), non-fatal, using `flags.RepoRoot` when supplied
- [x] 2.5 Tests: copies both trees; refresh drops removed modules; same-path is
      non-destructive; missing source is not an error; empty source root is a no-op

## 3. Capture warning

- [x] 3.1 Emit `module_catalog_unavailable` when the root is unresolvable or the catalog fails
      to load
- [x] 3.2 Keep "catalog wired" distinct from "catalog non-empty" so a wired-but-empty catalog
      does not warn
- [x] 3.3 Tests: warns on unresolvable root; warns on load failure; silent when wired-but-empty;
      silent under `--sanitize`

## 4. Verification

- [x] 4.1 `go build ./...` and `go vet ./...` clean
- [x] 4.2 End-to-end proof: real binary in a synthetic install layout with `ENDSTATE_ROOT`
      unset — `doctor` state-dir goes from `fail`/"cannot resolve repo root" to `pass` with the
      install path
- [x] 4.3 Full suite `go test ./...` — 0 failures
- [x] 4.4 `npm run openspec:validate`

## 5. Follow-ups (not this change)

- [ ] 5.1 Verify the GUI actually sets `ENDSTATE_ROOT` when spawning the engine. This is cited
      from `openspec/changes/scheduled-drift-check/design.md`, not read from the GUI's
      `engine.rs` (separate repo). If it does not, the bug's blast radius is larger than
      CLI-only.
- [ ] 5.2 Unix bootstrap (`bootstrap_unix.go`) does not install the catalog. Left out because
      the config moat is Windows-locked today, so a Unix install has little to attach — revisit
      with cross-platform config work.
- [ ] 5.3 `restoreModulesAvailable` is unscoped — it answers "which modules could exist for
      these apps" rather than "which settings this profile has", producing ~80% phantom
      settings entries. Fix is to intersect with `mf.ConfigModules`. Separate change; distinct
      root cause from anything here.
- [ ] 5.4 Consider embedding the catalog via `go:embed` so a bare binary is self-sufficient.
      `modules/` sits outside `go-engine/`'s module root, so it needs a build step — a product
      decision, not assumed here.
