<#
.SYNOPSIS
    Pester tests for manifest parsing (YAML, JSONC, includes).
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    $script:ManifestScript = Join-Path $script:ProvisioningRoot "engine\manifest.ps1"
    $script:FixturesDir = Join-Path $PSScriptRoot "..\fixtures"
    
    # Load dependencies (Pester 3.x compatible - no BeforeAll at script level)
    . $script:ManifestScript
}

Describe "Manifest.Yaml.Parses" {
    
    Context "Sample YAML manifest parsing" {
        
        It "Should parse sample YAML manifest and return hashtable" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest | Should -Not -BeNullOrEmpty
            $manifest | Should -BeOfType [hashtable]
        }
        
        It "Should have correct version field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.version | Should -Be 1
        }
        
        It "Should have correct name field" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.name | Should -Be "test-manifest"
        }
        
        It "Should have captured timestamp" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.captured | Should -Be "2025-01-01T00:00:00Z"
        }
        
        It "Should parse apps array with correct count" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.apps | Should -Not -BeNullOrEmpty
            $manifest.apps.Count | Should -Be 3
        }
        
        It "Should parse app id correctly" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.apps[0].id | Should -Be "test-app-1"
        }
        
        It "Should parse app refs.windows correctly" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.apps[0].refs.windows | Should -Be "Test.App1"
        }
        
        It "Should parse multi-platform refs" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.apps[1].refs.windows | Should -Be "Test.App2"
            $manifest.apps[1].refs.linux | Should -Be "test-app-2"
        }
        
        It "Should parse restore array" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.restore | Should -Not -BeNullOrEmpty
            $manifest.restore.Count | Should -Be 1
            $manifest.restore[0].type | Should -Be "copy"
        }
        
        It "Should parse verify array" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            $manifest = Read-Manifest -Path $yamlPath
            
            $manifest.verify | Should -Not -BeNullOrEmpty
            $manifest.verify.Count | Should -Be 1
            $manifest.verify[0].type | Should -Be "file-exists"
        }
    }
    
    Context "YAML parsing stability" {
        
        It "Should produce identical output on repeated parsing" {
            $yamlPath = Join-Path $script:FixturesDir "sample-manifest.yaml"
            
            $manifest1 = Read-Manifest -Path $yamlPath
            $manifest2 = Read-Manifest -Path $yamlPath
            
            # Compare key fields
            $manifest1.version | Should -Be $manifest2.version
            $manifest1.name | Should -Be $manifest2.name
            $manifest1.apps.Count | Should -Be $manifest2.apps.Count
            
            for ($i = 0; $i -lt $manifest1.apps.Count; $i++) {
                $manifest1.apps[$i].id | Should -Be $manifest2.apps[$i].id
                $manifest1.apps[$i].refs.windows | Should -Be $manifest2.apps[$i].refs.windows
            }
        }
    }
}

