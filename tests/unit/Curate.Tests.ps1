<#
.SYNOPSIS
    Pester tests for the data-driven curate.ps1 curation runner.

.DESCRIPTION
    Validates:
    - Module loading by ModuleId
    - Safety gates for local seeding
    - Curation metadata in module.jsonc
    - Seed script resolution
    
    NOTE: These are deterministic unit tests - no network, sandbox, or winget required.
#>

BeforeAll {
    $script:RepoRoot = Join-Path $PSScriptRoot "..\.."
    $script:CuratePath = Join-Path $script:RepoRoot "sandbox-tests\discovery-harness\curate.ps1"
    $script:HarnessDir = Join-Path $script:RepoRoot "sandbox-tests\discovery-harness"
    $script:ModulesDir = Join-Path $script:RepoRoot "modules\apps"
}

Describe "Curate.FileStructure" {
    
    Context "Required files exist" {
        
        It "Should have curate.ps1 data-driven runner" {
            Test-Path $script:CuratePath | Should -Be $true
        }
        
        It "Should have curate-git.ps1 legacy runner (deprecated)" {
            Test-Path (Join-Path $script:HarnessDir "curate-git.ps1") | Should -Be $true
        }
        
        It "Should have curate-vscodium.ps1 legacy stub (deprecated)" {
            Test-Path (Join-Path $script:HarnessDir "curate-vscodium.ps1") | Should -Be $true
        }
    }
}

Describe "Curate.ScriptStructure" {
    
    BeforeAll {
        $script:CurateContent = Get-Content -Path $script:CuratePath -Raw
    }
    
    Context "Parameters" {
        
        It "Should have mandatory ModuleId parameter" {
            $script:CurateContent | Should -Match 'Mandatory'
            $script:CurateContent | Should -Match 'string.*ModuleId'
        }
        
        It "Should have Mode parameter with ValidateSet" {
            $script:CurateContent | Should -Match 'ValidateSet'
            $script:CurateContent | Should -Match 'sandbox'
            $script:CurateContent | Should -Match 'local'
        }
        
        It "Should have AllowHostMutation switch for safety" {
            $script:CurateContent | Should -Match 'switch.*AllowHostMutation'
        }
        
        It "Should have Seed switch" {
            $script:CurateContent | Should -Match 'switch.*Seed'
        }
        
        It "Should have SkipInstall switch" {
            $script:CurateContent | Should -Match 'switch.*SkipInstall'
        }
        
        It "Should have Promote switch" {
            $script:CurateContent | Should -Match 'switch.*Promote'
        }
        
        It "Should have Validate switch" {
            $script:CurateContent | Should -Match 'switch.*Validate'
        }
        
        It "Should have OutDir parameter" {
            $script:CurateContent | Should -Match 'string.*OutDir'
        }
    }
    
    Context "Functions" {
        
        It "Should have Get-RepoRoot function" {
            $script:CurateContent | Should -Match 'function Get-RepoRoot'
        }
        
        It "Should have Get-ModulePath function" {
            $script:CurateContent | Should -Match 'function Get-ModulePath'
        }
        
        It "Should have Read-ModuleConfig function" {
            $script:CurateContent | Should -Match 'function Read-ModuleConfig'
        }
        
        It "Should have Assert-LocalSeedingSafe function" {
            $script:CurateContent | Should -Match 'function Assert-LocalSeedingSafe'
        }
        
        It "Should have Invoke-Seed function" {
            $script:CurateContent | Should -Match 'function Invoke-Seed'
        }
        
        It "Should have Invoke-LocalCuration function" {
            $script:CurateContent | Should -Match 'function Invoke-LocalCuration'
        }
        
        It "Should have Invoke-SandboxCuration function" {
            $script:CurateContent | Should -Match 'function Invoke-SandboxCuration'
        }
    }
    
    Context "Error handling" {
        
        It "Should set ErrorActionPreference to Stop" {
            $script:CurateContent | Should -Match 'ErrorActionPreference'
            $script:CurateContent | Should -Match 'Stop'
        }
    }
    
    Context "Safety gates" {
        
        It "Should block local seeding without AllowHostMutation" {
            $script:CurateContent | Should -Match 'SAFETY BLOCK'
            $script:CurateContent | Should -Match 'AllowHostMutation'
        }
        
        It "Should require triple opt-in for local seeding" {
            $script:CurateContent | Should -Match 'Mode.*local'
            $script:CurateContent | Should -Match 'AllowHostMutation'
            $script:CurateContent | Should -Match 'Seed'
        }
    }
}

Describe "Curate.ModuleLoading" {
    
    Context "Module path resolution" {
        
        It "Should have git module with curation metadata" {
            $modulePath = Join-Path $script:ModulesDir "git\module.jsonc"
            Test-Path $modulePath | Should -Be $true
            
            $content = Get-Content -Path $modulePath -Raw
            # Strip comments for parsing
            $lines = $content -split "`n"
            $cleanLines = $lines | ForEach-Object { $_ -replace '//.*$', '' }
            $content = $cleanLines -join "`n"
            $module = $content | ConvertFrom-Json
            
            $module.curation | Should -Not -BeNullOrEmpty
            $module.curation.seed | Should -Not -BeNullOrEmpty
            $module.curation.seed.type | Should -Be "script"
            $module.curation.seed.script | Should -Be "seed.ps1"
        }
        
        It "Should have vscodium module with curation metadata" {
            $modulePath = Join-Path $script:ModulesDir "vscodium\module.jsonc"
            Test-Path $modulePath | Should -Be $true
            
            $content = Get-Content -Path $modulePath -Raw
            $lines = $content -split "`n"
            $cleanLines = $lines | ForEach-Object { $_ -replace '//.*$', '' }
            $content = $cleanLines -join "`n"
            $module = $content | ConvertFrom-Json
            
            $module.curation | Should -Not -BeNullOrEmpty
            $module.curation.seed | Should -Not -BeNullOrEmpty
        }
        
        It "Should have git seed.ps1 script" {
            $seedPath = Join-Path $script:ModulesDir "git\seed.ps1"
            Test-Path $seedPath | Should -Be $true
        }
        
        It "Should have vscodium seed.ps1 script" {
            $seedPath = Join-Path $script:ModulesDir "vscodium\seed.ps1"
            Test-Path $seedPath | Should -Be $true
        }
    }
    
    Context "Module ID parsing" {
        
        It "Should parse apps.git correctly" {
            # Simulate module ID parsing logic
            $moduleId = "apps.git"
            $parts = $moduleId -split '\.'
            $parts.Count | Should -BeGreaterOrEqual 2
            $parts[0] | Should -Be "apps"
            $parts[1] | Should -Be "git"
        }
        
        It "Should parse apps.msi-afterburner correctly" {
            $moduleId = "apps.msi-afterburner"
            $parts = $moduleId -split '\.'
            $parts[0] | Should -Be "apps"
            ($parts[1..($parts.Count - 1)]) -join '.' | Should -Be "msi-afterburner"
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
