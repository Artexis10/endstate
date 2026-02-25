<#
.SYNOPSIS
    Pester tests for Build-ConfigModuleMap in engine/config-modules.ps1.
#>

BeforeAll {
    $script:EndstateRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)

    # manifest.ps1 must be loaded first because config-modules.ps1 dot-sources it.
    . (Join-Path $script:EndstateRoot "engine\manifest.ps1")
    . (Join-Path $script:EndstateRoot "engine\config-modules.ps1")

    # Catalog used across all test cases.
    $script:MockCatalog = @{
        "apps.git" = @{
            id          = "apps.git"
            displayName = "Git"
            matches     = @{ winget = @("Git.Git") }
        }
        "apps.powertoys" = @{
            id          = "apps.powertoys"
            displayName = "PowerToys"
            matches     = @{ winget = @("Microsoft.PowerToys") }
        }
        "apps.vscode" = @{
            id          = "apps.vscode"
            displayName = "VS Code"
            matches     = @{ winget = @("Microsoft.VisualStudioCode", "Microsoft.VisualStudioCode.Insiders") }
        }
        "apps.nowinget" = @{
            id          = "apps.nowinget"
            displayName = "No Winget"
            matches     = @{ exe = @("nowinget.exe") }
        }
    }
}

Describe "Build-ConfigModuleMap" {

    Context "Empty or null input" {

        It "Should return null when ModuleIds is an empty array" {
            $result = Build-ConfigModuleMap -ModuleIds @() -Catalog $script:MockCatalog

            $result | Should -BeNull
        }

        It "Should return null when ModuleIds is null" {
            $result = Build-ConfigModuleMap -ModuleIds $null -Catalog $script:MockCatalog

            $result | Should -BeNull
        }
    }

    Context "Single module with a single winget ref" {

        It "Should return a map with one entry" {
            $result = Build-ConfigModuleMap -ModuleIds @("apps.git") -Catalog $script:MockCatalog

            $result | Should -Not -BeNull
            $result.Count | Should -Be 1
        }

        It "Should map the winget ref to the correct module ID" {
            $result = Build-ConfigModuleMap -ModuleIds @("apps.git") -Catalog $script:MockCatalog

            $result["Git.Git"] | Should -Be "apps.git"
        }
    }

    Context "Multiple modules with multiple winget refs" {

        It "Should return a map with entries for all winget refs" {
            $result = Build-ConfigModuleMap -ModuleIds @("apps.git", "apps.powertoys", "apps.vscode") -Catalog $script:MockCatalog

            # apps.git -> 1 ref, apps.powertoys -> 1 ref, apps.vscode -> 2 refs = 4 total
            $result | Should -Not -BeNull
            $result.Count | Should -Be 4
        }

        It "Should map each winget ref to its module ID" {
            $result = Build-ConfigModuleMap -ModuleIds @("apps.git", "apps.powertoys", "apps.vscode") -Catalog $script:MockCatalog

            $result["Git.Git"]                            | Should -Be "apps.git"
            $result["Microsoft.PowerToys"]               | Should -Be "apps.powertoys"
            $result["Microsoft.VisualStudioCode"]        | Should -Be "apps.vscode"
            $result["Microsoft.VisualStudioCode.Insiders"] | Should -Be "apps.vscode"
        }
    }

    Context "Module IDs not present in the catalog" {

        It "Should return null when none of the module IDs exist in the catalog" {
            $result = Build-ConfigModuleMap -ModuleIds @("apps.missing", "apps.alsomissing") -Catalog $script:MockCatalog

            $result | Should -BeNull
        }
    }

    Context "Modules with no winget matches" {

        It "Should return null when module exists but has no winget matches" {
            $result = Build-ConfigModuleMap -ModuleIds @("apps.nowinget") -Catalog $script:MockCatalog

            $result | Should -BeNull
        }
    }

    Context "Mixed: some modules found, some not in catalog" {

        It "Should include only entries for modules that exist in the catalog" {
            $result = Build-ConfigModuleMap -ModuleIds @("apps.git", "apps.notincatalog") -Catalog $script:MockCatalog

            $result | Should -Not -BeNull
            $result.Count | Should -Be 1
            $result["Git.Git"] | Should -Be "apps.git"
        }

        It "Should not contain keys for missing modules" {
            $result = Build-ConfigModuleMap -ModuleIds @("apps.git", "apps.notincatalog") -Catalog $script:MockCatalog

            $result.Keys | Should -Not -Contain "apps.notincatalog"
        }
    }

    Context "Duplicate winget refs across modules - last module wins" {

        It "Should let the later module overwrite an earlier module's ref" {
            # Build a catalog where two modules share the same winget ref
            $conflictCatalog = @{
                "apps.first" = @{
                    id          = "apps.first"
                    displayName = "First App"
                    matches     = @{ winget = @("Shared.PackageId") }
                }
                "apps.second" = @{
                    id          = "apps.second"
                    displayName = "Second App"
                    matches     = @{ winget = @("Shared.PackageId") }
                }
            }

            # Pass second after first so it wins the key
            $result = Build-ConfigModuleMap -ModuleIds @("apps.first", "apps.second") -Catalog $conflictCatalog

            $result | Should -Not -BeNull
            $result["Shared.PackageId"] | Should -Be "apps.second"
        }

        It "Should contain only one entry for the shared ref" {
            $conflictCatalog = @{
                "apps.first" = @{
                    id          = "apps.first"
                    displayName = "First App"
                    matches     = @{ winget = @("Shared.PackageId") }
                }
                "apps.second" = @{
                    id          = "apps.second"
                    displayName = "Second App"
                    matches     = @{ winget = @("Shared.PackageId") }
                }
            }

            $result = Build-ConfigModuleMap -ModuleIds @("apps.first", "apps.second") -Catalog $conflictCatalog

            $result.Count | Should -Be 1
        }
    }
}
