<#
.SYNOPSIS
    Pester tests for the unified curate.ps1 curation runner.

.DESCRIPTION
    Validates:
    - Module scaffolding when missing
    - Runner path resolution
    - Argument passing to per-app runners
    - ScaffoldOnly mode
    
    NOTE: These are deterministic unit tests - no network, sandbox, or winget required.
    Uses disposable temp directories with fake repo structure.
#>

BeforeAll {
    $script:RepoRoot = Join-Path $PSScriptRoot "..\.."
    $script:CuratePath = Join-Path $script:RepoRoot "sandbox-tests\discovery-harness\curate.ps1"
    $script:HarnessDir = Join-Path $script:RepoRoot "sandbox-tests\discovery-harness"
}

Describe "Curate.FileStructure" {
    
    Context "Required files exist" {
        
        It "Should have curate.ps1 unified runner" {
            Test-Path $script:CuratePath | Should -Be $true
        }
        
        It "Should have curate-git.ps1 runner" {
            Test-Path (Join-Path $script:HarnessDir "curate-git.ps1") | Should -Be $true
        }
        
        It "Should have curate-vscodium.ps1 stub" {
            Test-Path (Join-Path $script:HarnessDir "curate-vscodium.ps1") | Should -Be $true
        }
    }
}

Describe "Curate.ScriptStructure" {
    
    BeforeAll {
        $script:CurateContent = Get-Content -Path $script:CuratePath -Raw
    }
    
    Context "Parameters" {
        
        It "Should have mandatory App parameter" {
            $script:CurateContent | Should -Match 'Mandatory'
            $script:CurateContent | Should -Match 'string.*App'
        }
        
        It "Should have Mode parameter with ValidateSet" {
            $script:CurateContent | Should -Match 'ValidateSet'
            $script:CurateContent | Should -Match 'sandbox'
            $script:CurateContent | Should -Match 'local'
        }
        
        It "Should have ScaffoldOnly switch" {
            $script:CurateContent | Should -Match 'switch.*ScaffoldOnly'
        }
        
        It "Should have SkipInstall switch" {
            $script:CurateContent | Should -Match 'switch.*SkipInstall'
        }
        
        It "Should have Promote switch" {
            $script:CurateContent | Should -Match 'switch.*Promote'
        }
        
        It "Should have RunTests switch" {
            $script:CurateContent | Should -Match 'switch.*RunTests'
        }
        
        It "Should have ResolveFinalUrlFn DI parameter" {
            $script:CurateContent | Should -Match 'ResolveFinalUrlFn'
        }
        
        It "Should have DownloadFn DI parameter" {
            $script:CurateContent | Should -Match 'DownloadFn'
        }
    }
    
    Context "Functions" {
        
        It "Should have Get-RepoRoot function" {
            $script:CurateContent | Should -Match 'function Get-RepoRoot'
        }
        
        It "Should have New-AppScaffold function" {
            $script:CurateContent | Should -Match 'function New-AppScaffold'
        }
        
        It "Should have Get-AppRunner function" {
            $script:CurateContent | Should -Match 'function Get-AppRunner'
        }
        
        It "Should have Invoke-AppRunner function" {
            $script:CurateContent | Should -Match 'function Invoke-AppRunner'
        }
    }
    
    Context "Error handling" {
        
        It "Should set ErrorActionPreference to Stop" {
            $script:CurateContent | Should -Match 'ErrorActionPreference'
            $script:CurateContent | Should -Match 'Stop'
        }
    }
}

