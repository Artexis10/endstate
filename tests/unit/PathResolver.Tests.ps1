# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Unit tests for centralized path resolver.

.DESCRIPTION
    Validates that:
    - Environment variable expansion works
    - Tilde (~) expansion works
    - Logical tokens (${home}, ${appdata}, etc.) expand correctly
    - Backup path normalization handles various path formats
    
    Note: Uses Pester 3.x compatible syntax (Should Be, not Should -Be)
#>

# Script-level setup (Pester 3.x compatible)
$script:RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$script:PathsScript = Join-Path $script:RepoRoot "engine\paths.ps1"

# Source the module
. $script:PathsScript

Describe "Path Resolver" {
    Context "Environment Variable Expansion" {
        It "Should expand %USERPROFILE%" {
            $platform = Get-CurrentPlatform
            if ($platform -eq "windows") {
                $result = Expand-EndstatePath -Path "%USERPROFILE%\test"
                $result | Should Match "\\test$"
                $result | Should Not Match "%USERPROFILE%"
            }
        }
        
        It "Should expand %LOCALAPPDATA%" {
            $platform = Get-CurrentPlatform
            if ($platform -eq "windows") {
                $result = Expand-EndstatePath -Path "%LOCALAPPDATA%\MyApp"
                $result | Should Match "\\MyApp$"
                $result | Should Not Match "%LOCALAPPDATA%"
            }
        }
        
        It "Should expand %APPDATA%" {
            $platform = Get-CurrentPlatform
            if ($platform -eq "windows") {
                $result = Expand-EndstatePath -Path "%APPDATA%\Config"
                $result | Should Match "\\Config$"
                $result | Should Not Match "%APPDATA%"
            }
        }
    }
    
    Context "Tilde Expansion" {
        It "Should expand ~ to home directory" {
            $result = Expand-EndstatePath -Path "~/.config/app"
            $result | Should Not Match "^~"
            
            $homeDir = Get-HomeDirectory
            $result | Should Match ([regex]::Escape($homeDir))
        }
        
        It "Should expand ~/path correctly" {
            $result = Expand-EndstatePath -Path "~/Documents"
            $result | Should Not Match "^~"
            $result | Should Match "Documents$"
        }
    }
    
    Context "Logical Token Expansion" {
        It "Should expand `${home} token" {
            $result = Expand-EndstatePath -Path '${home}/test'
            $result | Should Not Match '\$\{home\}'
            
            $homeDir = Get-HomeDirectory
            $result | Should Match ([regex]::Escape($homeDir))
        }
        
        It "Should expand `${appdata} token on Windows" {
            $platform = Get-CurrentPlatform
            if ($platform -eq "windows") {
                $result = Expand-EndstatePath -Path '${appdata}/MyApp'
                $result | Should Not Match '\$\{appdata\}'
                $result | Should Match "\\MyApp$"
            }
        }
        
        It "Should expand `${localappdata} token on Windows" {
            $platform = Get-CurrentPlatform
            if ($platform -eq "windows") {
                $result = Expand-EndstatePath -Path '${localappdata}/Config'
                $result | Should Not Match '\$\{localappdata\}'
                $result | Should Match "\\Config$"
            }
        }
    }
    
    Context "Relative Path Resolution" {
        It "Should resolve ./ paths against BasePath" {
            $basePath = $script:RepoRoot
            $result = Expand-EndstatePath -Path "./configs/test.json" -BasePath $basePath
            $result | Should Match "configs"
            $result | Should Match "test\.json$"
            $result | Should Not Match "^\.\/"
        }
        
        It "Should resolve ../ paths against BasePath" {
            $basePath = Join-Path $script:RepoRoot "engine"
            $result = Expand-EndstatePath -Path "../configs/test.json" -BasePath $basePath
            $result | Should Match "configs"
            $result | Should Not Match "\.\.\/"
        }
        
        It "Should not modify relative paths without BasePath" {
            $result = Expand-EndstatePath -Path "./test"
            # Without BasePath, relative paths starting with ./ are not resolved
            $result | Should Match "test"
        }
    }
    
    Context "Path Separator Normalization" {
        It "Should normalize separators for current platform" {
            $platform = Get-CurrentPlatform
            $result = Expand-EndstatePath -Path "~/test/path"
            
            if ($platform -eq "windows") {
                $result | Should Match "\\"
            } else {
                $result | Should Match "/"
            }
        }
    }
}

Describe "Backup Path Normalization" {
    Context "ConvertTo-BackupPath" {
        It "Should strip Windows drive letter" {
            $result = ConvertTo-BackupPath -Path "C:\Users\test\file.txt"
            $result | Should Not Match "^C:"
            $result | Should Match "Users"
        }
        
        It "Should strip leading slashes" {
            $result = ConvertTo-BackupPath -Path "/home/user/file.txt"
            $result | Should Not Match "^/"
            $result | Should Match "^home"
        }
        
        It "Should handle paths without drive letters" {
            $result = ConvertTo-BackupPath -Path "Users\test\file.txt"
            $result | Should Be "Users\test\file.txt"
        }
        
        It "Should replace colons with underscores" {
            $result = ConvertTo-BackupPath -Path "D:\path:with:colons"
            $result | Should Not Match ":"
        }
        
        It "Should handle whitespace path" {
            $result = ConvertTo-BackupPath -Path "   "
            $result | Should Be "   "
        }
    }
}

Describe "Helper Functions" {
    Context "Get-HomeDirectory" {
        It "Should return non-empty home directory" {
            $homeDir = Get-HomeDirectory
            $homeDir | Should Not BeNullOrEmpty
        }
        
        It "Should return existing directory" {
            $homeDir = Get-HomeDirectory
            Test-Path $homeDir | Should Be $true
        }
    }
    
    Context "Test-IsAbsolutePath" {
        It "Should detect Windows absolute paths" {
            Test-IsAbsolutePath -Path "C:\Users\test" | Should Be $true
            Test-IsAbsolutePath -Path "D:\folder" | Should Be $true
        }
        
        It "Should detect Unix absolute paths" {
            Test-IsAbsolutePath -Path "/home/user" | Should Be $true
            Test-IsAbsolutePath -Path "/etc/config" | Should Be $true
        }
        
        It "Should detect UNC paths" {
            Test-IsAbsolutePath -Path "\\server\share" | Should Be $true
        }
        
        It "Should reject relative paths" {
            Test-IsAbsolutePath -Path "relative/path" | Should Be $false
            Test-IsAbsolutePath -Path "./local" | Should Be $false
            Test-IsAbsolutePath -Path "../parent" | Should Be $false
        }
    }
    
    Context "Get-LogicalTokens" {
        It "Should return token definitions for current platform" {
            $tokens = Get-LogicalTokens
            $tokens | Should Not BeNullOrEmpty
            ($tokens.Keys -contains '${home}') | Should Be $true
        }
        
        It "Should return expanded values" {
            $tokens = Get-LogicalTokens
            $tokens['${home}'] | Should Not BeNullOrEmpty
            $tokens['${home}'] | Should Not Match '\$\{'
        }
    }
}
