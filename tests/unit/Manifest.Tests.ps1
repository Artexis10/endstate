<#
.SYNOPSIS
    Pester tests for manifest parsing (YAML, JSONC, includes).
#>

$script:ProvisioningRoot = Join-Path $PSScriptRoot "..\..\"
$script:ManifestScript = Join-Path $script:ProvisioningRoot "engine\manifest.ps1"
$script:FixturesDir = Join-Path $PSScriptRoot "..\fixtures"

# Load dependencies (Pester 3.x compatible - no BeforeAll at script level)
. $script:ManifestScript

Describe "Manifest.Yaml.Parses" {
    
    Context "Sample YAML manifest parsing" {
        
        It "Should parse sample YAML manifest and return hashtable" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest | Should Not BeNullOrEmpty
            $manifest | Should BeOfType [hashtable]
        }
        
        It "Should have correct version field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.version | Should Be 1
        }
        
        It "Should have correct name field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.name | Should Be "test-manifest"
        }
        
        It "Should have captured timestamp" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.captured | Should Be "2025-01-01T00:00:00Z"
        }
        
        It "Should parse apps array with correct count" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.apps | Should Not BeNullOrEmpty
            $manifest.apps.Count | Should Be 3
        }
        
        It "Should parse app id correctly" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.apps[0].id | Should Be "test-app-1"
        }
        
        It "Should parse app refs.windows correctly" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.apps[0].refs.windows | Should Be "Test.App1"
        }
        
        It "Should parse multi-platform refs" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.apps[1].refs.windows | Should Be "Test.App2"
            $manifest.apps[1].refs.linux | Should Be "test-app-2"
        }
        
        It "Should parse restore array" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.restore | Should Not BeNullOrEmpty
            $manifest.restore.Count | Should Be 1
            $manifest.restore[0].type | Should Be "copy"
        }
        
        It "Should parse verify array" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.verify | Should Not BeNullOrEmpty
            $manifest.verify.Count | Should Be 1
            $manifest.verify[0].type | Should Be "file-exists"
        }
    }
    
    Context "YAML parsing stability" {
        
        It "Should produce identical output on repeated parsing" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            
            $manifest1 = Read-Manifest -Path $yamlPath
            $manifest2 = Read-Manifest -Path $yamlPath
            
            # Compare key fields
            $manifest1.version | Should Be $manifest2.version
            $manifest1.name | Should Be $manifest2.name
            $manifest1.apps.Count | Should Be $manifest2.apps.Count
            
            for ($i = 0; $i -lt $manifest1.apps.Count; $i++) {
                $manifest1.apps[$i].id | Should Be $manifest2.apps[$i].id
                $manifest1.apps[$i].refs.windows | Should Be $manifest2.apps[$i].refs.windows
            }
        }
    }
}

Describe "Manifest.Jsonc.Includes.Parses" {
    
    Context "JSONC manifest with includes" {
        
        It "Should parse JSONC manifest with includes" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $manifest | Should Not BeNullOrEmpty
            $manifest | Should BeOfType [hashtable]
        }
        
        It "Should have correct root manifest fields" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $manifest.version | Should Be 1
            $manifest.name | Should Be "main-with-includes"
        }
        
        It "Should merge apps from included file" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            # Should have local app + 2 from base-apps.jsonc = 3 total
            $manifest.apps.Count | Should Be 3
        }
        
        It "Should contain local app from root manifest" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $localApp = $manifest.apps | Where-Object { $_.id -eq "local-app-1" }
            $localApp | Should Not BeNullOrEmpty
            $localApp.refs.windows | Should Be "Local.App1"
        }
        
        It "Should contain apps from included base-apps.jsonc" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $baseApp1 = $manifest.apps | Where-Object { $_.id -eq "base-app-1" }
            $baseApp2 = $manifest.apps | Where-Object { $_.id -eq "base-app-2" }
            
            $baseApp1 | Should Not BeNullOrEmpty
            $baseApp1.refs.windows | Should Be "Base.App1"
            
            $baseApp2 | Should Not BeNullOrEmpty
            $baseApp2.refs.windows | Should Be "Base.App2"
        }
        
        It "Should preserve multi-platform refs from included file" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $baseApp2 = $manifest.apps | Where-Object { $_.id -eq "base-app-2" }
            $baseApp2.refs.linux | Should Be "base-app-2"
        }
    }
    
    Context "JSONC comment stripping" {
        
        It "Should strip single-line comments" {
            $jsoncPath = Join-Path $script:FixturesDir "base-apps.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            # If comments weren't stripped, parsing would fail
            $manifest | Should Not BeNullOrEmpty
            $manifest.apps | Should Not BeNullOrEmpty
        }
    }
}

Describe "Manifest.Normalization" {
    
    Context "Default field initialization" {
        
        It "Should initialize missing arrays to empty" {
            $jsoncPath = Join-Path $script:FixturesDir "base-apps.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            # base-apps.jsonc has no restore/verify sections - they should default to empty arrays
            $manifest.ContainsKey('restore') | Should Be $true
            ($null -eq $manifest.restore) | Should Be $false
            @($manifest.restore).Count | Should Be 0
            
            $manifest.ContainsKey('verify') | Should Be $true
            ($null -eq $manifest.verify) | Should Be $false
            @($manifest.verify).Count | Should Be 0
        }
        
        It "Should set default version if missing" {
            $jsoncPath = Join-Path $script:FixturesDir "base-apps.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            # base-apps.jsonc has no version field
            $manifest.version | Should Be 1
        }
    }
}
