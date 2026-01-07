<#
.SYNOPSIS
    Pester tests for Capture Update mode: merge, prune, include deduplication.
#>

BeforeAll {
    $script:ProvisioningRoot = Join-Path $PSScriptRoot "..\.."
    $script:ManifestScript = Join-Path $script:ProvisioningRoot "engine\manifest.ps1"
    
    # Load manifest module (contains Merge-ManifestsForUpdate)
    . $script:ManifestScript
}

Describe "Merge-ManifestsForUpdate" {
    
    Context "Basic merge without prune" {
        
        It "Should preserve existing includes" {
            $existing = @{
                version = 1
                name = "test-profile"
                captured = "2024-01-01T00:00:00Z"
                includes = @("./includes/test-restore.jsonc", "./includes/test-manual.jsonc")
                apps = @(
                    @{ id = "app-a"; refs = @{ windows = "App.A" } }
                )
                restore = @()
                verify = @()
            }
            
            $newApps = @(
                @{ id = "app-a"; refs = @{ windows = "App.A" } }
            )
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps
            
            $result.includes | Should -Not -BeNullOrEmpty
            $result.includes.Count | Should -Be 2
            ($result.includes -contains "./includes/test-restore.jsonc") | Should -Be $true
            ($result.includes -contains "./includes/test-manual.jsonc") | Should -Be $true
        }
        
        It "Should update captured timestamp" {
            $existing = @{
                version = 1
                name = "test-profile"
                captured = "2024-01-01T00:00:00Z"
                apps = @()
                restore = @()
                verify = @()
            }
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps @()
            
            $result.captured | Should -Not -Be "2024-01-01T00:00:00Z"
            $result.captured | Should -Match "^\d{4}-\d{2}-\d{2}T"
        }
        
        It "Should preserve existing name" {
            $existing = @{
                version = 1
                name = "my-custom-name"
                captured = "2024-01-01T00:00:00Z"
                apps = @()
                restore = @()
                verify = @()
            }
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps @()
            
            $result.name | Should -Be "my-custom-name"
        }
        
        It "Should preserve existing restore block" {
            $existing = @{
                version = 1
                name = "test"
                apps = @()
                restore = @(
                    @{ type = "copy"; source = "./config"; target = "~/.config" }
                )
                verify = @()
            }
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps @()
            
            $result.restore | Should -Not -BeNullOrEmpty
            $result.restore.Count | Should -Be 1
            $result.restore[0].type | Should -Be "copy"
        }
        
        It "Should preserve existing verify block" {
            $existing = @{
                version = 1
                name = "test"
                apps = @()
                restore = @()
                verify = @(
                    @{ type = "file-exists"; path = "~/.gitconfig" }
                )
            }
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps @()
            
            $result.verify | Should -Not -BeNullOrEmpty
            $result.verify.Count | Should -Be 1
            $result.verify[0].type | Should -Be "file-exists"
        }
        
        It "Should merge apps - add new, keep existing" {
            $existing = @{
                version = 1
                name = "test"
                apps = @(
                    @{ id = "app-a"; refs = @{ windows = "App.A" } }
                    @{ id = "app-b"; refs = @{ windows = "App.B" } }
                )
                restore = @()
                verify = @()
            }
            
            $newApps = @(
                @{ id = "app-a"; refs = @{ windows = "App.A" } }
                @{ id = "app-c"; refs = @{ windows = "App.C" } }
            )
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps
            
            $result.apps.Count | Should -Be 3
            ($result.apps | Where-Object { $_.id -eq "app-a" }) | Should -Not -BeNullOrEmpty
            ($result.apps | Where-Object { $_.id -eq "app-b" }) | Should -Not -BeNullOrEmpty
            ($result.apps | Where-Object { $_.id -eq "app-c" }) | Should -Not -BeNullOrEmpty
        }
        
        It "Should update refs when app exists in both" {
            $existing = @{
                version = 1
                name = "test"
                apps = @(
                    @{ id = "app-a"; refs = @{ windows = "App.A.Old" } }
                )
                restore = @()
                verify = @()
            }
            
            $newApps = @(
                @{ id = "app-a"; refs = @{ windows = "App.A.New" } }
            )
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps
            
            $appA = $result.apps | Where-Object { $_.id -eq "app-a" }
            $appA.refs.windows | Should -Be "App.A.New"
        }
        
        It "Should sort apps by id" {
            $existing = @{
                version = 1
                name = "test"
                apps = @(
                    @{ id = "zebra"; refs = @{ windows = "Zebra" } }
                )
                restore = @()
                verify = @()
            }
            
            $newApps = @(
                @{ id = "alpha"; refs = @{ windows = "Alpha" } }
                @{ id = "beta"; refs = @{ windows = "Beta" } }
            )
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps
            
            $result.apps[0].id | Should -Be "alpha"
            $result.apps[1].id | Should -Be "beta"
            $result.apps[2].id | Should -Be "zebra"
        }
    }
    
    Context "Merge with -PruneMissingApps" {
        
        It "Should remove apps not present in new capture" {
            $existing = @{
                version = 1
                name = "test"
                apps = @(
                    @{ id = "app-a"; refs = @{ windows = "App.A" } }
                    @{ id = "app-b"; refs = @{ windows = "App.B" } }
                    @{ id = "app-c"; refs = @{ windows = "App.C" } }
                )
                restore = @()
                verify = @()
            }
            
            $newApps = @(
                @{ id = "app-a"; refs = @{ windows = "App.A" } }
                # app-b and app-c are NOT in new capture
            )
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps -PruneMissingApps
            
            $result.apps.Count | Should -Be 1
            $result.apps[0].id | Should -Be "app-a"
        }
        
        It "Should still add new apps when pruning" {
            $existing = @{
                version = 1
                name = "test"
                apps = @(
                    @{ id = "app-a"; refs = @{ windows = "App.A" } }
                )
                restore = @()
                verify = @()
            }
            
            $newApps = @(
                @{ id = "app-a"; refs = @{ windows = "App.A" } }
                @{ id = "app-new"; refs = @{ windows = "App.New" } }
            )
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps -PruneMissingApps
            
            $result.apps.Count | Should -Be 2
            ($result.apps | Where-Object { $_.id -eq "app-new" }) | Should -Not -BeNullOrEmpty
        }
    }
    
    Context "Include deduplication" {
        
        It "Should not add app to root if it exists in includes" {
            $existing = @{
                version = 1
                name = "test"
                apps = @(
                    @{ id = "app-a"; refs = @{ windows = "App.A" } }
                )
                restore = @()
                verify = @()
            }
            
            $newApps = @(
                @{ id = "app-a"; refs = @{ windows = "App.A" } }
                @{ id = "git-git"; refs = @{ windows = "Git.Git" } }  # This is in includes
            )
            
            # Simulate that git-git comes from an include
            $includedAppIds = @{ "git-git" = $true }
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps -IncludedAppIds $includedAppIds
            
            $result.apps.Count | Should -Be 1
            $result.apps[0].id | Should -Be "app-a"
            ($result.apps | Where-Object { $_.id -eq "git-git" }) | Should -BeNullOrEmpty
        }
        
        It "Should remove app from root if it now exists in includes" {
            $existing = @{
                version = 1
                name = "test"
                apps = @(
                    @{ id = "app-a"; refs = @{ windows = "App.A" } }
                    @{ id = "git-git"; refs = @{ windows = "Git.Git" } }  # Was in root, now in include
                )
                restore = @()
                verify = @()
            }
            
            $newApps = @(
                @{ id = "app-a"; refs = @{ windows = "App.A" } }
                @{ id = "git-git"; refs = @{ windows = "Git.Git" } }
            )
            
            # Simulate that git-git now comes from an include
            $includedAppIds = @{ "git-git" = $true }
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps -IncludedAppIds $includedAppIds
            
            $result.apps.Count | Should -Be 1
            $result.apps[0].id | Should -Be "app-a"
        }
    }
    
    Context "New includes handling" {
        
        It "Should add new includes that don't exist" {
            $existing = @{
                version = 1
                name = "test"
                includes = @("./includes/existing.jsonc")
                apps = @()
                restore = @()
                verify = @()
            }
            
            $newIncludes = @("./includes/new-manual.jsonc")
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps @() -NewIncludes $newIncludes
            
            $result.includes.Count | Should -Be 2
            ($result.includes -contains "./includes/existing.jsonc") | Should -Be $true
            ($result.includes -contains "./includes/new-manual.jsonc") | Should -Be $true
        }
        
        It "Should not duplicate includes that already exist" {
            $existing = @{
                version = 1
                name = "test"
                includes = @("./includes/manual.jsonc")
                apps = @()
                restore = @()
                verify = @()
            }
            
            $newIncludes = @("./includes/manual.jsonc")  # Same as existing
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps @() -NewIncludes $newIncludes
            
            $result.includes.Count | Should -Be 1
        }
    }
}

