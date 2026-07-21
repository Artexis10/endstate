## ADDED Requirements

### Requirement: Capture can produce a bundle intended for another person

`capture --share` SHALL produce a bundle whose configuration restore prefers merging onto the recipient's existing settings rather than replacing them, and SHALL omit the capturing machine's name from bundle metadata.

`--share` SHALL require `--only`, and SHALL be rejected when combined with `--sanitize`. Both rejections SHALL use `MANIFEST_VALIDATION_ERROR` and occur before anything is captured or written.

Bundle metadata SHALL additionally record the capture host OS, whether the bundle is a share bundle, and the name supplied at capture time.

#### Scenario: Share bundle omits the sender's machine name

- **WHEN** `capture --share --only <app>` runs
- **THEN** the bundle's metadata records no machine name
- **AND** the metadata records the capture host OS and that the bundle is a share bundle

#### Scenario: Share without a selection is rejected

- **WHEN** `capture --share` runs with no `--only`
- **THEN** the run fails with `MANIFEST_VALIDATION_ERROR`
- **AND** nothing is captured or written

#### Scenario: Share with sanitize is rejected

- **WHEN** `capture --share --only <app> --sanitize` runs
- **THEN** the run fails with `MANIFEST_VALIDATION_ERROR`

#### Scenario: Self-rebuild capture is unchanged

- **WHEN** capture runs without `--share`
- **THEN** restore entries, backup flags, and metadata are exactly as before

### Requirement: Share restore entries prefer merging, conservatively

In a share bundle, every restore entry SHALL have backup enabled, so any merge can be undone.

A `copy` entry SHALL be retyped to a merging strategy only when the bundled payload proves the merge is safe. A wrong merge silently corrupts a configuration file, whereas a replace is backed up and revertable, so an uncertain case SHALL remain a `copy`.

- A `copy` entry SHALL become a JSON merge only when its bundled payload parses as a strict JSON **object**. Payloads carrying comments or trailing commas, and payloads that are arrays or scalars, SHALL remain `copy`.
- A `copy` entry SHALL become an INI merge only for `.ini` targets, and SHALL NOT do so for git configuration files, whose duplicate keys INI merging would collapse.
- An entry that already declares a restore type SHALL NOT be retyped.

The retyping decision SHALL be recorded in the bundled entry itself, so an engine that predates this behaviour still merges when applying a share bundle.

#### Scenario: A JSON object payload merges

- **WHEN** a share bundle is produced for a module whose payload is a strict JSON object
- **THEN** that restore entry is a JSON merge with backup enabled

#### Scenario: A commented JSON payload stays a copy

- **WHEN** the payload contains comments or trailing commas
- **THEN** the entry remains a `copy` with backup enabled

#### Scenario: A JSON array payload stays a copy

- **WHEN** the payload is a JSON array, such as a keybindings file
- **THEN** the entry remains a `copy`, because merging an array would replace the recipient's file rather than layer onto it

#### Scenario: Git configuration stays a copy

- **WHEN** the restore target is a git configuration file
- **THEN** the entry remains a `copy`, because INI merging collapses the duplicate keys git relies on

#### Scenario: A declared strategy is preserved

- **WHEN** a module declares a restore type other than `copy`
- **THEN** that type is used unchanged, with backup enabled

#### Scenario: An unreadable payload stays a copy

- **WHEN** a bundled payload cannot be read
- **THEN** the entry remains a `copy`, rather than becoming a merge that would fail during restore

### Requirement: Rebuild refuses a bundle captured on a different OS

`rebuild` SHALL refuse a bundle whose recorded capture OS differs from the host, with `NOT_SUPPORTED` and a message naming both operating systems.

Configuration modules carry no non-Windows package identity and their paths are OS-specific, so a cross-OS apply installs nothing and restores to paths that do not exist. Refusing is more truthful than reporting a run whose every skip is "wrong OS".

A bundle that records no capture OS predates the field and SHALL be accepted.

#### Scenario: Cross-OS bundle is refused

- **WHEN** a bundle captured on one OS is applied on another
- **THEN** the run fails with `NOT_SUPPORTED` naming both operating systems
- **AND** nothing is installed or restored

#### Scenario: Same-OS bundle proceeds

- **WHEN** the bundle's capture OS matches the host
- **THEN** the rebuild proceeds

#### Scenario: A bundle without a recorded OS is accepted

- **WHEN** a bundle predating the OS field is applied
- **THEN** the rebuild proceeds

### Requirement: Share bundles have identity redacted and report the boundary

A share bundle SHALL have identity-bearing values removed from its configuration payloads. Self-rebuild bundles SHALL be unaffected.

Config modules whose payloads are inherently account- or device-bound SHALL be omitted from a share bundle entirely, and each omission SHALL be reported as a capture warning.

Payload text SHALL have Windows user-path segments, email addresses, and the capturing hostname replaced. Git configuration SHALL additionally have its user identity fields removed while other settings are preserved.

Bundle metadata SHALL report per-rule replacement counts and SHALL name every payload that could not be decoded as text. Those payloads SHALL be left byte-identical. A payload's encoding SHALL be preserved when it is rewritten.

#### Scenario: User paths, emails and hostname are replaced

- **WHEN** a share bundle is produced from payloads containing a user path, an email address, and the capturing machine's hostname
- **THEN** none of those values appear in the bundled payloads
- **AND** the metadata reports a replacement count per rule

#### Scenario: Escaped and percent-encoded paths are replaced

- **WHEN** a payload stores a path escaped, or as a file URI with a percent-encoded drive letter
- **THEN** the username segment is replaced in both forms

#### Scenario: Git identity is stripped but preferences survive

- **WHEN** a git configuration payload declares a user name, email, signing key, and an unrelated setting
- **THEN** the identity fields are redacted
- **AND** the unrelated setting is unchanged

#### Scenario: Account-bound modules are omitted and reported

- **WHEN** a selection includes a module whose configuration is account- or device-bound
- **THEN** that module's configuration is absent from the bundle
- **AND** a capture warning names it

#### Scenario: Undecodable payloads are reported, not altered

- **WHEN** a payload cannot be decoded as text
- **THEN** it is listed as unscanned in the metadata
- **AND** its bytes are unchanged

#### Scenario: Encoding is preserved on rewrite

- **WHEN** a UTF-16 payload with a byte-order mark is redacted
- **THEN** the rewritten payload retains its encoding and byte-order mark

#### Scenario: Redaction is idempotent

- **WHEN** already-redacted text is redacted again
- **THEN** it is unchanged and no further replacements are counted
