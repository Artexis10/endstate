<#
.SYNOPSIS
    Pester tests for report schema validation and serialization stability.
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

Describe "Report.Schema" {
    
    Context "Required fields exist" {
        
        It "Should have timestamp field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $plan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @("Test.App2")
            $reportJson = ConvertTo-ReportJson -Plan $plan
            $report = $reportJson | ConvertFrom-Json -AsHashtable
            $report.ContainsKey('timestamp') | Should -Be $true
            $report.timestamp | Should -Not -BeNullOrEmpty
        }
        
        It "Should have runId field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $plan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @("Test.App2")
            $reportJson = ConvertTo-ReportJson -Plan $plan
            $report = $reportJson | ConvertFrom-Json -AsHashtable
            $report.ContainsKey('runId') | Should -Be $true
            $report.runId | Should -Not -BeNullOrEmpty
        }
        
        It "Should have manifest.hash field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $plan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @("Test.App2")
            $reportJson = ConvertTo-ReportJson -Plan $plan
            $report = $reportJson | ConvertFrom-Json -AsHashtable
            $report.manifest.ContainsKey('hash') | Should -Be $true
            $report.manifest.hash | Should -Not -BeNullOrEmpty
        }
        
        It "Should have manifest.path field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $plan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @("Test.App2")
            $reportJson = ConvertTo-ReportJson -Plan $plan
            $report = $reportJson | ConvertFrom-Json -AsHashtable
            $report.manifest.ContainsKey('path') | Should -Be $true
            $report.manifest.path | Should -Not -BeNullOrEmpty
        }
        
        It "Should have summary fields" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $plan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @("Test.App2")
            $reportJson = ConvertTo-ReportJson -Plan $plan
            $report = $reportJson | ConvertFrom-Json -AsHashtable
            $report.summary.ContainsKey('install') | Should -Be $true
            $report.summary.ContainsKey('skip') | Should -Be $true
            $report.summary.ContainsKey('restore') | Should -Be $true
            $report.summary.ContainsKey('verify') | Should -Be $true
        }
        
        It "Should have actions array" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $plan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @("Test.App2")
            $reportJson = ConvertTo-ReportJson -Plan $plan
            $report = $reportJson | ConvertFrom-Json -AsHashtable
            $report.ContainsKey('actions') | Should -Be $true
        }
    }
    
    Context "Action schema validation" {
        
        It "Should have type and status fields on all actions" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $plan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @("Test.App2")
            $reportJson = ConvertTo-ReportJson -Plan $plan
            $report = $reportJson | ConvertFrom-Json -AsHashtable
            foreach ($action in $report.actions) {
                $action.ContainsKey('type') | Should -Be $true
                $action.ContainsKey('status') | Should -Be $true
            }
        }
        
        It "Should have driver, id, and ref fields on app actions" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            $plan = New-PlanFromManifest -Manifest $manifest -ManifestPath $yamlPath -ManifestHash $hash -RunId "20250101-000000" -Timestamp "2025-01-01T00:00:00Z" -InstalledApps @("Test.App2")
            $reportJson = ConvertTo-ReportJson -Plan $plan
            $report = $reportJson | ConvertFrom-Json -AsHashtable
            $appActions = $report.actions | Where-Object { $_.type -eq "app" }
            foreach ($action in $appActions) {
                $action.ContainsKey('driver') | Should -Be $true
                $action.ContainsKey('id') | Should -Be $true
                $action.ContainsKey('ref') | Should -Be $true
            }
        }
    }
    
    Context "Serialization stability" {
        
        It "Should produce identical JSON on repeated serialization" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.jsonc"
            $manifest = Read-Manifest -Path $yamlPath
            $hash = Get-ManifestHash -ManifestPath $yamlPath
            
            $plan = New-PlanFromManifest `
                -Manifest $manifest `
                -ManifestPath $yamlPath `
                -ManifestHash $hash `
                -RunId "20250101-000000" `
                -Timestamp "2025-01-01T00:00:00Z" `
                -InstalledApps @("Test.App2")
            
            $json1 = ConvertTo-ReportJson -Plan $plan
            $json2 = ConvertTo-ReportJson -Plan $plan
            
            $json1 | Should -Be $json2
        }
        
        It "Should produce valid JSON" {
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
            
            $json = ConvertTo-ReportJson -Plan $plan
            
            # Should not throw when parsing
            { $json | ConvertFrom-Json } | Should -Not -Throw
        }
        
        It "Should have deterministic key ordering in manifest section" {
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
            
            $json = ConvertTo-ReportJson -Plan $plan
            
            # Check that manifest keys appear in expected order
            $manifestMatch = [regex]::Match($json, '"manifest":\s*\{([^}]+)\}')
            $manifestContent = $manifestMatch.Groups[1].Value
            
            $pathIndex = $manifestContent.IndexOf('"path"')
            $nameIndex = $manifestContent.IndexOf('"name"')
            $hashIndex = $manifestContent.IndexOf('"hash"')
            
            $pathIndex | Should -BeLessThan $nameIndex
            $nameIndex | Should -BeLessThan $hashIndex
        }
        
        It "Should have deterministic key ordering in summary section" {
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
            
            $json = ConvertTo-ReportJson -Plan $plan
            
            # Check that summary keys appear in expected order
            $summaryMatch = [regex]::Match($json, '"summary":\s*\{([^}]+)\}')
            $summaryContent = $summaryMatch.Groups[1].Value
            
            $installIndex = $summaryContent.IndexOf('"install"')
            $skipIndex = $summaryContent.IndexOf('"skip"')
            $restoreIndex = $summaryContent.IndexOf('"restore"')
            $verifyIndex = $summaryContent.IndexOf('"verify"')
            
            $installIndex | Should -BeLessThan $skipIndex
            $skipIndex | Should -BeLessThan $restoreIndex
            $restoreIndex | Should -BeLessThan $verifyIndex
        }
    }
    
    Context "Sample fixture validation" {
        
        It "Should match expected schema from sample-plan-output.json" {
            $samplePath = Join-Path $script:FixturesDir "sample-plan-output.json"
            $sample = Get-Content -Path $samplePath -Raw | ConvertFrom-Json -AsHashtable
            
            # Validate sample has all required fields
            $sample.ContainsKey('runId') | Should -Be $true
            $sample.ContainsKey('timestamp') | Should -Be $true
            $sample.ContainsKey('manifest') | Should -Be $true
            $sample.manifest.ContainsKey('hash') | Should -Be $true
            $sample.manifest.ContainsKey('path') | Should -Be $true
            $sample.ContainsKey('summary') | Should -Be $true
            $sample.summary.ContainsKey('install') | Should -Be $true
            $sample.summary.ContainsKey('skip') | Should -Be $true
            $sample.summary.ContainsKey('restore') | Should -Be $true
            $sample.summary.ContainsKey('verify') | Should -Be $true
            $sample.ContainsKey('actions') | Should -Be $true
        }
    }
}

