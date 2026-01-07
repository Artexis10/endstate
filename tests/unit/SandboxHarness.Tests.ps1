<#
.SYNOPSIS
    Pester tests for sandbox contract test harness validation.

.DESCRIPTION
    Validates that the sandbox-tests/powertoys-afterburner harness files exist
    and the manifest is valid JSONC.
#>

$script:RepoRoot = Join-Path $PSScriptRoot "..\.."
$script:ManifestScript = Join-Path $script:RepoRoot "engine\manifest.ps1"
$script:SandboxTestDir = Join-Path $script:RepoRoot "sandbox-tests\powertoys-afterburner"

# Load manifest parser
. $script:ManifestScript

Describe "SandboxHarness.FilesExist" {
    
    Context "Harness directory structure" {
        
        It "Should have sandbox-tests/powertoys-afterburner directory" {
            Test-Path $script:SandboxTestDir | Should Be $true
        }
        
        It "Should have manifest.jsonc" {
            $manifestPath = Join-Path $script:SandboxTestDir "manifest.jsonc"
            Test-Path $manifestPath | Should Be $true
        }
        
        It "Should have run.ps1" {
            $runPath = Join-Path $script:SandboxTestDir "run.ps1"
            Test-Path $runPath | Should Be $true
        }
        
        It "Should have README.md" {
            $readmePath = Join-Path $script:SandboxTestDir "README.md"
            Test-Path $readmePath | Should Be $true
        }
        
        It "Should have .wsb file" {
            $wsbPath = Join-Path $script:SandboxTestDir "powertoys-afterburner.wsb"
            Test-Path $wsbPath | Should Be $true
        }
    }
}

Describe "SandboxHarness.ManifestValid" {
    
    Context "Manifest JSONC parsing" {
        
        It "Should parse manifest.jsonc without error" {
            $manifestPath = Join-Path $script:SandboxTestDir "manifest.jsonc"
            { Read-Manifest -Path $manifestPath } | Should Not Throw
        }
        
        It "Should have version field" {
            $manifestPath = Join-Path $script:SandboxTestDir "manifest.jsonc"
            $manifest = Read-Manifest -Path $manifestPath
            $manifest.version | Should Be 1
        }
        
        It "Should have name field" {
            $manifestPath = Join-Path $script:SandboxTestDir "manifest.jsonc"
            $manifest = Read-Manifest -Path $manifestPath
            $manifest.name | Should Be "sandbox-contract-test"
        }
        
        It "Should have modules array" {
            $manifestPath = Join-Path $script:SandboxTestDir "manifest.jsonc"
            $manifest = Read-Manifest -Path $manifestPath
            $manifest.modules | Should Not BeNullOrEmpty
        }
        
        It "Should include powertoys module" {
            $manifestPath = Join-Path $script:SandboxTestDir "manifest.jsonc"
            $manifest = Read-Manifest -Path $manifestPath
            $manifest.modules -contains "powertoys" | Should Be $true
        }
        
        It "Should include msi-afterburner module" {
            $manifestPath = Join-Path $script:SandboxTestDir "manifest.jsonc"
            $manifest = Read-Manifest -Path $manifestPath
            $manifest.modules -contains "msi-afterburner" | Should Be $true
        }
    }
    
    Context "Manifest module resolution" {
        
        It "Should resolve modules to restore entries" {
            $manifestPath = Join-Path $script:SandboxTestDir "manifest.jsonc"
            $manifest = Read-Manifest -Path $manifestPath
            
            # After module resolution, restore array should be populated
            $manifest.restore | Should Not BeNullOrEmpty
        }
        
        It "Should have at least 2 restore entries (one per module minimum)" {
            $manifestPath = Join-Path $script:SandboxTestDir "manifest.jsonc"
            $manifest = Read-Manifest -Path $manifestPath
            
            $manifest.restore.Count | Should BeGreaterThan 1
        }
    }
}

Describe "SandboxHarness.RunScript" {
    
    Context "run.ps1 script validation" {
        
        It "Should have ErrorActionPreference = Stop" {
            $runPath = Join-Path $script:SandboxTestDir "run.ps1"
            $content = Get-Content -Path $runPath -Raw
            $content | Should Match '\$ErrorActionPreference\s*=\s*[''"]Stop[''"]'
        }
        
        It "Should reference bin/endstate.cmd" {
            $runPath = Join-Path $script:SandboxTestDir "run.ps1"
            $content = Get-Content -Path $runPath -Raw
            $content | Should Match 'bin\\endstate\.cmd'
        }
        
        It "Should use -EnableRestore flag" {
            $runPath = Join-Path $script:SandboxTestDir "run.ps1"
            $content = Get-Content -Path $runPath -Raw
            $content | Should Match '-EnableRestore'
        }
        
        It "Should check PowerToys sentinel path" {
            $runPath = Join-Path $script:SandboxTestDir "run.ps1"
            $content = Get-Content -Path $runPath -Raw
            $content | Should Match 'PowerToys.*settings\.json'
        }
        
        It "Should check MSI Afterburner sentinel path" {
            $runPath = Join-Path $script:SandboxTestDir "run.ps1"
            $content = Get-Content -Path $runPath -Raw
            $content | Should Match 'MSI Afterburner.*MSIAfterburner\.cfg'
        }
    }
}
