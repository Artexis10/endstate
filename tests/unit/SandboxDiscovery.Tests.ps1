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
        
        It "Should use recursive search for UI.Xaml package candidates in NuGet fallback" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must search recursively for appx files in UI.Xaml NuGet fallback
            $content | Should -Match 'Get-ChildItem.*-Recurse.*-Include.*\*\.appx'
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
        
        It "Should include package list in error diagnostics" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must build diagnostic list with downloaded deps and dependency paths
            $content | Should -Match '\$downloadedDeps'
            $content | Should -Match '\$dependencyPaths'
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
        
        It "Should check Get-AppxPackage for Microsoft.WindowsAppRuntime.1.8 (non-CBS)" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must check for framework identity pattern that excludes CBS packages using explicit namespace
            $content | Should -Match 'Microsoft\.WindowsAppRuntime\.1\.8\*'
            # Explicit CBS namespace exclusion (not substring heuristic)
            $content | Should -Match '-notlike\s+[''"]Microsoft\.WindowsAppRuntime\.CBS\.\*[''"]'
        }
        
        It "Should explicitly exclude CBS packages from WAR 1.8 detection" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # CBS packages do NOT satisfy App Installer's dependency requirement
            $content | Should -Match 'CBS.*NOT.*satisfy|does NOT satisfy.*CBS'
            # Must query CBS identity separately with explicit namespace
            $content | Should -Match 'Microsoft\.WindowsAppRuntime\.CBS\.1\.8\*'
        }
        
        It "Should log when CBS package is present but insufficient" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must log CBS presence for diagnostics
            $content | Should -Match 'CBS present but insufficient'
        }
        
        It "Should download WindowsAppRuntime redist zip (not EXE installer)" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must use redist zip approach with MSIX packages
            $content | Should -Match 'Microsoft\.WindowsAppRuntime\.Redist\.1\.8\.zip'
            $content | Should -Match 'Expand-Archive.*redistZipPath'
        }
        
        It "Should download VCLibs Desktop explicitly from aka.ms" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must download VCLibs Desktop from official URL
            $content | Should -Match 'aka\.ms/Microsoft\.VCLibs\.x64\.14\.00\.Desktop\.appx'
        }
        
        It "Should download UI.Xaml explicitly (not from bundle)" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must download UI.Xaml from NuGet and extract appx
            $content | Should -Match 'Microsoft\.UI\.Xaml'
            $content | Should -Match '\*\.appx'
            $content | Should -Match 'selectedUiXaml'
        }
        
        It "Should have UI.Xaml NuGet fallback with Expand-Archive" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Fallback via NuGet package extraction
            $content | Should -Match 'uiXamlNugetUrl'
            $content | Should -Match 'ChangeExtension.*\.zip'
            $content | Should -Match 'Expand-Archive.*uiXamlZip'
        }
        
        It "Should use -DependencyPath when installing App Installer bundle" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must use Add-AppxPackage with -DependencyPath for App Installer
            $content | Should -Match 'Add-AppxPackage\s+-Path\s+\$wingetBundlePath\s+-DependencyPath'
        }
        
        It "Should have Select-BestPackageCandidate helper function" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'function\s+Select-BestPackageCandidate'
        }
        
        It "Should verify winget version after install" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'winget\s+--version'
        }
        
        It "Should have best-effort winget source update" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'winget\s+source\s+update'
        }
        
        It "Should include comprehensive diagnostics on failure" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must include diagnostic info about installed packages
            $content | Should -Match 'downloadedDeps'
            $content | Should -Match 'dependencyPaths'
            $content | Should -Match 'Get-AppxPackage.*VCLibs'
            $content | Should -Match 'Get-AppxPackage.*UI\.Xaml'
            $content | Should -Match 'Get-AppxPackage.*WindowsAppRuntime'
        }
        
        It "Should include Get-AppPackageLog on AppInstaller failure" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must call Get-AppPackageLog for ActivityId extraction
            $content | Should -Match 'Get-AppPackageLog\s+-ActivityID'
        }
        
        It "Should format Get-AppPackageLog output as readable text (not raw EventLogRecord)" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must format EventLogRecord objects with Format-List for readable output
            $content | Should -Match 'Format-List\s+TimeCreated.*Message.*Out-String'
        }
        
        It "Should list DesktopAppInstaller packages on failure" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must list DesktopAppInstaller packages in diagnostics
            $content | Should -Match 'Get-AppxPackage\s+Microsoft\.DesktopAppInstaller'
        }
    }
}

