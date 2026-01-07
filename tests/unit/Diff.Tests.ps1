<#
.SYNOPSIS
    Pester tests for diff engine.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    $script:DiffScript = Join-Path $script:ProvisioningRoot "engine\diff.ps1"
    $script:FixturesDir = Join-Path $PSScriptRoot "..\fixtures"
    
    # Load dependencies
    . $script:DiffScript
}

Describe "Diff.ActionKey" {
    
    Context "Key generation for different action types" {
        
        It "Should generate key for app action" {
            $action = @{
                type = "app"
                id = "test-app"
                ref = "Test.App"
                driver = "winget"
                status = "install"
            }
            
            $key = Get-ActionKey -Action $action
            $key | Should -Be "app:Test.App"
        }
        
        It "Should generate key for restore action" {
            $action = @{
                type = "restore"
                restoreType = "copy"
                source = "./config.conf"
                target = "~/.config.conf"
                status = "restore"
            }
            
            $key = Get-ActionKey -Action $action
            $key | Should -Be "restore:./config.conf->~/.config.conf"
        }
        
        It "Should generate key for verify action with path" {
            $action = @{
                type = "verify"
                verifyType = "file-exists"
                path = "~/.config.conf"
                status = "verify"
            }
            
            $key = Get-ActionKey -Action $action
            $key | Should -Be "verify:file-exists:~/.config.conf"
        }
        
        It "Should generate key for verify action with command" {
            $action = @{
                type = "verify"
                verifyType = "command-exists"
                command = "git"
                status = "verify"
            }
            
            $key = Get-ActionKey -Action $action
            $key | Should -Be "verify:command-exists:git"
        }
    }
}

Describe "Diff.Compare" {
    
    Context "Identical artifacts" {
        
        It "Should detect identical artifacts" {
            $action1 = @{ type = "app"; ref = "Test.App1"; status = "install" }
            $action2 = @{ type = "app"; ref = "Test.App2"; status = "skip"; reason = "already installed" }
            
            $artifactA = @{
                summary = @{ install = 1; skip = 1; restore = 0; verify = 0 }
                actions = @($action1, $action2)
            }
            
            $artifactB = @{
                summary = @{ install = 1; skip = 1; restore = 0; verify = 0 }
                actions = @($action1, $action2)
            }
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            
            $diff.identical | Should -Be $true
            $diff.actionsAdded.Count | Should -Be 0
            $diff.actionsRemoved.Count | Should -Be 0
            $diff.actionsChanged.Count | Should -Be 0
        }
    }
    
    Context "Added actions" {
        
        It "Should detect added actions" {
            $action1 = @{ type = "app"; ref = "Test.App1"; status = "install" }
            $action2 = @{ type = "app"; ref = "Test.App2"; status = "install" }
            
            $artifactA = @{
                summary = @{ install = 1; skip = 0; restore = 0; verify = 0 }
                actions = @($action1)
            }
            
            $artifactB = @{
                summary = @{ install = 2; skip = 0; restore = 0; verify = 0 }
                actions = @($action1, $action2)
            }
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            
            $diff.identical | Should -Be $false
            $diff.actionsAdded.Count | Should -Be 1
            $diff.actionsAdded[0].key | Should -Be "app:Test.App2"
        }
    }
    
    Context "Removed actions" {
        
        It "Should detect removed actions" {
            $action1 = @{ type = "app"; ref = "Test.App1"; status = "install" }
            $action2 = @{ type = "app"; ref = "Test.App2"; status = "install" }
            
            $artifactA = @{
                summary = @{ install = 2; skip = 0; restore = 0; verify = 0 }
                actions = @($action1, $action2)
            }
            
            $artifactB = @{
                summary = @{ install = 1; skip = 0; restore = 0; verify = 0 }
                actions = @($action1)
            }
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            
            $diff.identical | Should -Be $false
            $diff.actionsRemoved.Count | Should -Be 1
            $diff.actionsRemoved[0].key | Should -Be "app:Test.App2"
        }
    }
    
    Context "Changed actions" {
        
        It "Should detect status changes" {
            $actionA = @{ type = "app"; ref = "Test.App1"; status = "install" }
            $actionB = @{ type = "app"; ref = "Test.App1"; status = "skip"; reason = "already installed" }
            
            $artifactA = @{
                summary = @{ install = 1; skip = 0; restore = 0; verify = 0 }
                actions = @($actionA)
            }
            
            $artifactB = @{
                summary = @{ install = 0; skip = 1; restore = 0; verify = 0 }
                actions = @($actionB)
            }
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            
            $diff.identical | Should -Be $false
            $diff.actionsChanged.Count | Should -Be 1
            $diff.actionsChanged[0].statusA | Should -Be "install"
            $diff.actionsChanged[0].statusB | Should -Be "skip"
        }
    }
    
    Context "Summary extraction" {
        
        It "Should extract summary counts correctly" {
            $artifactA = @{
                summary = @{ install = 2; skip = 3; restore = 1; verify = 4 }
                actions = @()
            }
            
            $artifactB = @{
                summary = @{ install = 1; skip = 4; restore = 2; verify = 3 }
                actions = @()
            }
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            
            $diff.summaryA.install | Should -Be 2
            $diff.summaryA.skip | Should -Be 3
            $diff.summaryA.restore | Should -Be 1
            $diff.summaryA.verify | Should -Be 4
            
            $diff.summaryB.install | Should -Be 1
            $diff.summaryB.skip | Should -Be 4
            $diff.summaryB.restore | Should -Be 2
            $diff.summaryB.verify | Should -Be 3
        }
    }
}

