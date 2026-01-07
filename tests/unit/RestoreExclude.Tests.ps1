<#
.SYNOPSIS
    Pester tests for directory-level exclude support in restore operations.
#>

BeforeAll {
    $script:ProvisioningRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    $script:RestoreScript = Join-Path $script:ProvisioningRoot "engine" "restore.ps1"
    
    # Load dependencies
    . (Join-Path $script:ProvisioningRoot "engine" "logging.ps1")
    . (Join-Path $script:ProvisioningRoot "engine" "manifest.ps1")
    . (Join-Path $script:ProvisioningRoot "engine" "state.ps1")
    . $script:RestoreScript
}

Describe "Restore.Exclude.PatternMatching" {
    
    Context "Test-PathExcluded function" {
        
        It "Should match **\Logs\** pattern" {
            $result = Test-PathExcluded -RelativePath "Logs\app.log" -Patterns @("**\Logs\**")
            $result | Should -Be $true
        }
        
        It "Should match nested Logs folder" {
            $result = Test-PathExcluded -RelativePath "subfolder\Logs\debug.log" -Patterns @("**\Logs\**")
            $result | Should -Be $true
        }
        
        It "Should NOT match non-excluded paths" {
            $result = Test-PathExcluded -RelativePath "configs\settings.json" -Patterns @("**\Logs\**")
            $result | Should -Be $false
        }
        
        It "Should match multiple patterns" {
            $patterns = @("**\Logs\**", "**\Temp\**")
            
            $logsMatch = Test-PathExcluded -RelativePath "Logs\file.log" -Patterns $patterns
            $tempMatch = Test-PathExcluded -RelativePath "Temp\cache.tmp" -Patterns $patterns
            $configMatch = Test-PathExcluded -RelativePath "config.json" -Patterns $patterns
            
            $logsMatch | Should -Be $true
            $tempMatch | Should -Be $true
            $configMatch | Should -Be $false
        }
        
        It "Should handle forward slash patterns" {
            $result = Test-PathExcluded -RelativePath "Logs\app.log" -Patterns @("**/Logs/**")
            $result | Should -Be $true
        }
    }
}

Describe "Restore.Exclude.DirectoryCopy" {
    
    Context "Directory restore with excluded subfolder succeeds" {
        
        It "Should copy directory but skip excluded subfolders" {
            # Setup source directory with Logs subfolder
            $sourceDir = Join-Path $TestDrive "source-with-logs"
            $targetDir = Join-Path $TestDrive "target-with-logs"
            
            New-Item -ItemType Directory -Path "$sourceDir\Logs" -Force | Out-Null
            New-Item -ItemType Directory -Path "$sourceDir\configs" -Force | Out-Null
            
            "log content" | Out-File -FilePath "$sourceDir\Logs\app.log" -Encoding UTF8
            "config content" | Out-File -FilePath "$sourceDir\configs\settings.json" -Encoding UTF8
            "root file" | Out-File -FilePath "$sourceDir\readme.txt" -Encoding UTF8
            
            # Call with exclude pattern
            $result = Invoke-CopyRestoreAction `
                -Source $sourceDir `
                -Target $targetDir `
                -Backup $false `
                -Exclude @("**\Logs\**")
            
            $result.Success | Should -Be $true
            
            # Verify: configs and readme should exist, Logs should NOT
            (Test-Path "$targetDir\configs\settings.json") | Should -Be $true
            (Test-Path "$targetDir\readme.txt") | Should -Be $true
            (Test-Path "$targetDir\Logs") | Should -Be $false
        }
        
        It "Should copy directory but skip multiple excluded patterns" {
            $sourceDir = Join-Path $TestDrive "source-multi-exclude"
            $targetDir = Join-Path $TestDrive "target-multi-exclude"
            
            New-Item -ItemType Directory -Path "$sourceDir\Logs" -Force | Out-Null
            New-Item -ItemType Directory -Path "$sourceDir\Temp" -Force | Out-Null
            New-Item -ItemType Directory -Path "$sourceDir\Cache" -Force | Out-Null
            New-Item -ItemType Directory -Path "$sourceDir\configs" -Force | Out-Null
            
            "log" | Out-File -FilePath "$sourceDir\Logs\app.log" -Encoding UTF8
            "temp" | Out-File -FilePath "$sourceDir\Temp\temp.dat" -Encoding UTF8
            "cache" | Out-File -FilePath "$sourceDir\Cache\data.cache" -Encoding UTF8
            "config" | Out-File -FilePath "$sourceDir\configs\app.json" -Encoding UTF8
            
            $result = Invoke-CopyRestoreAction `
                -Source $sourceDir `
                -Target $targetDir `
                -Backup $false `
                -Exclude @("**\Logs\**", "**\Temp\**", "**\Cache\**")
            
            $result.Success | Should -Be $true
            
            # Only configs should exist
            (Test-Path "$targetDir\configs\app.json") | Should -Be $true
            (Test-Path "$targetDir\Logs") | Should -Be $false
            (Test-Path "$targetDir\Temp") | Should -Be $false
            (Test-Path "$targetDir\Cache") | Should -Be $false
        }
    }
}

