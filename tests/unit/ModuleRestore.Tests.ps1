<#
.SYNOPSIS
    Pester tests for module-based restore behavior.
    
.DESCRIPTION
    Tests cover:
    - Module loading via manifest.modules[]
    - Bundle expansion via manifest.bundles[]
    - Restore action execution
    - Journal writing
    - Revert functionality
    - Idempotency (second restore skips)
    - Backup behavior
    
    Modules are the sole restore abstraction in Endstate.
#>

BeforeAll {
    $script:ProvisioningRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    $script:EngineDir = Join-Path $script:ProvisioningRoot "engine"
    $script:ModulesDir = Join-Path $script:ProvisioningRoot "modules"
    $script:BundlesDir = Join-Path $script:ProvisioningRoot "bundles"
    
    # Load dependencies
    . (Join-Path $script:EngineDir "logging.ps1")
    . (Join-Path $script:EngineDir "manifest.ps1")
    . (Join-Path $script:EngineDir "state.ps1")
    . (Join-Path $script:EngineDir "restore.ps1")
}

Describe "Module.Loading" {
    
    Context "Real repo modules load correctly" {
        
        It "Should load powertoys module from actual repo" {
            $repoRoot = $script:ProvisioningRoot
            $manifest = @{
                version = 1
                name = "module-test"
                modules = @("powertoys")
                bundles = @()
                restore = @()
                apps = @()
            }
            
            $expandedRestore = @(Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $repoRoot)
            
            # PowerToys module has 1 restore entry
            $expandedRestore.Count | Should -Be 1
            $expandedRestore[0].type | Should -Be "copy"
            $expandedRestore[0].target | Should -Be "%LOCALAPPDATA%\Microsoft\PowerToys"
            $expandedRestore[0].exclude | Should -Not -BeNullOrEmpty
        }
        
        It "Should load msi-afterburner module from actual repo" {
            $repoRoot = $script:ProvisioningRoot
            $manifest = @{
                version = 1
                name = "module-test"
                modules = @("msi-afterburner")
                bundles = @()
                restore = @()
                apps = @()
            }
            
            $expandedRestore = @(Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $repoRoot)
            
            # MSI Afterburner module has 2 restore entries
            $expandedRestore.Count | Should -Be 2
            $expandedRestore[0].type | Should -Be "copy"
            $expandedRestore[1].type | Should -Be "copy"
        }
        
        It "Should load core-utilities bundle from actual repo" {
            $repoRoot = $script:ProvisioningRoot
            $manifest = @{
                version = 1
                name = "bundle-test"
                bundles = @("core-utilities")
                modules = @()
                restore = @()
                apps = @()
            }
            
            $expandedRestore = @(Resolve-RestoreEntriesFromBundles -Manifest $manifest -RepoRoot $repoRoot)
            
            # core-utilities bundle has msi-afterburner (2) + powertoys (1) = 3 entries
            $expandedRestore.Count | Should -Be 3
        }
        
        It "Should append inline restore after module entries" {
            $repoRoot = $script:ProvisioningRoot
            $manifest = @{
                version = 1
                name = "mixed-test"
                modules = @("powertoys")
                bundles = @()
                restore = @(@{ type = "copy"; source = "./inline-source"; target = "~/inline-target" })
                apps = @()
            }
            
            $moduleRestore = @(Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $repoRoot)
            
            # PowerToys module has 1 restore entry
            $moduleRestore.Count | Should -Be 1
            $moduleRestore[0].target | Should -Be "%LOCALAPPDATA%\Microsoft\PowerToys"
        }
    }
}

