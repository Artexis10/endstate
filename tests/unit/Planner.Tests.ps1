<#
.SYNOPSIS
    Pester tests for planner skip/install classification logic.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    $script:ManifestScript = Join-Path $script:ProvisioningRoot "engine\manifest.ps1"
    $script:StateScript = Join-Path $script:ProvisioningRoot "engine\state.ps1"
    $script:PlanScript = Join-Path $script:ProvisioningRoot "engine\plan.ps1"
    $script:LoggingScript = Join-Path $script:ProvisioningRoot "engine\logging.ps1"
    $script:FixturesDir = Join-Path $PSScriptRoot "..\fixtures"
    
    # Load dependencies (Pester 3.x compatible - no BeforeAll at script level)
    . $script:LoggingScript
    . $script:ManifestScript
    . $script:StateScript
    
    # Load plan.ps1 functions without re-dot-sourcing dependencies
    $planContent = Get-Content -Path $script:PlanScript -Raw
    $functionsOnly = $planContent -replace '\. "\$PSScriptRoot\\[^"]+\.ps1"', '# (dependency already loaded)'
    Invoke-Expression $functionsOnly
}

Describe "Planner.SkipLogic" {
    
    Context "Already installed detection" {
        
        It "Should mark app as skip when in installed list" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            # Mock: Test.App2 is already installed
            $installedApps = @("Test.App2")
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps $installedApps
            
            $app2Action = $plan.actions | Where-Object { $_.ref -eq "Test.App2" }
            $app2Action.status | Should -Be "skip"
        }
        
        It "Should set reason to 'already installed' for skipped apps" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            $installedApps = @("Test.App2")
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps $installedApps
            
            $app2Action = $plan.actions | Where-Object { $_.ref -eq "Test.App2" }
            $app2Action.reason | Should -Be "already installed"
        }
        
        It "Should mark app as install when not in installed list" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            # Mock: Test.App2 is installed, but Test.App1 is not
            $installedApps = @("Test.App2")
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps $installedApps
            
            $app1Action = $plan.actions | Where-Object { $_.ref -eq "Test.App1" }
            $app1Action.status | Should -Be "install"
        }
        
        It "Should not have reason field for install actions" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            $installedApps = @("Test.App2")
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps $installedApps
            
            $app1Action = $plan.actions | Where-Object { $_.ref -eq "Test.App1" }
            $app1Action.ContainsKey('reason') | Should -Be $false
        }
    }
    
    Context "Multiple apps installed" {
        
        It "Should correctly classify when all apps are installed" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            # All apps installed
            $installedApps = @("Test.App1", "Test.App2", "Test.App3")
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps $installedApps
            
            $plan.summary.install | Should -Be 0
            $plan.summary.skip | Should -Be 3
        }
        
        It "Should correctly classify when no apps are installed" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            # No apps installed
            $installedApps = @()
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps $installedApps
            
            $plan.summary.install | Should -Be 3
            $plan.summary.skip | Should -Be 0
        }
        
        It "Should correctly classify mixed install/skip scenario" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            # Only App1 and App3 installed
            $installedApps = @("Test.App1", "Test.App3")
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps $installedApps
            
            $plan.summary.install | Should -Be 1  # Only App2
            $plan.summary.skip | Should -Be 2     # App1 and App3
        }
    }
    
    Context "Summary counts accuracy" {
        
        It "Should count restore actions correctly" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps @()
            
            $plan.summary.restore | Should -Be 1
        }
        
        It "Should count verify actions correctly" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps @()
            
            $plan.summary.verify | Should -Be 1
        }
        
        It "Should have total actions equal to sum of all types" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            $installedApps = @("Test.App2")
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps $installedApps
            
            $expectedTotal = $plan.summary.install + $plan.summary.skip + $plan.summary.restore + $plan.summary.verify
            $plan.actions.Count | Should -Be $expectedTotal
        }
    }
}