<#
.SYNOPSIS
    Pester tests for engine/snapshot.ps1 - filesystem snapshot and diff helpers.

.DESCRIPTION
    Tests for:
    - Get-FilesystemSnapshot
    - Compare-FilesystemSnapshots
    - Apply-ExcludeHeuristics
    - ConvertTo-LogicalToken
    - Test-PathMatchesExcludePattern
#>

BeforeAll {
    $script:RepoRoot = Join-Path $PSScriptRoot "..\.."
    $script:SnapshotModule = Join-Path $script:RepoRoot "engine\snapshot.ps1"
    
    # Load snapshot module
    . $script:SnapshotModule
}

Describe "Snapshot.CompareFilesystemSnapshots" {
    
    Context "Empty snapshots" {
        
        It "Should return empty arrays for two empty snapshots" {
            $pre = @()
            $post = @()
            
            $result = Compare-FilesystemSnapshots -PreSnapshot $pre -PostSnapshot $post
            
            $result.added | Should -BeNullOrEmpty
            $result.modified | Should -BeNullOrEmpty
        }
    }
    
    Context "Added files detection" {
        
        It "Should detect a single added file" {
            $pre = @()
            $post = @(
                [PSCustomObject]@{ path = "C:\test\file.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
            )
            
            $result = Compare-FilesystemSnapshots -PreSnapshot $pre -PostSnapshot $post
            
            $result.added.Count | Should -Be 1
            $result.added[0].path | Should -Be "C:\test\file.txt"
            $result.modified | Should -BeNullOrEmpty
        }
        
        It "Should detect multiple added files" {
            $pre = @(
                [PSCustomObject]@{ path = "C:\existing.txt"; size = 50; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
            )
            $post = @(
                [PSCustomObject]@{ path = "C:\existing.txt"; size = 50; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
                [PSCustomObject]@{ path = "C:\new1.txt"; size = 100; lastWriteUtc = "2025-01-02T00:00:00Z"; isDirectory = $false }
                [PSCustomObject]@{ path = "C:\new2.txt"; size = 200; lastWriteUtc = "2025-01-02T00:00:00Z"; isDirectory = $false }
            )
            
            $result = Compare-FilesystemSnapshots -PreSnapshot $pre -PostSnapshot $post
            
            $result.added.Count | Should -Be 2
            $result.modified | Should -BeNullOrEmpty
        }
        
        It "Should sort added files by path" {
            $pre = @()
            $post = @(
                [PSCustomObject]@{ path = "C:\z.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
                [PSCustomObject]@{ path = "C:\a.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
                [PSCustomObject]@{ path = "C:\m.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
            )
            
            $result = Compare-FilesystemSnapshots -PreSnapshot $pre -PostSnapshot $post
            
            $result.added[0].path | Should -Be "C:\a.txt"
            $result.added[1].path | Should -Be "C:\m.txt"
            $result.added[2].path | Should -Be "C:\z.txt"
        }
    }
    
    Context "Modified files detection" {
        
        It "Should detect file modified by size change" {
            $pre = @(
                [PSCustomObject]@{ path = "C:\file.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
            )
            $post = @(
                [PSCustomObject]@{ path = "C:\file.txt"; size = 200; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
            )
            
            $result = Compare-FilesystemSnapshots -PreSnapshot $pre -PostSnapshot $post
            
            $result.added | Should -BeNullOrEmpty
            $result.modified.Count | Should -Be 1
            $result.modified[0].path | Should -Be "C:\file.txt"
        }
        
        It "Should detect file modified by timestamp change" {
            $pre = @(
                [PSCustomObject]@{ path = "C:\file.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
            )
            $post = @(
                [PSCustomObject]@{ path = "C:\file.txt"; size = 100; lastWriteUtc = "2025-01-02T00:00:00Z"; isDirectory = $false }
            )
            
            $result = Compare-FilesystemSnapshots -PreSnapshot $pre -PostSnapshot $post
            
            $result.added | Should -BeNullOrEmpty
            $result.modified.Count | Should -Be 1
        }
        
        It "Should not flag unchanged files as modified" {
            $pre = @(
                [PSCustomObject]@{ path = "C:\file.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
            )
            $post = @(
                [PSCustomObject]@{ path = "C:\file.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
            )
            
            $result = Compare-FilesystemSnapshots -PreSnapshot $pre -PostSnapshot $post
            
            $result.added | Should -BeNullOrEmpty
            $result.modified | Should -BeNullOrEmpty
        }
    }
    
    Context "Mixed added and modified" {
        
        It "Should correctly categorize added and modified files" {
            $pre = @(
                [PSCustomObject]@{ path = "C:\unchanged.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
                [PSCustomObject]@{ path = "C:\modified.txt"; size = 50; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
            )
            $post = @(
                [PSCustomObject]@{ path = "C:\unchanged.txt"; size = 100; lastWriteUtc = "2025-01-01T00:00:00Z"; isDirectory = $false }
                [PSCustomObject]@{ path = "C:\modified.txt"; size = 150; lastWriteUtc = "2025-01-02T00:00:00Z"; isDirectory = $false }
                [PSCustomObject]@{ path = "C:\new.txt"; size = 200; lastWriteUtc = "2025-01-02T00:00:00Z"; isDirectory = $false }
            )
            
            $result = Compare-FilesystemSnapshots -PreSnapshot $pre -PostSnapshot $post
            
            $result.added.Count | Should -Be 1
            $result.added[0].path | Should -Be "C:\new.txt"
            $result.modified.Count | Should -Be 1
            $result.modified[0].path | Should -Be "C:\modified.txt"
        }
    }
}

Describe "Snapshot.TestPathMatchesExcludePattern" {
    
    Context "Log patterns" {
        
        It "Should exclude paths containing \Logs\" {
            Test-PathMatchesExcludePattern -Path "C:\App\Logs\debug.log" | Should -Be $true
        }
        
        It "Should exclude .log files" {
            Test-PathMatchesExcludePattern -Path "C:\App\error.log" | Should -Be $true
        }
    }
    
    Context "Cache patterns" {
        
        It "Should exclude paths containing \Cache\" {
            Test-PathMatchesExcludePattern -Path "C:\App\Cache\data.bin" | Should -Be $true
        }
        
        It "Should exclude GPUCache directories" {
            Test-PathMatchesExcludePattern -Path "C:\App\GPUCache\shader.bin" | Should -Be $true
        }
        
        It "Should exclude ShaderCache directories" {
            Test-PathMatchesExcludePattern -Path "C:\App\ShaderCache\compiled.bin" | Should -Be $true
        }
    }
    
    Context "Temp patterns" {
        
        It "Should exclude paths containing \Temp\" {
            Test-PathMatchesExcludePattern -Path "C:\App\Temp\scratch.tmp" | Should -Be $true
        }
        
        It "Should exclude .tmp files" {
            Test-PathMatchesExcludePattern -Path "C:\App\data.tmp" | Should -Be $true
        }
    }
    
    Context "Crash dump patterns" {
        
        It "Should exclude Crashpad directories" {
            Test-PathMatchesExcludePattern -Path "C:\App\Crashpad\reports\crash.dmp" | Should -Be $true
        }
        
        It "Should exclude .dmp files" {
            Test-PathMatchesExcludePattern -Path "C:\App\memory.dmp" | Should -Be $true
        }
    }
    
    Context "Valid paths" {
        
        It "Should not exclude normal config files" {
            Test-PathMatchesExcludePattern -Path "C:\App\settings.json" | Should -Be $false
        }
        
        It "Should not exclude normal data files" {
            Test-PathMatchesExcludePattern -Path "C:\App\data\user.db" | Should -Be $false
        }
        
        It "Should not exclude paths with 'log' in filename but not as extension" {
            Test-PathMatchesExcludePattern -Path "C:\App\catalog.json" | Should -Be $false
        }
    }
}

Describe "Snapshot.ApplyExcludeHeuristics" {
    
    Context "Filtering entries" {
        
        It "Should filter out log files" {
            $entries = @(
                [PSCustomObject]@{ path = "C:\App\settings.json" }
                [PSCustomObject]@{ path = "C:\App\Logs\debug.log" }
                [PSCustomObject]@{ path = "C:\App\config.ini" }
            )
            
            $result = Apply-ExcludeHeuristics -Entries $entries
            
            $result.Count | Should -Be 2
            $result.path | Should -Contain "C:\App\settings.json"
            $result.path | Should -Contain "C:\App\config.ini"
            $result.path | Should -Not -Contain "C:\App\Logs\debug.log"
        }
        
        It "Should filter out cache directories" {
            $entries = @(
                [PSCustomObject]@{ path = "C:\App\data.json" }
                [PSCustomObject]@{ path = "C:\App\Cache\cached.bin" }
                [PSCustomObject]@{ path = "C:\App\GPUCache\shader.bin" }
            )
            
            $result = @(Apply-ExcludeHeuristics -Entries $entries)

            $result.Count | Should -Be 1
            $result[0].path | Should -Be "C:\App\data.json"
        }

        It "Should handle empty input" {
            $result = Apply-ExcludeHeuristics -Entries @()

            $result | Should -BeNullOrEmpty
        }

        It "Should support additional patterns" {
            $entries = @(
                [PSCustomObject]@{ path = "C:\App\settings.json" }
                [PSCustomObject]@{ path = "C:\App\custom\data.bin" }
            )

            $result = @(Apply-ExcludeHeuristics -Entries $entries -AdditionalPatterns @('\\custom\\'))

            $result.Count | Should -Be 1
            $result[0].path | Should -Be "C:\App\settings.json"
        }
    }
}

Describe "Snapshot.ConvertToLogicalToken" {
    
    Context "LOCALAPPDATA conversion" {
        
        It "Should convert LOCALAPPDATA path to token" {
            $testPath = Join-Path $env:LOCALAPPDATA "TestApp\config.json"
            
            $result = ConvertTo-LogicalToken -Path $testPath
            
            $result | Should -Match '^\$\{localappdata\}'
            $result | Should -Match 'TestApp/config\.json$'
        }
    }
    
    Context "APPDATA conversion" {
        
        It "Should convert APPDATA path to token" {
            $testPath = Join-Path $env:APPDATA "TestApp\settings.json"
            
            $result = ConvertTo-LogicalToken -Path $testPath
            
            $result | Should -Match '^\$\{appdata\}'
            $result | Should -Match 'TestApp/settings\.json$'
        }
    }
    
    Context "USERPROFILE conversion" {
        
        It "Should convert USERPROFILE path to token" {
            $testPath = Join-Path $env:USERPROFILE ".config\app.json"
            
            $result = ConvertTo-LogicalToken -Path $testPath
            
            $result | Should -Match '^\$\{home\}'
            $result | Should -Match '\.config/app\.json$'
        }
    }
    
    Context "ProgramFiles conversion" {
        
        It "Should convert ProgramFiles path to token" {
            $testPath = Join-Path $env:ProgramFiles "TestApp\config.ini"
            
            $result = ConvertTo-LogicalToken -Path $testPath
            
            $result | Should -Match '^\$\{programfiles\}'
            $result | Should -Match 'TestApp/config\.ini$'
        }
    }
    
    Context "Path separator normalization" {
        
        It "Should normalize backslashes to forward slashes" {
            $testPath = "C:\Some\Path\With\Backslashes"
            
            $result = ConvertTo-LogicalToken -Path $testPath
            
            $result | Should -Not -Match '\\'
            $result | Should -Match '/'
        }
    }
    
    Context "Non-standard paths" {
        
        It "Should preserve paths that don't match known roots" {
            $testPath = "D:\CustomDrive\App\config.json"
            
            $result = ConvertTo-LogicalToken -Path $testPath
            
            # Should still normalize slashes but not add token
            $result | Should -Be "D:/CustomDrive/App/config.json"
        }
    }
}

Describe "Snapshot.GetExcludePatterns" {
    
    Context "Pattern list" {
        
        It "Should return non-empty array of patterns" {
            $patterns = Get-ExcludePatterns
            
            $patterns | Should -Not -BeNullOrEmpty
            $patterns.Count | Should -BeGreaterThan 10
        }
        
        It "Should include log patterns" {
            $patterns = Get-ExcludePatterns
            
            $patterns | Should -Contain '\\Logs\\'
        }
        
        It "Should include cache patterns" {
            $patterns = Get-ExcludePatterns
            
            $patterns | Should -Contain '\\Cache\\'
        }
        
        It "Should include temp patterns" {
            $patterns = Get-ExcludePatterns
            
            $patterns | Should -Contain '\\Temp\\'
        }
    }
}

Describe "Snapshot.GetFilesystemSnapshot" {
    
    Context "Non-existent paths" {
        
        It "Should return empty array for non-existent root" {
            $result = Get-FilesystemSnapshot -Roots @("C:\NonExistent\Path\That\Does\Not\Exist")
            
            $result | Should -BeNullOrEmpty
        }
    }
    
    Context "Real filesystem" {
        
        BeforeAll {
            # Create temp directory for testing
            $script:TestDir = Join-Path $env:TEMP "EndstateSnapshotTest_$(Get-Random)"
            New-Item -ItemType Directory -Path $script:TestDir -Force | Out-Null
            
            # Create some test files
            "test content" | Out-File -FilePath (Join-Path $script:TestDir "file1.txt") -Encoding UTF8
            "more content" | Out-File -FilePath (Join-Path $script:TestDir "file2.txt") -Encoding UTF8
            
            $subDir = Join-Path $script:TestDir "subdir"
            New-Item -ItemType Directory -Path $subDir -Force | Out-Null
            "nested" | Out-File -FilePath (Join-Path $subDir "nested.txt") -Encoding UTF8
        }
        
        AfterAll {
            # Cleanup
            if (Test-Path $script:TestDir) {
                Remove-Item -Path $script:TestDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should capture files from real directory" {
            $result = Get-FilesystemSnapshot -Roots @($script:TestDir)
            
            $result.Count | Should -BeGreaterThan 0
        }
        
        It "Should include path property" {
            $result = Get-FilesystemSnapshot -Roots @($script:TestDir)
            
            $result[0].path | Should -Not -BeNullOrEmpty
        }
        
        It "Should include size property for files" {
            $result = Get-FilesystemSnapshot -Roots @($script:TestDir)
            $files = $result | Where-Object { -not $_.isDirectory }
            
            $files[0].size | Should -BeGreaterThan 0
        }
        
        It "Should include lastWriteUtc property" {
            $result = Get-FilesystemSnapshot -Roots @($script:TestDir)
            
            $result[0].lastWriteUtc | Should -Not -BeNullOrEmpty
        }
        
        It "Should sort results by path" {
            $result = Get-FilesystemSnapshot -Roots @($script:TestDir)
            
            $paths = $result | ForEach-Object { $_.path }
            $sorted = $paths | Sort-Object
            
            $paths | Should -Be $sorted
        }
    }
}
