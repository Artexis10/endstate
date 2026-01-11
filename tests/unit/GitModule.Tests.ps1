<#
.SYNOPSIS
    Pester tests for Git module (apps.git) and curation workflow.

.DESCRIPTION
    Validates:
    - Git module marks credential files as sensitive / not restored
    - Curation runner script exists and includes expected steps
    - Module schema is valid
    - Sensitive file handling is correct
    
    NOTE: These are deterministic unit tests - no network or sandbox execution.
#>

BeforeAll {
    $script:RepoRoot = Join-Path $PSScriptRoot "..\.."
    $script:ModulesDir = Join-Path $script:RepoRoot "modules\apps"
    $script:HarnessDir = Join-Path $script:RepoRoot "sandbox-tests\discovery-harness"
    $script:GitModulePath = Join-Path $script:ModulesDir "git\module.jsonc"
    $script:CurateGitPath = Join-Path $script:HarnessDir "curate-git.ps1"
    $script:SeedGitPath = Join-Path $script:HarnessDir "seed-git-config.ps1"
    
    # Load manifest helper for JSONC parsing
    $manifestModule = Join-Path $script:RepoRoot "engine\manifest.ps1"
    if (Test-Path $manifestModule) {
        . $manifestModule
    }
}

Describe "GitModule.FileStructure" {
    
    Context "Required files exist" {
        
        It "Should have git module.jsonc" {
            Test-Path $script:GitModulePath | Should -Be $true
        }
        
        It "Should have curate-git.ps1 curation runner" {
            Test-Path $script:CurateGitPath | Should -Be $true
        }
        
        It "Should have seed-git-config.ps1 seeding script" {
            Test-Path $script:SeedGitPath | Should -Be $true
        }
    }
}

Describe "GitModule.Schema" {
    
    BeforeAll {
        # Parse the module JSONC
        if (Test-Path $script:GitModulePath) {
            $content = Get-Content -Path $script:GitModulePath -Raw
            # Remove JSONC comments for parsing
            $jsonContent = $content -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
            $script:GitModule = $jsonContent | ConvertFrom-Json -AsHashtable -ErrorAction SilentlyContinue
        }
    }
    
    Context "Module identity" {
        
        It "Should have id 'apps.git'" {
            $script:GitModule.id | Should -Be "apps.git"
        }
        
        It "Should have displayName 'Git'" {
            $script:GitModule.displayName | Should -Be "Git"
        }
        
        It "Should have sensitivity level" {
            $script:GitModule.sensitivity | Should -Not -BeNullOrEmpty
        }
    }
    
    Context "Matches configuration" {
        
        It "Should match winget id Git.Git" {
            $script:GitModule.matches.winget | Should -Contain "Git.Git"
        }
        
        It "Should match exe git.exe" {
            $script:GitModule.matches.exe | Should -Contain "git.exe"
        }
    }
    
    Context "Verify configuration" {
        
        It "Should have verify array" {
            $script:GitModule.verify | Should -Not -BeNullOrEmpty
        }
        
        It "Should verify git command exists" {
            $verifyCommands = $script:GitModule.verify | Where-Object { $_.type -eq 'command-exists' -and $_.command -eq 'git' }
            $verifyCommands | Should -Not -BeNullOrEmpty
        }
    }
    
    Context "Capture configuration" {
        
        It "Should have capture.files array" {
            $script:GitModule.capture.files | Should -Not -BeNullOrEmpty
        }
        
        It "Should capture ~/.gitconfig" {
            $gitconfigCapture = $script:GitModule.capture.files | Where-Object { $_.source -match '\.gitconfig' }
            $gitconfigCapture | Should -Not -BeNullOrEmpty
        }
        
        It "Should capture ~/.config/git/config when present" {
            $xdgCapture = $script:GitModule.capture.files | Where-Object { $_.source -match '\.config/git/config' -or $_.source -match '\.config\\git\\config' }
            $xdgCapture | Should -Not -BeNullOrEmpty
        }
        
        It "Should have capture files marked as optional" {
            $optionalFiles = $script:GitModule.capture.files | Where-Object { $_.optional -eq $true }
            $optionalFiles.Count | Should -BeGreaterThan 0
        }
    }
}

Describe "GitModule.SensitiveFiles" {
    
    BeforeAll {
        if (Test-Path $script:GitModulePath) {
            $content = Get-Content -Path $script:GitModulePath -Raw
            $jsonContent = $content -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
            $script:GitModule = $jsonContent | ConvertFrom-Json -AsHashtable -ErrorAction SilentlyContinue
        }
    }
    
    Context "Credential files are marked sensitive" {
        
        It "Should have sensitive section" {
            $script:GitModule.sensitive | Should -Not -BeNullOrEmpty
        }
        
        It "Should mark ~/.git-credentials as sensitive" {
            $sensitiveFiles = $script:GitModule.sensitive.files
            $hasGitCredentials = $sensitiveFiles | Where-Object { $_ -match 'git-credentials' }
            $hasGitCredentials | Should -Not -BeNullOrEmpty
        }
        
        It "Should have warning message for credentials" {
            $script:GitModule.sensitive.warning | Should -Not -BeNullOrEmpty
            $script:GitModule.sensitive.warning | Should -Match 'credential|token|authentication'
        }
        
        It "Should use warn-only restorer for sensitive files" {
            $script:GitModule.sensitive.restorer | Should -Be "warn-only"
        }
    }
    
    Context "Credential files are excluded from capture" {
        
        It "Should have excludeGlobs in capture section" {
            $script:GitModule.capture.excludeGlobs | Should -Not -BeNullOrEmpty
        }
        
        It "Should exclude .git-credentials pattern" {
            $excludes = $script:GitModule.capture.excludeGlobs
            $hasCredentialExclude = $excludes | Where-Object { $_ -match 'git-credentials' -or $_ -match 'credentials' }
            $hasCredentialExclude | Should -Not -BeNullOrEmpty
        }
    }
    
    Context "Credential files are NOT in restore section" {
        
        It "Should not restore credential files" {
            $restoreTargets = $script:GitModule.restore | ForEach-Object { $_.target }
            $credentialRestores = $restoreTargets | Where-Object { $_ -match 'credential' }
            $credentialRestores | Should -BeNullOrEmpty
        }
    }
}

