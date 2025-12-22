param()

$ErrorActionPreference = 'Stop'

function Test-Command {
    param(
        [string]$TestName,
        [string[]]$Arguments,
        [hashtable]$Assertions
    )
    
    Write-Host "`n━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan
    Write-Host "TEST: $TestName" -ForegroundColor Cyan
    Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan
    
    $psi = New-Object System.Diagnostics.ProcessStartInfo
    $psi.FileName = "pwsh"
    $psi.Arguments = "-NoProfile -File autosuite.ps1 $($Arguments -join ' ')"
    $psi.WorkingDirectory = $PSScriptRoot
    $psi.RedirectStandardOutput = $true
    $psi.RedirectStandardError = $true
    $psi.UseShellExecute = $false
    $psi.CreateNoWindow = $true
    
    $process = New-Object System.Diagnostics.Process
    $process.StartInfo = $psi
    
    Write-Host "Running: autosuite $($Arguments -join ' ')" -ForegroundColor Yellow
    
    $process.Start() | Out-Null
    $stdout = $process.StandardOutput.ReadToEnd()
    $stderr = $process.StandardError.ReadToEnd()
    $process.WaitForExit()
    $exitCode = $process.ExitCode
    
    Write-Host "`nExit Code: $exitCode" -ForegroundColor $(if ($exitCode -eq 0) { 'Green' } else { 'Red' })
    Write-Host "`nSTDOUT Length: $($stdout.Length) bytes" -ForegroundColor Gray
    Write-Host "STDERR Length: $($stderr.Length) bytes" -ForegroundColor Gray
    
    if ($stderr) {
        Write-Host "`nSTDERR Content:" -ForegroundColor Magenta
        Write-Host $stderr -ForegroundColor DarkGray
    }
    
    $results = @{
        Passed = $true
        Failures = @()
    }
    
    # Assert: STDOUT is non-empty
    if ($Assertions.ContainsKey('StdoutNonEmpty') -and $Assertions.StdoutNonEmpty) {
        if ([string]::IsNullOrWhiteSpace($stdout)) {
            $results.Passed = $false
            $results.Failures += "STDOUT is empty"
        } else {
            Write-Host "✓ STDOUT is non-empty" -ForegroundColor Green
        }
    }
    
    # Assert: STDOUT is valid JSON
    $json = $null
    if ($Assertions.ContainsKey('ValidJson') -and $Assertions.ValidJson) {
        try {
            $json = $stdout | ConvertFrom-Json
            Write-Host "✓ STDOUT is valid JSON" -ForegroundColor Green
        } catch {
            $results.Passed = $false
            $results.Failures += "STDOUT is not valid JSON: $_"
            Write-Host "✗ STDOUT is not valid JSON" -ForegroundColor Red
            Write-Host "Raw STDOUT:" -ForegroundColor Yellow
            Write-Host $stdout -ForegroundColor DarkYellow
        }
    }
    
    # Assert: No non-JSON text in STDOUT
    if ($Assertions.ContainsKey('PureJson') -and $Assertions.PureJson -and $json) {
        $trimmed = $stdout.Trim()
        if (-not $trimmed.StartsWith('{') -or -not $trimmed.EndsWith('}')) {
            $results.Passed = $false
            $results.Failures += "STDOUT contains non-JSON text"
            Write-Host "✗ STDOUT contains non-JSON text" -ForegroundColor Red
        } else {
            Write-Host "✓ STDOUT is pure JSON (no extra text)" -ForegroundColor Green
        }
    }
    
    # Assert: Specific JSON fields
    if ($json -and $Assertions.ContainsKey('JsonFields')) {
        foreach ($field in $Assertions.JsonFields.Keys) {
            $expectedValue = $Assertions.JsonFields[$field]
            $actualValue = $json.$field
            
            if ($null -eq $actualValue) {
                $results.Passed = $false
                $results.Failures += "Missing field: $field"
                Write-Host "✗ Missing field: $field" -ForegroundColor Red
            } elseif ($expectedValue -ne $null -and $actualValue -ne $expectedValue) {
                $results.Passed = $false
                $results.Failures += "Field $field = $actualValue (expected: $expectedValue)"
                Write-Host "✗ Field $field = $actualValue (expected: $expectedValue)" -ForegroundColor Red
            } else {
                Write-Host "✓ Field $field = $actualValue" -ForegroundColor Green
            }
        }
    }
    
    # Assert: success field value
    if ($Assertions.ContainsKey('Success') -and $json) {
        if ($json.success -ne $Assertions.Success) {
            $results.Passed = $false
            $results.Failures += "success = $($json.success) (expected: $($Assertions.Success))"
            Write-Host "✗ success = $($json.success) (expected: $($Assertions.Success))" -ForegroundColor Red
        } else {
            Write-Host "✓ success = $($json.success)" -ForegroundColor Green
        }
    }
    
    # Assert: error field
    if ($Assertions.ContainsKey('ErrorNull') -and $json) {
        if ($Assertions.ErrorNull -and $json.error -ne $null) {
            $results.Passed = $false
            $results.Failures += "error should be null but is: $($json.error)"
            Write-Host "✗ error should be null" -ForegroundColor Red
        } elseif (-not $Assertions.ErrorNull -and $json.error -eq $null) {
            $results.Passed = $false
            $results.Failures += "error should be non-null"
            Write-Host "✗ error should be non-null" -ForegroundColor Red
        } else {
            Write-Host "✓ error field is correct" -ForegroundColor Green
        }
    }
    
    # Assert: error.code exists when error is non-null
    if ($Assertions.ContainsKey('ErrorCodeExists') -and $Assertions.ErrorCodeExists -and $json -and $json.error) {
        if (-not $json.error.code) {
            $results.Passed = $false
            $results.Failures += "error.code is missing"
            Write-Host "✗ error.code is missing" -ForegroundColor Red
        } else {
            Write-Host "✓ error.code exists: $($json.error.code)" -ForegroundColor Green
        }
    }
    
    # Assert: exit code
    if ($Assertions.ContainsKey('ExitCode')) {
        if ($exitCode -ne $Assertions.ExitCode) {
            $results.Passed = $false
            $results.Failures += "Exit code = $exitCode (expected: $($Assertions.ExitCode))"
            Write-Host "✗ Exit code = $exitCode (expected: $($Assertions.ExitCode))" -ForegroundColor Red
        } else {
            Write-Host "✓ Exit code = $exitCode" -ForegroundColor Green
        }
    }
    
    # Assert: data.hasState exists
    if ($Assertions.ContainsKey('HasStateField') -and $Assertions.HasStateField -and $json) {
        if ($null -eq $json.data.hasState) {
            $results.Passed = $false
            $results.Failures += "data.hasState is missing"
            Write-Host "✗ data.hasState is missing" -ForegroundColor Red
        } else {
            Write-Host "✓ data.hasState = $($json.data.hasState)" -ForegroundColor Green
        }
    }
    
    # Assert: Exactly one JSON object
    if ($Assertions.ContainsKey('SingleJsonObject') -and $Assertions.SingleJsonObject) {
        $lines = $stdout -split "`n" | Where-Object { $_.Trim() -ne '' }
        $jsonObjects = ($stdout | Select-String -Pattern '^\s*\{' -AllMatches).Matches.Count
        
        if ($jsonObjects -ne 1) {
            $results.Passed = $false
            $results.Failures += "Found $jsonObjects JSON objects (expected: 1)"
            Write-Host "✗ Found $jsonObjects JSON objects (expected: 1)" -ForegroundColor Red
        } else {
            Write-Host "✓ Exactly one JSON object in STDOUT" -ForegroundColor Green
        }
    }
    
    if ($results.Passed) {
        Write-Host "`n✓✓✓ TEST PASSED ✓✓✓" -ForegroundColor Green
    } else {
        Write-Host "`n✗✗✗ TEST FAILED ✗✗✗" -ForegroundColor Red
        Write-Host "Failures:" -ForegroundColor Red
        $results.Failures | ForEach-Object { Write-Host "  - $_" -ForegroundColor Red }
    }
    
    return $results
}

