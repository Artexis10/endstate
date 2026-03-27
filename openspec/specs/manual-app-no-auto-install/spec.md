# manual-app-no-auto-install Specification

## Purpose
Ensures that apps marked as manual-install are never automatically installed via winget or any other package manager during apply.

## Requirements
### Requirement: Manual Apps Are Skipped During Install

Apps designated as manual-install in their module definition SHALL NOT be installed by the automated install stage.

#### Scenario: Manual app is skipped during apply
- **WHEN** `apply` is run and the manifest includes an app whose module specifies manual install
- **THEN** the install stage skips that app
- **AND** no winget install command is issued for it

#### Scenario: Manual app is reported in plan
- **WHEN** `apply` generates a plan that includes a manual-install app
- **THEN** the plan entry indicates the app requires manual installation
- **AND** the user is informed that the app must be installed outside of Endstate

### Requirement: Manual Apps Still Support Restore and Verify

Manual-install apps SHALL still participate in restore and verification stages if the app is already present on the system.

#### Scenario: Manual app config is restored if app is present
- **WHEN** `apply --EnableRestore` is run and a manual-install app is already installed
- **THEN** restore entries for that app are executed normally
- **AND** config files are written according to the module's restore definitions

#### Scenario: Manual app is verified if present
- **WHEN** `apply` runs verification for a manual-install app that is installed
- **THEN** the verifier checks the app's expected state
- **AND** the result reflects whether the app meets its verification criteria
