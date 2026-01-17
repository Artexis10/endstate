<#
.SYNOPSIS
    Seeds meaningful Git user configuration for curation testing.

.DESCRIPTION
    Sets up Git configuration values that represent real-world user preferences
    WITHOUT creating any credentials or tokens. Used by the curation workflow
    to generate representative config files for module validation.

    Configures:
    - User identity (dummy values)
    - Default branch name
    - Editor preference
    - Diff/merge tools
    - Useful aliases
    - Pull/push behavior
    - Signing toggles (disabled)
    - Rerere and other advanced settings

    DOES NOT configure:
    - Credential helpers
    - Stored credentials
    - GPG keys
    - SSH keys

.PARAMETER Scope
    Git config scope: 'global' (default) or 'system'.

.EXAMPLE
    .\seed.ps1
    
.EXAMPLE
    .\seed.ps1 -Scope global
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $false)]
    [ValidateSet('global', 'system')]
    [string]$Scope = 'global'
)

$ErrorActionPreference = 'Stop'

function Write-Step {
    param([string]$Message)
    Write-Host "[SEED] $Message" -ForegroundColor Yellow
}

function Write-Pass {
    param([string]$Message)
    Write-Host "[PASS] $Message" -ForegroundColor Green
}

function Set-GitConfig {
    param(
        [string]$Key,
        [string]$Value,
        [string]$Scope = 'global'
    )
    
    $result = & git config --$Scope $Key $Value 2>&1
    if ($LASTEXITCODE -ne 0) {
        Write-Warning "Failed to set $Key`: $result"
        return $false
    }
    return $true
}

# Verify git is available
$gitCmd = Get-Command git -ErrorAction SilentlyContinue
if (-not $gitCmd) {
    Write-Error "Git is not installed or not in PATH"
    exit 1
}

Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " Git Configuration Seeding (Curation Mode)" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

Write-Step "Using scope: $Scope"

# ============================================================================
# USER IDENTITY (dummy values - no real identity)
# ============================================================================
Write-Step "Setting user identity (dummy values)..."

Set-GitConfig -Key "user.name" -Value "Endstate Test User" -Scope $Scope
Set-GitConfig -Key "user.email" -Value "test@endstate.local" -Scope $Scope

# ============================================================================
# CORE SETTINGS
# ============================================================================
Write-Step "Setting core preferences..."

Set-GitConfig -Key "init.defaultBranch" -Value "main" -Scope $Scope
Set-GitConfig -Key "core.autocrlf" -Value "true" -Scope $Scope
Set-GitConfig -Key "core.safecrlf" -Value "warn" -Scope $Scope
Set-GitConfig -Key "core.editor" -Value "code --wait" -Scope $Scope
Set-GitConfig -Key "core.pager" -Value "less -FRX" -Scope $Scope
Set-GitConfig -Key "core.whitespace" -Value "trailing-space,space-before-tab" -Scope $Scope

# ============================================================================
# DIFF / MERGE TOOLS
# ============================================================================
Write-Step "Setting diff/merge tool preferences..."

Set-GitConfig -Key "diff.tool" -Value "vscode" -Scope $Scope
Set-GitConfig -Key "difftool.vscode.cmd" -Value "code --wait --diff `$LOCAL `$REMOTE" -Scope $Scope
Set-GitConfig -Key "difftool.prompt" -Value "false" -Scope $Scope

Set-GitConfig -Key "merge.tool" -Value "vscode" -Scope $Scope
Set-GitConfig -Key "mergetool.vscode.cmd" -Value "code --wait `$MERGED" -Scope $Scope
Set-GitConfig -Key "mergetool.keepBackup" -Value "false" -Scope $Scope

# ============================================================================
# PULL / PUSH / FETCH BEHAVIOR
# ============================================================================
Write-Step "Setting pull/push behavior..."

Set-GitConfig -Key "pull.rebase" -Value "true" -Scope $Scope
Set-GitConfig -Key "push.default" -Value "current" -Scope $Scope
Set-GitConfig -Key "push.autoSetupRemote" -Value "true" -Scope $Scope
Set-GitConfig -Key "fetch.prune" -Value "true" -Scope $Scope
Set-GitConfig -Key "fetch.pruneTags" -Value "true" -Scope $Scope

# ============================================================================
# USEFUL ALIASES
# ============================================================================
Write-Step "Setting useful aliases..."

Set-GitConfig -Key "alias.st" -Value "status" -Scope $Scope
Set-GitConfig -Key "alias.co" -Value "checkout" -Scope $Scope
Set-GitConfig -Key "alias.br" -Value "branch" -Scope $Scope
Set-GitConfig -Key "alias.ci" -Value "commit" -Scope $Scope
Set-GitConfig -Key "alias.unstage" -Value "reset HEAD --" -Scope $Scope
Set-GitConfig -Key "alias.last" -Value "log -1 HEAD" -Scope $Scope
Set-GitConfig -Key "alias.lg" -Value "log --oneline --graph --decorate --all" -Scope $Scope
Set-GitConfig -Key "alias.amend" -Value "commit --amend --no-edit" -Scope $Scope
Set-GitConfig -Key "alias.wip" -Value "commit -am 'WIP'" -Scope $Scope
Set-GitConfig -Key "alias.undo" -Value "reset --soft HEAD~1" -Scope $Scope

# ============================================================================
# ADVANCED SETTINGS
# ============================================================================
Write-Step "Setting advanced preferences..."

Set-GitConfig -Key "rerere.enabled" -Value "true" -Scope $Scope
Set-GitConfig -Key "rebase.autoStash" -Value "true" -Scope $Scope
Set-GitConfig -Key "rebase.autoSquash" -Value "true" -Scope $Scope
Set-GitConfig -Key "status.showUntrackedFiles" -Value "all" -Scope $Scope
Set-GitConfig -Key "log.decorate" -Value "auto" -Scope $Scope
Set-GitConfig -Key "color.ui" -Value "auto" -Scope $Scope
Set-GitConfig -Key "help.autocorrect" -Value "10" -Scope $Scope

# ============================================================================
# SIGNING (disabled - no keys)
# ============================================================================
Write-Step "Setting signing preferences (disabled)..."

Set-GitConfig -Key "commit.gpgsign" -Value "false" -Scope $Scope
Set-GitConfig -Key "tag.gpgsign" -Value "false" -Scope $Scope

# ============================================================================
# CREDENTIAL HELPER - EXPLICITLY NOT SET
# ============================================================================
Write-Step "Skipping credential helper (not configured for security)..."
# NOTE: We intentionally do NOT set credential.helper
# This ensures no credentials are stored during curation

# ============================================================================
# SUMMARY
# ============================================================================
Write-Host ""
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host " Git Configuration Seeding Complete" -ForegroundColor Cyan
Write-Host "=" * 60 -ForegroundColor Cyan
Write-Host ""

# Show resulting config
Write-Step "Resulting configuration:"
$configPath = if ($Scope -eq 'global') { "$env:USERPROFILE\.gitconfig" } else { "$env:ProgramData\Git\config" }
if (Test-Path $configPath) {
    Write-Host ""
    Get-Content $configPath | ForEach-Object { Write-Host "  $_" -ForegroundColor Gray }
    Write-Host ""
    Write-Pass "Config written to: $configPath"
} else {
    Write-Warning "Config file not found at expected path: $configPath"
}

Write-Host ""
exit 0
