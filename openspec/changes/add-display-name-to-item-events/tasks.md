## 1. Event Contract

- [ ] 1.1 Add `name` (string, optional) field to the Item Event section in `docs/contracts/event-contract.md`

## 2. Go Engine Events Package

- [ ] 2.1 Add `Name string json:"name,omitempty"` field to `ItemEvent` struct in `internal/events/types.go`
- [ ] 2.2 Add `name string` parameter to `EmitItem` in `internal/events/emitter.go` and set `Name: name` on the event
- [ ] 2.3 Update emitter tests in `internal/events/emitter_test.go` to cover name field presence and omission

## 3. Winget Driver

- [ ] 3.1 Change `Driver` interface `Detect(ref string) (bool, error)` to `Detect(ref string) (bool, string, error)` in `internal/driver/driver.go`
- [ ] 3.2 Update winget `Detect` implementation in `internal/driver/winget/detect.go` to parse Name column from `winget list` output and return it as the second value
- [ ] 3.3 Update winget driver tests to verify display name extraction from winget list output

## 4. Command Updates — Apply

- [ ] 4.1 Update apply.go plan phase: capture display name from `Detect` and pass to `EmitItem`
- [ ] 4.2 Update apply.go apply phase: propagate display name through install flow to `EmitItem` calls
- [ ] 4.3 Update apply.go restore phase: pass empty name for restore `EmitItem` calls
- [ ] 4.4 Update apply.go verify phase: capture display name from `Detect` and pass to `EmitItem`

## 5. Command Updates — Other Commands

- [ ] 5.1 Update verify.go: capture display name from `Detect` and pass to `EmitItem`
- [ ] 5.2 Update capture.go: pass `app.Name` to `EmitItem` calls and include `name` in `appsIncluded` JSON envelope entries
- [ ] 5.3 Update plan.go: capture display name from `Detect` (via planner) and pass to `EmitItem`
- [ ] 5.4 Update restore.go: pass empty name for all `EmitItem` calls
- [ ] 5.5 Update export.go: pass empty name for all `EmitItem` calls
- [ ] 5.6 Update validate_export.go: pass empty name for all `EmitItem` calls
- [ ] 5.7 Update revert.go: pass empty name for all `EmitItem` calls

## 6. Planner Update

- [ ] 6.1 Update planner to propagate display name from `Detect` through plan actions so commands can access it

## 7. Command Tests

- [ ] 7.1 Update `commands_test.go` to verify item events include display name for winget items and omit it for non-winget items
