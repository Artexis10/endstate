<#
.SYNOPSIS
    Pester tests for pathExists matcher in engine/config-modules.ps1.
#>

BeforeAll {
    $script:EndstateRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)

    # manifest.ps1 must be loaded first because config-modules.ps1 dot-sources it.
    . (Join-Path $script:EndstateRoot "engine\manifest.ps1")
    . (Join-Path $script:EndstateRoot "engine\config-modules.ps1")
}

Describe "pathExists Matcher" {

    BeforeEach {
        # Create temp directory for fake paths
        $script:TempDir = Join-Path $env:TEMP "pathexists-tests-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TempDir -Force | Out-Null
    }

    AfterEach {
        if (Test-Path $script:TempDir) {
            Remove-Item -Path $script:TempDir -Recurse -Force
        }
    }

    Context "Get-ConfigModulesForInstalledApps with pathExists" {

        It "Should match when pathExists path exists on disk" {
            # Create a fake file to match
            $fakePath = Join-Path $script:TempDir "app.exe"
            New-Item -ItemType File -Path $fakePath -Force | Out-Null

            # Build a mock catalog with pathExists pointing to the temp file
            $mockCatalog = @{
                "apps.testapp" = @{
                    id          = "apps.testapp"
                    displayName = "Test App"
                    matches     = @{
                        winget    = @()
                        exe       = @()
                        pathExists = @($fakePath)
                    }
                    restore     = @()
                    verify      = @()
                }
            }

            # Override the catalog cache
            $script:ConfigModuleCatalog = $mockCatalog
            $script:ConfigModuleCatalogLoaded = $true

            $results = Get-ConfigModulesForInstalledApps -WingetInstalledIds @() -DiscoveredItems @()

            $results.Count | Should -Be 1
            $results[0].moduleId | Should -Be "apps.testapp"
            $results[0].matchReasons | Should -Contain "pathExists:$fakePath"

            # Clean up cache
            Clear-ConfigModuleCatalogCache
        }

        It "Should not match when pathExists paths do not exist" {
            $mockCatalog = @{
                "apps.testapp" = @{
                    id          = "apps.testapp"
                    displayName = "Test App"
                    matches     = @{
                        winget    = @()
                        exe       = @()
                        pathExists = @("C:\nonexistent\path\fake.exe")
                    }
                    restore     = @()
                    verify      = @()
                }
            }

            $script:ConfigModuleCatalog = $mockCatalog
            $script:ConfigModuleCatalogLoaded = $true

            $results = Get-ConfigModulesForInstalledApps -WingetInstalledIds @() -DiscoveredItems @()

            $results.Count | Should -Be 0

            Clear-ConfigModuleCatalogCache
        }

        It "Should report both winget and pathExists reasons when both match" {
            $fakePath = Join-Path $script:TempDir "app.exe"
            New-Item -ItemType File -Path $fakePath -Force | Out-Null

            $mockCatalog = @{
                "apps.testapp" = @{
                    id          = "apps.testapp"
                    displayName = "Test App"
                    matches     = @{
                        winget    = @("Test.App")
                        exe       = @()
                        pathExists = @($fakePath)
                    }
                    restore     = @()
                    verify      = @()
                }
            }

            $script:ConfigModuleCatalog = $mockCatalog
            $script:ConfigModuleCatalogLoaded = $true

            $results = Get-ConfigModulesForInstalledApps -WingetInstalledIds @("Test.App") -DiscoveredItems @()

            $results.Count | Should -Be 1
            $results[0].matchReasons.Count | Should -Be 2
            ($results[0].matchReasons | Where-Object { $_ -like "winget:*" }) | Should -Not -BeNullOrEmpty
            ($results[0].matchReasons | Where-Object { $_ -like "pathExists:*" }) | Should -Not -BeNullOrEmpty

            Clear-ConfigModuleCatalogCache
        }

        It "Should not match via pathExists when array is empty" {
            $mockCatalog = @{
                "apps.testapp" = @{
                    id          = "apps.testapp"
                    displayName = "Test App"
                    matches     = @{
                        winget    = @()
                        exe       = @()
                        pathExists = @()
                    }
                    restore     = @()
                    verify      = @()
                }
            }

            $script:ConfigModuleCatalog = $mockCatalog
            $script:ConfigModuleCatalogLoaded = $true

            $results = Get-ConfigModulesForInstalledApps -WingetInstalledIds @() -DiscoveredItems @()

            $results.Count | Should -Be 0

            Clear-ConfigModuleCatalogCache
        }

        It "Should match on second path when first does not exist" {
            $fakePath = Join-Path $script:TempDir "config.xml"
            New-Item -ItemType File -Path $fakePath -Force | Out-Null

            $mockCatalog = @{
                "apps.testapp" = @{
                    id          = "apps.testapp"
                    displayName = "Test App"
                    matches     = @{
                        winget    = @()
                        exe       = @()
                        pathExists = @("C:\nonexistent\fake.exe", $fakePath)
                    }
                    restore     = @()
                    verify      = @()
                }
            }

            $script:ConfigModuleCatalog = $mockCatalog
            $script:ConfigModuleCatalogLoaded = $true

            $results = Get-ConfigModulesForInstalledApps -WingetInstalledIds @() -DiscoveredItems @()

            $results.Count | Should -Be 1
            $results[0].matchReasons | Should -Contain "pathExists:$fakePath"
            # Should only have one pathExists reason (early exit)
            ($results[0].matchReasons | Where-Object { $_ -like "pathExists:*" }).Count | Should -Be 1

            Clear-ConfigModuleCatalogCache
        }
    }

    Context "Expand-ConfigPath in pathExists" {

        It "Should expand environment variables in pathExists paths" {
            # Use %TEMP% which resolves to our known temp dir
            $subDir = Join-Path $env:TEMP "pathexists-envtest-$(Get-Random)"
            New-Item -ItemType Directory -Path $subDir -Force | Out-Null
            $fakePath = Join-Path $subDir "test.cfg"
            New-Item -ItemType File -Path $fakePath -Force | Out-Null

            # Build the env var path
            $envVarPath = "%TEMP%\pathexists-envtest-$($subDir | Split-Path -Leaf | ForEach-Object { $_ -replace '.*-', '' })"
            # Actually, let's use the real subdir name
            $subDirName = Split-Path -Leaf $subDir
            $envVarPath = "%TEMP%\$subDirName\test.cfg"

            $mockCatalog = @{
                "apps.envtest" = @{
                    id          = "apps.envtest"
                    displayName = "Env Test"
                    matches     = @{
                        winget    = @()
                        exe       = @()
                        pathExists = @($envVarPath)
                    }
                    restore     = @()
                    verify      = @()
                }
            }

            $script:ConfigModuleCatalog = $mockCatalog
            $script:ConfigModuleCatalogLoaded = $true

            $results = Get-ConfigModulesForInstalledApps -WingetInstalledIds @() -DiscoveredItems @()

            $results.Count | Should -Be 1
            $results[0].moduleId | Should -Be "apps.envtest"

            Clear-ConfigModuleCatalogCache
            Remove-Item -Path $subDir -Recurse -Force
        }
    }
}

