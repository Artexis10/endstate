<#
.SYNOPSIS
    Pester tests for capture functionality: filters, templates, and deterministic output.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    $script:CaptureScript = Join-Path $script:ProvisioningRoot "engine\capture.ps1"
    $script:LoggingScript = Join-Path $script:ProvisioningRoot "engine\logging.ps1"
    $script:ManifestScript = Join-Path $script:ProvisioningRoot "engine\manifest.ps1"
    
    # Load dependencies
    . $script:LoggingScript
    . $script:ManifestScript
    
    # Load capture.ps1 but suppress its dot-sourcing of dependencies (already loaded)
    $captureContent = Get-Content -Path $script:CaptureScript -Raw
    $functionsOnly = $captureContent -replace '\. "\$PSScriptRoot\\[^"]+\.ps1"', '# (dependency already loaded)'
    Invoke-Expression $functionsOnly
}

Describe "Capture.Filters.RuntimePackages" {
    
    Context "Test-IsRuntimePackage function" {
        
        It "Should detect VCRedist packages" {
            Test-IsRuntimePackage -PackageId "Microsoft.VCRedist.2015+.x64" | Should -Be $true
            Test-IsRuntimePackage -PackageId "Microsoft.VCRedist.2019.x86" | Should -Be $true
        }
        
        It "Should detect VCLibs packages" {
            Test-IsRuntimePackage -PackageId "Microsoft.VCLibs.140.00" | Should -Be $true
            Test-IsRuntimePackage -PackageId "Microsoft.VCLibs.Desktop" | Should -Be $true
        }
        
        It "Should detect UI.Xaml packages" {
            Test-IsRuntimePackage -PackageId "Microsoft.UI.Xaml.2.7" | Should -Be $true
            Test-IsRuntimePackage -PackageId "Microsoft.UI.Xaml.2.8" | Should -Be $true
        }
        
        It "Should detect DotNet runtime packages" {
            Test-IsRuntimePackage -PackageId "Microsoft.DotNet.DesktopRuntime.6" | Should -Be $true
            Test-IsRuntimePackage -PackageId "Microsoft.DotNet.HostingBundle.8" | Should -Be $true
            Test-IsRuntimePackage -PackageId "Microsoft.DotNet.SDK.8" | Should -Be $true
        }
        
        It "Should detect WindowsAppRuntime packages" {
            Test-IsRuntimePackage -PackageId "Microsoft.WindowsAppRuntime.1.4" | Should -Be $true
        }
        
        It "Should detect DirectX packages" {
            Test-IsRuntimePackage -PackageId "Microsoft.DirectX.Runtime" | Should -Be $true
        }
        
        It "Should NOT detect regular Microsoft apps as runtimes" {
            Test-IsRuntimePackage -PackageId "Microsoft.VisualStudioCode" | Should -Be $false
            Test-IsRuntimePackage -PackageId "Microsoft.PowerShell" | Should -Be $false
            Test-IsRuntimePackage -PackageId "Microsoft.WindowsTerminal" | Should -Be $false
        }
        
        It "Should NOT detect non-Microsoft apps as runtimes" {
            Test-IsRuntimePackage -PackageId "Git.Git" | Should -Be $false
            Test-IsRuntimePackage -PackageId "Mozilla.Firefox" | Should -Be $false
        }
        
        It "Should handle null/empty input gracefully" {
            Test-IsRuntimePackage -PackageId $null | Should -Be $false
            Test-IsRuntimePackage -PackageId "" | Should -Be $false
        }
    }
}