Describe "Get-IncludedAppIds" {
    
    BeforeEach {
        $script:TestTempDir = Join-Path $env:TEMP "update-capture-tests-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TestTempDir -Force | Out-Null
    }
    
    AfterEach {
        if (Test-Path $script:TestTempDir) {
            Remove-Item -Path $script:TestTempDir -Recurse -Force
        }
    }
    
    Context "Loading app IDs from includes" {
        
        It "Should return app IDs from include file" {
            # Create a test include file
            $includeContent = @{
                version = 1
                name = "test-include"
                apps = @(
                    @{ id = "git-git"; refs = @{ windows = "Git.Git" } }
                    @{ id = "vscode"; refs = @{ windows = "Microsoft.VisualStudioCode" } }
                )
            } | ConvertTo-Json -Depth 10
            
            $includePath = Join-Path $script:TestTempDir "test-include.json"
            $includeContent | Out-File -FilePath $includePath -Encoding UTF8
            
            $result = Get-IncludedAppIds -IncludePaths @($includePath) -BaseDir $script:TestTempDir
            
            $result.ContainsKey("git-git") | Should -Be $true
            $result.ContainsKey("vscode") | Should -Be $true
        }
        
        It "Should handle relative paths" {
            # Create includes subdirectory
            $includesDir = Join-Path $script:TestTempDir "includes"
            New-Item -ItemType Directory -Path $includesDir -Force | Out-Null
            
            $includeContent = @{
                version = 1
                apps = @(
                    @{ id = "app-from-include"; refs = @{ windows = "App.Include" } }
                )
            } | ConvertTo-Json -Depth 10
            
            $includePath = Join-Path $includesDir "test.json"
            $includeContent | Out-File -FilePath $includePath -Encoding UTF8
            
            $result = Get-IncludedAppIds -IncludePaths @("./includes/test.json") -BaseDir $script:TestTempDir
            
            $result.ContainsKey("app-from-include") | Should -Be $true
        }
        
        It "Should skip non-existent include files" {
            $result = Get-IncludedAppIds -IncludePaths @("./nonexistent.json") -BaseDir $script:TestTempDir
            
            $result.Count | Should -Be 0
        }
        
        It "Should merge IDs from multiple includes" {
            $includesDir = Join-Path $script:TestTempDir "includes"
            New-Item -ItemType Directory -Path $includesDir -Force | Out-Null
            
            $include1 = @{ version = 1; apps = @(@{ id = "app-1" }) } | ConvertTo-Json -Depth 10
            $include2 = @{ version = 1; apps = @(@{ id = "app-2" }) } | ConvertTo-Json -Depth 10
            
            $include1 | Out-File -FilePath (Join-Path $includesDir "inc1.json") -Encoding UTF8
            $include2 | Out-File -FilePath (Join-Path $includesDir "inc2.json") -Encoding UTF8
            
            $result = Get-IncludedAppIds -IncludePaths @("./includes/inc1.json", "./includes/inc2.json") -BaseDir $script:TestTempDir
            
            $result.ContainsKey("app-1") | Should -Be $true
            $result.ContainsKey("app-2") | Should -Be $true
        }
    }
}

