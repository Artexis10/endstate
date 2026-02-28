## 1. Extract Copy Helper

- [ ] 1.1 Create `Copy-BootstrapDirectory` helper function in `Install-EndstateToPath` that takes a directory name, resolves source path (same priority chain: `$script:EndstateRoot` → `$RepoRootPath` → `Get-RepoRootPath` → `Find-RepoRoot`), handles self-copy detection, does `Remove-Item -Recurse -Force` then `Copy-Item -Recurse -Force`, and returns a hashtable with `FileCount` and `Errors`
- [ ] 1.2 Refactor existing engine/, modules/, payload/, restorers/ copy blocks to use the new helper

## 2. Add Missing Directories

- [ ] 2.1 Add `drivers/` copy using `Copy-BootstrapDirectory`
- [ ] 2.2 Add `verifiers/` copy using `Copy-BootstrapDirectory`

## 3. Copy Statistics and Error Reporting

- [ ] 3.1 Accumulate file counts and errors from all `Copy-BootstrapDirectory` calls
- [ ] 3.2 Display `[SYNC] Copied N files across M directories` summary line before `[SUCCESS]`
- [ ] 3.3 Display any copy errors with file paths and error messages

## 4. Verification

- [ ] 4.1 Run `endstate bootstrap -RepoRoot <repo-path>` and confirm all 6 directories appear under `%LOCALAPPDATA%\Endstate\bin\`
- [ ] 4.2 Run `endstate capabilities --json` from PATH and confirm output matches repo invocation
- [ ] 4.3 Run unit tests (`.\scripts\test-unit.ps1`) to confirm no regressions
