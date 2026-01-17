<#
.SYNOPSIS
    Pester tests for Provisioning CLI parameter contract.
#>

BeforeAll {
    $script:CliPath = Join-Path $PSScriptRoot "..\bin\cli.ps1"
}

Describe "Provisioning CLI Parameter Contract" {
    
    Context "capture command" {
        
        It "Should fail when -OutManifest is missing" {
            # Run CLI without -OutManifest - invoke directly
            $output = & $script:CliPath -Command capture 2>&1
            $exitCode = $LASTEXITCODE
            
            # Should exit with non-zero code
            $exitCode | Should -Be 1
            
            # Error message should mention -OutManifest or -Profile
            ($output -join "`n") | Should -Match "-OutManifest|-Profile"
        }
        
        It "Should proceed when -OutManifest is provided" {
            # Create a temp path for the manifest
            $tempDir = Join-Path $env:TEMP "provisioning-test-$(Get-Random)"
            $tempManifest = Join-Path $tempDir "test-manifest.jsonc"
            
            try {
                # Run CLI with -OutManifest - we expect it to NOT fail with the -OutManifest error
                $output = & $script:CliPath -Command capture -OutManifest $tempManifest 2>&1
                $outputText = $output -join "`n"
                
                # Should NOT contain the -OutManifest required error
                $outputText | Should -Not -Match "\[ERROR\] -OutManifest is required"
                
                # Should show capture starting (even if it fails later due to no winget)
                $outputText | Should -Match "Provisioning Capture|Starting capture|winget"
            }
            finally {
                # Cleanup
                if (Test-Path $tempDir) {
                    Remove-Item -Path $tempDir -Recurse -Force -ErrorAction SilentlyContinue
                }
            }
        }
    }
    
    Context "plan command" {
        
        It "Should fail when -Manifest is missing" {
            $output = & $script:CliPath -Command plan 2>&1
            ($output -join "`n") | Should -Match "-Manifest is required"
        }
    }
    
    Context "apply command" {
        
        It "Should fail when -Manifest is missing" {
            $output = & $script:CliPath -Command apply 2>&1
            ($output -join "`n") | Should -Match "-Manifest|-Plan"
        }
    }
    
    Context "verify command" {
        
        It "Should fail when -Manifest is missing" {
            $output = & $script:CliPath -Command verify 2>&1
            ($output -join "`n") | Should -Match "-Manifest is required"
        }
    }
    
    Context "doctor command" {
        
        It "Should run without any required parameters" {
            $output = & $script:CliPath -Command doctor 2>&1
            ($output -join "`n") | Should -Match "Provisioning Doctor"
        }
    }
    
    Context "help" {
        
        It "Should show help when no command is provided" {
            $output = & $script:CliPath 2>&1
            $outputText = $output -join "`n"
            
            $outputText | Should -Match "Provisioning CLI"
            $outputText | Should -Match "-OutManifest"
            $outputText | Should -Match "capture"
        }
    }
}
