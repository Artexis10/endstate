## 1. Engine Implementation

- [x] 1.1 Add `version` and `schemaVersion` package-level vars to `go-engine/internal/config/version.go` settable via ldflags
- [x] 1.2 Update `ReadVersion` to prefer ldflags value over file read
- [x] 1.3 Update `ReadSchemaVersion` to prefer ldflags value over file read
- [x] 1.4 Add `EmbeddedVersion()` and `EmbeddedSchemaVersion()` accessor functions

## 2. Tests

- [x] 2.1 Add `version_test.go` with fallback tests (no file, no ldflags -> "0.0.0-dev")
- [x] 2.2 Add test for file-based read when ldflags unset
- [x] 2.3 Add test for schema version fallback and file-based read

## 3. Verification

- [x] 3.1 Run `go test ./internal/config/...` -- all pass
- [x] 3.2 Build with ldflags and verify capabilities output shows injected version
- [x] 3.3 Clean up test binary
