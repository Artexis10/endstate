# platform-backend-selection Specification

## Purpose
Defines platform-aware backend selection: Winget as the Windows default with additive Chocolatey, the Nix realizer on Linux and macOS with additive Brew on macOS, platform-keyed package references, capabilities reporting, and platform path conventions.
## Requirements
### Requirement: Backend selection is platform-aware
The engine SHALL select its package backends by host operating system. Windows SHALL expose Winget as the default per-package driver and Chocolatey as an additive explicit driver. Linux and macOS SHALL preserve the Nix realizer, and macOS SHALL preserve its additive Brew lane.

#### Scenario: Windows defaults to Winget
- **WHEN** a Windows app omits `driver`
- **THEN** the Winget driver SHALL be used
- **AND** existing Windows install/verify behavior SHALL be unchanged

#### Scenario: Windows explicitly selects Chocolatey
- **WHEN** a Windows app declares `driver: "chocolatey"`
- **THEN** the Chocolatey driver SHALL be used for that app
- **AND** failure or unavailability SHALL NOT fall back to Winget

#### Scenario: Unsupported platform reports no backend
- **WHEN** the engine selects a backend on a host with no implemented backend
- **THEN** selection SHALL fail with an explicit "no backend available" error
- **AND** no install operation SHALL be attempted

### Requirement: Package-reference resolution is platform-keyed
Package-reference resolution SHALL prefer the `App.Refs` entry keyed by the host operating system, falling back to the first non-empty ref.

#### Scenario: Windows ref selected on Windows
- **WHEN** an app has a `refs.windows` entry and the host is Windows
- **THEN** `refs.windows` SHALL be used, unchanged from prior behavior

#### Scenario: Linux ref selected on Linux
- **WHEN** an app has a `refs.linux` entry and the host is Linux
- **THEN** `refs.linux` SHALL be used

#### Scenario: Fallback when no OS-keyed ref exists
- **WHEN** an app has no ref for the host OS but has at least one non-empty ref
- **THEN** the first non-empty ref SHALL be used

### Requirement: Capabilities reports host platform and backends
The `capabilities` command data SHALL report the host operating system and the ordered list of supported backends dynamically rather than as fixed literals.

#### Scenario: Windows capabilities
- **WHEN** `capabilities` runs on a Windows host
- **THEN** the data SHALL report operating system `windows`
- **AND** the available drivers SHALL contain `winget` followed by `chocolatey`

#### Scenario: Non-Windows capabilities
- **WHEN** `capabilities` runs on a non-Windows host
- **THEN** the data SHALL report that host's operating system
- **AND** the data SHALL NOT claim Winget or Chocolatey is available

### Requirement: Profile and path resolution follow platform conventions
Profile-directory and environment-variable expansion SHALL follow host-platform conventions and SHALL be unchanged on Windows.

#### Scenario: Windows paths unchanged
- **WHEN** the profile directory is resolved on a Windows host
- **THEN** it SHALL be the existing `Documents\Endstate\Profiles` location
- **AND** `%VAR%`-style environment expansion SHALL be used

#### Scenario: Linux uses XDG and POSIX expansion
- **WHEN** the profile directory is resolved on a Linux host
- **THEN** it SHALL follow the XDG Base Directory specification
- **AND** `$VAR`-style environment expansion SHALL be used