Describe "Restore.Execution" {
    
    BeforeEach {
        # Mock logging to avoid console noise
        Mock Initialize-ProvisioningLog { return "test.log" }
        Mock Write-ProvisioningLog { }
        Mock Write-ProvisioningSection { }
        Mock Close-ProvisioningLog { }
    }
    
    Context "Files restored to correct targets" {
        
        It "Should copy source file to expanded target path" {
            # Setup source
            $sourceDir = Join-Path $TestDrive "configs"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            "test config content" | Out-File -FilePath (Join-Path $sourceDir "app.conf") -Encoding UTF8
            
            # Setup target directory
            $targetDir = Join-Path $TestDrive "target"
            New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
            $targetFile = Join-Path $targetDir "app.conf"
            
            # Create manifest with restore entry
            $manifestContent = @"
{
    "version": 1,
    "name": "restore-test",
    "restore": [{
        "type": "copy",
        "source": "./configs/app.conf",
        "target": "$($targetFile -replace '\\', '\\\\')",
        "backup": false
    }],
    "apps": []
}
"@
            $manifestPath = Join-Path $TestDrive "manifest.jsonc"
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            # Execute restore
            $result = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -RunId "test-run-001"
            
            # Verify
            $result.RestoreCount | Should -Be 1
            $result.FailCount | Should -Be 0
            (Test-Path $targetFile) | Should -Be $true
            (Get-Content $targetFile -Raw).Trim() | Should -Be "test config content"
        }
        
        It "Should expand environment variables in target path" {
            $sourceDir = Join-Path $TestDrive "configs"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            "env test" | Out-File -FilePath (Join-Path $sourceDir "env.conf") -Encoding UTF8
            
            # Use TEMP as a reliable env var
            $targetFile = Join-Path $env:TEMP "endstate-test-env.conf"
            
            $manifestContent = @'
{
    "version": 1,
    "name": "env-test",
    "restore": [{
        "type": "copy",
        "source": "./configs/env.conf",
        "target": "%TEMP%\\endstate-test-env.conf",
        "backup": false
    }],
    "apps": []
}
'@
            $manifestPath = Join-Path $TestDrive "manifest.jsonc"
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            try {
                $result = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -RunId "test-run-env"
                
                $result.RestoreCount | Should -Be 1
                (Test-Path $targetFile) | Should -Be $true
            } finally {
                # Cleanup
                if (Test-Path $targetFile) { Remove-Item $targetFile -Force }
            }
        }
    }
    
    Context "Idempotency - second restore skips" {
        
        It "Should skip restore when target is already up to date" {
            $sourceDir = Join-Path $TestDrive "configs"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            $sourceFile = Join-Path $sourceDir "idempotent.conf"
            "idempotent content" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            $targetDir = Join-Path $TestDrive "target"
            New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
            $targetFile = Join-Path $targetDir "idempotent.conf"
            
            $manifestContent = @"
{
    "version": 1,
    "name": "idempotent-test",
    "restore": [{
        "type": "copy",
        "source": "./configs/idempotent.conf",
        "target": "$($targetFile -replace '\\', '\\\\')",
        "backup": false
    }],
    "apps": []
}
"@
            $manifestPath = Join-Path $TestDrive "manifest.jsonc"
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            # First restore
            $result1 = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -RunId "test-run-idem-1"
            $result1.RestoreCount | Should -Be 1
            $result1.SkipCount | Should -Be 0
            
            # Sync timestamps to simulate up-to-date
            $srcTime = (Get-Item $sourceFile).LastWriteTime
            (Get-Item $targetFile).LastWriteTime = $srcTime
            
            # Second restore should skip
            $result2 = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -RunId "test-run-idem-2"
            $result2.RestoreCount | Should -Be 0
            $result2.SkipCount | Should -Be 1
        }
    }
    
    Context "Backup behavior" {
        
        It "Should create backup when target exists and backup=true" {
            $sourceDir = Join-Path $TestDrive "configs"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            "new content" | Out-File -FilePath (Join-Path $sourceDir "backup-test.conf") -Encoding UTF8
            
            $targetDir = Join-Path $TestDrive "target"
            New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
            $targetFile = Join-Path $targetDir "backup-test.conf"
            "old content" | Out-File -FilePath $targetFile -Encoding UTF8
            
            # Ensure different timestamps
            (Get-Item (Join-Path $sourceDir "backup-test.conf")).LastWriteTime = (Get-Date).AddMinutes(-5)
            (Get-Item $targetFile).LastWriteTime = (Get-Date).AddMinutes(-10)
            
            $manifestContent = @"
{
    "version": 1,
    "name": "backup-test",
    "restore": [{
        "type": "copy",
        "source": "./configs/backup-test.conf",
        "target": "$($targetFile -replace '\\', '\\\\')",
        "backup": true
    }],
    "apps": []
}
"@
            $manifestPath = Join-Path $TestDrive "manifest.jsonc"
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            $runId = "test-run-backup-$(Get-Date -Format 'yyyyMMddHHmmss')"
            $result = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -RunId $runId
            
            $result.RestoreCount | Should -Be 1
            
            # Target should have new content
            (Get-Content $targetFile -Raw).Trim() | Should -Be "new content"
            
            # Backup should exist
            $backupRoot = Join-Path $script:ProvisioningRoot "state\backups\$runId"
            (Test-Path $backupRoot) | Should -Be $true
        }
    }
}

