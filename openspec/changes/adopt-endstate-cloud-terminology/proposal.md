# Change: Adopt "Endstate Cloud" as the public name for the managed service

## Why

The managed backup service already ships under the name Endstate Cloud where it
counts operationally: `go-engine/internal/backup/oidc/oidc.go` documents
`https://substratesystems.io` as "the Endstate Cloud production issuer", and the
contract's environment-variable table is headed "Default (Endstate Cloud)".
Everywhere a user actually reads, though, the same service is still called
"Hosted Backup" — CLI help text, the subscription-required remediation, the
recovery-key file header, and the readme. One service with two public names
costs support time and erodes trust in the documentation. This change makes the
user-facing surface match the name the service already carries internally.

The Endstate product itself is not renamed. Separately, the public phrase
"Supporter License" becomes "Support Endstate", so `SUPPORTERS.md` is brought
into line in the same pass.

## What Changes

- CLI help text for `backup` and `account` describes Endstate Cloud instead of
  Hosted Backup. Command tokens, flags, and help-column alignment are untouched.
- The `SUBSCRIPTION_REQUIRED` remediation names Endstate Cloud. The error code,
  the grace and cancelled semantics, and the action the user must take are
  unchanged.
- The recovery-key file header written by `backup signup` and `backup claim`
  names Endstate Cloud. It stays a `#`-prefixed comment line, which is the only
  property any consumer relies on.
- `readme.md` repositions the "Hosted Backup" section as "Endstate Cloud",
  keeping the self-hosting sentence, the contract link, and the "Endstate cannot
  decrypt user data" guarantee.
- `SUPPORTERS.md` replaces "Supporter License" with "Support Endstate". The
  `## Supporters` heading stays byte-identical because the website parses it.
- `PRINCIPLES.md` gains the service name in principles 3 and 5. No commitment is
  removed, narrowed, or softened.

**Deliberately not renamed** — these are compatibility surfaces, not cosmetics:

- the environment variables `ENDSTATE_OIDC_ISSUER_URL`, `ENDSTATE_OIDC_AUDIENCE`,
  and `ENDSTATE_BACKUP_CONCURRENCY`
- the JWT audience literal `endstate-backup`
- the `hostedBackup` capabilities key and the `HostedBackupFeature` Go type — a
  cross-repo wire contract with the GUI
- the `backup` command and every one of its subcommand names, which users have
  in scripts
- the `"hosted-backup"` event item-component literal
- Go package paths under `go-engine/internal/backup/`
- `docs/contracts/hosted-backup-contract.md`, filename and internal references

## Impact

- Affected specs: `endstate-cloud-terminology`
- Affected code: `go-engine/cmd/endstate/main.go` (help strings only),
  `go-engine/internal/backup/client/errors.go`,
  `go-engine/internal/commands/backup_signup.go`, `readme.md`, `PRINCIPLES.md`,
  and `SUPPORTERS.md`
- No behaviour, schema, wire format, or exit code changes. The only observable
  differences are the words in help output, one remediation string, and one
  comment line in the recovery file.
