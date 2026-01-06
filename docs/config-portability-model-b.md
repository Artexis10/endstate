# Config Portability: Model B (Export Snapshot + Runtime Resolution)

## Overview

Model B provides GUI-grade config portability by separating manifest sources from runtime export snapshots. This approach enables:

- **Clean manifests**: Source paths remain relative and portable (e.g., `configs/app/settings.json`)
- **Runtime resolution**: Sources are resolved from export snapshots at restore time via `-Export` parameter
- **Fallback safety**: If export snapshot is missing, sources fallback to manifest directory (dev-friendly)
- **Deterministic revert**: Journal-based revert can restore backups AND delete newly created targets

## Architecture

### Model A (Legacy)
```
manifest.jsonc
├── restore[].source = "configs/app/settings.json"  (relative to manifest dir)
└── configs/
    └── app/
        └── settings.json  (source files live next to manifest)
```

**Problem**: Manifest directory becomes cluttered with config files, not portable.

### Model B (New)
```
manifest.jsonc
├── restore[].source = "configs/app/settings.json"  (still relative, but resolved at runtime)

export/  (snapshot root, passed via -Export at restore time)
├── manifest.snapshot.jsonc  (snapshot of manifest at export time)
└── configs/
    └── app/
        └── settings.json  (exported config files)
```

**Benefit**: Manifest stays clean, export snapshot is self-contained and portable.

## Workflow

### 1. Export Config (Create Snapshot)

```powershell
.\bin\cli.ps1 -Command export-config -Manifest .\manifests\my-machine.jsonc -Export .\snapshot
```

Creates:
- `.\snapshot\configs\app\settings.json` (exported from system)
- `.\snapshot\manifest.snapshot.jsonc` (manifest snapshot)

### 2. Validate Export (Before Restore)

```powershell
.\bin\cli.ps1 -Command validate-export -Manifest .\manifests\my-machine.jsonc -Export .\snapshot
```

Validates:
- All `restore[].source` paths exist in export snapshot
- Targets are writable
- Snapshot manifest matches active manifest (warns if different)

### 3. Restore with Export Snapshot (Model B)

```powershell
.\bin\cli.ps1 -Command restore -Manifest .\manifests\my-machine.jsonc -Export .\snapshot -EnableRestore
```

Source resolution:
1. Try `<ExportRoot>/<source>` first (e.g., `.\snapshot\configs\app\settings.json`)
2. Fallback to `<ManifestDir>/<source>` if not found (e.g., `.\manifests\configs\app\settings.json`)

Logs show which root was used for each entry.

### 4. Revert (Journal-Based)

```powershell
.\bin\cli.ps1 -Command revert
```

Revert algorithm (processes journal entries in **reverse order**):
- **If backup exists**: Restore backup to target
- **If target was created by restore** (`targetExistedBefore=false`): Delete target
- **Otherwise**: Skip (no backup and target existed before)

Safety: Creates backup before reverting (even when deleting).

## Restore Journaling

Every non-dry-run restore writes a journal to `./logs/restore-journal-<RunId>.json`:

```json
{
  "runId": "20250105-223045-abc123",
  "manifestPath": "C:\\manifests\\my-machine.jsonc",
  "manifestDir": "C:\\manifests",
  "exportRoot": "C:\\snapshot",
  "timestamp": "2025-01-05T22:30:45Z",
  "entries": [
    {
      "kind": "copy",
      "source": "configs/app/settings.json",
      "target": "~/.config/app/settings.json",
      "resolvedSourcePath": "C:\\snapshot\\configs\\app\\settings.json",
      "targetPath": "C:\\Users\\user\\.config\\app\\settings.json",
      "backupRequested": true,
      "targetExistedBefore": false,
      "backupCreated": false,
      "backupPath": null,
      "action": "restored",
      "error": null
    }
  ]
}
```

### Journal Fields

