## ADDED Requirements

### Requirement: Capture never emits secret material

The captured manifest SHALL carry a home-manager secret as its declared *reference* — the path entry
or environment-variable wiring recorded with the provisioned configuration — and SHALL NOT contain
the secret plaintext or any engine-decryptable form of it. Capture SHALL source secrets from the
recorded provisioning input, which holds no secret material by construction, so the apply↔capture
loop is not a leak path.

#### Scenario: Captured manifest references the source, never the plaintext

- **WHEN** `capture` runs on a machine whose provisioned home-manager configuration declares
  `homeManager.secrets`
- **THEN** the captured manifest SHALL carry the declared secret references (paths and
  environment-variable wiring)
- **AND** it SHALL NOT contain the secret plaintext