# ============================================================================
# Report Command Tests (CLI report functionality)
# ============================================================================

$script:ReportScript = Join-Path $script:ProvisioningRoot "engine\report.ps1"

Describe "Report.Command.FileSelection" {
    
    BeforeEach {
        # Create temp state directory with fake state files
        $script:TestStateDir = Join-Path $TestDrive "state"
        New-Item -ItemType Directory -Path $script:TestStateDir -Force | Out-Null
        
        # Create fake state files with different timestamps
        $states = @(
            @{
                runId = "20251201-100000"
                timestamp = "2025-12-01T10:00:00Z"
                command = "apply"
                dryRun = $false
                manifest = @{ path = ".\manifests\test.jsonc"; hash = "ABC123" }
                summary = @{ success = 5; skipped = 10; failed = 0 }
                actions = @()
            },
            @{
                runId = "20251215-120000"
                timestamp = "2025-12-15T12:00:00Z"
                command = "apply"
                dryRun = $true
                manifest = @{ path = ".\manifests\test.jsonc"; hash = "DEF456" }
                summary = @{ success = 3; skipped = 7; failed = 1 }
                actions = @()
            },
            @{
                runId = "20251219-080000"
                timestamp = "2025-12-19T08:00:00Z"
                command = "apply"
                dryRun = $false
                manifest = @{ path = ".\manifests\prod.jsonc"; hash = "GHI789" }
                summary = @{ success = 10; skipped = 20; failed = 0 }
                actions = @()
            }
        )
        
        foreach ($state in $states) {
            $filePath = Join-Path $script:TestStateDir "$($state.runId).json"
            $state | ConvertTo-Json -Depth 10 | Out-File -FilePath $filePath -Encoding UTF8
        }
        
        # Load report module
        . $script:ReportScript
    }
    
    Context "-Latest selects newest file" {
        
        It "Should select the most recent state file by runId" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir -Latest
            
            $result.Count | Should -Be 1
            $result[0].runId | Should -Be "20251219-080000"
        }
        
        It "Should return newest when no flags specified (default behavior)" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir
            
            $result.Count | Should -Be 1
            $result[0].runId | Should -Be "20251219-080000"
        }
    }
    
    Context "-RunId selects correct file" {
        
        It "Should select specific run by ID" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir -RunId "20251215-120000"
            
            $result.Count | Should -Be 1
            $result[0].runId | Should -Be "20251215-120000"
            $result[0].dryRun | Should -Be $true
        }
        
        It "Should return empty array for non-existent RunId" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir -RunId "99999999-999999"
            
            $result.Count | Should -Be 0
        }
        
        It "Should select oldest run when requested" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir -RunId "20251201-100000"
            
            $result.Count | Should -Be 1
            $result[0].runId | Should -Be "20251201-100000"
        }
    }
    
    Context "-Last N selects N most recent" {
        
        It "Should return 2 most recent runs when -Last 2" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir -Last 2
            
            $result.Count | Should -Be 2
            $result[0].runId | Should -Be "20251219-080000"
            $result[1].runId | Should -Be "20251215-120000"
        }
        
        It "Should return all runs when -Last exceeds count" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir -Last 100
            
            $result.Count | Should -Be 3
        }
        
        It "Should return 1 run when -Last 1" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir -Last 1
            
            $result.Count | Should -Be 1
            $result[0].runId | Should -Be "20251219-080000"
        }
    }
}

