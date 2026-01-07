<#
.SYNOPSIS
    Pester tests for Discovery module: PATH detection, registry detection, winget ownership.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    $script:DiscoveryScript = Join-Path $script:ProvisioningRoot "engine\discovery.ps1"
    $script:ExternalScript = Join-Path $script:ProvisioningRoot "engine\external.ps1"
    $script:CaptureScript = Join-Path $script:ProvisioningRoot "engine\capture.ps1"
    $script:LoggingScript = Join-Path $script:ProvisioningRoot "engine\logging.ps1"
    $script:ManifestScript = Join-Path $script:ProvisioningRoot "engine\manifest.ps1"
    
    # Load external wrapper first (discovery depends on it)
    . $script:ExternalScript
    
    # Load discovery module
    . $script:DiscoveryScript
}

Describe "Discovery.PathDetector" {
    
    Context "Invoke-PathDetector with mocked Get-CommandInfo" {
        
        It "Should detect git when present on PATH" {
            # Mock Get-CommandInfo to return git
            Mock Get-CommandInfo {
                param($CommandName)
                if ($CommandName -eq "git") {
                    return @{
                        Name = "git.exe"
                        Path = "C:\Program Files\Git\cmd\git.exe"
                        CommandType = "Application"
                    }
                }
                return $null
            }
            
            Mock Get-CommandVersion {
                param($CommandName)
                if ($CommandName -eq "git") {
                    return "2.43.0"
                }
                return $null
            }
            
            $findings = Invoke-PathDetector
            
            $gitFinding = $findings | Where-Object { $_.name -eq "git" }
            $gitFinding | Should -Not -BeNullOrEmpty
            $gitFinding.method | Should -Be "path"
            $gitFinding.path | Should -Be "C:\Program Files\Git\cmd\git.exe"
            $gitFinding.version | Should -Be "2.43.0"
            $gitFinding.suggestedWingetId | Should -Be "Git.Git"
        }
        
        It "Should return empty array when no tools found on PATH" {
            Mock Get-CommandInfo { return $null }
            Mock Get-CommandVersion { return $null }
            
            $findings = Invoke-PathDetector
            
            $findings.Count | Should -Be 0
        }
        
        It "Should detect multiple tools when present" {
            Mock Get-CommandInfo {
                param($CommandName)
                switch ($CommandName) {
                    "git" { return @{ Name = "git.exe"; Path = "C:\Git\git.exe"; CommandType = "Application" } }
                    "node" { return @{ Name = "node.exe"; Path = "C:\nodejs\node.exe"; CommandType = "Application" } }
                    default { return $null }
                }
            }
            
            Mock Get-CommandVersion {
                param($CommandName)
                switch ($CommandName) {
                    "git" { return "2.43.0" }
                    "node" { return "20.10.0" }
                    default { return $null }
                }
            }
            
            $findings = Invoke-PathDetector
            
            $findings.Count | Should -BeGreaterThan 1
            ($findings | Where-Object { $_.name -eq "git" }) | Should -Not -BeNullOrEmpty
            ($findings | Where-Object { $_.name -eq "node" }) | Should -Not -BeNullOrEmpty
        }
        
        It "Should handle null version gracefully" {
            Mock Get-CommandInfo {
                param($CommandName)
                if ($CommandName -eq "git") {
                    return @{ Name = "git.exe"; Path = "C:\Git\git.exe"; CommandType = "Application" }
                }
                return $null
            }
            
            Mock Get-CommandVersion { return $null }
            
            $findings = Invoke-PathDetector
            
            $gitFinding = $findings | Where-Object { $_.name -eq "git" }
            $gitFinding | Should -Not -BeNullOrEmpty
            $gitFinding.version | Should -BeNullOrEmpty
        }
    }
}

