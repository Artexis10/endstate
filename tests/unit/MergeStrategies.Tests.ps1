<#
.SYNOPSIS
    Pester tests for merge strategies (JSON, INI, Append).
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    $script:HelpersScript = Join-Path $script:ProvisioningRoot "restorers\helpers.ps1"
    $script:JsonMergeScript = Join-Path $script:ProvisioningRoot "restorers\merge-json.ps1"
    $script:IniMergeScript = Join-Path $script:ProvisioningRoot "restorers\merge-ini.ps1"
    $script:AppendScript = Join-Path $script:ProvisioningRoot "restorers\append.ps1"
    
    # Load dependencies
    . $script:HelpersScript
    . $script:JsonMergeScript
    . $script:IniMergeScript
    . $script:AppendScript
}

Describe "JSON Merge" {
    
    Context "Creates target if missing" {
        
        It "Should create target file when it doesn't exist" {
            $sourceFile = Join-Path $TestDrive "source.json"
            $targetFile = Join-Path $TestDrive "target.json"
            
            '{"key": "value"}' | Out-File -FilePath $sourceFile -Encoding UTF8
            
            $result = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should -Be $true
            $result.Skipped | Should -Be $false
            (Test-Path $targetFile) | Should -Be $true
            
            $content = Get-Content $targetFile -Raw | ConvertFrom-Json
            $content.key | Should -Be "value"
        }
    }
    
    Context "Deep-merge overwrites keys correctly" {
        
        It "Should overwrite existing keys with source values" {
            $sourceFile = Join-Path $TestDrive "deep-source.json"
            $targetFile = Join-Path $TestDrive "deep-target.json"
            
            '{"name": "new-name", "nested": {"a": 1}}' | Out-File -FilePath $sourceFile -Encoding UTF8
            '{"name": "old-name", "nested": {"b": 2}, "keep": true}' | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should -Be $true
            
            $content = Get-Content $targetFile -Raw | ConvertFrom-Json
            $content.name | Should -Be "new-name"
            $content.keep | Should -Be $true
            $content.nested.a | Should -Be 1
            $content.nested.b | Should -Be 2
        }
        
        It "Should add new keys from source" {
            $sourceFile = Join-Path $TestDrive "add-source.json"
            $targetFile = Join-Path $TestDrive "add-target.json"
            
            '{"newKey": "newValue"}' | Out-File -FilePath $sourceFile -Encoding UTF8
            '{"existingKey": "existingValue"}' | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should -Be $true
            
            $content = Get-Content $targetFile -Raw | ConvertFrom-Json
            $content.existingKey | Should -Be "existingValue"
            $content.newKey | Should -Be "newValue"
        }
    }
    
    Context "Arrays replace by default" {
        
        It "Should replace arrays with source array (default behavior)" {
            $sourceFile = Join-Path $TestDrive "arr-source.json"
            $targetFile = Join-Path $TestDrive "arr-target.json"
            
            '{"items": [1, 2, 3]}' | Out-File -FilePath $sourceFile -Encoding UTF8
            '{"items": [4, 5, 6, 7]}' | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $false -ArrayStrategy "replace"
            
            $result.Success | Should -Be $true
            
            $content = Get-Content $targetFile -Raw | ConvertFrom-Json
            @($content.items).Count | Should -Be 3
            @($content.items)[0] | Should -Be 1
        }
    }
    
    Context "Array union strategy" {
        
        It "Should union arrays deterministically" {
            $sourceFile = Join-Path $TestDrive "union-source.json"
            $targetFile = Join-Path $TestDrive "union-target.json"
            
            '{"items": [2, 3, 4]}' | Out-File -FilePath $sourceFile -Encoding UTF8
            '{"items": [1, 2, 3]}' | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $false -ArrayStrategy "union"
            
            $result.Success | Should -Be $true
            
            $content = Get-Content $targetFile -Raw | ConvertFrom-Json
            # Union: [1, 2, 3] + [4] = [1, 2, 3, 4]
            @($content.items).Count | Should -Be 4
            @($content.items) -contains 1 | Should -Be $true
            @($content.items) -contains 4 | Should -Be $true
        }
        
        It "Should produce same result on re-run (deterministic)" {
            $sourceFile = Join-Path $TestDrive "det-source.json"
            $targetFile = Join-Path $TestDrive "det-target.json"
            
            '{"items": ["b", "c"]}' | Out-File -FilePath $sourceFile -Encoding UTF8
            '{"items": ["a", "b"]}' | Out-File -FilePath $targetFile -Encoding UTF8
            
            # First run
            $result1 = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $false -ArrayStrategy "union"
            $content1 = Get-Content $targetFile -Raw
            
            # Second run should skip (already up to date)
            $result2 = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $false -ArrayStrategy "union"
            
            $result2.Skipped | Should -Be $true
            $result2.Message | Should -Be "already up to date"
        }
    }
    
    Context "Sorted keys for deterministic output" {
        
        It "Should output JSON with sorted keys" {
            $sourceFile = Join-Path $TestDrive "sorted-source.json"
            $targetFile = Join-Path $TestDrive "sorted-target.json"
            
            '{"zebra": 1, "apple": 2}' | Out-File -FilePath $sourceFile -Encoding UTF8
            
            $result = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should -Be $true
            
            $content = Get-Content $targetFile -Raw
            # "apple" should appear before "zebra" in sorted output
            $appleIndex = $content.IndexOf('"apple"')
            $zebraIndex = $content.IndexOf('"zebra"')
            $appleIndex | Should -BeLessThan $zebraIndex
        }
    }
    
    Context "DryRun mode" {
        
        It "Should not modify files in DryRun mode" {
            $sourceFile = Join-Path $TestDrive "dry-source.json"
            $targetFile = Join-Path $TestDrive "dry-target.json"
            
            '{"new": "value"}' | Out-File -FilePath $sourceFile -Encoding UTF8
            '{"old": "value"}' | Out-File -FilePath $targetFile -Encoding UTF8
            
            $originalContent = Get-Content $targetFile -Raw
            
            $result = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $false -DryRun
            
            $result.Success | Should -Be $true
            
            $afterContent = Get-Content $targetFile -Raw
            $afterContent | Should -Be $originalContent
        }
    }
    
    Context "Backup when target exists" {
        
        It "Should create backup when target exists and backup is true" {
            $sourceFile = Join-Path $TestDrive "bak-source.json"
            $targetFile = Join-Path $TestDrive "bak-target.json"
            
            '{"new": "data"}' | Out-File -FilePath $sourceFile -Encoding UTF8
            '{"old": "data"}' | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-JsonMergeRestore -Source $sourceFile -Target $targetFile -Backup $true -RunId "test-backup-run"
            
            $result.Success | Should -Be $true
            $result.BackupPath | Should -Not -BeNullOrEmpty
        }
    }
}

