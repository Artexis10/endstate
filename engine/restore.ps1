<#
.SYNOPSIS
    Provisioning restore engine - restores configuration files with backup-first safety.

.DESCRIPTION
    Executes restore steps from a manifest with:
    - Opt-in behavior (requires explicit -EnableRestore flag)
    - Backup-first safety (backs up existing targets before overwriting)
    - Idempotent operation (skips if target already matches source)
    - Sensitive path warnings
#>

# Import dependencies
. "$PSScriptRoot\logging.ps1"
. "$PSScriptRoot\manifest.ps1"
. "$PSScriptRoot\state.ps1"
. "$PSScriptRoot\..\restorers\copy.ps1"

# Known sensitive path segments that trigger warnings
$script:SensitivePathSegments = @(
    '.ssh',
    '.aws',
    '.azure',
    '.gnupg',
    '.gpg',
    'credentials',
    'secrets',
    'tokens',
    '.kube',
    '.docker',
    'id_rsa',
    'id_ed25519',
    'id_ecdsa'
)

function Get-RestoreActionId {
    <#
    .SYNOPSIS
        Generate a deterministic ID for a restore action.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Item
    )
    
    if ($Item.id) {
        return $Item.id
    }
    
    # Generate deterministic ID from type, source, and target
    $normalized = "copy:$($Item.source)->$($Item.target)" -replace '[\\\/]', '/'
    return $normalized
}

function Test-SensitivePath {
    <#
    .SYNOPSIS
        Check if a path contains sensitive segments and return warnings.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path
    )
    
    $warnings = @()
    $normalizedPath = $Path.ToLower() -replace '\\', '/'
    
    foreach ($segment in $script:SensitivePathSegments) {
        if ($normalizedPath -match [regex]::Escape($segment.ToLower())) {
            $warnings += "Path contains sensitive segment '$segment': $Path"
        }
    }
    
    return $warnings
}

function Expand-RestorePath {
    <#
    .SYNOPSIS
        Expand a path with ~ and environment variables.
    .DESCRIPTION
        Supports:
        - ~ for user home directory
        - Environment variables like %USERPROFILE%
        - Relative paths resolved against manifest directory
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Path,
        
        [Parameter(Mandatory = $false)]
        [string]$BasePath = $null
    )
    
    $expanded = $Path
    
    # Expand environment variables
    $expanded = [Environment]::ExpandEnvironmentVariables($expanded)
    
    # Handle ~ for home directory (cross-platform)
    if ($expanded.StartsWith("~")) {
        $home = if ($env:HOME) { $env:HOME } else { $env:USERPROFILE }
        $expanded = $expanded -replace "^~", $home
    }
    
    # Handle relative paths (starting with ./ or ../)
    if ($BasePath -and ($expanded.StartsWith("./") -or $expanded.StartsWith("../"))) {
        $expanded = Join-Path $BasePath $expanded
        $expanded = [System.IO.Path]::GetFullPath($expanded)
    }
    
    return $expanded
}

function Test-FileUpToDate {
    <#
    .SYNOPSIS
        Check if target file matches source (up-to-date detection).
    .DESCRIPTION
        For files: compares size and last write time.
        For directories: shallow comparison (file count + newest mtime).
        Returns $true if target is up-to-date.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Source,
        
        [Parameter(Mandatory = $true)]
        [string]$Target
    )
    
    if (-not (Test-Path $Target)) {
        return $false
    }
    
    $sourceItem = Get-Item $Source
    $targetItem = Get-Item $Target
    
    # Both must be same type (file vs directory)
    if ($sourceItem.PSIsContainer -ne $targetItem.PSIsContainer) {
        return $false
    }
    
    if ($sourceItem.PSIsContainer) {
        # Directory comparison: shallow strategy
        # Compare file count and newest modification time
        $sourceFiles = @(Get-ChildItem -Path $Source -Recurse -File)
        $targetFiles = @(Get-ChildItem -Path $Target -Recurse -File)
        
        if ($sourceFiles.Count -ne $targetFiles.Count) {
            return $false
        }
        
        if ($sourceFiles.Count -eq 0) {
            return $true
        }
        
        $sourceNewest = ($sourceFiles | Sort-Object LastWriteTime -Descending | Select-Object -First 1).LastWriteTime
        $targetNewest = ($targetFiles | Sort-Object LastWriteTime -Descending | Select-Object -First 1).LastWriteTime
        
        # Allow 2-second tolerance for filesystem timestamp differences
        return [Math]::Abs(($sourceNewest - $targetNewest).TotalSeconds) -lt 2
    } else {
        # File comparison: size + last write time
        if ($sourceItem.Length -ne $targetItem.Length) {
            return $false
        }
        
        # Allow 2-second tolerance for filesystem timestamp differences
        return [Math]::Abs(($sourceItem.LastWriteTime - $targetItem.LastWriteTime).TotalSeconds) -lt 2
    }
}

