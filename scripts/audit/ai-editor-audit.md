# AI Editor Audit Report

Generated: 2026-03-07

## Editor Installation Status

| Editor | Installed | Primary Use | Extensions |
|--------|-----------|-------------|------------|
| VS Code | NO | - | - |
| VSCodium | YES | Light use | 1 (open-remote-wsl) |
| Cursor | YES | Minimal use | 0 |
| Windsurf | YES | **Primary editor** | 11 (848 MB) |
| Claude Code | YES | Active CLI tool | N/A |
| Claude Desktop | YES | MCP + extensions | N/A |

## VS Code (NOT INSTALLED)

VS Code is not installed on this machine. The `code` command is not on PATH. No `%APPDATA%\Code\User\` directory exists. No `.vscode\extensions\` directory exists.

**Module status:** Module updated with tasks.json and extensions.json capture entries (will work when/if VS Code is installed). All entries are `optional: true`.

## VSCodium (INSTALLED - user's VS Code alternative)

VSCodium is the user's telemetry-free VS Code fork.

### What exists on disk
- `settings.json` (43 bytes) - minimal settings
- `snippets/` - empty directory
- No `keybindings.json` (no custom bindings set)
- No `tasks.json`
- `History/` (664 KB) - edit history (ephemeral, not captured)
- `workspaceStorage/` (1.1 MB, 31 workspaces) - per-workspace state (ephemeral)
- `globalStorage/state.vscdb` (258 KB) - state database (sensitive, excluded)
- 1 extension installed: `jeanp413.open-remote-wsl`

### Module changes
- Added `tasks.json` capture/restore entry
- Added `extensions.json` manifest capture from `%USERPROFILE%\.vscode-oss\extensions\`
- Updated notes with extension reinstall instructions (`codium --list-extensions` / `codium --install-extension`)

### Extension restore strategy
VSCodium has no built-in settings sync. The extension list can be captured via `codium --list-extensions` and restored via `codium --install-extension <id>`. The `extensions.json` manifest file provides a machine-readable alternative. A post-restore script could automate this.

## Cursor (INSTALLED - minimal use)

### What exists on disk
- `%USERPROFILE%\.cursor\argv.json` (509 bytes) - runtime args
- `%USERPROFILE%\.cursor\extensions\` - empty (0 extensions)
- `%APPDATA%\Cursor\User\` exists but no `settings.json`
- No custom rules, no MCP config

### Module changes
- Added `%USERPROFILE%\.cursor\rules\` capture/restore (AI global rules directory)
- Added `%USERPROFILE%\.cursor\mcp.json` capture/restore (MCP server config)
- Added `extensions.json` manifest capture
- Updated notes with AI-specific config documentation

### AI config assessment
Cursor stores AI conversation history and context in database files (not portable). The key portable config items are:
- **Rules directory** (`%USERPROFILE%\.cursor\rules\`) - user-authored AI instructions
- **MCP config** (`%USERPROFILE%\.cursor\mcp.json`) - MCP server definitions
- **Settings** - includes AI model preferences, inline completions config

Neither rules nor MCP config exist on this machine yet.

## Windsurf (INSTALLED - primary editor)

### What exists on disk

**Editor config (`%APPDATA%\Windsurf\User\`):**
- `settings.json` (412 bytes) - CAPTURED
- No `keybindings.json` (not customized)
- `snippets/` - empty
- `History/` (131 MB!) - edit history (ephemeral)
- `workspaceStorage/` (7.5 MB) - per-workspace state (ephemeral)

**Codeium AI config (`%USERPROFILE%\.codeium\windsurf\`):**
- `memories/global_rules.md` (3,717 bytes) - **USER-AUTHORED global AI rules** (HIGH VALUE)
- `mcp_config.json` (855 bytes) - **MCP server configuration** (HIGH VALUE)
- `windsurf/workflows/review.md` (1,316 bytes) - **custom workflow** (HIGH VALUE)
- `cascade/` - 50+ `.pb` files (protobuf, Cascade conversation history, NOT portable)
- `memories/` - 30+ `.pb` files (AI memories, protobuf, NOT portable)
- `implicit/` - 20+ `.pb` files (implicit context, NOT portable)
- `codemaps/codemapindex.json` - code indexing (rebuilt automatically)
- `code_tracker/` - active workspace tracking (ephemeral)
- `database/` - internal database (NOT portable)
- `installation_id` - device identity (SENSITIVE, excluded)
- `user_settings.pb` - user settings protobuf (SENSITIVE, auth-linked)
- `recipes/` - empty

**Extensions (`%USERPROFILE%\.windsurf\extensions\`):**
- 11 extensions, 848 MB total
- `extensions.json` manifest file captures the full list
- Notable: prettier, GitHub Actions, Docker, Playwright, Python, PowerShell, ChatGPT

### Module changes
- Added `global_rules.md` capture/restore (Codeium AI global rules)
- Added `mcp_config.json` capture/restore (MCP server config)
- Added `workflows/` capture/restore (custom Cascade workflows)
- Added `extensions.json` manifest capture
- Added `installation_id` and `user_settings.pb` to sensitive section
- Updated notes documenting AI config locations and limitations

### What CANNOT be captured (and why)
- **Cascade conversation history** (`cascade/*.pb`) - binary protobuf, tied to server-side session
- **AI memories** (`memories/*.pb`) - binary protobuf, not human-readable or portable
- **Implicit context** (`implicit/*.pb`) - binary protobuf, ephemeral
- **Code maps** (`codemaps/`) - rebuilt automatically per workspace
- **Database** (`database/`) - internal state, rebuilt

## Claude Code (INSTALLED - active)

### What exists on disk
- `settings.json` (0 bytes empty) - CAPTURED
- `.credentials.json` (452 bytes) - SENSITIVE, excluded
- `history.jsonl` (16 KB) - command history (ephemeral)
- `plugins/installed_plugins.json` - plugin list
- `plugins/blocklist.json` - plugin blocklist
- `projects/` - per-project config (correctly excluded)
- `todos/` - per-session tasks (correctly excluded)
- No global `CLAUDE.md` at `~/.claude/` level (per-project only)
- No `commands/` directory at global level (per-project only)

### Module assessment
Module is current and well-designed. The `CLAUDE.md` and `commands/` entries are correctly `optional: true` since they're per-project, not global. No changes needed beyond what was already configured.

## Claude Desktop (INSTALLED - active)

### What exists on disk
- `claude_desktop_config.json` (1,871 bytes) - CAPTURED
- `config.json` (1,989 bytes) - general config, NOW CAPTURED
- `extensions-installations.json` (7,147 bytes) - installed extensions, NOW CAPTURED
- `Claude Extensions/` (80 MB) - extension runtimes (NOT captured, too large)
- `vm_bundles/` (13 GB!) - extension VM bundles (definitely NOT captured)

### Module changes
- Added `config.json` capture/restore (general Electron/app config)
- Added `extensions-installations.json` capture (installed extension records)
- Updated notes about vm_bundles size

## Extension Restore Strategy Recommendation

The module schema is file-based (copy restorer), which means command-based capture/restore (`--list-extensions` / `--install-extension`) is not natively supported. Recommended approach:

1. **Capture:** The `extensions.json` manifest file (present in `.windsurf/extensions/`, `.vscode/extensions/`, `.cursor/extensions/`) contains a machine-readable list of installed extensions with IDs and versions. This is captured as a regular file.

2. **Restore:** A post-restore script (or seed.ps1) could parse `extensions.json` and run `<editor> --install-extension <id>` for each entry. This is not yet implemented but the pattern exists in the curation system.

3. **Workaround:** Until command-based restore is supported, users can run:
   ```powershell
   # For Windsurf
   windsurf --list-extensions | ForEach-Object { windsurf --install-extension $_ }
   # For VSCodium
   codium --list-extensions | ForEach-Object { codium --install-extension $_ }
   ```

## Landmines Discovered

1. **Windsurf AI config is split across two locations:** `%APPDATA%\Windsurf\User\` (VS Code fork settings) and `%USERPROFILE%\.codeium\windsurf\` (Codeium AI config). Both must be captured for full restoration.

2. **Codeium AI data is mostly protobuf:** Conversation history, memories, and implicit context are stored as binary `.pb` files that are NOT portable between installations. Only `global_rules.md`, `mcp_config.json`, and `workflows/` are human-readable and portable.

3. **Windsurf History is huge:** The `History/` directory under `%APPDATA%\Windsurf\User\` is 131 MB of edit history. Correctly excluded by the module.

4. **Claude Desktop vm_bundles is 13 GB:** Extension runtimes are stored locally and cannot be captured. Extensions must be reinstalled from the store.

5. **Cursor is essentially bare on this machine:** Installed but no extensions, no AI config, no custom settings. The module updates are forward-looking for when it gets configured.
