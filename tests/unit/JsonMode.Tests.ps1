$script:EndstateRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$script:EndstatePath = Join-Path $script:EndstateRoot "endstate.ps1"

# Create test directory for mock files
$script:TestDir = Join-Path $env:TEMP "endstate-json-test-$([guid]::NewGuid().ToString('N').Substring(0,8))"
$script:MockWingetPath = Join-Path $script:TestDir "mock-winget.ps1"
$script:TestManifestPath = Join-Path $script:TestDir "test-manifest.jsonc"

New-Item -ItemType Directory -Path $script:TestDir -Force | Out-Null

# Mock winget command - simulates installed apps
$mockWingetContent = @'
param(
    [Parameter(Position=0)]
    [string]$Action,
    [string]$id,
    [switch]$AcceptSourceAgreements,
    [switch]$AcceptPackageAgreements,
    [switch]$e
)

# Simulate installed apps list
$installedApps = @"
Name                                   Id                                   Version
------------------------------------------------------------------------------------
7-Zip                                  7zip.7zip                            23.01
Git                                    Git.Git                              2.43.0
"@

if ($Action -eq "list") {
    Write-Output $installedApps
    exit 0
}

if ($Action -eq "install") {
    Write-Host "Installing $id..."
    exit 0
}

exit 0
'@
Set-Content -Path $script:MockWingetPath -Value $mockWingetContent

# Create test manifest
$testManifest = @'
{
  "version": 1,
  "name": "test-manifest",
  "apps": [
    { "id": "7zip-7zip", "refs": { "windows": "7zip.7zip" } },
    { "id": "git-git", "refs": { "windows": "Git.Git" } }
  ],
  "restore": [],
  "verify": []
}
'@
Set-Content -Path $script:TestManifestPath -Value $testManifest

Describe "JSON Mode - Pure stdout" {
    
    It "capabilities --json outputs valid JSON to stdout" {
            $output = & $script:EndstatePath capabilities --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            # Should be valid JSON
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
        }
        
        It "JSON output contains required envelope fields" {
            $output = & $script:EndstatePath capabilities --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            $json = $outputStr | ConvertFrom-Json
            
            $json.schemaVersion | Should -Be "1.0"
            $json.command | Should -Be "capabilities"
            $json.success | Should -Be $true
            $json.PSObject.Properties.Name | Should -Contain "cliVersion"
            $json.PSObject.Properties.Name | Should -Contain "timestampUtc"
            $json.PSObject.Properties.Name | Should -Contain "data"
            $json.PSObject.Properties.Name | Should -Contain "error"
        }
        
        It "JSON data contains commands list" {
            $output = & $script:EndstatePath capabilities --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            $json = $outputStr | ConvertFrom-Json
            
            $json.data.commands | Should -Contain "apply"
            $json.data.commands | Should -Contain "verify"
            $json.data.commands | Should -Contain "report"
            $json.data.commands | Should -Contain "capabilities"
        }
        
        It "Does not emit banner to stdout in JSON mode" {
            $output = & $script:EndstatePath capabilities --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            # Should not contain banner text in stdout
            $outputStr | Should -Not -Match "Automation Suite"
        }
    }
    
    Context "verify --json with missing manifest" {
        It "Returns JSON envelope with success:false and non-zero exit" {
            $output = pwsh -NoProfile -Command "& '$($script:EndstatePath)' verify --json 2>&1 | Where-Object { `$_ -is [string] }"
            $exitCode = $LASTEXITCODE
            $outputStr = $output -join "`n"
            
            # Should be valid JSON
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            
            $json = $outputStr | ConvertFrom-Json
            $json.success | Should -Be $false
            $json.command | Should -Be "verify"
            $json.error | Should -Not -BeNullOrEmpty
            $json.error.code | Should -Be "MANIFEST_NOT_FOUND"
            
            # Exit code should be non-zero
            $exitCode | Should -Be 1
        }
    }
    
    Context "verify --json with valid manifest" {
        BeforeEach {
            $env:ENDSTATE_WINGET_SCRIPT = $script:MockWingetPath
        }
        
        AfterEach {
            $env:ENDSTATE_WINGET_SCRIPT = $null
        }
        
        It "Returns JSON envelope with verify results" {
            $output = & $script:EndstatePath verify --manifest $script:TestManifestPath --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            # Should be valid JSON
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            
            $json = $outputStr | ConvertFrom-Json
            $json.success | Should -Be $true
            $json.command | Should -Be "verify"
            $json.data.okCount | Should -Be 2
            $json.data.missingCount | Should -Be 0
        }
    }
    
    Context "apply --json with valid manifest" {
        BeforeEach {
            $env:ENDSTATE_WINGET_SCRIPT = $script:MockWingetPath
        }
        
        AfterEach {
            $env:ENDSTATE_WINGET_SCRIPT = $null
        }
        
        It "Returns JSON envelope with apply results" {
            $output = & $script:EndstatePath apply --manifest $script:TestManifestPath --json -DryRun -OnlyApps 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            # Should be valid JSON
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            
            $json = $outputStr | ConvertFrom-Json
            $json.success | Should -Be $true
            $json.command | Should -Be "apply"
            $json.data.dryRun | Should -Be $true
            $json.data.PSObject.Properties.Name | Should -Contain "installed"
            $json.data.PSObject.Properties.Name | Should -Contain "skipped"
        }
    }
}