function Test-IsElevated {
    <#
    .SYNOPSIS
        Check if the current process is running with elevated privileges.
    #>
    if ($IsWindows -or $env:OS -eq "Windows_NT") {
        $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
        $principal = [Security.Principal.WindowsPrincipal]$identity
        return $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    } else {
        # Unix: check if running as root
        return (id -u) -eq 0
    }
}

function Invoke-RestoreAction {
    <#
    .SYNOPSIS
        Execute a single restore action with backup-first safety.
    .DESCRIPTION
        Pure function that performs the restore operation.
        Suitable for unit testing with mocked filesystem operations.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [hashtable]$Action,
        
        [Parameter(Mandatory = $true)]
        [string]$RunId,
        
        [Parameter(Mandatory = $false)]
        [string]$ManifestDir = $null,
        
        [Parameter(Mandatory = $false)]
        [switch]$DryRun
    )
    
    $result = @{
        id = $Action.id
        type = "restore"
        restoreType = $Action.restoreType
        source = $Action.source
        target = $Action.target
        status = "pending"
        reason = $null
        backupPath = $null
        warnings = @()
    }
    
    # Expand paths
    $expandedSource = Expand-RestorePath -Path $Action.source -BasePath $ManifestDir
    $expandedTarget = Expand-RestorePath -Path $Action.target
    
    $result.expandedSource = $expandedSource
    $result.expandedTarget = $expandedTarget
    
    # Check for sensitive paths and add warnings
    $sourceWarnings = Test-SensitivePath -Path $expandedSource
    $targetWarnings = Test-SensitivePath -Path $expandedTarget
    $result.warnings = @($sourceWarnings) + @($targetWarnings)
    
    # Check requiresAdmin
    if ($Action.requiresAdmin -and -not (Test-IsElevated)) {
        $result.status = "fail"
        $result.reason = "requires elevated privileges (run as Administrator)"
        return $result
    }
    
    # Check source exists
    if (-not (Test-Path $expandedSource)) {
        $result.status = "fail"
        $result.reason = "source not found: $expandedSource"
        return $result
    }
    
    # Check if up-to-date
    if (Test-FileUpToDate -Source $expandedSource -Target $expandedTarget) {
        $result.status = "skip"
        $result.reason = "already up to date"
        return $result
    }
    
    # Dry-run mode
    if ($DryRun) {
        $result.status = "dry-run"
        $result.reason = "would restore $expandedSource -> $expandedTarget"
        return $result
    }
    
    # Backup existing target if it exists
    $backup = if ($null -eq $Action.backup) { $true } else { $Action.backup }
    if ($backup -and (Test-Path $expandedTarget)) {
        $backupResult = Backup-RestoreTarget -Target $expandedTarget -RunId $RunId
        if (-not $backupResult.Success) {
            $result.status = "fail"
            $result.reason = "backup failed: $($backupResult.Error)"
            return $result
        }
        $result.backupPath = $backupResult.BackupPath
    }
    
    # Perform the copy
    try {
        $targetDir = Split-Path -Parent $expandedTarget
        if ($targetDir -and -not (Test-Path $targetDir)) {
            New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
        }
        
        if (Test-Path $expandedSource -PathType Container) {
            # Directory copy
            if (Test-Path $expandedTarget) {
                Remove-Item -Path $expandedTarget -Recurse -Force
            }
            Copy-Item -Path $expandedSource -Destination $expandedTarget -Recurse -Force
        } else {
            # File copy
            Copy-Item -Path $expandedSource -Destination $expandedTarget -Force
        }
        
        $result.status = "restore"
        $result.reason = "restored successfully"
        
    } catch {
        $result.status = "fail"
        $result.reason = $_.Exception.Message
    }
    
    return $result
}

