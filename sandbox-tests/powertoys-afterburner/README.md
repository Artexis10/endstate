# Sandbox Contract Test: PowerToys + MSI Afterburner

CLI-to-filesystem contract testing harness for Windows Sandbox.

## Purpose

This is a **CLI-to-filesystem contract test**. It validates:
- `endstate.cmd apply` executes without error on a clean profile
- Declared files land at correct absolute paths
- Idempotency: second apply succeeds with no changes
- Deterministic restore behavior

## What This Tests

✅ **CLI contract stability** (`endstate.cmd` invocation)  
✅ **Filesystem side effects** (files created at correct paths)  
✅ **Idempotency** (second apply is safe)  
✅ **Module resolution** (modules expand to restore entries)

## What This Does NOT Test

❌ **App installation** (no winget, no MSI installers)  
❌ **App behavior** (no launching PowerToys, no GPU overclocking)  
❌ **UI assertions** (no screenshots, no pixel matching)  
❌ **Registry writes** (deferred to future scope)

## Prerequisites

- Windows 10/11 with Windows Sandbox enabled
- Endstate repo cloned locally

## Running Locally (Outside Sandbox)

```powershell
# From repo root
pwsh sandbox-tests\powertoys-afterburner\run.ps1
```

## Running in Windows Sandbox

### Option 1: Use the .wsb file

Double-click `powertoys-afterburner.wsb` to launch Sandbox with the repo mapped.

Then in Sandbox:
```powershell
cd C:\Endstate
pwsh sandbox-tests\powertoys-afterburner\run.ps1
```

### Option 2: Manual Setup

1. Create a `.wsb` file with this content:

```xml
<Configuration>
  <MappedFolders>
    <MappedFolder>
      <HostFolder>C:\path\to\endstate</HostFolder>
      <SandboxFolder>C:\Endstate</SandboxFolder>
      <ReadOnly>false</ReadOnly>
    </MappedFolder>
  </MappedFolders>
  <LogonCommand>
    <Command>powershell -NoExit -Command "cd C:\Endstate; Write-Host 'Endstate repo mapped. Run: pwsh sandbox-tests\powertoys-afterburner\run.ps1'"</Command>
  </LogonCommand>
</Configuration>
```

2. Save as `test.wsb` and double-click to launch
3. Run the test script in the Sandbox terminal

## Sentinel Files

The harness checks these paths after apply:

| Module | Sentinel Path |
|--------|---------------|
| PowerToys | `%LOCALAPPDATA%\Microsoft\PowerToys\settings.json` |
| MSI Afterburner | `C:\Program Files (x86)\MSI Afterburner\MSIAfterburner.cfg` |
| MSI Afterburner | `C:\Program Files (x86)\MSI Afterburner\Profiles\` |

## Expected Behavior

In a fresh Sandbox without source config files, the harness will:
- Run apply successfully (exit 0)
- Report sentinel files as missing (expected - no source configs to restore)
- Run apply again successfully (idempotent)

The test validates the **engine contract**, not that files are actually restored.

## Exit Codes

- `0` - All assertions passed
- `1` - One or more assertions failed
