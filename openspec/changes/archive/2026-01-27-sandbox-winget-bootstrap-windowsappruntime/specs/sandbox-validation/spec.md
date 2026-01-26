# sandbox-validation Specification Delta

## MODIFIED Requirements

### Requirement: Winget Bootstrap (Strategy A)

The system SHALL attempt to bootstrap winget inside Windows Sandbox when it is not available, including installing required dependencies.

#### Scenario: Winget missing in Sandbox

- **WHEN** validation starts and winget is not available
- **THEN** the system attempts to:
  1. Download and install Windows App Runtime 1.8 redistributable (x64)
  2. Download and install VCLibs dependency
  3. Download and install App Installer from aka.ms/getwinget
- **AND** each bootstrap step is logged to `winget-bootstrap.log`
- **AND** winget availability is re-checked after bootstrap

#### Scenario: Windows App Runtime install

- **WHEN** winget bootstrap starts
- **THEN** the system downloads Windows App Runtime 1.8 redistributable from Microsoft
- **AND** installs the x64 framework package via Add-AppxPackage
- **AND** logs success or failure with full error details

#### Scenario: Bootstrap dependency failure

- **WHEN** Windows App Runtime or VCLibs installation fails
- **THEN** the error is logged with full exception details
- **AND** bootstrap continues to attempt remaining steps
- **AND** final winget availability determines success/failure
