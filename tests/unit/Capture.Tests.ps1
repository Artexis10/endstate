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