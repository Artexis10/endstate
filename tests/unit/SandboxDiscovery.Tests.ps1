<#
.SYNOPSIS
    Pester tests for sandbox discovery harness validation.

.DESCRIPTION
    Validates that the sandbox-tests/discovery-harness files exist
    and scripts/sandbox-discovery.ps1 has required structure.
    
    NOTE: Does NOT run Windows Sandbox - only validates file structure and syntax.
#>

BeforeAll {
    $script:RepoRoot = Join-Path $PSScriptRoot "..\.."
    $script:DiscoveryScript = Join-Path $script:RepoRoot "scripts\sandbox-discovery.ps1"
    $script:HarnessDir = Join-Path $script:RepoRoot "sandbox-tests\discovery-harness"
    $script:SnapshotModule = Join-Path $script:RepoRoot "engine\snapshot.ps1"
}

Describe "SandboxDiscovery.FilesExist" {
    
    Context "Discovery harness directory structure" {
        
        It "Should have sandbox-tests/discovery-harness directory" {
            Test-Path $script:HarnessDir | Should -Be $true
        }
        
        It "Should have sandbox-install.ps1" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            Test-Path $installScript | Should -Be $true
        }
    }
    
    Context "Main discovery script" {
        
        It "Should have scripts/sandbox-discovery.ps1" {
            Test-Path $script:DiscoveryScript | Should -Be $true
        }
        
        It "Should have engine/snapshot.ps1" {
            Test-Path $script:SnapshotModule | Should -Be $true
        }
    }
}

Describe "SandboxDiscovery.ScriptValidation" {
    
    Context "sandbox-discovery.ps1 structure" {
        
        It "Should have WingetId parameter" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\$WingetId'
            $content | Should -Match '\[string\]\$WingetId'
        }
        
        It "Should have OutDir parameter" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\$OutDir'
        }
        
        It "Should have DryRun parameter" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\[switch\]\$DryRun'
        }
        
        It "Should have WriteModule parameter" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\[switch\]\$WriteModule'
        }
        
        It "Should have synopsis documentation" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\.SYNOPSIS'
        }
        
        It "Should have example documentation" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\.EXAMPLE'
        }
        
        It "Should load snapshot module" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match 'snapshot\.ps1'
        }
        
        It "Should generate .wsb file" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\.wsb'
        }
        
        It "Should reference winget install" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match 'winget'
        }
    }
    
    Context "sandbox-install.ps1 structure" {
        
        It "Should have WingetId parameter" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\$WingetId'
        }
        
        It "Should have OutputDir parameter" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\$OutputDir'
        }
        
        It "Should have DryRun parameter" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\$DryRun'
        }
        
        It "Should load snapshot module" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'snapshot\.ps1'
        }
        
        It "Should call Get-FilesystemSnapshot" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'Get-FilesystemSnapshot'
        }
        
        It "Should call Compare-FilesystemSnapshots" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'Compare-FilesystemSnapshots'
        }
        
        It "Should output pre.json" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'pre\.json'
        }
        
        It "Should output post.json" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'post\.json'
        }
        
        It "Should output diff.json" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'diff\.json'
        }
    }
}

Describe "SandboxDiscovery.SnapshotModule" {
    
    Context "Required functions exist" {
        
        BeforeAll {
            . $script:SnapshotModule
        }
        
        It "Should export Get-FilesystemSnapshot" {
            Get-Command -Name Get-FilesystemSnapshot -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export Compare-FilesystemSnapshots" {
            Get-Command -Name Compare-FilesystemSnapshots -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export Apply-ExcludeHeuristics" {
            Get-Command -Name Apply-ExcludeHeuristics -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export ConvertTo-LogicalToken" {
            Get-Command -Name ConvertTo-LogicalToken -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export Test-PathMatchesExcludePattern" {
            Get-Command -Name Test-PathMatchesExcludePattern -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export Get-ExcludePatterns" {
            Get-Command -Name Get-ExcludePatterns -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
    }
}
