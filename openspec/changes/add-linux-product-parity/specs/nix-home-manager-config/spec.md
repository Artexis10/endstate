## ADDED Requirements

### Requirement: Engine-generated Home Manager configuration uses immutable compatible inputs
Release-default generated Home Manager configurations SHALL use revision-locked, mutually compatible Nixpkgs and Home Manager inputs owned by the Endstate release. The engine SHALL record the effective inputs used for activation. Environment overrides MAY replace them for advanced use, but the effective resolved inputs SHALL remain inspectable and SHALL NOT be reported as release defaults. If an override source remains mutable, the engine SHALL distinguish that source from its frozen activation derivation and effective lock graph. The engine SHALL NOT claim that the original source became immutable.

#### Scenario: Release-default catalog activation
- **WHEN** Endstate compiles and activates `homeManager.settings` under release defaults
- **THEN** the generated flake SHALL reference the release's immutable Nixpkgs and Home Manager revisions
- **AND** the Provisioning Generation SHALL record those effective inputs

#### Scenario: Curated options are validated against the release pair
- **WHEN** a release includes curated Home Manager settings mappings
- **THEN** required CI SHALL evaluate and activate the full curated corpus against the exact embedded input pair
- **AND** release SHALL fail if an option mapping is incompatible

### Requirement: External Home Manager ownership is preserved
The engine SHALL distinguish an Endstate-owned generated Home Manager activation from an externally owned activation using Endstate provisioning provenance. It SHALL NOT automatically replace, edit, or compose into an externally owned configuration. A requested Endstate-generated activation on such a target SHALL return an inspectable ownership action, including an importable generated module or an explicitly supported safe-restore fallback.

#### Scenario: External activation has no Endstate provenance
- **WHEN** a live Home Manager activation exists and no matching Endstate Provisioning Generation owns it
- **THEN** the engine SHALL classify it as externally owned
- **AND** it SHALL NOT run a replacing `home-manager switch` automatically

#### Scenario: Endstate activation advances normally
- **WHEN** the current Home Manager activation is linked to Endstate provisioning provenance
- **THEN** a consented settings apply MAY advance it through the existing generated configuration path
- **AND** the new generation and effective inputs SHALL be recorded

#### Scenario: Dry run reveals ownership action
- **WHEN** a dry run targets an externally owned Home Manager configuration
- **THEN** the plan SHALL identify the ownership conflict and generated import artifact or fallback
- **AND** it SHALL make no configuration changes

#### Scenario: Native ownership cannot be established
- **WHEN** the profile is unreadable, malformed, incomplete, changes during observation, or requires unreadable provisioning history
- **THEN** generated activation SHALL stop with an ownership observation error
- **AND** the engine SHALL NOT report absence or an established external owner
- **AND** the initial ownership check SHALL precede package mutation

#### Scenario: Native identity is observed together
- **WHEN** the engine observes a Home Manager profile
- **THEN** it SHALL obtain the generation and exact output from that specific native generation link
- **AND** it SHALL reject a detected profile change before accepting the observation
- **AND** only a missing profile SHALL establish absence

#### Scenario: Ownership changes after preparation
- **WHEN** the profile becomes externally owned or unreadable before generated activation
- **THEN** the engine SHALL stop that activation after a fresh ownership check
- **AND** it SHALL return the external ownership action only for an established external owner
- **AND** it SHALL retain provisioning evidence for any package work already committed

#### Scenario: Legacy history cannot override output identity
- **WHEN** provisioning history contains output identity and the active output has no matching record
- **THEN** a matching generation-only record SHALL NOT grant Endstate ownership
- **AND** wholly legacy history MAY match a successfully observed native generation

#### Scenario: Unavailable ownership remains visible to other commands
- **WHEN** verify or capture cannot observe native Home Manager identity
- **THEN** verify SHALL report unreadable state rather than missing state
- **AND** capture SHALL report unavailable provenance while retaining independent capture results
- **AND** unreadable post-activation identity SHALL remain an uncertain committed outcome

### Requirement: Home Manager adapter metadata is derived from the same immutable input pair
The release SHALL generate its Home Manager settings-adapter metadata from the exact Home Manager revision and compatible Nixpkgs revision embedded for realization. Generation SHALL use the upstream option JSON and pure evaluated module outputs to identify candidate program options and managed home-file targets, then freeze source hashes and reviewed dispositions into a runtime registry. Runtime discovery SHALL consume the frozen registry without evaluating arbitrary user Nix or requiring the Home Manager CLI.

#### Scenario: Adapter registry and realizer inputs agree
- **WHEN** release validation builds the Home Manager adapter registry
- **THEN** the registry SHALL record the exact Home Manager and Nixpkgs revisions used by generated activations
- **AND** CI SHALL reject a release whose adapter metadata was generated from different inputs

#### Scenario: User already owns a Home Manager graph
- **WHEN** an existing external Home Manager configuration is present
- **THEN** Endstate MAY use its safely inspectable evaluated values as additional read-only evidence after the ownership boundary is established
- **AND** ordinary discovery SHALL still read live targets through the frozen registry and SHALL NOT depend on evaluating or editing that graph

### Requirement: Home Manager activation verification includes managed target contents
For every advertised Home Manager settings adapter, verify SHALL compare the expected reviewed portable values or file bytes with the live engine-owned targets after activation. Matching Endstate provisioning provenance and a matching active Home Manager generation SHALL establish ownership/generation identity but SHALL NOT substitute for target-content verification. Revert SHALL restore and verify the previous Endstate-owned Home Manager generation or the explicitly journaled safe fallback state.

#### Scenario: Active generation matches but managed target was edited
- **WHEN** the active Home Manager generation matches the Endstate Provisioning Generation but a managed target differs from the expected portable content
- **THEN** verify SHALL report settings drift for that target
- **AND** it SHALL NOT report the adapter verified solely from generation identity

#### Scenario: Advertised adapter has no content verifier
- **WHEN** a registry/module adapter can activate a target but has no reviewed expected-content comparison
- **THEN** capabilities and discovery SHALL NOT advertise full verify/revert support for that adapter
- **AND** the applicability matrix SHALL NOT count it as release-supported

#### Scenario: Revert advances to the previous owned generation
- **WHEN** a user explicitly reverts an Endstate-owned Home Manager settings apply
- **THEN** Endstate SHALL activate the recorded previous owned generation or restore an explicitly journaled safe fallback
- **AND** it SHALL verify the restored target contents before reporting success
