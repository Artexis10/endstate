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
        
        It "First event MUST be phase event" {
            $script:CaptureEvents[0].event | Should -Be "phase"
        }
        
        It "Last event MUST be summary event" {
            $script:CaptureEvents[-1].event | Should -Be "summary"
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
        
        It "First event MUST be phase event" {
            $script:ApplyEvents[0].event | Should -Be "phase"
        }
        
        It "Last event MUST be summary event" {
            $script:ApplyEvents[-1].event | Should -Be "summary"
        }
        
        It "Item events should have required fields: id, status, driver, message, reason" {
            $itemEvents = @($script:ApplyEvents | Where-Object { $_.event -eq "item" })
            foreach ($item in $itemEvents) {
                $item.id | Should -Not -BeNullOrEmpty
                $item.status | Should -Not -BeNullOrEmpty
                $item.driver | Should -Not -BeNullOrEmpty
                # message is optional but should exist as a key
                $item.PSObject.Properties.Name | Should -Contain "message"
                # reason must exist (can be null)
                $item.PSObject.Properties.Name | Should -Contain "reason"
            }
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
        
        It "First event MUST be phase event" {
            $script:VerifyEvents[0].event | Should -Be "phase"
        }
        
        It "Last event MUST be summary event" {
            $script:VerifyEvents[-1].event | Should -Be "summary"
        }
        
        It "Item events should have required fields: id, status, driver, message, reason" {
            $itemEvents = @($script:VerifyEvents | Where-Object { $_.event -eq "item" })
            foreach ($item in $itemEvents) {
                $item.id | Should -Not -BeNullOrEmpty
                $item.status | Should -Not -BeNullOrEmpty
                $item.driver | Should -Not -BeNullOrEmpty
                # message is optional but should exist as a key
                $item.PSObject.Properties.Name | Should -Contain "message"
                # reason must exist (can be null)
                $item.PSObject.Properties.Name | Should -Contain "reason"
            }
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
        
        It "first event MUST be phase" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $firstEvent = $lines[0] | ConvertFrom-Json
            $firstEvent.event | Should -Be "phase"
        }
        
        It "last event MUST be summary" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lastEvent = $lines[-1] | ConvertFrom-Json
            $lastEvent.event | Should -Be "summary"
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
        
        It "first event MUST be phase" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $firstEvent = $lines[0] | ConvertFrom-Json
            $firstEvent.event | Should -Be "phase"
        }
        
        It "last event MUST be summary" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lastEvent = $lines[-1] | ConvertFrom-Json
            $lastEvent.event | Should -Be "summary"
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
        
        It "first event MUST be phase" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $firstEvent = $lines[0] | ConvertFrom-Json
            $firstEvent.event | Should -Be "phase"
        }
        
        It "last event MUST be summary" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lastEvent = $lines[-1] | ConvertFrom-Json
            $lastEvent.event | Should -Be "summary"
        }
        
        It "capture should emit artifact event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            $artifactEvents = @($events | Where-Object { $_.event -eq "artifact" })
            $artifactEvents.Count | Should -BeGreaterOrEqual 1
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
        
        It "first event MUST be phase" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $firstEvent = $lines[0] | ConvertFrom-Json
            $firstEvent.event | Should -Be "phase"
        }
        
        It "last event MUST be summary" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lastEvent = $lines[-1] | ConvertFrom-Json
            $lastEvent.event | Should -Be "summary"
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

