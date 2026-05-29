## 1. Backend selection seam

- [x] 1.1 Add `SelectBackend(goos string) (driver.Driver, error)` in `internal/driver/select.go` — `windows` → `winget.New()`, default → exported `ErrNoBackend`
- [x] 1.2 Route the driver factory (`commands/verify.go:24` `newDriverFn`) and the `apply.go` / `plan.go` call sites through `SelectBackend(runtime.GOOS)`; preserve the test-injection seam

## 2. Platform-aware ref resolution

- [x] 2.1 Replace `resolveWindowsRef` with `resolveRef` that prefers `App.Refs[runtime.GOOS]` and falls back to the first non-empty ref (`planner.go`, `verify.go`)
- [x] 2.2 Confirm `App.Refs` map already accepts arbitrary OS keys (`manifest/types.go:27`) — no schema change needed

## 3. Dynamic capabilities

- [x] 3.1 `capabilities.go` — populate `os` from `runtime.GOOS` and `drivers` from `SelectBackend` availability instead of literals

## 4. Platform-aware paths

- [x] 4.1 `ProfileDir()` — XDG on Linux, unchanged `Documents\Endstate\Profiles` on Windows (`config/paths.go`)
- [x] 4.2 Add `ExpandEnvVars` dispatch (`%VAR%` Windows / `$VAR` other); route the four `ExpandWindowsEnvVars` callers (`bundle/collect.go`, `modules/matcher.go`, `restore/restore.go`, `apply.go`)

## 5. Contract documentation (PROTECTED — needs explicit go-ahead)

- [x] 5.1 `docs/contracts/cli-json-contract.md` — document `capabilities` `os`/`drivers` as host-dependent (additive)

## 6. Verification

- [x] 6.1 `cd go-engine && go test ./...` green on Windows (no regressions)
- [x] 6.2 Regression tests: `capabilities` → `os=windows` + `drivers=["winget"]`; `resolveRef` Windows selection unchanged (incl. multi-ref fallback); `ProfileDir` unchanged on Windows
- [x] 6.3 `GOOS=linux go build ./...` succeeds; tests for Linux ref/path/capabilities behavior
- [x] 6.4 `npm run openspec:validate` passes