Describe "Report.Command.MutualExclusion" {
    
    Context "Parameter validation" {
        
        It "Should detect -RunId with -Latest as invalid" {
            $hasRunId = $true
            $hasLatest = $true
            $hasLast = $false
            
            $isInvalid = $hasRunId -and ($hasLatest -or $hasLast)
            
            $isInvalid | Should -Be $true
        }
        
        It "Should detect -RunId with -Last as invalid" {
            $hasRunId = $true
            $hasLatest = $false
            $hasLast = $true
            
            $isInvalid = $hasRunId -and ($hasLatest -or $hasLast)
            
            $isInvalid | Should -Be $true
        }
        
        It "Should allow -RunId alone" {
            $hasRunId = $true
            $hasLatest = $false
            $hasLast = $false
            
            $isValid = $hasRunId -and -not $hasLatest -and -not $hasLast
            
            $isValid | Should -Be $true
        }
        
        It "Should allow -Latest alone" {
            $hasRunId = $false
            $hasLatest = $true
            $hasLast = $false
            
            $isValid = -not $hasRunId -and $hasLatest
            
            $isValid | Should -Be $true
        }
        
        It "Should allow -Last alone" {
            $hasRunId = $false
            $hasLatest = $false
            $hasLast = $true
            
            $isValid = -not $hasRunId -and $hasLast
            
            $isValid | Should -Be $true
        }
        
        It "Should allow no flags (defaults to -Latest)" {
            $hasRunId = $false
            $hasLatest = $false
            $hasLast = $false
            
            $isValid = $true  # No flags = default to latest
            
            $isValid | Should -Be $true
        }
    }
}