- **kind**: Restore type (`copy`, `merge`, `append`)
- **source**: Original source path from manifest
- **target**: Original target path from manifest
- **resolvedSourcePath**: Actual source path used (export root or manifest dir)
- **targetPath**: Expanded target path
- **backupRequested**: Whether backup was requested in manifest
- **targetExistedBefore**: Whether target existed before restore (critical for revert)
- **backupCreated**: Whether backup was actually created
- **backupPath**: Path to backup file (if created)
- **action**: Outcome (`restored`, `skipped_up_to_date`, `skipped_missing_source`, `failed`)
- **error**: Error message (if action=failed)

## Revert Behavior

Revert finds the latest journal and processes entries in **reverse order**:

### Case 1: Backup Exists
```
Entry: targetExistedBefore=true, backupCreated=true, backupPath="..."
Action: Restore backup to target
```

### Case 2: Target Created by Restore
```
Entry: targetExistedBefore=false, backupCreated=false
Action: Delete target (it was created by restore)
```

### Case 3: No Backup, Target Existed
```
Entry: targetExistedBefore=true, backupCreated=false
Action: Skip (nothing to revert)
```

## CLI Reference

### restore
```powershell
.\bin\cli.ps1 -Command restore -Manifest <path> -EnableRestore [-Export <path>] [-DryRun]
```

**Parameters**:
- `-Manifest`: Path to manifest file
- `-EnableRestore`: Required opt-in flag
- `-Export`: Export snapshot root (Model B). If omitted, uses manifest dir (Model A)
- `-DryRun`: Preview without making changes

### validate-export
```powershell
.\bin\cli.ps1 -Command validate-export -Manifest <path> [-Export <path>]
```

**Parameters**:
- `-Manifest`: Path to manifest file
- `-Export`: Export snapshot root. If omitted, uses `<manifestDir>/export/`

### revert
```powershell
.\bin\cli.ps1 -Command revert [-DryRun]
```

**Parameters**:
- `-DryRun`: Preview revert without making changes

Automatically finds the latest restore journal.

## Best Practices

1. **Always validate before restore**:
   ```powershell
   .\bin\cli.ps1 -Command validate-export -Manifest .\manifest.jsonc -Export .\snapshot
   .\bin\cli.ps1 -Command restore -Manifest .\manifest.jsonc -Export .\snapshot -EnableRestore
   ```

2. **Use dry-run first**:
   ```powershell
   .\bin\cli.ps1 -Command restore -Manifest .\manifest.jsonc -Export .\snapshot -EnableRestore -DryRun
   ```

3. **Keep export snapshots portable**:
   - Export snapshot directory is self-contained
   - Can be zipped and transferred to other machines
   - Includes `manifest.snapshot.jsonc` for reference

4. **Revert is safe**:
   - Creates safety backups before reverting
   - Only reverts entries that were actually restored
   - Deletes targets that were created by restore

## Migration from Model A

Model A (legacy) still works:
```powershell
.\bin\cli.ps1 -Command restore -Manifest .\manifest.jsonc -EnableRestore
```

To migrate to Model B:
1. Export config to create snapshot:
   ```powershell
   .\bin\cli.ps1 -Command export-config -Manifest .\manifest.jsonc -Export .\snapshot
   ```

2. Use `-Export` for future restores:
   ```powershell
   .\bin\cli.ps1 -Command restore -Manifest .\manifest.jsonc -Export .\snapshot -EnableRestore
   ```

3. Optionally remove config files from manifest directory (now in export snapshot)

## Troubleshooting

### Source not found in export
```
[ERROR] source not found: C:\snapshot\configs\app\settings.json (tried export-root)
```

**Solution**: Run `validate-export` to check which sources are missing, then re-export or fix paths.

### Revert doesn't delete target
```
SKIP: entry was not restored (action: skipped_up_to_date)
```

**Explanation**: Revert only reverts entries that were actually restored. If restore skipped an entry (already up-to-date), revert also skips it.

### Journal not found
```
No restore operation found to revert.
```

**Solution**: Run a non-dry-run restore first. Dry-run restores don't create journals.
