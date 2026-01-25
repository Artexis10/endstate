# Offline Installers Directory

This directory holds offline installer files for the **Strategy B fallback** when winget bootstrap fails in Windows Sandbox.

## When to Use

Offline installers are only needed when:
1. Winget bootstrap fails in Windows Sandbox (rare)
2. You want deterministic validation without network dependency

Most apps should work with the default winget bootstrap (Strategy A).

## How to Add an Offline Installer

1. Download the installer for your app (`.exe`, `.msi`, or `.msixbundle`)
2. Place it in this directory
3. Update `sandbox-tests/golden-queue.jsonc` with installer metadata:

```jsonc
{
  "appId": "your-app",
  "wingetId": "Publisher.AppName",
  "installer": {
    "path": "sandbox-tests/installers/your-installer.exe",
    "silentArgs": "/S",
    "exePath": "C:\\Program Files\\YourApp\\app.exe"
  }
}
```

## Installer Metadata Fields

| Field | Required | Description |
|-------|----------|-------------|
| `path` | Yes | Relative path to installer from repo root |
| `silentArgs` | Yes | Command-line args for silent/unattended install |
| `exePath` | No | Path to verify installation succeeded |

## Common Silent Install Arguments

| Installer Type | Silent Args |
|----------------|-------------|
| NSIS (.exe) | `/S` |
| Inno Setup (.exe) | `/VERYSILENT /NORESTART` |
| MSI (.msi) | `/quiet /norestart` |
| MSIX/AppX | (none needed - uses Add-AppxPackage) |

## Note on Git Tracking

Installer binaries (`.exe`, `.msi`, `.msixbundle`, etc.) are **gitignored** to avoid bloating the repository. Only this README is tracked.

If you need to share installers with your team, consider:
- A shared network drive
- Cloud storage with download scripts
- Git LFS (for large files)
