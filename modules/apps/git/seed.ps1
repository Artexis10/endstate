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
    Write-Output "[SEED] $Message"
}

function Write-Pass {
    param([string]$Message)
    Write-Output "[PASS] $Message"
}

function Set-GitConfig {
    param(
        [string]$Key,
        [string]$Value,
        [string]$Scope = 'global'
    )
    
    & git config --$Scope $Key $Value 2>$null
    if ($LASTEXITCODE -ne 0) {
        Write-Output "[WARN] Failed to set $Key (exit code $LASTEXITCODE)"
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

# Ensure HOME is set so git config --global can find the config file.
# In Windows Sandbox (WDAGUtilityAccount), HOME may not be set.
if (-not $env:HOME) {
    $env:HOME = $env:USERPROFILE
}
Write-Output "[SEED] HOME=$env:HOME  USERPROFILE=$env:USERPROFILE"
Write-Output "[SEED] git path: $($gitCmd.Source)"

# Diagnostic: verify git config --global actually works
Write-Output "[SEED] Testing git config --global write..."
& git config --global endstate.test "probe" 2>$null
$probeExit = $LASTEXITCODE
Write-Output "[SEED] Probe exit code: $probeExit"
$probePath = Join-Path $env:USERPROFILE ".gitconfig"
$probeExists = Test-Path $probePath
Write-Output "[SEED] .gitconfig exists after probe: $probeExists (path: $probePath)"
if (-not $probeExists) {
    # Try with explicit --file flag as fallback
    Write-Output "[SEED] Trying explicit --file flag..."
    & git config --file $probePath endstate.test "probe" 2>$null
    $probeExists2 = Test-Path $probePath
    Write-Output "[SEED] .gitconfig exists after --file probe: $probeExists2"
}
# Clean up probe key
& git config --global --unset endstate.test 2>$null

Write-Output ""
Write-Output ("=" * 60)
Write-Output " Git Configuration Seeding (Curation Mode)"
Write-Output ("=" * 60)
Write-Output ""

Write-Step "Using scope: $Scope"

# ============================================================================
# USER IDENTITY (dummy values - no real identity)
# ============================================================================
Write-Step "Setting user identity (dummy values)..."

$null = Set-GitConfig -Key "user.name" -Value "Endstate Test User" -Scope $Scope
$null = Set-GitConfig -Key "user.email" -Value "test@endstate.local" -Scope $Scope

# ============================================================================
# CORE SETTINGS
# ============================================================================
Write-Step "Setting core preferences..."

$null = Set-GitConfig -Key "init.defaultBranch" -Value "main" -Scope $Scope
$null = Set-GitConfig -Key "core.autocrlf" -Value "true" -Scope $Scope
$null = Set-GitConfig -Key "core.safecrlf" -Value "warn" -Scope $Scope
$null = Set-GitConfig -Key "core.editor" -Value "code --wait" -Scope $Scope
$null = Set-GitConfig -Key "core.pager" -Value "less -FRX" -Scope $Scope
$null = Set-GitConfig -Key "core.whitespace" -Value "trailing-space,space-before-tab" -Scope $Scope

# ============================================================================
# DIFF / MERGE TOOLS
# ============================================================================
Write-Step "Setting diff/merge tool preferences..."

$null = Set-GitConfig -Key "diff.tool" -Value "vscode" -Scope $Scope
$null = Set-GitConfig -Key "difftool.vscode.cmd" -Value "code --wait --diff `$LOCAL `$REMOTE" -Scope $Scope
$null = Set-GitConfig -Key "difftool.prompt" -Value "false" -Scope $Scope

$null = Set-GitConfig -Key "merge.tool" -Value "vscode" -Scope $Scope
$null = Set-GitConfig -Key "mergetool.vscode.cmd" -Value "code --wait `$MERGED" -Scope $Scope
$null = Set-GitConfig -Key "mergetool.keepBackup" -Value "false" -Scope $Scope

# ============================================================================
# PULL / PUSH / FETCH BEHAVIOR
# ============================================================================
Write-Step "Setting pull/push behavior..."

$null = Set-GitConfig -Key "pull.rebase" -Value "true" -Scope $Scope
$null = Set-GitConfig -Key "push.default" -Value "current" -Scope $Scope
$null = Set-GitConfig -Key "push.autoSetupRemote" -Value "true" -Scope $Scope
$null = Set-GitConfig -Key "fetch.prune" -Value "true" -Scope $Scope
$null = Set-GitConfig -Key "fetch.pruneTags" -Value "true" -Scope $Scope

# ============================================================================
# USEFUL ALIASES
# ============================================================================
Write-Step "Setting useful aliases..."

$null = Set-GitConfig -Key "alias.st" -Value "status" -Scope $Scope
$null = Set-GitConfig -Key "alias.co" -Value "checkout" -Scope $Scope
$null = Set-GitConfig -Key "alias.br" -Value "branch" -Scope $Scope
$null = Set-GitConfig -Key "alias.ci" -Value "commit" -Scope $Scope
$null = Set-GitConfig -Key "alias.unstage" -Value "reset HEAD --" -Scope $Scope
$null = Set-GitConfig -Key "alias.last" -Value "log -1 HEAD" -Scope $Scope
$null = Set-GitConfig -Key "alias.lg" -Value "log --oneline --graph --decorate --all" -Scope $Scope
$null = Set-GitConfig -Key "alias.amend" -Value "commit --amend --no-edit" -Scope $Scope
$null = Set-GitConfig -Key "alias.wip" -Value "commit -am 'WIP'" -Scope $Scope
$null = Set-GitConfig -Key "alias.undo" -Value "reset --soft HEAD~1" -Scope $Scope

# ============================================================================
# ADVANCED SETTINGS
# ============================================================================
Write-Step "Setting advanced preferences..."

$null = Set-GitConfig -Key "rerere.enabled" -Value "true" -Scope $Scope
$null = Set-GitConfig -Key "rebase.autoStash" -Value "true" -Scope $Scope
$null = Set-GitConfig -Key "rebase.autoSquash" -Value "true" -Scope $Scope
$null = Set-GitConfig -Key "status.showUntrackedFiles" -Value "all" -Scope $Scope
$null = Set-GitConfig -Key "log.decorate" -Value "auto" -Scope $Scope
$null = Set-GitConfig -Key "color.ui" -Value "auto" -Scope $Scope
$null = Set-GitConfig -Key "help.autocorrect" -Value "10" -Scope $Scope

# ============================================================================
# SIGNING (disabled - no keys)
# ============================================================================
Write-Step "Setting signing preferences (disabled)..."

$null = Set-GitConfig -Key "commit.gpgsign" -Value "false" -Scope $Scope
$null = Set-GitConfig -Key "tag.gpgsign" -Value "false" -Scope $Scope

# ============================================================================
# CREDENTIAL HELPER - EXPLICITLY NOT SET
# ============================================================================
Write-Step "Skipping credential helper (not configured for security)..."
# NOTE: We intentionally do NOT set credential.helper
# This ensures no credentials are stored during curation

# ============================================================================
# POST-CONFIG DIAGNOSTIC
# ============================================================================
Write-Output "[SEED] Post-config diagnostic:"
$configList = & git config --global --list 2>$null
if ($configList) {
    $configList | ForEach-Object { Write-Output "  $_" }
} else {
    Write-Output "  WARNING: git config --global --list returned nothing"
}
$gitconfigPath = Join-Path $env:HOME ".gitconfig"
Write-Output "[SEED] .gitconfig exists: $(Test-Path $gitconfigPath)"
if (Test-Path $gitconfigPath) {
    Write-Output "[SEED] .gitconfig size: $((Get-Item $gitconfigPath).Length) bytes"
}

# ============================================================================
# SUMMARY
# ============================================================================
Write-Output ""
Write-Output ("=" * 60)
Write-Output " Git Configuration Seeding Complete"
Write-Output ("=" * 60)
Write-Output ""

# Show resulting config
Write-Step "Resulting configuration:"
$configPath = if ($Scope -eq 'global') { "$env:USERPROFILE\.gitconfig" } else { "$env:ProgramData\Git\config" }
if (Test-Path $configPath) {
    Write-Output ""
    Get-Content $configPath | ForEach-Object { Write-Output "  $_" }
    Write-Output ""
    Write-Pass "Config written to: $configPath"
} else {
    Write-Output "[WARN] Config file not found at expected path: $configPath"
    Write-Output "[DIAG] git config --list --show-origin:"
    try { & git config --list --show-origin 2>&1 | ForEach-Object { Write-Output "  $_" } } catch { Write-Output "  (failed: $_)" }
    Write-Output "[DIAG] git config --global --list:"
    try { & git config --global --list 2>&1 | ForEach-Object { Write-Output "  $_" } } catch { Write-Output "  (failed: $_)" }
    Write-Output "[DIAG] Checking common config locations:"
    @("$env:USERPROFILE\.gitconfig", "$env:HOME\.gitconfig", "$env:PROGRAMDATA\Git\config", "$env:APPDATA\.gitconfig") | ForEach-Object {
        $exists = Test-Path $_
        $size = if ($exists) { (Get-Item $_).Length } else { 'N/A' }
        Write-Output "  $_ exists=$exists size=$size"
    }
}

Write-Output ""
exit 0
