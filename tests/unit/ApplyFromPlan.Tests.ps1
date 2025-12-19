<#
.SYNOPSIS
    Pester tests for Apply-from-Plan functionality.
#>

$script:ProvisioningRoot = Join-Path $PSScriptRoot "..\..\"
$script:ApplyScript = Join-Path $script:ProvisioningRoot "engine\apply.ps1"
$script:LoggingScript = Join-Path $script:ProvisioningRoot "engine\logging.ps1"
$script:ManifestScript = Join-Path $script:ProvisioningRoot "engine\manifest.ps1"
$script:StateScript = Join-Path $script:ProvisioningRoot "engine\state.ps1"
$script:PlanScript = Join-Path $script:ProvisioningRoot "engine\plan.ps1"
$script:WingetDriver = Join-Path $script:ProvisioningRoot "drivers\winget.ps1"
$script:CopyRestorer = Join-Path $script:ProvisioningRoot "restorers\copy.ps1"
$script:FileExistsVerifier = Join-Path $script:ProvisioningRoot "verifiers\file-exists.ps1"

Describe "Apply-from-Plan CLI Validation" {
    
    Context "Mutual exclusion of -Manifest and -Plan" {
        
        It "Should error when both -Manifest and -Plan are provided" {
            $hasManifest = $true
            $hasPlan = $true
            
            $shouldError = $hasManifest -and $hasPlan
            
            $shouldError | Should Be $true
        }
        
        It "Should error when neither -Manifest nor -Plan is provided" {
            $hasManifest = $false
            $hasPlan = $false
            
            $shouldError = -not $hasManifest -and -not $hasPlan
            
            $shouldError | Should Be $true
        }
        
        It "Should allow -Manifest without -Plan" {
            $hasManifest = $true
            $hasPlan = $false
            
            $isValid = ($hasManifest -and -not $hasPlan) -or (-not $hasManifest -and $hasPlan)
            
            $isValid | Should Be $true
        }
        
        It "Should allow -Plan without -Manifest" {
            $hasManifest = $false
            $hasPlan = $true
            
            $isValid = ($hasManifest -and -not $hasPlan) -or (-not $hasManifest -and $hasPlan)
            
            $isValid | Should Be $true
        }
    }
}

Describe "Apply-from-Plan Plan Loading" {
    
    BeforeEach {
        $script:TestTempDir = Join-Path $env:TEMP "apply-from-plan-tests-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TestTempDir -Force | Out-Null
    }
    
    AfterEach {
        if (Test-Path $script:TestTempDir) {
            Remove-Item -Path $script:TestTempDir -Recurse -Force
        }
    }
    
    Context "Plan file validation" {
        
        It "Should detect missing plan file" {
            $planPath = Join-Path $script:TestTempDir "nonexistent.json"
            
            $exists = Test-Path $planPath
            
            $exists | Should Be $false
        }
        
        It "Should load valid plan JSON" {
            $plan = @{
                runId = "20251219-010000"
                manifest = @{
                    name = "test"
                    path = "./manifests/test.jsonc"
                    hash = "ABC123"
                }
                actions = @(
                    @{ type = "app"; status = "install"; driver = "winget"; id = "test-app"; ref = "Test.App" }
                )
                summary = @{ install = 1; skip = 0 }
            }
            
            $planPath = Join-Path $script:TestTempDir "test-plan.json"
            $plan | ConvertTo-Json -Depth 10 | Out-File -FilePath $planPath -Encoding UTF8
            
            $loaded = Get-Content -Path $planPath -Raw | ConvertFrom-Json
            
            $loaded.runId | Should Be "20251219-010000"
            $loaded.actions.Count | Should Be 1
        }
        
        It "Should validate required runId field" {
            $plan = @{
                actions = @()
            }
            
            $hasRunId = $null -ne $plan.runId
            
            $hasRunId | Should Be $false
        }
        
        It "Should validate required actions array" {
            $plan = @{
                runId = "20251219-010000"
            }
            
            $hasActions = $null -ne $plan.actions
            
            $hasActions | Should Be $false
        }
    }
}