function Backup-RestoreTarget {
    <#
    .SYNOPSIS
        Backup a target file/directory before overwriting.
    .DESCRIPTION
        Creates backup under provisioning/state/backups/<runId>/...
        preserving the original path structure.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$Target,
        
        [Parameter(Mandatory = $true)]
        [string]$RunId
    )
    
    $result = @{
        Success = $false
        BackupPath = $null
        Error = $null
    }
    
    try {
        # Create backup directory structure
        $backupRoot = Join-Path $PSScriptRoot "..\state\backups\$RunId"
        
        # Preserve path structure in backup
        # Convert absolute path to relative structure
        $normalizedTarget = $Target -replace ':', ''  # Remove drive letter colon
        $normalizedTarget = $normalizedTarget -replace '^[/\\]+', ''  # Remove leading slashes
        
        $backupPath = Join-Path $backupRoot $normalizedTarget
        $backupDir = Split-Path -Parent $backupPath
        
        if (-not (Test-Path $backupDir)) {
            New-Item -ItemType Directory -Path $backupDir -Force | Out-Null
        }
        
        if (Test-Path $Target -PathType Container) {
            Copy-Item -Path $Target -Destination $backupPath -Recurse -Force
        } else {
            Copy-Item -Path $Target -Destination $backupPath -Force
        }
        
        $result.Success = $true
        $result.BackupPath = $backupPath
        
    } catch {
        $result.Error = $_.Exception.Message
    }
    
    return $result
}