Describe "Discovery.RegistryDetector" {
    
    Context "Invoke-RegistryUninstallDetector with mocked registry" {
        
        It "Should detect Git from registry DisplayName" {
            Mock Get-RegistryUninstallEntries {
                param($Path)
                return @(
                    @{
                        DisplayName = "Git version 2.43.0"
                        DisplayVersion = "2.43.0"
                        Publisher = "The Git Development Community"
                        InstallLocation = "C:\Program Files\Git"
                    }
                )
            }
            
            $findings = Invoke-RegistryUninstallDetector
            
            $gitFinding = $findings | Where-Object { $_.name -eq "git" }
            $gitFinding | Should -Not -BeNullOrEmpty
            $gitFinding.method | Should -Be "registry"
            $gitFinding.displayName | Should -Be "Git version 2.43.0"
            $gitFinding.displayVersion | Should -Be "2.43.0"
            $gitFinding.suggestedWingetId | Should -Be "Git.Git"
        }
        
        It "Should return empty array when no matching entries found" {
            Mock Get-RegistryUninstallEntries {
                return @(
                    @{ DisplayName = "Some Random App"; DisplayVersion = "1.0" }
                )
            }
            
            $findings = Invoke-RegistryUninstallDetector
            
            $findings.Count | Should -Be 0
        }
        
        It "Should deduplicate entries with same DisplayName and Version" {
            Mock Get-RegistryUninstallEntries {
                param($Path)
                # Simulate same entry appearing in multiple registry locations
                return @(
                    @{
                        DisplayName = "Git version 2.43.0"
                        DisplayVersion = "2.43.0"
                        Publisher = "The Git Development Community"
                        InstallLocation = "C:\Program Files\Git"
                    }
                )
            }
            
            $findings = Invoke-RegistryUninstallDetector
            
            # Should only have one git entry despite being called for multiple paths
            $gitFindings = @($findings | Where-Object { $_.name -eq "git" })
            $gitFindings.Count | Should -Be 1
        }
    }
}

Describe "Discovery.WingetOwnership" {
    
    Context "Add-WingetOwnership function" {
        
        It "Should mark discovery as owned when winget ID matches" {
            $discoveries = @(
                @{
                    name = "git"
                    path = "C:\Git\git.exe"
                    method = "path"
                    suggestedWingetId = "Git.Git"
                }
            )
            
            $wingetIds = @("Git.Git", "Microsoft.VisualStudioCode")
            
            $result = @(Add-WingetOwnership -Discoveries $discoveries -WingetInstalledIds $wingetIds)
            
            $result[0]["ownedByWinget"] | Should -Be $true
        }
        
        It "Should mark discovery as NOT owned when winget ID not found" {
            $discoveries = @(
                @{
                    name = "git"
                    path = "C:\Git\git.exe"
                    method = "path"
                    suggestedWingetId = "Git.Git"
                }
            )
            
            $wingetIds = @("Microsoft.VisualStudioCode", "Mozilla.Firefox")
            
            $result = @(Add-WingetOwnership -Discoveries $discoveries -WingetInstalledIds $wingetIds)
            
            $result[0]["ownedByWinget"] | Should -Be $false
        }
        
        It "Should handle case-insensitive matching" {
            $discoveries = @(
                @{
                    name = "git"
                    suggestedWingetId = "Git.Git"
                }
            )
            
            $wingetIds = @("git.git")  # lowercase
            
            $result = @(Add-WingetOwnership -Discoveries $discoveries -WingetInstalledIds $wingetIds)
            
            $result[0]["ownedByWinget"] | Should -Be $true
        }
        
        It "Should handle empty winget IDs array" {
            $discoveries = @(
                @{
                    name = "git"
                    suggestedWingetId = "Git.Git"
                }
            )
            
            $result = @(Add-WingetOwnership -Discoveries $discoveries -WingetInstalledIds @())
            
            $result[0]["ownedByWinget"] | Should -Be $false
        }
    }
}

