<#
.SYNOPSIS
    Pester tests for Model B export-root support and restore journaling.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    $script:RestoreScript = Join-Path $script:ProvisioningRoot "engine\restore.ps1"
    $script:RevertScript = Join-Path $script:ProvisioningRoot "engine\export-revert.ps1"
    
    # Load dependencies
    . (Join-Path $script:ProvisioningRoot "engine\logging.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\manifest.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\state.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\export-capture.ps1")
    . $script:RestoreScript
}

Describe "Restore.ModelB.ExportRoot" {
    
    Context "Source resolution prefers export root over manifest dir" {
        
        It "Should resolve source from export root when -Export is provided" {
            # Setup test directories
            $manifestDir = Join-Path $TestDrive "manifest"
            $exportDir = Join-Path $TestDrive "export"
            New-Item -ItemType Directory -Path $manifestDir -Force | Out-Null
            New-Item -ItemType Directory -Path $exportDir -Force | Out-Null
            
            # Create source file in export root
            $exportSourceDir = Join-Path $exportDir "configs"
            New-Item -ItemType Directory -Path $exportSourceDir -Force | Out-Null
            $exportSourceFile = Join-Path $exportSourceDir "app.conf"
            "export-content" | Out-File -FilePath $exportSourceFile -Encoding UTF8
            
            # Create different source file in manifest dir (should be ignored)
            $manifestSourceDir = Join-Path $manifestDir "configs"
            New-Item -ItemType Directory -Path $manifestSourceDir -Force | Out-Null
            $manifestSourceFile = Join-Path $manifestSourceDir "app.conf"
            "manifest-content" | Out-File -FilePath $manifestSourceFile -Encoding UTF8
            
            # Create manifest
            $manifestPath = Join-Path $manifestDir "test.jsonc"
            $manifestContent = @'
{
    "version": 1,
    "name": "test-manifest",
    "apps": [],
    "restore": [
        {
            "type": "copy",
            "source": "configs/app.conf",
            "target": "~/.config/app.conf"
        }
    ]
}
'@
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            # Mock logging functions
            Mock Initialize-ProvisioningLog { return "test.log" }
            Mock Write-ProvisioningLog { }
            Mock Write-ProvisioningSection { }
            Mock Close-ProvisioningLog { }
            
            # Run restore with -Export in dry-run mode
            $result = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -DryRun -ExportPath $exportDir
            
            # Verify source was resolved from export root
            $result | Should -Not -BeNullOrEmpty
            $result.Results | Should -Not -BeNullOrEmpty
            $result.Results[0].expandedSource | Should -BeLike "*export*configs*app.conf"
            $result.Results[0].expandedSource | Should -Not -BeLike "*manifest*configs*app.conf"
        }
        
        It "Should fallback to manifest dir when source not found in export root" {
            # Setup test directories
            $manifestDir = Join-Path $TestDrive "manifest2"
            $exportDir = Join-Path $TestDrive "export2"
            New-Item -ItemType Directory -Path $manifestDir -Force | Out-Null
            New-Item -ItemType Directory -Path $exportDir -Force | Out-Null
            
            # Create source file ONLY in manifest dir
            $manifestSourceDir = Join-Path $manifestDir "configs"
            New-Item -ItemType Directory -Path $manifestSourceDir -Force | Out-Null
            $manifestSourceFile = Join-Path $manifestSourceDir "app.conf"
            "manifest-content" | Out-File -FilePath $manifestSourceFile -Encoding UTF8
            
            # Create manifest
            $manifestPath = Join-Path $manifestDir "test.jsonc"
            $manifestContent = @'
{
    "version": 1,
    "name": "test-manifest",
    "apps": [],
    "restore": [
        {
            "type": "copy",
            "source": "configs/app.conf",
            "target": "~/.config/app.conf"
        }
    ]
}
'@
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            # Mock logging functions
            Mock Initialize-ProvisioningLog { return "test.log" }
            Mock Write-ProvisioningLog { }
            Mock Write-ProvisioningSection { }
            Mock Close-ProvisioningLog { }
            
            # Run restore with -Export in dry-run mode
            $result = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -DryRun -ExportPath $exportDir
            
            # Verify source was resolved from manifest dir (fallback)
            $result | Should -Not -BeNullOrEmpty
            $result.Results | Should -Not -BeNullOrEmpty
            $result.Results[0].expandedSource | Should -BeLike "*manifest2*configs*app.conf"
        }
    }
}

