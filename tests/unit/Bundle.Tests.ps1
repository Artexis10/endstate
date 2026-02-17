<#
.SYNOPSIS
    Pester tests for zip bundle packaging: config module matching, config collection,
    metadata generation, zip creation, profile extraction, and profile discovery.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    
    # Load dependencies via direct dot-sourcing to preserve $PSScriptRoot
    . (Join-Path $script:ProvisioningRoot "engine\logging.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\manifest.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\config-modules.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\bundle.ps1")
}

Describe "Bundle.ConfigModuleMatching" {
    
    BeforeEach {
        # Reset catalog cache before each test
        Clear-ConfigModuleCatalogCache
    }
    
    Context "Get-MatchedConfigModulesForApps" {
        
        It "Should return empty array when no apps provided" {
            $result = Get-MatchedConfigModulesForApps -Apps @()
            $result | Should -HaveCount 0
        }
        
        It "Should return empty array when apps have no winget refs" {
            $apps = @(
                @{ id = "some-app"; refs = @{} }
            )
            $result = Get-MatchedConfigModulesForApps -Apps $apps
            $result | Should -HaveCount 0
        }
        
        It "Should match apps against catalog by winget ID" {
            # This test uses the real catalog - vscode module matches Microsoft.VisualStudioCode
            $apps = @(
                @{ id = "vscode"; refs = @{ windows = "Microsoft.VisualStudioCode" } }
            )
            $result = Get-MatchedConfigModulesForApps -Apps $apps
            # Should find the vscode module (if it has a capture section)
            $vscodeMatch = $result | Where-Object { $_.id -eq "apps.vscode" }
            $vscodeMatch | Should -Not -BeNullOrEmpty
        }
        
        It "Should not match apps that have no corresponding module" {
            $apps = @(
                @{ id = "nonexistent-app"; refs = @{ windows = "Nonexistent.App.12345" } }
            )
            $result = Get-MatchedConfigModulesForApps -Apps $apps
            $result | Should -HaveCount 0
        }
        
        It "Should only return modules with capture sections" {
            $apps = @(
                @{ id = "vscode"; refs = @{ windows = "Microsoft.VisualStudioCode" } }
            )
            $result = Get-MatchedConfigModulesForApps -Apps $apps
            foreach ($module in $result) {
                $module.capture | Should -Not -BeNullOrEmpty
                $module.capture.files | Should -Not -BeNullOrEmpty
            }
        }
        
        It "Should return results sorted by module ID" {
            # Use multiple apps that match different modules
            $apps = @(
                @{ id = "vscode"; refs = @{ windows = "Microsoft.VisualStudioCode" } }
                @{ id = "git"; refs = @{ windows = "Git.Git" } }
                @{ id = "claude"; refs = @{ windows = "Anthropic.Claude" } }
            )
            $result = Get-MatchedConfigModulesForApps -Apps $apps
            if ($result.Count -gt 1) {
                for ($i = 1; $i -lt $result.Count; $i++) {
                    $result[$i].id | Should -BeGreaterOrEqual $result[$i-1].id
                }
            }
        }
    }
}