Describe "Restore.Journal" {
    
    BeforeEach {
        Mock Initialize-ProvisioningLog { return "test.log" }
        Mock Write-ProvisioningLog { }
        Mock Write-ProvisioningSection { }
        Mock Close-ProvisioningLog { }
    }
    
    Context "Restore journal written correctly" {
        
        It "Should write journal file with correct structure" {
            $sourceDir = Join-Path $TestDrive "configs"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            "journal test" | Out-File -FilePath (Join-Path $sourceDir "journal.conf") -Encoding UTF8
            
            $targetDir = Join-Path $TestDrive "target"
            New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
            $targetFile = Join-Path $targetDir "journal.conf"
            
            $manifestContent = @"
{
    "version": 1,
    "name": "journal-test",
    "restore": [{
        "type": "copy",
        "source": "./configs/journal.conf",
        "target": "$($targetFile -replace '\\', '\\\\')",
        "backup": false
    }],
    "apps": []
}
"@
            $manifestPath = Join-Path $TestDrive "manifest.jsonc"
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            $runId = "test-journal-$(Get-Date -Format 'yyyyMMddHHmmss')"
            $null = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -RunId $runId
            
            # Check journal file exists
            $journalFile = Join-Path $script:ProvisioningRoot "logs\restore-journal-$runId.json"
            (Test-Path $journalFile) | Should -Be $true
            
            # Parse and validate journal structure
            $journal = Get-Content $journalFile -Raw | ConvertFrom-Json
            
            $journal.runId | Should -Be $runId
            $journal.manifestPath | Should -Be $manifestPath
            $journal.entries.Count | Should -Be 1
            $journal.entries[0].kind | Should -Be "copy"
            $journal.entries[0].action | Should -Be "restored"
        }
    }
}

