# Configuration Export & Restore

## Overview

Endstate provides a safe, explicit configuration export and restore system for transferring machine configurations between systems. This system is designed for **machine-to-machine profile transfer** and follows strict safety principles.

## Core Principles

### Terminology

- **Export** (not "bundle"): The process of capturing configuration files from the system
- **Restore**: The process of applying configuration files to the system
- **Revert**: The process of undoing a restore operation

### Single Manifest

- Uses the existing JSONC manifest format
- No second manifest introduced
- The `restore[]` section defines what configs are exportable/restorable

### Explicit Opt-In

- Config restore must be explicitly enabled by the user
- No automatic config restore during normal apply
- Export and restore are separate, intentional operations

### Safety First

- Restore always backs up existing configs before overwriting
- Backups are recoverable via the `revert` command
- No secrets auto-captured (warns on sensitive paths)
- All operations are deterministic and reversible

## Architecture

```
Export Flow:
  Manifest restore[] → System Files → Export Folder
  
Restore Flow:
  Export Folder → Backup Current → System Files
  
Revert Flow:
  Restore Backups → Backup Current → System Files
```

## Export Folder Structure

```
<manifest-dir>/
  ├── manifest.jsonc              # Main manifest
  └── export/                     # Export folder (default location)
      ├── manifest.snapshot.jsonc # Snapshot of manifest at export time
      └── <relative-paths>        # Config files as defined in restore[]
```

## Commands

### 1. Export Configuration

Captures configuration files from the system to an export folder.

```powershell
# Export configs defined in manifest
.\cli.ps1 -Command export-config -Manifest .\manifests\my-machine.jsonc

# Export to custom location
.\cli.ps1 -Command export-config -Manifest .\manifests\my-machine.jsonc -Export .\my-export
```

**Behavior:**
- Reads `restore[]` entries from manifest
- Copies system targets → export folder
- Preserves relative paths
- Creates `manifest.snapshot.jsonc` for audit
- Warns (does not fail) on sensitive paths

**Output:**
- Exported config files in export folder
- Snapshot manifest for validation
- State file: `state/export-<runId>.json`

### 2. Validate Export

Validates that an export is complete and ready for restore.

```powershell
# Validate export
.\cli.ps1 -Command validate-export -Manifest .\manifests\my-machine.jsonc

# Validate custom export location
.\cli.ps1 -Command validate-export -Manifest .\manifests\my-machine.jsonc -Export .\my-export
```

**Validation Checks:**
- All `restore[].source` paths exist in export
- Targets are writable (or require elevation)
- Snapshot manifest exists (warns if missing)
- Snapshot matches current manifest (warns if different)

**Fails Fast:**
- Missing export files
- Unwritable targets (without proper elevation)

### 3. Restore from Export

Restores configuration files from export to system.

```powershell
# Preview restore (dry-run)
.\cli.ps1 -Command restore -Manifest .\manifests\my-machine.jsonc -EnableRestore -DryRun

# Execute restore
.\cli.ps1 -Command restore -Manifest .\manifests\my-machine.jsonc -EnableRestore
```

**Behavior:**
- Source paths resolve relative to export folder
- **Always backs up** existing configs before overwrite
- Backups stored per-run: `state/backups/<runId>/`
- Restore is deterministic and reversible
- Skips if target already matches source

**Safety:**
- Requires `-EnableRestore` flag (explicit opt-in)
- Creates timestamped backups
- Preserves original path structure in backups
- No silent overwrites

### 4. Revert Last Restore

Reverts the most recent restore operation by restoring backups.

```powershell
# Preview revert (dry-run)
.\cli.ps1 -Command revert -DryRun

# Execute revert
.\cli.ps1 -Command revert
```

**Behavior:**
- Finds the most recent restore run with backups
- Restores backed-up files to original locations
- Creates new backup before reverting (safety layer)
- Explicit user action only (no auto-rollback)

**Safety:**
- Only reverts the most recent restore
- Backs up current state before reverting
- Preserves revert history in state files

## Manifest Configuration

### Restore Section

The `restore[]` section in the manifest defines what configurations are exportable and restorable.

```jsonc
{
  "version": 1,
  "name": "my-machine",
  "apps": [...],
  
  "restore": [
    {
      "type": "copy",
      "source": "./configs/.gitconfig",
      "target": "~/.gitconfig",
      "backup": true
    },
    {
      "type": "copy",
      "source": "./configs/vscode-settings.json",
      "target": "~/AppData/Roaming/Code/User/settings.json",
      "backup": true
    }
  ]
}
```

### Restore Entry Fields

| Field | Required | Description |
|-------|----------|-------------|
| `type` | No | Restore type: `copy` (default), `merge`, `append` |
| `source` | Yes | Path relative to export folder |
| `target` | Yes | Absolute path on target system (supports `~` and env vars) |
| `backup` | No | Create backup before restore (default: `true`) |
| `requiresAdmin` | No | Requires elevated privileges (default: `false`) |

### Path Expansion

Paths support:
- `~` for user home directory
- Environment variables: `%USERPROFILE%`, `$HOME`
- Relative paths in `source` (resolved against export folder)

## Sensitive Paths

