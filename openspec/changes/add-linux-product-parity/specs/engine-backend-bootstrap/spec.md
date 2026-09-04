## ADDED Requirements

### Requirement: Linux discovery and capture do not require backend bootstrap
Read-only ordinary-machine Linux discovery and capture SHALL run without bootstrapping Nix or any native package source. If Nix is absent, its inventory adapter SHALL report unavailable while other discovery adapters continue. Nix installation SHALL remain an apply-time operation governed by the existing explicit consent flags/event contract.

#### Scenario: Capture runs with Nix absent
- **WHEN** Linux capture starts without a Nix executable and has usable native/desktop/config evidence
- **THEN** it SHALL discover and capture supported resolved state
- **AND** it SHALL neither request bootstrap consent nor install Nix

#### Scenario: Apply later needs Nix
- **WHEN** the resulting manifest is applied on a Linux target without Nix
- **THEN** the existing combined backend consent flow SHALL govern Nix bootstrap
- **AND** no package mutation SHALL occur before consent and post-install verification

#### Scenario: Nix inventory source fails but capture continues
- **WHEN** Nix is installed but its read-only inventory probe fails while another trustworthy discovery source succeeds
- **THEN** capture SHALL surface a Nix-source warning and continue with the successful evidence
- **AND** it SHALL NOT attempt repair or reinstallation during capture