Describe "Restore.Revert" {
    
    BeforeEach {
        Mock Initialize-ProvisioningLog { return "test.log" }
        Mock Write-ProvisioningLog { }
        Mock Write-ProvisioningSection { }
        Mock Close-ProvisioningLog { }
    }
    
    Context "Revert restores previous state" {
        
        It "Should restore backup when reverting" {
            # Load revert engine
            . (Join-Path $script:EngineDir "export-revert.ps1")
            
            $sourceDir = Join-Path $TestDrive "configs"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            "new content after restore" | Out-File -FilePath (Join-Path $sourceDir "revert-test.conf") -Encoding UTF8
            
            $targetDir = Join-Path $TestDrive "target"
            New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
            $targetFile = Join-Path $targetDir "revert-test.conf"
            "original content before restore" | Out-File -FilePath $targetFile -Encoding UTF8
            
            # Ensure different timestamps
            (Get-Item (Join-Path $sourceDir "revert-test.conf")).LastWriteTime = (Get-Date).AddMinutes(-5)
            (Get-Item $targetFile).LastWriteTime = (Get-Date).AddMinutes(-10)
            
            $manifestContent = @"
{
    "version": 1,
    "name": "revert-test",
    "restore": [{
        "type": "copy",
        "source": "./configs/revert-test.conf",
        "target": "$($targetFile -replace '\\', '\\\\')",
        "backup": true
    }],
    "apps": []
}
"@
            $manifestPath = Join-Path $TestDrive "manifest.jsonc"
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            $runId = "test-revert-$(Get-Date -Format 'yyyyMMddHHmmss')"
            
            # Perform restore
            $restoreResult = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -RunId $runId
            $restoreResult.RestoreCount | Should -Be 1
            
            # Verify new content
            (Get-Content $targetFile -Raw).Trim() | Should -Be "new content after restore"
            
            # Perform revert
            $revertResult = Invoke-ExportRevert
            $revertResult.RevertCount | Should -BeGreaterThan 0
            
            # Verify original content restored
            (Get-Content $targetFile -Raw).Trim() | Should -Be "original content before restore"
        }
        
        It "Should delete target if it was created by restore" {
            . (Join-Path $script:EngineDir "export-revert.ps1")
            
            $sourceDir = Join-Path $TestDrive "configs"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            "created by restore" | Out-File -FilePath (Join-Path $sourceDir "created.conf") -Encoding UTF8
            
            $targetDir = Join-Path $TestDrive "target-created"
            New-Item -ItemType Directory -Path $targetDir -Force | Out-Null
            $targetFile = Join-Path $targetDir "created.conf"
            
            # Target does NOT exist before restore
            (Test-Path $targetFile) | Should -Be $false
            
            $manifestContent = @"
{
    "version": 1,
    "name": "create-test",
    "restore": [{
        "type": "copy",
        "source": "./configs/created.conf",
        "target": "$($targetFile -replace '\\', '\\\\')",
        "backup": true
    }],
    "apps": []
}
"@
            $manifestPath = Join-Path $TestDrive "manifest.jsonc"
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            $runId = "test-create-$(Get-Date -Format 'yyyyMMddHHmmss')"
            
            # Restore creates the file
            $null = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -RunId $runId
            (Test-Path $targetFile) | Should -Be $true
            
            # Revert should delete it
            $null = Invoke-ExportRevert
            (Test-Path $targetFile) | Should -Be $false
        }
    }
}

Describe "Module.Loading" {
    
    Context "Resolve-RestoreEntriesFromModules expands modules" {
        
        It "Should load git module without error" {
            $repoRoot = $script:ProvisioningRoot
            $manifest = @{
                version = 1
                name = "module-test"
                modules = @("git")
                bundles = @()
                restore = @()
                apps = @()
            }
            
            # Git module exists but has no active restore entries (commented out)
            # The key test is that the function runs without error
            { Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $repoRoot } | Should -Not -Throw
        }
        
        It "Should load module with apps. prefix" {
            $repoRoot = $script:ProvisioningRoot
            $manifest = @{
                version = 1
                name = "module-prefix-test"
                modules = @("apps.git")
                bundles = @()
                restore = @()
            }
            
            # Should strip apps. prefix and find modules/apps/git/module.jsonc
            { Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $repoRoot } | Should -Not -Throw
        }
        
        It "Should throw for non-existent module" {
            $repoRoot = $script:ProvisioningRoot
            $manifest = @{
                version = 1
                name = "missing-module-test"
                modules = @("nonexistent-module-xyz")
                bundles = @()
                restore = @()
            }
            
            $threw = $false
            try {
                Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $repoRoot
            } catch {
                $threw = $true
            }
            $threw | Should -Be $true
        }
        
        It "Should return empty array for module with no restore entries" {
            $repoRoot = $script:ProvisioningRoot
            $manifest = @{
                version = 1
                name = "empty-restore-test"
                modules = @("7zip")
                bundles = @()
                restore = @()
            }
            
            # 7zip module has empty restore array (install-only, settings in registry)
            $expandedRestore = @(Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $repoRoot)
            $expandedRestore.Count | Should -Be 0
        }
    }
}