Describe "Diff.FileFixtures" {
    
    Context "Comparing fixture files" {
        
        It "Should read and compare plan-a.json and plan-b.json" {
            $planAPath = Join-Path $script:FixturesDir "plan-a.json"
            $planBPath = Join-Path $script:FixturesDir "plan-b.json"
            
            $artifactA = Read-ArtifactFile -Path $planAPath
            $artifactB = Read-ArtifactFile -Path $planBPath
            
            $artifactA | Should -Not -BeNullOrEmpty
            $artifactB | Should -Not -BeNullOrEmpty
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            
            $diff.identical | Should -Be $false
        }
        
        It "Should detect App1 status changed from install to skip" {
            $planAPath = Join-Path $script:FixturesDir "plan-a.json"
            $planBPath = Join-Path $script:FixturesDir "plan-b.json"
            
            $artifactA = Read-ArtifactFile -Path $planAPath
            $artifactB = Read-ArtifactFile -Path $planBPath
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            
            $app1Change = $diff.actionsChanged | Where-Object { $_.key -eq "app:Test.App1" }
            $app1Change | Should -Not -BeNullOrEmpty
            $app1Change.statusA | Should -Be "install"
            $app1Change.statusB | Should -Be "skip"
        }
        
        It "Should detect App3 was added in plan-b" {
            $planAPath = Join-Path $script:FixturesDir "plan-a.json"
            $planBPath = Join-Path $script:FixturesDir "plan-b.json"
            
            $artifactA = Read-ArtifactFile -Path $planAPath
            $artifactB = Read-ArtifactFile -Path $planBPath
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            
            $app3Added = $diff.actionsAdded | Where-Object { $_.key -eq "app:Test.App3" }
            $app3Added | Should -Not -BeNullOrEmpty
        }
        
        It "Should show correct summary differences" {
            $planAPath = Join-Path $script:FixturesDir "plan-a.json"
            $planBPath = Join-Path $script:FixturesDir "plan-b.json"
            
            $artifactA = Read-ArtifactFile -Path $planAPath
            $artifactB = Read-ArtifactFile -Path $planBPath
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            
            $diff.summaryA.install | Should -Be 1
            $diff.summaryA.skip | Should -Be 1
            $diff.summaryB.install | Should -Be 1
            $diff.summaryB.skip | Should -Be 2
        }
    }
}

Describe "Diff.JsonOutput" {
    
    Context "JSON serialization" {
        
        It "Should produce valid JSON" {
            $actionA = @{ type = "app"; ref = "Test.App1"; status = "install" }
            $actionB = @{ type = "app"; ref = "Test.App1"; status = "skip"; reason = "already installed" }
            
            $artifactA = @{
                summary = @{ install = 1; skip = 0; restore = 0; verify = 0 }
                actions = @($actionA)
            }
            
            $artifactB = @{
                summary = @{ install = 0; skip = 1; restore = 0; verify = 0 }
                actions = @($actionB)
            }
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            $json = ConvertTo-DiffJson -Diff $diff
            
            { $json | ConvertFrom-Json } | Should -Not -Throw
        }
        
        It "Should have deterministic output" {
            $actionA = @{ type = "app"; ref = "Test.App1"; status = "install" }
            $actionB = @{ type = "app"; ref = "Test.App1"; status = "skip"; reason = "already installed" }
            
            $artifactA = @{
                summary = @{ install = 1; skip = 0; restore = 0; verify = 0 }
                actions = @($actionA)
            }
            
            $artifactB = @{
                summary = @{ install = 0; skip = 1; restore = 0; verify = 0 }
                actions = @($actionB)
            }
            
            $diff = Compare-ProvisioningArtifacts -ArtifactA $artifactA -ArtifactB $artifactB
            $json1 = ConvertTo-DiffJson -Diff $diff
            $json2 = ConvertTo-DiffJson -Diff $diff
            
            $json1 | Should -Be $json2
        }
    }
}