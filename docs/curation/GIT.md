# Git Curation Workflow

This document describes how to run the automated Git curation workflow to generate and validate the `apps.git` module.

## Overview

The Git curation workflow:
1. Ensures Git is installed (via winget id `Git.Git`)
2. Seeds meaningful Git user state (aliases, default branch, editor, diff tool, signing toggles)
3. Runs capture/discovery diff
4. Emits a draft module folder and human-readable report

**Security**: The workflow does NOT create or store any credentials. Credential files are explicitly excluded.

---

## Quick Start

### Run in Windows Sandbox (Recommended)

```powershell
# Full curation in isolated Windows Sandbox
.\sandbox-tests\discovery-harness\curate-git.ps1
```

### Run Locally (Use with Caution)

```powershell
# Local curation - modifies your ~/.gitconfig
.\sandbox-tests\discovery-harness\curate-git.ps1 -Mode local -SkipInstall
```

### Dry Run (Validate Wiring)

```powershell
# Validate without making changes
.\sandbox-tests\discovery-harness\curate-git.ps1 -DryRun
```

---

## What It Produces

After running, artifacts are written to `sandbox-tests/curation/git/<timestamp>/`:

| File | Description |
|------|-------------|
| `diff.json` | Raw filesystem diff (pre/post seeding) |
| `CURATION_REPORT.txt` | Human-readable analysis with recommendations |
| `module.jsonc` | Draft module (copy of current module) |
| `pre.json` | Pre-install filesystem snapshot (sandbox mode) |
| `post.json` | Post-install filesystem snapshot (sandbox mode) |

### Curation Report Sections

The `CURATION_REPORT.txt` includes:

1. **Summary**: Total files changed, categorized by type
2. **Touched File Paths**: All files modified during seeding
3. **Recommended Capture List**: Config files that should be captured
4. **Recommended Excludes**: Patterns to exclude (cache, temp, logs)
5. **Sensitive Candidates**: Credential files that must NOT be auto-restored

---

## Reviewing and Promoting the Module

### Step 1: Run Curation

```powershell
.\sandbox-tests\discovery-harness\curate-git.ps1 -Mode local -SkipInstall
```

### Step 2: Review the Report

```powershell
# View the curation report
Get-Content .\sandbox-tests\curation\git\<timestamp>\CURATION_REPORT.txt
```

Look for:
- ✅ Config files listed under "Recommended Capture List"
- ⚠️ Any unexpected files in "Touched File Paths"
- 🔒 Credential files correctly listed under "Sensitive Candidates"

### Step 3: Compare with Current Module

```powershell
# Current module location
cat .\modules\apps\git\module.jsonc
```

Verify:
- All captured paths are in the module's `capture.files`
- Credential patterns are in `capture.excludeGlobs`
- Sensitive files are in the `sensitive` section

### Step 4: Promote Changes

If the curation reveals needed updates:

```powershell
# Edit the module directly
code .\modules\apps\git\module.jsonc

# Or copy the draft (if using WriteModule flag)
.\sandbox-tests\discovery-harness\curate-git.ps1 -WriteModule
```

### Step 5: Run Tests

```powershell
# Run Git module tests
Invoke-Pester -Path .\tests\unit\GitModule.Tests.ps1 -Output Detailed
```

---

## Configuration Seeded

The `seed-git-config.ps1` script sets up representative Git configuration:

### User Identity (Dummy Values)
```ini
[user]
    name = Endstate Test User
    email = test@endstate.local
```

### Core Settings
```ini
[init]
    defaultBranch = main
[core]
    autocrlf = true
    editor = code --wait
    pager = less -FRX
```

### Diff/Merge Tools
```ini
[diff]
    tool = vscode
[merge]
    tool = vscode
```

### Behavior
```ini
[pull]
    rebase = true
[push]
    default = current
    autoSetupRemote = true
[fetch]
    prune = true
```

### Aliases
```ini
[alias]
    st = status
    co = checkout
    br = branch
    ci = commit
    lg = log --oneline --graph --decorate --all
    amend = commit --amend --no-edit
    wip = commit -am 'WIP'
    undo = reset --soft HEAD~1
```

### Advanced
```ini
[rerere]
    enabled = true
[rebase]
    autoStash = true
    autoSquash = true
```

### NOT Configured (Security)
- `credential.helper` - Not set to avoid credential storage
- GPG/SSH keys - Not generated
- Real email addresses - Only dummy values used

---

## Sensitive File Handling

The following files are treated as **sensitive** and are:
- ❌ Excluded from capture
- ❌ NOT auto-restored
- ⚠️ Listed in the `sensitive` section with warnings

| File | Content |
|------|---------|
| `~/.git-credentials` | Stored plaintext credentials |
| `~/.config/git/credentials` | XDG credentials location |
| `%APPDATA%/git/credentials` | Windows credentials location |

**After restore, users must re-authenticate** with their Git remotes.

---

## Module Structure

The `apps.git` module captures:

### Capture Paths
```jsonc
"capture": {
  "files": [
    { "source": "~/.gitconfig", "dest": "apps/git/.gitconfig", "optional": true },
    { "source": "~/.gitattributes", "dest": "apps/git/.gitattributes", "optional": true },
    { "source": "~/.config/git/config", "dest": "apps/git/config", "optional": true },
    { "source": "~/.config/git/ignore", "dest": "apps/git/ignore", "optional": true },
    { "source": "~/.config/git/attributes", "dest": "apps/git/attributes", "optional": true }
  ],
  "excludeGlobs": [
    "**/.git-credentials",
    "**/credentials",
    "**/*.credential*"
  ]
}
```

### Verify
```jsonc
"verify": [
  { "type": "command-exists", "command": "git" }
]
```

Note: Verify only checks Git is installed. Having a `.gitconfig` is optional since users may not have configured Git yet.

---

## Troubleshooting

### Windows Sandbox Not Available

```
[ERROR] WindowsSandbox.exe not found
```

Enable Windows Sandbox:
```powershell
Enable-WindowsOptionalFeature -Online -FeatureName 'Containers-DisposableClientVM'
# Restart required
```

Or use local mode:
```powershell
.\sandbox-tests\discovery-harness\curate-git.ps1 -Mode local
```

### Git Not Found (Local Mode)

```
Git is not installed or not in PATH
```

Install Git first:
```powershell
winget install --id Git.Git --silent
```

Or let the curation script install it:
```powershell
.\sandbox-tests\discovery-harness\curate-git.ps1 -Mode local
# (without -SkipInstall)
```

### Seeding Failed

Check that Git is properly installed and in PATH:
```powershell
git --version
git config --list --show-origin
```

---

## Related Files

| Path | Description |
|------|-------------|
| `modules/apps/git/module.jsonc` | Git config module definition |
| `sandbox-tests/discovery-harness/curate-git.ps1` | Main curation runner |
| `sandbox-tests/discovery-harness/seed-git-config.ps1` | Git config seeding script |
| `tests/unit/GitModule.Tests.ps1` | Pester tests for Git module |
| `docs/curation-matrix.md` | Overall curation strategy |
