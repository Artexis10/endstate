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

### Requirement: Absent Commit Endpoint Degrades Gracefully

A backend that does not implement the version commit endpoint SHALL answer 404, and the engine SHALL treat that response as "this version is already durable" rather than as a failure. This preserves schema 2.0 behaviour, in which creating a version is the durability point.

Any commit response other than 2xx or 404 SHALL be treated as a failure.

#### Scenario: 404 on commit is not an error

- **WHEN** the commit request returns 404
- **THEN** the engine SHALL NOT return an error
- **AND** SHALL report that the backend did not acknowledge an explicit commit
- **AND** the push SHALL succeed

#### Scenario: Push against a schema 2.0 backend still succeeds

- **GIVEN** a backend advertising `X-Endstate-API-Version: 2.0` with no commit route
- **WHEN** `endstate backup push` uploads every chunk and the manifest successfully
- **THEN** the push SHALL report success
- **AND** the returned versionId SHALL be the one the backend minted at create time

#### Scenario: 5xx on commit is an error

- **WHEN** the commit request returns a 5xx status
- **THEN** the engine SHALL return an error
- **AND** the push SHALL fail
