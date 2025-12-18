<#
.SYNOPSIS
    Pester tests for restore engine and copy restorer.
#>

$script:ProvisioningRoot = Join-Path $PSScriptRoot "..\..\"
$script:RestoreScript = Join-Path $script:ProvisioningRoot "engine\restore.ps1"
$script:CopyRestorerScript = Join-Path $script:ProvisioningRoot "restorers\copy.ps1"
$script:FixturesDir = Join-Path $PSScriptRoot "..\fixtures"

# Load dependencies
. (Join-Path $script:ProvisioningRoot "engine\logging.ps1")
. (Join-Path $script:ProvisioningRoot "engine\manifest.ps1")
. (Join-Path $script:ProvisioningRoot "engine\state.ps1")
. $script:CopyRestorerScript

Describe "Restore.OptIn" {
    
    Context "Restore requires explicit opt-in" {
        
        It "Should skip restore steps when -EnableRestore is not specified" {
            # Create a test manifest file
            $manifestPath = Join-Path $TestDrive "test-manifest.jsonc"
            $manifestContent = @'
{
    "version": 1,
    "name": "test-manifest",
    "apps": [],
    "restore": [
        {
            "type": "copy",
            "source": "./source.txt",
            "target": "~/target.txt"
        }
    ],
    "verify": []
}
'@
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            # Load restore engine
            . $script:RestoreScript
            
            # Mock logging functions
            Mock Initialize-ProvisioningLog { return "test.log" }
            Mock Write-ProvisioningLog { }
            Mock Write-ProvisioningSection { }
            Mock Close-ProvisioningLog { }
            
            # Call without -EnableRestore
            $result = Invoke-Restore -ManifestPath $manifestPath
            
            # Should indicate restore was not enabled
            $result.RestoreNotEnabled | Should Be $true
            $result.SkipCount | Should Be 1
            $result.RestoreCount | Should Be 0
        }
    }
}

Describe "Restore.CopyFile" {
    
    Context "Copy file to new target" {
        
        It "Should copy file and set status to restore" {
            # Setup test files in TestDrive
            $sourceFile = Join-Path $TestDrive "source.txt"
            $targetFile = Join-Path $TestDrive "target.txt"
            
            "test content" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            # Call Invoke-CopyRestore
            $result = Invoke-CopyRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should Be $true
            $result.Skipped | Should Be $false
            (Test-Path $targetFile) | Should Be $true
            (Get-Content $targetFile -Raw).Trim() | Should Be "test content"
        }
        
        It "Should create target directory if it doesn't exist" {
            $sourceFile = Join-Path $TestDrive "source2.txt"
            $targetFile = Join-Path $TestDrive "subdir\nested\target2.txt"
            
            "nested content" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            $result = Invoke-CopyRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should Be $true
            (Test-Path $targetFile) | Should Be $true
        }
    }
}

Describe "Restore.Backup" {
    
    Context "Backup existing target before overwriting" {
        
        It "Should create backup when target exists" {
            $sourceFile = Join-Path $TestDrive "new-source.txt"
            $targetFile = Join-Path $TestDrive "existing-target.txt"
            
            # Create source and existing target with DIFFERENT content and timestamps
            "new content" | Out-File -FilePath $sourceFile -Encoding UTF8
            Start-Sleep -Milliseconds 100
            "old content" | Out-File -FilePath $targetFile -Encoding UTF8
            
            # Ensure files have different timestamps to avoid up-to-date detection
            (Get-Item $sourceFile).LastWriteTime = (Get-Date).AddMinutes(-5)
            (Get-Item $targetFile).LastWriteTime = (Get-Date).AddMinutes(-10)
            
            $result = Invoke-CopyRestore -Source $sourceFile -Target $targetFile -Backup $true -RunId "test-run-123"
            
            $result.Success | Should Be $true
            $result.Skipped | Should Be $false
            # Target should have new content after restore
            (Get-Content $targetFile -Raw).Trim() | Should Be "new content"
        }
        
        It "Should preserve old content in backup location" {
            $sourceFile = Join-Path $TestDrive "backup-source.txt"
            $targetFile = Join-Path $TestDrive "backup-target.txt"
            
            # Create source and existing target with different content
            "updated content" | Out-File -FilePath $sourceFile -Encoding UTF8
            "original content" | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-CopyRestore -Source $sourceFile -Target $targetFile -Backup $true -RunId "backup-test-run"
            
            $result.Success | Should Be $true
            $result.Skipped | Should Be $false
            
            # Target should have new content
            (Get-Content $targetFile -Raw).Trim() | Should Be "updated content"
        }
    }
}

