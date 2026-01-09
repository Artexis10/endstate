<#
.SYNOPSIS
    Pester tests for sandbox discovery harness validation.

.DESCRIPTION
    Validates that the sandbox-tests/discovery-harness files exist
    and scripts/sandbox-discovery.ps1 has required structure.
    
    NOTE: Does NOT run Windows Sandbox - only validates file structure and syntax.
#>

BeforeAll {
    $script:RepoRoot = Join-Path $PSScriptRoot "..\.."
    $script:DiscoveryScript = Join-Path $script:RepoRoot "scripts\sandbox-discovery.ps1"
    $script:HarnessDir = Join-Path $script:RepoRoot "sandbox-tests\discovery-harness"
    $script:SnapshotModule = Join-Path $script:RepoRoot "engine\snapshot.ps1"
}

Describe "SandboxDiscovery.FilesExist" {
    
    Context "Discovery harness directory structure" {
        
        It "Should have sandbox-tests/discovery-harness directory" {
            Test-Path $script:HarnessDir | Should -Be $true
        }
        
        It "Should have sandbox-install.ps1" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            Test-Path $installScript | Should -Be $true
        }
    }
    
    Context "Main discovery script" {
        
        It "Should have scripts/sandbox-discovery.ps1" {
            Test-Path $script:DiscoveryScript | Should -Be $true
        }
        
        It "Should have engine/snapshot.ps1" {
            Test-Path $script:SnapshotModule | Should -Be $true
        }
    }
}

Describe "SandboxDiscovery.ScriptValidation" {
    
    Context "sandbox-discovery.ps1 structure" {
        
        It "Should have WingetId parameter" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\$WingetId'
            $content | Should -Match '\[string\]\$WingetId'
        }
        
        It "Should have OutDir parameter" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\$OutDir'
        }
        
        It "Should have DryRun parameter" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\[switch\]\$DryRun'
        }
        
        It "Should have WriteModule parameter" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\[switch\]\$WriteModule'
        }
        
        It "Should have synopsis documentation" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\.SYNOPSIS'
        }
        
        It "Should have example documentation" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\.EXAMPLE'
        }
        
        It "Should load snapshot module" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match 'snapshot\.ps1'
        }
        
        It "Should generate .wsb file" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '\.wsb'
        }
        
        It "Should reference winget install" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match 'winget'
        }
        
        It "Should use powershell.exe not pwsh in LogonCommand" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match 'powershell\.exe'
            $content | Should -Not -Match 'pwsh\s+-NoExit\s+-Command'
        }
        
        It "Should use -ExecutionPolicy Bypass" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match '-ExecutionPolicy Bypass'
        }
        
        It "Should use -File invocation for sandbox-install.ps1" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            # The script builds the path in $scriptPath and uses -File `"$scriptPath`"
            $content | Should -Match 'sandbox-install\.ps1'
            $content | Should -Match '-File\s+`"\$scriptPath'
        }
        
        It "Should launch sandbox via WindowsSandbox.exe explicitly" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            # Must use WindowsSandbox.exe directly, not rely on .wsb file association
            $content | Should -Match 'WindowsSandbox\.exe'
            $content | Should -Match '\$wsExe\s*=\s*Join-Path\s+\$env:WINDIR'
            $content | Should -Match 'Start-Process\s+-FilePath\s+\$wsExe'
        }
        
        It "Should poll for sentinel files with timeout" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            # Must have a poll loop for DONE.txt/ERROR.txt
            $content | Should -Match '\$timeoutSeconds'
            $content | Should -Match '\$pollIntervalMs'
            $content | Should -Match 'while.*\$elapsed.*\$timeoutSeconds'
            $content | Should -Match 'Test-Path\s+\$doneFile.*Test-Path\s+\$errorFile'
        }
        
        It "Should check ERROR.txt before reporting missing artifacts" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            # ERROR.txt check must come before artifact existence check
            $errorCheckPos = $content.IndexOf('Test-Path $errorFile')
            $artifactCheckPos = $content.IndexOf('$artifactsExist')
            $errorCheckPos | Should -BeLessThan $artifactCheckPos
        }
        
        It "Should provide helpful error when WindowsSandbox.exe is missing" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match 'Windows Sandbox is not installed'
            $content | Should -Match 'Containers-DisposableClientVM'
        }
    }
    
    Context "sandbox-install.ps1 structure" {
        
        It "Should have WingetId parameter" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\$WingetId'
        }
        
        It "Should have OutputDir parameter" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\$OutputDir'
        }
        
        It "Should have DryRun parameter" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\$DryRun'
        }
        
        It "Should load snapshot module" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'snapshot\.ps1'
        }
        
        It "Should call Get-FilesystemSnapshot" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'Get-FilesystemSnapshot'
        }
        
        It "Should call Compare-FilesystemSnapshots" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'Compare-FilesystemSnapshots'
        }
        
        It "Should output pre.json" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'pre\.json'
        }
        
        It "Should output post.json" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'post\.json'
        }
        
        It "Should output diff.json" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'diff\.json'
        }
        
        It "Should write DONE.txt sentinel on success" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'DONE\.txt'
        }
        
        It "Should write ERROR.txt sentinel on failure" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'ERROR\.txt'
        }
        
        It "Should check for winget presence before install" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must check if winget is available
            $content | Should -Match 'Get-Command\s+winget'
        }
        
        It "Should have winget bootstrap function" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must have Ensure-Winget function
            $content | Should -Match 'function\s+Ensure-Winget'
        }
        
        It "Should download winget from aka.ms/getwinget" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must use official winget download URL
            $content | Should -Match 'aka\.ms/getwinget'
        }
        
        It "Should use Add-AppxPackage for winget installation" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must install via Add-AppxPackage
            $content | Should -Match 'Add-AppxPackage'
        }
        
        It "Should download VCLibs dependency" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must download VCLibs dependency
            $content | Should -Match 'VCLibs'
        }
        
        It "Should use recursive search for UI.Xaml package candidates" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must search recursively for appx/msix/msixbundle files
            $content | Should -Match 'Get-ChildItem.*-Recurse.*-Include.*\*\.appx.*\*\.msix'
        }
        
        It "Should detect x64 candidates by filename or folder path" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must check for x64 in name or path patterns
            $content | Should -Match "Name\s+-match\s+'x64'"
            $content | Should -Match "FullName\s+-match\s+'\\\\x64\\\\'"
            $content | Should -Match "\\\\win10-x64\\\\"
        }
        
        It "Should have fallback logic when no x64 candidate found" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must handle single candidate case and largest candidate fallback
            $content | Should -Match 'candidates\.Count\s*-eq\s*1'
            $content | Should -Match 'Sort-Object.*Length.*Descending'
        }
        
        It "Should include candidate list in error diagnostics" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must build diagnostic list with paths and sizes
            $content | Should -Match '\$candidateList.*candidates.*FullName.*Length'
        }
        
        It "Should call Ensure-Winget before winget install" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Ensure-Winget must be called before winget install
            $ensurePos = $content.IndexOf('Ensure-Winget')
            $wingetInstallPos = $content.IndexOf('& winget @wingetArgs')
            $ensurePos | Should -BeLessThan $wingetInstallPos
        }
        
        It "Should have Ensure-WindowsAppRuntime18 function" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'function\s+Ensure-WindowsAppRuntime18'
        }
        
        It "Should check Get-AppxPackage for Microsoft.WindowsAppRuntime.1.8" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'Get-AppxPackage\s+-Name\s+[''"]Microsoft\.WindowsAppRuntime\.1\.8[''"]'
        }
        
        It "Should call Ensure-WindowsAppRuntime18 before installing App Installer" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Ensure-WindowsAppRuntime18 must be called before Add-AppxPackage for winget
            $runtimePos = $content.IndexOf('Ensure-WindowsAppRuntime18')
            $wingetInstallPos = $content.IndexOf('Add-AppxPackage -Path $wingetPath')
            $runtimePos | Should -BeLessThan $wingetInstallPos
        }
    }
}