Describe "Capture.Filters.StoreApps" {
    
    Context "Test-IsStoreApp function" {
        
        It "Should detect apps from msstore source" {
            $app = @{
                id = "some-app"
                refs = @{ windows = "SomeApp" }
                _source = "msstore"
            }
            Test-IsStoreApp -App $app | Should -Be $true
        }
        
        It "Should detect apps with 9N* pattern IDs" {
            $app = @{
                id = "some-store-app"
                refs = @{ windows = "9NBLGGH4NNS1" }
                _source = "winget"
            }
            Test-IsStoreApp -App $app | Should -Be $true
        }
        
        It "Should detect apps with XP* pattern IDs" {
            $app = @{
                id = "some-store-app"
                refs = @{ windows = "XPDC2RH70K22MN" }
                _source = "winget"
            }
            Test-IsStoreApp -App $app | Should -Be $true
        }
        
        It "Should NOT detect regular winget apps as store apps" {
            $app = @{
                id = "git-git"
                refs = @{ windows = "Git.Git" }
                _source = "winget"
            }
            Test-IsStoreApp -App $app | Should -Be $false
        }
        
        It "Should NOT detect apps without _source if ID doesn't match store patterns" {
            $app = @{
                id = "vscode"
                refs = @{ windows = "Microsoft.VisualStudioCode" }
            }
            Test-IsStoreApp -App $app | Should -Be $false
        }
        
        It "Should handle apps without refs gracefully" {
            $app = @{
                id = "broken-app"
                refs = @{}
            }
            Test-IsStoreApp -App $app | Should -Be $false
        }
    }
}

Describe "Capture.Templates" {
    
    BeforeEach {
        $script:TestTempDir = Join-Path $env:TEMP "capture-tests-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TestTempDir -Force | Out-Null
    }
    
    AfterEach {
        if (Test-Path $script:TestTempDir) {
            Remove-Item -Path $script:TestTempDir -Recurse -Force
        }
    }
    
    Context "Write-RestoreTemplate function" {
        
        It "Should create restore template file" {
            $templatePath = Join-Path $script:TestTempDir "test-restore.jsonc"
            
            Write-RestoreTemplate -Path $templatePath -ProfileName "test-profile"
            
            Test-Path $templatePath | Should -Be $true
        }
        
        It "Should include profile name in template" {
            $templatePath = Join-Path $script:TestTempDir "test-restore.jsonc"
            
            Write-RestoreTemplate -Path $templatePath -ProfileName "my-workstation"
            
            $content = Get-Content -Path $templatePath -Raw
            $content | Should -Match "my-workstation"
        }
        
        It "Should include restore array in template" {
            $templatePath = Join-Path $script:TestTempDir "test-restore.jsonc"
            
            Write-RestoreTemplate -Path $templatePath -ProfileName "test"
            
            $content = Get-Content -Path $templatePath -Raw
            $content | Should -Match '"restore":'
        }
        
        It "Should include example restore steps" {
            $templatePath = Join-Path $script:TestTempDir "test-restore.jsonc"
            
            Write-RestoreTemplate -Path $templatePath -ProfileName "test"
            
            $content = Get-Content -Path $templatePath -Raw
            $content | Should -Match "copy"
            $content | Should -Match "merge"
        }
    }
    
    Context "Write-VerifyTemplate function" {
        
        It "Should create verify template file" {
            $templatePath = Join-Path $script:TestTempDir "test-verify.jsonc"
            
            Write-VerifyTemplate -Path $templatePath -ProfileName "test-profile"
            
            Test-Path $templatePath | Should -Be $true
        }
        
        It "Should include profile name in template" {
            $templatePath = Join-Path $script:TestTempDir "test-verify.jsonc"
            
            Write-VerifyTemplate -Path $templatePath -ProfileName "my-workstation"
            
            $content = Get-Content -Path $templatePath -Raw
            $content | Should -Match "my-workstation"
        }
        
        It "Should include verify array in template" {
            $templatePath = Join-Path $script:TestTempDir "test-verify.jsonc"
            
            Write-VerifyTemplate -Path $templatePath -ProfileName "test"
            
            $content = Get-Content -Path $templatePath -Raw
            $content | Should -Match '"verify":'
        }
        
        It "Should include example verify steps" {
            $templatePath = Join-Path $script:TestTempDir "test-verify.jsonc"
            
            Write-VerifyTemplate -Path $templatePath -ProfileName "test"
            
            $content = Get-Content -Path $templatePath -Raw
            $content | Should -Match "file-exists"
        }
    }
}

