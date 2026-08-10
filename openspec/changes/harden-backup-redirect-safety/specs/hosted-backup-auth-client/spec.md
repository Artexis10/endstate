## ADDED Requirements

### Requirement: Default backup clients preserve redirect origin boundaries

The default backup API and OIDC HTTP clients SHALL follow an HTTP redirect only
when the target has the same scheme, hostname, and effective port as the
original request. They SHALL reject a cross-origin redirect, including an
HTTPS-to-HTTP downgrade, before sending a replayed request body or credentials.
Errors returned for a rejected redirect SHALL NOT expose request query values.

This redirect rule does not restrict direct endpoint values advertised in OIDC
discovery, including `endstate_extensions.backup_api_base` on a different
origin; those are explicit endpoint selections rather than redirect targets.

#### Scenario: Auth POST redirect to another origin is rejected

- **GIVEN** a self-hosted login, signup, recovery, or create-version endpoint
  responds with HTTP 307 or 308 targeting another origin
- **WHEN** the engine uses its default backup API client
- **THEN** the target receives no request or request body
- **AND** the engine returns a transport failure without request query values

#### Scenario: OIDC redirect to another origin is rejected

- **GIVEN** a self-hosted OIDC discovery or JWKS endpoint responds with a
  redirect to another origin
- **WHEN** the engine uses its default OIDC HTTP client
- **THEN** the target receives no request
- **AND** the error does not expose request query values

#### Scenario: Same-origin redirect is allowed

- **GIVEN** a backup API or OIDC endpoint responds with a redirect whose
  scheme, hostname, and effective port match the original request
- **WHEN** the engine uses its default client
- **THEN** the engine follows the redirect normally
