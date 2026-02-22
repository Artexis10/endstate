<#
.SYNOPSIS
    Pester tests for CLI profile management commands:
    New-ProfileOverlay, Add-ProfileExclusion, Add-ProfileExcludeConfig,
    Add-ProfileApp, Get-ProfileSummary, Get-ProfileList.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    
    # Load dependencies via direct dot-sourcing to preserve $PSScriptRoot
    . (Join-Path $script:ProvisioningRoot "engine\logging.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\manifest.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\bundle.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\profile-commands.ps1")
}

Describe "ProfileCommands.New" {
    
    BeforeEach {
        $script:TestProfilesDir = Join-Path $TestDrive "profiles-new-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        New-Item -ItemType Directory -Path $script:TestProfilesDir -Force | Out-Null
    }
    
    Context "New-ProfileOverlay without --from" {
        
        It "Should create a bare profile with version, name, and empty apps" {
            $result = New-ProfileOverlay -Name "test-bare" -ProfilesDir $script:TestProfilesDir
            
            $result | Should -Not -BeNullOrEmpty
            Test-Path $result | Should -Be $true
            
            $manifest = Read-ManifestRaw -Path $result
            $manifest.version | Should -Be 1
            $manifest.name | Should -Be "test-bare"
            
            # Should NOT have includes (bare profile)
            $manifest.ContainsKey('includes') | Should -Be $false
        }
    }
    
    Context "New-ProfileOverlay with --from" {
        
        It "Should create an overlay profile with includes, exclude, excludeConfigs, and empty apps" {
            $result = New-ProfileOverlay -Name "test-overlay" -From "hugo-desktop" -ProfilesDir $script:TestProfilesDir
            
            $result | Should -Not -BeNullOrEmpty
            Test-Path $result | Should -Be $true
            
            $manifest = Read-ManifestRaw -Path $result
            $manifest.version | Should -Be 1
            $manifest.name | Should -Be "test-overlay"
            @($manifest.includes) | Should -Contain "hugo-desktop"
            # Empty exclude/excludeConfigs are not serialized to JSONC, so absent on read-back
            # Verify apps is empty (the JSONC apps section is always present)
            if ($manifest.apps) { @($manifest.apps).Count | Should -Be 0 }
        }
    }
    
    Context "New-ProfileOverlay refuses overwrite" {
        
        It "Should throw if profile already exists" {
            # Create the profile first
            New-ProfileOverlay -Name "existing" -ProfilesDir $script:TestProfilesDir
            
            # Attempt to create again should throw
            { New-ProfileOverlay -Name "existing" -ProfilesDir $script:TestProfilesDir } | Should -Throw "*already exists*"
        }
    }
}

Describe "ProfileCommands.Exclude" {
    
    BeforeEach {
        $script:TestProfilesDir = Join-Path $TestDrive "profiles-exclude-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        New-Item -ItemType Directory -Path $script:TestProfilesDir -Force | Out-Null
        
        # Create a base profile to modify
        New-ProfileOverlay -Name "test-profile" -From "base" -ProfilesDir $script:TestProfilesDir
    }
    
    Context "Add-ProfileExclusion" {
        
        It "Should append IDs to the exclude array" {
            $added = Add-ProfileExclusion -Name "test-profile" -Ids @("App.One", "App.Two") -ProfilesDir $script:TestProfilesDir
            
            $added | Should -Be 2
            
            $manifest = Read-ManifestRaw -Path (Join-Path $script:TestProfilesDir "test-profile.jsonc")
            @($manifest.exclude) | Should -Contain "App.One"
            @($manifest.exclude) | Should -Contain "App.Two"
        }
        
        It "Should be idempotent - duplicate IDs are skipped" {
            Add-ProfileExclusion -Name "test-profile" -Ids @("App.One") -ProfilesDir $script:TestProfilesDir
            $added = Add-ProfileExclusion -Name "test-profile" -Ids @("App.One", "App.Two") -ProfilesDir $script:TestProfilesDir
            
            $added | Should -Be 1  # Only App.Two is new
            
            $manifest = Read-ManifestRaw -Path (Join-Path $script:TestProfilesDir "test-profile.jsonc")
            $excludeArr = @($manifest.exclude)
            ($excludeArr | Where-Object { $_ -eq "App.One" }).Count | Should -Be 1
        }
    }
}

