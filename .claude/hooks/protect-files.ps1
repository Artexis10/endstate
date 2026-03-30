# Blocks edits to immutable files
# Used as a Claude Code PreToolUse hook
$f = $env:CLAUDE_FILE_PATH
if (-not $f) { exit 0 }
$f = $f -replace '\\', '/'
$protected = @(
    '(^|/)LICENSE$'
    '(^|/)NOTICE$'
)
foreach ($pattern in $protected) {
    if ($f -match $pattern) {
        Write-Error "BLOCKED: $f is a protected file. Requires explicit user instruction to modify."
        exit 1
    }
}
