## ADDED Requirements

### Requirement: Engine Advertises Its Schema Version On Every Request

The engine SHALL send `X-Endstate-API-Version: MAJOR.MINOR` on every request to the backup backend. The backend uses the advertised minor to decide whether a created version requires an explicit commit before it becomes durable (contract §7, §8), so a request without the header leaves the backend unable to negotiate.

The header SHALL be set centrally in the HTTP client rather than per call site, so no endpoint can omit it.

#### Scenario: Every request carries the engine schema version

- **WHEN** the engine makes any request to the backup backend
- **THEN** the request SHALL include `X-Endstate-API-Version`
- **AND** the value SHALL be the engine's `MAJOR.MINOR` schema version

#### Scenario: Engine advertises 2.1 for the commit-aware contract

- **WHEN** the engine implements contract schema 2.1
- **THEN** the advertised value SHALL be `2.1`

### Requirement: Older Backend Minor Is Accepted On Reads And Writes

A backend advertising a minor version LOWER than the engine's SHALL be treated as compatible for both read-only and write requests. Only a HIGHER backend minor restricts the engine (writes blocked, reads warned), and only a differing major blocks unconditionally.

#### Scenario: 2.1 engine writes to a 2.0 backend

- **GIVEN** the engine knows minor `1`
- **WHEN** the backend returns `X-Endstate-API-Version: 2.0` on a write request
- **THEN** the engine SHALL proceed with the request
- **AND** SHALL NOT return `SCHEMA_INCOMPATIBLE`

#### Scenario: 2.1 engine reads from a 2.0 backend

- **GIVEN** the engine knows minor `1`
- **WHEN** the backend returns `X-Endstate-API-Version: 2.0` on a read-only request
- **THEN** the engine SHALL proceed with the request

### Requirement: Commit Requirement Is Negotiated At Create Time

The engine SHALL parse `requiresCommit` from create-version. When absent or
false, creation retains schema-2.0 create-is-durable behaviour and the engine
SHALL NOT call commit. When true, a durable result requires a 2xx commit; every
other outcome, including 404, authorization failure, timeout, rejection, or
cancellation, SHALL fail the push.

#### Scenario: Legacy create response skips commit

- **WHEN** create-version omits `requiresCommit`
- **THEN** the engine SHALL NOT send a commit request
- **AND** the push SHALL retain create-is-durable behaviour

#### Scenario: Required commit 404 fails the push

- **GIVEN** create-version returned `requiresCommit: true`
- **WHEN** the commit request returns 404
- **THEN** the push SHALL fail
- **AND** the engine SHALL NOT return a push result payload

#### Scenario: 5xx on commit is an error

- **WHEN** the commit request returns a 5xx status
- **THEN** the engine SHALL return an error
- **AND** the push SHALL fail

### Requirement: Create-Version Replay Requires Explicit Capability

The engine SHALL set a stable `X-Endstate-Operation-ID` and mark
create-version retry-safe only when OIDC discovery's optional
`backup_api_capabilities` array contains the exact token
`version-create-operation-replay-v1`. Missing, malformed, or unknown capability
data SHALL retain one-shot legacy create behavior.

#### Scenario: Capability enables byte-identical replay

- **GIVEN** discovery advertises `version-create-operation-replay-v1`
- **WHEN** a create-version response is lost
- **THEN** the engine MAY retry with the same operation ID and request bytes

#### Scenario: No capability retains legacy safety

- **GIVEN** discovery omits or malforms `backup_api_capabilities`
- **WHEN** the engine creates a version
- **THEN** it SHALL omit the operation ID and SHALL NOT automatically replay an
  ambiguous create request