Describe "GNU-style Flag Support" {
    
    BeforeEach {
        $env:ENDSTATE_WINGET_SCRIPT = $script:MockWingetPath
    }
    
    AfterEach {
        $env:ENDSTATE_WINGET_SCRIPT = $null
    }
    
    Context "--json flag" {
        It "capabilities --json works" {
            $output = & $script:EndstatePath capabilities --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
        }
        
        It "verify --json works" {
            $output = & $script:EndstatePath verify --manifest $script:TestManifestPath --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
        }
    }
    
    Context "--profile flag" {
        It "verify --profile accepts profile name" {
            # This will fail because profile doesn't exist, but it should parse the flag
            $output = & $script:EndstatePath verify --profile NonExistent --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            # Should still be valid JSON (error response)
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            $json = $outputStr | ConvertFrom-Json
            $json.command | Should -Be "verify"
        }
        
        It "apply --profile accepts profile name" {
            $output = & $script:EndstatePath apply --profile NonExistent --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            $json = $outputStr | ConvertFrom-Json
            $json.command | Should -Be "apply"
        }
    }
    
    Context "--manifest flag" {
        It "verify --manifest accepts manifest path" {
            $output = & $script:EndstatePath verify --manifest $script:TestManifestPath --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            $json = $outputStr | ConvertFrom-Json
            $json.success | Should -Be $true
        }
        
        It "apply --manifest accepts manifest path" {
            $output = & $script:EndstatePath apply --manifest $script:TestManifestPath --json -DryRun -OnlyApps 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            $json = $outputStr | ConvertFrom-Json
            $json.success | Should -Be $true
        }
    }
    
    Context "Combined GNU flags" {
        It "apply --profile <name> --json works" {
            $output = & $script:EndstatePath apply --profile NonExistent --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
        }
        
        It "verify --manifest <path> --json works" {
            $output = & $script:EndstatePath verify --manifest $script:TestManifestPath --json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            $json = $outputStr | ConvertFrom-Json
            $json.success | Should -Be $true
        }
    }
    
    Context "Backward compatibility with PowerShell-style flags" {
        It "verify -Manifest still works" {
            $output = & $script:EndstatePath verify -Manifest $script:TestManifestPath -Json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
        }
        
        It "apply -Profile still works" {
            $output = & $script:EndstatePath apply -Profile NonExistent -Json 2>&1 | Where-Object { $_ -is [string] }
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
        }
    }
}

Describe "Error Handling in JSON Mode" {
    
    Context "Missing required parameters" {
        It "verify without profile/manifest returns JSON error" {
            $output = pwsh -NoProfile -Command "& '$($script:EndstatePath)' verify --json 2>&1 | Where-Object { `$_ -is [string] }"
            $exitCode = $LASTEXITCODE
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            $json = $outputStr | ConvertFrom-Json
            
            $json.success | Should -Be $false
            $json.error.code | Should -Be "MANIFEST_NOT_FOUND"
            $exitCode | Should -Be 1
        }
        
        It "apply without profile/manifest returns JSON error" {
            $output = pwsh -NoProfile -Command "& '$($script:EndstatePath)' apply --json 2>&1 | Where-Object { `$_ -is [string] }"
            $exitCode = $LASTEXITCODE
            $outputStr = $output -join "`n"
            
            { $outputStr | ConvertFrom-Json } | Should -Not -Throw
            $json = $outputStr | ConvertFrom-Json
            
            $json.success | Should -Be $false
            $json.error.code | Should -Be "MANIFEST_NOT_FOUND"
            $exitCode | Should -Be 1
        }
    }
    
    Context "Exit codes" {
        It "capabilities --json exits 0" {
            pwsh -NoProfile -Command "& '$($script:EndstatePath)' capabilities --json | Out-Null"
            $LASTEXITCODE | Should -Be 0
        }
        
        It "verify --json without manifest exits 1" {
            pwsh -NoProfile -Command "& '$($script:EndstatePath)' verify --json 2>&1 | Out-Null"
            $LASTEXITCODE | Should -Be 1
        }
        
        It "apply --json without manifest exits 1" {
            pwsh -NoProfile -Command "& '$($script:EndstatePath)' apply --json 2>&1 | Out-Null"
            $LASTEXITCODE | Should -Be 1
        }
    }
}