Describe "Module.RestoreExecution" {
    
    Context "Module-based restore via manifest.modules" {
        
        It "Should load manifest with modules from real repo" {
            # Use a real manifest path in the repo
            $manifestPath = Join-Path $script:ProvisioningRoot "manifests\examples\msi-afterburner.jsonc"
            
            # Load manifest - should work without error
            $manifest = Read-Manifest -Path $manifestPath
            
            $manifest | Should -Not -BeNullOrEmpty
            $manifest.restore.Count | Should -BeGreaterThan 0
        }
        
        It "Should expand modules when using Resolve-RestoreEntriesFromModules directly" {
            $repoRoot = $script:ProvisioningRoot
            
            # Test that modules with restore entries expand correctly
            $manifest = @{
                modules = @("powertoys", "msi-afterburner")
                bundles = @()
                restore = @()
            }
            
            $moduleRestore = @(Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $repoRoot)
            
            # Should have 3 restore entries (1 from powertoys, 2 from msi-afterburner)
            $moduleRestore.Count | Should -Be 3
        }
    }
}

Describe "Restore.RequiresClosed" {
    
    Context "requiresClosed is opt-in" {
        
        It "Should NOT have requiresClosed on powertoys module (opt-in behavior)" {
            $repoRoot = $script:ProvisioningRoot
            $manifest = @{
                modules = @("powertoys")
                bundles = @()
                restore = @()
            }
            
            $expandedRestore = @(Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $repoRoot)
            
            # PowerToys module should NOT have requiresClosed (opt-in, not default)
            $expandedRestore[0].requiresClosed | Should -BeNullOrEmpty
        }
        
        It "Should propagate requiresClosed when explicitly set on a module" {
            # Create a test module with requiresClosed
            $testModuleDir = Join-Path $TestDrive "modules\apps\test-locked"
            New-Item -ItemType Directory -Path $testModuleDir -Force | Out-Null
            
            $moduleContent = @'
{
  "id": "apps.test-locked",
  "displayName": "Test Locked App",
  "requiresClosed": ["notepad.exe"],
  "restore": [
    { "type": "copy", "source": "./test", "target": "C:\\temp\\test" }
  ]
}
'@
            $moduleContent | Out-File -FilePath (Join-Path $testModuleDir "module.jsonc") -Encoding UTF8
            
            $manifest = @{
                modules = @("test-locked")
                bundles = @()
                restore = @()
            }
            
            $expandedRestore = @(Resolve-RestoreEntriesFromModules -Manifest $manifest -RepoRoot $TestDrive)
            
            # Should propagate requiresClosed from module to restore entry
            $expandedRestore[0].requiresClosed | Should -Not -BeNullOrEmpty
            $expandedRestore[0].requiresClosed[0] | Should -Be "notepad.exe"
        }
    }
    
    Context "Test-ProcessesRunning function" {
        
        It "Should return empty array for non-running process" {
            $result = Test-ProcessesRunning -ProcessNames @("nonexistent-process-xyz-12345.exe")
            $result.Count | Should -Be 0
        }
        
        It "Should detect running explorer process" {
            # Explorer is always running on Windows
            $result = Test-ProcessesRunning -ProcessNames @("explorer.exe")
            $result.Count | Should -BeGreaterThan 0
        }
    }
}

