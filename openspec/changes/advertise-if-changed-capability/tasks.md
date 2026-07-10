## 1. Engine Implementation

- [x] 1.1 Add `IfChanged bool` field (JSON tag `ifChanged`) to `HostedBackupFeature` struct in `go-engine/internal/commands/capabilities.go`
- [x] 1.2 Set `IfChanged: true` in the `HostedBackupFeature` literal inside `RunCapabilities`

## 2. Contract Documentation

- [x] 2.1 Update `docs/contracts/gui-integration-contract.md` capabilities handshake example to show the current `hostedBackup` features shape (`supported`, `minSchemaVersion`, `issuerUrl`, `audience`, `rename`, `ifChanged`)
- [x] 2.2 Add a one-line note that `ifChanged` is the canonical gate for the GUI auto-backup conditional upload flow

## 3. Tests

- [x] 3.1 Add `TestRunCapabilities_HostedBackupIfChangedAdvertised` to `go-engine/internal/commands/capabilities_test.go` asserting `"ifChanged": true` in the JSON output
- [x] 3.2 Run `cd go-engine && go test ./internal/commands/...` — green

## 4. OpenSpec

- [x] 4.1 `npm run openspec:validate` — green
