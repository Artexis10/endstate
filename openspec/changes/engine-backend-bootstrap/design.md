## Context

- **Windows gets backends for free; Unix does not.** `winget` ships with Windows, so on Windows the
  package backend is always present. On macOS/Linux Endstate's backends are **not** preinstalled: the
  **Nix realizer** (`selectRealizer("linux"|"darwin") → nix.New()`) needs a Nix installation, and the
  **Homebrew driver** (scoped in `macos-brew-driver`) needs `brew` on `PATH`. A missing backend is a
  hard stop today: `nix.New()`/the realizer surface `REALIZER_UNAVAILABLE`; a brew driver would fail
  identically when `brew` is absent.
- **The audience makes "install it for me" load-bearing.** Endstate targets **non-technical users
  first**. "Rebuild my Mac/Linux box" must not require the user to first install a package manager they
  have never heard of. The backend must be **set up by the engine**, invisibly at the concept level.
- **Invisible ≠ unconsented.** The governing principle is **invisible but inspectable**: hide the
  *concepts* (the user never learns "Nix"/"Homebrew"), never the *consent*. macOS enforces this: the
  Homebrew installer triggers an Xcode Command Line Tools install and a sudo password prompt; the
  Determinate Nix installer is a privileged multi-user system change. Those prompts cannot and must not
  be suppressed. So the only honest model is **one plain-language consent**, then the official
  installer, then verify.
- **The brew design already did the brew half.** `macos-brew-driver` §8 + its Requirement "Homebrew is
  bootstrapped when absent, with consent" sketch the Homebrew bootstrap. It explicitly flags that the
  Nix half is **broader** (root/daemon, a macOS volume, the uninstall-the-backend question) and is
  "scoped separately." **This change is that separate scope** — generalized into one capability so the
  two bootstraps share a contract instead of drifting into two ad-hoc flows.

## Goals / Non-Goals

**Goals (of the eventual feature this scopes):**

- One backend-agnostic bootstrap contract — *detect → consent → official install → verify →
  proceed-or-decline* — that both the Nix realizer and the Homebrew driver satisfy.
- Turn today's "backend absent → hard stop" into "backend absent → offer to set it up," without ever
  installing a backend silently or leaving an apply half-done.
- Capture the **Nix-specific** footprint (multi-user daemon, macOS APFS volume, root, the
  uninstall-the-backend question) the brew §8 sketch does not cover.
- Keep the privileged step explicit, consented, inspectable, and consistent with Endstate's
  non-destructive / no-silent-mutation posture.

**Non-Goals:**

- No implementation in this change. No Go, no `internal/bootstrap/` package, no `select.go`/`apply.go`
  edit, no installer is run.
- Not re-specifying the Homebrew-specific bootstrap — that requirement stays in `macos-brew-driver` §8
  and graduates from there. This capability provides the **shared contract** it is one instance of.
- Not selecting the final installer flags, the exact consent UX, or whether the engine ever offers to
  *uninstall* a backend — those are the human's decisions (see Open Questions).
- Not a Windows concern — winget ships with the OS; the only Windows angle is asserting "no bootstrap
  needed."

---

## 1. The unified contract — five states, both backends

The bootstrap is one small state machine, identical in shape for Nix and Homebrew, differing only in
the *detect* probe, the *installer* invoked, and the *verify* probe:

| State | Nix realizer | Homebrew driver | Behavior |
|---|---|---|---|
| **Detect** | working `nix` (daemon + store) | working `brew` (on `PATH` / at prefix) | present+working → **no-op, no prompt** |
| **Consent** | "Endstate needs to set up its installer; macOS will ask for your password" | same plain-language ask | **one** prompt; the *concept* is hidden, the consent is not |
| **Install** | **Determinate Nix installer** | upstream Homebrew `install.sh` | the **official** installer, orchestrated (not vendored), inspectable |
| **Verify** | a Nix eval / `nix --version` + store reachable | `brew --version` | must pass **before** any package work; fail → backend unavailable |
| **Decline / fail** | skip the realizer lane | skip the brew lane | clear message; **the rest of the run continues**; never half-done |