The system warns (but does not block) when encountering sensitive paths:

- `.ssh`, `.aws`, `.azure`, `.gnupg`, `.gpg`
- `credentials`, `secrets`, `tokens`
- `.kube`, `.docker`
- `id_rsa`, `id_ed25519`, `id_ecdsa`

**Warning Behavior:**
- Export: Warns but continues
- Validate: Warns but passes validation
- Restore: Warns but proceeds if `-EnableRestore` is set

**Recommendation:** Do not export/restore secrets. Use proper secret management tools.

## State Files

### Export State

Location: `state/export-<runId>.json`

```json
{
  "runId": "abc123",
  "timestamp": "2025-01-05T12:00:00Z",
  "command": "export",
  "manifest": {
    "path": "manifests/my-machine.jsonc",
    "name": "my-machine",
    "hash": "sha256..."
  },
  "export": {
    "path": "manifests/export"
  },
  "summary": {
    "captured": 5,
    "skip": 0,
    "fail": 0,
    "warn": 1
  },
  "actions": [...]
}
```

### Restore State

Location: `state/restore-<runId>.json`

```json
{
  "runId": "def456",
  "timestamp": "2025-01-05T12:05:00Z",
  "command": "restore",
  "dryRun": false,
  "manifest": {
    "path": "manifests/my-machine.jsonc",
    "name": "my-machine",
    "hash": "sha256..."
  },
  "summary": {
    "restore": 5,
    "skip": 0,
    "fail": 0
  },
  "actions": [...]
}
```

### Revert State

Location: `state/revert-<runId>.json`

```json
{
  "runId": "ghi789",
  "timestamp": "2025-01-05T12:10:00Z",
  "command": "revert",
  "dryRun": false,
  "revertedRestoreRunId": "def456",
  "summary": {
    "reverted": 5,
    "skip": 0,
    "fail": 0
  },
  "actions": [...]
}
```

## Backup Structure

Backups are stored in: `state/backups/<runId>/`

Structure preserves original paths:
```
state/backups/def456/
  └── C/
      └── Users/
          └── username/
              └── .gitconfig
```

## Workflow Examples

### Machine-to-Machine Transfer

**Source Machine:**
```powershell
# 1. Capture current state
.\cli.ps1 -Command capture -Profile source-machine

# 2. Export configurations
.\cli.ps1 -Command export-config -Manifest .\manifests\source-machine.jsonc

# 3. Copy manifest + export folder to target machine
```

**Target Machine:**
```powershell
# 1. Validate export
.\cli.ps1 -Command validate-export -Manifest .\manifests\source-machine.jsonc

# 2. Preview restore
.\cli.ps1 -Command restore -Manifest .\manifests\source-machine.jsonc -EnableRestore -DryRun

# 3. Execute restore
.\cli.ps1 -Command restore -Manifest .\manifests\source-machine.jsonc -EnableRestore

# 4. If needed, revert
.\cli.ps1 -Command revert
```

### Update Existing Export

```powershell
# Re-export to update configurations
.\cli.ps1 -Command export-config -Manifest .\manifests\my-machine.jsonc

# Validate updated export
.\cli.ps1 -Command validate-export -Manifest .\manifests\my-machine.jsonc
```

## Safety Guarantees

1. **No Automatic Restore**: Config restore never happens automatically during `apply`
2. **Explicit Opt-In**: Restore requires `-EnableRestore` flag
3. **Always Backup**: Existing configs are backed up before overwrite
4. **Reversible**: Revert command restores previous state
5. **Deterministic**: Same inputs produce same outputs
6. **Fail-Safe**: Missing sources fail validation before restore
7. **Audit Trail**: All operations logged in state files

## Non-Goals

The following are **intentionally not supported**:

- ❌ Second manifest format
- ❌ Encrypted exports
- ❌ Cloud/remote exports
- ❌ Auto-merge or diff logic
- ❌ Secrets capture
- ❌ Automatic config restore during apply

## Error Handling

### Export Errors

- **Missing system file**: Skipped with warning
- **Sensitive path**: Warned but continues
- **Write failure**: Fails with error

### Validate Errors

- **Missing export**: Fails validation
- **Unwritable target**: Warns (fails if not elevated when required)
- **Snapshot mismatch**: Warns but passes

### Restore Errors

- **Missing source**: Fails restore
- **Backup failure**: Fails restore (safety first)
- **Copy failure**: Fails restore

### Revert Errors

- **No restore found**: Completes with message
- **Backup failure**: Fails revert (safety first)
- **Restore failure**: Fails revert

## Best Practices

1. **Always validate** before restore
2. **Use dry-run** to preview changes
3. **Keep exports versioned** (e.g., in git)
4. **Document sensitive exclusions** in manifest comments
5. **Test revert** in safe environment first
6. **Avoid secrets** in exports
7. **Use meaningful manifest names** for clarity
8. **Review snapshot warnings** before restore

## Integration with GUI

The GUI will provide:
- Visual export preview (what will be exported)
- Visual restore preview (what will be overwritten)
- Clear backup location display
- One-click revert of last restore
- Sensitive path warnings in UI
- Color-coded status (no icons, text only)

See GUI implementation notes for UX flows.
