## Context

The design-only `engine-backend-bootstrap` change scoped this capability and left six decisions to
the human. Those are now ratified:

1. **Scope = maximal**: shared contract + brew instance + Nix instance + the brew-default routing
   flip, delivered as one arc across three stacked PRs.
2. **Combined consent**: the engine aggregates the backends a run needs and emits **one**
   consent-request; on the single yes it installs+verifies each backend independently.
3. **Non-interactive default = skip-the-lane-with-a-clear-message** + emit the consent-request event;
   `--bootstrap-backends` opts in, `--no-bootstrap` forces skip. Never installs without an explicit
   yes; never hard-crashes on no.
4. **Never silently uninstall** a backend the engine installed; removal is a separate user-owned
   action — at most point at the official uninstaller.
5. **Nix flavor = Determinate multi-user** installer (matches CI, flake-enabled).
6. **Disclosure = plain-language + inspectable details**: the consent names no product but plainly
   states it will set up installers, that the OS will ask for a password, and that a background helper
   and a dedicated storage area may be created — with an inspectable details field carrying the exact
   commands.

## Goals / Non-Goals

**Goals (this increment, PR 1):**

- One backend-agnostic contract — *detect → consent → official install → verify → proceed-or-decline*
  — implemented behind injectable seams, hermetically testable with no real installer.
- Wire the **Homebrew** instance into the apply brew lane: present → no-op; absent+consented →
  install → verify → use; declined/failed → the existing visible-skip path; never silent.
- The flag + streamed-consent-event contract (`--bootstrap-backends` / `--no-bootstrap`; combined
  consent-request event; install path is apply-only).

**Non-Goals (this increment):**

- The **Nix** command-lane wiring + the declined-Nix-but-consented-brew restructuring (PR 2).
- The **brew-default-for-apps** routing flip (PR 3).
- Any interactive CLI stdin prompt — the GUI event path is the primary audience; consent arrives as a
  flag (the GUI re-invokes with `--bootstrap-backends`).
- An assisted backend **uninstall** flow — never silently uninstall; at most point at the official
  uninstaller.

## 1. The contract — five states, backend-agnostic

| State | Brew | Nix | Behavior |
|---|---|---|---|
| **Detect** | `brew` on PATH / known prefix | `nix` on PATH / Determinate default | present → no-op, no prompt |
| **Consent** | combined plain-language ask | combined plain-language ask | **one** request; concept hidden, consent not |
| **Install** | upstream `install.sh` | Determinate installer | the **official** installer, orchestrated, inspectable |
| **Verify** | `brew --version` | `nix --version` + trivial eval | must pass **before** any package work |
| **Decline / fail** | visible-skip the brew lane | (PR 2) skip the realizer lane | clear message; the rest of the run continues |

Load-bearing rules: present → silent no-op; absent → exactly one combined consent then the official
installer, never silent; verify gates use; decline/failure is graceful, never a half-done apply.

## 2. Package shape (`internal/bootstrap`)

```go
type Backend string
const ( BackendBrew Backend = "brew"; BackendNix Backend = "nix" )

type Bootstrapper struct {
    GOOS    string
    Detect  func(b Backend) (present bool, err error) // LookPath / known prefix
    Install func(b Backend) error                     // shells the OFFICIAL installer
    Verify  func(b Backend) (ok bool, err error)      // <backend> --version (+ nix eval)
}

func (bs *Bootstrapper) Probe(needed []Backend)   (absent, present []Backend)
func (bs *Bootstrapper) Provision(absent []Backend) map[Backend]Outcome // Installed|InstallFailed|VerifyFailed
```

Two-phase to support **combined** consent: `Probe` detects the whole needed set first (so the engine
can emit one consent-request for all absent backends), then `Provision` installs+verifies each only
after consent is granted. The real `Install` strategies are the only place a privileged installer is
shelled, and they are never invoked under `go test` (tests inject fakes) — mirroring the winget
real-output lesson: the install path is validated on a real machine, not reasoned about in CI.

## 3. Where it runs (the pre-step seam)

`apply`/`capture`/`verify`/`plan` resolve backends at the factory gate
(`newRealizerFn`/`newBrewDriverFn`, defined in `commands/verify.go`). The bootstrap is a **pre-step in
front of that gate**, behind a new injectable seam `bootstrapBackendsFn` (default no-op fake in the
commands `TestMain`, so every existing test stays byte-identical). In PR 1 only the **brew** lane is
gated through it: when the run has brew apps, `ensureBackendsForRun([BackendBrew], mutating, …)` runs
*detect → (consent → install → verify)*; only an `available` brew backend resolves `newBrewDriverFn()`.
Absent+declined leaves `brewDrv` nil → the existing visible-skip path (`apply_brew.go`).

The **install path is apply-only**. `mutating=false` (plan/verify/capture) means detect → use-or-skip
with a clear message, no install and no consent prompt — you do not install a package manager just to
preview or read state.

## 4. Consent surface

The **engine owns the gate** (never install without consent) and **emits a streamed consent-request
event** carrying the combined absent-backend set, a plain-language message, and an inspectable
`details` field (the exact installer commands). It accepts the human's answer as
`--bootstrap-backends` / `--no-bootstrap`. The **GUI renders** the event and re-invokes with the flag
(CLI is source of truth). No interactive stdin prompt is implemented in this increment.

## 5. The CI-test wrinkle (recorded, not solved here)

The GH `macos-latest` runner has Homebrew and (via the Determinate action) Nix preinstalled, so the
path-filtered macOS smoke exercises only **detect → present → no-op**. The *absent → consent →
install → verify* path is validated on a **clean real machine / throwaway VM**, out of band. Hermetic
unit tests cover the detect/consent/verify orchestration with fake installers; the spec and tests do
**not** over-claim CI coverage of the install path.

## Risks / Verification

- **Unsuppressable, environment-specific prompts** (Xcode-CLT, sudo, MDM/corp policy) — surfaced as
  plain-language failure states, never reasoned away; failure is a clear error, not a half-done apply.
- **Install path under-covered by CI** (§5) — mitigated by hermetic fake-installer tests + a
  manual/real-machine run before any correctness claim.
- **Non-regression** — present-backend and no-brew-manifest paths must be byte-identical to today; the
  default `bootstrapBackendsFn` fake (present/available) guarantees existing tests are unchanged.
