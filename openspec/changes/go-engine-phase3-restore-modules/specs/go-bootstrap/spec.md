## ADDED Requirements

### Requirement: Bootstrap installs endstate to system PATH
The Go engine SHALL implement a bootstrap command that: creates %LOCALAPPDATA%\Endstate\bin\ and %LOCALAPPDATA%\Endstate\bin\lib\ directories, copies the running binary (os.Executable()) to lib/endstate.exe, creates/updates an endstate.cmd shim in the bin directory that delegates to the Go binary, and adds the bin directory to the user PATH if not already present. The envelope data SHALL include installPath, shimPath, and addedToPath (bool).

#### Scenario: Fresh bootstrap
- **WHEN** bootstrap is run and %LOCALAPPDATA%\Endstate\bin\ does not exist
- **THEN** directories are created, binary is copied, shim is created, and PATH is updated

#### Scenario: Re-bootstrap updates binary
- **WHEN** bootstrap is run and the installation already exists
- **THEN** the binary is overwritten with the current version and the shim is updated

#### Scenario: PATH already contains endstate
- **WHEN** the user PATH already includes the endstate bin directory
- **THEN** addedToPath is false and PATH is not modified