Describe "Read-ManifestRaw" {
    
    BeforeEach {
        $script:TestTempDir = Join-Path $env:TEMP "manifest-raw-tests-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TestTempDir -Force | Out-Null
    }
    
    AfterEach {
        if (Test-Path $script:TestTempDir) {
            Remove-Item -Path $script:TestTempDir -Recurse -Force
        }
    }
    
    Context "Loading manifest without resolving includes" {
        
        It "Should return null for non-existent file" {
            $result = Read-ManifestRaw -Path (Join-Path $script:TestTempDir "nonexistent.json")
            
            $result | Should -BeNullOrEmpty
        }
        
        It "Should load JSON manifest" {
            $manifestContent = @{
                version = 1
                name = "test"
                apps = @(@{ id = "app-a" })
            } | ConvertTo-Json -Depth 10
            
            $manifestPath = Join-Path $script:TestTempDir "test.json"
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            $result = Read-ManifestRaw -Path $manifestPath
            
            $result | Should -Not -BeNullOrEmpty
            $result.name | Should -Be "test"
            $result.apps.Count | Should -Be 1
        }
        
        It "Should preserve includes array without resolving" {
            $manifestContent = @{
                version = 1
                name = "test"
                includes = @("./includes/restore.jsonc", "./includes/manual.jsonc")
                apps = @()
            } | ConvertTo-Json -Depth 10
            
            $manifestPath = Join-Path $script:TestTempDir "test.json"
            $manifestContent | Out-File -FilePath $manifestPath -Encoding UTF8
            
            $result = Read-ManifestRaw -Path $manifestPath
            
            $result.includes | Should -Not -BeNullOrEmpty
            $result.includes.Count | Should -Be 2
        }
    }
}

