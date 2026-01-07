# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Unit tests for driver registry and interface abstraction.

.DESCRIPTION
    Validates that:
    - Engine no longer directly imports winget driver
    - Driver registry correctly initializes and returns active driver
    - Driver name is dynamic, not hardcoded
    
    Note: Uses Pester 3.x compatible syntax (Should Be, not Should -Be)
#>

BeforeAll {
    # Script-level setup (Pester 3.x compatible)
    $script:RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    $script:DriverScript = Join-Path $script:RepoRoot "drivers\driver.ps1"
    $script:PathsScript = Join-Path $script:RepoRoot "engine\paths.ps1"
    
    # Source the modules
    . $script:DriverScript
    . $script:PathsScript
}

Describe "Driver Registry" {
    Context "Initialization" {
        It "Should initialize without error" {
            { Initialize-Drivers } | Should -Not -Throw
        }
        
        It "Should have winget registered on Windows" {
            $platform = Get-CurrentPlatform
            if ($platform -eq "windows") {
                $drivers = Get-RegisteredDrivers
                ($drivers -contains "winget") | Should -Be $true
            }
        }
        
        It "Should return active driver name" {
            $platform = Get-CurrentPlatform
            if ($platform -eq "windows") {
                $driverName = Get-ActiveDriverName
                $driverName | Should -Be "winget"
            }
        }
    }
    
    Context "Driver Interface" {
        It "Should return driver with required interface functions" {
            $platform = Get-CurrentPlatform
            if ($platform -eq "windows") {
                $driver = Get-ActiveDriver
                $driver | Should -Not -BeNullOrEmpty
                $driver.Name | Should -Be "winget"
                $driver.TestAvailable | Should -Not -BeNullOrEmpty
                $driver.TestPackageInstalled | Should -Not -BeNullOrEmpty
                $driver.InstallPackage | Should -Not -BeNullOrEmpty
                $driver.GetInstalledPackages | Should -Not -BeNullOrEmpty
            }
        }
        
        It "Should invoke driver functions through interface" {
            $platform = Get-CurrentPlatform
            if ($platform -eq "windows") {
                # Test that we can call through the interface
                { Invoke-DriverTestAvailable } | Should -Not -Throw
            }
        }
    }
    
    Context "No Direct Winget Import" {
        It "Engine apply.ps1 should import driver.ps1 not winget.ps1" {
            $applyContent = Get-Content (Join-Path $script:RepoRoot "engine\apply.ps1") -Raw
            $applyContent | Should -Match 'drivers\\driver\.ps1'
            $applyContent | Should -Not -Match 'drivers\\winget\.ps1'
        }
        
        It "Engine plan.ps1 should import driver.ps1 not winget.ps1" {
            $planContent = Get-Content (Join-Path $script:RepoRoot "engine\plan.ps1") -Raw
            $planContent | Should -Match 'drivers\\driver\.ps1'
            $planContent | Should -Not -Match 'drivers\\winget\.ps1'
        }
        
        It "Engine verify.ps1 should import driver.ps1 not winget.ps1" {
            $verifyContent = Get-Content (Join-Path $script:RepoRoot "engine\verify.ps1") -Raw
            $verifyContent | Should -Match 'drivers\\driver\.ps1'
            $verifyContent | Should -Not -Match 'drivers\\winget\.ps1'
        }
    }
    
    Context "Dynamic Driver Name in Events" {
        It "apply.ps1 should use Get-ActiveDriverName not hardcoded winget" {
            $applyContent = Get-Content (Join-Path $script:RepoRoot "engine\apply.ps1") -Raw
            # Should use dynamic driver name
            $applyContent | Should -Match 'Get-ActiveDriverName'
            # Should not have hardcoded winget in Write-ItemEvent calls
            $applyContent | Should -Not -Match 'Write-ItemEvent.*-Driver "winget"'
        }
        
        It "plan.ps1 should use dynamic driver name" {
            $planContent = Get-Content (Join-Path $script:RepoRoot "engine\plan.ps1") -Raw
            $planContent | Should -Match 'Get-ActiveDriverName'
            # driver field should use variable, not hardcoded string
            $planContent | Should -Not -Match 'driver = "winget"'
        }
        
        It "verify.ps1 should use dynamic driver name" {
            $verifyContent = Get-Content (Join-Path $script:RepoRoot "engine\verify.ps1") -Raw
            $verifyContent | Should -Match 'Get-ActiveDriverName'
            $verifyContent | Should -Not -Match 'Write-ItemEvent.*-Driver "winget"'
        }
    }
}

Describe "Platform Detection" {
    Context "Get-CurrentPlatform" {
        It "Should return a valid platform string" {
            $platform = Get-CurrentPlatform
            (@("windows", "macos", "linux") -contains $platform) | Should -Be $true
        }
        
        It "Should return 'windows' on Windows" {
            if ($env:OS -eq "Windows_NT") {
                $platform = Get-CurrentPlatform
                $platform | Should -Be "windows"
            }
        }
    }
    
    Context "Test-IsWindowsPlatform" {
        It "Should return boolean" {
            $result = Test-IsWindowsPlatform
            $result.GetType().Name | Should -Be "Boolean"
        }
        
        It "Should return true on Windows" {
            if ($env:OS -eq "Windows_NT") {
                Test-IsWindowsPlatform | Should -Be $true
            }
        }
    }
}