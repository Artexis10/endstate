# Endstate Roadmap

This document captures the forward roadmap for Endstate with a focus on
correctness, contract stability, and UX safety.  
Items are ordered by **impact × risk reduction**, not convenience.

---

## 1. Event Stream Completeness & Schema Stability (NEXT)

**Goal:**  
Guarantee that NDJSON event streams are **complete, deterministic, and stable**
across `capture`, `apply`, and `verify`, with a schema the GUI can rely on long-term.

This is foundational. Everything else (GUI, automation, future integrations)
depends on this being rock solid.

### Scope

- Ensure **all lifecycle phases emit events**:
  - `capture`
  - `apply`
  - `verify`

- Ensure **minimum guaranteed events per command**:
  - Phase start event
  - Item events (where applicable)
  - Summary event (always last)

- Ensure **consistent semantics across commands**:
  - `status`, `reason`, `message` meanings are aligned
  - Dry-run vs real execution differences are explicit

### Schema Contract (v1)

Each NDJSON line must:
- Be valid JSON
- Be emitted on **process stderr**
- Contain:
  - `version` (integer, currently `1`)
  - `event` (string enum)
  - `timestamp` (ISO-8601 UTC)
- Be single-line (no wrapping, no prefixes)

#### Event Types

- `phase`
- `item`
- `artifact`
- `summary`

#### Non-Goals (for now)

- No breaking schema changes
- No renaming fields
- No compression / batching
- No stdout event emission

### Tests (Required)

- Contract tests for **each command**:
  - capture / apply / verify
- Tests must:
  - Run via the **native shim** (`endstate.cmd`)
  - Use real PowerShell redirection (`1>` / `2>`)
  - Assert:
    - stderr contains NDJSON
    - stdout contains no NDJSON
    - last event is always `summary`
- Tests must fail on:
  - Missing events
  - Invalid JSON
  - Out-of-order summary

### Acceptance Criteria

- GUI can rely on:
  - Always seeing a `phase` event first
  - Always seeing a `summary` event last
- No command emits partial or silent streams
- Schema documented and versioned

---

## 2. GUI Integration Hardening (NEXT AFTER EVENTS)

**Goal:**  
Make the GUI resilient to real-world stderr streams without coupling it to
engine internals.

The GUI must be **defensive**, not optimistic.

### Scope

- NDJSON parser must:
  - Tolerate:
    - Partial lines
    - Interleaved non-JSON stderr noise
    - Unknown fields
  - Ignore invalid lines safely

- GUI must:
  - Key off `version` and `event`
  - Never crash on schema extensions
  - Handle missing optional fields gracefully

- Status resolution:
  - Centralized mapping from event → UI state
  - No ad-hoc logic per screen

### Non-Goals (for now)

- No live progress estimation
- No retries / recovery logic
- No event persistence beyond runtime

### Tests

- GUI unit tests with:
  - Mixed stderr input (JSON + noise)
  - Out-of-order item events
  - Unknown future fields
- Integration tests replaying captured NDJSON logs

### Acceptance Criteria

- GUI never crashes on malformed or unexpected stderr
- UI degrades gracefully when events are missing
- Adding a new event field does not require GUI changes

---

## 3. Apply / Verify Correctness Polish

**Goal:**  
Reduce noise and ambiguity in apply/verify output so users trust results.

### Examples

- Reduce false “extra” counts in verify
- Make dry-run output deterministic
- Align human output with event semantics

---

## 4. Bootstrap & Installation Robustness

**Goal:**  
Make installation self-healing and diagnosable.

### Ideas

- Detect broken installs and repair automatically
- Clear diagnostics for missing engine scripts
- Version mismatch warnings

---

## 5. Native `.exe` Wrapper (Later)

**Status:** Deferred intentionally.

**Rationale:**
- Current `.cmd` shim is correct and contract-tested
- `.exe` adds build, signing, and distribution complexity
- No functional blocker today

### Future Goals

- Single binary entrypoint
- Faster startup
- Cleaner Windows UX
- No dependency on `.cmd` or PowerShell resolution rules

### Acceptance Criteria (Future)

- `Get-Command endstate` resolves to `.exe`
- stderr/stdout behavior identical to current shim
- Existing scripts continue to work unchanged

---

## 6. Linux / macOS Platform Arc — COMPLETE (2026-06)

**Status:** Shipped and closed out. Endstate provisions Linux and macOS through the Nix realizer
(packages + home-manager configuration: catalog, capture, verify, rollback, boundary secrets) with a
per-app Homebrew driver lane on macOS (formulae + Casks, two-lane apply, best-effort rollback, Cask
auto-routing) and a consent-gated engine-installed backend bootstrap (Nix / Homebrew, official
installers only). CI covers hermetic tests on windows/macos/ubuntu plus real-Nix (macOS + Linux) and
real-Homebrew (macOS) integration smokes. Specs: `nix-package-backend`,
`platform-backend-selection`, `nix-home-manager-*`, `macos-brew-apply-wiring`,
`macos-brew-best-effort-rollback`, `engine-backend-bootstrap`. See `docs/COMPATIBILITY.md` for the
platform support matrix.

### Deferred scope (consciously not shipped — the durable record)

- **Managed secrets backend** (agenix, later sops-nix on demonstrated need): the typed managed tier
  beyond boundary + env `*_FILE` — ciphertext capture handling (path-only vs bundled safe-at-rest
  ciphertext), key-bootstrap UX (fail-fast warn on a missing user-owned identity), and the macOS
  age-identity-path default. The boundary model ships today; capture redaction is specced in
  `nix-home-manager-secrets-boundary`.
- **Assisted backend uninstall**: the engine never silently uninstalls a backend it installed;
  whether it ever *assists* (pointing at the official uninstaller) versus staying entirely hands-off
  remains an open decision.
- **Interactive CLI stdin consent prompt** for the backend bootstrap: today consent is flag-driven
  (`--bootstrap-backends` / `--no-bootstrap`) plus a streamed consent-request event for the GUI (the
  primary audience); a TTY prompt was deliberately not built.
- **Graceful read-only lane skip**: read-only commands (verify/plan/capture) never install a backend
  and surface `REALIZER_UNAVAILABLE` when Nix is absent; downgrading that to a graceful per-lane skip
  is a possible follow-up, not a regression.
- **Brew version strictness**: brew pins stay advisory (verify reports drift; apply never
  downgrades/reinstalls to chase a pin). Precise pinning was rejected given Homebrew's weak
  version-selection model.
- **Linuxbrew**: explicit non-goal — brew is the macOS driver only.
- **Real install-path CI**: the GH macOS runner has Homebrew (and CI-Nix) preinstalled, so the
  bootstrap *install* path (absent → consent → install) is only exercisable on a clean real machine,
  not in the existing smokes.
- **Real-machine validation queue** (composes with the real-Mac E2E pass): real macOS Keychain
  round-trip (`backup login` → `backup push` with an unlocked login keychain) and real Secret
  Service round-trip on a desktop Linux session (unlocked GNOME Keyring / KWallet) — the Unix
  keychain backends (#129) are CI-verified hermetically only; plus a darwin release-binary check
  once the first multi-platform release (#130) is cut.

---

## Guiding Principles

- Contracts > convenience
- Native behavior over PowerShell quirks
- CLI is an API — treat it as such
- Tests must reproduce real user commands