Describe "Test-ConfigModuleSchema pathExists validation" {

    It "Should accept module with valid pathExists array" {
        $module = @{
            id          = "apps.test"
            displayName = "Test"
            matches     = @{
                pathExists = @("C:\some\path.exe")
            }
        }

        $result = Test-ConfigModuleSchema -Module $module
        $result.Valid | Should -Be $true
    }

    It "Should reject module where pathExists is not an array" {
        $module = @{
            id          = "apps.test"
            displayName = "Test"
            matches     = @{
                pathExists = "C:\some\path.exe"
            }
        }

        $result = Test-ConfigModuleSchema -Module $module
        $result.Valid | Should -Be $false
        $result.Error | Should -Match "pathExists"
    }

    It "Should reject module where pathExists contains empty strings" {
        $module = @{
            id          = "apps.test"
            displayName = "Test"
            matches     = @{
                pathExists = @("C:\valid\path.exe", "")
            }
        }

        $result = Test-ConfigModuleSchema -Module $module
        $result.Valid | Should -Be $false
        $result.Error | Should -Match "non-empty string"
    }

    It "Should accept module with pathExists as the only matcher" {
        $module = @{
            id          = "apps.test"
            displayName = "Test"
            matches     = @{
                winget    = @()
                exe       = @()
                pathExists = @("C:\some\path.exe")
            }
        }

        $result = Test-ConfigModuleSchema -Module $module
        $result.Valid | Should -Be $true
    }
}