Describe "SandboxDiscovery.TimeoutConfiguration" {
    
    Context "sandbox-discovery.ps1 timeout settings" {
        
        It "Should have timeout of at least 900 seconds" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            # Extract timeout value using regex
            if ($content -match '\$timeoutSeconds\s*=\s*(\d+)') {
                [int]$timeout = $matches[1]
                $timeout | Should -BeGreaterOrEqual 900
            } else {
                throw "Could not find timeoutSeconds variable in script"
            }
        }
        
        It "Should mention winget/runtime bootstrap in timeout messaging" {
            $content = Get-Content -Path $script:DiscoveryScript -Raw
            $content | Should -Match 'winget.*runtime.*bootstrap|runtime.*bootstrap.*winget'
        }
    }
}

Describe "SandboxDiscovery.SnapshotModule" {
    
    Context "Required functions exist" {
        
        BeforeAll {
            . $script:SnapshotModule
        }
        
        It "Should export Get-FilesystemSnapshot" {
            Get-Command -Name Get-FilesystemSnapshot -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export Compare-FilesystemSnapshots" {
            Get-Command -Name Compare-FilesystemSnapshots -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export Apply-ExcludeHeuristics" {
            Get-Command -Name Apply-ExcludeHeuristics -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export ConvertTo-LogicalToken" {
            Get-Command -Name ConvertTo-LogicalToken -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export Test-PathMatchesExcludePattern" {
            Get-Command -Name Test-PathMatchesExcludePattern -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
        
        It "Should export Get-ExcludePatterns" {
            Get-Command -Name Get-ExcludePatterns -ErrorAction SilentlyContinue | Should -Not -BeNullOrEmpty
        }
    }
}