Describe "INI Merge" {
    
    Context "Overwrites and adds keys" {
        
        It "Should overwrite existing keys and add new ones" {
            $sourceFile = Join-Path $TestDrive "ini-source.ini"
            $targetFile = Join-Path $TestDrive "ini-target.ini"
            
            @"
[Section1]
key1=newvalue1
key3=value3
"@ | Out-File -FilePath $sourceFile -Encoding UTF8
            
            @"
[Section1]
key1=oldvalue1
key2=value2
"@ | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-IniMergeRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should -Be $true
            
            $content = Get-Content $targetFile -Raw
            $content | Should -Match "key1=newvalue1"
            $content | Should -Match "key2=value2"
            $content | Should -Match "key3=value3"
        }
        
        It "Should preserve keys not in source" {
            $sourceFile = Join-Path $TestDrive "ini-preserve-source.ini"
            $targetFile = Join-Path $TestDrive "ini-preserve-target.ini"
            
            @"
[Settings]
newSetting=true
"@ | Out-File -FilePath $sourceFile -Encoding UTF8
            
            @"
[Settings]
existingSetting=value
[OtherSection]
otherKey=otherValue
"@ | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-IniMergeRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should -Be $true
            
            $content = Get-Content $targetFile -Raw
            $content | Should -Match "existingSetting=value"
            $content | Should -Match "newSetting=true"
            $content | Should -Match "\[OtherSection\]"
            $content | Should -Match "otherKey=otherValue"
        }
    }
    
    Context "Creates target if missing" {
        
        It "Should create INI file when target doesn't exist" {
            $sourceFile = Join-Path $TestDrive "ini-new-source.ini"
            $targetFile = Join-Path $TestDrive "ini-new-target.ini"
            
            @"
[Config]
setting=value
"@ | Out-File -FilePath $sourceFile -Encoding UTF8
            
            $result = Invoke-IniMergeRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should -Be $true
            (Test-Path $targetFile) | Should -Be $true
            
            $content = Get-Content $targetFile -Raw
            $content | Should -Match "\[Config\]"
            $content | Should -Match "setting=value"
        }
    }
    
    Context "DryRun mode" {
        
        It "Should not modify files in DryRun mode" {
            $sourceFile = Join-Path $TestDrive "ini-dry-source.ini"
            $targetFile = Join-Path $TestDrive "ini-dry-target.ini"
            
            "[New]`nnew=value" | Out-File -FilePath $sourceFile -Encoding UTF8
            "[Old]`nold=value" | Out-File -FilePath $targetFile -Encoding UTF8
            
            $originalContent = Get-Content $targetFile -Raw
            
            $result = Invoke-IniMergeRestore -Source $sourceFile -Target $targetFile -Backup $false -DryRun
            
            $result.Success | Should -Be $true
            
            $afterContent = Get-Content $targetFile -Raw
            $afterContent | Should -Be $originalContent
        }
    }
}

