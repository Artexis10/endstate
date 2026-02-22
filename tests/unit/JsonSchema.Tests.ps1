<#
.SYNOPSIS
    Pester tests for CLI JSON schema contract v1.0.
.DESCRIPTION
    These tests verify that JSON outputs conform to the documented schema,
    ensuring the contract between Endstate CLI and GUI consumers is stable.
#>

BeforeAll {
    $script:EngineRoot = Join-Path $PSScriptRoot "..\..\engine"
    $script:DriverScript = Join-Path $PSScriptRoot "..\..\drivers\driver.ps1"
    $script:PathsScript = Join-Path $PSScriptRoot "..\..\engine\paths.ps1"
    
    . $script:PathsScript
    . $script:DriverScript
    . "$PSScriptRoot\..\..\engine\json-output.ps1"
    
    # Initialize drivers so Get-RegisteredDrivers returns data
    Reset-DriversState
    Initialize-Drivers
    
    # Helper function to extract JSON envelope from mixed CLI stdout
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
        $lastBraceIndex = $OutputText.LastIndexOf('{')
        while ($lastBraceIndex -ge 0) {
            $candidate = $OutputText.Substring($lastBraceIndex)
            try {
                $parsed = $candidate | ConvertFrom-Json
                if ($parsed.PSObject.Properties.Name -contains 'schemaVersion') {
                    return $parsed
                }
                if ($lastBraceIndex -gt 0) {
                    $lastBraceIndex = $OutputText.LastIndexOf('{', $lastBraceIndex - 1)
                } else {
                    break
                }
            } catch {
                if ($lastBraceIndex -gt 0) {
                    $lastBraceIndex = $OutputText.LastIndexOf('{', $lastBraceIndex - 1)
                } else {
                    break
                }
            }
        }
        
        throw "No valid JSON envelope found in output"
    }
}

Describe "JSON Schema Contract v1.0" {
    
    Context "Schema Version" {
        
        It "Should return schema version 1.0" {
            $version = Get-SchemaVersion
            $version | Should -Be "1.0"
        }
        
        It "Should return a valid CLI version" {
            $version = Get-EndstateVersion
            $version | Should -Not -BeNullOrEmpty
            # Should match semver pattern (with optional prerelease/build metadata)
            $version | Should -Match "^\d+\.\d+\.\d+(-[a-zA-Z0-9.+-]+)?$"
        }
    }
    
    Context "JSON Envelope Structure" {
        
        It "Should create envelope with all required fields" {
            $envelope = New-JsonEnvelope -Command "test" -Success $true -Data @{ foo = "bar" }
            
            $envelope.schemaVersion | Should -Not -BeNullOrEmpty
            $envelope.cliVersion | Should -Not -BeNullOrEmpty
            $envelope.command | Should -Be "test"
            $envelope.runId | Should -Not -BeNullOrEmpty
            $envelope.timestampUtc | Should -Not -BeNullOrEmpty
            $envelope.success | Should -Be $true
            $envelope.data | Should -Not -BeNull
            $envelope.error | Should -BeNull
        }
        
        It "Should use provided runId when specified" {
            $envelope = New-JsonEnvelope -Command "test" -RunId "20241220-120000" -Success $true
            
            $envelope.runId | Should -Be "20241220-120000"
        }
        
        It "Should generate runId in correct format when not provided" {
            $envelope = New-JsonEnvelope -Command "test" -Success $true
            
            # Format: yyyyMMdd-HHmmss (per contract)
            $envelope.runId | Should -Match "^\d{8}-\d{6}$"
        }
        
        It "Should include timestampUtc in ISO 8601 format" {
            $envelope = New-JsonEnvelope -Command "test" -Success $true
            
            # ISO 8601 UTC format: yyyy-MM-ddTHH:mm:ssZ
            $envelope.timestampUtc | Should -Match "^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}Z$"
        }
        
        It "Should set success to false when specified" {
            $envelope = New-JsonEnvelope -Command "test" -Success $false
            
            $envelope.success | Should -Be $false
        }
        
        It "Should include error object when provided" {
            $errorObj = New-JsonError -Code "TEST_ERROR" -Message "Test error message"
            $envelope = New-JsonEnvelope -Command "test" -Success $false -Error $errorObj
            
            $envelope.error | Should -Not -BeNull
            $envelope.error.code | Should -Be "TEST_ERROR"
        }
        
        It "Should serialize to valid JSON" {
            $envelope = New-JsonEnvelope -Command "test" -Success $true -Data @{ items = @(1, 2, 3) }
            $json = ConvertTo-JsonOutput -Envelope $envelope
            
            $json | Should -Not -BeNullOrEmpty
            
            # Should be parseable
            $parsed = $json | ConvertFrom-Json
            $parsed.command | Should -Be "test"
        }
    }
    
    Context "Error Object Structure" {
        
        It "Should create error with required fields" {
            $errorObj = New-JsonError -Code "MANIFEST_NOT_FOUND" -Message "File not found"
            
            $errorObj.code | Should -Be "MANIFEST_NOT_FOUND"
            $errorObj.message | Should -Be "File not found"
        }
        
        It "Should include optional detail when provided" {
            $errorObj = New-JsonError -Code "TEST" -Message "Test" -Detail @{ path = "C:\test" }
            
            $errorObj.detail | Should -Not -BeNull
            $errorObj.detail.path | Should -Be "C:\test"
        }
        
        It "Should include optional remediation when provided" {
            $errorObj = New-JsonError -Code "TEST" -Message "Test" -Remediation "Try this fix"
            
            $errorObj.remediation | Should -Be "Try this fix"
        }
        
        It "Should include optional docsKey when provided" {
            $errorObj = New-JsonError -Code "TEST" -Message "Test" -DocsKey "errors/test"
            
            $errorObj.docsKey | Should -Be "errors/test"
        }
        
        It "Should not include optional fields when not provided" {
            $errorObj = New-JsonError -Code "TEST" -Message "Test"
            
            $errorObj.Keys | Should -Not -Contain "detail"
            $errorObj.Keys | Should -Not -Contain "remediation"
            $errorObj.Keys | Should -Not -Contain "docsKey"
        }
    }
    
    Context "Capabilities Data Structure" {
        
        It "Should return capabilities with all required sections" {
            $caps = Get-CapabilitiesData
            
            $caps.supportedSchemaVersions | Should -Not -BeNull
            $caps.commands | Should -Not -BeNull
            $caps.features | Should -Not -BeNull
            $caps.platform | Should -Not -BeNull
        }
        
        It "Should include min/max schema versions" {
            $caps = Get-CapabilitiesData
            
            $caps.supportedSchemaVersions.min | Should -Be "1.0"
            $caps.supportedSchemaVersions.max | Should -Be "1.0"
        }
        
        It "Should list all core commands" {
            $caps = Get-CapabilitiesData
            
            $caps.commands.capture.supported | Should -Be $true
            $caps.commands.plan.supported | Should -Be $true
            $caps.commands.apply.supported | Should -Be $true
            $caps.commands.verify.supported | Should -Be $true
            $caps.commands.report.supported | Should -Be $true
            $caps.commands.capabilities.supported | Should -Be $true
        }
        
        It "Should include command flags" {
            $caps = Get-CapabilitiesData
            
            $caps.commands.apply.flags | Should -Contain "--manifest"
            $caps.commands.apply.flags | Should -Contain "--dry-run"
            $caps.commands.apply.flags | Should -Contain "--json"
        }
        
        It "Should report platform information" {
            $caps = Get-CapabilitiesData
            
            $caps.platform.os | Should -Be "windows"
            $caps.platform.drivers | Should -Contain "winget"
        }
        
        It "Should report feature flags" {
            $caps = Get-CapabilitiesData
            
            $caps.features.jsonOutput | Should -Be $true
        }
    }
    
    Context "Error Codes" {
        
        It "Should return known error codes" {
            Get-ErrorCode -Name "MANIFEST_NOT_FOUND" | Should -Be "MANIFEST_NOT_FOUND"
            Get-ErrorCode -Name "INSTALL_FAILED" | Should -Be "INSTALL_FAILED"
            Get-ErrorCode -Name "SCHEMA_INCOMPATIBLE" | Should -Be "SCHEMA_INCOMPATIBLE"
        }
        
        It "Should return INTERNAL_ERROR for unknown codes" {
            Get-ErrorCode -Name "UNKNOWN_CODE" | Should -Be "INTERNAL_ERROR"
        }
    }
    
    Context "Envelope Field Order" {
        
        It "Should maintain consistent field order for stable output" {
            $envelope = New-JsonEnvelope -Command "test" -Success $true
            $keys = @($envelope.Keys)
            
            # Verify order matches contract
            $keys[0] | Should -Be "schemaVersion"
            $keys[1] | Should -Be "cliVersion"
            $keys[2] | Should -Be "command"
            $keys[3] | Should -Be "runId"
            $keys[4] | Should -Be "timestampUtc"
            $keys[5] | Should -Be "success"
            $keys[6] | Should -Be "data"
            $keys[7] | Should -Be "error"
        }
    }
}

