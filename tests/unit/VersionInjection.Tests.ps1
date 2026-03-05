<#
.SYNOPSIS
    Pester tests for version injection: VERSION and SCHEMA_VERSION files
    are the single source of truth for all envelope and metadata construction.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."

    # Load json-output.ps1 (provides Get-EndstateVersion, Get-SchemaVersion, New-JsonEnvelope, Get-CapabilitiesData)
    . (Join-Path $script:ProvisioningRoot "engine\json-output.ps1")
    # Load bundle.ps1 (provides New-CaptureMetadata, now uses shared version functions)
    . (Join-Path $script:ProvisioningRoot "engine\logging.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\manifest.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\config-modules.ps1")
    . (Join-Path $script:ProvisioningRoot "engine\bundle.ps1")

    # Read expected values from version files
    $script:ExpectedCliVersion = (Get-Content -Path (Join-Path $script:ProvisioningRoot "VERSION") -Raw).Trim()
    $script:ExpectedSchemaVersion = (Get-Content -Path (Join-Path $script:ProvisioningRoot "SCHEMA_VERSION") -Raw).Trim()
}

Describe "VersionInjection" {

    Context "VERSION file format" {
        It "Contains a valid semver string" {
            $script:ExpectedCliVersion | Should -Match '^\d+\.\d+\.\d+$'
        }
    }

    Context "SCHEMA_VERSION file format" {
        It "Contains a valid major.minor string" {
            $script:ExpectedSchemaVersion | Should -Match '^\d+\.\d+$'
        }
    }

    Context "Get-EndstateVersion" {
        It "Returns the content of the VERSION file" {
            $result = Get-EndstateVersion
            $result | Should -Be $script:ExpectedCliVersion
        }
    }

    Context "Get-SchemaVersion" {
        It "Returns the content of the SCHEMA_VERSION file" {
            $result = Get-SchemaVersion
            $result | Should -Be $script:ExpectedSchemaVersion
        }
    }

    Context "New-JsonEnvelope uses file-based versions" {
        It "Populates cliVersion from VERSION file" {
            $envelope = New-JsonEnvelope -Command "test" -Success $true
            $envelope.cliVersion | Should -Be $script:ExpectedCliVersion
        }

        It "Populates schemaVersion from SCHEMA_VERSION file" {
            $envelope = New-JsonEnvelope -Command "test" -Success $true
            $envelope.schemaVersion | Should -Be $script:ExpectedSchemaVersion
        }
    }

    Context "New-CaptureMetadata uses shared version functions" {
        It "Populates endstateVersion from Get-EndstateVersion" {
            $metadata = New-CaptureMetadata
            $metadata.endstateVersion | Should -Be $script:ExpectedCliVersion
        }

        It "Populates schemaVersion from Get-SchemaVersion" {
            $metadata = New-CaptureMetadata
            $metadata.schemaVersion | Should -Be $script:ExpectedSchemaVersion
        }
    }

    Context "Get-CapabilitiesData supportedSchemaVersions" {
        It "Derives min from SCHEMA_VERSION file" {
            $caps = Get-CapabilitiesData
            $caps.supportedSchemaVersions.min | Should -Be $script:ExpectedSchemaVersion
        }

        It "Derives max from SCHEMA_VERSION file" {
            $caps = Get-CapabilitiesData
            $caps.supportedSchemaVersions.max | Should -Be $script:ExpectedSchemaVersion
        }
    }
}
