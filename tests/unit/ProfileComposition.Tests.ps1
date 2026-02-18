<#
.SYNOPSIS
    Pester tests for profile composition: includes with profile name resolution and exclusions.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    $script:ManifestScript = Join-Path $script:ProvisioningRoot "engine\manifest.ps1"
    $script:BundleScript = Join-Path $script:ProvisioningRoot "engine\bundle.ps1"
    $script:FixturesDir = Join-Path $PSScriptRoot "..\fixtures"
    
    # Load dependencies
    . $script:ManifestScript
    . $script:BundleScript
}

Describe "ProfileComposition" {

    Context "Extensionless include resolves to profile via Resolve-ProfilePath" {

        It "Should resolve extensionless include as profile name" {
            # Setup: create a temp dir that mimics Documents\Endstate\Profiles
            $tempDocuments = Join-Path $env:TEMP "endstate-test-compose-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            $profilesDir = Join-Path $tempDocuments "Endstate\Profiles"
            New-Item -ItemType Directory -Path $profilesDir -Force | Out-Null

            try {
                # Create the included profile (bare format)
                $includedContent = @'
{
    "version": 1,
    "name": "base-profile",
    "apps": [
        { "id": "included-app", "refs": { "windows": "Included.App" } }
    ]
}
'@
                Set-Content -Path (Join-Path $profilesDir "my-base.jsonc") -Value $includedContent -Encoding UTF8

                # Test Resolve-ProfilePath directly (unit test of the resolution)
                $result = Resolve-ProfilePath -ProfileName "my-base" -ProfilesDir $profilesDir
                $result.Found | Should -Be $true
                $result.Format | Should -Be "bare"

                # Test that the resolved path can be loaded as a manifest
                $includedManifest = Read-ManifestInternal -Path (Join-Path $profilesDir "my-base.jsonc")
                
                # Verify the included manifest loaded correctly
                $includedManifest.apps.Count | Should -Be 1
                $includedManifest.apps[0].refs.windows | Should -Be "Included.App"
            } finally {
                if (Test-Path $tempDocuments) {
                    Remove-Item -Path $tempDocuments -Recurse -Force -ErrorAction SilentlyContinue
                }
            }
        }
    }

    Context "Include with extension resolves as file path (backward compat)" {

        It "Should resolve include with .jsonc extension as file path" {
            $tempDir = Join-Path $env:TEMP "endstate-test-compose-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

            try {
                # Create included file
                $includedContent = @'
{
    "version": 1,
    "apps": [
        { "id": "file-app", "refs": { "windows": "File.App" } }
    ]
}
'@
                Set-Content -Path (Join-Path $tempDir "extras.jsonc") -Value $includedContent -Encoding UTF8

                # Create root manifest with file path include
                $rootContent = @'
{
    "version": 1,
    "name": "root",
    "includes": ["./extras.jsonc"],
    "apps": [
        { "id": "root-app", "refs": { "windows": "Root.App" } }
    ]
}
'@
                $rootPath = Join-Path $tempDir "root.jsonc"
                Set-Content -Path $rootPath -Value $rootContent -Encoding UTF8

                $manifest = Read-Manifest -Path $rootPath

                $manifest.apps.Count | Should -Be 2
                $wingetIds = @($manifest.apps | ForEach-Object { $_.refs.windows })
                $wingetIds | Should -Contain "Root.App"
                $wingetIds | Should -Contain "File.App"
            } finally {
                if (Test-Path $tempDir) {
                    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
                }
            }
        }
    }

    Context "Exclude removes matching apps from merged list by refs.windows" {

        It "Should remove apps matching exclude entries" {
            $tempDir = Join-Path $env:TEMP "endstate-test-compose-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

            try {
                $rootContent = @'
{
    "version": 1,
    "name": "test-exclude",
    "exclude": ["Unwanted.App", "Another.Unwanted"],
    "apps": [
        { "id": "keep-me", "refs": { "windows": "Keep.Me" } },
        { "id": "unwanted", "refs": { "windows": "Unwanted.App" } },
        { "id": "also-unwanted", "refs": { "windows": "Another.Unwanted" } },
        { "id": "also-keep", "refs": { "windows": "Also.Keep" } }
    ]
}
'@
                $rootPath = Join-Path $tempDir "root.jsonc"
                Set-Content -Path $rootPath -Value $rootContent -Encoding UTF8

                $manifest = Read-Manifest -Path $rootPath

                $manifest.apps.Count | Should -Be 2
                $wingetIds = @($manifest.apps | ForEach-Object { $_.refs.windows })
                $wingetIds | Should -Contain "Keep.Me"
                $wingetIds | Should -Contain "Also.Keep"
                $wingetIds | Should -Not -Contain "Unwanted.App"
                $wingetIds | Should -Not -Contain "Another.Unwanted"
            } finally {
                if (Test-Path $tempDir) {
                    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
                }
            }
        }
    }

    Context "ExcludeConfigs is preserved for downstream processing" {

        It "Should preserve excludeConfigs array in manifest" {
            $tempDir = Join-Path $env:TEMP "endstate-test-compose-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

            try {
                $rootContent = @'
{
    "version": 1,
    "name": "test-excludeconfigs",
    "excludeConfigs": ["powertoys", "windows-terminal"],
    "apps": [
        { "id": "app1", "refs": { "windows": "App.One" } }
    ]
}
'@
                $rootPath = Join-Path $tempDir "root.jsonc"
                Set-Content -Path $rootPath -Value $rootContent -Encoding UTF8

                $manifest = Read-Manifest -Path $rootPath

                $manifest.excludeConfigs | Should -Not -BeNullOrEmpty
                $manifest.excludeConfigs | Should -Contain "powertoys"
                $manifest.excludeConfigs | Should -Contain "windows-terminal"
            } finally {
                if (Test-Path $tempDir) {
                    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
                }
            }
        }
    }

    Context "Exclude implies excludeConfigs" {

        It "Should add excluded app winget IDs to excludeConfigs" {
            $tempDir = Join-Path $env:TEMP "endstate-test-compose-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

            try {
                $rootContent = @'
{
    "version": 1,
    "name": "test-exclude-implies",
    "exclude": ["Excluded.App"],
    "excludeConfigs": ["manual-exclude"],
    "apps": [
        { "id": "keep", "refs": { "windows": "Keep.App" } },
        { "id": "excluded", "refs": { "windows": "Excluded.App" } }
    ]
}
'@
                $rootPath = Join-Path $tempDir "root.jsonc"
                Set-Content -Path $rootPath -Value $rootContent -Encoding UTF8

                $manifest = Read-Manifest -Path $rootPath

                # excludeConfigs should contain both the manual entry and the excluded app
                $manifest.excludeConfigs | Should -Contain "manual-exclude"
                $manifest.excludeConfigs | Should -Contain "Excluded.App"
            } finally {
                if (Test-Path $tempDir) {
                    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
                }
            }
        }
    }

    Context "Included profile's own exclude is NOT inherited (root only)" {

        It "Should ignore exclude from included manifest" {
            $tempDir = Join-Path $env:TEMP "endstate-test-compose-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

            try {
                # Create included manifest that has its own exclude
                $includedContent = @'
{
    "version": 1,
    "name": "included-with-exclude",
    "exclude": ["ShouldNot.BeExcluded"],
    "apps": [
        { "id": "included-app", "refs": { "windows": "Included.App" } },
        { "id": "target-app", "refs": { "windows": "ShouldNot.BeExcluded" } }
    ]
}
'@
                Set-Content -Path (Join-Path $tempDir "included.jsonc") -Value $includedContent -Encoding UTF8

                # Root manifest includes the file but does NOT exclude that app
                $rootContent = @'
{
    "version": 1,
    "name": "root",
    "includes": ["./included.jsonc"],
    "apps": [
        { "id": "root-app", "refs": { "windows": "Root.App" } }
    ]
}
'@
                $rootPath = Join-Path $tempDir "root.jsonc"
                Set-Content -Path $rootPath -Value $rootContent -Encoding UTF8

                $manifest = Read-Manifest -Path $rootPath

                # The included profile's exclude should NOT have removed the app
                # because only root-level exclude applies
                $wingetIds = @($manifest.apps | ForEach-Object { $_.refs.windows })
                $wingetIds | Should -Contain "ShouldNot.BeExcluded"
                $wingetIds | Should -Contain "Included.App"
                $wingetIds | Should -Contain "Root.App"
                $manifest.apps.Count | Should -Be 3
            } finally {
                if (Test-Path $tempDir) {
                    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
                }
            }
        }
    }

    Context "Normalize-Manifest adds exclude and excludeConfigs defaults" {

        It "Should add empty exclude and excludeConfigs arrays to bare manifest" {
            $manifest = @{
                version = 1
                name = "test"
                apps = @()
            }

            $normalized = Normalize-Manifest -Manifest $manifest

            $normalized.ContainsKey('exclude') | Should -Be $true -Because "exclude key should exist"
            $normalized.exclude | Should -HaveCount 0
            $normalized.ContainsKey('excludeConfigs') | Should -Be $true -Because "excludeConfigs key should exist"
            $normalized.excludeConfigs | Should -HaveCount 0
        }

        It "Should preserve existing exclude and excludeConfigs values" {
            $manifest = @{
                version = 1
                name = "test"
                apps = @()
                exclude = @("Some.App")
                excludeConfigs = @("some-module")
            }

            $normalized = Normalize-Manifest -Manifest $manifest

            $normalized.exclude | Should -HaveCount 1
            $normalized.exclude | Should -Contain "Some.App"
            $normalized.excludeConfigs | Should -HaveCount 1
            $normalized.excludeConfigs | Should -Contain "some-module"
        }
    }

    Context "Profile name not found produces clear error" {

        It "Should throw clear error when profile name cannot be resolved" {
            $tempDir = Join-Path $env:TEMP "endstate-test-compose-$([guid]::NewGuid().ToString('N').Substring(0,8))"
            New-Item -ItemType Directory -Path $tempDir -Force | Out-Null

            try {
                $rootContent = @'
{
    "version": 1,
    "name": "root",
    "includes": ["nonexistent-profile"],
    "apps": []
}
'@
                $rootPath = Join-Path $tempDir "root.jsonc"
                Set-Content -Path $rootPath -Value $rootContent -Encoding UTF8

                { Read-Manifest -Path $rootPath } | Should -Throw "*Included profile not found: nonexistent-profile*"
            } finally {
                if (Test-Path $tempDir) {
                    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
                }
            }
        }
    }
}
