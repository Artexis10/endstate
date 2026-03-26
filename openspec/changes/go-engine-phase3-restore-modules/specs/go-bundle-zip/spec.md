## ADDED Requirements

### Requirement: Bundle zip creation packages manifest, metadata, and configs
The Go engine SHALL create zip bundles containing: manifest.jsonc at zip root, metadata.json with timestamp/machine name/app count, and configs/<module-dir-name>/ directories with collected config files. Source paths in injected restore entries SHALL be rewritten from ./payload/apps/<id>/ to ./configs/<module-dir-name>/ to match the zip layout. The zip SHALL be created atomically (write to temp, rename).

#### Scenario: Create bundle with config files
- **WHEN** capture matches modules with config files
- **THEN** the bundle zip contains manifest.jsonc, metadata.json, and configs/<module-id>/ directories with the collected files

#### Scenario: Path rewriting in bundle manifest
- **WHEN** a module restore entry has source "./payload/apps/vscode/settings.json"
- **THEN** the bundle manifest rewrites it to "./configs/vscode/settings.json"

#### Scenario: Atomic zip creation
- **WHEN** bundle creation completes
- **THEN** the zip is written to a temp file first, then renamed to the final path

### Requirement: Bundle extraction unpacks to temp directory
The Go engine SHALL extract a zip bundle to a temporary directory and return the path to manifest.jsonc within the extracted directory. This enables apply to consume zip profiles.

#### Scenario: Extract and locate manifest
- **WHEN** a .zip file is provided as the manifest path
- **THEN** the bundle is extracted to a temp dir and the path to manifest.jsonc is returned

### Requirement: Config file collection copies from system paths
The Go engine SHALL collect config files from system paths as defined in module capture.files entries. Environment variables in source paths SHALL be expanded. Optional files that don't exist SHALL be skipped. ExcludeGlobs SHALL filter out matching files during collection.

#### Scenario: Collect with optional missing file
- **WHEN** a capture.files entry has optional=true and the source path does not exist
- **THEN** the file is skipped without error

#### Scenario: Collect with excludeGlobs
- **WHEN** a module has excludeGlobs=["**\\Cache\\**"]
- **THEN** files matching the pattern are not collected

### Requirement: Bundle detection by file extension
The Go engine SHALL detect whether a path is a bundle by checking for .zip extension. This is used by apply to determine whether to extract before processing.

#### Scenario: Detect zip bundle
- **WHEN** the manifest path ends with .zip
- **THEN** IsBundle returns true
