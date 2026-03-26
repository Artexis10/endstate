## ADDED Requirements

### Requirement: File-exists verifier checks path existence
The Go engine SHALL implement a file-exists verifier that expands environment variables in the path and checks os.Stat. Pass if the path exists (file or directory). Fail with "File not found" if not.

#### Scenario: File exists
- **WHEN** a verify entry of type "file-exists" references an existing file path
- **THEN** the verifier returns Pass=true

#### Scenario: File does not exist
- **WHEN** a verify entry of type "file-exists" references a non-existent path
- **THEN** the verifier returns Pass=false with message "File not found"

### Requirement: Command-exists verifier checks PATH
The Go engine SHALL implement a command-exists verifier that uses exec.LookPath to check if a command is on PATH. Pass if found, fail with "Command not found" if not.

#### Scenario: Command on PATH
- **WHEN** a verify entry of type "command-exists" references "git"
- **THEN** the verifier returns Pass=true (assuming git is installed)

#### Scenario: Command not on PATH
- **WHEN** a verify entry of type "command-exists" references "nonexistent-command-xyz"
- **THEN** the verifier returns Pass=false with message "Command not found"

### Requirement: Registry-key-exists verifier checks Windows registry
The Go engine SHALL implement a registry-key-exists verifier that opens a Windows registry key and optionally checks for a value name. It SHALL parse the registry path to extract hive (HKCU, HKLM) and subkey. On non-Windows platforms, it SHALL return fail with "Registry checks only supported on Windows".

#### Scenario: Registry key exists
- **WHEN** a verify entry of type "registry-key-exists" references an existing registry key
- **THEN** the verifier returns Pass=true

#### Scenario: Non-Windows platform
- **WHEN** registry-key-exists is invoked on a non-Windows platform
- **THEN** the verifier returns Pass=false with message "Registry checks only supported on Windows"

### Requirement: Verifier dispatch by type
The Go engine SHALL dispatch verify entries to the correct checker by the Type field. Unknown types SHALL return a fail result with an appropriate message.

#### Scenario: Dispatch file-exists
- **WHEN** a VerifyEntry has type "file-exists"
- **THEN** the dispatcher calls CheckFileExists

#### Scenario: Unknown verify type
- **WHEN** a VerifyEntry has an unrecognized type
- **THEN** the dispatcher returns Pass=false with message indicating unknown type
