# Export-Config Command

## Overview

The `export-config` command captures configuration files from your system and exports them to a structured folder. This is the **inverse of restore**: it reads `restore[]` entries from a manifest and copies files from system locations to the export folder.

## Purpose

- **Backup configurations** before system changes
- **Migrate settings** between machines
- **Version control** application configurations
- **Share configurations** with team members

## How It Works

1. Reads `restore[]` entries from your manifest
2. For each entry, copies from `target` (system path) → `source` (export folder)
3. Creates `manifest.snapshot.jsonc` in the export folder
4. Skips missing files, warns on sensitive paths

## Usage

### Basic Export

```powershell
.\bin\cli.ps1 -Command export-config -Manifest .\manifests\my-machine.jsonc
```

This exports to `<manifestDir>/export/` by default.

### Custom Export Path

```powershell
.\bin\cli.ps1 -Command export-config -Manifest .\manifests\my-machine.jsonc -Export C:\Backups\configs
```

### Preview (Dry-Run)

```powershell
.\bin\cli.ps1 -Command export-config -Manifest .\manifests\my-machine.jsonc -DryRun
```

Shows what would be exported without copying files.

## Manifest Structure

Define files to export in the `restore[]` section of your manifest:

```jsonc
{
  "version": 1,
  "name": "my-config",
  "apps": [],
  
  "restore": [
    {
      "type": "copy",
      "source": "./configs/app/settings.json",
      "target": "C:\\Users\\YourName\\AppData\\Local\\App\\settings.json",
      "backup": true
    },
    {
      "type": "copy",
      "source": "./configs/app/profiles",
      "target": "C:\\Users\\YourName\\AppData\\Local\\App\\Profiles",
      "backup": true
    }
  ],
  
  "verify": []
}
```

### Path Resolution

- **`source`**: Relative path in export folder (e.g., `./configs/app/settings.json`)
- **`target`**: Absolute system path (e.g., `C:\Users\...\settings.json`)
- **Environment variables**: Use `~` for user home directory

## Export Folder Structure

After running `export-config`, your export folder contains:

```
export/
├── manifest.snapshot.jsonc    # Copy of manifest at export time
└── configs/
    └── app/
        ├── settings.json
        └── profiles/
            ├── profile1.json
            └── profile2.json
```

## Machine ID

The export folder structure uses the machine name from `$env:COMPUTERNAME`. This allows organizing exports by machine:

```
manifests/
└── export/
    └── <MACHINE-NAME>/
        └── <capability-id>/
            └── ...
```

## Workflow: Export → Restore

### 1. Export Configuration

On source machine:

```powershell
.\bin\cli.ps1 -Command export-config -Manifest .\manifests\my-app.jsonc
```

### 2. Validate Export

```powershell
.\bin\cli.ps1 -Command validate-export -Manifest .\manifests\my-app.jsonc
```

Checks:
- All `restore[].source` paths exist in export
- Targets are writable on current machine
- Snapshot manifest exists

### 3. Transfer Export Folder

Copy the entire export folder to the target machine.

### 4. Restore on Target Machine

```powershell
.\bin\cli.ps1 -Command restore -Manifest .\manifests\my-app.jsonc -EnableRestore -DryRun
```

Preview first, then:

```powershell
.\bin\cli.ps1 -Command restore -Manifest .\manifests\my-app.jsonc -EnableRestore
```

### 5. Revert if Needed

If restore causes issues:

```powershell
.\bin\cli.ps1 -Command revert
```

Restores the last backup created by the restore operation.

## Example: MSI Afterburner

See `manifests/examples/msi-afterburner.jsonc` for a complete example.

### Manifest

```jsonc
{
  "version": 1,
  "name": "msi-afterburner-config",
  "apps": [],
  
  "restore": [
    {
      "type": "copy",
      "source": "./configs/msi-afterburner/MSIAfterburner.cfg",
      "target": "C:\\Program Files (x86)\\MSI Afterburner\\MSIAfterburner.cfg",
      "backup": true
    },
    {
      "type": "copy",
      "source": "./configs/msi-afterburner/Profiles",
      "target": "C:\\Program Files (x86)\\MSI Afterburner\\Profiles",
      "backup": true
    }
  ],
  
  "verify": []
}
```

### Export

```powershell
# Preview what will be exported
.\bin\cli.ps1 -Command export-config -Manifest .\manifests\examples\msi-afterburner.jsonc -DryRun

# Export for real
.\bin\cli.ps1 -Command export-config -Manifest .\manifests\examples\msi-afterburner.jsonc
```

### Result

```
manifests/examples/export/
├── manifest.snapshot.jsonc
└── configs/
    └── msi-afterburner/
        ├── MSIAfterburner.cfg
        └── Profiles/
            ├── Profile1.cfg
            └── Profile2.cfg
```

## Safety Features

### Sensitive Path Detection

The engine warns when exporting from sensitive locations:
- System directories
- Program Files
- Windows directory
- Other users' directories

Warnings are logged but export continues (you can review and abort if needed).

### Backup on Restore

When restoring, the `"backup": true` flag creates backups of existing files before overwriting. Use `revert` to restore these backups.

### Dry-Run Mode

Always test with `-DryRun` first to see what will be exported/restored without making changes.

## Troubleshooting

### "No restore entries found"

Your manifest needs a `restore[]` section with at least one entry. Add file paths you want to export.

### "Source not found in export"

When running `validate-export`, this means a file referenced in `restore[].source` doesn't exist in the export folder. Run `export-config` first to populate the export folder.

### "Target may not be writable"

When validating or restoring, this warns that you may not have permission to write to the target location. Run as Administrator if needed, or set `"requiresAdmin": true` in the restore entry.

### Files not exported

Check:
1. Does the `target` path exist on your system?
2. Is the path correct (check for typos)?
3. Run with `-DryRun` to see what would be exported

## Related Commands

- **`restore`**: Restore configurations from export folder to system
- **`validate-export`**: Validate export integrity before restore
- **`revert`**: Revert last restore operation
- **`apply`**: Full provisioning workflow (apps + restore)

## See Also

- [Config Export/Restore UX](./config-export-restore.md)
- [CLI JSON Contract](./cli-json-contract.md)
- Main README for general CLI usage