Describe "GitCuration.ScriptStructure" {
    
    Context "curate-git.ps1 structure" {
        
        It "Should have Mode parameter" {
            $content = Get-Content -Path $script:CurateGitPath -Raw
            $content | Should -Match '\$Mode'
            $content | Should -Match "ValidateSet\('sandbox',\s*'local'\)"
        }
        
        It "Should have synopsis documentation" {
            $content = Get-Content -Path $script:CurateGitPath -Raw
            $content | Should -Match '\.SYNOPSIS'
        }
        
        It "Should reference Git.Git winget ID" {
            $content = Get-Content -Path $script:CurateGitPath -Raw
            $content | Should -Match 'Git\.Git'
        }
        
        It "Should have seed step" {
            $content = Get-Content -Path $script:CurateGitPath -Raw
            $content | Should -Match 'seed|Seed'
        }
        
        It "Should have discovery/diff step" {
            $content = Get-Content -Path $script:CurateGitPath -Raw
            $content | Should -Match 'diff|Diff|discovery|Discovery'
        }
        
        It "Should have report generation step" {
            $content = Get-Content -Path $script:CurateGitPath -Raw
            $content | Should -Match 'report|Report'
        }
        
        It "Should reference sandbox-discovery.ps1" {
            $content = Get-Content -Path $script:CurateGitPath -Raw
            $content | Should -Match 'sandbox-discovery\.ps1'
        }
    }
    
    Context "seed-git-config.ps1 structure" {
        
        It "Should have Scope parameter" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            $content | Should -Match '\$Scope'
        }
        
        It "Should set user.name" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            $content | Should -Match 'user\.name'
        }
        
        It "Should set user.email" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            $content | Should -Match 'user\.email'
        }
        
        It "Should set init.defaultBranch" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            $content | Should -Match 'init\.defaultBranch'
        }
        
        It "Should set core.editor" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            $content | Should -Match 'core\.editor'
        }
        
        It "Should set aliases" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            $content | Should -Match 'alias\.'
        }
        
        It "Should set pull.rebase" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            $content | Should -Match 'pull\.rebase'
        }
        
        It "Should set rerere.enabled" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            $content | Should -Match 'rerere\.enabled'
        }
        
        It "Should NOT set credential helper" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            # Should explicitly mention NOT setting it
            $content | Should -Match 'credential.*NOT|NOT.*credential|Skipping credential'
        }
        
        It "Should use dummy email (not real address)" {
            $content = Get-Content -Path $script:SeedGitPath -Raw
            $content | Should -Match 'endstate\.local|test@|dummy|example\.com'
        }
    }
}

Describe "GitCuration.WorkflowSteps" {
    
    Context "Curation workflow includes required phases" {
        
        BeforeAll {
            $script:CurateContent = Get-Content -Path $script:CurateGitPath -Raw
        }
        
        It "Step 1: Should check/install Git" {
            $script:CurateContent | Should -Match 'install|Install|winget'
        }
        
        It "Step 2: Should seed configuration" {
            $script:CurateContent | Should -Match 'seed-git-config\.ps1|Seed.*config'
        }
        
        It "Step 3: Should capture state or run discovery" {
            $script:CurateContent | Should -Match 'snapshot|Snapshot|discovery|Discovery|capture|Capture'
        }
        
        It "Step 4: Should generate report" {
            $script:CurateContent | Should -Match 'New-CurationReport|CURATION_REPORT'
        }
        
        It "Should handle both local and sandbox modes" {
            $script:CurateContent | Should -Match 'Invoke-LocalCuration'
            $script:CurateContent | Should -Match 'Invoke-SandboxCuration'
        }
    }
}

Describe "GitModule.RestoreOptional" {
    
    BeforeAll {
        if (Test-Path $script:GitModulePath) {
            $content = Get-Content -Path $script:GitModulePath -Raw
            $jsonContent = $content -replace '//.*$', '' -replace '/\*[\s\S]*?\*/', ''
            $script:GitModule = $jsonContent | ConvertFrom-Json -AsHashtable -ErrorAction SilentlyContinue
        }
    }
    
    Context "Restore entries are optional" {
        
        It "Should have restore entries marked as optional" {
            $optionalRestores = $script:GitModule.restore | Where-Object { $_.optional -eq $true }
            $optionalRestores.Count | Should -BeGreaterThan 0
        }
        
        It "Should restore with backup enabled" {
            $backupRestores = $script:GitModule.restore | Where-Object { $_.backup -eq $true }
            $backupRestores.Count | Should -BeGreaterThan 0
        }
    }
}