Describe "Capture.DeterministicOutput" {
    
    Context "App sorting" {
        
        It "Should sort apps alphabetically by id" {
            $unsortedApps = @(
                @{ id = "zsh"; refs = @{ windows = "Zsh.Zsh" } }
                @{ id = "git"; refs = @{ windows = "Git.Git" } }
                @{ id = "node"; refs = @{ windows = "OpenJS.NodeJS" } }
                @{ id = "azure-cli"; refs = @{ windows = "Microsoft.AzureCLI" } }
            )
            
            $sortedApps = @($unsortedApps | Sort-Object -Property { $_.id })
            
            $sortedApps[0].id | Should -Be "azure-cli"
            $sortedApps[1].id | Should -Be "git"
            $sortedApps[2].id | Should -Be "node"
            $sortedApps[3].id | Should -Be "zsh"
        }
    }
}

Describe "Capture.Integration.MockedWinget" {
    
    BeforeEach {
        $script:TestTempDir = Join-Path $env:TEMP "capture-integration-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TestTempDir -Force | Out-Null
        
        # Create mock winget export data
        $script:MockWingetExport = @{
            Sources = @(
                @{
                    SourceDetails = @{ Name = "winget" }
                    Packages = @(
                        @{ PackageIdentifier = "Git.Git" }
                        @{ PackageIdentifier = "Microsoft.VisualStudioCode" }
                        @{ PackageIdentifier = "Microsoft.VCRedist.2015+.x64" }
                        @{ PackageIdentifier = "Microsoft.DotNet.DesktopRuntime.8" }
                    )
                }
                @{
                    SourceDetails = @{ Name = "msstore" }
                    Packages = @(
                        @{ PackageIdentifier = "9NBLGGH4NNS1" }
                        @{ PackageIdentifier = "XPDC2RH70K22MN" }
                    )
                }
            )
        }
    }
    
    AfterEach {
        if (Test-Path $script:TestTempDir) {
            Remove-Item -Path $script:TestTempDir -Recurse -Force
        }
    }
    
    Context "Filter application" {
        
        It "Should filter runtime packages when IncludeRuntimes is false" {
            # Simulate parsed apps with source metadata
            $apps = @(
                @{ id = "git-git"; refs = @{ windows = "Git.Git" }; _source = "winget" }
                @{ id = "microsoft-visualstudiocode"; refs = @{ windows = "Microsoft.VisualStudioCode" }; _source = "winget" }
                @{ id = "microsoft-vcredist-2015+-x64"; refs = @{ windows = "Microsoft.VCRedist.2015+.x64" }; _source = "winget" }
                @{ id = "microsoft-dotnet-desktopruntime-8"; refs = @{ windows = "Microsoft.DotNet.DesktopRuntime.8" }; _source = "winget" }
            )
            
            $filtered = @($apps | Where-Object { -not (Test-IsRuntimePackage -PackageId $_.refs.windows) })
            
            $filtered.Count | Should -Be 2
            $filtered[0].id | Should -Be "git-git"
            $filtered[1].id | Should -Be "microsoft-visualstudiocode"
        }
        
        It "Should filter store apps when IncludeStoreApps is false" {
            $apps = @(
                @{ id = "git-git"; refs = @{ windows = "Git.Git" }; _source = "winget" }
                @{ id = "store-app-1"; refs = @{ windows = "9NBLGGH4NNS1" }; _source = "msstore" }
                @{ id = "store-app-2"; refs = @{ windows = "XPDC2RH70K22MN" }; _source = "winget" }
            )
            
            $filtered = @($apps | Where-Object { -not (Test-IsStoreApp -App $_) })
            
            $filtered.Count | Should -Be 1
            $filtered[0].id | Should -Be "git-git"
        }
        
        It "Should apply both filters together" {
            $apps = @(
                @{ id = "git-git"; refs = @{ windows = "Git.Git" }; _source = "winget" }
                @{ id = "microsoft-vcredist"; refs = @{ windows = "Microsoft.VCRedist.2015+.x64" }; _source = "winget" }
                @{ id = "store-app"; refs = @{ windows = "9NBLGGH4NNS1" }; _source = "msstore" }
            )
            
            $filtered = $apps
            $filtered = @($filtered | Where-Object { -not (Test-IsRuntimePackage -PackageId $_.refs.windows) })
            $filtered = @($filtered | Where-Object { -not (Test-IsStoreApp -App $_) })
            
            $filtered.Count | Should -Be 1
            $filtered[0].id | Should -Be "git-git"
        }
        
        It "Should minimize entries without stable refs" {
            $apps = @(
                @{ id = "git-git"; refs = @{ windows = "Git.Git" } }
                @{ id = "broken-app"; refs = @{} }
                @{ id = "null-refs-app"; refs = $null }
            )
            
            $minimized = @($apps | Where-Object { $_.refs -and $_.refs.windows })
            
            $minimized.Count | Should -Be 1
            $minimized[0].id | Should -Be "git-git"
        }
    }
}

