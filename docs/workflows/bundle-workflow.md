# Bundle Workflow

## Overview

The bundle workflow enables reliable configuration capture and restore across machines. A **bundle** is a local folder containing configuration files and a manifest snapshot, allowing you to move settings between systems.

---

## Bundle Convention

### Structure

A bundle is a **local folder** with the following structure:

```
<manifestDir>/bundle/
├── manifest.snapshot.jsonc    # Manifest copy at capture time
└── <relative-paths>            # Config files referenced in restore[]
```

**Default location**: `<manifestDir>/bundle/`  
**Custom location**: Use `--bundle <path>` flag

### Key Principles

1. **Manifest is single source of truth**: The engine always executes against the manifest explicitly provided by the user
2. **Snapshot for reference only**: `manifest.snapshot.jsonc` is never auto-loaded; it exists for comparison/audit
3. **Relative paths preserved**: Bundle maintains the same relative structure as defined in `restore[].source`
4. **No encryption**: Bundle is a plain folder (encryption/compression out of scope for MVP)

---

## Commands

### 1. `capture-config` - Capture Configuration to Bundle

Reads `restore[]` entries from manifest, resolves targets on current system, and copies them to bundle.

**Usage:**
```powershell
.\cli.ps1 -Command capture-config -Manifest <path> [-Bundle <path>]
```

**Behavior:**
- Reads `restore[]` entries from active manifest
- For each entry: copies `target` (system path) → `source` (bundle path)
- Creates directories as needed
- Copies active manifest to `bundle/manifest.snapshot.jsonc`
- **Does NOT modify** the original manifest

**Safety:**
- Warns on sensitive paths (`.ssh`, `.aws`, `.gnupg`, etc.)
- Skips if source not found on system
- Continues with warnings (does not fail on sensitive paths)

**Example:**
```powershell
# Capture configs from current system to bundle
.\cli.ps1 -Command capture-config -Manifest .\manifests\my-machine.jsonc

# Capture to custom bundle location
.\cli.ps1 -Command capture-config -Manifest .\manifests\my-machine.jsonc -Bundle C:\Backups\my-bundle
```

---

### 2. `validate-bundle` - Validate Bundle Integrity

Validates that a bundle is complete and ready for restore.

**Usage:**
```powershell
.\cli.ps1 -Command validate-bundle -Manifest <path> [-Bundle <path>]
```

**Validation Checks:**
- All `restore[].source` paths exist in bundle
- Targets are writable (or require elevation)
- Snapshot manifest exists (warn if missing)
- Snapshot differs from active manifest (warn if mismatch)

**Fail-fast:** Missing sources cause validation failure.

**Example:**
```powershell
# Validate bundle before restore
.\cli.ps1 -Command validate-bundle -Manifest .\manifests\my-machine.jsonc
```

---

### 3. `restore` - Apply Configuration from Bundle

Executes restore operations using bundle contents (existing command, no changes).

**Usage:**
```powershell
.\cli.ps1 -Command restore -Manifest <path> -EnableRestore [-DryRun]
```

**Behavior:**
- Requires explicit `--enable-restore` flag (opt-in for safety)
- Uses bundle paths correctly (resolves `source` relative to manifest directory)
- Emits `phase=restore` events
- Creates backups before overwriting (in `state/backups/<runId>/`)

**Example:**
```powershell
# Dry-run restore
.\cli.ps1 -Command restore -Manifest .\manifests\my-machine.jsonc -EnableRestore -DryRun

# Execute restore
.\cli.ps1 -Command restore -Manifest .\manifests\my-machine.jsonc -EnableRestore
```

---

## Complete Workflow

### Machine A (Source) → Machine B (Target)

#### On Machine A (Capture):

```powershell
# 1. Create manifest with restore entries
# Edit manifests/my-setup.jsonc:
{
  "version": 1,
  "name": "my-setup",
  "restore": [
    { "type": "copy", "source": "./configs/.gitconfig", "target": "~/.gitconfig", "backup": true },
    { "type": "copy", "source": "./configs/settings.json", "target": "$env:APPDATA/Code/User/settings.json", "backup": true }
  ]
}

# 2. Capture configs to bundle
.\cli.ps1 -Command capture-config -Manifest .\manifests\my-setup.jsonc

# Result: bundle/ folder created with:
#   - bundle/manifest.snapshot.jsonc
#   - bundle/configs/.gitconfig
#   - bundle/configs/settings.json
```

#### Transfer Bundle:

```powershell
# Copy manifest + bundle to Machine B
# Example: via USB drive, network share, or cloud sync
Copy-Item -Path .\manifests\my-setup.jsonc -Destination D:\Transfer\
Copy-Item -Path .\manifests\bundle -Destination D:\Transfer\ -Recurse
```

#### On Machine B (Restore):

```powershell
# 1. Validate bundle integrity
.\cli.ps1 -Command validate-bundle -Manifest D:\Transfer\my-setup.jsonc

# 2. Preview restore (dry-run)
.\cli.ps1 -Command restore -Manifest D:\Transfer\my-setup.jsonc -EnableRestore -DryRun

# 3. Execute restore
.\cli.ps1 -Command restore -Manifest D:\Transfer\my-setup.jsonc -EnableRestore
```