Describe "Capabilities Command Integration" {
    # These are integration tests that call the full CLI
    
    BeforeAll {
        $script:EndstatePath = Join-Path $PSScriptRoot "..\..\bin\endstate.ps1"
        $env:ENDSTATE_ALLOW_DIRECT = '1'
    }
    
    # Pre-existing CI-gated integration test
    It "Should output valid JSON when -Json flag is used" -Skip:($env:CI -eq "true") {
        $output = & $script:EndstatePath capabilities --json 2>&1
        $outputText = ($output | ForEach-Object { $_.ToString() }) -join "`n"
        $parsed = script:Get-JsonFromMixedOutput -OutputText $outputText
        
        # Should have parsed JSON successfully
        $parsed | Should -Not -BeNull
    }
    
    It "Should include schemaVersion in capabilities output" -Skip:($env:CI -eq "true") {
        $output = & $script:EndstatePath capabilities --json 2>&1
        $outputText = ($output | ForEach-Object { $_.ToString() }) -join "`n"
        $parsed = script:Get-JsonFromMixedOutput -OutputText $outputText
        
        $parsed.schemaVersion | Should -Be "1.0"
    }
    
    It "Should include cliVersion in capabilities output" -Skip:($env:CI -eq "true") {
        $output = & $script:EndstatePath capabilities --json 2>&1
        $outputText = ($output | ForEach-Object { $_.ToString() }) -join "`n"
        $parsed = script:Get-JsonFromMixedOutput -OutputText $outputText
        
        $parsed.cliVersion | Should -Not -BeNullOrEmpty
    }
    
    It "Should report success=true for capabilities" -Skip:($env:CI -eq "true") {
        $output = & $script:EndstatePath capabilities --json 2>&1
        $outputText = ($output | ForEach-Object { $_.ToString() }) -join "`n"
        $parsed = script:Get-JsonFromMixedOutput -OutputText $outputText
        
        $parsed.success | Should -Be $true
    }
}
