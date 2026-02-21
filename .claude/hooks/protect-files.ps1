# Blocks edits to protected files per docs/ai/PROJECT_RULES.md
# Used as a Claude Code PreToolUse hook

$f = $env:CLAUDE_FILE_PATH
if (-not $f) { exit 0 }

$f = $f -replace '\\', '/'

$protected = @(
    '(^|/)bin/endstate\.ps1$'
    '(^|/)docs/contracts/.*\.md$'
    '(^|/)\.github/workflows/'
    '(^|/)docs/ai/AI_CONTRACT\.md$'
    '(^|/)docs/ai/PROJECT_SHADOW\.md$'
    '(^|/)LICENSE$'
    '(^|/)NOTICE$'
)

foreach ($pattern in $protected) {
    if ($f -match $pattern) {
        Write-Error "BLOCKED: $f is a protected file (see docs/ai/PROJECT_RULES.md). Requires explicit user instruction to modify."
        exit 1
    }
}
