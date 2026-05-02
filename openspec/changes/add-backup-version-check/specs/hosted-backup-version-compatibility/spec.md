## ADDED Requirements

### Requirement: Engine Inspects Version Header on Every Response

The hosted-backup HTTP client SHALL parse `X-Endstate-API-Version` from every backend response. The header value is `MAJOR.MINOR` and reflects the schema major + minor the backend implements.

#### Scenario: Header missing logs a warning, request proceeds

- **WHEN** a backend response omits the `X-Endstate-API-Version` header
- **THEN** the engine SHALL log a warning
- **AND** SHALL still process the response per the standard rules

#### Scenario: Header malformed logs a warning, request proceeds

- **WHEN** the header is present but cannot be parsed as `MAJOR.MINOR`
- **THEN** the engine SHALL log a warning naming the offending value
- **AND** SHALL still process the response per the standard rules

### Requirement: Major Mismatch Always Blocks

If the backend advertises a major version other than the engine's expected major, the request SHALL fail with `SCHEMA_INCOMPATIBLE` regardless of whether the request is a read or a write.

#### Scenario: Major-2 backend rejected on a read

- **GIVEN** the engine expects major `1`
- **WHEN** the backend returns `X-Endstate-API-Version: 2.0` on a `ReadOnly: true` request
- **THEN** the engine SHALL return `SCHEMA_INCOMPATIBLE`
- **AND** SHALL NOT decode the response body

### Requirement: Minor Mismatch Distinguishes Read From Write

If the backend advertises a higher minor than the engine knows about, the engine SHALL distinguish read-only from write-path requests:

- Read-only: log a warning, proceed
- Write: `SCHEMA_INCOMPATIBLE`

#### Scenario: 1.5 backend tolerated on read

- **GIVEN** the engine knows `1.0`
- **WHEN** the backend returns `X-Endstate-API-Version: 1.5` on a `ReadOnly: true` request
- **THEN** the engine SHALL log a warning
- **AND** SHALL still decode the response body and return success

#### Scenario: 1.5 backend rejected on write

- **GIVEN** the engine knows `1.0`
- **WHEN** the backend returns `X-Endstate-API-Version: 1.5` on a write request
- **THEN** the engine SHALL return `SCHEMA_INCOMPATIBLE`

### Requirement: Mismatch Skips Retry Loop

When the version check produces `SCHEMA_INCOMPATIBLE`, the engine SHALL NOT retry the request. Retrying an incompatible backend wastes time and obscures the diagnosis.

#### Scenario: Mismatch ends the retry loop

- **WHEN** the version check returns `SCHEMA_INCOMPATIBLE`
- **THEN** the engine SHALL return immediately
- **AND** SHALL NOT consume any retry attempts on the same incompatible backend
