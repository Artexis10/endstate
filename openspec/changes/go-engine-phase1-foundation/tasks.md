## 1. Project Scaffold

- [ ] 1.1 Initialize Go module (`go mod init`) and create directory structure
- [ ] 1.2 Implement version reading from VERSION and SCHEMA_VERSION files
- [ ] 1.3 Implement ENDSTATE_ROOT resolution and path utilities

## 2. JSON Envelope

- [ ] 2.1 Define Envelope struct matching cli-json-contract.md (all fields)
- [ ] 2.2 Implement NewEnvelope constructor with runId generation and timestampUtc
- [ ] 2.3 Define all error codes and Error struct from cli-json-contract.md
- [ ] 2.4 Write envelope tests (field presence, runId format, error serialization)

## 3. JSONC Manifest Loading

- [ ] 3.1 Implement JSONC comment stripping (line and block comments)
- [ ] 3.2 Define manifest types (Manifest, App, RestoreEntry, VerifyEntry)
- [ ] 3.3 Implement manifest loading with includes resolution and circular detection
- [ ] 3.4 Implement profile validation per profile-contract.md
- [ ] 3.5 Write manifest tests (valid profiles, comments, circular includes, validation errors)

## 4. Event System

- [ ] 4.1 Define event types (Phase, Item, Summary, Error, Artifact)
- [ ] 4.2 Implement Emitter with NDJSON output to stderr
- [ ] 4.3 Write event tests (NDJSON format, required fields, ordering)

## 5. Winget Driver

- [ ] 5.1 Define Driver interface (Detect, Install methods)
- [ ] 5.2 Implement winget Detect via `winget list --id <ref> -e`
- [ ] 5.3 Implement winget Install with exit code parsing and user_denied heuristic
- [ ] 5.4 Write winget driver tests with mocked exec.Command

## 6. CLI Entrypoint and Commands

- [ ] 6.1 Implement arg parsing for `endstate <command> [--flags]`
- [ ] 6.2 Implement `capabilities --json` command
- [ ] 6.3 Implement `verify --manifest <path> --json` command
- [ ] 6.4 Implement `apply --manifest <path> [--dry-run] [--enable-restore] --json` command
- [ ] 6.5 Implement `--help` and per-command help output
- [ ] 6.6 Wire `--events jsonl` flag to Emitter

## 7. Integration Verification

- [ ] 7.1 Verify `go build ./cmd/endstate` compiles with zero errors
- [ ] 7.2 Verify `go test ./...` passes all tests
- [ ] 7.3 Verify capabilities/verify/apply envelope shapes against contracts