Describe "Append Lines" {
    
    Context "Adds missing lines only (idempotent)" {
        
        It "Should add lines not already present" {
            $sourceFile = Join-Path $TestDrive "append-source.txt"
            $targetFile = Join-Path $TestDrive "append-target.txt"
            
            "line2`nline3" | Out-File -FilePath $sourceFile -Encoding UTF8
            "line1" | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-AppendRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should -Be $true
            
            $content = Get-Content $targetFile
            ($content -join "`n") -match "line1" | Should -Be $true
            ($content -join "`n") -match "line2" | Should -Be $true
            ($content -join "`n") -match "line3" | Should -Be $true
        }
        
        It "Should not duplicate existing lines" {
            $sourceFile = Join-Path $TestDrive "append-dup-source.txt"
            $targetFile = Join-Path $TestDrive "append-dup-target.txt"
            
            "line1`nline2" | Out-File -FilePath $sourceFile -Encoding UTF8
            "line1`nline3" | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-AppendRestore -Source $sourceFile -Target $targetFile -Backup $false -Dedupe $true
            
            $result.Success | Should -Be $true
            
            $content = Get-Content $targetFile
            $line1Count = ($content | Where-Object { $_ -eq "line1" }).Count
            $line1Count | Should -Be 1
        }
    }
    
    Context "Rerun is idempotent" {
        
        It "Should skip on second run when all lines present" {
            $sourceFile = Join-Path $TestDrive "append-idem-source.txt"
            $targetFile = Join-Path $TestDrive "append-idem-target.txt"
            
            "line1`nline2" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            # First run - creates target
            $result1 = Invoke-AppendRestore -Source $sourceFile -Target $targetFile -Backup $false
            $result1.Success | Should -Be $true
            $result1.Skipped | Should -Be $false
            
            # Second run - should skip
            $result2 = Invoke-AppendRestore -Source $sourceFile -Target $targetFile -Backup $false
            $result2.Success | Should -Be $true
            $result2.Skipped | Should -Be $true
            $result2.Message | Should -Be "already up to date"
        }
    }
    
    Context "Creates target if missing" {
        
        It "Should create target file when it doesn't exist" {
            $sourceFile = Join-Path $TestDrive "append-new-source.txt"
            $targetFile = Join-Path $TestDrive "append-new-target.txt"
            
            "new line 1`nnew line 2" | Out-File -FilePath $sourceFile -Encoding UTF8
            
            $result = Invoke-AppendRestore -Source $sourceFile -Target $targetFile -Backup $false
            
            $result.Success | Should -Be $true
            (Test-Path $targetFile) | Should -Be $true
            
            $content = Get-Content $targetFile
            ($content -join "`n") -match "new line 1" | Should -Be $true
            ($content -join "`n") -match "new line 2" | Should -Be $true
        }
    }
    
    Context "DryRun mode" {
        
        It "Should not modify files in DryRun mode" {
            $sourceFile = Join-Path $TestDrive "append-dry-source.txt"
            $targetFile = Join-Path $TestDrive "append-dry-target.txt"
            
            "new line" | Out-File -FilePath $sourceFile -Encoding UTF8
            "existing line" | Out-File -FilePath $targetFile -Encoding UTF8
            
            $originalContent = Get-Content $targetFile -Raw
            
            $result = Invoke-AppendRestore -Source $sourceFile -Target $targetFile -Backup $false -DryRun
            
            $result.Success | Should -Be $true
            
            $afterContent = Get-Content $targetFile -Raw
            $afterContent | Should -Be $originalContent
        }
    }
    
    Context "Backup when target exists" {
        
        It "Should create backup when target exists" {
            $sourceFile = Join-Path $TestDrive "append-bak-source.txt"
            $targetFile = Join-Path $TestDrive "append-bak-target.txt"
            
            "new line" | Out-File -FilePath $sourceFile -Encoding UTF8
            "existing line" | Out-File -FilePath $targetFile -Encoding UTF8
            
            $result = Invoke-AppendRestore -Source $sourceFile -Target $targetFile -Backup $true -RunId "append-backup-test"
            
            $result.Success | Should -Be $true
            $result.BackupPath | Should -Not -BeNullOrEmpty
        }
    }
}

Describe "Helpers" {
    
    Context "Read-TextFileUtf8" {
        
        It "Should return null for non-existent file" {
            $content = Read-TextFileUtf8 -Path (Join-Path $TestDrive "nonexistent.txt")
            $content | Should -Be $null
        }
        
        It "Should read UTF-8 content" {
            $testFile = Join-Path $TestDrive "utf8-test.txt"
            "Hello World" | Out-File -FilePath $testFile -Encoding UTF8
            
            $content = Read-TextFileUtf8 -Path $testFile
            $content | Should -Match "Hello World"
        }
    }
    
    Context "Write-TextFileUtf8Atomic" {
        
        It "Should write content atomically" {
            $testFile = Join-Path $TestDrive "atomic-test.txt"
            
            Write-TextFileUtf8Atomic -Path $testFile -Content "atomic content"
            
            (Test-Path $testFile) | Should -Be $true
            $content = Get-Content $testFile -Raw
            $content | Should -Be "atomic content"
        }
        
        It "Should create parent directories" {
            $testFile = Join-Path $TestDrive "subdir\nested\atomic-nested.txt"
            
            Write-TextFileUtf8Atomic -Path $testFile -Content "nested content"
            
            (Test-Path $testFile) | Should -Be $true
        }
    }
}