Describe "Restore.Journaling" {
    
    Context "Journal is written for non-dry-run restore operations" {
        
        It "Should write journal file after restore completes" {
            # Setup test directories
            $manifestDir = Join-Path $TestDrive "journal-test"
            $logsDir = Join-Path $script:ProvisioningRoot "logs"
            New-Item -ItemType Directory -Path $manifestDir -Force | Out-Null
            
            # Create source file
            $sourceFile = Join-Path $manifestDir "source.txt"
            "test-content" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            # Create manifest
            $manifestPath = Join-Path $manifestDir "test.jsonc"
            $targetPath = Join-Path $TestDrive "target.txt"
            $manifestContent = @"
{
    "version": 1,
    "name": "test-manifest",
    "apps": [],
    "restore": [
        {
            "type": "copy",
            "source": "source.txt",
            "target": "$($targetPath -replace '\\', '\\')"
        }
    ]
}
"@
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            # Mock logging functions
            Mock Initialize-ProvisioningLog { return "test.log" }
            Mock Write-ProvisioningLog { }
            Mock Write-ProvisioningSection { }
            Mock Close-ProvisioningLog { }
            
            # Run restore (NOT dry-run)
            $result = Invoke-Restore -ManifestPath $manifestPath -EnableRestore
            
            # Verify journal was created
            $journalPattern = "restore-journal-$($result.RunId).json"
            $journalFile = Join-Path $logsDir $journalPattern
            
            Test-Path $journalFile | Should -Be $true
            
            # Verify journal content
            $journal = Get-Content -Path $journalFile -Raw | ConvertFrom-Json
            $journal.runId | Should -Be $result.RunId
            $journal.entries | Should -Not -BeNullOrEmpty
            $journal.entries[0].source | Should -Be "source.txt"
            $journal.entries[0].action | Should -BeIn @("restored", "skipped_up_to_date")
            
            # Cleanup
            if (Test-Path $journalFile) {
                Remove-Item -Path $journalFile -Force
            }
            if (Test-Path $targetPath) {
                Remove-Item -Path $targetPath -Force
            }
        }
        
        It "Should NOT write journal for dry-run restore" {
            # Setup test directories
            $manifestDir = Join-Path $TestDrive "journal-dryrun"
            $logsDir = Join-Path $script:ProvisioningRoot "logs"
            New-Item -ItemType Directory -Path $manifestDir -Force | Out-Null
            
            # Create source file
            $sourceFile = Join-Path $manifestDir "source.txt"
            "test-content" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            # Create manifest
            $manifestPath = Join-Path $manifestDir "test.jsonc"
            $targetPath = Join-Path $TestDrive "target-dryrun.txt"
            $manifestContent = @"
{
    "version": 1,
    "name": "test-manifest",
    "apps": [],
    "restore": [
        {
            "type": "copy",
            "source": "source.txt",
            "target": "$($targetPath -replace '\\', '\\')"
        }
    ]
}
"@
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            # Mock logging functions
            Mock Initialize-ProvisioningLog { return "test.log" }
            Mock Write-ProvisioningLog { }
            Mock Write-ProvisioningSection { }
            Mock Close-ProvisioningLog { }
            
            # Run restore in DRY-RUN mode
            $result = Invoke-Restore -ManifestPath $manifestPath -EnableRestore -DryRun
            
            # Verify journal was NOT created
            $journalPattern = "restore-journal-$($result.RunId).json"
            $journalFile = Join-Path $logsDir $journalPattern
            
            Test-Path $journalFile | Should -Be $false
        }
    }
}

Describe "Revert.JournalBased" {
    
    Context "Revert can delete newly created targets" {
        
        It "Should delete target that was created by restore (targetExistedBefore=false)" {
            # Setup
            $manifestDir = Join-Path $TestDrive "revert-delete"
            $logsDir = Join-Path $script:ProvisioningRoot "logs"
            New-Item -ItemType Directory -Path $manifestDir -Force | Out-Null
            
            # Create source file
            $sourceFile = Join-Path $manifestDir "source.txt"
            "test-content" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            # Target that doesn't exist yet
            $targetPath = Join-Path $TestDrive "new-target.txt"
            
            # Create manifest
            $manifestPath = Join-Path $manifestDir "test.jsonc"
            $manifestContent = @"
{
    "version": 1,
    "name": "test-manifest",
    "apps": [],
    "restore": [
        {
            "type": "copy",
            "source": "source.txt",
            "target": "$($targetPath -replace '\\', '\\')"
        }
    ]
}
"@
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            # Mock logging functions
            Mock Initialize-ProvisioningLog { return "test.log" }
            Mock Write-ProvisioningLog { }
            Mock Write-ProvisioningSection { }
            Mock Close-ProvisioningLog { }
            
            # Run restore to create the target
            $restoreResult = Invoke-Restore -ManifestPath $manifestPath -EnableRestore
            
            # Verify target was created
            Test-Path $targetPath | Should -Be $true
            
            # Load revert script
            . $script:RevertScript
            
            # Run revert
            $revertResult = Invoke-ExportRevert
            
            # Verify target was deleted
            Test-Path $targetPath | Should -Be $false
            $revertResult.RevertCount | Should -BeGreaterThan 0
            
            # Cleanup journal
            $journalFile = Join-Path $logsDir "restore-journal-$($restoreResult.RunId).json"
            if (Test-Path $journalFile) {
                Remove-Item -Path $journalFile -Force
            }
        }
    }
}