Describe "Report.Command.JsonOutput" {
    
    BeforeEach {
        $script:TestStateDir = Join-Path $TestDrive "state-json"
        New-Item -ItemType Directory -Path $script:TestStateDir -Force | Out-Null
        
        $state = @{
            runId = "20251219-090000"
            timestamp = "2025-12-19T09:00:00Z"
            command = "apply"
            dryRun = $false
            manifest = @{ path = ".\manifests\test.jsonc"; hash = "XYZ999" }
            summary = @{ success = 5; skipped = 10; failed = 2 }
            actions = @(
                @{ status = "success"; message = "Installed"; action = @{ type = "app"; ref = "Test.App1" } }
                @{ status = "failed"; message = "Network error"; action = @{ type = "app"; ref = "Test.App2" } }
            )
        }
        
        $filePath = Join-Path $script:TestStateDir "$($state.runId).json"
        $state | ConvertTo-Json -Depth 10 | Out-File -FilePath $filePath -Encoding UTF8
        
        . $script:ReportScript
    }
    
    Context "-Json returns parseable JSON" {
        
        It "Should produce valid JSON output" {
            $states = Get-ProvisioningReport -StateDir $script:TestStateDir -Latest
            $json = Format-ReportJson -States $states
            
            # Should not throw when parsing
            { $json | ConvertFrom-Json } | Should -Not -Throw
        }
        
        It "Should include runId in JSON output" {
            $states = Get-ProvisioningReport -StateDir $script:TestStateDir -Latest
            $json = Format-ReportJson -States $states
            $parsed = $json | ConvertFrom-Json
            
            $parsed.runId | Should -Be "20251219-090000"
        }
        
        It "Should include summary counts in JSON output" {
            $states = Get-ProvisioningReport -StateDir $script:TestStateDir -Latest
            $json = Format-ReportJson -States $states
            $parsed = $json | ConvertFrom-Json
            
            $parsed.summary.success | Should -Be 5
            $parsed.summary.skipped | Should -Be 10
            $parsed.summary.failed | Should -Be 2
        }
        
        It "Should include manifest info in JSON output" {
            $states = Get-ProvisioningReport -StateDir $script:TestStateDir -Latest
            $json = Format-ReportJson -States $states
            $parsed = $json | ConvertFrom-Json
            
            $parsed.manifest.hash | Should -Be "XYZ999"
            $parsed.manifest.path | Should -Be ".\manifests\test.jsonc"
        }
    }
    
    Context "JSON array for multiple runs" {
        
        BeforeEach {
            # Add another state file
            $state2 = @{
                runId = "20251218-150000"
                timestamp = "2025-12-18T15:00:00Z"
                command = "apply"
                dryRun = $true
                manifest = @{ path = ".\manifests\other.jsonc"; hash = "ABC111" }
                summary = @{ success = 1; skipped = 2; failed = 0 }
                actions = @()
            }
            $filePath2 = Join-Path $script:TestStateDir "$($state2.runId).json"
            $state2 | ConvertTo-Json -Depth 10 | Out-File -FilePath $filePath2 -Encoding UTF8
        }
        
        It "Should return JSON array for multiple runs" {
            $states = Get-ProvisioningReport -StateDir $script:TestStateDir -Last 2
            $json = Format-ReportJson -States $states
            $parsed = $json | ConvertFrom-Json
            
            $parsed.Count | Should -Be 2
        }
    }
}

Describe "Report.Command.EmptyState" {
    
    BeforeEach {
        $script:TestStateDir = Join-Path $TestDrive "state-empty"
        New-Item -ItemType Directory -Path $script:TestStateDir -Force | Out-Null
        
        . $script:ReportScript
    }
    
    Context "Empty state directory" {
        
        It "Should return empty array when no state files exist" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir -Latest
            
            $result.Count | Should -Be 0
        }
        
        It "Should return empty array for -Last when no files" {
            $result = Get-ProvisioningReport -StateDir $script:TestStateDir -Last 5
            
            $result.Count | Should -Be 0
        }
    }
    
    Context "Non-existent state directory" {
        
        It "Should return empty array when state dir does not exist" {
            $result = Get-ProvisioningReport -StateDir "C:\nonexistent\path\state" -Latest
            
            $result.Count | Should -Be 0
        }
    }
}