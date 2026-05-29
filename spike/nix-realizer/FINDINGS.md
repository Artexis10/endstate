# Nix realizer spike — FINDINGS

> **Status: COMPLETE.** Run against real Determinate Nix 3.21.0 in WSL2 Ubuntu,
> 3× per case + a forced-collision probe. This doc feeds the `## Decisions` +
> `## Risks` of the future `nix-realizer-backend` OpenSpec change's `design.md`.
> Throwaway; does not merge into the engine.

## Goal (the one question)

Is Nix's steady-state error surface **bounded and translatable** — each failure
class deterministically mapped to one stable engine `error.code` using **only**
`(exit code, internal-json action/level/activity-type, generation-advanced?)`,
**without** depending on the unstable human `msg` text — when the user interacts
only with Endstate? Verdict gates the "Realizer on `nix profile`" model.

## VERDICT: 🟡 YELLOW — proceed, with a documented anchor-map risk

The realizer model is **viable**. Its core mechanic (atomic generation switch)
is **confirmed**. But the failure *class* is **not** classifiable from structural
signals alone — the two operator-action classes (daemon, permission) require
stable **message-text anchors**, so this is YELLOW, not GREEN. No class is truly
untranslatable, so it is not RED.

### Why not GREEN
`#3 daemon` and `#6 permission` — which the rubric requires to be structurally
separable — are **not**. Daemon failures start no activity (structurally
indistinguishable from a cached eval error); permission failures surface at the
*final* commit step *after* a successful build, so structurally they look
identical to a build/store failure (the classifier actively *mis*labels them
`INSTALL_FAILED`). Both need msg anchors.

### Why not RED
Atomicity holds (below); every class we could provoke **was** translatable via a
(corrected) anchor; unrecognised errors degrade gracefully to
`INSTALL_FAILED` + raw Nix text in `error.detail` rather than dumping the user
into Nix. The moat mostly holds; the residual risk is *slightly-wrong
remediation*, not *raw Nix-isms everywhere*.

## Environment

| Item | Value |
|------|-------|
| Nix | **Determinate Nix 3.21.0** (nix 2.34.6) |
| Installer | Determinate Systems (systemd-managed daemon) |
| Store type | multi-user (nix-daemon; build users 30001-30032) |
| `experimental-features` | nix-command flakes (forced by harness) |
| `nix profile list --json` | **version=3**, `elements` = **name-keyed object** |
| nixpkgs pin | unpinned (`nixpkgs` registry → latest) — **pin for production** |
| ⚠️ CLI surface | `nix profile install` is a **deprecated alias for `nix profile add`** in 3.21 — the realizer should call `add` |

## Atomicity — ✅ CONFIRMED (the model's load-bearing premise)

| Probe | Result | Generation |
|-------|--------|-----------|
| `#0` install `hello` (success) | exit 0 | 0 → **1** |
| `#7` install `hello` + nonexistent (partial) | exit 1 | 0 → **0** (valid pkg NOT partially applied) |
| `#6` build OK, fail at commit (permission) | exit 1 | 0 → **0** (prior gen intact) |

**A generation advances only on full success; any failure — including a
mixed valid/invalid batch and a commit-time failure — leaves the previous
generation untouched.** This validates `apply = atomic switch` and
`restore = switch generation`. No need to move to a generated-flake *for
atomicity reasons* (flakes remain a separate call for reproducibility/pinning).

## Failure taxonomy — observed (Determinate Nix 3.21.0, 3× each)