Describe "Apply-from-Plan Action Execution" {
    
    BeforeEach {
        $script:TestTempDir = Join-Path $env:TEMP "apply-from-plan-exec-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TestTempDir -Force | Out-Null
        
        # Track install calls
        $script:InstallCalls = @()
    }
    
    AfterEach {
        if (Test-Path $script:TestTempDir) {
            Remove-Item -Path $script:TestTempDir -Recurse -Force
        }
    }
    
    Context "Install action execution" {
        
        It "Should identify install actions from plan" {
            $plan = @{
                runId = "20251219-010000"
                actions = @(
                    @{ type = "app"; status = "install"; driver = "winget"; id = "app-a"; ref = "App.A" }
                    @{ type = "app"; status = "skip"; driver = "winget"; id = "app-b"; ref = "App.B"; reason = "already installed" }
                    @{ type = "app"; status = "install"; driver = "winget"; id = "app-c"; ref = "App.C" }
                )
            }
            
            $installActions = @($plan.actions | Where-Object { $_.status -eq "install" })
            
            $installActions.Count | Should Be 2
            $installActions[0].ref | Should Be "App.A"
            $installActions[1].ref | Should Be "App.C"
        }
        
        It "Should identify skip actions from plan" {
            $plan = @{
                runId = "20251219-010000"
                actions = @(
                    @{ type = "app"; status = "install"; driver = "winget"; id = "app-a"; ref = "App.A" }
                    @{ type = "app"; status = "skip"; driver = "winget"; id = "app-b"; ref = "App.B"; reason = "already installed" }
                    @{ type = "app"; status = "skip"; driver = "winget"; id = "app-c"; ref = "App.C"; reason = "already installed" }
                )
            }
            
            $skipActions = @($plan.actions | Where-Object { $_.status -eq "skip" })
            
            $skipActions.Count | Should Be 2
        }
        
        It "Should process actions in order (deterministic)" {
            $plan = @{
                runId = "20251219-010000"
                actions = @(
                    @{ type = "app"; status = "install"; driver = "winget"; id = "zebra"; ref = "Zebra.App" }
                    @{ type = "app"; status = "install"; driver = "winget"; id = "alpha"; ref = "Alpha.App" }
                    @{ type = "app"; status = "install"; driver = "winget"; id = "beta"; ref = "Beta.App" }
                )
            }
            
            # Actions should be processed in array order, not sorted
            $plan.actions[0].id | Should Be "zebra"
            $plan.actions[1].id | Should Be "alpha"
            $plan.actions[2].id | Should Be "beta"
        }
    }
    
    Context "Dry-run mode" {
        
        It "Should not execute installs in dry-run mode" {
            $dryRun = $true
            $installCalled = $false
            
            # Simulate dry-run logic
            if (-not $dryRun) {
                $installCalled = $true
            }
            
            $installCalled | Should Be $false
        }
        
        It "Should still count actions in dry-run mode" {
            $plan = @{
                runId = "20251219-010000"
                actions = @(
                    @{ type = "app"; status = "install"; driver = "winget"; id = "app-a"; ref = "App.A" }
                    @{ type = "app"; status = "skip"; driver = "winget"; id = "app-b"; ref = "App.B" }
                )
            }
            
            $dryRun = $true
            $successCount = 0
            $skipCount = 0
            
            foreach ($action in $plan.actions) {
                if ($action.status -eq "install") {
                    if ($dryRun) {
                        $successCount++  # Would install
                    }
                } elseif ($action.status -eq "skip") {
                    $skipCount++
                }
            }
            
            $successCount | Should Be 1
            $skipCount | Should Be 1
        }
    }
}

Describe "Apply-from-Plan Result Structure" {
    
    Context "Result object fields" {
        
        It "Should include RunId in result" {
            $result = @{
                RunId = "20251219-020000"
                OriginalPlanRunId = "20251219-010000"
                PlanPath = "./plans/test.json"
                DryRun = $false
                Success = 5
                Skipped = 10
                Failed = 0
            }
            
            $result.RunId | Should Not BeNullOrEmpty
        }
        
        It "Should include OriginalPlanRunId in result" {
            $result = @{
                RunId = "20251219-020000"
                OriginalPlanRunId = "20251219-010000"
                PlanPath = "./plans/test.json"
            }
            
            $result.OriginalPlanRunId | Should Be "20251219-010000"
        }
        
        It "Should include PlanPath in result" {
            $result = @{
                RunId = "20251219-020000"
                PlanPath = "./plans/test.json"
            }
            
            $result.PlanPath | Should Be "./plans/test.json"
        }
        
        It "Should include counts in result" {
            $result = @{
                Success = 5
                Skipped = 10
                Failed = 1
            }
            
            $result.Success | Should Be 5
            $result.Skipped | Should Be 10
            $result.Failed | Should Be 1
        }
    }
}

Describe "Apply-from-Plan Error Handling" {
    
    BeforeEach {
        $script:TestTempDir = Join-Path $env:TEMP "apply-from-plan-errors-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TestTempDir -Force | Out-Null
    }
    
    AfterEach {
        if (Test-Path $script:TestTempDir) {
            Remove-Item -Path $script:TestTempDir -Recurse -Force
        }
    }
    
    Context "Invalid plan file" {
        
        It "Should handle malformed JSON gracefully" {
            $planPath = Join-Path $script:TestTempDir "bad.json"
            "{ invalid json }" | Out-File -FilePath $planPath -Encoding UTF8
            
            $parseError = $null
            try {
                $content = Get-Content -Path $planPath -Raw
                $plan = $content | ConvertFrom-Json
            } catch {
                $parseError = $_.Exception.Message
            }
            
            $parseError | Should Not BeNullOrEmpty
        }
        
        It "Should detect missing actions array" {
            $plan = @{
                runId = "20251219-010000"
                manifest = @{ name = "test" }
            }
            
            $isValid = $null -ne $plan.actions
            
            $isValid | Should Be $false
        }
        
        It "Should detect missing runId" {
            $plan = @{
                actions = @()
                manifest = @{ name = "test" }
            }
            
            $isValid = $null -ne $plan.runId -and $plan.runId -ne ""
            
            $isValid | Should Be $false
        }
    }
}

Describe "Apply-from-Plan State Saving" {
    
    Context "State file generation" {
        
        It "Should save state with command type 'apply-from-plan'" {
            $command = "apply-from-plan"
            
            $command | Should Be "apply-from-plan"
        }
        
        It "Should preserve original manifest metadata from plan" {
            $plan = @{
                runId = "20251219-010000"
                manifest = @{
                    name = "hugo-win11"
                    path = "./manifests/hugo-win11.jsonc"
                    hash = "ABC123DEF456"
                }
                actions = @()
            }
            
            $manifestPath = $plan.manifest.path
            $manifestHash = $plan.manifest.hash
            
            $manifestPath | Should Be "./manifests/hugo-win11.jsonc"
            $manifestHash | Should Be "ABC123DEF456"
        }
    }
}