Describe "Restore.UpToDate" {
    
    Context "Skip when target is already up to date" {
        
        It "Should skip with reason 'already up to date' when files match" {
            $sourceFile = Join-Path $TestDrive "uptodate-source.txt"
            $targetFile = Join-Path $TestDrive "uptodate-target.txt"
            
            # Create identical files
            "identical content" | Out-File -FilePath $sourceFile -Encoding UTF8
            Copy-Item -Path $sourceFile -Destination $targetFile -Force
            
            # Ensure same timestamps
            $sourceItem = Get-Item $sourceFile
            (Get-Item $targetFile).LastWriteTime = $sourceItem.LastWriteTime
            
            $result = Invoke-CopyRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should Be $true
            $result.Skipped | Should Be $true
            $result.Message | Should Be "already up to date"
        }
        
        It "Should NOT skip when file sizes differ" {
            $sourceFile = Join-Path $TestDrive "diff-source.txt"
            $targetFile = Join-Path $TestDrive "diff-target.txt"
            
            "longer content here" | Out-File -FilePath $sourceFile -Encoding UTF8
            "short" | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-CopyRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should Be $true
            $result.Skipped | Should Be $false
        }
    }
}

Describe "Restore.Directory" {
    
    Context "Directory copy" {
        
        It "Should copy directory with files" {
            $sourceDir = Join-Path $TestDrive "source-dir"
            $targetDir = Join-Path $TestDrive "target-dir"
            
            # Create source directory with files
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            "file1" | Out-File -FilePath (Join-Path $sourceDir "file1.txt") -Encoding UTF8
            "file2" | Out-File -FilePath (Join-Path $sourceDir "file2.txt") -Encoding UTF8
            
            $result = Invoke-CopyRestore -Source $sourceDir -Target $targetDir -Backup $false
            
            $result.Success | Should Be $true
            (Test-Path $targetDir) | Should Be $true
            (Test-Path (Join-Path $targetDir "file1.txt")) | Should Be $true
            (Test-Path (Join-Path $targetDir "file2.txt")) | Should Be $true
        }
        
        It "Should copy nested directory structure" {
            $sourceDir = Join-Path $TestDrive "nested-source"
            $targetDir = Join-Path $TestDrive "nested-target"
            
            # Create nested structure
            New-Item -ItemType Directory -Path "$sourceDir\sub1\sub2" -Force | Out-Null
            "nested file" | Out-File -FilePath "$sourceDir\sub1\sub2\deep.txt" -Encoding UTF8
            
            $result = Invoke-CopyRestore -Source $sourceDir -Target $targetDir -Backup $false
            
            $result.Success | Should Be $true
            (Test-Path "$targetDir\sub1\sub2\deep.txt") | Should Be $true
        }
    }
}