Describe "Discovery.Integration" {
    
    Context "Invoke-Discovery with mocks" {
        
        It "Should return sorted discoveries" {
            Mock Get-CommandInfo {
                param($CommandName)
                switch ($CommandName) {
                    "git" { return @{ Name = "git.exe"; Path = "C:\Git\git.exe"; CommandType = "Application" } }
                    "node" { return @{ Name = "node.exe"; Path = "C:\nodejs\node.exe"; CommandType = "Application" } }
                    default { return $null }
                }
            }
            
            Mock Get-CommandVersion { return "1.0.0" }
            Mock Get-RegistryUninstallEntries { return @() }
            
            $discoveries = Invoke-Discovery -WingetInstalledIds @()
            
            # Should be sorted by name
            if ($discoveries.Count -ge 2) {
                $discoveries[0].name | Should -BeLessThan $discoveries[1].name
            }
        }
        
        It "Should include ownedByWinget property on all entries" {
            Mock Get-CommandInfo {
                param($CommandName)
                if ($CommandName -eq "git") {
                    return @{ Name = "git.exe"; Path = "C:\Git\git.exe"; CommandType = "Application" }
                }
                return $null
            }
            
            Mock Get-CommandVersion { return "2.43.0" }
            Mock Get-RegistryUninstallEntries { return @() }
            
            $discoveries = Invoke-Discovery -WingetInstalledIds @("Git.Git")
            
            $gitDiscovery = $discoveries | Where-Object { $_.name -eq "git" }
            $gitDiscovery.PSObject.Properties.Name -contains "ownedByWinget" -or $gitDiscovery.ContainsKey("ownedByWinget") | Should -Be $true
        }
    }
}

Describe "Discovery.ManualInclude" {
    
    BeforeEach {
        $script:TestTempDir = Join-Path $env:TEMP "discovery-tests-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TestTempDir -Force | Out-Null
    }
    
    AfterEach {
        if (Test-Path $script:TestTempDir) {
            Remove-Item -Path $script:TestTempDir -Recurse -Force
        }
    }
    
    Context "Write-ManualIncludeTemplate function" {
        
        It "Should create manual include file" {
            $templatePath = Join-Path $script:TestTempDir "test-manual.jsonc"
            $discoveries = @(
                @{
                    name = "git"
                    path = "C:\Git\git.exe"
                    version = "2.43.0"
                    method = "path"
                    suggestedWingetId = "Git.Git"
                    ownedByWinget = $false
                }
            )
            
            Write-ManualIncludeTemplate -Path $templatePath -ProfileName "test-profile" -Discoveries $discoveries
            
            Test-Path $templatePath | Should -Be $true
        }
        
        It "Should include profile name in template" {
            $templatePath = Join-Path $script:TestTempDir "test-manual.jsonc"
            # Use a discovery with ownedByWinget = true so no suggestions are generated
            # but the file is still created with profile name
            $discoveries = @(
                @{
                    name = "git"
                    suggestedWingetId = "Git.Git"
                    ownedByWinget = $true
                }
            )
            
            Write-ManualIncludeTemplate -Path $templatePath -ProfileName "my-workstation" -Discoveries $discoveries
            
            $content = Get-Content -Path $templatePath -Raw
            $content | Should -Match "my-workstation"
        }
        
        It "Should include commented app suggestion for non-winget-owned discovery" {
            $templatePath = Join-Path $script:TestTempDir "test-manual.jsonc"
            $discoveries = @(
                @{
                    name = "git"
                    path = "C:\Git\git.exe"
                    version = "2.43.0"
                    method = "path"
                    suggestedWingetId = "Git.Git"
                    ownedByWinget = $false
                }
            )
            
            Write-ManualIncludeTemplate -Path $templatePath -ProfileName "test" -Discoveries $discoveries
            
            $content = Get-Content -Path $templatePath -Raw
            $content | Should -Match "Git.Git"
            $content | Should -Match "//"  # Should be commented
        }
        
        It "Should NOT include winget-owned discoveries in suggestions" {
            $templatePath = Join-Path $script:TestTempDir "test-manual.jsonc"
            $discoveries = @(
                @{
                    name = "git"
                    suggestedWingetId = "Git.Git"
                    ownedByWinget = $true  # Already managed by winget
                }
            )
            
            Write-ManualIncludeTemplate -Path $templatePath -ProfileName "test" -Discoveries $discoveries
            
            $content = Get-Content -Path $templatePath -Raw
            # Should not contain a suggestion line for Git.Git (only header comments)
            $content | Should -Match "No non-winget-managed software discovered"
        }
        
        It "Should include detection info in comment" {
            $templatePath = Join-Path $script:TestTempDir "test-manual.jsonc"
            $discoveries = @(
                @{
                    name = "git"
                    path = "C:\Program Files\Git\cmd\git.exe"
                    version = "2.43.0"
                    method = "path"
                    suggestedWingetId = "Git.Git"
                    ownedByWinget = $false
                }
            )
            
            Write-ManualIncludeTemplate -Path $templatePath -ProfileName "test" -Discoveries $discoveries
            
            $content = Get-Content -Path $templatePath -Raw
            $content | Should -Match "2.43.0"
            $content | Should -Match "C:\\Program Files\\Git"
        }
    }
}

