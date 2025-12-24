# Endstate — Vision

This document captures the intent, boundaries, and long-term direction of Endstate. It exists to prevent architectural drift and anchor decisions.

---

## Why This Exists

Endstate exists to eliminate the **clean install tax** — the repeated, error-prone, mentally draining work required to rebuild a machine after reinstalling an OS, switching hardware, or starting fresh.

This project is not about convenience scripts. It is about **trustworthy reconstruction of state**.

A machine should be:

- **Rebuildable** — from a single manifest
- **Auditable** — with clear records of what was applied
- **Deterministic** — same inputs produce same outcomes
- **Safe to re-run** — at any time, without side effects

Endstate treats machines as **systems with intent**, not piles of imperative steps.

---

## Core Idea

You declare **what should be true** about a machine.

Endstate:

1. Observes current state (capture)
2. Computes the delta (plan)
3. Applies only what is necessary (apply)
4. Verifies outcomes (verify)
5. Produces a report you can trust

Re-running the same plan should always converge to the same result.

---

## What Endstate Is

**Endstate is a declarative system provisioning and recovery tool.** It restores a machine to a known-good end state safely, repeatably, and without guesswork.

Core capabilities:

- **Capture** — snapshot current machine state into a manifest
- **Plan** — compute minimal actions needed to reach desired state
- **Apply** — execute changes safely with dry-run support
- **Verify** — confirm desired state is achieved
- **Drift detection** — identify divergence from declared state

All operations follow consistent principles:

- Declarative desired state
- Idempotent operations
- Non-destructive defaults
- Verification-first design

---

## What Endstate Is NOT

Explicit non-goals:

- **Not a one-shot bootstrap script** — it is designed for repeated, safe re-runs
- **Not a fragile dotfiles repo** — manifests are structured, versioned, and verifiable
- **Not a pile of ad-hoc scripts** — all automation follows consistent principles
- **Not an always-on agent** — it runs on demand, not continuously
- **Not enterprise endpoint management** — it targets personal and small-team machines
- **Not a replacement for OS installers** — it operates after the OS is installed
- **Not cross-platform today** — Windows-first in implementation, platform-agnostic in design

This project favors **clarity over cleverness** and **safety over speed**.

---

## Design Principles

These principles apply to all components in Endstate.

### 1. Idempotent by Default

Running the same operation twice must:

- Never duplicate work
- Never corrupt state
- Clearly log what was skipped and why

### 2. Declarative Desired State

Describe *what should be true*, not *how to do it*. The system decides how to reach the desired state.

### 3. Non-Destructive + Safe

- Backups before overwrite
- Explicit opt-in for destructive actions
- No silent deletion
- No implicit assumptions

### 4. Deterministic Output

Given the same inputs:

- Plans are reproducible
- Hashes are stable
- Reports are consistent

### 5. Separation of Concerns

Each stage must have clear boundaries:

- Capture ≠ planning ≠ execution ≠ verification
- No step assumes success from a previous step

### 6. Verification Is First-Class

"It ran" is not success. Success means the desired state is **observable**.

### 7. Auditable by Humans

Outputs must be:

- Readable
- Inspectable
- Reviewable before execution

Endstate optimizes for *confidence*, not opacity.

---

## Long-Term Direction

Over time, Endstate should be able to:

- Rebuild a machine from scratch using a single repo + manifest
- Detect drift between declared state and reality
- Apply changes safely and incrementally
- Produce machine-readable and human-readable reports
- Scale across operating systems without changing intent

**Not everything needs to be automated. But everything automated must be trustworthy.**

---

## Boundaries

To maintain focus and quality:

- **No feature creep** — new capabilities must align with core principles
- **No magic** — behavior must be predictable and inspectable
- **No silent failures** — errors are surfaced, not hidden
- **No enterprise scope** — this is a personal/small-team tool
- **No runtime dependencies** — PowerShell and standard OS tools only

If a feature compromises trust, determinism, or safety, it does not belong here.

---

## Guiding Philosophy

> Treat machines like living systems with memory and intent.

Endstate exists so that rebuilding a machine feels **boring, predictable, and safe** — instead of stressful and fragile.
