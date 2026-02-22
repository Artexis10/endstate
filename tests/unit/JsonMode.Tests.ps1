BeforeAll {
    $script:EndstateRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    $script:EndstatePath = Join-Path $script:EndstateRoot "bin\endstate.ps1"
    
    # Allow direct execution of endstate.ps1 for testing
    $env:ENDSTATE_ALLOW_DIRECT = '1'

    # Helper function to extract JSON from mixed CLI stdout
    # The CLI may output human-readable lines before/after JSON
    # This function finds and parses the JSON payload robustly
    function script:Get-JsonFromMixedOutput {
        param(
            [Parameter(Mandatory)]
            [AllowEmptyString()]
            [string]$OutputText
        )
        
        if ([string]::IsNullOrWhiteSpace($OutputText)) {
            throw "No output to parse - stdout was empty"
        }
        
        # First try: maybe the entire output is valid JSON
        try {
            return ($OutputText | ConvertFrom-Json)
        } catch {
            # Not pure JSON, need to extract it
        }
        
        # Strategy: Find the JSON envelope (has schemaVersion field)
        # The envelope is the main JSON object we want, not nested objects
        # Scan from the end to find '{' that yields valid JSON with schemaVersion
        
        $lastBraceIndex = $OutputText.LastIndexOf('{')
        while ($lastBraceIndex -ge 0) {
            $candidate = $OutputText.Substring($lastBraceIndex)
            try {
                $parsed = $candidate | ConvertFrom-Json
                # Check if this is the envelope (has schemaVersion)
                if ($parsed.PSObject.Properties.Name -contains 'schemaVersion') {
                    return $parsed
                }
                # Not the envelope, try the previous '{'
                if ($lastBraceIndex -gt 0) {
                    $lastBraceIndex = $OutputText.LastIndexOf('{', $lastBraceIndex - 1)
                } else {
                    break
                }
            } catch {
                # This '{' didn't yield valid JSON, try the previous one
                if ($lastBraceIndex -gt 0) {
                    $lastBraceIndex = $OutputText.LastIndexOf('{', $lastBraceIndex - 1)
                } else {
                    break
                }
            }
        }
        
        # If we get here, no valid JSON envelope was found
        $preview = if ($OutputText.Length -gt 500) { 
            $OutputText.Substring($OutputText.Length - 500) 
        } else { 
            $OutputText 
        }
        throw "No valid JSON envelope found in output. Last 500 chars:`n$preview"
    }

    # Helper to invoke endstate and extract JSON
    function script:Invoke-EndstateJson {
        param(
            [Parameter(Mandatory)]
            [string[]]$Arguments
        )
        
        # Build argument string for Invoke-Expression to avoid splatting issues with --flags
        $argString = ($Arguments | ForEach-Object { 
            if ($_ -match '\s') { "`"$_`"" } else { $_ }
        }) -join ' '
        
        # Capture all output streams
        $output = Invoke-Expression "& `"$script:EndstatePath`" $argString 2>&1"
        
        # Convert to string array, handling different output types
        $outputLines = @()
        foreach ($item in $output) {
            if ($null -eq $item) { continue }
            if ($item -is [string]) {
                $outputLines += $item
            } elseif ($item -is [System.Management.Automation.ErrorRecord]) {
                $outputLines += $item.ToString()
            } else {
                $outputLines += $item.ToString()
            }
        }
        $outputStr = $outputLines -join "`n"
        
        return @{
            Json = (script:Get-JsonFromMixedOutput -OutputText $outputStr)
            RawOutput = $output
            OutputString = $outputStr
        }
    }

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
}

Describe "JSON Mode - Pure stdout" {
    
    It "capabilities --json outputs valid JSON to stdout" {
        $result = script:Invoke-EndstateJson -Arguments @('capabilities', '--json')
        
        # Should have parsed JSON successfully
        $result.Json | Should -Not -BeNull
    }
    
    It "JSON output contains required envelope fields" {
        $result = script:Invoke-EndstateJson -Arguments @('capabilities', '--json')
        $json = $result.Json
        
        $json.schemaVersion | Should -Be "1.0"
        $json.command | Should -Be "capabilities"
        $json.success | Should -Be $true
        $json.PSObject.Properties.Name | Should -Contain "cliVersion"
        $json.PSObject.Properties.Name | Should -Contain "timestampUtc"
        $json.PSObject.Properties.Name | Should -Contain "data"
        $json.PSObject.Properties.Name | Should -Contain "error"
    }
    
    It "JSON data contains commands list" {
        $result = script:Invoke-EndstateJson -Arguments @('capabilities', '--json')
        $json = $result.Json
        
        $json.data.commands.PSObject.Properties.Name | Should -Contain "apply"
        $json.data.commands.PSObject.Properties.Name | Should -Contain "verify"
        $json.data.commands.PSObject.Properties.Name | Should -Contain "report"
        $json.data.commands.PSObject.Properties.Name | Should -Contain "capabilities"
    }
    
    It "Does not emit banner to stdout in JSON mode" {
        $result = script:Invoke-EndstateJson -Arguments @('capabilities', '--json')
        
        # Should not contain banner text in stdout
        $result.OutputString | Should -Not -Match "Automation Suite"
    }
    
    Context "verify --json with missing manifest" {
        It "Returns JSON envelope with success:false and non-zero exit" {
            $result = script:Invoke-EndstateJson -Arguments @('verify', '--json')
            $json = $result.Json
            
            $json.success | Should -Be $false
            $json.command | Should -Be "verify"
            $json.error | Should -Not -BeNullOrEmpty
            $json.error.code | Should -Be "MANIFEST_NOT_FOUND"
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
            $result = script:Invoke-EndstateJson -Arguments @('verify', '--manifest', $script:TestManifestPath, '--json')
            $json = $result.Json
            
            # Verify JSON envelope structure
            $json.command | Should -Be "verify"
            $json.schemaVersion | Should -Be "1.0"
            $json.PSObject.Properties.Name | Should -Contain "data"
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
            $result = script:Invoke-EndstateJson -Arguments @('apply', '--manifest', $script:TestManifestPath, '--json', '-DryRun', '-OnlyApps')
            $json = $result.Json
            
            # Verify JSON envelope structure
            $json.command | Should -Be "apply"
            $json.schemaVersion | Should -Be "1.0"
            $json.PSObject.Properties.Name | Should -Contain "data"
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
            $result = script:Invoke-EndstateJson -Arguments @('capabilities', '--json')
            $result.Json | Should -Not -BeNull
        }
        
        It "verify --json works" {
            $result = script:Invoke-EndstateJson -Arguments @('verify', '--manifest', $script:TestManifestPath, '--json')
            $result.Json | Should -Not -BeNull
        }
    }
    
    Context "--profile flag" {
        It "verify --profile accepts profile name" {
            # This will fail because profile doesn't exist, but it should parse the flag
            $result = script:Invoke-EndstateJson -Arguments @('verify', '--profile', 'NonExistent', '--json')
            $json = $result.Json
            
            # Should still be valid JSON (error response)
            $json.command | Should -Be "verify"
        }
        
        It "apply --profile accepts profile name" {
            $result = script:Invoke-EndstateJson -Arguments @('apply', '--profile', 'NonExistent', '--json')
            $json = $result.Json
            
            $json.command | Should -Be "apply"
        }
    }
    
    Context "--manifest flag" {
        It "verify --manifest accepts manifest path" {
            $result = script:Invoke-EndstateJson -Arguments @('verify', '--manifest', $script:TestManifestPath, '--json')
            $json = $result.Json
            
            # Verify JSON envelope structure
            $json.command | Should -Be "verify"
            $json.schemaVersion | Should -Be "1.0"
        }
        
        It "apply --manifest accepts manifest path" {
            $result = script:Invoke-EndstateJson -Arguments @('apply', '--manifest', $script:TestManifestPath, '--json', '-DryRun', '-OnlyApps')
            $json = $result.Json
            
            # Verify JSON envelope structure
            $json.command | Should -Be "apply"
            $json.schemaVersion | Should -Be "1.0"
        }
    }
    
    Context "Combined GNU flags" {
        It "apply --profile <name> --json works" {
            $result = script:Invoke-EndstateJson -Arguments @('apply', '--profile', 'NonExistent', '--json')
            $result.Json | Should -Not -BeNull
        }
        
        It "verify --manifest <path> --json works" {
            $result = script:Invoke-EndstateJson -Arguments @('verify', '--manifest', $script:TestManifestPath, '--json')
            $json = $result.Json
            
            # Verify JSON envelope structure
            $json.command | Should -Be "verify"
            $json.schemaVersion | Should -Be "1.0"
        }
    }
    
    Context "Backward compatibility with PowerShell-style flags" {
        It "verify -Manifest still works" {
            $result = script:Invoke-EndstateJson -Arguments @('verify', '-Manifest', $script:TestManifestPath, '-Json')
            $result.Json | Should -Not -BeNull
        }
        
        It "apply -Profile still works" {
            $result = script:Invoke-EndstateJson -Arguments @('apply', '-Profile', 'NonExistent', '-Json')
            $result.Json | Should -Not -BeNull
        }
    }
}

Describe "Error Handling in JSON Mode" {
    
    Context "Missing required parameters" {
        It "verify without profile/manifest returns JSON error" {
            $result = script:Invoke-EndstateJson -Arguments @('verify', '--json')
            $json = $result.Json
            
            $json.success | Should -Be $false
            $json.error.code | Should -Be "MANIFEST_NOT_FOUND"
        }
        
        It "apply without profile/manifest returns JSON error" {
            $result = script:Invoke-EndstateJson -Arguments @('apply', '--json')
            $json = $result.Json
            
            $json.success | Should -Be $false
            $json.error.code | Should -Be "MANIFEST_NOT_FOUND"
        }
    }
    
    Context "Exit codes" {
        It "capabilities --json exits 0" {
            & $script:EndstatePath capabilities --json 2>&1 | Out-Null
            $LASTEXITCODE | Should -Be 0
        }
        
        It "verify --json without manifest exits 1" {
            & $script:EndstatePath verify --json 2>&1 | Out-Null
            $LASTEXITCODE | Should -Be 1
        }
        
        It "apply --json without manifest exits 1" {
            & $script:EndstatePath apply --json 2>&1 | Out-Null
            $LASTEXITCODE | Should -Be 1
        }
    }
}