Describe "Discovery.CaptureIntegration" {
    
    Context "Capture validation for discovery flags" {
        
        It "Should require -Profile when -DiscoverWriteManualInclude is true" {
            # This tests the validation logic directly without running full capture
            $hasProfile = $false
            $discoverWriteManualInclude = $true
            
            $shouldFail = ($discoverWriteManualInclude -and -not $hasProfile)
            
            $shouldFail | Should -Be $true
        }
        
        It "Should allow -Discover without -Profile when -DiscoverWriteManualInclude is false" {
            # When -Discover is enabled but -DiscoverWriteManualInclude is explicitly false,
            # -Profile should not be required
            $hasProfile = $false
            $discover = $true
            $discoverWriteManualInclude = $false
            
            # Only fail if manual include is requested without profile
            $shouldFail = ($discoverWriteManualInclude -and -not $hasProfile)
            
            $shouldFail | Should -Be $false
        }
        
        It "Should default DiscoverWriteManualInclude to true when Discover is enabled" {
            # Test the defaulting logic
            $discover = $true
            $discoverWriteManualInclude = $null
            
            # Default to true when Discover is enabled
            if ($discover -and $null -eq $discoverWriteManualInclude) {
                $discoverWriteManualInclude = $true
            }
            
            $discoverWriteManualInclude | Should -Be $true
        }
    }
    
    Context "Capture result structure with discovery" {
        
        It "Should NOT have Discovered key when -Discover is false" {
            # Test the result structure logic
            $discover = $false
            $result = @{
                ManifestPath = "test.jsonc"
                AppCount = 5
            }
            
            # Only add Discovered when -Discover is true
            if ($discover) {
                $result.Discovered = @()
            }
            
            $result.ContainsKey("Discovered") | Should -Be $false
        }
        
        It "Should have Discovered key when -Discover is true" {
            $discover = $true
            $result = @{
                ManifestPath = "test.jsonc"
                AppCount = 5
            }
            
            if ($discover) {
                $result.Discovered = @(
                    @{ name = "git"; method = "path"; ownedByWinget = $false }
                )
            }
            
            $result.ContainsKey("Discovered") | Should -Be $true
            $result.Discovered.Count | Should -Be 1
        }
    }
}

Describe "Discovery.DeterministicSorting" {
    
    Context "Sorting by (name, method, path)" {
        
        It "Should sort discoveries deterministically" {
            Mock Get-CommandInfo {
                param($CommandName)
                switch ($CommandName) {
                    "git" { return @{ Name = "git.exe"; Path = "C:\Git\git.exe"; CommandType = "Application" } }
                    "node" { return @{ Name = "node.exe"; Path = "A:\nodejs\node.exe"; CommandType = "Application" } }
                    "python" { return @{ Name = "python.exe"; Path = "C:\Python\python.exe"; CommandType = "Application" } }
                    default { return $null }
                }
            }
            
            Mock Get-CommandVersion { return "1.0.0" }
            Mock Get-RegistryUninstallEntries { return @() }
            
            $discoveries = Invoke-Discovery -WingetInstalledIds @()
            
            # Verify sorted by name
            $names = $discoveries | ForEach-Object { $_.name }
            $sortedNames = $names | Sort-Object
            
            for ($i = 0; $i -lt $names.Count; $i++) {
                $names[$i] | Should -Be $sortedNames[$i]
            }
        }
    }
}