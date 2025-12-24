BeforeAll {
    # Load the endstate.ps1 script with functions only
    $script:EndstateRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    . (Join-Path $script:EndstateRoot "endstate.ps1") -LoadFunctionsOnly
}

Describe "Resolve-ManifestPath" {
    Context "Profile name resolution" {
        It "resolves simple profile name to repo manifests directory" {
            $result = Resolve-ManifestPath -ProfileName "test-profile"
            
            $result | Should -Not -BeNullOrEmpty
            $result | Should -BeLike "*\manifests\test-profile.jsonc"
        }
        
        It "resolves profile name without extension" {
            $result = Resolve-ManifestPath -ProfileName "my-machine"
            
            $result | Should -Match "manifests\\my-machine\.jsonc$"
        }
    }
    
    Context "File path detection and resolution" {
        It "detects full path with backslash separator" {
            $testPath = "C:\Users\test\Documents\Endstate\Setups\setup.jsonc"
            $result = Resolve-ManifestPath -ProfileName $testPath
            
            $result | Should -Be $testPath
        }
        
        It "detects full path with forward slash separator" {
            $testPath = "C:/Users/test/Documents/Endstate/Setups/setup.jsonc"
            $result = Resolve-ManifestPath -ProfileName $testPath
            
            $result | Should -Be $testPath
        }
        
        It "detects path with .jsonc extension" {
            $testPath = "setup_2025-12-22_14-56-26.jsonc"
            $result = Resolve-ManifestPath -ProfileName $testPath
            
            # Should be treated as file path and resolved to absolute
            $result | Should -Not -BeNullOrEmpty
            $result | Should -BeLike "*setup_2025-12-22_14-56-26.jsonc"
        }
        
        It "detects path with .json extension" {
            $testPath = "my-manifest.json"
            $result = Resolve-ManifestPath -ProfileName $testPath
            
            $result | Should -Not -BeNullOrEmpty
            $result | Should -BeLike "*my-manifest.json"
        }
        
        It "resolves relative path to absolute" {
            $testPath = ".\manifests\test.jsonc"
            $result = Resolve-ManifestPath -ProfileName $testPath
            
            $result | Should -Not -BeNullOrEmpty
            [System.IO.Path]::IsPathRooted($result) | Should -Be $true
        }
        
        It "returns absolute path unchanged" {
            $testPath = "C:\Temp\manifest.jsonc"
            $result = Resolve-ManifestPath -ProfileName $testPath
            
            $result | Should -Be $testPath
        }
    }
    
    Context "Backward compatibility" {
        It "still resolves simple names under repo manifests" {
            $result = Resolve-ManifestPath -ProfileName "hugo-laptop"
            
            $result | Should -Match "manifests\\hugo-laptop\.jsonc$"
        }
        
        It "does not treat simple name as file path" {
            $result = Resolve-ManifestPath -ProfileName "simple-name"
            
            # Should resolve under manifests/, not as file path
            $result | Should -Match "manifests\\simple-name\.jsonc$"
        }
    }
}

Describe "Manifest path resolution integration" -Tag "Integration" {
    BeforeAll {
        # Create a temporary manifest file
        $script:TempDir = Join-Path $env:TEMP "endstate-test-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TempDir -Force | Out-Null
        
        $script:TempManifest = Join-Path $script:TempDir "test-setup.jsonc"
        $manifestContent = @"
{
  "version": 1,
  "name": "test-setup",
  "apps": []
}
"@
        Set-Content -Path $script:TempManifest -Value $manifestContent -Encoding UTF8
    }
    
    AfterAll {
        if (Test-Path $script:TempDir) {
            Remove-Item -Recurse -Force $script:TempDir
        }
    }
    
    It "resolves full path to existing manifest file" {
        $result = Resolve-ManifestPath -ProfileName $script:TempManifest
        
        $result | Should -Be $script:TempManifest
        Test-Path $result | Should -Be $true
    }
    
    It "resolves path even if file doesn't exist yet (for capture scenarios)" {
        $nonExistentPath = Join-Path $script:TempDir "future-manifest.jsonc"
        $result = Resolve-ManifestPath -ProfileName $nonExistentPath
        
        $result | Should -Be $nonExistentPath
    }
}