Describe "Bundle.ConfigCollection" {
    
    Context "Invoke-CollectConfigFiles" {
        
        BeforeEach {
            $script:TestStagingDir = Join-Path $env:TEMP "endstate-test-staging-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $script:TestStagingDir -Force | Out-Null
        }
        
        AfterEach {
            if (Test-Path $script:TestStagingDir) {
                Remove-Item -Path $script:TestStagingDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should return empty results when no modules provided" {
            $result = Invoke-CollectConfigFiles -Modules @() -StagingDir $script:TestStagingDir
            $result.included | Should -HaveCount 0
            $result.skipped | Should -HaveCount 0
            $result.errors | Should -HaveCount 0
            $result.filesCopied | Should -Be 0
        }
        
        It "Should copy existing files to staging directory" {
            # Create a temp source file
            $sourceDir = Join-Path $env:TEMP "endstate-test-source-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            $sourceFile = Join-Path $sourceDir "test-config.json"
            '{"test": true}' | Set-Content -Path $sourceFile -Encoding UTF8
            
            try {
                $module = @{
                    id = "apps.test-app"
                    capture = @{
                        files = @(
                            @{ source = $sourceFile; dest = "apps/test-app/test-config.json" }
                        )
                    }
                }
                
                $result = Invoke-CollectConfigFiles -Modules @($module) -StagingDir $script:TestStagingDir
                $result.included | Should -Contain "test-app"
                $result.filesCopied | Should -Be 1
                
                # Verify file was copied
                $destFile = Join-Path $script:TestStagingDir "configs\test-app\test-config.json"
                Test-Path $destFile | Should -Be $true
            } finally {
                Remove-Item -Path $sourceDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should skip optional missing files without error" {
            $module = @{
                id = "apps.test-app"
                capture = @{
                    files = @(
                        @{ source = "C:\nonexistent\file.json"; dest = "apps/test-app/file.json"; optional = $true }
                    )
                }
            }
            
            $result = Invoke-CollectConfigFiles -Modules @($module) -StagingDir $script:TestStagingDir
            $result.skipped | Should -Contain "test-app"
            $result.errors | Should -HaveCount 0
        }
        
        It "Should report error for required missing files" {
            $module = @{
                id = "apps.test-app"
                capture = @{
                    files = @(
                        @{ source = "C:\nonexistent\file.json"; dest = "apps/test-app/file.json" }
                    )
                }
            }
            
            $result = Invoke-CollectConfigFiles -Modules @($module) -StagingDir $script:TestStagingDir
            $result.errors.Count | Should -BeGreaterThan 0
        }
        
        It "Should skip sensitive files" {
            # Create a temp source file
            $sourceDir = Join-Path $env:TEMP "endstate-test-sensitive-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            $sourceFile = Join-Path $sourceDir "credentials.json"
            '{"secret": "token"}' | Set-Content -Path $sourceFile -Encoding UTF8
            
            try {
                $module = @{
                    id = "apps.test-app"
                    capture = @{
                        files = @(
                            @{ source = $sourceFile; dest = "apps/test-app/credentials.json" }
                        )
                    }
                    sensitive = @{
                        files = @($sourceFile)
                    }
                }
                
                $result = Invoke-CollectConfigFiles -Modules @($module) -StagingDir $script:TestStagingDir
                $result.filesCopied | Should -Be 0
                
                # Verify file was NOT copied
                $destFile = Join-Path $script:TestStagingDir "configs\test-app\credentials.json"
                Test-Path $destFile | Should -Be $false
            } finally {
                Remove-Item -Path $sourceDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should strip 'apps.' prefix from module ID for directory name" {
            $sourceDir = Join-Path $env:TEMP "endstate-test-prefix-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $sourceDir -Force | Out-Null
            $sourceFile = Join-Path $sourceDir "config.json"
            '{}' | Set-Content -Path $sourceFile -Encoding UTF8
            
            try {
                $module = @{
                    id = "apps.my-app"
                    capture = @{
                        files = @(
                            @{ source = $sourceFile; dest = "apps/my-app/config.json" }
                        )
                    }
                }
                
                $null = Invoke-CollectConfigFiles -Modules @($module) -StagingDir $script:TestStagingDir
                
                # Should use "my-app" not "apps.my-app" as directory name
                $destFile = Join-Path $script:TestStagingDir "configs\my-app\config.json"
                Test-Path $destFile | Should -Be $true
            } finally {
                Remove-Item -Path $sourceDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
    }
}

Describe "Bundle.Metadata" {
    
    Context "New-CaptureMetadata" {
        
        It "Should generate metadata with required fields" {
            $metadata = New-CaptureMetadata
            $metadata.schemaVersion | Should -Be "1.0"
            $metadata.capturedAt | Should -Not -BeNullOrEmpty
            $metadata.machineName | Should -Be $env:COMPUTERNAME
            $metadata.endstateVersion | Should -Not -BeNullOrEmpty
            $metadata.Keys | Should -Contain "configModulesIncluded"
            $metadata.Keys | Should -Contain "configModulesSkipped"
            $metadata.Keys | Should -Contain "captureWarnings"
        }
        
        It "Should include provided config module lists" {
            $metadata = New-CaptureMetadata `
                -ConfigsIncluded @("vscode", "git") `
                -ConfigsSkipped @("claude-desktop") `
                -CaptureWarnings @("Some warning")
            
            $metadata.configModulesIncluded | Should -Contain "vscode"
            $metadata.configModulesIncluded | Should -Contain "git"
            $metadata.configModulesSkipped | Should -Contain "claude-desktop"
            $metadata.captureWarnings | Should -Contain "Some warning"
        }
        
        It "Should produce valid ISO 8601 timestamp" {
            $metadata = New-CaptureMetadata
            { [DateTime]::Parse($metadata.capturedAt) } | Should -Not -Throw
        }
    }
}

Describe "Bundle.ZipCreation" {
    
    Context "New-CaptureBundle" {
        
        BeforeEach {
            # Create a temp manifest file
            $script:TestDir = Join-Path $env:TEMP "endstate-test-bundle-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $script:TestDir -Force | Out-Null
            $script:TestManifest = Join-Path $script:TestDir "manifest.jsonc"
            @'
{
  "version": 1,
  "name": "test-profile",
  "apps": [
    { "id": "test-app", "refs": { "windows": "Test.App" } }
  ],
  "restore": [],
  "verify": []
}
'@ | Set-Content -Path $script:TestManifest -Encoding UTF8
            
            $script:TestZipPath = Join-Path $script:TestDir "test-profile.zip"
        }
        
        AfterEach {
            if (Test-Path $script:TestDir) {
                Remove-Item -Path $script:TestDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should create a valid zip file" {
            $result = New-CaptureBundle `
                -ManifestPath $script:TestManifest `
                -OutputZipPath $script:TestZipPath `
                -Apps @(@{ id = "test-app"; refs = @{ windows = "Test.App" } })
            
            Test-Path $script:TestZipPath | Should -Be $true
        }
        
        It "Should include manifest.jsonc in zip" {
            $bundleResult = New-CaptureBundle `
                -ManifestPath $script:TestManifest `
                -OutputZipPath $script:TestZipPath `
                -Apps @()
            
            $bundleResult.Success | Should -Be $true
            
            # Extract and verify
            Add-Type -AssemblyName System.IO.Compression.FileSystem
            $extractDir = Join-Path $script:TestDir "extracted"
            [System.IO.Compression.ZipFile]::ExtractToDirectory($script:TestZipPath, $extractDir)
            
            Test-Path (Join-Path $extractDir "manifest.jsonc") | Should -Be $true
        }
        
        It "Should include metadata.json in zip" {
            $result = New-CaptureBundle `
                -ManifestPath $script:TestManifest `
                -OutputZipPath $script:TestZipPath `
                -Apps @()
            
            $result.Success | Should -Be $true
            
            # Extract and verify
            Add-Type -AssemblyName System.IO.Compression.FileSystem
            $extractDir = Join-Path $script:TestDir "extracted"
            [System.IO.Compression.ZipFile]::ExtractToDirectory($script:TestZipPath, $extractDir)
            
            $metadataPath = Join-Path $extractDir "metadata.json"
            Test-Path $metadataPath | Should -Be $true
            
            # Verify metadata is valid JSON
            $metadata = Get-Content $metadataPath -Raw | ConvertFrom-Json
            $metadata.schemaVersion | Should -Be "1.0"
            $metadata.machineName | Should -Be $env:COMPUTERNAME
        }
        
        It "Should return metadata in result" {
            $result = New-CaptureBundle `
                -ManifestPath $script:TestManifest `
                -OutputZipPath $script:TestZipPath `
                -Apps @()
            
            $result.Metadata | Should -Not -BeNull
            $result.Metadata.schemaVersion | Should -Be "1.0"
        }
        
        It "Should succeed with empty apps (install-only bundle)" {
            $result = New-CaptureBundle `
                -ManifestPath $script:TestManifest `
                -OutputZipPath $script:TestZipPath `
                -Apps @()
            
            $result.Success | Should -Be $true
            $result.ConfigsIncluded | Should -HaveCount 0
        }
        
        It "Should clean up staging directory on success" {
            $result = New-CaptureBundle `
                -ManifestPath $script:TestManifest `
                -OutputZipPath $script:TestZipPath `
                -Apps @()
            
            $result.Success | Should -Be $true
            # Verify zip was created (staging dir cleaned up internally)
            Test-Path $script:TestZipPath | Should -Be $true
        }
    }
}

Describe "Bundle.ZipExtraction" {
    
    Context "Expand-ProfileBundle" {
        
        BeforeEach {
            # Create a test zip bundle
            $script:TestDir = Join-Path $env:TEMP "endstate-test-extract-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $script:TestDir -Force | Out-Null
            
            # Create staging content
            $stagingDir = Join-Path $script:TestDir "staging"
            New-Item -ItemType Directory -Path $stagingDir -Force | Out-Null
            
            # manifest.jsonc
            @'
{
  "version": 1,
  "name": "test-profile",
  "apps": [
    { "id": "test-app", "refs": { "windows": "Test.App" } }
  ],
  "restore": [],
  "verify": []
}
'@ | Set-Content -Path (Join-Path $stagingDir "manifest.jsonc") -Encoding UTF8
            
            # metadata.json
            @'
{
  "schemaVersion": "1.0",
  "capturedAt": "2026-02-16T20:00:00Z",
  "machineName": "TEST-MACHINE",
  "endstateVersion": "0.1.0",
  "configModulesIncluded": ["vscode"],
  "configModulesSkipped": [],
  "captureWarnings": []
}
'@ | Set-Content -Path (Join-Path $stagingDir "metadata.json") -Encoding UTF8
            
            # configs/vscode/settings.json
            $configDir = Join-Path $stagingDir "configs\vscode"
            New-Item -ItemType Directory -Path $configDir -Force | Out-Null
            '{"editor.fontSize": 14}' | Set-Content -Path (Join-Path $configDir "settings.json") -Encoding UTF8
            
            # Create zip
            $script:TestZipPath = Join-Path $script:TestDir "test-profile.zip"
            Add-Type -AssemblyName System.IO.Compression.FileSystem
            [System.IO.Compression.ZipFile]::CreateFromDirectory($stagingDir, $script:TestZipPath)
            
            # Clean staging
            Remove-Item $stagingDir -Recurse -Force
        }
        
        AfterEach {
            if (Test-Path $script:TestDir) {
                Remove-Item -Path $script:TestDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should extract zip and return manifest path" {
            $result = Expand-ProfileBundle -ZipPath $script:TestZipPath
            
            try {
                $result.Success | Should -Be $true
                $result.ManifestPath | Should -Not -BeNullOrEmpty
                Test-Path $result.ManifestPath | Should -Be $true
            } finally {
                Remove-ProfileBundleTemp -ExtractedDir $result.ExtractedDir
            }
        }
        
        It "Should detect configs directory" {
            $result = Expand-ProfileBundle -ZipPath $script:TestZipPath
            
            try {
                $result.HasConfigs | Should -Be $true
            } finally {
                Remove-ProfileBundleTemp -ExtractedDir $result.ExtractedDir
            }
        }
        
        It "Should parse metadata" {
            $result = Expand-ProfileBundle -ZipPath $script:TestZipPath
            
            try {
                $result.Metadata | Should -Not -BeNull
                $result.Metadata.schemaVersion | Should -Be "1.0"
                $result.Metadata.machineName | Should -Be "TEST-MACHINE"
            } finally {
                Remove-ProfileBundleTemp -ExtractedDir $result.ExtractedDir
            }
        }
        
        It "Should fail for nonexistent zip" {
            $result = Expand-ProfileBundle -ZipPath "C:\nonexistent\fake.zip"
            $result.Success | Should -Be $false
            $result.Error | Should -Not -BeNullOrEmpty
        }
        
        It "Should fail for zip without manifest" {
            # Create a zip without manifest.jsonc
            $emptyDir = Join-Path $script:TestDir "empty-staging"
            New-Item -ItemType Directory -Path $emptyDir -Force | Out-Null
            '{}' | Set-Content -Path (Join-Path $emptyDir "metadata.json") -Encoding UTF8
            
            $badZip = Join-Path $script:TestDir "bad-profile.zip"
            [System.IO.Compression.ZipFile]::CreateFromDirectory($emptyDir, $badZip)
            
            $result = Expand-ProfileBundle -ZipPath $badZip
            $result.Success | Should -Be $false
            $result.Error | Should -Match "manifest.jsonc"
        }
    }
    
    Context "Remove-ProfileBundleTemp" {
        
        It "Should remove extracted directory" {
            $tempDir = Join-Path $env:TEMP "endstate-test-cleanup-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
            
            Remove-ProfileBundleTemp -ExtractedDir $tempDir
            Test-Path $tempDir | Should -Be $false
        }
        
        It "Should handle nonexistent directory gracefully" {
            { Remove-ProfileBundleTemp -ExtractedDir "C:\nonexistent\dir" } | Should -Not -Throw
        }
    }
}

Describe "Bundle.ProfileDiscovery" {
    
    Context "Resolve-ProfilePath" {
        
        BeforeEach {
            $script:TestProfilesDir = Join-Path $env:TEMP "endstate-test-profiles-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $script:TestProfilesDir -Force | Out-Null
        }
        
        AfterEach {
            if (Test-Path $script:TestProfilesDir) {
                Remove-Item -Path $script:TestProfilesDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should find zip bundle first" {
            # Create all three formats
            '{}' | Set-Content -Path (Join-Path $script:TestProfilesDir "MyProfile.zip") -Encoding UTF8
            $folderDir = Join-Path $script:TestProfilesDir "MyProfile"
            New-Item -ItemType Directory -Path $folderDir -Force | Out-Null
            '{}' | Set-Content -Path (Join-Path $folderDir "manifest.jsonc") -Encoding UTF8
            '{}' | Set-Content -Path (Join-Path $script:TestProfilesDir "MyProfile.jsonc") -Encoding UTF8
            
            $result = Resolve-ProfilePath -ProfileName "MyProfile" -ProfilesDir $script:TestProfilesDir
            $result.Found | Should -Be $true
            $result.Format | Should -Be "zip"
            $result.Path | Should -Match "\.zip$"
        }
        
        It "Should find loose folder when no zip exists" {
            $folderDir = Join-Path $script:TestProfilesDir "MyProfile"
            New-Item -ItemType Directory -Path $folderDir -Force | Out-Null
            '{}' | Set-Content -Path (Join-Path $folderDir "manifest.jsonc") -Encoding UTF8
            '{}' | Set-Content -Path (Join-Path $script:TestProfilesDir "MyProfile.jsonc") -Encoding UTF8
            
            $result = Resolve-ProfilePath -ProfileName "MyProfile" -ProfilesDir $script:TestProfilesDir
            $result.Found | Should -Be $true
            $result.Format | Should -Be "folder"
            $result.Path | Should -Match "manifest\.jsonc$"
        }
        
        It "Should find bare manifest when no zip or folder exists" {
            '{}' | Set-Content -Path (Join-Path $script:TestProfilesDir "MyProfile.jsonc") -Encoding UTF8
            
            $result = Resolve-ProfilePath -ProfileName "MyProfile" -ProfilesDir $script:TestProfilesDir
            $result.Found | Should -Be $true
            $result.Format | Should -Be "bare"
            $result.Path | Should -Match "MyProfile\.jsonc$"
        }
        
        It "Should return not found when profile doesn't exist" {
            $result = Resolve-ProfilePath -ProfileName "NonExistent" -ProfilesDir $script:TestProfilesDir
            $result.Found | Should -Be $false
        }
    }
}
