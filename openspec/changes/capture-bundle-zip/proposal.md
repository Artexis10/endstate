# Proposal: Capture Bundle — Zip-Based Profile Packaging

## Problem

Capture currently produces a bare `.jsonc` manifest. Config export is a separate command (`export-config`). This creates a two-step workflow that breaks the user mental model of "package my machine, transfer, restore."

## Solution

Capture produces a single portable zip artifact containing both the app manifest and all available config module payloads. This becomes the unit of portability.

## Key Design Rules

- **Capture is greedy** — grab everything portable. Apply is conservative — install-only default, configs opt-in via `--enable-restore`.
- **Sensitive files never bundled** — enforced at capture time via `module.sensitive.files`.
- **Config capture failures don't block app capture** — install-only profiles are always valid and successful.
- **`export-config` preserved** — standalone power-user command, no breaking changes.

## Zip Structure

```
Hugo-Desktop.zip
├── manifest.jsonc          # App list (existing capture output)
├── configs/                # Config module payloads
│   ├── vscode/
│   │   ├── settings.json
│   │   └── keybindings.json
│   ├── claude-desktop/
│   │   └── claude_desktop_config.json
│   └── ...
└── metadata.json           # Capture timestamp, machine name, schema version
```

## Profile Discovery

Three formats recognized (resolution order: zip → folder → bare manifest):
1. `<name>.zip` — zip bundle (new, preferred)
2. `<name>\manifest.jsonc` — loose folder
3. `<name>.jsonc` — bare manifest (legacy, install-only)

## Non-Goals

- No `--zip` / `--no-zip` flags
- No encryption or signing
- No remote transfer protocol
- No GUI changes in this PR
- No changes to `restore` or `revert` commands
