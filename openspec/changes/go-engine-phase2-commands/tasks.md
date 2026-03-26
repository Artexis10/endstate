## 1. System Snapshot Package

- [ ] 1.1 Create `internal/snapshot/snapshot.go` with `SnapshotApp` struct and `TakeSnapshot()` function that runs `winget list --source winget` and parses tabular output via column-position detection
- [ ] 1.2 Add `GetDisplayNameMap()` function returning `map[string]string` (winget ID → display name) from snapshot data
- [ ] 1.3 Add injectable `ExecCommand` for test mocking (same pattern as winget driver)
- [ ] 1.4 Handle edge cases: winget not available (WINGET_NOT_AVAILABLE error), empty output, malformed lines
- [ ] 1.5 Create `internal/snapshot/snapshot_test.go` with realistic winget output fixtures, empty output, malformed lines, display name map tests

## 2. State Persistence Package

- [ ] 2.1 Create `internal/state/state.go` with `State` struct, `StateDir()`, `ReadState()`, and `WriteState()` using atomic temp+rename pattern
- [ ] 2.2 Handle missing state file gracefully (return default empty state, not error)
- [ ] 2.3 Create `internal/state/history.go` with `SaveRunHistory()`, `ListRunHistory()`, `GetRunHistory()` functions
- [ ] 2.4 `ListRunHistory()` returns entries sorted by timestamp descending, limited to `limit` entries
- [ ] 2.5 Create `internal/state/state_test.go` testing atomic writes, run history save/list/get, missing state, and sort order

## 3. Planner Package

- [ ] 3.1 Create `internal/planner/planner.go` with `Plan`, `PlanAction`, `PlanSummary` structs
- [ ] 3.2 Implement `ComputePlan()` accepting manifest and driver, detecting each app, building actions list
- [ ] 3.3 Create `internal/planner/planner_test.go` with mock driver tests: all present, all missing, mixed, empty manifest

## 4. Capture Command

- [ ] 4.1 Create `internal/commands/capture.go` with `CaptureFlags` and `RunCapture()` following Phase 1 command patterns
- [ ] 4.2 Implement core flow: take snapshot → convert to manifest → apply filters → write output
- [ ] 4.3 Implement `--sanitize` behavior: strip underscore-prefixed fields, sort apps by id
- [ ] 4.4 Implement `--update` behavior: load existing manifest, merge newly discovered apps
- [ ] 4.5 Implement `--profile` behavior: write to `Documents/Endstate/Profiles/<name>.jsonc`
- [ ] 4.6 Implement runtime/store-app filtering (`--include-runtimes`, `--include-store-apps`)
- [ ] 4.7 Emit capture events: phase("capture"), item events per app, summary, artifact event with path
- [ ] 4.8 Implement INV-CAPTURE-2: verify output file exists and is non-empty after write
- [ ] 4.9 Wire `case "capture":` into `main.go` dispatch with all capture-specific flags

## 5. Plan Command

- [ ] 5.1 Create `internal/commands/plan.go` with `PlanFlags` and `RunPlan()` using planner package
- [ ] 5.2 Envelope data shape: `{ manifest: {path, name}, plan: {total, toInstall, alreadyPresent}, actions: [...] }`
- [ ] 5.3 Wire `case "plan":` into `main.go` dispatch

## 6. Report Command

- [ ] 6.1 Create `internal/commands/report.go` with `ReportFlags` and `RunReport()`
- [ ] 6.2 Support `--latest`, `--last N`, `--run-id` filters via state/history package
- [ ] 6.3 Envelope data shape per cli-json-contract.md: `{ reports: [...] }`
- [ ] 6.4 Wire `case "report":` into `main.go` dispatch with `--latest`, `--last`, `--run-id` flags

## 7. Doctor Command

- [ ] 7.1 Create `internal/commands/doctor.go` with `RunDoctor()` performing 6 checks
- [ ] 7.2 Check: winget available and version, PowerShell available, profiles dir exists, state dir writable, engine version
- [ ] 7.3 Use context timeout (5s) for subprocess calls to prevent hanging
- [ ] 7.4 Envelope data: `{ checks: [...], summary: {total, pass, fail, warn} }`
- [ ] 7.5 Wire `case "doctor":` into `main.go` dispatch
- [ ] 7.6 Create `internal/commands/doctor_test.go` testing check structure and summary computation

## 8. Profile Subcommands

- [ ] 8.1 Create `internal/commands/profile.go` with `RunProfile()` dispatching to list/path/validate subcommands
- [ ] 8.2 Implement `profile list`: discover profiles in profiles dir, exclude *.meta.json, validate candidates, resolve display labels (meta.json > name > filename stem)
- [ ] 8.3 Implement `profile path`: resolve name to path checking .zip, folder/manifest.jsonc, .jsonc, .json, .json5
- [ ] 8.4 Implement `profile validate`: use existing `manifest.ValidateProfile()`, return valid/errors/summary
- [ ] 8.5 Wire `case "profile":` into `main.go` dispatch with subcommand parsing
- [ ] 8.6 Create `internal/commands/profile_test.go` testing discovery, label resolution, and validation

## 9. Integration

- [ ] 9.1 Update `main.go` `parseArgs()` to handle new flags: `--out`, `--name`, `--profile`, `--sanitize`, `--discover`, `--update`, `--include-runtimes`, `--include-store-apps`, `--minimize`, `--latest`, `--last`, `--run-id`
- [ ] 9.2 Update `main.go` `commandUsage()` for all new commands
- [ ] 9.3 Update `capabilities.go` commands map with accurate flag lists for new commands
- [ ] 9.4 Verify `go build ./cmd/endstate` compiles with zero errors
- [ ] 9.5 Verify `go test ./...` passes all tests with zero failures
- [ ] 9.6 Verify all new commands appear in `--help` output and capabilities response
