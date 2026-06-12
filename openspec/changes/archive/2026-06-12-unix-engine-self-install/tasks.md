# Tasks — unix-engine-self-install

> Verification: `cd go-engine && go test ./...` + `go vet ./...` +
> `GOOS=windows go build ./...` + `GOOS=darwin go build ./...` +
> `openspec validate --all --strict --no-interactive`.

## Bootstrap platform split

- [x] 1.1 Move shared flags/`BootstrapData`/`copyFile` into `bootstrap.go`; document the
      platform-dependent `ShimPath`/`AddedToPath` semantics in the type doc.
- [x] 1.2 `bootstrap_windows.go` (`//go:build windows`): carry the current behavior byte-identically
      (install to `%LOCALAPPDATA%\Endstate\bin`, `.cmd` shim, `setx` PATH, payload fields).
- [x] 1.3 `bootstrap_unix.go` (`//go:build !windows`): install to
      `${XDG_DATA_HOME:-$HOME/.local/share}/endstate/bin`, copy binary to `lib/endstate`, chmod 0755.
- [x] 1.4 Create/re-point `$HOME/.local/bin/endstate` symlink idempotently (remove existing target
      first; never nest or fail on exists).
- [x] 1.5 No PATH/shell-rc mutation on Unix; `addedToPath` always false; print a one-line PATH hint
      to stderr when `$HOME/.local/bin` is not on PATH (JSON payload shape unchanged).
- [x] 1.6 Self-copy guard: when the resolved running binary == the install target, skip the copy.

## Tests

- [x] 2.1 `bootstrap_unix_test.go` (`//go:build !windows`): redirect HOME/XDG_DATA_HOME to t.TempDir;
      assert binary 0755, symlink target, idempotent re-run, payload fields, stale-file re-point.
- [x] 2.2 `bootstrap_windows_test.go` (`//go:build windows`): redirect LOCALAPPDATA; assert binary +
      shim present, payload fields, idempotent re-run (hermetic — no real `setx`).
- [x] 2.3 Real Linux smoke: build, `HOME=<tmp> XDG_DATA_HOME=<tmp>/share endstate bootstrap`, assert
      layout + symlink + idempotency + installed binary executes + self-copy preserves the binary.

## Release artifacts

- [x] 3.1 `.github/workflows/release.yml`: add `publish-unix-artifacts` (ubuntu-latest) matrix over
      `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`; `CGO_ENABLED=0`; same ldflags.
- [x] 3.2 Emit `endstate-<os>-<arch>` + sibling `.sha256` (lowercase hex); upload to the same
      release; verify the new assets are present. Windows job left byte-identical.

## Docs + spec

- [x] 4.1 `docs/COMPATIBILITY.md`: distribution paragraph (Windows exe + Unix per-platform binaries;
      bootstrap self-install on all three).
- [x] 4.2 OpenSpec delta against `bootstrap-full-sync` (MODIFIED Windows requirements + ADDED Unix
      requirements); `openspec validate --strict` passes.
