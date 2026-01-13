---
trigger: always_on
---

# Windsurf Project Ruleset — Endstate

This file is a **thin Windsurf adapter**. It delegates all substantive policy to the AI governance documents.

---

## Authority Hierarchy

Follow these documents in order of precedence:

1. **`docs/ai/AI_CONTRACT.md`** — global AI behavior contract (highest authority)
2. **`docs/ai/PROJECT_SHADOW.md`** — architectural truth, invariants, landmines, non-goals
3. **`docs/ai/PROJECT_RULES.md`** — repo-specific operational policy

If any instruction in this file conflicts with the above, the governance documents win.

If code conflicts with PROJECT_SHADOW.md, code wins and a PROJECT_SHADOW.md update should be proposed.

---

## Editing Guidance

| To change... | Edit... |
|--------------|---------|
| AI behavior rules | `docs/ai/AI_CONTRACT.md` |
| Architecture, invariants, landmines | `docs/ai/PROJECT_SHADOW.md` |
| Operational policy (env, testing, protected areas) | `docs/ai/PROJECT_RULES.md` |
| Windsurf-specific enforcement | This file |

---

## Windsurf-Specific Enforcement

### File Write Fallback

If standard file write tools fail, use PowerShell:

```powershell
$Path = "<target-file>"
if (!(Test-Path -LiteralPath $Path -PathType Leaf)) {
  throw "Expected leaf file not found: $Path"
}
$content = Get-Content -LiteralPath $Path -Raw -Encoding UTF8
# Modify $content
Set-Content -LiteralPath $Path -Value $content -Encoding UTF8 -NoNewline
```

Treat inability to write files as a bug to work around. Never claim changes are applied unless confirmed.

### Verification Before Done

Before marking any task complete:
- Verify changes are actually written to disk
- Run minimum targeted verification (not full test suites unless requested)
- Provide copy-pastable commands when verification cannot be run

### Git Hook Enforcement

- **Never** use `--no-verify` to bypass git hooks
- Commit messages must be meaningful
- Runtime artifacts (`logs/`, `plans/`, `state/`) must never be committed

---

## Quick Reference

### Test Command
```powershell
.\scripts\test-unit.ps1
```

### Key Directories
- `engine/` — core orchestration logic
- `drivers/` — package manager adapters
- `modules/apps/` — config module catalog
- `docs/contracts/` — integration contracts

### Protected Files (require explicit instruction)
- `bin/endstate.ps1` — CLI entrypoint
- `docs/contracts/*.md` — integration contracts
- `docs/ai/*.md` — AI governance documents