The load-bearing rules:

1. **Present → silent no-op.** A working backend never prompts and never re-installs.
2. **Absent → exactly one consent, then the official installer.** Never silent. The prompt is plain
   language and names no backend product.
3. **Verify gates use.** A backend that installs but fails its verify probe is treated as
   **unavailable**, not used half-configured. (Mirrors the engine's verification-first invariant:
   observable working state is success, not "the installer exited 0".)
4. **Decline/failure is graceful, not fatal.** The affected lane is skipped with a clear message; other
   lanes (and config) still run. A failed bootstrap surfaces a clear error and never proceeds "as if"
   the backend were present.

## 2. Where it runs in the pipeline

`apply`/`capture`/`verify`/`plan` call `newRealizerFn()` (and, per the brew design, a brew-driver
factory) and treat "absent" as a hard error. The bootstrap is a **pre-step in front of that gate**: for
each backend a lane needs, *detect → (consent → install → verify)* runs **before** the factory would
hard-fail. A present backend makes the pre-step a no-op (today's fast path, unchanged). A declined or
failed bootstrap removes that lane and the run continues with whatever lanes remain — so a Mac with no
Nix but consented Homebrew still installs the brew apps, and a user who declines everything gets a
clear "nothing to do, these lanes were skipped" rather than a crash.

This composes with the brew two-lane design: the brew lane's bootstrap and the realizer lane's
bootstrap are independent pre-steps, each gating only its own lane.

## 3. The Nix half — why it is heavier than brew

The brew §8 sketch is correct but light because Homebrew's install is comparatively contained (a
user-owned prefix, `install.sh`, the unavoidable Xcode-CLT + sudo prompts). **Nix is a bigger system
change**, and that is the substance this capability adds:

- **Multi-user / daemon.** The standard (and Determinate) Nix install is a **multi-user** install:
  it creates the `/nix` store, a build-user group, and a **daemon** (launchd on macOS, systemd on
  Linux). This is root-level and persistent — heavier than a per-user `~/.brew`.
- **macOS APFS Nix Store volume.** On modern macOS the installer creates a **dedicated APFS volume**
  for `/nix` (because the root filesystem is read-only/sealed). That is a real disk-level artifact a
  user should be told about in plain language, and it bears on the uninstall question.
- **Root + the unsuppressable prompts.** Like brew's sudo/Xcode-CLT prompts, the Nix install needs
  elevated privilege; the engine surfaces one plain-language consent and lets the OS own the
  credential prompt.
- **The uninstall-the-backend question.** Because the Nix install is a persistent, root-level, possibly
  volume-creating change, "Endstate installed Nix for me — does Endstate remove it?" is a real
  question. **Recommendation:** the engine does **not** silently uninstall a backend it installed.
  Uninstalling a backend is a **separate, explicit, user-owned action** (the Determinate installer ships
  its own uninstaller; the engine may at most *point at it*), consistent with non-destructive defaults
  and no-silent-deletion. Whether the engine offers an *assisted* uninstall at all is an Open Question.
- **Determinate installer as the vehicle.** The repo already standardizes on the **Determinate Nix
  installer** in CI (`nix-integration.yml`). Reusing it for the user-facing bootstrap means the same,
  well-supported, flake-enabled install path the engine is already validated against — not a bespoke
  install script.

## 4. The brew half — owned by `macos-brew-driver`, referenced here

