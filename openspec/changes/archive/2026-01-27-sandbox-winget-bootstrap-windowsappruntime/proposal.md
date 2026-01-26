# Proposal: sandbox-winget-bootstrap-windowsappruntime

## Summary

Extend the winget bootstrap (Strategy A) to install the Windows App Runtime 1.8 dependency before installing App Installer/winget in Windows Sandbox.

## Problem

Windows Sandbox lacks the Microsoft.WindowsAppRuntime.1.8 framework that modern versions of winget (App Installer) require. The current bootstrap downloads and attempts to install App Installer, but fails with:

```
Deployment failed with HRESULT: 0x80073CF3
Package failed updates, dependency or conflict validation.
Windows cannot install package Microsoft.DesktopAppInstaller because this package depends on a framework that could not be found. Provide the framework "Microsoft.WindowsAppRuntime.1.8"...
```

## Solution

1. Before installing App Installer, download and install the Windows App Runtime 1.8 redistributable packages.
2. Log all steps to `winget-bootstrap.log` with clear diagnostics.
3. Preserve existing Strategy B (offline installer fallback) as the fallback path.

## Scope

- `sandbox-tests/discovery-harness/sandbox-validate.ps1` - Extend Ensure-Winget function
- `openspec/specs/sandbox-validation/spec.md` - Add WindowsAppRuntime requirement
- `docs/VALIDATION.md` - Update troubleshooting section

## Non-Goals

- Changing the offline installer fallback behavior
- Modifying host-side scripts
- Supporting other Windows App Runtime versions
