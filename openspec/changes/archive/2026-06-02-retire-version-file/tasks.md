> No engine behavior change (nothing reads `VERSION`). Verify: `cd go-engine && go test ./...`
> (Linux) + `GOOS=windows go build ./...` + `go vet`; `npm run openspec:validate` (strict) +
> `npx openspec validate retire-version-file --strict`; run `scripts/version_format.ps1` on WSL and
> confirm it validates `SCHEMA_VERSION` (and fails on a malformed value). Closes #54.

## 1. Spec

- [x] 1.1 `specs/version-envelope-injection/spec.md` delta: REMOVED "VERSION file is the source of
      truth for cliVersion" + "Schema major bump forces CLI major bump"; ADDED "Release-please
      manifest is the source of truth for cliVersion"; MODIFIED "No hardcoded version strings in
      envelope construction" (trace cliVersion to `config.ReadVersion` → manifest/ldflags)

## 2. Retire the file + tooling

- [x] 2.1 Delete `VERSION`
- [x] 2.2 Delete `scripts/bump-version.ps1`; remove `version:bump` from `package.json`; repoint
      `version:show` to read `.release-please-manifest.json`; keep `version:schema`
- [x] 2.3 Delete the stray tracked `openspec/specs/version-envelope-injection.bak/` directory

## 3. Fix the pre-push hook

- [x] 3.1 Add `scripts/version_format.ps1` validating `SCHEMA_VERSION` format (`^\d+\.\d+$`),
      exit non-zero on mismatch
- [x] 3.2 `lefthook.yml`: `version-format` → `pwsh -NoProfile -File scripts/version_format.ps1`
      (no inline `$var`, so the POSIX shell can't blank it on Linux/WSL)

## 4. Docs / comments

- [x] 4.1 `docs/SEMVER_SYSTEM.md`: add a current-state note (release-please manifest + ldflags own
      the CLI version; `VERSION` and `bump-version.ps1` retired; `SCHEMA_VERSION` manual)
- [x] 4.2 `go-engine/internal/commands/doctor.go`: fix the stale `checkEngineVersion` comment
      ("reads … from the VERSION file" → from the release-please manifest via `config.ReadVersion`)

## 5. Verification

- [x] 5.1 `cd go-engine && go test ./...` green on Linux
- [x] 5.2 `GOOS=windows go build ./...` + `go vet ./...` clean
- [x] 5.3 `npm run openspec:validate` (strict) + `npx openspec validate retire-version-file --strict`
- [x] 5.4 `pwsh -NoProfile -File scripts/version_format.ps1` validates on WSL; malformed
      `SCHEMA_VERSION` → non-zero exit (the case the old inline hook silently passed)
