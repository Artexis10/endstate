# Copyright 2025 Substrate Systems OÜ
# SPDX-License-Identifier: Apache-2.0

<#
.SYNOPSIS
    Unit tests for NDJSON streaming events module.

.DESCRIPTION
    Tests the events.ps1 module for correct NDJSON output format,
    event types, and schema compliance.
#>

BeforeAll {
    $script:EngineRoot = Join-Path $PSScriptRoot "..\..\engine"
    . "$script:EngineRoot\events.ps1"
}

Describe "Streaming Events Module" {
    BeforeEach {
        # Reset events state before each test
        Disable-StreamingEvents
    }

    AfterEach {
        Disable-StreamingEvents
    }

    Context "Enable/Disable Events" {
        It "Should be disabled by default" {
            Test-StreamingEventsEnabled | Should -Be $false
        }

        It "Should enable events when Enable-StreamingEvents is called" {
            Enable-StreamingEvents
            Test-StreamingEventsEnabled | Should -Be $true
        }

        It "Should disable events when Disable-StreamingEvents is called" {
            Enable-StreamingEvents
            Disable-StreamingEvents
            Test-StreamingEventsEnabled | Should -Be $false
        }
    }

    Context "RFC3339 Timestamp" {
        It "Should return valid RFC3339 UTC timestamp" {
            $timestamp = Get-Rfc3339Timestamp
            $timestamp | Should -Match '^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$'
        }

        It "Should return UTC time (ends with Z)" {
            $timestamp = Get-Rfc3339Timestamp
            $timestamp | Should -Match 'Z$'
        }
    }

    Context "Write-StreamingEvent" {
        It "Should not write when events are disabled" {
            $stderr = $null
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-StreamingEvent @{ event = "test" }
                $stderr = $stringWriter.ToString()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $stderr | Should -BeNullOrEmpty
        }

        It "Should write valid JSON when events are enabled" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-StreamingEvent @{ event = "test"; data = "value" }
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.event | Should -Be "test"
            $parsed.data | Should -Be "value"
        }

        It "Should add version field automatically" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-StreamingEvent @{ event = "test" }
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.version | Should -Be 1
        }

        It "Should add timestamp field automatically" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-StreamingEvent @{ event = "test" }
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            # Verify timestamp is present in raw JSON (ConvertFrom-Json may parse it as DateTime)
            $stderr | Should -Match '"timestamp"\s*:\s*"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z"'
        }

        It "Should output single-line JSON (NDJSON format)" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-StreamingEvent @{ event = "test"; nested = @{ key = "value" } }
                $stderr = $stringWriter.ToString()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            # Should be exactly one line (plus newline)
            $lines = $stderr.Split("`n") | Where-Object { $_.Trim() }
            $lines.Count | Should -Be 1
        }
    }

    Context "Write-PhaseEvent" {
        It "Should emit phase event with correct structure" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-PhaseEvent -Phase "apply"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.version | Should -Be 1
            $parsed.event | Should -Be "phase"
            $parsed.phase | Should -Be "apply"
            $parsed.timestamp | Should -Not -BeNullOrEmpty
        }

        It "Should accept plan phase" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-PhaseEvent -Phase "plan"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.phase | Should -Be "plan"
        }

        It "Should accept verify phase" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-PhaseEvent -Phase "verify"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.phase | Should -Be "verify"
        }

        It "Should accept capture phase" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-PhaseEvent -Phase "capture"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.phase | Should -Be "capture"
        }
    }

    Context "Write-ItemEvent" {
        It "Should emit item event with all required fields" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-ItemEvent -Id "Notepad++.Notepad++" -Driver "winget" -Status "installing"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.version | Should -Be 1
            $parsed.event | Should -Be "item"
            $parsed.id | Should -Be "Notepad++.Notepad++"
            $parsed.driver | Should -Be "winget"
            $parsed.status | Should -Be "installing"
            $parsed.timestamp | Should -Not -BeNullOrEmpty
        }

        It "Should include reason when provided" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-ItemEvent -Id "App.Id" -Driver "winget" -Status "skipped" -Reason "filtered"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.reason | Should -Be "filtered"
        }

        It "Should include message when provided" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-ItemEvent -Id "App.Id" -Driver "winget" -Status "failed" -Message "Connection timeout"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.message | Should -Be "Connection timeout"
        }

        It "Should set reason to null when not provided" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-ItemEvent -Id "App.Id" -Driver "winget" -Status "installed"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.reason | Should -Be $null
        }

        It "Should accept all valid status values" {
            Enable-StreamingEvents
            
            $validStatuses = @("to_install", "installing", "installed", "present", "skipped", "failed")
            
            foreach ($status in $validStatuses) {
                $originalError = [Console]::Error
                $stringWriter = [System.IO.StringWriter]::new()
                [Console]::SetError($stringWriter)
                
                try {
                    Write-ItemEvent -Id "App.Id" -Driver "winget" -Status $status
                    $stderr = $stringWriter.ToString().Trim()
                } finally {
                    [Console]::SetError($originalError)
                    $stringWriter.Dispose()
                }
                
                $parsed = $stderr | ConvertFrom-Json
                $parsed.status | Should -Be $status
            }
        }
    }

    Context "Write-SummaryEvent" {
        It "Should emit summary event with correct structure" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-SummaryEvent -Phase "apply" -Total 10 -Success 7 -Skipped 2 -Failed 1
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.version | Should -Be 1
            $parsed.event | Should -Be "summary"
            $parsed.phase | Should -Be "apply"
            $parsed.total | Should -Be 10
            $parsed.success | Should -Be 7
            $parsed.skipped | Should -Be 2
            $parsed.failed | Should -Be 1
            $parsed.timestamp | Should -Not -BeNullOrEmpty
        }

        It "Should accept verify phase" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-SummaryEvent -Phase "verify" -Total 5 -Success 4 -Skipped 0 -Failed 1
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.phase | Should -Be "verify"
        }

        It "Should accept capture phase" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-SummaryEvent -Phase "capture" -Total 15 -Success 12 -Skipped 3 -Failed 0
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.phase | Should -Be "capture"
            $parsed.total | Should -Be 15
            $parsed.success | Should -Be 12
            $parsed.skipped | Should -Be 3
        }
    }

    Context "Write-ArtifactEvent" {
        It "Should emit artifact event with correct structure" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-ArtifactEvent -Phase "capture" -Kind "manifest" -Path "C:\profiles\test.jsonc"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.version | Should -Be 1
            $parsed.event | Should -Be "artifact"
            $parsed.phase | Should -Be "capture"
            $parsed.kind | Should -Be "manifest"
            $parsed.path | Should -Be "C:\profiles\test.jsonc"
            $parsed.timestamp | Should -Not -BeNullOrEmpty
        }
    }

    Context "Write-ErrorEvent" {
        It "Should emit error event with correct structure" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-ErrorEvent -Scope "engine" -Message "Failed to connect"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.version | Should -Be 1
            $parsed.event | Should -Be "error"
            $parsed.scope | Should -Be "engine"
            $parsed.message | Should -Be "Failed to connect"
            $parsed.timestamp | Should -Not -BeNullOrEmpty
        }

        It "Should include item ID when provided for item scope" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-ErrorEvent -Scope "item" -Message "Install failed" -ItemId "App.Id"
                $stderr = $stringWriter.ToString().Trim()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $parsed = $stderr | ConvertFrom-Json
            $parsed.scope | Should -Be "item"
            $parsed.id | Should -Be "App.Id"
        }
    }

    Context "NDJSON Stream Format" {
        It "Should emit multiple events as separate lines" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-PhaseEvent -Phase "apply"
                Write-ItemEvent -Id "App1" -Driver "winget" -Status "installing"
                Write-ItemEvent -Id "App2" -Driver "winget" -Status "installed"
                Write-SummaryEvent -Phase "apply" -Total 2 -Success 2 -Skipped 0 -Failed 0
                $stderr = $stringWriter.ToString()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $lines = $stderr.Split("`n") | Where-Object { $_.Trim() }
            $lines.Count | Should -Be 4
            
            # Each line should be valid JSON
            foreach ($line in $lines) {
                { $line | ConvertFrom-Json } | Should -Not -Throw
            }
        }

        It "Should produce parseable NDJSON stream" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-PhaseEvent -Phase "plan"
                Write-ItemEvent -Id "App.Id" -Driver "winget" -Status "to_install"
                Write-PhaseEvent -Phase "apply"
                Write-ItemEvent -Id "App.Id" -Driver "winget" -Status "installing"
                Write-ItemEvent -Id "App.Id" -Driver "winget" -Status "installed"
                Write-SummaryEvent -Phase "apply" -Total 1 -Success 1 -Skipped 0 -Failed 0
                $stderr = $stringWriter.ToString()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $lines = $stderr.Split("`n") | Where-Object { $_.Trim() }
            
            # Parse each line and verify event types
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            
            $events[0].event | Should -Be "phase"
            $events[0].phase | Should -Be "plan"
            
            $events[1].event | Should -Be "item"
            $events[1].status | Should -Be "to_install"
            
            $events[2].event | Should -Be "phase"
            $events[2].phase | Should -Be "apply"
            
            $events[3].event | Should -Be "item"
            $events[3].status | Should -Be "installing"
            
            $events[4].event | Should -Be "item"
            $events[4].status | Should -Be "installed"
            
            $events[5].event | Should -Be "summary"
        }

        It "Should produce parseable capture NDJSON stream" {
            Enable-StreamingEvents
            
            $originalError = [Console]::Error
            $stringWriter = [System.IO.StringWriter]::new()
            [Console]::SetError($stringWriter)
            
            try {
                Write-PhaseEvent -Phase "capture"
                Write-ItemEvent -Id "Git.Git" -Driver "winget" -Status "present" -Reason "detected" -Message "Detected"
                Write-ItemEvent -Id "Microsoft.VCRedist.2019.x64" -Driver "winget" -Status "skipped" -Reason "filtered_runtime" -Message "Excluded (runtime)"
                Write-ItemEvent -Id "C:\Users\test\.ssh" -Driver "fs" -Status "skipped" -Reason "sensitive_excluded" -Message "Sensitive excluded"
                Write-ArtifactEvent -Phase "capture" -Kind "manifest" -Path "C:\profiles\test.jsonc"
                Write-SummaryEvent -Phase "capture" -Total 3 -Success 1 -Skipped 2 -Failed 0
                $stderr = $stringWriter.ToString()
            } finally {
                [Console]::SetError($originalError)
                $stringWriter.Dispose()
            }
            
            $lines = $stderr.Split("`n") | Where-Object { $_.Trim() }
            $lines.Count | Should -Be 6
            
            # Parse each line and verify event types
            $events = $lines | ForEach-Object { $_ | ConvertFrom-Json }
            
            $events[0].event | Should -Be "phase"
            $events[0].phase | Should -Be "capture"
            
            $events[1].event | Should -Be "item"
            $events[1].status | Should -Be "present"
            $events[1].reason | Should -Be "detected"
            
            $events[2].event | Should -Be "item"
            $events[2].status | Should -Be "skipped"
            $events[2].reason | Should -Be "filtered_runtime"
            
            $events[3].event | Should -Be "item"
            $events[3].status | Should -Be "skipped"
            $events[3].reason | Should -Be "sensitive_excluded"
            
            $events[4].event | Should -Be "artifact"
            $events[4].kind | Should -Be "manifest"
            
            $events[5].event | Should -Be "summary"
            $events[5].phase | Should -Be "capture"
        }
    }
}