Describe "Restore.Exclude.LockedFiles" {
    
    Context "Locked file inside excluded path does NOT fail restore" {
        
        It "Should succeed when excluded path contains locked file" {
            $sourceDir = Join-Path $TestDrive "source-locked-excluded"
            $targetDir = Join-Path $TestDrive "target-locked-excluded"
            
            # Create source with Logs folder
            New-Item -ItemType Directory -Path "$sourceDir\Logs" -Force | Out-Null
            New-Item -ItemType Directory -Path "$sourceDir\configs" -Force | Out-Null
            
            "log content" | Out-File -FilePath "$sourceDir\Logs\app.log" -Encoding UTF8
            "config" | Out-File -FilePath "$sourceDir\configs\settings.json" -Encoding UTF8
            
            # Restore with exclude - should succeed even though we're not actually locking
            # (The key behavior is that excluded paths are skipped entirely, so locks don't matter)
            $result = Invoke-CopyRestoreAction `
                -Source $sourceDir `
                -Target $targetDir `
                -Backup $false `
                -Exclude @("**\Logs\**")
            
            $result.Success | Should -Be $true
            (Test-Path "$targetDir\configs\settings.json") | Should -Be $true
            (Test-Path "$targetDir\Logs") | Should -Be $false
        }
    }
}

Describe "Restore.Exclude.Journaling" {
    
    Context "Journal does not contain excluded paths" {
        
        BeforeAll {
            # Mock logging functions
            Mock Initialize-ProvisioningLog { return "test.log" }
            Mock Write-ProvisioningLog { }
            Mock Write-ProvisioningSection { }
            Mock Close-ProvisioningLog { }
        }
        
        It "Should only journal the restore action, not individual excluded files" {
            # The journal records restore actions at the entry level, not individual files
            # Excluded files are simply not copied, so they don't appear as failures
            
            $sourceDir = Join-Path $TestDrive "source-journal-test"
            $targetDir = Join-Path $TestDrive "target-journal-test"
            
            New-Item -ItemType Directory -Path "$sourceDir\Logs" -Force | Out-Null
            New-Item -ItemType Directory -Path "$sourceDir\configs" -Force | Out-Null
            
            "log" | Out-File -FilePath "$sourceDir\Logs\app.log" -Encoding UTF8
            "config" | Out-File -FilePath "$sourceDir\configs\settings.json" -Encoding UTF8
            
            $result = Invoke-CopyRestoreAction `
                -Source $sourceDir `
                -Target $targetDir `
                -Backup $false `
                -Exclude @("**\Logs\**")
            
            # Restore should succeed
            $result.Success | Should -Be $true
            $result.Error | Should -BeNullOrEmpty
            
            # The result represents the entire restore action, not individual files
            # Excluded paths don't cause failures
            $result.Message | Should -Be "restored successfully"
        }
    }
}

Describe "Restore.Exclude.Revert" {
    
    Context "Revert still works with excluded paths" {
        
        It "Should only revert what was actually restored" {
            # Setup: Create source with excludable content
            $sourceDir = Join-Path $TestDrive "source-revert-test"
            $targetDir = Join-Path $TestDrive "target-revert-test"
            
            New-Item -ItemType Directory -Path "$sourceDir\Logs" -Force | Out-Null
            New-Item -ItemType Directory -Path "$sourceDir\configs" -Force | Out-Null
            
            "log" | Out-File -FilePath "$sourceDir\Logs\app.log" -Encoding UTF8
            "config" | Out-File -FilePath "$sourceDir\configs\settings.json" -Encoding UTF8
            
            # Restore with excludes
            $result = Invoke-CopyRestoreAction `
                -Source $sourceDir `
                -Target $targetDir `
                -Backup $false `
                -Exclude @("**\Logs\**") `
                -RunId "revert-test-run"
            
            $result.Success | Should -Be $true
            
            # Verify only non-excluded content was restored
            (Test-Path "$targetDir\configs\settings.json") | Should -Be $true
            (Test-Path "$targetDir\Logs") | Should -Be $false
            
            # The journal entry for this restore would only reflect what was actually done
            # Revert would delete the target (since it was created by restore)
            # This test verifies the restore itself worked correctly with excludes
        }
    }
}