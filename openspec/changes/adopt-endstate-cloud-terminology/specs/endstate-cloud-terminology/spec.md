## MODIFIED Requirements

### Requirement: Managed Service Public Name

The engine SHALL refer to Endstate's managed backup service as "Endstate Cloud"
in every user-facing string it emits — CLI help text, error remediation,
generated file headers, and repository documentation. The engine SHALL NOT
present "Hosted Backup" as a user-facing service name.

Machine-facing identifiers are excluded from this requirement and SHALL remain
unchanged: the `backup` command and all of its subcommand names, the
`hostedBackup` capabilities key, the `HostedBackupFeature` Go type, the
`"hosted-backup"` event item component, the `endstate-backup` JWT audience, the
`ENDSTATE_OIDC_ISSUER_URL`, `ENDSTATE_OIDC_AUDIENCE`, and
`ENDSTATE_BACKUP_CONCURRENCY` environment variables, Go package paths under
`internal/backup/`, and the `docs/contracts/hosted-backup-contract.md` filename.

#### Scenario: CLI help names the service

- **WHEN** a user runs `endstate --help`
- **THEN** the `backup` and `account` entries describe Endstate Cloud
- **AND** the `backup` and `account` command tokens are unchanged

#### Scenario: Subcommand names survive the rename

- **WHEN** a user runs `endstate backup login` from a pre-existing script
- **THEN** the command resolves exactly as it did before the rename
- **AND** only its description text differs from the previous release

#### Scenario: Subscription remediation names the service

- **WHEN** an upload is refused with the `SUBSCRIPTION_REQUIRED` error code
- **THEN** the remediation tells the user to subscribe to Endstate Cloud
- **AND** the error code is still `SUBSCRIPTION_REQUIRED`
- **AND** the remediation still states that restore remains available during the
  grace and cancelled states

#### Scenario: Wire identifiers survive the rename

- **WHEN** the GUI reads `endstate capabilities --json`
- **THEN** the managed-service feature block is still keyed
  `data.features.hostedBackup`

### Requirement: Recovery Key File Header

`backup signup` and `backup claim` SHALL write the recovery mnemonic to the
`--save-recovery-to` path beneath a header of `#`-prefixed comment lines that
names Endstate Cloud and warns that anyone holding the phrase can reset the
passphrase and decrypt the data. The header SHALL remain purely informational:
consumers locate the mnemonic by discarding `#`-prefixed and blank lines, so
header wording MUST NOT be parsed for meaning.

#### Scenario: Header is discarded when the mnemonic is read back

- **WHEN** a consumer reads a recovery file written by the engine
- **THEN** it discards every `#`-prefixed line and every blank line
- **AND** the remaining whitespace-separated tokens are exactly the 24 mnemonic
  words

#### Scenario: Header names Endstate Cloud

- **WHEN** `backup signup --save-recovery-to <path>` writes the recovery file
- **THEN** the first line is a `#` comment naming Endstate Cloud

### Requirement: Public Support Phrasing

Public documentation SHALL describe the paid support offering as "Support
Endstate" rather than "Supporter License". Supporting Endstate SHALL remain
optional, existing support records SHALL remain valid, and listing a supporter's
name SHALL remain opt-in. The `## Supporters` heading in `SUPPORTERS.md` SHALL
remain byte-identical, because the website parses it as a live anchor.

#### Scenario: Supporters heading is stable

- **WHEN** the website matches `SUPPORTERS.md` against `/^##\s+Supporters\s*$/i`
- **THEN** the heading matches exactly as it did before the rename

#### Scenario: Support remains optional and opt-in

- **WHEN** a user reads `SUPPORTERS.md`
- **THEN** it states that supporting Endstate is optional
- **AND** it states that being listed is opt-in
- **AND** it states that Endstate is fully free either way

### Requirement: Principles Renamed Without Weakening

Naming edits to `PRINCIPLES.md` SHALL preserve every commitment the file
records, verbatim in substance. The file MAY adopt the Endstate Cloud name; an
edit that removes, narrows, or softens a commitment SHALL NOT be made.

#### Scenario: Self-hosting commitment survives the rename

- **WHEN** principle 5 is updated to name the Endstate Cloud backup protocol
- **THEN** it still states that the protocol is documented and open
- **AND** it still states that anyone can run their own backup server
- **AND** it still states that the client supports configuring an alternative
  server endpoint

#### Scenario: Subscription-scope commitment survives the rename

- **WHEN** principle 3 is updated to name Endstate Cloud
- **THEN** it still states that a subscription buys access to the managed
  services and gates nothing else
- **AND** it still states that everything running on the user's own machine works
  without a subscription, forever
