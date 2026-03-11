## ADDED Requirements

### Requirement: Go engine implements bundle zip creation matching zip contract
The Go engine's bundle package SHALL produce zip files matching the layout defined in the capture-bundle-zip spec: manifest.jsonc at root, metadata.json, and configs/<module-id>/ directories. Path rewriting from ./payload/apps/<id>/ to ./configs/<module-id>/ SHALL be applied to module restore entries injected into the bundle manifest.

#### Scenario: Go bundle matches PowerShell zip layout
- **WHEN** the Go engine creates a capture bundle
- **THEN** the zip layout matches the PowerShell engine's output: manifest.jsonc at root, metadata.json, configs/<module-id>/ directories