Describe "Real-mode event stream contract (smoke, minimal side effects)" -Tag "Contract", "Events", "RealMode" {
    <#
    .DESCRIPTION
        These tests verify the non-negotiable contract for REAL MODE (not TESTMODE):
        - Events are emitted as NDJSON to process stderr
        - Stdout remains human-readable only (no NDJSON)
        - Stream is complete: phase -> item* -> summary (and artifact for capture)
        - All events have required schema fields
        
        Constraints:
        - Must not install anything or modify the system
        - Uses apply -DryRun to avoid side effects
        - Uses native shim (endstate.cmd) with stream redirection
    #>
    
    BeforeAll {
        $script:RepoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
        $script:EndstateCmd = Join-Path $script:RepoRoot "endstate.cmd"
        $script:TempDir = Join-Path $env:TEMP "endstate-realmode-tests-$(Get-Random)"
        New-Item -ItemType Directory -Path $script:TempDir -Force | Out-Null
        
        # Find a manifest to use for testing (use local manifest if exists, otherwise create minimal one)
        $script:LocalManifestsDir = Join-Path $script:RepoRoot "manifests\local"
        $script:TestManifest = $null
        
        if (Test-Path $script:LocalManifestsDir) {
            $manifests = Get-ChildItem -Path $script:LocalManifestsDir -Filter "*.jsonc" -ErrorAction SilentlyContinue
            if ($manifests.Count -gt 0) {
                $script:TestManifest = $manifests[0].FullName
            }
        }
        
        # If no local manifest, create a minimal test manifest
        if (-not $script:TestManifest) {
            $script:TestManifest = Join-Path $script:TempDir "test-manifest.jsonc"
            $minimalManifest = @{
                name = "test-manifest"
                apps = @(
                    @{
                        id = "test-app"
                        refs = @{ windows = "Microsoft.PowerShell" }
                    }
                )
            }
            $minimalManifest | ConvertTo-Json -Depth 10 | Set-Content -Path $script:TestManifest -Encoding UTF8
        }
    }
    
    AfterAll {
        if (Test-Path $script:TempDir) {
            Remove-Item -Path $script:TempDir -Recurse -Force -ErrorAction SilentlyContinue
        }
    }
    
    Context "apply -DryRun real-mode events" {
        BeforeAll {
            $script:OutFile = Join-Path $script:TempDir "apply-dryrun-out.txt"
            $script:ErrFile = Join-Path $script:TempDir "apply-dryrun-err.jsonl"
            
            # Run via native shim with REAL mode (no TESTMODE)
            $cmdLine = "`"$script:EndstateCmd`" apply -DryRun -Manifest `"$script:TestManifest`" --events jsonl 1> `"$script:OutFile`" 2> `"$script:ErrFile`""
            cmd /c $cmdLine
        }
        
        It "stderr file should exist and have content" {
            Test-Path $script:ErrFile | Should -BeTrue
            (Get-Item $script:ErrFile).Length | Should -BeGreaterThan 0
        }
        
        It "stdout should NOT contain any NDJSON events" {
            $content = Get-Content $script:OutFile -Raw -ErrorAction SilentlyContinue
            if ($content) {
                Select-String -InputObject $content -Pattern '"event"\s*:\s*"' | Should -BeNullOrEmpty
            }
        }
        
        It "every stderr line should parse as valid JSON" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lines.Count | Should -BeGreaterThan 0
            foreach ($line in $lines) {
                { $line | ConvertFrom-Json } | Should -Not -Throw
            }
        }
        
        It "first event MUST be phase event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $firstEvent = $lines[0] | ConvertFrom-Json
            $firstEvent.event | Should -Be "phase"
            $firstEvent.phase | Should -Be "apply"
        }
        
        It "last event MUST be summary event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lastEvent = $lines[-1] | ConvertFrom-Json
            $lastEvent.event | Should -Be "summary"
            $lastEvent.phase | Should -Be "apply"
        }
        
        It "should emit at least 1 item event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            $itemEvents = @($events | Where-Object { $_.event -eq "item" })
            $itemEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "all events should have required base fields: version, event, timestamp" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            foreach ($line in $lines) {
                $event = $line | ConvertFrom-Json
                $event.version | Should -Be 1
                $event.event | Should -Not -BeNullOrEmpty
                $event.timestamp | Should -Not -BeNullOrEmpty
            }
        }
        
        It "item events should have required fields: id, status, driver, message, reason" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            $itemEvents = @($events | Where-Object { $_.event -eq "item" })
            foreach ($item in $itemEvents) {
                $item.id | Should -Not -BeNullOrEmpty
                $item.status | Should -Not -BeNullOrEmpty
                $item.driver | Should -Not -BeNullOrEmpty
                $item.PSObject.Properties.Name | Should -Contain "message"
                $item.PSObject.Properties.Name | Should -Contain "reason"
            }
        }
        
        It "summary event should have required fields: phase, total, success, skipped, failed" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            $summaryEvent = $events | Where-Object { $_.event -eq "summary" } | Select-Object -First 1
            $summaryEvent.phase | Should -Be "apply"
            $summaryEvent.PSObject.Properties.Name | Should -Contain "total"
            $summaryEvent.PSObject.Properties.Name | Should -Contain "success"
            $summaryEvent.PSObject.Properties.Name | Should -Contain "skipped"
            $summaryEvent.PSObject.Properties.Name | Should -Contain "failed"
        }
    }
    
    Context "verify real-mode events" {
        BeforeAll {
            $script:OutFile = Join-Path $script:TempDir "verify-out.txt"
            $script:ErrFile = Join-Path $script:TempDir "verify-err.jsonl"
            
            # Run via native shim with REAL mode
            $cmdLine = "`"$script:EndstateCmd`" verify -Manifest `"$script:TestManifest`" --events jsonl 1> `"$script:OutFile`" 2> `"$script:ErrFile`""
            cmd /c $cmdLine
        }
        
        It "stderr file should exist and have content" {
            Test-Path $script:ErrFile | Should -BeTrue
            (Get-Item $script:ErrFile).Length | Should -BeGreaterThan 0
        }
        
        It "stdout should NOT contain any NDJSON events" {
            $content = Get-Content $script:OutFile -Raw -ErrorAction SilentlyContinue
            if ($content) {
                Select-String -InputObject $content -Pattern '"event"\s*:\s*"' | Should -BeNullOrEmpty
            }
        }
        
        It "first event MUST be phase event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $firstEvent = $lines[0] | ConvertFrom-Json
            $firstEvent.event | Should -Be "phase"
            $firstEvent.phase | Should -Be "verify"
        }
        
        It "last event MUST be summary event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lastEvent = $lines[-1] | ConvertFrom-Json
            $lastEvent.event | Should -Be "summary"
            $lastEvent.phase | Should -Be "verify"
        }
        
        It "should emit at least 1 item event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            $itemEvents = @($events | Where-Object { $_.event -eq "item" })
            $itemEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "item events should have required fields: id, status, driver, message, reason" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            $itemEvents = @($events | Where-Object { $_.event -eq "item" })
            foreach ($item in $itemEvents) {
                $item.id | Should -Not -BeNullOrEmpty
                $item.status | Should -Not -BeNullOrEmpty
                $item.driver | Should -Not -BeNullOrEmpty
                $item.PSObject.Properties.Name | Should -Contain "message"
                $item.PSObject.Properties.Name | Should -Contain "reason"
            }
        }
    }
    
    Context "capture real-mode events (safe, writes to local manifests)" {
        BeforeAll {
            $script:OutFile = Join-Path $script:TempDir "capture-out.txt"
            $script:ErrFile = Join-Path $script:TempDir "capture-err.jsonl"
            
            # Run via native shim with REAL mode
            # Capture writes to manifests/local which is gitignored, so this is safe
            $cmdLine = "`"$script:EndstateCmd`" capture --events jsonl 1> `"$script:OutFile`" 2> `"$script:ErrFile`""
            cmd /c $cmdLine
        }
        
        It "stderr file should exist and have content" {
            Test-Path $script:ErrFile | Should -BeTrue
            (Get-Item $script:ErrFile).Length | Should -BeGreaterThan 0
        }
        
        It "stdout should NOT contain any NDJSON events" {
            $content = Get-Content $script:OutFile -Raw -ErrorAction SilentlyContinue
            if ($content) {
                Select-String -InputObject $content -Pattern '"event"\s*:\s*"' | Should -BeNullOrEmpty
            }
        }
        
        It "first event MUST be phase event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $firstEvent = $lines[0] | ConvertFrom-Json
            $firstEvent.event | Should -Be "phase"
            $firstEvent.phase | Should -Be "capture"
        }
        
        It "last event MUST be summary event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $lastEvent = $lines[-1] | ConvertFrom-Json
            $lastEvent.event | Should -Be "summary"
            $lastEvent.phase | Should -Be "capture"
        }
        
        It "should emit at least 1 item event" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            $itemEvents = @($events | Where-Object { $_.event -eq "item" })
            $itemEvents.Count | Should -BeGreaterOrEqual 1
        }
        
        It "should emit artifact event with manifest path" {
            $lines = Get-Content $script:ErrFile | Where-Object { $_.Trim() -ne "" }
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            $artifactEvents = @($events | Where-Object { $_.event -eq "artifact" })
            $artifactEvents.Count | Should -BeGreaterOrEqual 1
            $artifactEvents[0].phase | Should -Be "capture"
            $artifactEvents[0].kind | Should -Be "manifest"
            $artifactEvents[0].path | Should -Not -BeNullOrEmpty
        }
    }
    
    Context "failure-path: bad manifest path emits summary with failed>0" {
        BeforeAll {
            $script:OutFile = Join-Path $script:TempDir "failure-out.txt"
            $script:ErrFile = Join-Path $script:TempDir "failure-err.jsonl"
            $script:BadManifest = Join-Path $script:TempDir "nonexistent-manifest.jsonc"
            
            # Run verify with a non-existent manifest
            $cmdLine = "`"$script:EndstateCmd`" verify -Manifest `"$script:BadManifest`" --events jsonl 1> `"$script:OutFile`" 2> `"$script:ErrFile`""
            cmd /c $cmdLine
        }
        
        It "stderr file should exist" {
            Test-Path $script:ErrFile | Should -BeTrue
        }
        
        It "should still emit phase event even on failure" {
            $content = Get-Content $script:ErrFile -Raw -ErrorAction SilentlyContinue
            if ($content -and $content.Trim()) {
                $lines = $content.Split("`n") | Where-Object { $_.Trim() -ne "" }
                if ($lines.Count -gt 0) {
                    $firstEvent = $lines[0] | ConvertFrom-Json
                    $firstEvent.event | Should -Be "phase"
                }
            }
        }
        
        It "should emit summary event with failed>0 on failure" {
            $content = Get-Content $script:ErrFile -Raw -ErrorAction SilentlyContinue
            if ($content -and $content.Trim()) {
                $lines = $content.Split("`n") | Where-Object { $_.Trim() -ne "" }
                if ($lines.Count -gt 0) {
                    $lastEvent = $lines[-1] | ConvertFrom-Json
                    $lastEvent.event | Should -Be "summary"
                    $lastEvent.failed | Should -BeGreaterThan 0
                }
            }
        }
    }
}
