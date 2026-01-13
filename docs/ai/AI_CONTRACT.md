# AI Development Contract

This document is the **single source of truth** for AI collaborator behavior in this repository.

Tool-specific rule files (e.g., Windsurf, Cursor) must delegate to this contract. If a tool-specific rule conflicts with this contract, **this contract wins**.

---

## Behavior Specification System

**OpenSpec is the canonical system for behavior specifications in this repository.**

- Behavior changes MUST be represented in OpenSpec specs (`openspec/specs/`)
- Architecture truth lives in `docs/ai/PROJECT_SHADOW.md`
- Procedures live in runbooks (`docs/runbooks/`)

### Enforcement Levels

| Level | Gate | Description |
|-------|------|-------------|
| 1 | Advisory | `openspec validate` available but not enforced |
| 2 | Workflow | Pre-push hook blocks on validation failure |
| 3 | CI | CI pipeline fails on validation failure |

This repository enforces **Level 2** (workflow gate). See `docs/runbooks/OPENSPEC_ENFORCEMENT.md`.

### Repo-Local Enforcement

OpenSpec is installed as a devDependency and invoked via npm scripts. No global installs or npx required. Bypass is available via `OPENSPEC_BYPASS=1` for emergencies only.

---

## Authority & Context

### Project Shadow

If `docs/ai/PROJECT_SHADOW.md` exists:
- Treat it as authoritative architectural context
- Do not contradict it
- If it appears outdated or incomplete, propose a minimal update via PR — do not free-form architectural assumptions

If `docs/ai/PROJECT_SHADOW.md` does not exist and the task is architecture-sensitive:
- Generate it first before proceeding

If the Project Shadow and repository code appear to conflict, prefer the repository code and propose an update to reconcile the discrepancy.

### Decision Authority

- The human maintainer is the final decision-maker on architecture
- AI proposes; human disposes
- When intent is unclear, ask — do not assume

---

## Scope Discipline

- Make the **smallest change** that satisfies acceptance criteria
- No unrelated refactors
- No formatting sweeps
- No dependency bumps unless explicitly requested
- No opportunistic cleanups
- Stop once acceptance criteria and required verification are met

---

## Contract & Change Safety

- **Preserve public APIs** and integration contracts unless explicitly changing them
- Prefer **contract-first edits**: schema/contract → implementation → tests
- Do not weaken security, authentication, or validation boundaries
- Do not remove error handling or defensive code without explicit instruction
- Do not collapse multi-step workflows into monolithic changes

---

## Verification Rules

- Run only the **minimum targeted verification** needed to confirm the change
- Do not run full test suites or full coverage unless explicitly requested
- If verification requires secrets, credentials, or external systems:
  - Do not guess or fabricate values
  - Ask for guidance or skip with explicit acknowledgment
- Provide copy-pastable verification commands when you cannot run them

---

## File-Write & Tool Restrictions

- Treat inability to write to files as a bug to work around
- Use a reliable fallback method (e.g., PowerShell `Set-Content` with leaf-path guard)
- **Never claim changes are applied** unless file contents are actually written and confirmed
- Do not create files outside the project directory without explicit permission

---

## Output Quality

- Prefer **concise, high-signal output**
- Avoid speculation and roadmap content
- Use patch-style language for Shadow updates
- Do not restate unchanged context
- Do not pad responses with filler or hedging

---

## When to Update Project Shadow

Propose a Project Shadow update when changes affect any of the following:

| Category | Examples |
|----------|----------|
| Core invariants | Rules that must never be violated |
| Architecture or subsystem boundaries | New modules, removed components, restructured directories |
| Contracts or public APIs | Interface changes, new integration points |
| Authority or ownership model | Changed review process, new decision-makers |
| Landmines or sharp edges | Newly discovered non-obvious failure modes |
| Explicit non-goals | Scope boundaries added or removed |
| Testing philosophy | Strategy changes (not individual test additions) |
| Development workflow assumptions | Build process, environment requirements |

Do **not** update Project Shadow for:
- Bug fixes within existing architecture
- Documentation updates
- Dependency version bumps
- Test additions that follow existing strategy
- Performance optimizations that preserve contracts

---

## Compliance

AI collaborators operating in this repository must:

1. Read and follow this contract
2. Respect Project Shadow authority when present
3. Represent behavior changes in OpenSpec specs
4. Propose Project Shadow updates for architectural changes
5. Stop when acceptance criteria are met
6. Ask when uncertain rather than assume
