## ADDED Requirements

### Requirement: backup subscribe command initiates checkout

The CLI SHALL provide a `backup subscribe` subcommand that initiates a Hosted Backup subscription checkout against substrate's billing endpoint and returns the checkout URL and transaction id. The engine SHALL NOT open a browser; the GUI opens the returned `checkoutUrl`.

#### Scenario: Signed-in user receives checkout URL

- **WHEN** `endstate backup subscribe --json` is run and the session is signed in
- **THEN** the engine issues `POST <issuer>/api/billing/checkout` with the persisted access token as a bearer credential
- **AND** the success envelope `data` contains `checkoutUrl` and `transactionId` from the substrate response

#### Scenario: Signed-out user is not charged a network call

- **WHEN** `endstate backup subscribe` is run and no session is signed in
- **THEN** the command returns error code `AUTH_REQUIRED`
- **AND** no request is made to the billing endpoint
- **AND** the error remediation directs the user to run `endstate backup login`

#### Scenario: Backend reports payment required

- **WHEN** the billing endpoint responds with HTTP 402
- **THEN** the command returns error code `SUBSCRIPTION_REQUIRED`

### Requirement: Checkout URL resolves from the issuer base

The billing checkout URL SHALL be derived from the configured issuer base as `<issuer>/api/billing/checkout`, consistent with how `/api/account/me` is resolved, rather than from a discovery-document field.

#### Scenario: URL derived from configured issuer

- **WHEN** the engine builds the checkout request
- **THEN** the request URL is the trimmed issuer URL with `/api/billing/checkout` appended

### Requirement: Checkout endpoint documented in contract

`docs/contracts/hosted-backup-contract.md` §7 SHALL document the engine-initiated `POST /api/billing/checkout` endpoint and its `{ checkoutUrl, transactionId }` response.

#### Scenario: Contract lists the billing checkout endpoint

- **WHEN** a developer reads the §7 API surface of the hosted-backup contract
- **THEN** `POST /api/billing/checkout → { checkoutUrl, transactionId }` is listed as engine-initiated and GUI-opened
