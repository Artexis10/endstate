## 1. Restore Engine — Types and Backup

- [ ] 1.1 Create `internal/restore/` package with RestoreAction, RestoreResult, RestoreOptions types
- [ ] 1.2 Implement CreateBackup (copy target to state/backups/<timestamp>/<hash>/)
- [ ] 1.3 Implement ComputeFileHash (SHA256 of file content)
- [ ] 1.4 Implement IsUpToDate (compare source and target hashes)
- [ ] 1.5 Implement environment variable expansion for source/target paths

## 2. Restore Engine — Strategies

- [ ] 2.1 Implement copy strategy: file copy with backup, up-to-date skip
- [ ] 2.2 Implement copy strategy: directory recursive copy with exclude glob matching
- [ ] 2.3 Implement copy strategy: locked file handling (catch sharing violations, warn, continue)
- [ ] 2.4 Implement merge-json strategy: deep merge with sorted keys, 2-space indent
- [ ] 2.5 Implement merge-ini strategy: section-aware parse, merge, format
- [ ] 2.6 Implement append strategy: append source to target, create if not exists

## 3. Restore Engine — Orchestration

- [ ] 3.1 Implement RunRestore orchestrator with strategy dispatch by type field
- [ ] 3.2 Implement Model B source resolution (ExportRoot → ManifestDir fallback)
- [ ] 3.3 Implement sensitive path detection (warn on .ssh, .aws, .gnupg, credentials, etc.)
- [ ] 3.4 Implement optional entry handling (skip missing source without error)
- [ ] 3.5 Implement DryRun mode (report actions without filesystem changes)

## 4. Restore Journal and Revert

- [ ] 4.1 Implement WriteJournal (atomic write to logs/restore-journal-<runId>.json)
- [ ] 4.2 Implement ReadJournal (parse journal file)
- [ ] 4.3 Implement FindLatestJournal (find most recent journal by filename sort)
- [ ] 4.4 Implement RunRevert (process journal entries in reverse order)
- [ ] 4.5 Implement revert actions: restore from backup, delete created files, skip no-ops

## 5. Restore Engine — Tests

- [ ] 5.1 Test copy strategy: file copy with backup, up-to-date skip
- [ ] 5.2 Test copy strategy: directory copy with exclude globs
- [ ] 5.3 Test merge-json: deep merge objects, array replace, scalar overwrite
- [ ] 5.4 Test merge-ini: section merge, key preservation
- [ ] 5.5 Test append: new file, existing file
- [ ] 5.6 Test journal write/read round-trip
- [ ] 5.7 Test revert reverse order processing
- [ ] 5.8 Test optional entry with missing source
- [ ] 5.9 Test sensitive path detection

## 6. Config Modules

- [ ] 6.1 Create `internal/modules/` package with Module, MatchCriteria, RestoreDef, CaptureDef types
- [ ] 6.2 Implement LoadCatalog (scan modules/apps/*/module.jsonc, parse JSONC, validate)
- [ ] 6.3 Implement MatchModulesForApps (match captured apps to modules by winget ID and pathExists)
- [ ] 6.4 Implement ExpandConfigModules (inject module restore/verify entries into manifest)
- [ ] 6.5 Test catalog loading with fixture modules
- [ ] 6.6 Test module matching (winget ID match, no match)
- [ ] 6.7 Test configModules expansion

## 7. Bundle Zip

- [ ] 7.1 Create `internal/bundle/` package
- [ ] 7.2 Implement CollectConfigFiles (copy from system paths to staging dir)
- [ ] 7.3 Implement CreateBundle (zip with manifest, metadata, configs/ — atomic write)
- [ ] 7.4 Implement path rewriting (./payload/apps/<id>/ → ./configs/<module-id>/)
- [ ] 7.5 Implement ExtractBundle (unzip to temp dir, return manifest path)
- [ ] 7.6 Implement IsBundle (.zip extension check)
- [ ] 7.7 Test zip creation and extraction round-trip
- [ ] 7.8 Test path rewriting correctness
- [ ] 7.9 Test missing optional files skipped

## 8. Verifiers

- [ ] 8.1 Create `internal/verifier/` package with VerifyResult type and RunVerify dispatcher
- [ ] 8.2 Implement CheckFileExists (expand env vars, os.Stat)
- [ ] 8.3 Implement CheckCommandExists (exec.LookPath)
- [ ] 8.4 Implement CheckRegistryKeyExists (golang.org/x/sys/windows/registry, GOOS guard)
- [ ] 8.5 Add golang.org/x/sys dependency to go.mod
- [ ] 8.6 Test file-exists with temp files
- [ ] 8.7 Test command-exists with known command
- [ ] 8.8 Test dispatch by type and unknown type handling

## 9. Commands — Restore, Revert, Export, Bootstrap

- [ ] 9.1 Implement `internal/commands/restore.go` (standalone restore command)
- [ ] 9.2 Implement `internal/commands/revert.go` (journal-based revert command)
- [ ] 9.3 Implement `internal/commands/export.go` (export-config: target → source inverse copy)
- [ ] 9.4 Implement `internal/commands/validate_export.go` (check export completeness)
- [ ] 9.5 Implement `internal/commands/bootstrap.go` (copy binary, create shim, update PATH)
- [ ] 9.6 Test export creates files in export dir
- [ ] 9.7 Test validate-export detects missing sources

## 10. Wiring and Integration

- [ ] 10.1 Wire --enable-restore into apply command (after install, before verify)
- [ ] 10.2 Wire --restore-filter flag into apply and restore commands
- [ ] 10.3 Wire verifiers into verify command (run manifest verify entries)
- [ ] 10.4 Wire bundle extraction into apply (detect .zip, extract, proceed)
- [ ] 10.5 Wire configModules expansion into manifest loading
- [ ] 10.6 Wire all new commands into main.go dispatch
- [ ] 10.7 Update capabilities.go with all new commands and flags
- [ ] 10.8 Update --help text in main.go

## 11. Validation

- [ ] 11.1 `go build ./cmd/endstate` compiles with zero errors
- [ ] 11.2 `go test ./...` passes all tests
- [ ] 11.3 `go vet ./...` passes with zero warnings
- [ ] 11.4 Verify all new commands appear in capabilities --json
- [ ] 11.5 Verify no PowerShell files modified
