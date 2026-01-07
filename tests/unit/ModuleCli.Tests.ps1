
BeforeAll {
    # Module CLI Routing Smoke Tests
    # Tests that the 'module' command is properly wired into the CLI
    
    $script:EndstateRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    $script:EndstateBin = Join-Path $script:EndstateRoot "bin\endstate.ps1"
}

Describe "Module CLI Routing" {
    
    Context "Help text includes module command" {
        
        It "Should show 'module' in main help output" {
            $env:ENDSTATE_ALLOW_DIRECT = '1'
            $output = powershell.exe -NoProfile -Command "& '$script:EndstateBin' --help" 2>&1
            $outputStr = $output -join "`n"
            
            ($outputStr -match "module") | Should -Be $true
            ($outputStr -match "Generate config modules") | Should -Be $true
        }
        
        It "Should show module help with 'endstate module --help'" {
            $env:ENDSTATE_ALLOW_DIRECT = '1'
            $output = powershell.exe -NoProfile -Command "& '$script:EndstateBin' module --help" 2>&1
            $outputStr = $output -join "`n"
            
            ($outputStr -match "MODULE") | Should -Be $true
            ($outputStr -match "snapshot") | Should -Be $true
            ($outputStr -match "draft") | Should -Be $true
        }
    }
    
    Context "Module command routing" {
        
        It "Should show usage when no subcommand provided" {
            $env:ENDSTATE_ALLOW_DIRECT = '1'
            $output = powershell.exe -NoProfile -Command "& '$script:EndstateBin' module" 2>&1
            $outputStr = $output -join "`n"
            
            ($outputStr -match "requires a subcommand") | Should -Be $true
        }
        
        It "Should error on unknown subcommand" {
            $env:ENDSTATE_ALLOW_DIRECT = '1'
            $output = powershell.exe -NoProfile -Command "& '$script:EndstateBin' module unknown" 2>&1
            $outputStr = $output -join "`n"
            
            ($outputStr -match "Unknown module subcommand") | Should -Be $true
        }
        
        It "Should require --out for snapshot subcommand" {
            $env:ENDSTATE_ALLOW_DIRECT = '1'
            $output = powershell.exe -NoProfile -Command "& '$script:EndstateBin' module snapshot" 2>&1
            $outputStr = $output -join "`n"
            
            ($outputStr -match "--out.*required") | Should -Be $true
        }
        
        It "Should require --trace for draft subcommand" {
            $env:ENDSTATE_ALLOW_DIRECT = '1'
            $output = powershell.exe -NoProfile -Command "& '$script:EndstateBin' module draft --out test.jsonc" 2>&1
            $outputStr = $output -join "`n"
            
            ($outputStr -match "--trace.*required") | Should -Be $true
        }
    }
    
    Context "Capabilities includes module" {
        
        It "Should include module in JSON capabilities" {
            $env:ENDSTATE_ALLOW_DIRECT = '1'
            $output = powershell.exe -NoProfile -Command "& '$script:EndstateBin' capabilities -Json" 2>&1
            $outputStr = $output -join "`n"
            
            # Parse JSON and check for module
            $json = $outputStr | ConvertFrom-Json
            ($json.data.commands -contains "module") | Should -Be $true
        }
    }
}