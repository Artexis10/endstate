# OpenSpec Enforcement Runbook

This runbook covers OpenSpec workflow for the Endstate repository.

---

## Enforcement Level

This repository enforces **Level 2** (workflow gate):
- Pre-push hook validates all specs
- Push is blocked on validation failure
- CI validation is advisory (Level 3 not yet enabled)

---

## Quick Reference

### Validate Specs
```powershell
npm run openspec:validate
```

### List All Specs
```powershell
npm run openspec:list
npm run openspec:list:specs
```

### Emergency Bypass
```powershell
$env:OPENSPEC_BYPASS = "1"
git push
```

Use bypass sparingly. Document reason in commit message.

---

## Adding a New Spec

1. Create spec file in `openspec/specs/<category>/<name>.md`
2. Run `npm run openspec:validate` to verify
3. Commit spec with implementation

---

## Validation Failures

If validation fails:
1. Read the error message carefully
2. Fix the spec or implementation
3. Re-run `npm run openspec:validate`
4. Push when green

Common issues:
- **Missing spec**: Behavior change without corresponding spec
- **Spec drift**: Implementation doesn't match spec
- **Syntax error**: Malformed spec file

---

## References

- [AI_CONTRACT.md](../ai/AI_CONTRACT.md) — enforcement levels definition
- [PROJECT_RULES.md](../ai/PROJECT_RULES.md) — OpenSpec scripts reference