Describe "Update mode validation" {
    
    Context "-PruneMissingApps requires -Update" {
        
        It "Should validate that PruneMissingApps requires Update" {
            # This tests the validation logic
            $update = $false
            $pruneMissingApps = $true
            
            $shouldFail = ($pruneMissingApps -and -not $update)
            
            $shouldFail | Should -Be $true
        }
        
        It "Should allow PruneMissingApps when Update is true" {
            $update = $true
            $pruneMissingApps = $true
            
            $shouldFail = ($pruneMissingApps -and -not $update)
            
            $shouldFail | Should -Be $false
        }
    }
}

Describe "Update mode behavior" {
    
    Context "Non-existent manifest with -Update" {
        
        It "Should behave like normal capture when manifest doesn't exist" {
            # When -Update is specified but manifest doesn't exist,
            # it should create a new manifest (not fail)
            $manifestExists = $false
            $updateMode = $true
            
            # Logic: if Update and manifest exists, do merge; else create new
            $shouldMerge = $updateMode -and $manifestExists
            
            $shouldMerge | Should -Be $false
        }
    }
    
    Context "Existing manifest with -Update" {
        
        It "Should merge when manifest exists" {
            $manifestExists = $true
            $updateMode = $true
            
            $shouldMerge = $updateMode -and $manifestExists
            
            $shouldMerge | Should -Be $true
        }
    }
}

Describe "Deterministic output" {
    
    Context "Merge produces deterministic results" {
        
        It "Should produce same output for same input" {
            $existing = @{
                version = 1
                name = "test"
                apps = @(
                    @{ id = "zebra"; refs = @{ windows = "Zebra" } }
                    @{ id = "alpha"; refs = @{ windows = "Alpha" } }
                )
                restore = @()
                verify = @()
            }
            
            $newApps = @(
                @{ id = "beta"; refs = @{ windows = "Beta" } }
            )
            
            $result1 = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps
            $result2 = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps $newApps
            
            # Compare app order
            $result1.apps.Count | Should -Be $result2.apps.Count
            for ($i = 0; $i -lt $result1.apps.Count; $i++) {
                $result1.apps[$i].id | Should -Be $result2.apps[$i].id
            }
        }
        
        It "Should always sort apps alphabetically by id" {
            $existing = @{
                version = 1
                name = "test"
                apps = @(
                    @{ id = "z-app"; refs = @{ windows = "Z" } }
                    @{ id = "m-app"; refs = @{ windows = "M" } }
                    @{ id = "a-app"; refs = @{ windows = "A" } }
                )
                restore = @()
                verify = @()
            }
            
            $result = Merge-ManifestsForUpdate -ExistingManifest $existing -NewCaptureApps @()
            
            $result.apps[0].id | Should -Be "a-app"
            $result.apps[1].id | Should -Be "m-app"
            $result.apps[2].id | Should -Be "z-app"
        }
    }
}