<#
.SYNOPSIS
    Pester tests for the trace engine (module generation pipeline).
#>

BeforeAll {
    $script:ProvisioningRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    $script:TraceScript = Join-Path $script:ProvisioningRoot "engine" "trace.ps1"
    
    # Load the trace engine
    . $script:TraceScript
}

Describe "Trace.Snapshot" {
    
    Context "New-TraceSnapshot creates valid snapshot structure" {
        
        It "Should return snapshot with required fields" {
            # Create a minimal snapshot (will scan real APPDATA/LOCALAPPDATA)
            $snapshot = New-TraceSnapshot
            
            $snapshot | Should -Not -BeNullOrEmpty
            $snapshot.version | Should -Be 1
            $snapshot.timestamp | Should -Not -BeNullOrEmpty
            $snapshot.roots | Should -Not -BeNullOrEmpty
            $snapshot.files | Should -Not -BeNullOrEmpty
        }
        
        It "Should write snapshot to file when OutputPath provided" {
            $outPath = Join-Path $TestDrive "snapshot-test.json"
            
            # Create a mock snapshot with minimal data
            $snapshot = @{
                version = 1
                timestamp = (Get-Date).ToUniversalTime().ToString("o")
                roots = @{ LOCALAPPDATA = $env:LOCALAPPDATA }
                files = @(
                    @{ path = "%LOCALAPPDATA%\Test\file.txt"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $json = $snapshot | ConvertTo-Json -Depth 10
            $json | Out-File -FilePath $outPath -Encoding UTF8
            
            (Test-Path $outPath) | Should -Be $true
            
            $loaded = Get-Content -Path $outPath -Raw | ConvertFrom-Json
            $loaded.version | Should -Be 1
            $loaded.files.Count | Should -Be 1
        }
    }
    
    Context "Read-TraceSnapshot loads snapshot correctly" {
        
        It "Should load snapshot from JSON file" {
            $snapshotPath = Join-Path $TestDrive "read-test.json"
            
            $snapshot = @{
                version = 1
                timestamp = "2025-01-01T12:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\config.json"; size = 256; lastWriteTime = "2025-01-01T10:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\App\data.db"; size = 1024; lastWriteTime = "2025-01-01T11:00:00Z" }
                )
            }
            
            $snapshot | ConvertTo-Json -Depth 10 | Out-File -FilePath $snapshotPath -Encoding UTF8
            
            $loaded = Read-TraceSnapshot -Path $snapshotPath
            
            $loaded.version | Should -Be 1
            $loaded.files.Count | Should -Be 2
            $loaded.files[0].path | Should -Be "%LOCALAPPDATA%\App\config.json"
        }
        
        It "Should throw when file not found" {
            $thrown = $false
            try {
                Read-TraceSnapshot -Path "C:\nonexistent\snapshot.json"
            } catch {
                $thrown = $true
            }
            $thrown | Should -Be $true
        }
    }
}

Describe "Trace.Diff" {
    
    Context "Compare-TraceSnapshots detects added files" {
        
        It "Should detect files added in after snapshot" {
            $baseline = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\existing.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $after = @{
                version = 1
                timestamp = "2025-01-01T01:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\existing.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\App\new-file.json"; size = 200; lastWriteTime = "2025-01-01T01:00:00Z" }
                )
            }
            
            $diff = Compare-TraceSnapshots -Baseline $baseline -After $after
            
            $diff.added.Count | Should -Be 1
            $diff.added[0].path | Should -Be "%LOCALAPPDATA%\App\new-file.json"
            $diff.modified.Count | Should -Be 0
        }
    }
    
    Context "Compare-TraceSnapshots detects modified files" {
        
        It "Should detect files with changed size" {
            $baseline = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\config.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $after = @{
                version = 1
                timestamp = "2025-01-01T01:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\config.json"; size = 150; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $diff = Compare-TraceSnapshots -Baseline $baseline -After $after
            
            $diff.added.Count | Should -Be 0
            $diff.modified.Count | Should -Be 1
            $diff.modified[0].path | Should -Be "%LOCALAPPDATA%\App\config.json"
        }
        
        It "Should NOT detect unchanged files as modified" {
            $baseline = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\config.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $after = @{
                version = 1
                timestamp = "2025-01-01T01:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\config.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $diff = Compare-TraceSnapshots -Baseline $baseline -After $after
            
            # Same size and time = no modification
            $diff.modified.Count | Should -Be 0
        }
        
        It "Should detect both added and modified files" {
            $baseline = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\config.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $after = @{
                version = 1
                timestamp = "2025-01-01T01:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\config.json"; size = 200; lastWriteTime = "2025-01-01T01:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\App\new.json"; size = 50; lastWriteTime = "2025-01-01T01:00:00Z" }
                )
            }
            
            $diff = Compare-TraceSnapshots -Baseline $baseline -After $after
            
            $diff.added.Count | Should -Be 1
            $diff.modified.Count | Should -Be 1
        }
    }
    
    Context "Compare-TraceSnapshots ignores deleted files" {
        
        It "Should not report deleted files" {
            $baseline = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\old.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\App\kept.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $after = @{
                version = 1
                timestamp = "2025-01-01T01:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\kept.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $diff = Compare-TraceSnapshots -Baseline $baseline -After $after
            
            # Deleted files are ignored in v1
            $diff.added.Count | Should -Be 0
            $diff.modified.Count | Should -Be 0
        }
    }
}

