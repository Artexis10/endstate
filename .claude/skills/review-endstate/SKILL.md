---
name: review-endstate
description: Use when the user asks to review changes, review a diff, sanity-check a PR before merge, or verify uncommitted work against Endstate's locked contracts and principles. Trigger phrases include "review my changes", "review the diff", "sanity check this branch", "check against contracts", "review before PR". Repo-scoped to the Endstate engine.
---

# Review Endstate Changes

A structured review of a diff against Endstate's locked contracts (`docs/contracts/`), public commitments (`PRINCIPLES.md`), and Go-quality posture. Output is a four-section verdict the user can act on before merge.

## Step 1 — Determine diff scope (ask if ambiguous)

Before reading anything, confirm what to review. If the user did not specify, ask:

> Which diff should I review?
> 1. Current uncommitted changes (`git diff` + `git diff --cached` + untracked)
> 2. A branch against `main` (`git diff main...<branch>`)
> 3. A specific GitHub PR (`gh pr diff <num>`)

Do not guess. Reviewing the wrong scope wastes the user's time and produces noise.

Once scope is known, capture the changed file list — every check below is conditional on what the diff actually touches.

## Step 2 — Always-read inputs

Read these every review, regardless of diff:

- `PRINCIPLES.md` — the seven public commitments. Pay special attention to:
  - **Principle 1** (the local product is free, forever — no nag screens, no profile/feature limits, no local features gated on subscription status, no telemetry-by-default)
  - **Principle 4** (hosted data is encrypted end-to-end with client-side keys — passphrase and `masterKey` never leave the device; server cannot decrypt)
- `CLAUDE.md` — landmines and protected areas, so the review flags forbidden patterns.

## Step 3 — Conditional contract reads

Read each contract only if the diff touches its domain. Cite the specific section number when flagging a violation.

| Diff touches | Read |
|---|---|
| `go-engine/internal/backup/**`, hosted backup auth/crypto, JWT/JWKS, KDF, encryption envelope, R2 storage layout, subscription state | `docs/contracts/hosted-backup-contract.md` |
| Any `--json` envelope, error shapes, error codes, `cliVersion`/`schemaVersion`, `endstate capabilities` | `docs/contracts/cli-json-contract.md` |
| Anything emitted to stderr as JSONL, phase/item/summary/error/artifact events, `--events jsonl` | `docs/contracts/event-contract.md` |
| Capabilities handshake, GUI ↔ engine boundary, anything the GUI reads | `docs/contracts/gui-integration-contract.md` |
| Capture artifact layout, profile composition, restore safety, config portability | `docs/contracts/capture-artifact-contract.md`, `profile-contract.md`, `restore-safety-contract.md`, `config-portability-contract.md` |

If the diff modifies behavior covered by a spec under `openspec/specs/`, confirm the same commit (or branch) carries a corresponding `openspec/changes/<id>/` proposal.

## Step 4 — Project-specific checklist

Run each check against the diff. Skip checks that don't apply (e.g., no new HTTP routes → skip ownership check); never skip because "it probably looks fine."

