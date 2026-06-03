## ADDED Requirements

> SKETCH — this delta-spec is part of a DESIGN-ONLY change. The requirements below define the
> behavior the eventual implementation MUST satisfy; no engine code implements them yet. They are
> stated behavior-level and backend-agnostic so they hold whether the chosen backend is the
> documented boundary, agenix, or sops-nix.

### Requirement: Secrets are referenced, never embedded in generated config

When a home-manager configuration depends on secret material, the engine SHALL emit only a
**reference** to that material — a path the secret is expected at, an environment variable name, or
an external secret-store handle — into the generated, inspectable artifacts. The generated
`flake.nix`, the compiled `home.nix`, and any files staged beside them SHALL NOT contain secret
plaintext, and SHALL NOT contain ciphertext the engine itself produced. The generated directory
remains safe to read and to commit.

#### Scenario: Generated config contains only a reference, never the secret

- **WHEN** `apply` activates a home-manager configuration that declares a dependency on secret
  material
- **THEN** the generated `flake.nix`, `home.nix`, and any staged files SHALL contain only a
  reference to where the secret lands (a decrypted-at-activation path, an environment variable name,
  or an external store handle)
- **AND** they SHALL NOT contain the secret plaintext

#### Scenario: Committed encrypted material is ciphertext the engine did not author

- **WHEN** a managed backend is used and encrypted secret files live beside the generated flake
- **THEN** those files SHALL be ciphertext authored by the user (or their tooling), decryptable only
  with a user-owned key that the engine never generates and never stores
- **AND** the generated config SHALL reference the decrypted-at-activation path, not the plaintext

#### Scenario: Decryption key stays user-owned and out of the generated tree

- **WHEN** the engine generates the home-manager flake for a secrets-bearing configuration
- **THEN** the engine SHALL NOT write any decryption key (age identity, PGP key, or equivalent) into
  the generated directory or any captured artifact
- **AND** the key SHALL remain user-provisioned, outside the inspectable flake tree

### Requirement: Capture never emits secret material

The captured manifest SHALL reference the secret *source* — the declared path, environment variable,
or encrypted file location — and SHALL NOT contain the secret plaintext or any engine-decryptable
form of it. When `capture` records a home-manager configuration that depends on secret material, the
apply↔capture loop SHALL NOT be a leak path.

#### Scenario: Captured manifest references the source, never the plaintext

- **WHEN** `capture` runs on a machine whose home-manager configuration depends on secret material
- **THEN** the captured manifest SHALL reference the secret source (path / environment variable /
  encrypted file location)
- **AND** it SHALL NOT contain the secret plaintext

#### Scenario: Capture omits material it cannot reference safely

- **WHEN** `capture` cannot represent a secret as a safe reference without including its material
- **THEN** the engine SHALL omit that secret from the captured manifest rather than emit the material
- **AND** capture SHALL still produce a valid manifest of the remaining configuration

### Requirement: Secrets backend is pluggable and explicitly declared

If a typed secrets input is adopted, the secret-management backend SHALL be named explicitly in the
manifest (for example: the documented boundary, agenix, or sops-nix), and the engine SHALL NOT infer
a backend implicitly. An unsupported or unspecified backend SHALL fail loudly at load rather than
silently degrade to embedding the secret.

#### Scenario: Declared backend selects the generation strategy

- **WHEN** a manifest declares a home-manager secret with an explicit, supported backend
- **THEN** the engine SHALL generate the reference wiring for that backend
- **AND** the no-embed and capture invariants SHALL hold regardless of which backend is named

#### Scenario: Unsupported backend fails loudly

- **WHEN** a manifest declares a secrets backend the engine does not support
- **THEN** the engine SHALL reject the manifest at load with a clear error
- **AND** it SHALL NOT fall back to embedding the secret in the generated config
