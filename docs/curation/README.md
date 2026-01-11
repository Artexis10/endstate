# Curation Runner

Unified entrypoint for running Endstate config module curation workflows.

## Quick Start

```powershell
# Curate Git locally (assumes Git installed)
pwsh -File .\sandbox-tests\discovery-harness\curate.ps1 -App git -Mode local -SkipInstall

# Curate Git in Windows Sandbox (full isolation)
pwsh -File .\sandbox-tests\discovery-harness\curate.ps1 -App git -Mode sandbox

# Curate, promote module, and run tests
pwsh -File .\sandbox-tests\discovery-harness\curate.ps1 -App git -Mode local -Promote -RunTests

# Scaffold only (create module.jsonc template)
pwsh -File .\sandbox-tests\discovery-harness\curate.ps1 -App newapp -ScaffoldOnly
```

## Modes

| Mode | Description | Use Case |
|------|-------------|----------|
| `sandbox` | Runs in Windows Sandbox (isolated VM) | Clean-room discovery, no host contamination |
| `local` | Runs directly on host machine | Fast iteration, requires app pre-installed |

### Local Mode

- **Faster**: No sandbox startup overhead
- **Requires**: App already installed on host
- **Warning**: Modifies user config files (e.g., `~/.gitconfig`)
- **Best for**: Iterating on curation workflow, verifying capture paths

### Sandbox Mode

- **Isolated**: Fresh Windows environment each run
- **Slower**: Sandbox startup + app installation
- **Clean**: No pre-existing user state
- **Best for**: Final validation, discovering all touched files

## Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `-App` | string | (required) | App to curate (e.g., `git`, `vscodium`) |
| `-Mode` | string | `sandbox` | Execution mode: `sandbox` or `local` |
| `-ScaffoldOnly` | switch | false | Only scaffold module, don't run curation |
| `-SkipInstall` | switch | false | Skip app installation |
| `-Promote` | switch | false | Copy curated module to `modules/apps/<app>/` |
| `-RunTests` | switch | false | Run targeted unit tests after curation |
| `-ResolveFinalUrlFn` | scriptblock | null | DI: Custom URL resolver |
| `-DownloadFn` | scriptblock | null | DI: Custom downloader |

## Runner Naming Convention

Per-app curation logic lives in runner scripts:

```
sandbox-tests/discovery-harness/curate-<app>.ps1
```

Examples:
- `curate-git.ps1` — Git curation workflow
- `curate-vscodium.ps1` — VSCodium curation (stub)

### Creating a New Runner

1. Create `curate-<app>.ps1` with these parameters:
   ```powershell
   param(
       [ValidateSet('sandbox', 'local')]
       [string]$Mode = 'sandbox',
       [switch]$SkipInstall,
       [switch]$Promote
   )
   ```

2. Implement:
   - `Invoke-LocalCuration` — Local mode workflow
   - `Invoke-SandboxCuration` — Sandbox mode workflow

3. Reference `curate-git.ps1` for a complete example.

## Module Scaffolding

If `modules/apps/<app>/module.jsonc` doesn't exist, `curate.ps1` auto-creates it:

```jsonc
{
  "id": "apps.<app>",
  "displayName": "<app>",
  "sensitivity": "medium",
  "matches": { "winget": [], "exe": [] },
  "verify": [],
  "restore": [],
  "capture": { "files": [], "excludeGlobs": [] },
  "notes": "Auto-scaffolded module. Run curation workflow to populate."
}
```

## Workflow Steps

1. **Scaffold** — Ensure module directory and template exist
2. **Locate Runner** — Find `curate-<app>.ps1`
3. **Execute Curation** — Run the per-app workflow
4. **Run Tests** (optional) — Execute targeted Pester tests

## Examples

### Basic Git Curation (Local)

```powershell
.\sandbox-tests\discovery-harness\curate.ps1 -App git -Mode local -SkipInstall
```

Output:
- Curation report in `sandbox-tests/curation/git/<timestamp>/`
- Human-readable `CURATION_REPORT.txt`
- Diff JSON with changed files

### Full Sandbox Curation with Promotion

```powershell
.\sandbox-tests\discovery-harness\curate.ps1 -App git -Mode sandbox -Promote -RunTests
```

This:
1. Launches Windows Sandbox
2. Installs Git via winget
3. Seeds configuration
4. Captures state diff
5. Generates curation report
6. Copies module to `modules/apps/git/`
7. Runs `GitModule.Tests.ps1`

### Scaffold New App

```powershell
.\sandbox-tests\discovery-harness\curate.ps1 -App myapp -ScaffoldOnly
```

Creates `modules/apps/myapp/module.jsonc` with template structure.

## Supported Apps

| App | Runner | Status |
|-----|--------|--------|
| `git` | `curate-git.ps1` | ✅ Complete |
| `vscodium` | `curate-vscodium.ps1` | 🚧 Stub |

## Testing

Run curation tests:

```powershell
.\scripts\test-unit.ps1 -Path tests\unit\Curate.Tests.ps1
```

Run Git module tests:

```powershell
.\scripts\test-unit.ps1 -Path tests\unit\GitModule.Tests.ps1
```

## See Also

- [Git Curation Details](./GIT.md)
- [Curation Matrix](../curation-matrix.md)