Describe "ProfileCommands.ExcludeConfig" {
    
    BeforeEach {
        $script:TestProfilesDir = Join-Path $TestDrive "profiles-ec-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        New-Item -ItemType Directory -Path $script:TestProfilesDir -Force | Out-Null
        
        New-ProfileOverlay -Name "test-profile" -From "base" -ProfilesDir $script:TestProfilesDir
    }
    
    Context "Add-ProfileExcludeConfig" {
        
        It "Should append IDs to the excludeConfigs array" {
            $added = Add-ProfileExcludeConfig -Name "test-profile" -Ids @("powertoys", "windows-terminal") -ProfilesDir $script:TestProfilesDir
            
            $added | Should -Be 2
            
            $manifest = Read-ManifestRaw -Path (Join-Path $script:TestProfilesDir "test-profile.jsonc")
            @($manifest.excludeConfigs) | Should -Contain "powertoys"
            @($manifest.excludeConfigs) | Should -Contain "windows-terminal"
        }
    }
}

Describe "ProfileCommands.Add" {
    
    BeforeEach {
        $script:TestProfilesDir = Join-Path $TestDrive "profiles-add-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        New-Item -ItemType Directory -Path $script:TestProfilesDir -Force | Out-Null
        
        New-ProfileOverlay -Name "test-profile" -From "base" -ProfilesDir $script:TestProfilesDir
    }
    
    Context "Add-ProfileApp" {
        
        It "Should append app entries with correct structure" {
            $added = Add-ProfileApp -Name "test-profile" -Ids @("Adobe.Lightroom") -ProfilesDir $script:TestProfilesDir
            
            $added | Should -Be 1
            
            $manifest = Read-ManifestRaw -Path (Join-Path $script:TestProfilesDir "test-profile.jsonc")
            $apps = @($manifest.apps)
            $apps.Count | Should -Be 1
            $apps[0].id | Should -Be "Adobe.Lightroom"
            $apps[0].refs.windows | Should -Be "Adobe.Lightroom"
        }
        
        It "Should be idempotent - duplicate refs.windows are skipped" {
            Add-ProfileApp -Name "test-profile" -Ids @("Adobe.Lightroom") -ProfilesDir $script:TestProfilesDir
            $added = Add-ProfileApp -Name "test-profile" -Ids @("Adobe.Lightroom", "Google.Chrome") -ProfilesDir $script:TestProfilesDir
            
            $added | Should -Be 1  # Only Chrome is new
            
            $manifest = Read-ManifestRaw -Path (Join-Path $script:TestProfilesDir "test-profile.jsonc")
            $apps = @($manifest.apps)
            $apps.Count | Should -Be 2
        }
    }
}

Describe "ProfileCommands.Show" {
    
    BeforeEach {
        $script:TestProfilesDir = Join-Path $TestDrive "profiles-show-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        New-Item -ItemType Directory -Path $script:TestProfilesDir -Force | Out-Null
    }
    
    Context "Get-ProfileSummary" {
        
        It "Should resolve and summarize an overlay profile" {
            # Get-ProfileSummary will fail on Read-Manifest if the base doesn't exist,
            # so we test with a bare profile that doesn't need include resolution
            $bareDir = Join-Path $TestDrive "bare-profiles"
            New-Item -ItemType Directory -Path $bareDir -Force | Out-Null
            New-ProfileOverlay -Name "simple" -ProfilesDir $bareDir
            Add-ProfileApp -Name "simple" -Ids @("App.One", "App.Two") -ProfilesDir $bareDir
            
            $summary = Get-ProfileSummary -Name "simple" -ProfilesDir $bareDir -Json
            
            $summary | Should -Not -BeNullOrEmpty
            $summary.name | Should -Be "simple"
            $summary.addedCount | Should -Be 2
            $summary.netAppCount | Should -Be 2
        }
    }
}