function Invoke-Restore {
    <#
    .SYNOPSIS
        Execute all restore steps from a manifest.
    .DESCRIPTION
        Main entry point for restore operations.
        Requires explicit opt-in via -EnableRestore flag.
    #>
    param(
        [Parameter(Mandatory = $true)]
        [string]$ManifestPath,
        
        [Parameter(Mandatory = $false)]
        [switch]$EnableRestore,
        
        [Parameter(Mandatory = $false)]
        [switch]$DryRun,
        
        [Parameter(Mandatory = $false)]
        [string]$RunId = $null
    )
    
    if (-not $RunId) {
        $RunId = Get-RunId
    }
    
    $logFile = Initialize-ProvisioningLog -RunId "restore-$RunId"
    
    Write-ProvisioningSection "Provisioning Restore"
    Write-ProvisioningLog "Manifest: $ManifestPath" -Level INFO
    Write-ProvisioningLog "Run ID: $RunId" -Level INFO
    
    # Load manifest
    $manifest = Read-Manifest -Path $ManifestPath
    $manifestDir = Split-Path -Parent (Resolve-Path $ManifestPath)
    
    # Check if restore steps exist
    $restoreItems = @($manifest.restore)
    
    if ($restoreItems.Count -eq 0) {
        Write-ProvisioningLog "No restore steps in manifest" -Level INFO
        Write-Host ""
        Write-Host "No restore steps defined in manifest." -ForegroundColor Yellow
        return @{
            RunId = $RunId
            RestoreCount = 0
            SkipCount = 0
            FailCount = 0
            Results = @()
        }
    }
    
    # Check opt-in
    if (-not $EnableRestore) {
        Write-ProvisioningLog "Restore steps found but -EnableRestore not specified" -Level WARN
        Write-Host ""
        Write-Host "Restore steps found in manifest but restore is not enabled." -ForegroundColor Yellow
        Write-Host ""
        Write-Host "Restore is opt-in for safety. To enable restore, use:" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command restore -Manifest `"$ManifestPath`" -EnableRestore" -ForegroundColor DarkGray
        Write-Host ""
        Write-Host "Restore steps that would run:" -ForegroundColor Yellow
        foreach ($item in $restoreItems) {
            $id = Get-RestoreActionId -Item $item
            Write-Host "  - $id : $($item.source) -> $($item.target)" -ForegroundColor DarkGray
        }
        Write-Host ""
        
        return @{
            RunId = $RunId
            RestoreCount = 0
            SkipCount = $restoreItems.Count
            FailCount = 0
            Results = @()
            RestoreNotEnabled = $true
        }
    }
    
    if ($DryRun) {
        Write-Host ""
        Write-Host "  *** DRY-RUN MODE - No changes will be made ***" -ForegroundColor Yellow
        Write-Host ""
    }
    
    # Process restore items
    Write-ProvisioningSection "Executing Restore Steps"
    
    $restoreCount = 0
    $skipCount = 0
    $failCount = 0
    $results = @()
    
    foreach ($item in $restoreItems) {
        # Build action from manifest item
        $action = @{
            id = Get-RestoreActionId -Item $item
            restoreType = $item.type
            source = $item.source
            target = $item.target
            backup = if ($null -eq $item.backup) { $true } else { $item.backup }
            requiresAdmin = if ($item.requiresAdmin) { $true } else { $false }
        }
        
        # Log sensitive path warnings
        $expandedSource = Expand-RestorePath -Path $item.source -BasePath $manifestDir
        $expandedTarget = Expand-RestorePath -Path $item.target
        
        $warnings = @(Test-SensitivePath -Path $expandedSource) + @(Test-SensitivePath -Path $expandedTarget)
        foreach ($warning in $warnings) {
            Write-ProvisioningLog "WARNING: $warning" -Level WARN
        }
        
        # Execute restore
        $result = Invoke-RestoreAction -Action $action -RunId $RunId -ManifestDir $manifestDir -DryRun:$DryRun
        
        # Log result
        switch ($result.status) {
            "restore" {
                Write-ProvisioningLog "RESTORED: $($action.id)" -Level SUCCESS
                if ($result.backupPath) {
                    Write-ProvisioningLog "  Backup: $($result.backupPath)" -Level INFO
                }
                $restoreCount++
            }
            "skip" {
                Write-ProvisioningLog "SKIP: $($action.id) - $($result.reason)" -Level SKIP
                $skipCount++
            }
            "fail" {
                Write-ProvisioningLog "FAIL: $($action.id) - $($result.reason)" -Level ERROR
                $failCount++
            }
            "dry-run" {
                Write-ProvisioningLog "[DRY-RUN] $($action.id) - $($result.reason)" -Level ACTION
                $restoreCount++
            }
        }
        
        $results += $result
    }
    
    # Save state
    $manifestHash = Get-ManifestHash -ManifestPath $ManifestPath
    
    $runState = @{
        runId = $RunId
        timestamp = Get-Date -Format "yyyy-MM-ddTHH:mm:ssZ"
        command = "restore"
        dryRun = $DryRun.IsPresent
        manifest = @{
            path = $ManifestPath
            name = $manifest.name
            hash = $manifestHash
        }
        summary = @{
            restore = $restoreCount
            skip = $skipCount
            fail = $failCount
        }
        actions = $results
    }
    
    $stateDir = Join-Path $PSScriptRoot "..\state"
    if (-not (Test-Path $stateDir)) {
        New-Item -ItemType Directory -Path $stateDir -Force | Out-Null
    }
    $stateFile = Join-Path $stateDir "restore-$RunId.json"
    $runState | ConvertTo-Json -Depth 10 | Out-File -FilePath $stateFile -Encoding UTF8
    
    # Summary
    Write-ProvisioningSection "Restore Results"
    Close-ProvisioningLog -SuccessCount $restoreCount -SkipCount $skipCount -FailCount $failCount
    
    Write-Host ""
    if ($DryRun) {
        Write-Host "Dry-run complete. No changes were made." -ForegroundColor Yellow
        Write-Host ""
        Write-Host "To restore for real:" -ForegroundColor Yellow
        Write-Host "  .\cli.ps1 -Command restore -Manifest `"$ManifestPath`" -EnableRestore"
    } elseif ($failCount -eq 0) {
        Write-Host "Restore complete!" -ForegroundColor Green
    } else {
        Write-Host "Restore completed with $failCount failure(s)." -ForegroundColor Yellow
    }
    Write-Host ""
    
    return @{
        RunId = $RunId
        RestoreCount = $restoreCount
        SkipCount = $skipCount
        FailCount = $failCount
        Results = $results
        LogFile = $logFile
    }
}

# Functions exported: Invoke-Restore, Invoke-RestoreAction, Get-RestoreActionId, Test-SensitivePath, Expand-RestorePath, Test-FileUpToDate, Backup-RestoreTarget
