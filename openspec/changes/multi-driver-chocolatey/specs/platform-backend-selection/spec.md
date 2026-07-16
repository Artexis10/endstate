## MODIFIED Requirements

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