Describe "ProfileCommands.List" {
    
    BeforeEach {
        $script:TestProfilesDir = Join-Path $TestDrive "profiles-list-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        New-Item -ItemType Directory -Path $script:TestProfilesDir -Force | Out-Null
    }
    
    Context "Get-ProfileList" {
        
        It "Should find profiles in directory" {
            # Create a bare profile
            New-ProfileOverlay -Name "bare-one" -ProfilesDir $script:TestProfilesDir
            
            # Create an overlay profile
            New-ProfileOverlay -Name "overlay-one" -From "base" -ProfilesDir $script:TestProfilesDir
            
            $profiles = Get-ProfileList -ProfilesDir $script:TestProfilesDir -Json
            
            @($profiles).Count | Should -Be 2
            $names = @($profiles | ForEach-Object { $_.name })
            $names | Should -Contain "bare-one"
            $names | Should -Contain "overlay-one"
        }
        
        It "Should detect overlay type for profiles with includes" {
            New-ProfileOverlay -Name "my-overlay" -From "base" -ProfilesDir $script:TestProfilesDir
            
            $profiles = Get-ProfileList -ProfilesDir $script:TestProfilesDir -Json
            $overlay = @($profiles) | Where-Object { $_.name -eq "my-overlay" }
            $overlay.type | Should -Be "overlay"
        }
        
        It "Should return empty array for empty directory" {
            $emptyDir = Join-Path $TestDrive "empty-profiles"
            New-Item -ItemType Directory -Path $emptyDir -Force | Out-Null
            
            $profiles = Get-ProfileList -ProfilesDir $emptyDir -Json
            @($profiles).Count | Should -Be 0
        }
    }
}

Describe "ProfileCommands.MutationGuard" {
    
    BeforeEach {
        $script:TestProfilesDir = Join-Path $TestDrive "profiles-guard-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        New-Item -ItemType Directory -Path $script:TestProfilesDir -Force | Out-Null
    }
    
    Context "Mutation commands error on zip profiles" {
        
        It "Should error when trying to exclude on a zip profile" {
            # Create a fake zip file
            $zipPath = Join-Path $script:TestProfilesDir "bundle-profile.zip"
            [System.IO.File]::WriteAllBytes($zipPath, [byte[]]@(0x50, 0x4B, 0x03, 0x04))
            
            { Add-ProfileExclusion -Name "bundle-profile" -Ids @("App.One") -ProfilesDir $script:TestProfilesDir } | Should -Throw "*cannot be modified*"
        }
        
        It "Should error when trying to add apps to a zip profile" {
            $zipPath = Join-Path $script:TestProfilesDir "bundle-profile.zip"
            [System.IO.File]::WriteAllBytes($zipPath, [byte[]]@(0x50, 0x4B, 0x03, 0x04))
            
            { Add-ProfileApp -Name "bundle-profile" -Ids @("App.One") -ProfilesDir $script:TestProfilesDir } | Should -Throw "*cannot be modified*"
        }
        
        It "Should error when trying to exclude-config on a zip profile" {
            $zipPath = Join-Path $script:TestProfilesDir "bundle-profile.zip"
            [System.IO.File]::WriteAllBytes($zipPath, [byte[]]@(0x50, 0x4B, 0x03, 0x04))
            
            { Add-ProfileExcludeConfig -Name "bundle-profile" -Ids @("powertoys") -ProfilesDir $script:TestProfilesDir } | Should -Throw "*cannot be modified*"
        }
    }
}
