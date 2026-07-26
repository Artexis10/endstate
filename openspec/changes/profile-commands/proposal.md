## Why

Profile composition introduced overlay profiles — `includes`, `exclude`, and `excludeConfigs` — but the only way to author one was to hand-edit JSONC. That is error-prone: users must know the field names, get the winget ID exactly right, and avoid corrupting the file. It also invites a subtle failure mode, where an editor round-trips a resolved manifest and flattens the base profile's apps into the overlay.

## What Changes

Add an `endstate profile` command that creates and mutates overlay profiles safely, with six subcommands:

- `profile new <name> [--from <base>]` — create a profile, optionally as an overlay including `<base>`
- `profile exclude <name> <id>...` — append winget IDs to `exclude`
- `profile exclude-config <name> <id>...` — append config module IDs to `excludeConfigs`
- `profile add <name> <id>...` — append app entries to `apps`
- `profile show <name>` — summarize base, exclusions, additions, and net app count
- `profile list` — enumerate profiles across all three storage formats

Supporting behavior:

- **Only bare `.jsonc` profiles are mutable.** Zip and folder profiles are read-only; mutating them fails without side effects.
- **Mutations use `Read-ManifestRaw` + modify + `Write-Manifest`**, never `Read-Manifest`, so resolved include content is never flattened into the overlay file.
- **Appends are idempotent** — entries already present are skipped, and each subcommand reports the count actually added.
- **`--json` emits a standard envelope** for every subcommand, with stable error codes and exit 1 on failure.

## Impact

- Affected specs: `profile-commands`
- Affected code: new `engine/profile-commands.ps1`; `bin/endstate.ps1` (dispatch, capabilities, help); `engine/manifest.ps1` (`ConvertTo-Jsonc` serializes `exclude` / `excludeConfigs`)
- Depends on the `profile-composition` change, whose fields these subcommands author
- **Additive.** A new top-level command; no existing command's behavior or output changes.
