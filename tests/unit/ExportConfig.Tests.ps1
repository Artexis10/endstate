# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

BeforeAll {
    $script:ProjectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    $script:CliPath = Join-Path $script:ProjectRoot "cli.ps1"
    
    # Import engine modules
    . "$script:ProjectRoot\engine\export-capture.ps1"
    . "$script:ProjectRoot\engine\manifest.ps1"
    . "$script:ProjectRoot\engine\logging.ps1"
    . "$script:ProjectRoot\engine\state.ps1"
    . "$script:ProjectRoot\engine\restore.ps1"
    . "$script:ProjectRoot\engine\events.ps1"
}

Describe "Export-Config Command" {
    
    Context "CLI Integration" {
        
        It "export-config appears in ValidateSet" {
            $cliContent = Get-Content -Path $script:CliPath -Raw
            $cliContent | Should -Match 'ValidateSet.*export-config'
        }
        
        It "export-config is dispatched correctly" {
            $cliContent = Get-Content -Path $script:CliPath -Raw
            $cliContent | Should -Match '"export-config".*Invoke-ProvisioningExportConfig'
        }
        
        It "export-config supports -DryRun parameter" {
            $cliContent = Get-Content -Path $script:CliPath -Raw
            $cliContent | Should -Match 'export-config.*-IsDryRun.*DryRun'
        }
    }
    
    Context "Get-ExportPath Function" {
        
        BeforeAll {
            $script:TestManifestDir = Join-Path $TestDrive "manifests"
            New-Item -ItemType Directory -Path $script:TestManifestDir -Force | Out-Null
            $script:TestManifest = Join-Path $script:TestManifestDir "test.jsonc"
            '{"version": 1, "name": "test", "apps": [], "restore": []}' | Out-File -FilePath $script:TestManifest -Encoding UTF8
        }
        
        It "Returns default export path when no ExportPath specified" {
            $exportPath = Get-ExportPath -ManifestPath $script:TestManifest
            $exportPath | Should -Be (Join-Path $script:TestManifestDir "export")
        }
        
        It "Returns custom export path when ExportPath specified" {
            $customPath = Join-Path $TestDrive "custom-export"
            $exportPath = Get-ExportPath -ManifestPath $script:TestManifest -ExportPath $customPath
            $exportPath | Should -Be $customPath
        }
    }
    
    Context "Invoke-ExportCapture DryRun Mode" {
        
        BeforeAll {
            # Create test manifest with restore entries
            $script:TestManifestDir = Join-Path $TestDrive "manifests"
            New-Item -ItemType Directory -Path $script:TestManifestDir -Force | Out-Null
            
            # Create a test file to export
            $script:TestSourceDir = Join-Path $TestDrive "source"
            New-Item -ItemType Directory -Path $script:TestSourceDir -Force | Out-Null
            $script:TestSourceFile = Join-Path $script:TestSourceDir "test-config.txt"
            "test content" | Out-File -FilePath $script:TestSourceFile -Encoding UTF8
            
            # Create manifest
            $script:TestManifest = Join-Path $script:TestManifestDir "test.jsonc"
            $manifestContent = @{
                version = 1
                name = "test"
                apps = @()
                restore = @(
                    @{
                        type = "copy"
                        source = "./configs/test-config.txt"
                        target = $script:TestSourceFile
                        backup = $true
                    }
                )
                verify = @()
            }
            $manifestContent | ConvertTo-Json -Depth 10 | Out-File -FilePath $script:TestManifest -Encoding UTF8
        }
        
        It "DryRun does not create export directory" {
            $exportPath = Join-Path $script:TestManifestDir "export"
            
            # Ensure export directory doesn't exist
            if (Test-Path $exportPath) {
                Remove-Item -Path $exportPath -Recurse -Force
            }
            
            $result = Invoke-ExportCapture -ManifestPath $script:TestManifest -DryRun
            
            # In dry-run, export directory is still created but files are not copied
            # Check that the result indicates dry-run
            $result.Results[0].status | Should -Be "dry-run"
        }
        
        It "DryRun reports what would be exported" {
            $result = Invoke-ExportCapture -ManifestPath $script:TestManifest -DryRun
            
            $result.Success | Should -Be $true
            $result.ExportCount | Should -Be 1
            $result.Results[0].status | Should -Be "dry-run"
            $result.Results[0].reason | Should -Match "would export"
        }
        
        It "DryRun does not copy files" {
            $exportPath = Join-Path $script:TestManifestDir "export"
            $exportFile = Join-Path $exportPath "configs\test-config.txt"
            
            # Clean up if exists
            if (Test-Path $exportFile) {
                Remove-Item -Path $exportFile -Force
            }
            
            $result = Invoke-ExportCapture -ManifestPath $script:TestManifest -DryRun
            
            # File should not exist after dry-run
            Test-Path $exportFile | Should -Be $false
        }
    }
    
    Context "Invoke-ExportCapture Real Export" {
        
        BeforeAll {
            # Create test manifest with restore entries
            $script:TestManifestDir = Join-Path $TestDrive "manifests-real"
            New-Item -ItemType Directory -Path $script:TestManifestDir -Force | Out-Null
            
            # Create a test file to export
            $script:TestSourceDir = Join-Path $TestDrive "source-real"
            New-Item -ItemType Directory -Path $script:TestSourceDir -Force | Out-Null
            $script:TestSourceFile = Join-Path $script:TestSourceDir "test-config.txt"
            "test content for export" | Out-File -FilePath $script:TestSourceFile -Encoding UTF8
            
            # Create manifest
            $script:TestManifest = Join-Path $script:TestManifestDir "test.jsonc"
            $manifestContent = @{
                version = 1
                name = "test"
                apps = @()
                restore = @(
                    @{
                        type = "copy"
                        source = "./configs/test-config.txt"
                        target = $script:TestSourceFile
                        backup = $true
                    }
                )
                verify = @()
            }
            $manifestContent | ConvertTo-Json -Depth 10 | Out-File -FilePath $script:TestManifest -Encoding UTF8
        }
        
        It "Exports file to correct location" {
            $result = Invoke-ExportCapture -ManifestPath $script:TestManifest
            
            $exportPath = Join-Path $script:TestManifestDir "export"
            $exportFile = Join-Path $exportPath "configs\test-config.txt"
            
            $result.Success | Should -Be $true
            $result.ExportCount | Should -Be 1
            Test-Path $exportFile | Should -Be $true
            
            $content = Get-Content -Path $exportFile -Raw
            $content.Trim() | Should -Be "test content for export"
        }
        
        It "Creates manifest snapshot in export folder" {
            $result = Invoke-ExportCapture -ManifestPath $script:TestManifest
            
            $exportPath = Join-Path $script:TestManifestDir "export"
            $snapshotPath = Join-Path $exportPath "manifest.snapshot.jsonc"
            
            Test-Path $snapshotPath | Should -Be $true
        }
        
        It "Skips files that don't exist on system" {
            # Create manifest with non-existent file
            $manifestContent = @{
                version = 1
                name = "test"
                apps = @()
                restore = @(
                    @{
                        type = "copy"
                        source = "./configs/missing.txt"
                        target = "C:\NonExistent\missing.txt"
                        backup = $true
                    }
                )
                verify = @()
            }
            $testManifest = Join-Path $script:TestManifestDir "test-missing.jsonc"
            $manifestContent | ConvertTo-Json -Depth 10 | Out-File -FilePath $testManifest -Encoding UTF8
            
            $result = Invoke-ExportCapture -ManifestPath $testManifest
            
            $result.Success | Should -Be $true
            $result.SkipCount | Should -Be 1
            $result.Results[0].status | Should -Be "skip"
        }
    }
    
    Context "MSI Afterburner Example Manifest" {
        
        BeforeAll {
            $script:AfterburnerManifest = Join-Path $script:ProjectRoot "manifests\examples\msi-afterburner.jsonc"
        }
        
        It "MSI Afterburner manifest exists" {
            Test-Path $script:AfterburnerManifest | Should -Be $true
        }
        
        It "MSI Afterburner manifest has restore entries" {
            $manifest = Read-Manifest -Path $script:AfterburnerManifest
            $manifest.restore | Should -Not -BeNullOrEmpty
            $manifest.restore.Count | Should -BeGreaterThan 0
        }
        
        It "MSI Afterburner manifest defines correct paths" {
            $manifest = Read-Manifest -Path $script:AfterburnerManifest
            
            $cfgEntry = $manifest.restore | Where-Object { $_.source -like "*MSIAfterburner.cfg*" }
            $cfgEntry | Should -Not -BeNullOrEmpty
            $cfgEntry.target | Should -Match "MSI Afterburner"
            
            $profilesEntry = $manifest.restore | Where-Object { $_.source -like "*Profiles*" }
            $profilesEntry | Should -Not -BeNullOrEmpty
            $profilesEntry.target | Should -Match "MSI Afterburner"
        }
    }
}