Describe "SandboxDiscovery.DownloadHelpers" {
    
    Context "Resolve-FinalUrl function" {
        
        It "Should have Resolve-FinalUrl function" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'function\s+Resolve-FinalUrl'
        }
        
        It "Should return FinalUrl, RedirectChain, ContentType, Success properties" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'FinalUrl\s*='
            $content | Should -Match 'RedirectChain\s*='
            $content | Should -Match 'ContentType\s*='
            $content | Should -Match 'Success\s*='
        }
        
        It "Should try curl.exe first for redirect resolution" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'curl\.exe.*-L.*-I'
        }
        
        It "Should have HttpClient fallback for redirect resolution" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'System\.Net\.Http\.HttpClient'
            $content | Should -Match 'AllowAutoRedirect\s*=\s*\$true'
        }
    }
    
    Context "Test-ZipMagic function" {
        
        It "Should have Test-ZipMagic function" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'function\s+Test-ZipMagic'
        }
        
        It "Should check for valid ZIP signatures (50 4B)" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Must check for PK magic bytes
            $content | Should -Match '0x50.*0x4B.*0x03.*0x04'
        }
        
        It "Should return IsValid, MagicBytes, FirstBytesHex, FileSize properties" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'IsValid\s*='
            $content | Should -Match 'MagicBytes\s*='
            $content | Should -Match 'FirstBytesHex\s*='
            $content | Should -Match 'FileSize\s*='
        }
        
        It "Should detect HTML content as invalid ZIP" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'File appears to be HTML, not ZIP'
        }
    }
    
    Context "Test-ZipMagic runtime behavior" {
        
        BeforeAll {
            # Load the sandbox-install.ps1 to get the Test-ZipMagic function
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            # Extract just the Test-ZipMagic function and dot-source it
            $content = Get-Content -Path $installScript -Raw
            if ($content -match '(?s)(function Test-ZipMagic \{.+?\n\})') {
                $functionDef = $matches[1]
                Invoke-Expression $functionDef
            }
        }
        
        It "Should return IsValid=true for valid ZIP magic bytes (PK header)" {
            $tempFile = [System.IO.Path]::GetTempFileName()
            try {
                # Write valid ZIP magic bytes: PK\x03\x04
                [byte[]]$zipBytes = @(0x50, 0x4B, 0x03, 0x04) + @(0x00) * 28
                [System.IO.File]::WriteAllBytes($tempFile, $zipBytes)
                
                $result = Test-ZipMagic -FilePath $tempFile
                $result.IsValid | Should -Be $true
                $result.MagicBytes | Should -Be "50 4B 03 04"
                $result.FileSize | Should -Be 32
                $result.Error | Should -BeNullOrEmpty
            }
            finally {
                Remove-Item $tempFile -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should return IsValid=false for HTML content masquerading as ZIP" {
            $tempFile = [System.IO.Path]::GetTempFileName()
            try {
                # Write HTML content (common redirect/error page)
                $htmlContent = "<!DOCTYPE html><html><body>Redirect</body></html>"
                [System.IO.File]::WriteAllText($tempFile, $htmlContent)
                
                $result = Test-ZipMagic -FilePath $tempFile
                $result.IsValid | Should -Be $false
                $result.Error | Should -Match "HTML"
                $result.FirstBytesHex | Should -Not -BeNullOrEmpty
                $result.FirstBytesAscii | Should -Match "<!DOCTYPE"
            }
            finally {
                Remove-Item $tempFile -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should return IsValid=false for random binary (not ZIP)" {
            $tempFile = [System.IO.Path]::GetTempFileName()
            try {
                # Write random non-ZIP bytes
                [byte[]]$randomBytes = @(0xDE, 0xAD, 0xBE, 0xEF) + @(0x00) * 28
                [System.IO.File]::WriteAllBytes($tempFile, $randomBytes)
                
                $result = Test-ZipMagic -FilePath $tempFile
                $result.IsValid | Should -Be $false
                $result.MagicBytes | Should -Be "DE AD BE EF"
                $result.Error | Should -Match "Invalid ZIP magic bytes"
            }
            finally {
                Remove-Item $tempFile -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should include first-bytes hex and ASCII in result for diagnostics" {
            $tempFile = [System.IO.Path]::GetTempFileName()
            try {
                # Write recognizable ASCII content
                $content = "This is not a ZIP file at all!"
                [System.IO.File]::WriteAllText($tempFile, $content)
                
                $result = Test-ZipMagic -FilePath $tempFile
                $result.FirstBytesHex | Should -Match "54 68 69 73"  # "This" in hex
                $result.FirstBytesAscii | Should -Match "This is not"
            }
            finally {
                Remove-Item $tempFile -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should handle file not found gracefully" {
            $result = Test-ZipMagic -FilePath "C:\nonexistent\file.zip"
            $result.IsValid | Should -Be $false
            $result.Error | Should -Match "not found"
        }
        
        It "Should handle empty file gracefully" {
            $tempFile = [System.IO.Path]::GetTempFileName()
            try {
                # Create empty file
                [System.IO.File]::WriteAllBytes($tempFile, @())
                
                $result = Test-ZipMagic -FilePath $tempFile
                $result.IsValid | Should -Be $false
                $result.FileSize | Should -Be 0
                $result.Error | Should -Match "too small"
            }
            finally {
                Remove-Item $tempFile -Force -ErrorAction SilentlyContinue
            }
        }
    }
    
    Context "Invoke-RobustDownload integration" {
        
        It "Should have ValidateZip parameter" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\[switch\]\$ValidateZip'
        }
        
        It "Should call Resolve-FinalUrl before downloading" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Resolve-FinalUrl should be called before the download loop
            $resolvePos = $content.IndexOf('Resolve-FinalUrl -Url')
            $downloadLoopPos = $content.IndexOf('for ($retry = 1')
            $resolvePos | Should -BeLessThan $downloadLoopPos
        }
        
        It "Should download to temp file first then move on success" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\$tempFile\s*=.*\.tmp'
            $content | Should -Match 'Move-Item.*\$tempFile.*\$OutFile'
        }
        
        It "Should call Test-ZipMagic when ValidateZip is set" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'if\s*\(\$ValidateZip\)'
            $content | Should -Match 'Test-ZipMagic\s+-FilePath'
        }
        
        It "Should include rich diagnostics on ZIP validation failure" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Diagnostics should include final URL, Content-Type, magic bytes, first bytes hex
            $content | Should -Match 'Final URL:.*\$finalUrl'
            $content | Should -Match 'Magic bytes:.*\$.*MagicBytes'
            $content | Should -Match 'First 32 bytes \(hex\):.*\$.*FirstBytesHex'
        }
        
        It "Should use ValidateZip for WindowsAppRuntime download" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'WindowsAppRuntime18-download.*-ValidateZip'
        }
        
        It "Should have ResolveFinalUrlFn parameter for dependency injection" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\[scriptblock\]\$ResolveFinalUrlFn'
        }
        
        It "Should have DownloadFn parameter for dependency injection" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\[scriptblock\]\$DownloadFn'
        }
    }
    
    Context "Invoke-RobustDownload runtime behavior" {
        
        BeforeAll {
            # Load required functions from sandbox-install.ps1
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            
            # Extract Test-ZipMagic function
            if ($content -match '(?s)(function Test-ZipMagic \{.+?\n\})') {
                Invoke-Expression $matches[1]
            }
            
            # Extract Invoke-RobustDownload function
            if ($content -match '(?s)(function Invoke-RobustDownload \{.+?\n\})\s*\n\s*function') {
                Invoke-Expression $matches[1]
            }
            
            # Mock Write-* functions to avoid output during tests
            function global:Write-Step { param($msg) }
            function global:Write-Heartbeat { param($msg, $Details) }
            function global:Write-Info { param($msg) }
            function global:Write-Pass { param($msg) }
            function global:Write-FatalError { param($Step, $Message, $Details) throw "$Message`n$Details" }
        }
        
        AfterAll {
            # Clean up global mocks
            Remove-Item Function:\Write-Step -ErrorAction SilentlyContinue
            Remove-Item Function:\Write-Heartbeat -ErrorAction SilentlyContinue
            Remove-Item Function:\Write-Info -ErrorAction SilentlyContinue
            Remove-Item Function:\Write-Pass -ErrorAction SilentlyContinue
            Remove-Item Function:\Write-FatalError -ErrorAction SilentlyContinue
        }
        
        It "Should use injected ResolveFinalUrlFn and return final URL in diagnostics" {
            $tempDir = [System.IO.Path]::GetTempPath()
            $outFile = Join-Path $tempDir "test-download-$(Get-Random).zip"
            
            try {
                # Fake resolver that simulates redirect chain
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = "https://cdn.example.com/final.zip"
                        RedirectChain = @("https://aka.ms/redirect1", "https://aka.ms/redirect2")
                        ContentType = "application/zip"
                        ContentLength = 1000000
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes valid ZIP
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    [byte[]]$zipBytes = @(0x50, 0x4B, 0x03, 0x04) + @(0x00) * 1000000
                    [System.IO.File]::WriteAllBytes($outFile, $zipBytes)
                    return $true
                }
                
                $result = Invoke-RobustDownload `
                    -Url "https://aka.ms/test" `
                    -OutFile $outFile `
                    -StepName "TestDownload" `
                    -MinExpectedBytes 100 `
                    -MaxRetries 1 `
                    -ValidateZip `
                    -ResolveFinalUrlFn $fakeResolver `
                    -DownloadFn $fakeDownloader
                
                $result | Should -Be $true
                Test-Path $outFile | Should -Be $true
            }
            finally {
                Remove-Item $outFile -Force -ErrorAction SilentlyContinue
                Remove-Item "$outFile.tmp" -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should throw with diagnostics when ValidateZip rejects non-ZIP binary payload" {
            $tempDir = [System.IO.Path]::GetTempPath()
            $outFile = Join-Path $tempDir "test-download-$(Get-Random).zip"
            
            try {
                # Fake resolver
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = "https://cdn.example.com/bad.bin"
                        RedirectChain = @("https://aka.ms/redirect")
                        ContentType = "application/octet-stream"
                        ContentLength = 1000000
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes non-ZIP binary (not HTML, so ZIP validation runs)
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    # Write binary content that's not HTML and not ZIP (large enough to pass size check)
                    [byte[]]$badBytes = @(0xCA, 0xFE, 0xBA, 0xBE) + @(0x00) * 1000000
                    [System.IO.File]::WriteAllBytes($outFile, $badBytes)
                    return $true
                }
                
                $errorThrown = $null
                try {
                    Invoke-RobustDownload `
                        -Url "https://aka.ms/test" `
                        -OutFile $outFile `
                        -StepName "TestDownload" `
                        -MinExpectedBytes 100 `
                        -MaxRetries 1 `
                        -ValidateZip `
                        -ResolveFinalUrlFn $fakeResolver `
                        -DownloadFn $fakeDownloader
                } catch {
                    $errorThrown = $_.Exception.Message
                }
                
                $errorThrown | Should -Not -BeNullOrEmpty
                # Verify error includes diagnostic fields (from final error after ZIP validation fails)
                $errorThrown | Should -Match "Final URL:"
                $errorThrown | Should -Match "Content-Type:"
                $errorThrown | Should -Match "Redirect chain:"
            }
            finally {
                Remove-Item $outFile -Force -ErrorAction SilentlyContinue
                Remove-Item "$outFile.tmp" -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should detect HTML content before ZIP validation and include diagnostics" {
            $tempDir = [System.IO.Path]::GetTempPath()
            $outFile = Join-Path $tempDir "test-download-$(Get-Random).zip"
            
            try {
                # Fake resolver
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = "https://cdn.example.com/error.html"
                        RedirectChain = @("https://aka.ms/redirect")
                        ContentType = "text/html"
                        ContentLength = 500
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes HTML
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    $htmlContent = "<!DOCTYPE html><html><body>Error: File not found</body></html>"
                    [System.IO.File]::WriteAllText($outFile, $htmlContent)
                    return $true
                }
                
                $errorThrown = $null
                try {
                    Invoke-RobustDownload `
                        -Url "https://aka.ms/test" `
                        -OutFile $outFile `
                        -StepName "TestDownload" `
                        -MinExpectedBytes 10 `
                        -MaxRetries 1 `
                        -ValidateZip `
                        -ResolveFinalUrlFn $fakeResolver `
                        -DownloadFn $fakeDownloader
                } catch {
                    $errorThrown = $_.Exception.Message
                }
                
                $errorThrown | Should -Not -BeNullOrEmpty
                # HTML detection happens before ZIP validation
                $errorThrown | Should -Match "HTML"
                $errorThrown | Should -Match "Final URL:"
                $errorThrown | Should -Match "Content-Type:"
                $errorThrown | Should -Match "Redirect chain:"
            }
            finally {
                Remove-Item $outFile -Force -ErrorAction SilentlyContinue
                Remove-Item "$outFile.tmp" -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should succeed when DownloadFn writes valid ZIP magic bytes" {
            $tempDir = [System.IO.Path]::GetTempPath()
            $outFile = Join-Path $tempDir "test-download-$(Get-Random).zip"
            
            try {
                # Fake resolver (no redirects)
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = $url
                        RedirectChain = @()
                        ContentType = "application/zip"
                        ContentLength = 2000000
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes valid ZIP
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    [byte[]]$zipBytes = @(0x50, 0x4B, 0x03, 0x04) + @(0x00) * 2000000
                    [System.IO.File]::WriteAllBytes($outFile, $zipBytes)
                    return $true
                }
                
                $result = Invoke-RobustDownload `
                    -Url "https://example.com/file.zip" `
                    -OutFile $outFile `
                    -StepName "TestDownload" `
                    -MinExpectedBytes 1000000 `
                    -MaxRetries 1 `
                    -ValidateZip `
                    -ResolveFinalUrlFn $fakeResolver `
                    -DownloadFn $fakeDownloader
                
                $result | Should -Be $true
                Test-Path $outFile | Should -Be $true
                
                # Verify the file has valid ZIP magic
                $bytes = [System.IO.File]::ReadAllBytes($outFile)
                $bytes[0] | Should -Be 0x50
                $bytes[1] | Should -Be 0x4B
                $bytes[2] | Should -Be 0x03
                $bytes[3] | Should -Be 0x04
            }
            finally {
                Remove-Item $outFile -Force -ErrorAction SilentlyContinue
                Remove-Item "$outFile.tmp" -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should include redirect chain in error diagnostics" {
            $tempDir = [System.IO.Path]::GetTempPath()
            $outFile = Join-Path $tempDir "test-download-$(Get-Random).zip"
            
            try {
                # Fake resolver with redirect chain
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = "https://cdn.example.com/bad.zip"
                        RedirectChain = @("https://aka.ms/step1", "https://aka.ms/step2", "https://cdn.example.com/bad.zip")
                        ContentType = "application/octet-stream"
                        ContentLength = 1000000
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes invalid content (large enough to pass size check)
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    [byte[]]$badBytes = @(0xDE, 0xAD, 0xBE, 0xEF) + @(0x00) * 1000000
                    [System.IO.File]::WriteAllBytes($outFile, $badBytes)
                    return $true
                }
                
                $errorThrown = $null
                try {
                    Invoke-RobustDownload `
                        -Url "https://aka.ms/test" `
                        -OutFile $outFile `
                        -StepName "TestDownload" `
                        -MinExpectedBytes 100 `
                        -MaxRetries 1 `
                        -ValidateZip `
                        -ResolveFinalUrlFn $fakeResolver `
                        -DownloadFn $fakeDownloader
                } catch {
                    $errorThrown = $_.Exception.Message
                }
                
                $errorThrown | Should -Not -BeNullOrEmpty
                # Verify redirect chain is in diagnostics (from final error after retries exhausted)
                $errorThrown | Should -Match "Redirect chain:"
                $errorThrown | Should -Match "Final URL:"
                $errorThrown | Should -Match "Content-Type:"
            }
            finally {
                Remove-Item $outFile -Force -ErrorAction SilentlyContinue
                Remove-Item "$outFile.tmp" -Force -ErrorAction SilentlyContinue
            }
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

Describe "SandboxDiscovery.SmokeMode" {
    
    Context "sandbox-install.ps1 smoke mode parameter" {
        
        It "Should have SmokeWindowsAppRuntime parameter" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match '\[switch\]\$SmokeWindowsAppRuntime'
        }
        
        It "Should have Invoke-SmokeWindowsAppRuntime function" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'function\s+Invoke-SmokeWindowsAppRuntime'
        }
        
        It "Should have smoke mode early exit logic" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'if\s*\(\$SmokeWindowsAppRuntime\)'
        }
        
        It "Should document smoke mode in synopsis" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            $content | Should -Match 'SMOKE MODE'
        }
    }
    
    Context "Invoke-SmokeWindowsAppRuntime function structure" {
        
        It "Should have ResolveFinalUrlFn parameter for DI" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Check within Invoke-SmokeWindowsAppRuntime function
            $content | Should -Match 'Invoke-SmokeWindowsAppRuntime[\s\S]*?\[scriptblock\]\$ResolveFinalUrlFn'
        }
        
        It "Should have DownloadFn parameter for DI" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Check within Invoke-SmokeWindowsAppRuntime function
            $content | Should -Match 'Invoke-SmokeWindowsAppRuntime[\s\S]*?\[scriptblock\]\$DownloadFn'
        }
        
        It "Should return result object with diagnostic fields" {
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            # Check for key diagnostic fields in result object
            $content | Should -Match 'FinalUrl\s*=\s*\$null'
            $content | Should -Match 'RedirectChain\s*=\s*@\(\)'
            $content | Should -Match 'RedirectHopCount\s*='
            $content | Should -Match 'MagicBytes\s*='
            $content | Should -Match 'FirstBytesHex\s*='
            $content | Should -Match 'FirstBytesAscii\s*='
        }
    }
    
    Context "Invoke-SmokeWindowsAppRuntime runtime behavior" {
        
        BeforeAll {
            # Load required functions from sandbox-install.ps1
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            
            # Extract Resolve-FinalUrl function
            if ($content -match '(?s)(function Resolve-FinalUrl \{.+?\n\})\s*\n\s*function') {
                Invoke-Expression $matches[1]
            }
            
            # Extract Test-ZipMagic function
            if ($content -match '(?s)(function Test-ZipMagic \{.+?\n\})') {
                Invoke-Expression $matches[1]
            }
            
            # Extract Invoke-SmokeWindowsAppRuntime function
            if ($content -match '(?s)(function Invoke-SmokeWindowsAppRuntime \{.+?\n\})\s*\n\s*function Ensure-WindowsAppRuntime18') {
                Invoke-Expression $matches[1]
            }
        }
        
        It "Should return Success=true when download produces valid ZIP" {
            $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "smoke-test-$(Get-Random)"
            
            try {
                # Fake resolver
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = "https://cdn.example.com/runtime.zip"
                        RedirectChain = @("https://aka.ms/redirect1")
                        ContentType = "application/zip"
                        ContentLength = 2000000
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes valid ZIP
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    [byte[]]$zipBytes = @(0x50, 0x4B, 0x03, 0x04) + @(0x00) * 1000
                    [System.IO.File]::WriteAllBytes($outFile, $zipBytes)
                    return $true
                }
                
                $result = Invoke-SmokeWindowsAppRuntime `
                    -TempDir $tempDir `
                    -ResolveFinalUrlFn $fakeResolver `
                    -DownloadFn $fakeDownloader
                
                $result.Success | Should -Be $true
                $result.FinalUrl | Should -Be "https://cdn.example.com/runtime.zip"
                $result.RedirectHopCount | Should -Be 1
                $result.ZipValid | Should -Be $true
                $result.MagicBytes | Should -Be "50 4B 03 04"
                $result.Error | Should -BeNullOrEmpty
            }
            finally {
                Remove-Item $tempDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should return Success=false with diagnostics when download produces HTML" {
            $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "smoke-test-$(Get-Random)"
            
            try {
                # Fake resolver
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = "https://cdn.example.com/error.html"
                        RedirectChain = @("https://aka.ms/redirect1", "https://aka.ms/redirect2")
                        ContentType = "text/html"
                        ContentLength = 500
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes HTML
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    $htmlContent = "<!DOCTYPE html><html><body>Error</body></html>"
                    [System.IO.File]::WriteAllText($outFile, $htmlContent)
                    return $true
                }
                
                $result = Invoke-SmokeWindowsAppRuntime `
                    -TempDir $tempDir `
                    -ResolveFinalUrlFn $fakeResolver `
                    -DownloadFn $fakeDownloader
                
                $result.Success | Should -Be $false
                $result.FinalUrl | Should -Be "https://cdn.example.com/error.html"
                $result.RedirectHopCount | Should -Be 2
                $result.ZipValid | Should -Be $false
                $result.FirstBytesAscii | Should -Match "<!DOCTYPE"
                $result.Error | Should -Match "HTML|ZIP"
            }
            finally {
                Remove-Item $tempDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should return Success=false with diagnostics when download produces non-ZIP binary" {
            $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "smoke-test-$(Get-Random)"
            
            try {
                # Fake resolver
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = "https://cdn.example.com/bad.bin"
                        RedirectChain = @()
                        ContentType = "application/octet-stream"
                        ContentLength = 1000
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes non-ZIP binary
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    [byte[]]$badBytes = @(0xCA, 0xFE, 0xBA, 0xBE) + @(0x00) * 100
                    [System.IO.File]::WriteAllBytes($outFile, $badBytes)
                    return $true
                }
                
                $result = Invoke-SmokeWindowsAppRuntime `
                    -TempDir $tempDir `
                    -ResolveFinalUrlFn $fakeResolver `
                    -DownloadFn $fakeDownloader
                
                $result.Success | Should -Be $false
                $result.ZipValid | Should -Be $false
                $result.MagicBytes | Should -Be "CA FE BA BE"
                $result.Error | Should -Match "ZIP validation failed"
            }
            finally {
                Remove-Item $tempDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
        
        It "Should include redirect chain in result" {
            $tempDir = Join-Path ([System.IO.Path]::GetTempPath()) "smoke-test-$(Get-Random)"
            
            try {
                # Fake resolver with multi-hop redirect
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = "https://cdn.example.com/final.zip"
                        RedirectChain = @("https://aka.ms/step1", "https://aka.ms/step2", "https://cdn.example.com/final.zip")
                        ContentType = "application/zip"
                        ContentLength = 2000000
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes valid ZIP
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    [byte[]]$zipBytes = @(0x50, 0x4B, 0x03, 0x04) + @(0x00) * 1000
                    [System.IO.File]::WriteAllBytes($outFile, $zipBytes)
                    return $true
                }
                
                $result = Invoke-SmokeWindowsAppRuntime `
                    -TempDir $tempDir `
                    -ResolveFinalUrlFn $fakeResolver `
                    -DownloadFn $fakeDownloader
                
                $result.RedirectChain.Count | Should -Be 3
                $result.RedirectHopCount | Should -Be 3
                $result.RedirectChain[0] | Should -Be "https://aka.ms/step1"
            }
            finally {
                Remove-Item $tempDir -Recurse -Force -ErrorAction SilentlyContinue
            }
        }
    }
    
    Context "Invoke-RobustDownload final error includes first-bytes diagnostics" {
        
        BeforeAll {
            # Load required functions from sandbox-install.ps1
            $installScript = Join-Path $script:HarnessDir "sandbox-install.ps1"
            $content = Get-Content -Path $installScript -Raw
            
            # Extract Test-ZipMagic function
            if ($content -match '(?s)(function Test-ZipMagic \{.+?\n\})') {
                Invoke-Expression $matches[1]
            }
            
            # Extract Invoke-RobustDownload function
            if ($content -match '(?s)(function Invoke-RobustDownload \{.+?\n\})\s*\n\s*function Invoke-AppxInstall') {
                Invoke-Expression $matches[1]
            }
            
            # Mock Write-* functions
            function global:Write-Step { param($msg) }
            function global:Write-Heartbeat { param($msg, $Details) }
            function global:Write-Info { param($msg) }
            function global:Write-Pass { param($msg) }
            function global:Write-FatalError { param($Step, $Message, $Details) throw "$Message`n$Details" }
        }
        
        AfterAll {
            Remove-Item Function:\Write-Step -ErrorAction SilentlyContinue
            Remove-Item Function:\Write-Heartbeat -ErrorAction SilentlyContinue
            Remove-Item Function:\Write-Info -ErrorAction SilentlyContinue
            Remove-Item Function:\Write-Pass -ErrorAction SilentlyContinue
            Remove-Item Function:\Write-FatalError -ErrorAction SilentlyContinue
        }
        
        It "Should include first-bytes diagnostics in final error when ValidateZip fails and retries exhaust" {
            $tempDir = [System.IO.Path]::GetTempPath()
            $outFile = Join-Path $tempDir "test-download-$(Get-Random).zip"
            
            try {
                # Fake resolver
                $fakeResolver = {
                    param($url)
                    [PSCustomObject]@{
                        FinalUrl = "https://cdn.example.com/bad.zip"
                        RedirectChain = @("https://aka.ms/redirect")
                        ContentType = "application/octet-stream"
                        ContentLength = 1000000
                        Success = $true
                        Error = $null
                    }
                }
                
                # Fake downloader that writes non-ZIP binary (large enough to pass size check)
                $fakeDownloader = {
                    param($url, $outFile, $timeout)
                    [byte[]]$badBytes = @(0xDE, 0xAD, 0xBE, 0xEF) + @(0x00) * 1000000
                    [System.IO.File]::WriteAllBytes($outFile, $badBytes)
                    return $true
                }
                
                $errorThrown = $null
                try {
                    Invoke-RobustDownload `
                        -Url "https://aka.ms/test" `
                        -OutFile $outFile `
                        -StepName "TestDownload" `
                        -MinExpectedBytes 100 `
                        -MaxRetries 1 `
                        -ValidateZip `
                        -ResolveFinalUrlFn $fakeResolver `
                        -DownloadFn $fakeDownloader
                } catch {
                    $errorThrown = $_.Exception.Message
                }
                
                $errorThrown | Should -Not -BeNullOrEmpty
                # Verify final error includes first-bytes diagnostics
                $errorThrown | Should -Match "Final URL:"
                $errorThrown | Should -Match "Redirect chain:"
                $errorThrown | Should -Match "Magic bytes:"
                $errorThrown | Should -Match "First 32 bytes \(hex\):"
                $errorThrown | Should -Match "First 32 bytes \(ASCII\):"
            }
            finally {
                Remove-Item $outFile -Force -ErrorAction SilentlyContinue
                Remove-Item "$outFile.tmp" -Force -ErrorAction SilentlyContinue
            }
        }
    }
}