---

## Safety Rules

### Sensitive Path Detection

The following path segments trigger warnings during capture:

- `.ssh` - SSH keys
- `.aws` - AWS credentials
- `.azure` - Azure credentials
- `.gnupg` / `.gpg` - GPG keys
- `credentials` - Generic credentials
- `secrets` - Secret files
- `tokens` - API tokens
- `.kube` - Kubernetes configs
- `.docker` - Docker credentials
- `id_rsa` / `id_ed25519` / `id_ecdsa` - SSH private keys

**Behavior:**
- **Capture**: Warns but continues (does not skip)
- **Restore**: Warns during validation and execution
- **User responsibility**: Review warnings and ensure secrets are handled appropriately

### No Auto-Secret Capture

The engine **never** auto-discovers or captures secrets. All capture is explicit via `restore[]` entries defined by the user.

### Backup-First Restore

Restore operations:
- Backup existing targets before overwriting
- Backup location: `state/backups/<runId>/`
- Backup preserves original path structure
- Skip if target already matches source (idempotent)

---

## Manifest Snapshot Behavior

### Purpose

`bundle/manifest.snapshot.jsonc` serves as:
- Audit trail: What manifest was active during capture
- Drift detection: Compare snapshot vs. active manifest
- Documentation: Self-contained bundle reference

### Rules

1. **Never auto-loaded**: Engine always uses the manifest explicitly provided via `--manifest`
2. **Warn on mismatch**: `validate-bundle` warns if snapshot differs from active manifest
3. **Not required**: Missing snapshot triggers warning, not failure
4. **Read-only**: Snapshot is never modified after creation

### Example Scenario

```powershell
# Capture with manifest v1
.\cli.ps1 -Command capture-config -Manifest .\manifests\setup.jsonc
# Creates: bundle/manifest.snapshot.jsonc (copy of setup.jsonc)

# User edits setup.jsonc (adds new restore entry)

# Validate warns about mismatch
.\cli.ps1 -Command validate-bundle -Manifest .\manifests\setup.jsonc
# Output: "WARNING: Snapshot manifest differs from active manifest"

# Restore uses active manifest (not snapshot)
.\cli.ps1 -Command restore -Manifest .\manifests\setup.jsonc -EnableRestore
# Uses current setup.jsonc, not snapshot
```

---

## Streaming Events

When using `--events jsonl`, commands emit structured events:

### capture-config Events

```jsonl
{"phase":"capture","status":"started","message":"Starting bundle capture"}
{"phase":"capture","itemId":"copy:./configs/.gitconfig->~/.gitconfig","status":"success","message":"Captured to bundle"}
{"phase":"capture","status":"completed","message":"Bundle capture completed"}
```

### validate-bundle Events

```jsonl
{"phase":"validate-bundle","status":"started","message":"Starting bundle validation"}
{"phase":"validate-bundle","itemId":"copy:./configs/.gitconfig->~/.gitconfig","status":"success","message":"Valid"}
{"phase":"validate-bundle","status":"completed","message":"Bundle validation completed"}
```

---

## Limitations (MVP Scope)

### Out of Scope

- **Remote bundles**: Bundle must be local folder
- **Encryption**: No built-in encryption (use OS-level tools)
- **Compression**: No zip/archive support (use external tools if needed)
- **Incremental capture**: Full capture only (no delta/diff)
- **Auto-discovery**: No automatic config file detection

### Workarounds

**Encryption:**
```powershell
# Encrypt bundle folder with Windows EFS or third-party tools
cipher /e /s:.\manifests\bundle
```

**Compression:**
```powershell
# Compress bundle for transfer
Compress-Archive -Path .\manifests\bundle -DestinationPath bundle.zip
```

---

## Troubleshooting

### "Source not found in bundle"

**Cause**: `restore[].source` path doesn't exist in bundle.

**Fix:**
1. Check bundle path is correct
2. Re-run `capture-config` to populate bundle
3. Verify `restore[].source` paths in manifest

### "Target may not be writable"

**Cause**: Current user lacks write permission to target path.

**Fix:**
1. Run as Administrator if `requiresAdmin: true`
2. Check target path permissions
3. Ensure parent directories exist

### "Snapshot manifest differs from active manifest"

**Cause**: Manifest was edited after capture.

**Impact**: Warning only, not a failure.

**Fix (if needed):**
1. Review changes in active manifest
2. Re-run `capture-config` to update snapshot
3. Or ignore if changes are intentional

---

## Best Practices

1. **Version control manifests**: Commit manifests to git, exclude bundles
2. **Test with dry-run**: Always use `-DryRun` before real restore
3. **Validate before restore**: Run `validate-bundle` to catch issues early
4. **Review sensitive paths**: Check warnings and ensure secrets are handled properly
5. **Document bundle location**: Note where bundle is stored/transferred
6. **Keep bundles local**: Don't commit bundles to version control (add to `.gitignore`)

---

## See Also

- [Restore System](../engine/restore.ps1) - Restore implementation
- [Manifest Format](../README.md#manifest-format) - Manifest schema
- [Project Ruleset](.windsurf/rules/project-ruleset.md) - Engineering discipline