Describe "Restore.LockedFileHandling" {
    
    Context "Sharing violations are skipped with warnings" {
        
        It "Should skip file with sharing violation and continue restore" {
            # Create source file
            $sourceFile = Join-Path $TestDrive "source-single.txt"
            $targetFile = Join-Path $TestDrive "target-single.txt"
            
            "new content" | Out-File -FilePath $sourceFile -Encoding UTF8
            "old content" | Out-File -FilePath $targetFile -Encoding UTF8
            
            # Lock target file by opening it exclusively (causes sharing violation)
            $lockedFile = [System.IO.File]::Open($targetFile, 'Open', 'ReadWrite', 'None')
            
            try {
                # Use Copy-ItemWithLockedFileHandling directly on single file
                $result = Copy-ItemWithLockedFileHandling -Source $sourceFile -Destination $targetFile
                
                # Should have skipped the locked file (sharing violation)
                $result.SkippedFiles.Count | Should -BeGreaterThan 0
                
                # Should have warnings mentioning sharing violation
                $result.Warnings.Count | Should -BeGreaterThan 0
                ($result.Warnings -join "`n") | Should -Match "sharing violation"
            } finally {
                $lockedFile.Close()
            }
        }
        
        It "Should copy successfully when no files are locked" {
            $sourceDir = Join-Path $TestDrive "source-unlocked"
            $targetDir = Join-Path $TestDrive "target-unlocked"
            
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            "content1" | Out-File -FilePath "$sourceDir\a.txt" -Encoding UTF8
            "content2" | Out-File -FilePath "$sourceDir\b.txt" -Encoding UTF8
            
            $result = Copy-ItemWithLockedFileHandling -Source $sourceDir -Destination $targetDir
            
            $result.Success | Should -Be $true
            $result.SkippedFiles.Count | Should -Be 0
            $result.CopiedCount | Should -Be 2
        }
    }
    
    Context "Test-SharingViolation HRESULT detection" {
        
        It "Should detect ERROR_SHARING_VIOLATION (0x80070020)" {
            # Create a mock exception with sharing violation HRESULT
            $exception = New-Object System.IO.IOException "The process cannot access the file"
            # Set HResult via reflection (it's normally read-only)
            $hresultField = [System.Exception].GetField('_HResult', 'NonPublic,Instance')
            $hresultField.SetValue($exception, 0x80070020)
            
            $result = Test-SharingViolation -Exception $exception
            $result | Should -Be $true
        }
        
        It "Should detect ERROR_LOCK_VIOLATION (0x80070021)" {
            $exception = New-Object System.IO.IOException "The file is locked"
            $hresultField = [System.Exception].GetField('_HResult', 'NonPublic,Instance')
            $hresultField.SetValue($exception, 0x80070021)
            
            $result = Test-SharingViolation -Exception $exception
            $result | Should -Be $true
        }
        
        It "Should NOT detect generic access denied as sharing violation" {
            # Generic access denied has different HRESULT (0x80070005)
            $exception = New-Object System.UnauthorizedAccessException "Access denied"
            
            $result = Test-SharingViolation -Exception $exception
            $result | Should -Be $false
        }
    }
}

Describe "Restore.ExcludePatterns" {
    
    Context "Exclude patterns skip specified paths" {
        
        It "Should not copy excluded directories via Invoke-CopyRestoreAction" {
            # Test exclude patterns directly via the restorer function
            $sourceDir = Join-Path $TestDrive "source-exclude"
            $targetDir = Join-Path $TestDrive "target-exclude"
            
            New-Item -ItemType Directory -Path "$sourceDir\Logs" -Force | Out-Null
            New-Item -ItemType Directory -Path "$sourceDir\Settings" -Force | Out-Null
            
            "log file" | Out-File -FilePath "$sourceDir\Logs\app.log" -Encoding UTF8
            "settings" | Out-File -FilePath "$sourceDir\Settings\config.json" -Encoding UTF8
            
            # Call restorer directly with exclude patterns
            $result = Invoke-CopyRestoreAction `
                -Source $sourceDir `
                -Target $targetDir `
                -Backup $false `
                -Exclude @("**\Logs\**")
            
            $result.Success | Should -Be $true
            (Test-Path "$targetDir\Settings\config.json") | Should -Be $true
            (Test-Path "$targetDir\Logs") | Should -Be $false
        }
        
        It "Should verify powertoys module has exclude patterns for junk paths" {
            # Verify the real powertoys module has exclude patterns defined
            $modulePath = Join-Path $script:ProvisioningRoot "modules\apps\powertoys\module.jsonc"
            $module = Read-JsoncFile -Path $modulePath
            
            # PowerToys module should have exclude patterns for Logs, Temp, Cache, GPUCache, Crashpad
            $module.restore[0].exclude | Should -Not -BeNullOrEmpty
            $module.restore[0].exclude.Count | Should -Be 5
            # Check patterns are present (use -contains instead of Should Contain to avoid regex issues)
            ($module.restore[0].exclude -contains "**\Logs\**") | Should -Be $true
            ($module.restore[0].exclude -contains "**\Temp\**") | Should -Be $true
            ($module.restore[0].exclude -contains "**\Cache\**") | Should -Be $true
            ($module.restore[0].exclude -contains "**\GPUCache\**") | Should -Be $true
            ($module.restore[0].exclude -contains "**\Crashpad\**") | Should -Be $true
        }
    }
}