<#
.SYNOPSIS
    Pester tests for plan generation determinism and hash stability.
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
    
    # Load plan.ps1 but suppress its dot-sourcing of dependencies (already loaded)
    $planContent = Get-Content -Path $script:PlanScript -Raw
    $functionsOnly = $planContent -replace '\. "\$PSScriptRoot\\[^"]+\.ps1"', '# (dependency already loaded)'
    Invoke-Expression $functionsOnly
}

Describe "Plan.Deterministic.HashAndRunId" {
    
    Context "Manifest hash determinism" {
        
        It "Should produce same hash for same manifest file" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            
            $hash1 = Get-ManifestHash -ManifestPath $yamlPath
            $hash2 = Get-ManifestHash -ManifestPath $yamlPath
            
            $hash1 | Should -Be $hash2
        }
        
        It "Should produce 16-character hash" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            $hash | Should -Not -BeNullOrEmpty
            $hash.Length | Should -Be 16
        }
        
        It "Should produce different hash for different manifests" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            
            $hash1 = Get-ManifestHash -ManifestPath $yamlPath
            $hash2 = Get-ManifestHash -ManifestPath $jsoncPath
            
            $hash1 | Should -Not -Be $hash2
        }
    }
    
    Context "RunId format validation" {
        
        It "Should produce RunId in expected format (yyyyMMdd-HHmmss)" {
            $runId = Get-RunId
            
            $runId | Should -Match '^\d{8}-\d{6}$'
        }
        
        It "Should produce valid date component" {
            $runId = Get-RunId
            $datePart = $runId.Split('-')[0]
            
            # Should be parseable as date
            $year = [int]$datePart.Substring(0, 4)
            $month = [int]$datePart.Substring(4, 2)
            $day = [int]$datePart.Substring(6, 2)
            
            $year | Should -BeGreaterThan 2019
            $month | Should -BeGreaterThan 0
            $month | Should -BeLessThan 13
            $day | Should -BeGreaterThan 0
            $day | Should -BeLessThan 32
        }
    }
    
    Context "Plan content determinism" {
        
        It "Should produce identical plan for same inputs" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            # Fixed inputs for determinism
            $fixedRunId = "20250101-000000"
            $fixedTimestamp = "2025-01-01T00:00:00Z"
            $installedApps = @("Test.App2")  # App2 is installed
            
            $plan1 = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId $fixedRunId `
                -Timestamp $fixedTimestamp `
                -InstalledApps $installedApps
            
            $plan2 = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId $fixedRunId `
                -Timestamp $fixedTimestamp `
                -InstalledApps $installedApps
            
            # Compare key fields
            $plan1.runId | Should -Be $plan2.runId
            $plan1.timestamp | Should -Be $plan2.timestamp
            $plan1.manifest.hash | Should -Be $plan2.manifest.hash
            $plan1.actions.Count | Should -Be $plan2.actions.Count
            $plan1.summary.install | Should -Be $plan2.summary.install
            $plan1.summary.skip | Should -Be $plan2.summary.skip
        }
        
        It "Should produce stable action list order" {
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
            
            # Actions should be in manifest order: apps first, then restore, then verify
            # Wrap in @() to ensure array even for single results
            $appActions = @($plan.actions | Where-Object { $_.type -eq "app" })
            $restoreActions = @($plan.actions | Where-Object { $_.type -eq "restore" })
            $verifyActions = @($plan.actions | Where-Object { $_.type -eq "verify" })
            
            $appActions.Count | Should -Be 3
            $restoreActions.Count | Should -Be 1
            $verifyActions.Count | Should -Be 1
            
            # Verify order matches manifest
            $appActions[0].id | Should -Be "test-app-1"
            $appActions[1].id | Should -Be "test-app-2"
            $appActions[2].id | Should -Be "test-app-3"
        }
    }
}

Describe "Plan.Structure" {
    
    Context "Plan contains required fields" {
        
        It "Should have runId field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.runId | Should -Not -BeNullOrEmpty
        }
        
        It "Should have timestamp field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.timestamp | Should -Not -BeNullOrEmpty
        }
        
        It "Should have manifest.path field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.manifest.path | Should -Not -BeNullOrEmpty
        }
        
        It "Should have manifest.name field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.manifest.name | Should -Not -BeNullOrEmpty
        }
        
        It "Should have manifest.hash field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.manifest.hash | Should -Not -BeNullOrEmpty
        }
        
        It "Should have actions array" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.actions | Should -Not -BeNullOrEmpty
        }
        
        It "Should have summary.install field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.summary.ContainsKey('install') | Should -Be $true
        }
        
        It "Should have summary.skip field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.summary.ContainsKey('skip') | Should -Be $true
        }
        
        It "Should have summary.restore field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.summary.ContainsKey('restore') | Should -Be $true
        }
        
        It "Should have summary.verify field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $testPlan.summary.ContainsKey('verify') | Should -Be $true
        }
    }
    
    Context "Action structure" {
        
        It "Should have type field on app actions" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $appAction = $testPlan.actions | Where-Object { $_.type -eq "app" } | Select-Object -First 1
            $appAction.type | Should -Be "app"
        }
        
        It "Should have driver field on app actions" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $appAction = $testPlan.actions | Where-Object { $_.type -eq "app" } | Select-Object -First 1
            $appAction.driver | Should -Be "winget"
        }
        
        It "Should have id field on app actions" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $appAction = $testPlan.actions | Where-Object { $_.type -eq "app" } | Select-Object -First 1
            $appAction.id | Should -Not -BeNullOrEmpty
        }
        
        It "Should have ref field on app actions" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $appAction = $testPlan.actions | Where-Object { $_.type -eq "app" } | Select-Object -First 1
            $appAction.ref | Should -Not -BeNullOrEmpty
        }
        
        It "Should have status field on app actions" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $testPlan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @()
            $appAction = $testPlan.actions | Where-Object { $_.type -eq "app" } | Select-Object -First 1
            $appAction.status | Should -Not -BeNullOrEmpty
        }
    }
}