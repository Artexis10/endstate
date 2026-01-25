# Sandbox-Based Module Validation

This document describes how to use the automated Sandbox-based validation loop to test Endstate modules without touching the host environment.

## Overview

The validation loop performs a complete capture/restore cycle inside Windows Sandbox:

1. **Install** - Install the app via winget
2. **Seed** - Run the module's seed script (if present) to create representative config
3. **Capture** - Copy config files defined in the module's `capture` section
4. **Wipe** - Simulate data loss by moving captured files to backup
5. **Restore** - Restore files using the module's `restore` definitions
6. **Verify** - Run verification checks from the module's `verify` section

The result is a deterministic **PASS/FAIL** output with full artifacts for debugging.

## Prerequisites

- **Windows Sandbox** must be enabled on your system
  - Open "Turn Windows features on or off"
  - Check "Windows Sandbox"
  - Restart your computer
  - Or run: `Enable-WindowsOptionalFeature -Online -FeatureName 'Containers-DisposableClientVM'`

## Single-App Validation

Validate a single module:

```powershell
# By app ID (folder name in modules/apps/)
.\scripts\sandbox-validate.ps1 -AppId git

# By winget ID (auto-resolves to module)
.\scripts\sandbox-validate.ps1 -WingetId "Git.Git"

# Skip seed script
.\scripts\sandbox-validate.ps1 -AppId vscodium -Seed:$false

# Custom output directory
.\scripts\sandbox-validate.ps1 -AppId git -OutDir "C:\temp\git-validation"

# Generate .wsb without launching (for manual testing)
.\scripts\sandbox-validate.ps1 -AppId git -NoLaunch
```

### Parameters

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `-AppId` | No* | - | Module app ID (e.g., "git", "vscodium") |
| `-WingetId` | No* | - | Winget package ID (e.g., "Git.Git") |
| `-Seed` | No | `$true` | Run seed script if present |
| `-OutDir` | No | Auto | Output directory for artifacts |
| `-NoLaunch` | No | `$false` | Generate .wsb without launching |

*Either `-AppId` or `-WingetId` is required.

### Output

Artifacts are written to `sandbox-tests/validation/<appId>/<timestamp>/`:

```
sandbox-tests/validation/git/20250125-120500/
├── validate.wsb           # Sandbox configuration
├── STARTED.txt            # Startup marker
├── STEP.txt               # Current step tracker
├── DONE.txt               # Completion sentinel (or ERROR.txt)
├── result.json            # Structured result
├── install.log            # Winget install output
├── seed.log               # Seed script output (if ran)
├── capture/               # Captured config files
├── wipe-backup/           # Wiped files (backup)
├── capture-manifest.json  # Capture details
├── wipe-manifest.json     # Wipe details
├── restore-manifest.json  # Restore details
└── verify-manifest.json   # Verification results
```

## Batch Validation

Validate multiple modules from a queue file:

```powershell
# Use default golden queue
.\scripts\sandbox-validate-batch.ps1

# Custom queue file
.\scripts\sandbox-validate-batch.ps1 -QueueFile "my-queue.jsonc"

# Stop on first failure
.\scripts\sandbox-validate-batch.ps1 -StopOnFail

# Custom output directory
.\scripts\sandbox-validate-batch.ps1 -OutDir "C:\temp\batch-validation"
```

### Parameters

| Parameter | Required | Default | Description |
|-----------|----------|---------|-------------|
| `-QueueFile` | No | `sandbox-tests/golden-queue.jsonc` | Queue file path |
| `-OutDir` | No | Auto | Base output directory |
| `-StopOnFail` | No | `$false` | Stop on first failure |

### Queue File Format

The queue file is JSONC with an `apps` array:

```jsonc
{
  // Golden Queue: Apps to validate in Sandbox
  "apps": [
    { "appId": "git", "wingetId": "Git.Git" },
    { "appId": "vscodium", "wingetId": "VSCodium.VSCodium" },
    { "appId": "powertoys", "wingetId": "Microsoft.PowerToys" },
    { "appId": "msi-afterburner", "wingetId": "Guru3D.Afterburner" }
  ]
}
```

### Output

Batch artifacts are written to `sandbox-tests/validation/batch/<timestamp>/`:

```
sandbox-tests/validation/batch/20250125-120500/
├── summary.json           # JSON summary of all results
├── summary.md             # Human-readable markdown summary
├── git/                   # Per-app validation artifacts
│   ├── result.json
│   └── ...
├── vscodium/
│   └── ...
└── ...
```

## Interpreting Results

### PASS

A validation passes when:
- App installs successfully
- Seed script runs without error (if present and enabled)
- Config files are captured
- Files are successfully wiped
- Files are restored from capture
- All verification checks pass

### FAIL

A validation fails when any stage encounters an error:
- `install` - Winget installation failed
- `seed` - Seed script returned non-zero exit code
- `capture` - Required capture source not found
- `restore` - Required restore source not found
- `verify` - One or more verification checks failed

The `result.json` file contains:
- `status`: "PASS" or "FAIL"
- `failedStage`: Which stage failed (if FAIL)
- `failReason`: Human-readable failure reason
- Counts for captured/wiped/restored files
- Verification pass/total counts

## Troubleshooting

### Sandbox doesn't start
- Ensure Windows Sandbox is enabled
- Check that Hyper-V is enabled (required for Sandbox)
- Verify virtualization is enabled in BIOS

### Winget not available in Sandbox
- The harness attempts to bootstrap winget automatically
- If bootstrap fails, check `install.log` for details
- Some packages require Windows App Runtime which may take time to install

### Validation times out
- Default timeout is 15 minutes (900 seconds)
- Large apps or slow network may need more time
- Check `STEP.txt` to see where it stopped
- Check `HEARTBEAT.txt` for progress markers

### Verification fails after restore
- Check `verify-manifest.json` for which checks failed
- Compare `capture/` contents with expected paths
- Ensure module's `restore` paths match `capture` paths

## Adding New Modules to Validation

1. Create the module in `modules/apps/<appId>/module.jsonc`
2. Ensure the module has:
   - `matches.winget` with at least one winget ID
   - `capture.files` defining what to capture
   - `restore` defining how to restore
   - `verify` defining verification checks
3. Optionally add a `seed.ps1` script to create representative config
4. Add to `sandbox-tests/golden-queue.jsonc` for batch validation
5. Run single validation to test: `.\scripts\sandbox-validate.ps1 -AppId <appId>`

## CI Integration

The validation scripts return appropriate exit codes:
- `0` - All validations passed
- `1` - One or more validations failed

Example CI usage:
```yaml
- name: Validate modules
  run: .\scripts\sandbox-validate-batch.ps1 -StopOnFail
  shell: pwsh
```

Note: CI runners must have Windows Sandbox enabled, which requires nested virtualization support.
