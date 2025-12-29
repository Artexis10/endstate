# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Contract tests for NDJSON streaming events.

.DESCRIPTION
    These tests verify that the engine emits proper NDJSON events to stderr
    when --events jsonl is enabled. Uses ENDSTATE_TESTMODE=1 for deterministic
    execution without real system calls.

.NOTES
    Run via: pwsh scripts/test-engine.ps1 -Path tests/contract
#>

BeforeAll {
    $script:RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
    $script:EndstateScript = Join-Path $script:RepoRoot "endstate.ps1"
    
    # Helper function to invoke endstate and capture stderr
    function Invoke-EndstateWithEvents {
        param(
            [Parameter(Mandatory = $true)]
            [string]$Command
        )
        
        $psi = [System.Diagnostics.ProcessStartInfo]::new()
        $psi.FileName = "pwsh"
        $psi.Arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$script:EndstateScript`" $Command --events jsonl"
        $psi.RedirectStandardOutput = $true
        $psi.RedirectStandardError = $true
        $psi.UseShellExecute = $false
        $psi.CreateNoWindow = $true
        $psi.Environment["ENDSTATE_TESTMODE"] = "1"
        
        $process = [System.Diagnostics.Process]::new()
        $process.StartInfo = $psi
        $process.Start() | Out-Null
        
        $stdout = $process.StandardOutput.ReadToEnd()
        $stderr = $process.StandardError.ReadToEnd()
        $process.WaitForExit()
        
        return @{
            ExitCode = $process.ExitCode
            Stdout = $stdout
            Stderr = $stderr
        }
    }
    
    # Helper to parse NDJSON lines
    function Get-NdjsonEvents {
        param(
            [Parameter(Mandatory = $true)]
            [string]$Stderr
        )
        
        $lines = $Stderr.Split("`n") | Where-Object { $_.Trim() -ne "" }
        $events = @()
        
        foreach ($line in $lines) {
            try {
                $parsed = $line | ConvertFrom-Json
                $events += $parsed
            } catch {
                # Skip non-JSON lines (e.g., PowerShell warnings)
            }
        }
        
        return $events
    }
}