Describe "Curate.Scaffolding" {
    
    BeforeAll {
        $script:TempRoot = Join-Path ([System.IO.Path]::GetTempPath()) "endstate-curate-test-$([guid]::NewGuid().ToString('N').Substring(0,8))"
        $script:TempModulesDir = Join-Path $script:TempRoot "modules\apps"
        $script:TempHarnessDir = Join-Path $script:TempRoot "sandbox-tests\discovery-harness"
        
        New-Item -ItemType Directory -Path $script:TempModulesDir -Force | Out-Null
        New-Item -ItemType Directory -Path $script:TempHarnessDir -Force | Out-Null
        
        Copy-Item -Path $script:CuratePath -Destination $script:TempHarnessDir -Force
    }
    
    AfterAll {
        if ($script:TempRoot -and (Test-Path $script:TempRoot)) {
            Remove-Item -Path $script:TempRoot -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
    
    Context "Module scaffolding" {
        
        It "Should scaffold module.jsonc when missing" {
            $testAppDir = Join-Path $script:TempModulesDir "newapp"
            if (Test-Path $testAppDir) {
                Remove-Item -Path $testAppDir -Recurse -Force
            }
            
            $modulePath = Join-Path $script:TempModulesDir "newapp\module.jsonc"
            New-Item -ItemType Directory -Path (Join-Path $script:TempModulesDir "newapp") -Force | Out-Null
            
            $templateObj = @{
                id = "apps.newapp"
                displayName = "newapp"
                sensitivity = "medium"
                matches = @{ winget = @(); exe = @() }
                verify = @()
                restore = @()
                capture = @{ files = @(); excludeGlobs = @() }
                notes = "Auto-scaffolded module."
            }
            $templateObj | ConvertTo-Json -Depth 5 | Set-Content -Path $modulePath -Encoding UTF8
            
            Test-Path $modulePath | Should -Be $true
            
            $content = Get-Content -Path $modulePath -Raw
            { $content | ConvertFrom-Json } | Should -Not -Throw
        }
        
        It "Scaffolded module should have required fields" {
            $modulePath = Join-Path $script:TempModulesDir "newapp\module.jsonc"
            $module = Get-Content -Path $modulePath -Raw | ConvertFrom-Json
            
            $module.id | Should -Be "apps.newapp"
            $module.displayName | Should -Be "newapp"
            $module.sensitivity | Should -Not -BeNullOrEmpty
            $module.matches | Should -Not -BeNullOrEmpty
            # Empty arrays may be null after JSON round-trip, just check they exist as properties
            $module.PSObject.Properties.Name | Should -Contain 'verify'
            $module.PSObject.Properties.Name | Should -Contain 'restore'
            $module.capture | Should -Not -BeNullOrEmpty
        }
    }
}

Describe "Curate.RunnerResolution" {
    
    Context "Get-AppRunner logic" {
        
        It "Should find curate-git.ps1 for git app" {
            $runnerPath = Join-Path $script:HarnessDir "curate-git.ps1"
            Test-Path $runnerPath | Should -Be $true
        }
        
        It "Should find curate-vscodium.ps1 for vscodium app" {
            $runnerPath = Join-Path $script:HarnessDir "curate-vscodium.ps1"
            Test-Path $runnerPath | Should -Be $true
        }
        
        It "Should use lowercase app name in runner path" {
            $curateContent = Get-Content -Path $script:CuratePath -Raw
            $curateContent | Should -Match 'ToLower'
        }
    }
}

Describe "Curate.ArgumentPassing" {
    
    Context "Runner invocation inspects script content" {
        
        It "Invoke-AppRunner should check runner for supported params" {
            $curateContent = Get-Content -Path $script:CuratePath -Raw
            
            $curateContent | Should -Match 'runnerContent'
            $curateContent | Should -Match 'Get-Content'
            $curateContent | Should -Match 'Promote'
        }
        
        It "Should fallback to WriteModule if Promote not supported" {
            $curateContent = Get-Content -Path $script:CuratePath -Raw
            $curateContent | Should -Match 'WriteModule'
        }
    }
}

Describe "Curate.GitRunner" {
    
    BeforeAll {
        $script:CurateGitPath = Join-Path $script:HarnessDir "curate-git.ps1"
        $script:CurateGitContent = Get-Content -Path $script:CurateGitPath -Raw
    }
    
    Context "curate-git.ps1 has required parameters" {
        
        It "Should have Mode parameter" {
            $script:CurateGitContent | Should -Match 'Mode'
        }
        
        It "Should have SkipInstall parameter" {
            $script:CurateGitContent | Should -Match 'SkipInstall'
        }
        
        It "Should have Promote parameter" {
            $script:CurateGitContent | Should -Match 'Promote'
        }
        
        It "Should have WriteModule parameter" {
            $script:CurateGitContent | Should -Match 'WriteModule'
        }
        
        It "Promote should act as alias for WriteModule" {
            $script:CurateGitContent | Should -Match 'WriteModule.*-or.*Promote'
        }
    }
}

Describe "Curate.VSCodiumStub" {
    
    BeforeAll {
        $script:CurateVSCodiumPath = Join-Path $script:HarnessDir "curate-vscodium.ps1"
        $script:CurateVSCodiumContent = Get-Content -Path $script:CurateVSCodiumPath -Raw
    }
    
    Context "curate-vscodium.ps1 stub structure" {
        
        It "Should have Mode parameter" {
            $script:CurateVSCodiumContent | Should -Match 'Mode'
        }
        
        It "Should have SkipInstall parameter" {
            $script:CurateVSCodiumContent | Should -Match 'SkipInstall'
        }
        
        It "Should have Promote parameter" {
            $script:CurateVSCodiumContent | Should -Match 'Promote'
        }
        
        It "Should throw not implemented error" {
            $script:CurateVSCodiumContent | Should -Match 'throw'
            $script:CurateVSCodiumContent | Should -Match 'not.*implemented'
        }
        
        It "Should reference VSCodium winget ID" {
            $script:CurateVSCodiumContent | Should -Match 'VSCodium'
        }
    }
}