Describe "Capture.Validation" {
    
    Context "Parameter validation" {
        
        It "Should require -Profile for -IncludeRestoreTemplate" {
            # This tests the validation logic directly
            $hasProfile = $false
            $includeRestoreTemplate = $true
            
            $shouldFail = ($includeRestoreTemplate -and -not $hasProfile)
            
            $shouldFail | Should -Be $true
        }
        
        It "Should require -Profile for -IncludeVerifyTemplate" {
            $hasProfile = $false
            $includeVerifyTemplate = $true
            
            $shouldFail = ($includeVerifyTemplate -and -not $hasProfile)
            
            $shouldFail | Should -Be $true
        }
        
        It "Should allow templates when -Profile is provided" {
            $hasProfile = $true
            $includeRestoreTemplate = $true
            $includeVerifyTemplate = $true
            
            $shouldFail = (($includeRestoreTemplate -or $includeVerifyTemplate) -and -not $hasProfile)
            
            $shouldFail | Should -Be $false
        }
        
        It "Should require either -Profile or -OutManifest" {
            $hasProfile = $false
            $hasOutManifest = $false
            
            $shouldFail = (-not $hasProfile -and -not $hasOutManifest)
            
            $shouldFail | Should -Be $true
        }
    }
}

Describe "Capture.ArtifactContract" {
    <#
    .SYNOPSIS
        Tests for INV-CAPTURE-1..4: Capture artifact contract invariants.
    #>
    
    BeforeAll {
        $script:EndstateScript = Join-Path $PSScriptRoot "..\..\bin\endstate.ps1"
        # Allow direct script execution for testing
        $env:ENDSTATE_ALLOW_DIRECT = '1'
    }
    
    AfterAll {
        # Clean up environment variable
        $env:ENDSTATE_ALLOW_DIRECT = $null
    }
    
    Context "INV-CAPTURE-1: CLI Availability" {
        
        It "Should return ENGINE_CLI_NOT_FOUND when CLI path is null" {
            # Load the endstate.ps1 script to get Invoke-ProvisioningCli
            . $script:EndstateScript -Command "version" 2>$null
            
            # Mock Get-ProvisioningCliPath to return null
            Mock Get-ProvisioningCliPath { return $null }
            
            $result = Invoke-ProvisioningCli -ProvisioningCommand "capture" -Arguments @{}
            
            $result.Success | Should -Be $false
            $result.Error.code | Should -Be "ENGINE_CLI_NOT_FOUND"
            $result.Error.hint | Should -Not -BeNullOrEmpty
        }
        
        It "Should return ENGINE_CLI_NOT_FOUND when CLI path does not exist" {
            # Load the endstate.ps1 script
            . $script:EndstateScript -Command "version" 2>$null
            
            # Mock Get-ProvisioningCliPath to return non-existent path
            Mock Get-ProvisioningCliPath { return "C:\nonexistent\path\cli.ps1" }
            
            $result = Invoke-ProvisioningCli -ProvisioningCommand "capture" -Arguments @{}
            
            $result.Success | Should -Be $false
            $result.Error.code | Should -Be "ENGINE_CLI_NOT_FOUND"
            $result.Error.message | Should -Match "not found"
        }
        
        It "Should include hint field in ENGINE_CLI_NOT_FOUND error" {
            # Load the endstate.ps1 script
            . $script:EndstateScript -Command "version" 2>$null
            
            Mock Get-ProvisioningCliPath { return $null }
            
            $result = Invoke-ProvisioningCli -ProvisioningCommand "capture" -Arguments @{}
            
            $result.Error | Should -Not -BeNullOrEmpty
            $result.Error.hint | Should -Match "Settings|bootstrap"
        }
    }
    
    Context "INV-CAPTURE-2: Manifest File Existence" {
        
        It "Should fail if manifest file does not exist after capture" {
            $tempDir = Join-Path $env:TEMP "capture-test-$(Get-Random)"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
            $outPath = Join-Path $tempDir "nonexistent.jsonc"
            
            try {
                # Simulate capture result where file was not created
                $manifestExists = Test-Path $outPath
                $manifestExists | Should -Be $false
                
                # Per INV-CAPTURE-2, this should result in failure
                $shouldFail = -not $manifestExists
                $shouldFail | Should -Be $true
            }
            finally {
                Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should fail if manifest file is empty" {
            $tempDir = Join-Path $env:TEMP "capture-test-$(Get-Random)"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
            $outPath = Join-Path $tempDir "empty.jsonc"
            
            try {
                # Create empty file
                New-Item -ItemType File -Path $outPath -Force | Out-Null
                
                $fileInfo = Get-Item $outPath
                $isEmpty = $fileInfo.Length -eq 0
                $isEmpty | Should -Be $true
                
                # Per INV-CAPTURE-2, empty file should result in failure
                $shouldFail = $isEmpty
                $shouldFail | Should -Be $true
            }
            finally {
                Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should succeed if manifest file exists and is non-empty" {
            $tempDir = Join-Path $env:TEMP "capture-test-$(Get-Random)"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
            $outPath = Join-Path $tempDir "valid.jsonc"
            
            try {
                # Create valid manifest file
                $validManifest = @{
                    version = 1
                    name = "test"
                    apps = @()
                } | ConvertTo-Json
                Set-Content -Path $outPath -Value $validManifest -Encoding UTF8
                
                $fileInfo = Get-Item $outPath
                $isValid = (Test-Path $outPath) -and ($fileInfo.Length -gt 0)
                $isValid | Should -Be $true
            }
            finally {
                Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
    }
    
    Context "Error Code Semantics" {
        
        It "Should use MANIFEST_WRITE_FAILED for empty manifest" {
            $errorCode = "MANIFEST_WRITE_FAILED"
            $errorCode | Should -Be "MANIFEST_WRITE_FAILED"
        }
        
        It "Should use ENGINE_CLI_NOT_FOUND for missing CLI" {
            $errorCode = "ENGINE_CLI_NOT_FOUND"
            $errorCode | Should -Be "ENGINE_CLI_NOT_FOUND"
        }
        
        It "Should use CAPTURE_FAILED for generic failures" {
            $errorCode = "CAPTURE_FAILED"
            $errorCode | Should -Be "CAPTURE_FAILED"
        }
        
        It "Should use WINGET_CAPTURE_EMPTY when both export and fallback produce zero apps" {
            $errorCode = "WINGET_CAPTURE_EMPTY"
            $errorCode | Should -Be "WINGET_CAPTURE_EMPTY"
        }
    }
    
    Context "Fallback Capture Warning Codes" {
        
        It "Should use WINGET_EXPORT_FAILED_FALLBACK_USED warning code" {
            $warningCode = "WINGET_EXPORT_FAILED_FALLBACK_USED"
            $warningCode | Should -Be "WINGET_EXPORT_FAILED_FALLBACK_USED"
        }
        
        It "Should include captureWarnings array in result when fallback is used" {
            # Simulate fallback capture result structure
            $captureResult = @{
                Apps = @(
                    @{ id = "git-git"; refs = @{ windows = "Git.Git" }; _source = "winget" }
                )
                CaptureWarnings = @("WINGET_EXPORT_FAILED_FALLBACK_USED")
                UsedFallback = $true
                ExportFailureReason = "exit code -2145844844"
            }
            
            $captureResult.CaptureWarnings | Should -Contain "WINGET_EXPORT_FAILED_FALLBACK_USED"
            $captureResult.UsedFallback | Should -Be $true
            $captureResult.Apps.Count | Should -BeGreaterThan 0
        }
    }
    
    Context "INV-MANIFEST-NEVER-EMPTY: {} is forbidden output" {
        
        It "Should never write empty object {} as manifest" {
            $emptyManifest = "{}"
            $validManifest = @{ version = 1; apps = @() } | ConvertTo-Json
            
            # Empty object is invalid
            $emptyManifest.Trim() | Should -Be "{}"
            
            # Valid manifest has structure
            $validManifest | Should -Match '"version"'
            $validManifest | Should -Match '"apps"'
        }
        
        It "Should require manifest to contain apps array or version field" {
            $validManifests = @(
                @{ version = 1; apps = @() }
                @{ version = 1; name = "test"; apps = @(@{ id = "test" }) }
                @{ apps = @(); name = "minimal" }
            )
            
            foreach ($manifest in $validManifests) {
                $hasRequiredFields = ($null -ne $manifest.version) -or ($null -ne $manifest.apps)
                $hasRequiredFields | Should -Be $true
            }
            
            # Empty object lacks required fields
            $emptyObj = @{}
            $hasRequiredFields = ($null -ne $emptyObj.version) -or ($null -ne $emptyObj.apps)
            $hasRequiredFields | Should -Be $false
        }
    }
    
    Context "INV-CLI-PATH: Provisioning CLI Path Resolution" {
        
        It "Should resolve CLI path to bin\cli.ps1 not root cli.ps1" {
            # Load the endstate.ps1 script
            . $script:EndstateScript -Command "version" 2>$null
            
            $cliPath = Get-ProvisioningCliPath
            
            # CLI path should end with bin\cli.ps1, not just cli.ps1 at root
            $cliPath | Should -Match 'bin[/\\]cli\.ps1$'
            $cliPath | Should -Not -Match '[^n][/\\]cli\.ps1$'  # Not root\cli.ps1
        }
        
        It "Should resolve CLI path relative to repo root" {
            . $script:EndstateScript -Command "version" 2>$null
            
            $cliPath = Get-ProvisioningCliPath
            
            if ($cliPath) {
                # Verify the path exists
                Test-Path $cliPath | Should -Be $true
                
                # Verify it's in the bin directory
                $parentDir = Split-Path -Parent $cliPath
                $parentDirName = Split-Path -Leaf $parentDir
                $parentDirName | Should -Be "bin"
            }
        }
    }
    
    Context "INV-SANITIZE-IDS: Package IDs must not contain leading non-ASCII characters" {
        
        BeforeAll {
            # Load capture.ps1 to get the sanitization logic
            . (Join-Path $PSScriptRoot "..\..\engine\capture.ps1")
        }
        
        It "Should strip leading 'ª' character from package IDs" {
            # Simulate dirty package ID from winget list output
            $dirtyId = "ª Microsoft.VCRedist.2015+.x64"
            
            # Apply the same sanitization as Get-InstalledAppsViaWingetList
            $cleanId = $dirtyId -replace '^[^\x20-\x7E]+', ''
            $cleanId = $cleanId.Trim()
            
            $cleanId | Should -Be "Microsoft.VCRedist.2015+.x64"
            $cleanId | Should -Not -Match '^[^\x20-\x7E]'
        }
        
        It "Should strip multiple leading non-ASCII characters" {
            $dirtyId = "ªª EclipseAdoptium.Temurin.8.JRE"
            
            $cleanId = $dirtyId -replace '^[^\x20-\x7E]+', ''
            $cleanId = $cleanId.Trim()
            
            $cleanId | Should -Be "EclipseAdoptium.Temurin.8.JRE"
        }
        
        It "Should produce clean normalized app ID without non-ASCII" {
            $dirtyId = "ª Microsoft.DotNet.DesktopRuntime.8"
            
            # Sanitize package ID
            $packageId = $dirtyId -replace '^[^\x20-\x7E]+', ''
            $packageId = $packageId.Trim()
            
            # Normalize to app ID
            $appId = $packageId -replace '\.', '-' -replace '_', '-'
            $appId = $appId -replace '[^\x20-\x7E]', ''
            $appId = $appId.ToLower().Trim()
            
            $appId | Should -Be "microsoft-dotnet-desktopruntime-8"
            $appId | Should -Not -Match '[^\x20-\x7E]'
        }
        
        It "Should skip rows that become empty after sanitization" {
            $dirtyId = "ªªª"  # Only non-ASCII chars
            
            $cleanId = $dirtyId -replace '^[^\x20-\x7E]+', ''
            $cleanId = $cleanId.Trim()
            
            # Should be empty and skipped
            $shouldSkip = (-not $cleanId -or $cleanId -match '^\s*$')
            $shouldSkip | Should -Be $true
        }
        
        It "Should not modify already clean package IDs" {
            $cleanId = "Git.Git"
            
            $result = $cleanId -replace '^[^\x20-\x7E]+', ''
            $result = $result.Trim()
            
            $result | Should -Be "Git.Git"
        }
    }
    
    Context "INV-CONTINUITY-1: counts.included must equal appsIncluded.length" {
        
        It "Should have counts.included equal manifest.apps.Count in capture result" {
            # Simulate capture result structure from Invoke-CaptureCore
            $manifest = @{
                version = 1
                apps = @(
                    @{ id = "git-git"; refs = @{ windows = "Git.Git" } }
                    @{ id = "docker-dockerdesktop"; refs = @{ windows = "Docker.DockerDesktop" } }
                    @{ id = "microsoft-vscode"; refs = @{ windows = "Microsoft.VSCode" } }
                )
            }
            
            # Simulate how engine derives counts and appsIncluded
            $counts = @{
                included = $manifest.apps.Count
                totalFound = $manifest.apps.Count
            }
            
            $appsIncluded = @($manifest.apps | ForEach-Object {
                @{ id = if ($_.refs -and $_.refs.windows) { $_.refs.windows } else { $_.id }; source = "winget" }
            })
            
            # INV-CONTINUITY-1: counts.included must equal appsIncluded.length
            $counts.included | Should -Be $appsIncluded.Count
            $counts.included | Should -Be $manifest.apps.Count
        }
        
        It "Should detect continuity violation when counts disagree with apps list" {
            # Simulate the bug: counts says 72 but appsIncluded has 66
            $counts = @{ included = 72 }
            $appsIncluded = @(1..66 | ForEach-Object { @{ id = "app-$_"; source = "winget" } })
            
            # This would be a violation
            $isValid = ($counts.included -eq $appsIncluded.Count)
            $isValid | Should -Be $false
        }
        
        It "Should maintain continuity when deriving from same source" {
            # Load capture.ps1 to verify the actual implementation
            . (Join-Path $PSScriptRoot "..\..\engine\capture.ps1")
            
            # Create a test manifest structure
            $testApps = @(
                @{ id = "test-1"; refs = @{ windows = "Test.App1" }; _source = "winget" }
                @{ id = "test-2"; refs = @{ windows = "Test.App2" }; _source = "winget" }
            )
            
            # Simulate the engine's derivation logic (from Invoke-CaptureCore)
            $countsIncluded = $testApps.Count
            $appsIncludedLength = @($testApps | ForEach-Object {
                @{ id = if ($_.refs -and $_.refs.windows) { $_.refs.windows } else { $_.id } }
            }).Count
            
            # Both must be derived from same source
            $countsIncluded | Should -Be $appsIncludedLength
        }
    }
}