Describe "NDJSON Events Contract" -Tag "Contract", "Events" {
    
    Context "capture command" {
        BeforeAll {
            $script:CaptureResult = Invoke-EndstateWithEvents -Command "capture"
            $script:CaptureEvents = Get-NdjsonEvents -Stderr $script:CaptureResult.Stderr
        }
        
        It "Should exit successfully in test mode" {
            $script:CaptureResult.ExitCode | Should -Be 0
        }
        
        It "Should emit at least one NDJSON event to stderr" {
            $script:CaptureEvents.Count | Should -BeGreaterThan 0
        }
        
        It "Should emit events with required fields: version, event, timestamp" {
            foreach ($event in $script:CaptureEvents) {
                $event.version | Should -Be 1
                $event.event | Should -Not -BeNullOrEmpty
                $event.timestamp | Should -Not -BeNullOrEmpty
            }
        }
        
        It "Should emit at least one phase event with phase=capture" {
            $phaseEvents = @($script:CaptureEvents | Where-Object { $_.event -eq "phase" -and $_.phase -eq "capture" })
            $phaseEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "Should emit at least one summary event" {
            $summaryEvents = @($script:CaptureEvents | Where-Object { $_.event -eq "summary" })
            $summaryEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "Should emit an artifact event" {
            $artifactEvents = @($script:CaptureEvents | Where-Object { $_.event -eq "artifact" })
            $artifactEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "Summary event should have required fields" {
            $summaryEvent = $script:CaptureEvents | Where-Object { $_.event -eq "summary" } | Select-Object -First 1
            $summaryEvent.phase | Should -Be "capture"
            $summaryEvent.total | Should -BeGreaterOrEqual 0
            $summaryEvent.success | Should -BeGreaterOrEqual 0
            $summaryEvent.skipped | Should -BeGreaterOrEqual 0
            $summaryEvent.failed | Should -BeGreaterOrEqual 0
        }
    }
    
    Context "apply command" {
        BeforeAll {
            $script:ApplyResult = Invoke-EndstateWithEvents -Command "apply"
            $script:ApplyEvents = Get-NdjsonEvents -Stderr $script:ApplyResult.Stderr
        }
        
        It "Should exit successfully in test mode" {
            $script:ApplyResult.ExitCode | Should -Be 0
        }
        
        It "Should emit at least one NDJSON event to stderr" {
            $script:ApplyEvents.Count | Should -BeGreaterThan 0
        }
        
        It "Should emit events with required fields: version, event, timestamp" {
            foreach ($event in $script:ApplyEvents) {
                $event.version | Should -Be 1
                $event.event | Should -Not -BeNullOrEmpty
                $event.timestamp | Should -Not -BeNullOrEmpty
            }
        }
        
        It "Should emit at least one phase event with phase=apply" {
            $phaseEvents = @($script:ApplyEvents | Where-Object { $_.event -eq "phase" -and $_.phase -eq "apply" })
            $phaseEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "Should emit at least one summary event" {
            $summaryEvents = @($script:ApplyEvents | Where-Object { $_.event -eq "summary" })
            $summaryEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "Should emit item events" {
            $itemEvents = @($script:ApplyEvents | Where-Object { $_.event -eq "item" })
            $itemEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "Summary event should have required fields" {
            $summaryEvent = $script:ApplyEvents | Where-Object { $_.event -eq "summary" } | Select-Object -First 1
            $summaryEvent.phase | Should -Be "apply"
            $summaryEvent.total | Should -BeGreaterOrEqual 0
            $summaryEvent.success | Should -BeGreaterOrEqual 0
            $summaryEvent.skipped | Should -BeGreaterOrEqual 0
            $summaryEvent.failed | Should -BeGreaterOrEqual 0
        }
    }
    
    Context "verify command" {
        BeforeAll {
            $script:VerifyResult = Invoke-EndstateWithEvents -Command "verify"
            $script:VerifyEvents = Get-NdjsonEvents -Stderr $script:VerifyResult.Stderr
        }
        
        It "Should exit successfully in test mode" {
            $script:VerifyResult.ExitCode | Should -Be 0
        }
        
        It "Should emit at least one NDJSON event to stderr" {
            $script:VerifyEvents.Count | Should -BeGreaterThan 0
        }
        
        It "Should emit events with required fields: version, event, timestamp" {
            foreach ($event in $script:VerifyEvents) {
                $event.version | Should -Be 1
                $event.event | Should -Not -BeNullOrEmpty
                $event.timestamp | Should -Not -BeNullOrEmpty
            }
        }
        
        It "Should emit at least one phase event with phase=verify" {
            $phaseEvents = @($script:VerifyEvents | Where-Object { $_.event -eq "phase" -and $_.phase -eq "verify" })
            $phaseEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "Should emit at least one summary event" {
            $summaryEvents = @($script:VerifyEvents | Where-Object { $_.event -eq "summary" })
            $summaryEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "Should emit item events" {
            $itemEvents = @($script:VerifyEvents | Where-Object { $_.event -eq "item" })
            $itemEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "Summary event should have required fields" {
            $summaryEvent = $script:VerifyEvents | Where-Object { $_.event -eq "summary" } | Select-Object -First 1
            $summaryEvent.phase | Should -Be "verify"
            $summaryEvent.total | Should -BeGreaterOrEqual 0
            $summaryEvent.success | Should -BeGreaterOrEqual 0
            $summaryEvent.skipped | Should -BeGreaterOrEqual 0
            $summaryEvent.failed | Should -BeGreaterOrEqual 0
        }
    }
    
    Context "NDJSON format compliance" {
        BeforeAll {
            $script:Result = Invoke-EndstateWithEvents -Command "apply"
        }
        
        It "Each line should be valid JSON (no prefixes, no banners)" {
            $lines = $script:Result.Stderr.Split("`n") | Where-Object { $_.Trim() -ne "" }
            
            foreach ($line in $lines) {
                # Each non-empty line must parse as JSON
                { $line | ConvertFrom-Json } | Should -Not -Throw
            }
        }
        
        It "Events should be single-line JSON (no pretty-printing)" {
            $lines = $script:Result.Stderr.Split("`n") | Where-Object { $_.Trim() -ne "" }
            
            foreach ($line in $lines) {
                # Should not contain newlines within the JSON
                $line | Should -Not -Match "`n"
                # Should start with { and end with }
                $line.Trim() | Should -Match '^\{.*\}$'
            }
        }
    }
}

Describe "Native process stderr redirection contract" -Tag "Contract", "Events", "Native" {
    <#
    .DESCRIPTION
        These tests verify the non-negotiable contract: NDJSON events MUST be captured
        by native process stderr redirection (2>) when using cmd /c.
        
        This is the authoritative test for the event emission contract.
    #>
    
    BeforeAll {
        $script:TempDir = Join-Path $env:TEMP "endstate-contract-tests-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TempDir -Force | Out-Null
        
        # Get the repo root endstate.cmd shim for testing
        $script:EndstateCmd = Join-Path $script:RepoRoot "endstate.cmd"
    }
    
    AfterAll {
        if (Test-Path $script:TempDir) {
            Remove-Item -Path $script:TempDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
    
    Context "PowerShell redirection via native shim (user repro)" {
        <#
        .DESCRIPTION
            This test reproduces the exact user scenario:
            endstate apply ... --events jsonl 1> stdout.txt 2> events.jsonl
            
            This MUST work when invoked from PowerShell.
        #>
        BeforeAll {
            $script:OutFile = Join-Path $script:TempDir "shim-out.txt"
            $script:ErrFile = Join-Path $script:TempDir "shim-err.jsonl"
            
            # Run via the native .cmd shim with PowerShell redirection
            # This is the exact user scenario that was broken
            $env:ENDSTATE_TESTMODE = "1"
            $cmdLine = "`"$script:EndstateCmd`" apply --events jsonl 1> `"$script:OutFile`" 2> `"$script:ErrFile`""
            cmd /c $cmdLine
            $env:ENDSTATE_TESTMODE = $null
        }
        
        It "stderr file should exist and have content" {
            Test-Path $script:ErrFile | Should -BeTrue
            (Get-Item $script:ErrFile).Length | Should -BeGreaterThan 0
        }
        
        It "stdout file should exist (may have banner)" {
            Test-Path $script:OutFile | Should -BeTrue
        }
        
        It "every stderr line should be valid NDJSON with event field" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lines.Count | Should -BeGreaterThan 0
            foreach ($line in $lines) {
                $parsed = $line | ConvertFrom-Json
                $parsed.event | Should -Not -BeNullOrEmpty
                $parsed.version | Should -Be 1
                $parsed.timestamp | Should -Not -BeNullOrEmpty
            }
        }
        
        It "stdout should NOT contain any NDJSON events" {
            $content = Get-Content $script:OutFile -Raw -ErrorAction SilentlyContinue
            if ($content) {
                Select-String -InputObject $content -Pattern '"event"\s*:\s*"' | Should -BeNullOrEmpty
            }
        }
        
        It "stderr should contain phase and summary events" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            $phaseEvents = @($events | Where-Object { $_.event -eq "phase" })
            $summaryEvents = @($events | Where-Object { $_.event -eq "summary" })
            $phaseEvents.Count | Should -BeGreaterOrEqual 1
            $summaryEvents.Count | Should -BeGreaterOrEqual 1
        }
    }
    
    Context "apply --events jsonl" {
        BeforeAll {
            $script:OutFile = Join-Path $script:TempDir "apply-out.txt"
            $script:ErrFile = Join-Path $script:TempDir "apply-err.jsonl"
            
            # Run via cmd /c with native file redirection - this is the contract
            $env:ENDSTATE_TESTMODE = "1"
            $cmdLine = "pwsh -NoProfile -ExecutionPolicy Bypass -File `"$script:EndstateScript`" apply --events jsonl 1> `"$script:OutFile`" 2> `"$script:ErrFile`""
            cmd /c $cmdLine
            $env:ENDSTATE_TESTMODE = $null
        }
        
        It "stderr file should exist" {
            Test-Path $script:ErrFile | Should -BeTrue
        }
        
        It "stderr file should have content (size > 0)" {
            (Get-Item $script:ErrFile).Length | Should -BeGreaterThan 0
        }
        
        It "every line in stderr should parse as valid JSON" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lines.Count | Should -BeGreaterThan 0
            foreach ($line in $lines) {
                { $line | ConvertFrom-Json } | Should -Not -Throw
            }
        }
        
        It "stderr should contain event fields" {
            $content = Get-Content $script:ErrFile -Raw
            $content | Should -Match '"event"\s*:'
        }
        
        It "stdout should NOT contain any NDJSON events" {
            $content = Get-Content $script:OutFile -Raw -ErrorAction SilentlyContinue
            if ($content) {
                Select-String -InputObject $content -Pattern '"event"\s*:\s*"' | Should -BeNullOrEmpty
            }
        }
    }
    
    Context "capture --events jsonl" {
        BeforeAll {
            $script:OutFile = Join-Path $script:TempDir "capture-out.txt"
            $script:ErrFile = Join-Path $script:TempDir "capture-err.jsonl"
            
            $env:ENDSTATE_TESTMODE = "1"
            $cmdLine = "pwsh -NoProfile -ExecutionPolicy Bypass -File `"$script:EndstateScript`" capture --events jsonl 1> `"$script:OutFile`" 2> `"$script:ErrFile`""
            cmd /c $cmdLine
            $env:ENDSTATE_TESTMODE = $null
        }
        
        It "stderr file should exist" {
            Test-Path $script:ErrFile | Should -BeTrue
        }
        
        It "stderr file should have content (size > 0)" {
            (Get-Item $script:ErrFile).Length | Should -BeGreaterThan 0
        }
        
        It "every line in stderr should parse as valid JSON" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lines.Count | Should -BeGreaterThan 0
            foreach ($line in $lines) {
                { $line | ConvertFrom-Json } | Should -Not -Throw
            }
        }
        
        It "stderr should contain event fields" {
            $content = Get-Content $script:ErrFile -Raw
            $content | Should -Match '"event"\s*:'
        }
        
        It "stdout should NOT contain any NDJSON events" {
            $content = Get-Content $script:OutFile -Raw -ErrorAction SilentlyContinue
            if ($content) {
                Select-String -InputObject $content -Pattern '"event"\s*:\s*"' | Should -BeNullOrEmpty
            }
        }
    }
    
    Context "verify --events jsonl" {
        BeforeAll {
            $script:OutFile = Join-Path $script:TempDir "verify-out.txt"
            $script:ErrFile = Join-Path $script:TempDir "verify-err.jsonl"
            
            $env:ENDSTATE_TESTMODE = "1"
            $cmdLine = "pwsh -NoProfile -ExecutionPolicy Bypass -File `"$script:EndstateScript`" verify --events jsonl 1> `"$script:OutFile`" 2> `"$script:ErrFile`""
            cmd /c $cmdLine
            $env:ENDSTATE_TESTMODE = $null
        }
        
        It "stderr file should exist" {
            Test-Path $script:ErrFile | Should -BeTrue
        }
        
        It "stderr file should have content (size > 0)" {
            (Get-Item $script:ErrFile).Length | Should -BeGreaterThan 0
        }
        
        It "every line in stderr should parse as valid JSON" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lines.Count | Should -BeGreaterThan 0
            foreach ($line in $lines) {
                { $line | ConvertFrom-Json } | Should -Not -Throw
            }
        }
        
        It "stderr should contain event fields" {
            $content = Get-Content $script:ErrFile -Raw
            $content | Should -Match '"event"\s*:'
        }
        
        It "stdout should NOT contain any NDJSON events" {
            $content = Get-Content $script:OutFile -Raw -ErrorAction SilentlyContinue
            if ($content) {
                Select-String -InputObject $content -Pattern '"event"\s*:\s*"' | Should -BeNullOrEmpty
            }
        }
    }
}

Describe "Events disabled by default" -Tag "Contract", "Events" {
    
    It "Should NOT emit events when --events jsonl is not specified" {
        $psi = [System.Diagnostics.ProcessStartInfo]::new()
        $psi.FileName = "pwsh"
        $psi.Arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$script:EndstateScript`" apply"
        $psi.RedirectStandardOutput = $true
        $psi.RedirectStandardError = $true
        $psi.UseShellExecute = $false
        $psi.CreateNoWindow = $true
        $psi.Environment["ENDSTATE_TESTMODE"] = "1"
        
        $process = [System.Diagnostics.Process]::new()
        $process.StartInfo = $psi
        $process.Start() | Out-Null
        
        $stderr = $process.StandardError.ReadToEnd()
        $process.WaitForExit()
        
        # Parse any JSON events (handle empty stderr)
        if ([string]::IsNullOrWhiteSpace($stderr)) {
            $events = @()
        } else {
            $events = Get-NdjsonEvents -Stderr $stderr
        }
        
        # Should have no events when --events jsonl is not specified
        $events.Count | Should -Be 0
    }
}

Describe "Entrypoint guard contract" -Tag "Contract", "Entrypoint" {
    <#
    .DESCRIPTION
        These tests verify the native shim entrypoint guard:
        - Get-Command endstate should resolve to the .cmd shim (not .ps1)
        - Direct execution of endstate.ps1 should fail with an error message
    #>
    
    BeforeAll {
        $script:RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
        $script:EndstateScript = Join-Path $script:RepoRoot "endstate.ps1"
        $script:EndstateCmd = Join-Path $script:RepoRoot "endstate.cmd"
    }
    
    Context "Command resolution" {
        It "Get-Command endstate should show CommandType Application" {
            $cmd = Get-Command endstate -ErrorAction SilentlyContinue
            $cmd | Should -Not -BeNullOrEmpty
            $cmd.CommandType | Should -Be "Application"
        }
        
        It "Get-Command endstate Source should end with endstate.cmd" {
            $cmd = Get-Command endstate -ErrorAction SilentlyContinue
            $cmd | Should -Not -BeNullOrEmpty
            $cmd.Source | Should -Match 'endstate\.cmd$'
        }
    }
    
    Context "Direct ps1 execution guard" {
        It "Direct execution of endstate.ps1 should fail with exit code 1" {
            $psi = [System.Diagnostics.ProcessStartInfo]::new()
            $psi.FileName = "pwsh"
            $psi.Arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$script:EndstateScript`" apply"
            $psi.RedirectStandardOutput = $true
            $psi.RedirectStandardError = $true
            $psi.UseShellExecute = $false
            $psi.CreateNoWindow = $true
            # Do NOT set ENDSTATE_ENTRYPOINT or ENDSTATE_TESTMODE - simulate direct invocation
            
            $process = [System.Diagnostics.Process]::new()
            $process.StartInfo = $psi
            $process.Start() | Out-Null
            
            $stderr = $process.StandardError.ReadToEnd()
            $process.WaitForExit()
            
            $process.ExitCode | Should -Be 1
        }
        
        It "Direct execution should print the expected error message" {
            $psi = [System.Diagnostics.ProcessStartInfo]::new()
            $psi.FileName = "pwsh"
            $psi.Arguments = "-NoProfile -ExecutionPolicy Bypass -File `"$script:EndstateScript`" apply"
            $psi.RedirectStandardOutput = $true
            $psi.RedirectStandardError = $true
            $psi.UseShellExecute = $false
            $psi.CreateNoWindow = $true
            
            $process = [System.Diagnostics.Process]::new()
            $process.StartInfo = $psi
            $process.Start() | Out-Null
            
            $stderr = $process.StandardError.ReadToEnd()
            $process.WaitForExit()
            
            $stderr | Should -Match 'Do not run endstate\.ps1 directly'
            $stderr | Should -Match 'endstate\.cmd'
        }
        
        It "Execution via endstate.cmd should succeed (ENDSTATE_ENTRYPOINT=cmd)" {
            $psi = [System.Diagnostics.ProcessStartInfo]::new()
            $psi.FileName = "cmd"
            $psi.Arguments = "/c `"`"$script:EndstateCmd`" --help`""
            $psi.RedirectStandardOutput = $true
            $psi.RedirectStandardError = $true
            $psi.UseShellExecute = $false
            $psi.CreateNoWindow = $true
            
            $process = [System.Diagnostics.Process]::new()
            $process.StartInfo = $psi
            $process.Start() | Out-Null
            
            $stdout = $process.StandardOutput.ReadToEnd()
            $stderr = $process.StandardError.ReadToEnd()
            $process.WaitForExit()
            
            # Should NOT contain the guard error message
            $stderr | Should -Not -Match 'Do not run endstate\.ps1 directly'
            # Should show help or version info
            $stdout | Should -Match 'Endstate'
        }
    }
}