The Homebrew-specific bootstrap requirement ("absent → consent → official `install.sh` → verify →
graceful decline; present → no-op") is already sketched in `macos-brew-driver` §8. This capability does
**not** restate it as a brew-specific requirement; instead its backend-agnostic requirements (§1) are
written so the brew bootstrap is one **instance** of the contract. On graduation, the brew-specific
requirement graduates from `macos-brew-driver`; the shared contract graduates from here. This keeps the
two changes composable under `openspec validate --all --strict` (distinct capabilities, distinct
requirement names) without forking the brew story.

## 5. Consent UX and the CLI-source-of-truth boundary

The **CLI is the source of truth**; the **GUI is the presentation layer**, and the primary audience
runs the GUI. So the bootstrap's consent should be an **engine-emitted, streamed consent request** that
the GUI renders as a plain-language dialog ("Endstate needs to set up its installer — this is safe;
your Mac will ask for your password"), with the engine proceeding only on an explicit affirmative. For
the CLI, the same consent is an interactive prompt (or an explicit `--bootstrap-backends` /
`--no-bootstrap` flag for non-interactive/CI use). The exact UX (one combined consent vs per-backend,
flag names, default in non-interactive mode) is an Open Question; the **invariant** this capability
fixes is only that consent is explicit, plain-language, and names no backend product.

## 6. The CI-test wrinkle (recorded, not solved here)

The eventual implementation cannot fully validate the **install** path on the existing macOS CI runner:
the GH `macos-latest` runner has **Homebrew preinstalled** and (via the Determinate action) **Nix
preinstalled** in the smoke job. So in CI the bootstrap can only exercise **detect → present → no-op**.
The *absent → consent → install → verify* path must be validated on a **clean real machine** (or a
throwaway VM image without the backend), out of band from the path-filtered macOS smoke. This is a
known limitation of the existing CI vehicle, recorded so the implementation does not over-claim CI
coverage of the install path (the winget/real-output lesson, applied to installers).

## Open Questions (for the human)

1. **Phase / sequencing.** Does the bootstrap land **with** the brew increment (so "rebuild my Mac with
   zero prerequisites" is true on day one) or as a **fast-follow** after the brew install/capture lanes
   are proven? (The brew design's Open Question 7 asks the same for the brew half.)
2. **One consent or per-backend.** On a Mac needing **both** Nix (for config) and Homebrew (for apps),
   is it **one** combined consent ("set up Endstate's installers") or **two** (one per backend)? One is
   gentler for the audience; two is more honest about two distinct system changes.
3. **Non-interactive default.** In a non-interactive/CI context with no TTY, is the default to **skip**
   the bootstrap (and the lane) or to **fail loudly**? Recommend: skip-with-clear-message by default,
   `--bootstrap-backends` to opt in, `--no-bootstrap` to force skip.
4. **Assisted uninstall.** Should the engine ever offer to **uninstall** a backend it installed (via the
   official uninstaller), or only ever *point at* the uninstaller? Recommend: never silently uninstall;
   at most surface the official uninstaller. Confirm.
5. **Nix install flavor.** Confirm the **Determinate** installer (matches CI, flake-enabled,
   well-supported) over upstream `nix-installer` / a single-user install. Multi-user is heavier but is
   the supported default; is a single-user/no-daemon option ever wanted (e.g. locked-down corp Macs)?
6. **macOS volume disclosure.** How much of the APFS Nix Store volume / daemon footprint must the
   plain-language consent disclose to stay honest without overwhelming a non-technical user?

## Risks / Verification (of the eventual feature)

- **Unsuppressable, environment-specific prompts** — Xcode-CLT, sudo, macOS volume creation, MDM/corp
  policy blocks. The bootstrap must surface these as plain-language states, never reason them away;
  failure is a clear error, not a half-done apply.
- **Install-path is under-covered by CI** (§6) — the macOS runner has the backends preinstalled, so the
  install path is validated only on clean real machines. Mitigation: hermetic tests of the
  detect/consent/verify orchestration (fake `ExecCommand`), plus a manual/real-machine install run
  before any claim of correctness.
- **Privileged, persistent system change** — a multi-user Nix daemon + APFS volume is not trivially
  reversible. Mitigation: explicit consent, official installer only, no silent uninstall, and clear
  disclosure of what is being changed.
- **Partial-bootstrap leaving a confusing state** — installer exits non-zero midway. Mitigation: the
  verify gate (§1 rule 3) — a backend that does not pass verify is treated as unavailable, and the lane
  is skipped with a clear error rather than used half-configured.
- These are verification *targets for the future implementation*, recorded so the eventual proposal
  inherits them. **No code is verified by this design-only change.**
