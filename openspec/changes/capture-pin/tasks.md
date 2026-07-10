# Tasks: capture-pin

## 1. Engine

- [ ] 1.1 `capture.go`: swap the display-name seam to `listInstalledFn = snapshot.TakeSnapshot`; derive display-name map + version map from one pass in the step-2 goroutine (non-fatal on error, retry logic unchanged); keep `withMockDisplayNames` test-helper signature, add `withMockInstalledApps`
- [ ] 1.2 `capture.go`: add `Pin bool` to `CaptureFlags`
- [ ] 1.3 `cmd/endstate/main.go`: `--pin` flag parsing + global help + capture usage + dispatch (protected area — this change is the explicit instruction)
- [ ] 1.4 `capture.go` conversion loop: under `--pin`, set `version` from the version map (fallback snapshot Version), empty omitted
- [ ] 1.5 `capture.go` `--update` merge: always copy `Version`/`Driver` through; under `--pin`, refresh only on a non-empty lookup (never blank a pin)
- [ ] 1.6 `capture.go` sanitize: copy `Driver`/`Version` into `cleanApp` (realizer-path parity)
- [ ] 1.7 `capabilities.go`: add `--pin` to `commands.capture.flags`
- [ ] 1.8 Tests (hermetic): no-pin regression (no `version` keys), pin writes versions, export/list skew omits non-fatally, snapshot-error non-fatal, sanitize keeps versions, update preserves version+driver, update+pin refreshes, missing lookup never blanks, new apps under update+pin get versions, capabilities advertises `--pin`

## 2. Contract docs

- [ ] 2.1 `docs/contracts/cli-json-contract.md`: `--pin` in the capture flag list + one-line note in the version-capture-and-pinning section (additive, no schema bump)

## 3. Verification

- [ ] 3.1 `cd go-engine && go test ./internal/commands/...`
- [ ] 3.2 `cd go-engine && go test ./...`
- [ ] 3.3 `npm run openspec:validate`
- [ ] 3.4 Manual smoke (Windows): `go run ./cmd/endstate capture --pin --out %TEMP%\pin-test.jsonc --json`; inspect per-app `version`; `apply --dry-run` the result