| # | Class | Exit | Structural signal | Gen | Structural class | Stable anchor (corrected from real output) | Score |
|---|-------|------|-------------------|-----|------------------|--------------------------------------------|-------|
| 0 | happy `hello` | 0 | Realise+Build+Substitute+Copy | 0→1 | OK | n/a | **BOUNDED** |
| 1 | eval / bad attr | 1 | **no activity** (nixpkgs cached) | 0→0 | UNKNOWN | `does not provide attribute` | **PARTIAL** |
| 2 | network / bad flake host | 1 | FileTransfer, no build | 0→0 | INSTALL_FAILED ✓ | `unable to download` / `HTTP error` / `while fetching` | **BOUNDED** |
| 3 | daemon unavailable | 1 | **no activity** | 0→0 | UNKNOWN | `opening a connection to remote store` (+ warn `cannot connect to socket`) | **PARTIAL** |
| 4 | store / disk-full | — | invasive, not run | — | — | (`no space left` — untested) | UNSCORED |
| 5 | collision (forced =priority) | 1 | Realise+Build, no gen advance | 0→0 | INSTALL_FAILED ✓ | `an existing package already provides the following file` / `conflicting packages have a priority` | **BOUNDED**¹ |
| 6 | permission (ro profile dir) | 1 | Build+Substitute then commit fail | 0→0 | INSTALL_FAILED ✗ (mislabel) | `Permission denied` | **PARTIAL** |
| 7 | atomic partial mix | 1 | no activity | 0→0 | UNKNOWN | `does not provide attribute` | **PARTIAL** |

¹ `#5` only collides when packages share a path **at equal priority**; with
default priorities Determinate Nix **auto-resolves** (exit 0, no error) — so
real-world collisions are rare. When forced, it's structurally INSTALL_FAILED
(build ran, gen didn't advance) → BOUNDED at the top-level code.

**Score: BOUNDED ×3 (happy, network, collision-forced), PARTIAL ×4 (eval,
daemon, permission, atomic), UNSCORED ×1 (disk).** Top-level engine codes are
all reachable; the daemon + permission classes are the ones gating GREEN, and
both need anchors.

## Key finding: anchors are real but **version-specific and must be tested**

My *reasoned* collision anchors (`collision between`, `files in conflict`,
`conflict between`) **all failed** — Determinate Nix 3.21 actually says
`an existing package already provides the following file` /
`The conflicting packages have a priority of N`. This is the YELLOW risk made
concrete: **anchors must be derived from real output of the pinned Nix and
locked behind a contract test, never reasoned.** Reasoning got 0/3 collision
anchors right.

Secondary: structural signals are NOT useless — `(activity type, gen-advanced)`
reliably tells you the **pipeline stage** (fetch / build / commit) and is ideal
for the progress UI and for an `error.detail.subcode`; it just can't carry the
top-level class for daemon/permission.

## Proposed Nix → engine `error.code` map (the deliverable)

| Failure class | Primary signal | Engine `error.code` | New? |
|---------------|----------------|---------------------|------|
| eval / bad attr | anchor `does not provide attribute` | `INSTALL_FAILED` (detail.subcode `eval`) | no |
| network / fetch | structural FileTransfer-no-build **or** anchor `unable to download`/`HTTP error` | `INSTALL_FAILED` (subcode `network`) | no |
| daemon / store unavailable | anchor `opening a connection to remote store` | `REALIZER_UNAVAILABLE` | **yes (additive)** |
| store / disk | anchor `no space left` (untested) | `INSTALL_FAILED` (subcode `store`) / `INTERNAL_ERROR` | no |
| collision | structural build-no-gen-advance **or** anchor `already provides the following file` | `INSTALL_FAILED` (subcode `collision`) | no |
| permission | anchor `Permission denied`/`read-only file system` | `PERMISSION_DENIED` | no |
| _unrecognised_ | fallback | `INSTALL_FAILED` + raw Nix text in `error.detail` | no |

## What this means for `nix-realizer-backend` (design.md inputs)

1. **Classifier = anchors (top-level class) + structural (pipeline stage / subcode).**
2. **Pin the Determinate Nix version**; derive anchors from its real output and
   lock them behind a **contract test** that re-runs the provocations and asserts
   the anchor→code mapping. Re-validate on every Nix bump (the spike harness is
   the seed for this test).
3. **Graceful fallback:** unrecognised → `INSTALL_FAILED` + raw message in
   `error.detail`, never an unhandled crash. Moat is preserved.
4. **Use `nix profile add`** (not the deprecated `install` alias) and parse
   `nix profile list --json` **version=3 name-keyed** shape (handle older array
   shape defensively).
5. **Atomicity confirmed** → keep `apply = generation switch`,
   `restore = nix profile rollback`/generation switch. The `nix profile` vs
   generated-flake decision is now about *reproducibility/pinning*, not atomicity.
6. The throwaway harness (`main.go`) is reusable as the seed for the production
   contract test and the realizer's `classify()`.
