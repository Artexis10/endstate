## ADDED Requirements

### Requirement: Config module catalog loads from modules/apps/*/module.jsonc
The Go engine SHALL scan modules/apps/*/module.jsonc files and parse each into a Module struct with fields: id, displayName, sensitivity, matches (winget, exe, pathExists), verify, restore, capture, and notes. Invalid modules SHALL be skipped with a warning. Missing modules directory SHALL return an empty catalog.

#### Scenario: Load valid catalog
- **WHEN** modules/apps/vscode/module.jsonc and modules/apps/git/module.jsonc exist and are valid
- **THEN** LoadCatalog returns a map with "vscode" and "git" modules

#### Scenario: Skip invalid module
- **WHEN** a module.jsonc file has malformed JSON
- **THEN** the module is skipped and the catalog loads without it

#### Scenario: Missing modules directory
- **WHEN** the modules/apps/ directory does not exist
- **THEN** LoadCatalog returns an empty map without error

### Requirement: Module matching identifies applicable modules for captured apps
The Go engine SHALL match captured apps to modules by checking if any app's winget ID matches a module's matches.winget list. PathExists matches SHALL expand environment variables and check the filesystem. Only modules with capture sections SHALL be returned as matched.

#### Scenario: Match by winget ID
- **WHEN** a captured app has winget ID "Microsoft.VisualStudioCode" and a module has matches.winget=["Microsoft.VisualStudioCode"]
- **THEN** the module is included in matched results

#### Scenario: No match
- **WHEN** no captured apps match any module's winget IDs
- **THEN** the matched results list is empty

### Requirement: ConfigModules expansion injects module entries into manifest
The Go engine SHALL expand configModules references in a manifest by looking up each referenced module ID in the catalog, then injecting the module's restore entries into the manifest's restore array and verify entries into the manifest's verify array. Module restore source paths (./payload/apps/<id>/) SHALL be preserved as-is.

#### Scenario: Expand configModules
- **WHEN** a manifest has configModules=["vscode"] and the catalog has a vscode module with 3 restore entries
- **THEN** the manifest's restore array gains 3 additional entries from the vscode module

#### Scenario: Unknown module reference
- **WHEN** a manifest references a configModules ID that doesn't exist in the catalog
- **THEN** the unknown module is skipped with a warning
