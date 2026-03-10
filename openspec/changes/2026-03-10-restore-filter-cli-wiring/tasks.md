## 1. CLI Param and Flag Normalization
- [x] 1.1 Add `$RestoreFilter` string parameter to param block in bin/endstate.ps1
- [x] 1.2 Add `--restore-filter` case to GNU-style flag normalization switch block

## 2. Command Dispatch Wiring
- [x] 2.1 Pass `-RestoreFilter $RestoreFilter` to Invoke-ApplyCore call in apply dispatch
- [x] 2.2 Add RestoreFilter parameter to Invoke-ApplyCore and filtering logic in restore section
- [x] 2.3 Add standalone restore command dispatch that delegates to engine Invoke-Restore with RestoreFilter

## 3. Capabilities and Help
- [x] 3.1 Add "--restore-filter" to commands.apply.flags in capabilities output
- [x] 3.2 Add restore command entry to capabilities with "--restore-filter" in flags
- [x] 3.3 Add --restore-filter to Show-ApplyHelp output
- [x] 3.4 Add "restore" to help dispatch and command listings

## 4. Contract Documentation
- [x] 4.1 Add "--restore-filter" to apply and restore command flags in docs/contracts/cli-json-contract.md
- [x] 4.2 Add restore command to supported commands table in docs/contracts/gui-integration-contract.md

## 5. Verification
- [ ] 5.1 `.\scripts\test-unit.ps1` passes (all existing tests)
- [ ] 5.2 `npm run openspec:validate` passes