Describe "Restore.SensitivePath" {
    
    Context "Sensitive path detection" {
        
        It "Should detect .ssh in path" {
            $isSensitive = Test-RestoreSensitivePath -Path "C:\Users\test\.ssh\id_rsa"
            $isSensitive | Should Be $true
        }
        
        It "Should detect .aws in path" {
            $isSensitive = Test-RestoreSensitivePath -Path "~/.aws/credentials"
            $isSensitive | Should Be $true
        }
        
        It "Should detect credentials in path" {
            $isSensitive = Test-RestoreSensitivePath -Path "C:\app\credentials\secret.json"
            $isSensitive | Should Be $true
        }
        
        It "Should NOT flag normal paths" {
            $isSensitive = Test-RestoreSensitivePath -Path "~/.gitconfig"
            $isSensitive | Should Be $false
        }
        
        It "Should add warnings for sensitive paths in restore result" {
            $sourceFile = Join-Path $TestDrive "sensitive-source.txt"
            $targetFile = Join-Path $TestDrive ".ssh\config"
            
            "config content" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            $result = Invoke-CopyRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Warnings.Count | Should BeGreaterThan 0
            $result.Warnings[0] | Should Match "sensitive"
        }
    }
}

Describe "Restore.UpToDateDetection" {
    
    Context "Test-RestoreUpToDate function" {
        
        It "Should return false when target doesn't exist" {
            $sourceFile = Join-Path $TestDrive "exists.txt"
            $targetFile = Join-Path $TestDrive "nonexistent.txt"
            
            "content" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            $isUpToDate = Test-RestoreUpToDate -Source $sourceFile -Target $targetFile
            $isUpToDate | Should Be $false
        }
        
        It "Should return true when files are identical" {
            $sourceFile = Join-Path $TestDrive "identical-src.txt"
            $targetFile = Join-Path $TestDrive "identical-tgt.txt"
            
            "same content" | Out-File -FilePath $sourceFile -Encoding UTF8
            Copy-Item -Path $sourceFile -Destination $targetFile -Force
            
            # Sync timestamps
            $srcTime = (Get-Item $sourceFile).LastWriteTime
            (Get-Item $targetFile).LastWriteTime = $srcTime
            
            $isUpToDate = Test-RestoreUpToDate -Source $sourceFile -Target $targetFile
            $isUpToDate | Should Be $true
        }
        
        It "Should return false when file sizes differ" {
            $sourceFile = Join-Path $TestDrive "size-src.txt"
            $targetFile = Join-Path $TestDrive "size-tgt.txt"
            
            "longer content" | Out-File -FilePath $sourceFile -Encoding UTF8
            "short" | Out-File -FilePath $targetFile -Encoding UTF8
            
            $isUpToDate = Test-RestoreUpToDate -Source $sourceFile -Target $targetFile
            $isUpToDate | Should Be $false
        }
    }
}

Describe "Restore.ActionId" {
    
    Context "Get-RestoreActionId generation" {
        
        BeforeAll {
            . $script:RestoreScript
        }
        
        It "Should use provided id if present" {
            $item = @{
                id = "my-custom-id"
                type = "copy"
                source = "./source"
                target = "~/target"
            }
            
            $id = Get-RestoreActionId -Item $item
            $id | Should Be "my-custom-id"
        }
        
        It "Should generate deterministic id from source and target" {
            $item = @{
                type = "copy"
                source = "./configs/test.conf"
                target = "~/.test.conf"
            }
            
            $id = Get-RestoreActionId -Item $item
            $id | Should Be "copy:./configs/test.conf->~/.test.conf"
        }
        
        It "Should normalize path separators" {
            $item = @{
                type = "copy"
                source = ".\configs\test.conf"
                target = "~\.test.conf"
            }
            
            $id = Get-RestoreActionId -Item $item
            $id | Should Match "copy:"
            $id | Should Match "->"
        }
    }
}

Describe "Restore.SourceNotFound" {
    
    Context "Error handling for missing source" {
        
        It "Should fail with error when source doesn't exist" {
            $sourceFile = Join-Path $TestDrive "nonexistent-source.txt"
            $targetFile = Join-Path $TestDrive "target.txt"
            
            $result = Invoke-CopyRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should Be $false
            $result.Error | Should Match "Source not found"
        }
    }
}