Describe "Manifest.Jsonc.Includes.Parses" {
    
    Context "JSONC manifest with includes" {
        
        It "Should parse JSONC manifest with includes" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $manifest | Should -Not -BeNullOrEmpty
            $manifest | Should -BeOfType [hashtable]
        }
        
        It "Should have correct root manifest fields" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $manifest.version | Should -Be 1
            $manifest.name | Should -Be "main-with-includes"
        }
        
        It "Should merge apps from included file" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            # Should have local app + 2 from base-apps.jsonc = 3 total
            $manifest.apps.Count | Should -Be 3
        }
        
        It "Should contain local app from root manifest" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $localApp = $manifest.apps | Where-Object { $_.id -eq "local-app-1" }
            $localApp | Should -Not -BeNullOrEmpty
            $localApp.refs.windows | Should -Be "Local.App1"
        }
        
        It "Should contain apps from included base-apps.jsonc" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $baseApp1 = $manifest.apps | Where-Object { $_.id -eq "base-app-1" }
            $baseApp2 = $manifest.apps | Where-Object { $_.id -eq "base-app-2" }
            
            $baseApp1 | Should -Not -BeNullOrEmpty
            $baseApp1.refs.windows | Should -Be "Base.App1"
            
            $baseApp2 | Should -Not -BeNullOrEmpty
            $baseApp2.refs.windows | Should -Be "Base.App2"
        }
        
        It "Should preserve multi-platform refs from included file" {
            $jsoncPath = Join-Path $script:FixturesDir "main-with-includes.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            $baseApp2 = $manifest.apps | Where-Object { $_.id -eq "base-app-2" }
            $baseApp2.refs.linux | Should -Be "base-app-2"
        }
    }
    
    Context "JSONC comment stripping (PS5.1+ compatible)" {
        
        It "Should strip single-line comments from base-apps.jsonc" {
            $jsoncPath = Join-Path $script:FixturesDir "base-apps.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            # If comments weren't stripped, parsing would fail
            $manifest | Should -Not -BeNullOrEmpty
            $manifest.apps | Should -Not -BeNullOrEmpty
        }
        
        It "Should parse JSONC with header comments like hugo-desktop.jsonc (regression test)" {
            # Regression test: ensure manifests with header comments at lines 2-6 parse correctly
            # This is the primary bug fix - manifests were failing with "Invalid object passed in, ':' or '}' expected. (6)"
            $testManifestPath = Join-Path $script:ProvisioningRoot "manifests\fixture-test.jsonc"
            
            # fixture-test.jsonc has comments at lines 2-4
            $manifest = Read-Manifest -Path $testManifestPath
            
            # Should parse successfully on both PS5.1 and PS7+
            $manifest | Should -Not -BeNullOrEmpty
            $manifest.version | Should -Be 1
            $manifest.name | Should -Be "fixture-test"
            $manifest.apps | Should -Not -BeNullOrEmpty
        }
        
        It "Should parse JSONC with inline comments after values" {
            # Test inline // comments after JSON values
            $tempDir = Join-Path $script:ProvisioningRoot "state\temp-test"
            if (-not (Test-Path $tempDir)) {
                New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
            }
            
            $tempFile = Join-Path $tempDir "inline-comments-test.jsonc"
            $content = @"
{
  "version": 1, // version comment
  "name": "test",
  "apps": [
    {
      "id": "test-app",
      "refs": {
        "windows": "Test.App" // platform ref
      }
    }
  ]
}
"@
            $content | Out-File -FilePath $tempFile -Encoding UTF8 -NoNewline
            
            try {
                $manifest = Read-Manifest -Path $tempFile
                
                $manifest | Should -Not -BeNullOrEmpty
                $manifest.version | Should -Be 1
                $manifest.name | Should -Be "test"
                $manifest.apps[0].id | Should -Be "test-app"
            } finally {
                if (Test-Path $tempFile) {
                    Remove-Item $tempFile -Force
                }
            }
        }
        
        It "Should parse JSONC with multi-line /* */ comments" {
            # Test block comments
            $tempDir = Join-Path $script:ProvisioningRoot "state\temp-test"
            if (-not (Test-Path $tempDir)) {
                New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
            }
            
            $tempFile = Join-Path $tempDir "multiline-comments-test.jsonc"
            $content = @"
{
  /* This is a multi-line comment
     spanning multiple lines */
  "version": 1,
  "name": "test",
  "apps": []
}
"@
            $content | Out-File -FilePath $tempFile -Encoding UTF8 -NoNewline
            
            try {
                $manifest = Read-Manifest -Path $tempFile
                
                $manifest | Should -Not -BeNullOrEmpty
                $manifest.version | Should -Be 1
                $manifest.name | Should -Be "test"
            } finally {
                if (Test-Path $tempFile) {
                    Remove-Item $tempFile -Force
                }
            }
        }
        
        It "Should preserve http:// URLs inside strings (do not strip as comment)" {
            # Critical test: ensure we don't strip // inside JSON strings
            $tempDir = Join-Path $script:ProvisioningRoot "state\temp-test"
            if (-not (Test-Path $tempDir)) {
                New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
            }
            
            $tempFile = Join-Path $tempDir "url-test.jsonc"
            $content = @"
{
  "version": 1,
  "name": "test",
  "homepage": "http://example.com",
  "docs": "https://example.com/docs",
  "apps": []
}
"@
            $content | Out-File -FilePath $tempFile -Encoding UTF8 -NoNewline
            
            try {
                $manifest = Read-Manifest -Path $tempFile
                
                $manifest | Should -Not -BeNullOrEmpty
                $manifest.homepage | Should -Be "http://example.com"
                $manifest.docs | Should -Be "https://example.com/docs"
            } finally {
                if (Test-Path $tempFile) {
                    Remove-Item $tempFile -Force
                }
            }
        }
        
        It "Should work on Windows PowerShell 5.1 and PowerShell 7+" {
            # Verify PS version compatibility
            $psVersion = $PSVersionTable.PSVersion.Major
            
            # This test should pass on both PS5.1 and PS7+
            $tempDir = Join-Path $script:ProvisioningRoot "state\temp-test"
            if (-not (Test-Path $tempDir)) {
                New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
            }
            
            $tempFile = Join-Path $tempDir "ps-version-test.jsonc"
            $content = @"
{
  // Comment at top
  "version": 1,
  "name": "ps-version-test",
  "psVersion": $psVersion,
  "apps": []
}
"@
            $content | Out-File -FilePath $tempFile -Encoding UTF8 -NoNewline
            
            try {
                # Should not throw on either PS version
                { Read-Manifest -Path $tempFile } | Should -Not -Throw
                
                $manifest = Read-Manifest -Path $tempFile
                $manifest | Should -Not -BeNullOrEmpty
                $manifest.version | Should -Be 1
                
                # Verify we got a hashtable (not PSCustomObject)
                $manifest | Should -BeOfType [hashtable]
            } finally {
                if (Test-Path $tempFile) {
                    Remove-Item $tempFile -Force
                }
            }
        }
        
        It "Should handle CRLF line endings correctly" {
            # Ensure CRLF line endings are preserved/handled correctly
            $tempDir = Join-Path $script:ProvisioningRoot "state\temp-test"
            if (-not (Test-Path $tempDir)) {
                New-Item -ItemType Directory -Path $tempDir -Force | Out-Null
            }
            
            $tempFile = Join-Path $tempDir "crlf-test.jsonc"
            # Explicitly use CRLF
            $content = "{`r`n  // Comment with CRLF`r`n  `"version`": 1,`r`n  `"name`": `"test`",`r`n  `"apps`": []`r`n}"
            $content | Out-File -FilePath $tempFile -Encoding UTF8 -NoNewline
            
            try {
                $manifest = Read-Manifest -Path $tempFile
                
                $manifest | Should -Not -BeNullOrEmpty
                $manifest.version | Should -Be 1
                $manifest.name | Should -Be "test"
            } finally {
                if (Test-Path $tempFile) {
                    Remove-Item $tempFile -Force
                }
            }
        }
    }
}

Describe "Manifest.Normalization" {
    
    Context "Default field initialization" {
        
        It "Should initialize missing arrays to empty" {
            $jsoncPath = Join-Path $script:FixturesDir "base-apps.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            # base-apps.jsonc has no restore/verify sections - they should default to empty arrays
            $manifest.ContainsKey('restore') | Should -Be $true
            ($null -eq $manifest.restore) | Should -Be $false
            @($manifest.restore).Count | Should -Be 0
            
            $manifest.ContainsKey('verify') | Should -Be $true
            ($null -eq $manifest.verify) | Should -Be $false
            @($manifest.verify).Count | Should -Be 0
        }
        
        It "Should set default version if missing" {
            $jsoncPath = Join-Path $script:FixturesDir "base-apps.jsonc"
            $manifest = Read-Manifest -Path $jsoncPath
            
            # base-apps.jsonc has no version field
            $manifest.version | Should -Be 1
        }
    }
}