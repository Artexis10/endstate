## ADDED Requirements

### Requirement: Go engine implements restore filter flag
The Go engine SHALL support --restore-filter on apply and restore commands, matching the restore-filter spec behavior: limiting restore execution to specified config modules, with inline entries always executing regardless of filter.

#### Scenario: Go engine restore filter matches PowerShell behavior
- **WHEN** the Go engine runs apply --enable-restore --restore-filter apps.vscode
- **THEN** only restore entries from module apps.vscode are executed and entries from other modules are skipped
