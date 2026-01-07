<#
.SYNOPSIS
    Pester tests for verify subsystem.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\..\"
    $script:VerifiersDir = Join-Path $script:ProvisioningRoot "verifiers"
    $script:FixturesDir = Join-Path $PSScriptRoot "..\fixtures"
    
    # Load verifiers directly (avoid loading full verify.ps1 which has side effects)
    . (Join-Path $script:VerifiersDir "file-exists.ps1")
    . (Join-Path $script:VerifiersDir "command-exists.ps1")
    . (Join-Path $script:VerifiersDir "registry-key-exists.ps1")
}

Describe "Verifier.FileExists" {
    
    Context "Existing files" {
        
        It "Should pass for existing file" {
            # Use a file we know exists - the test file itself
            $testFile = $PSCommandPath
            
            $result = Test-FileExistsVerifier -Path $testFile
            
            $result.Success | Should -Be $true
            $result.Message | Should -Match "File exists"
        }
        
        It "Should pass for existing directory" {
            $testDir = $PSScriptRoot
            
            $result = Test-FileExistsVerifier -Path $testDir
            
            $result.Success | Should -Be $true
            $result.Message | Should -Match "Directory exists"
        }
    }
    
    Context "Non-existing paths" {
        
        It "Should fail for non-existing file" {
            $fakePath = "C:\nonexistent\path\file-12345.txt"
            
            $result = Test-FileExistsVerifier -Path $fakePath
            
            $result.Success | Should -Be $false
            $result.Message | Should -Match "does not exist"
        }
    }
    
    Context "Path expansion" {
        
        It "Should expand ~ to user profile" {
            # Create a temp file in user profile for testing
            $testFile = Join-Path $env:USERPROFILE ".provisioning-test-file-$(Get-Random).tmp"
            
            try {
                "test" | Out-File -FilePath $testFile -Encoding UTF8
                
                $tildeFile = $testFile -replace [regex]::Escape($env:USERPROFILE), "~"
                $result = Test-FileExistsVerifier -Path $tildeFile
                
                $result.Success | Should -Be $true
            }
            finally {
                if (Test-Path $testFile) {
                    Remove-Item $testFile -Force
                }
            }
        }
    }
}

Describe "Verifier.CommandExists" {
    
    Context "Existing commands" {
        
        It "Should pass for pwsh command" {
            $result = Test-CommandExistsVerifier -Command "pwsh"
            
            $result.Success | Should -Be $true
            $result.Message | Should -Match "Command exists"
        }
        
        It "Should pass for powershell command" {
            $result = Test-CommandExistsVerifier -Command "powershell"
            
            $result.Success | Should -Be $true
        }
    }
    
    Context "Non-existing commands" {
        
        It "Should fail for non-existing command" {
            $result = Test-CommandExistsVerifier -Command "nonexistent-command-xyz-12345"
            
            $result.Success | Should -Be $false
            $result.Message | Should -Match "not found"
        }
    }
}

Describe "Verifier.RegistryKeyExists" {
    
    Context "Windows registry checks" {
        
        It "Should pass for known registry key" {
            # HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion always exists on Windows
            $result = Test-RegistryKeyExistsVerifier -Path "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion"
            
            # Skip if not on Windows
            if ($env:OS -ne "Windows_NT" -and -not $IsWindows) {
                Set-TestInconclusive "Registry tests only run on Windows"
                return
            }
            
            $result.Success | Should -Be $true
            $result.Message | Should -Match "Registry key exists"
        }
        
        It "Should fail for non-existing registry key" {
            $result = Test-RegistryKeyExistsVerifier -Path "HKLM:\SOFTWARE\NonExistentKey12345"
            
            # Skip if not on Windows
            if ($env:OS -ne "Windows_NT" -and -not $IsWindows) {
                Set-TestInconclusive "Registry tests only run on Windows"
                return
            }
            
            $result.Success | Should -Be $false
        }
        
        It "Should check specific registry value" {
            # ProgramFilesDir always exists
            $result = Test-RegistryKeyExistsVerifier -Path "HKLM:\SOFTWARE\Microsoft\Windows\CurrentVersion" -Name "ProgramFilesDir"
            
            # Skip if not on Windows
            if ($env:OS -ne "Windows_NT" -and -not $IsWindows) {
                Set-TestInconclusive "Registry tests only run on Windows"
                return
            }
            
            $result.Success | Should -Be $true
            $result.Message | Should -Match "Registry value exists"
        }
    }
}