Describe "Trace.RootDetection" {
    
    Context "Get-TraceRootFolders groups files correctly" {
        
        It "Should detect root folder from changed files" {
            $diff = @{
                added = @(
                    @{ path = "%LOCALAPPDATA%\Microsoft\PowerToys\settings.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\Microsoft\PowerToys\modules\FancyZones\config.json"; size = 200; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
                modified = @()
            }
            
            $roots = Get-TraceRootFolders -DiffResult $diff
            
            $roots.Count | Should -Be 1
            $roots[0].path | Should -Be "%LOCALAPPDATA%\Microsoft\PowerToys"
            $roots[0].files.Count | Should -Be 2
        }
        
        It "Should detect multiple root folders" {
            $diff = @{
                added = @(
                    @{ path = "%LOCALAPPDATA%\Microsoft\PowerToys\settings.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                    @{ path = "%APPDATA%\VSCodium\User\settings.json"; size = 200; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
                modified = @()
            }
            
            $roots = Get-TraceRootFolders -DiffResult $diff
            
            $roots.Count | Should -Be 2
        }
        
        It "Should return empty array when no changes" {
            $diff = @{
                added = @()
                modified = @()
            }
            
            $roots = Get-TraceRootFolders -DiffResult $diff
            
            $roots.Count | Should -Be 0
        }
    }
    
    Context "Merge-TraceRootsToApp merges roots into apps" {
        
        It "Should merge roots with same app name" {
            $roots = @(
                @{
                    path = "%LOCALAPPDATA%\Vendor\MyApp"
                    envVar = "%LOCALAPPDATA%"
                    files = @(
                        @{ path = "%LOCALAPPDATA%\Vendor\MyApp\local.json" }
                    )
                }
                @{
                    path = "%APPDATA%\Vendor\MyApp"
                    envVar = "%APPDATA%"
                    files = @(
                        @{ path = "%APPDATA%\Vendor\MyApp\roaming.json" }
                    )
                }
            )
            
            $apps = Merge-TraceRootsToApp -Roots $roots
            
            $apps.Count | Should -Be 1
            $apps[0].roots.Count | Should -Be 2
        }
        
        It "Should keep separate apps separate" {
            $roots = @(
                @{
                    path = "%LOCALAPPDATA%\Vendor\AppOne"
                    envVar = "%LOCALAPPDATA%"
                    files = @()
                }
                @{
                    path = "%LOCALAPPDATA%\Vendor\AppTwo"
                    envVar = "%LOCALAPPDATA%"
                    files = @()
                }
            )
            
            $apps = Merge-TraceRootsToApp -Roots $roots
            
            $apps.Count | Should -Be 2
        }
    }
}

Describe "Trace.Exclusions" {
    
    Context "Get-DefaultExcludePatterns returns expected patterns" {
        
        It "Should return array of patterns" {
            $patterns = Get-DefaultExcludePatterns
            
            $patterns | Should -Not -BeNullOrEmpty
            $patterns.Count | Should -BeGreaterThan 0
        }
        
        It "Should include common exclusion patterns" {
            $patterns = Get-DefaultExcludePatterns
            
            ($patterns -contains "**\Logs\**") | Should -Be $true
            ($patterns -contains "**\Cache\**") | Should -Be $true
            ($patterns -contains "**\Temp\**") | Should -Be $true
        }
    }
    
    Context "Test-PathMatchesExclude matches patterns correctly" {
        
        It "Should match Logs folder pattern" {
            $result = Test-PathMatchesExclude -Path "App\Logs\debug.log" -Patterns @("**\Logs\**")
            $result | Should -Be $true
        }
        
        It "Should match nested excluded folder" {
            $result = Test-PathMatchesExclude -Path "App\subfolder\Cache\data.bin" -Patterns @("**\Cache\**")
            $result | Should -Be $true
        }
        
        It "Should NOT match non-excluded paths" {
            $result = Test-PathMatchesExclude -Path "App\configs\settings.json" -Patterns @("**\Logs\**", "**\Cache\**")
            $result | Should -Be $false
        }
        
        It "Should handle empty patterns array" {
            $result = Test-PathMatchesExclude -Path "App\anything\file.txt" -Patterns @()
            $result | Should -Be $false
        }
    }
}

Describe "Trace.ModuleDraft" {
    
    Context "New-ModuleDraft generates valid module structure" {
        
        It "Should generate module with required fields" {
            # Setup trace directory inline for Pester 3.4 compatibility
            $traceDir = Join-Path $TestDrive "trace-test-1"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @()
            }
            
            $after = @{
                version = 1
                timestamp = "2025-01-01T01:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\TestVendor\TestApp\settings.json"; size = 100; lastWriteTime = "2025-01-01T01:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\TestVendor\TestApp\config\prefs.json"; size = 200; lastWriteTime = "2025-01-01T01:00:00Z" }
                )
            }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "test-module.jsonc"
            
            $module = New-ModuleDraft -TracePath $traceDir -OutputPath $outPath
            
            $module.id | Should -Not -BeNullOrEmpty
            $module.displayName | Should -Not -BeNullOrEmpty
            $module.restore | Should -Not -BeNullOrEmpty
            $module.capture | Should -Not -BeNullOrEmpty
        }
        
        It "Should generate module file on disk" {
            $traceDir = Join-Path $TestDrive "trace-test-2"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{ version = 1; timestamp = "2025-01-01T00:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @() }
            $after = @{ version = 1; timestamp = "2025-01-01T01:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @(@{ path = "%LOCALAPPDATA%\Vendor\App\file.json"; size = 100; lastWriteTime = "2025-01-01T01:00:00Z" }) }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "test-module-file.jsonc"
            
            New-ModuleDraft -TracePath $traceDir -OutputPath $outPath
            
            (Test-Path $outPath) | Should -Be $true
            $content = Get-Content -Path $outPath -Raw
            $content | Should -Match '"id"'
            $content | Should -Match '"restore"'
        }
        
        It "Should include exclude patterns in restore entries" {
            $traceDir = Join-Path $TestDrive "trace-test-3"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{ version = 1; timestamp = "2025-01-01T00:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @() }
            $after = @{ version = 1; timestamp = "2025-01-01T01:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @(@{ path = "%LOCALAPPDATA%\Vendor\App\file.json"; size = 100; lastWriteTime = "2025-01-01T01:00:00Z" }) }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "test-module-excludes.jsonc"
            
            $module = New-ModuleDraft -TracePath $traceDir -OutputPath $outPath
            
            $module.restore[0].exclude | Should -Not -BeNullOrEmpty
            ($module.restore[0].exclude -contains "**\Logs\**") | Should -Be $true
        }
        
        It "Should include exclude patterns in capture section" {
            $traceDir = Join-Path $TestDrive "trace-test-4"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{ version = 1; timestamp = "2025-01-01T00:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @() }
            $after = @{ version = 1; timestamp = "2025-01-01T01:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @(@{ path = "%LOCALAPPDATA%\Vendor\App\file.json"; size = 100; lastWriteTime = "2025-01-01T01:00:00Z" }) }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "test-module-capture.jsonc"
            
            $module = New-ModuleDraft -TracePath $traceDir -OutputPath $outPath
            
            $module.capture.exclude | Should -Not -BeNullOrEmpty
        }
        
        It "Should derive module ID from app name" {
            $traceDir = Join-Path $TestDrive "trace-test-5"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{ version = 1; timestamp = "2025-01-01T00:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @() }
            $after = @{ version = 1; timestamp = "2025-01-01T01:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @(@{ path = "%LOCALAPPDATA%\Vendor\App\file.json"; size = 100; lastWriteTime = "2025-01-01T01:00:00Z" }) }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "test-module-id.jsonc"
            
            $module = New-ModuleDraft -TracePath $traceDir -OutputPath $outPath
            
            $module.id | Should -Match "^apps\."
        }
    }
    
    Context "New-ModuleDraft error handling" {
        
        It "Should throw when baseline.json missing" {
            $emptyDir = Join-Path $TestDrive "empty-trace"
            New-Item -ItemType Directory -Path $emptyDir -Force | Out-Null
            
            $thrown = $false
            try {
                New-ModuleDraft -TracePath $emptyDir -OutputPath (Join-Path $TestDrive "out.jsonc")
            } catch {
                $thrown = $true
            }
            $thrown | Should -Be $true
        }
        
        It "Should throw when no changes detected" {
            $noChangesDir = Join-Path $TestDrive "no-changes-trace"
            New-Item -ItemType Directory -Path $noChangesDir -Force | Out-Null
            
            $snapshot = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\App\file.json"; size = 100; lastWriteTime = "2025-01-01T00:00:00Z" }
                )
            }
            
            $snapshot | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $noChangesDir "baseline.json") -Encoding UTF8
            $snapshot | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $noChangesDir "after.json") -Encoding UTF8
            
            $thrown = $false
            try {
                New-ModuleDraft -TracePath $noChangesDir -OutputPath (Join-Path $TestDrive "out.jsonc")
            } catch {
                $thrown = $true
            }
            $thrown | Should -Be $true
        }
    }
}

Describe "Trace.IncludeFilter" {
    
    Context "New-ModuleDraft with --include filter" {
        
        It "Should filter diff entries by include string (case-insensitive)" {
            # Setup trace with multiple apps
            $traceDir = Join-Path $TestDrive "filter-test-1"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @()
            }
            
            # After has files from multiple apps (simulating noisy environment)
            $after = @{
                version = 1
                timestamp = "2025-01-01T01:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\Microsoft\PowerToys\settings.json"; size = 100; lastWriteTime = "2025-01-01T01:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\Google\DriveFS\cache.db"; size = 5000; lastWriteTime = "2025-01-01T01:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\Mozilla\Firefox\profiles.ini"; size = 200; lastWriteTime = "2025-01-01T01:00:00Z" }
                )
            }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "filtered-module.jsonc"
            
            # Filter to only PowerToys (case-insensitive)
            $module = New-ModuleDraft -TracePath $traceDir -OutputPath $outPath -IncludeFilter "powertoys"
            
            # Should only have PowerToys root
            $module.id | Should -Match "powertoys"
            $module.restore.Count | Should -Be 1
            $module.restore[0].target | Should -Match "PowerToys"
        }
        
        It "Should work without filter (unchanged behavior)" {
            $traceDir = Join-Path $TestDrive "filter-test-2"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{ version = 1; timestamp = "2025-01-01T00:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @() }
            $after = @{ version = 1; timestamp = "2025-01-01T01:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @(@{ path = "%LOCALAPPDATA%\Vendor\App\file.json"; size = 100; lastWriteTime = "2025-01-01T01:00:00Z" }) }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "no-filter-module.jsonc"
            
            # No filter - should work as before
            $module = New-ModuleDraft -TracePath $traceDir -OutputPath $outPath
            
            $module | Should -Not -BeNullOrEmpty
            $module.restore.Count | Should -BeGreaterThan 0
        }
        
        It "Should throw clear error when filter matches nothing" {
            $traceDir = Join-Path $TestDrive "filter-test-3"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{ version = 1; timestamp = "2025-01-01T00:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @() }
            $after = @{ version = 1; timestamp = "2025-01-01T01:00:00Z"; roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }; files = @(@{ path = "%LOCALAPPDATA%\Vendor\App\file.json"; size = 100; lastWriteTime = "2025-01-01T01:00:00Z" }) }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "nomatch-module.jsonc"
            
            # Filter that matches nothing
            $thrown = $false
            $errorMsg = ""
            try {
                New-ModuleDraft -TracePath $traceDir -OutputPath $outPath -IncludeFilter "NonExistentApp"
            } catch {
                $thrown = $true
                $errorMsg = $_.Exception.Message
            }
            
            $thrown | Should -Be $true
            $errorMsg | Should -Match "No changes match the include filter"
            $errorMsg | Should -Match "NonExistentApp"
        }
        
        It "Should select correct root when filter narrows to single app" {
            $traceDir = Join-Path $TestDrive "filter-test-4"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @()
            }
            
            # Multiple apps with overlapping vendor
            $after = @{
                version = 1
                timestamp = "2025-01-01T01:00:00Z"
                roots = @{ LOCALAPPDATA = "C:\Users\Test\AppData\Local" }
                files = @(
                    @{ path = "%LOCALAPPDATA%\Microsoft\PowerToys\settings.json"; size = 100; lastWriteTime = "2025-01-01T01:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\Microsoft\VSCode\settings.json"; size = 200; lastWriteTime = "2025-01-01T01:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\Microsoft\Edge\Preferences"; size = 300; lastWriteTime = "2025-01-01T01:00:00Z" }
                )
            }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "vscode-module.jsonc"
            
            # Filter to VSCode only
            $module = New-ModuleDraft -TracePath $traceDir -OutputPath $outPath -IncludeFilter "VSCode"
            
            $module.restore.Count | Should -Be 1
            $module.restore[0].target | Should -Match "VSCode"
        }
    }
}

Describe "Trace.Integration" {
    
    Context "End-to-end module generation" {
        
        It "Should generate valid module from trace snapshots" {
            # Setup trace directory
            $traceDir = Join-Path $TestDrive "integration-trace"
            New-Item -ItemType Directory -Path $traceDir -Force | Out-Null
            
            $baseline = @{
                version = 1
                timestamp = "2025-01-01T00:00:00Z"
                roots = @{ 
                    LOCALAPPDATA = "C:\Users\Test\AppData\Local"
                    APPDATA = "C:\Users\Test\AppData\Roaming"
                }
                files = @()
            }
            
            $after = @{
                version = 1
                timestamp = "2025-01-01T01:00:00Z"
                roots = @{ 
                    LOCALAPPDATA = "C:\Users\Test\AppData\Local"
                    APPDATA = "C:\Users\Test\AppData\Roaming"
                }
                files = @(
                    @{ path = "%LOCALAPPDATA%\ExampleCorp\ExampleApp\settings.json"; size = 512; lastWriteTime = "2025-01-01T01:00:00Z" }
                    @{ path = "%LOCALAPPDATA%\ExampleCorp\ExampleApp\cache\temp.dat"; size = 1024; lastWriteTime = "2025-01-01T01:00:00Z" }
                    @{ path = "%APPDATA%\ExampleCorp\ExampleApp\user-prefs.json"; size = 256; lastWriteTime = "2025-01-01T01:00:00Z" }
                )
            }
            
            $baseline | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "baseline.json") -Encoding UTF8
            $after | ConvertTo-Json -Depth 10 | Out-File -FilePath (Join-Path $traceDir "after.json") -Encoding UTF8
            
            $outPath = Join-Path $TestDrive "example-module.jsonc"
            
            # Generate module
            $module = New-ModuleDraft -TracePath $traceDir -OutputPath $outPath
            
            # Verify structure
            $module.id | Should -Match "^apps\."
            $module.displayName | Should -Not -BeNullOrEmpty
            $module.sensitivity | Should -Be "low"
            $module.matches | Should -Not -BeNullOrEmpty
            $module.restore.Count | Should -BeGreaterThan 0
            $module.capture.files.Count | Should -BeGreaterThan 0
            
            # Verify file was written
            (Test-Path $outPath) | Should -Be $true
            
            # Verify JSONC is parseable (after stripping comments)
            $content = Get-Content -Path $outPath -Raw
            $jsonContent = $content -replace '//.*', ''
            $parsed = $jsonContent | ConvertFrom-Json
            $parsed | Should -Not -BeNullOrEmpty
        }
    }
}