# Run all tests
$allResults = @()

# Test 1: Capabilities discovery
$allResults += Test-Command -TestName "1) Capabilities discovery" `
    -Arguments @('capabilities', '--json') `
    -Assertions @{
        StdoutNonEmpty = $true
        ValidJson = $true
        PureJson = $true
        JsonFields = @{
            schemaVersion = $null
            command = 'capabilities'
        }
        Success = $true
        ErrorNull = $true
        ExitCode = 0
    }

# Test 2: Initial GUI landing state
$allResults += Test-Command -TestName "2) Initial GUI landing state" `
    -Arguments @('report', '--json') `
    -Assertions @{
        ValidJson = $true
        JsonFields = @{
            command = 'report'
        }
        Success = $true
        ErrorNull = $true
        HasStateField = $true
        SingleJsonObject = $true
        ExitCode = 0
    }

# Test 3: Verify failure handling (missing profile)
$allResults += Test-Command -TestName "3) Verify failure handling (missing profile)" `
    -Arguments @('verify', '--profile', 'DefinitelyMissing', '--json') `
    -Assertions @{
        ValidJson = $true
        Success = $false
        ErrorNull = $false
        ErrorCodeExists = $true
    }

# Test 4: Apply failure handling (missing profile)
$allResults += Test-Command -TestName "4) Apply failure handling (missing profile)" `
    -Arguments @('apply', '--profile', 'DefinitelyMissing', '--json') `
    -Assertions @{
        ValidJson = $true
        Success = $false
        ErrorNull = $false
        ErrorCodeExists = $true
    }

# Final summary
Write-Host "`n" -NoNewline
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan
Write-Host "FINAL RESULTS" -ForegroundColor Cyan
Write-Host "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━" -ForegroundColor Cyan

$passed = ($allResults | Where-Object { $_.Passed }).Count
$total = $allResults.Count

Write-Host "`nTests Passed: $passed / $total" -ForegroundColor $(if ($passed -eq $total) { 'Green' } else { 'Red' })

if ($passed -eq $total) {
    Write-Host "`n✓ autosuite is GUI-grade" -ForegroundColor Green
    Write-Host "✓ GUI can be implemented with zero business logic" -ForegroundColor Green
    Write-Host "✓ Parsing STDOUT only is safe" -ForegroundColor Green
    Write-Host "✓ Error handling is deterministic" -ForegroundColor Green
    exit 0
} else {
    Write-Host "`n✗ Some tests failed - autosuite needs fixes" -ForegroundColor Red
    exit 1
}