- [ ] **API version header.** Any new substrate API call site verifies that the response carries `X-Endstate-API-Version: 1.0` (or compatible major). Refuses to write on major mismatch; warns on minor mismatch for read paths. (`hosted-backup-contract.md` §11)
- [ ] **Ownership + 404, not 403.** Any new authenticated route enforces `userId` from JWT == `userId` on the row, AND returns 404 (not 403) on cross-user access to avoid existence leaks. (`hosted-backup-contract.md` §7 "Ownership enforcement")
- [ ] **Crypto hygiene.** Any new crypto code uses `crypto/rand` exclusively (no `math/rand`). AES-GCM nonces are freshly generated per call from CSPRNG. Auth tags are verified before any plaintext is returned to the caller. AAD binds chunk index where chunks are involved. (`hosted-backup-contract.md` §3)
- [ ] **Error envelope.** Any new error path returns the standard envelope (`success: false`, `error.code` SCREAMING_SNAKE_CASE, `error.message`, optional `detail`/`remediation`/`docsKey`). No bare strings, no ad-hoc shapes. (`cli-json-contract.md` "Error Object")
- [ ] **Event ordering.** Any new streaming output respects ordering invariants — first event is `phase`, monotonic phase transitions, last event is `summary`, schema v1 required fields (`version`, `runId`, `timestamp`, `event`). (`event-contract.md` §"Event Fundamentals" and §"Schema v1")
- [ ] **Stdout vs stderr split.** Any new command emits the JSON envelope on **stdout** and streaming events on **stderr** (`[Console]::Error.WriteLine()` equivalent in Go). Never mix.
- [ ] **OpenSpec coupling.** If the change modifies behavior covered by an existing spec, the same commit references an `openspec/changes/<id>/` proposal. New behavior without a spec change is a missing artifact.
- [ ] **Tests for new behavior.** New behavior has new tests under the matching `*_test.go` (e.g., changes in `internal/commands/restore.go` add cases to `internal/commands/restore_test.go`). Tests are hermetic — no real winget calls, no network, no shared state. Match the table-driven, single-purpose pattern in existing `_test.go` files.
- [ ] **JSONC parsing.** Any new code reading a `.jsonc` file uses `manifest.StripJsoncComments` before unmarshalling. Raw `json.Unmarshal` on `.jsonc` is forbidden. (`CLAUDE.md` Landmine #1)
- [ ] **UX language (only if user-facing strings change).** New error messages, status reasons, or progress strings line up with `docs/ux-language.md` (lives in the GUI repo `endstate-gui`). If unavailable to read, flag it as "verify against GUI repo before merge" rather than silently passing.

## Step 5 — Go-quality checklist

- [ ] **Sensitive buffers zeroed.** Passphrase bytes, derived `masterKey`, DEK, and any plaintext key material are zeroed (`for i := range b { b[i] = 0 }`) immediately after use. Don't rely on GC.
- [ ] **Errors carry context.** Errors are wrapped with `fmt.Errorf("doing X: %w", err)` not returned bare. Caller-visible errors at command boundaries map to a stable `error.code`.
- [ ] **Deferred cleanup.** Every `Open`/`Create`/`NewWriter`/transaction has a matching `defer Close()` (or equivalent), placed immediately after the success check.
- [ ] **Dependency justification.** Any new entry in `go-engine/go.mod` is justified — the project's posture is minimal-dependency and audit-friendly. Prefer stdlib; if a third-party module is added, the review notes the reason. Crypto deps in particular need to be widely-used and well-audited (e.g., `golang.org/x/crypto` over a one-author module).

## Step 6 — Output

Produce exactly four sections, in this order. Every item names the file and (where useful) the line number, and cites the contract section when applicable.

```
### Blocking issues
Items that violate a locked contract, principle, or invariant. Must be fixed before merge.

### Should fix
Items that are likely defects or near-future maintenance debt. Catch before merge if possible.

### Worth considering
Subjective design or style notes. Take or leave.

### Off-contract behavior
Anywhere the diff disagrees with a contract — name the contract and the section.
Even if the user accepts the divergence, the contract must be amended in the same change.
```

If a section is empty, write `_None._` rather than omitting it. Empty sections signal that the check was actually performed.

## Boundary rules

- **Don't review with insufficient information.** If the diff is too large to load fully, or scope is ambiguous, ask first. A partial review that reads as complete is worse than no review.
- **Don't fabricate.** Don't claim a contract section exists unless verified by reading. Don't claim a file is missing without confirming via `Read` or `Glob`. Quote the contract when flagging a violation.
- **Don't restate the diff.** The user can see the diff. The review is the analysis on top.
- **Don't expand scope.** Do not flag pre-existing issues outside the diff. If you see one and it's important, mention it in `Worth considering` clearly labeled as pre-existing.

## Reference patterns

- Existing review-flavored automation: `.claude/agents/contract-guard.md` (an agent with overlapping scope — this skill is the lighter, on-demand version)
- Test patterns to match: `go-engine/internal/commands/restore_test.go`, `capture_test.go`, `report_test.go` — table-driven, hermetic, one assertion per test
- OpenSpec change layout: `openspec/changes/add-hosted-backup-contract/` is a current example