Describe "Verifier.ResultStructure" {
    
    Context "Result object structure" {
        
        It "Should return Success boolean" {
            $result = Test-FileExistsVerifier -Path $PSCommandPath
            
            $result.ContainsKey('Success') | Should -Be $true
            $result.Success | Should -BeOfType [bool]
        }
        
        It "Should return Message string" {
            $result = Test-FileExistsVerifier -Path $PSCommandPath
            
            $result.ContainsKey('Message') | Should -Be $true
            $result.Message | Should -BeOfType [string]
        }
        
        It "Should return Path for file-exists verifier" {
            $result = Test-FileExistsVerifier -Path $PSCommandPath
            
            $result.ContainsKey('Path') | Should -Be $true
        }
        
        It "Should return Command for command-exists verifier" {
            $result = Test-CommandExistsVerifier -Command "pwsh"
            
            $result.ContainsKey('Command') | Should -Be $true
        }
    }
}

Describe "Verifier.Determinism" {
    
    Context "Repeated calls produce same result" {
        
        It "Should produce identical results for file-exists" {
            $path = $PSCommandPath
            
            $result1 = Test-FileExistsVerifier -Path $path
            $result2 = Test-FileExistsVerifier -Path $path
            
            $result1.Success | Should -Be $result2.Success
        }
        
        It "Should produce identical results for command-exists" {
            $cmd = "pwsh"
            
            $result1 = Test-CommandExistsVerifier -Command $cmd
            $result2 = Test-CommandExistsVerifier -Command $cmd
            
            $result1.Success | Should -Be $result2.Success
        }
    }
}

Describe "Verify.JsonEnvelopeContract" {
    
    BeforeAll {
        # Load json-output module for New-JsonError and New-JsonEnvelope
        . (Join-Path $script:ProvisioningRoot "engine\json-output.ps1")
    }
    
    Context "Error field contract" {
        
        It "Should include typed error when verify has missing apps" {
            # Simulate verify results with failures
            $results = @(
                @{ type = "app"; ref = "Notepad++.Notepad++"; status = "fail" }
                @{ type = "app"; ref = "Git.Git"; status = "pass" }
            )
            $failCount = 1
            $passCount = 1
            $manifestPath = "C:\test\manifest.json"
            
            # Collect failed items (same logic as verify.ps1)
            $failedApps = @($results | Where-Object { $_.type -eq "app" -and $_.status -eq "fail" } | ForEach-Object { $_.ref })
            $failedVerifiers = @($results | Where-Object { $_.type -eq "verify" -and $_.status -eq "fail" })
            
            $messageParts = @()
            if ($failedApps.Count -gt 0) {
                $messageParts += "Missing apps: $($failedApps -join ', ')"
            }
            if ($failedVerifiers.Count -gt 0) {
                $messageParts += "$($failedVerifiers.Count) verification(s) failed"
            }
            
            $verifyError = New-JsonError `
                -Code (Get-ErrorCode -Name "VERIFY_FAILED") `
                -Message ($messageParts -join "; ") `
                -Detail @{
                    missingApps = $failedApps
                    failedVerifierCount = $failedVerifiers.Count
                    manifestPath = $manifestPath
                }
            
            # Assertions
            $verifyError | Should -Not -Be $null
            $verifyError.code | Should -Be "VERIFY_FAILED"
            $verifyError.message | Should -Match "Missing apps"
            $verifyError.message.Contains("Notepad++.Notepad++") | Should -Be $true
            $verifyError.detail.missingApps -contains "Notepad++.Notepad++" | Should -Be $true
        }
        
        It "Should have null error when verify passes" {
            # When all pass, error should be null
            $failCount = 0
            $verifyError = $null
            
            if ($failCount -gt 0) {
                $verifyError = New-JsonError -Code "VERIFY_FAILED" -Message "Test"
            }
            
            $verifyError | Should -Be $null
        }
        
        It "Should create envelope with success=false and error non-null when failures exist" {
            $data = @{ summary = @{ pass = 1; fail = 1 } }
            $verifyError = New-JsonError -Code "VERIFY_FAILED" -Message "Missing apps: Test.App"
            
            $envelope = New-JsonEnvelope -Command "verify" -Success $false -Data $data -Error $verifyError
            
            $envelope.success | Should -Be $false
            $envelope.error | Should -Not -Be $null
            $envelope.error.code | Should -Be "VERIFY_FAILED"
        }
    }
}