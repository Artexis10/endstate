$modules = @('windsurf','cursor','vscodium','claude-desktop','foobar2000','docker-desktop','notepad-plus-plus','vscode','claude-code')
$repoRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)

foreach ($m in $modules) {
    $file = Join-Path $repoRoot "modules\apps\$m\module.jsonc"
    $raw = Get-Content $file -Raw
    $lines = $raw -split "`n"
    $clean = @()
    foreach ($line in $lines) {
        $inStr = $false; $esc = $false; $cStart = -1
        for ($i = 0; $i -lt $line.Length; $i++) {
            $c = $line[$i]
            if ($esc) { $esc = $false; continue }
            if ($c -eq '\') { $esc = $true; continue }
            if ($c -eq '"') { $inStr = !$inStr; continue }
            if (!$inStr -and $c -eq '/' -and ($i+1) -lt $line.Length -and $line[$i+1] -eq '/') { $cStart = $i; break }
        }
        if ($cStart -ge 0) { $clean += $line.Substring(0, $cStart) } else { $clean += $line }
    }
    $json = ($clean -join "`n") -replace ',(\s*[}\]])', '$1'
    try {
        $null = $json | ConvertFrom-Json -ErrorAction Stop
        Write-Host "OK: $m" -ForegroundColor Green
    } catch {
        Write-Host "FAIL: $m - $_" -ForegroundColor Red
    }
}
