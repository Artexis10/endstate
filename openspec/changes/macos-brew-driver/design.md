## Context

- **Two backend shapes already exist.** `driver.Driver` (`internal/driver/driver.go`) is per-package
  and non-atomic — `Detect(ref) (bool, name, err)` + `Install(ref) (*InstallResult, err)` — with
  three **optional** interfaces a driver may also implement: `Uninstaller` (best-effort rollback),
  `VersionedInstaller` (`InstallVersion` / `ReinstallVersion` pinning), and `BatchDetector`
  (`DetectBatch(refs) → map[ref]DetectResult`, where `DetectResult` carries `Installed`,
  `DisplayName`, and best-effort `Version`). `winget` is the only `Driver` and implements all three.
  `realizer.Realizer` is the whole-set, atomic-generation model (Nix), deliberately **not** a
  `Driver`.
- **Selection is GOOS-keyed and mutually exclusive today.** `selectBackend(goos)` returns the winget
  `Driver` for `windows` and `ErrNoBackend` otherwise; `selectRealizer(goos)` returns `nix.New()` for
  `linux`/`darwin` and `ErrNoRealizer` otherwise. The doc comment states the invariant plainly: *"on
  any host at most one succeeds."* `apply`/`capture`/`plan`/`verify` all call `newRealizerFn()`
  **first** and, when it succeeds, **return the realizer path** — the per-package driver loop below is
  unreachable on darwin. This single short-circuit is the crux this design must break.
- **The per-app selection seam already exists in the schema.** `manifest.App`
  (`internal/manifest/types.go`) already has `Refs map[goos]string`, `Driver string` (`json:"driver"`,
  omitempty), and `Version string`. The GOOS-keyed ref and a per-app `driver` field are exactly the
  hooks a brew/nix split needs — no new schema invention is required for routing, only a meaning for
  `driver: "brew"` and a ref convention for Casks.
- **Winget already set the precedent brew mirrors.** `winget-best-effort-rollback` specifies the
  non-native rollback-by-uninstall pattern (uninstall the union of later generations' added refs,
  per-package failure tolerated, confirmation required, append a rollback generation).
  `windows-version-capture-pinning` specifies version capture (record installed version, empty is not
  a failure) and pinning (declared version installs that exact version, pinning only applies to
  drivers that support a specific version; the realizer ignores per-package version). Brew reuses both
  shapes, with brew-specific caveats noted below.
- **The primary audience is non-technical.** Endstate targets non-technical users first (technical users
  second). For them the headline macOS need is reinstalling *GUI apps* (Chrome, Zoom, Spotify, Office…)
  with zero terminal and zero knowledge of any package manager — a need Nix on macOS cannot meet and brew
  Casks can. This reframes brew from a developer convenience into the **core app-recovery path** for the
  people Endstate is for, and it bears directly on the routing-default decision (§3 / Open Question 2) and
  on bootstrapping the backend invisibly (§8).

## Goals / Non-Goals

**Goals (of the eventual feature this scopes):**

- A Mac user can declare native GUI apps (Casks) and CLI tools (formulae) and have Endstate install,
  verify, and capture them through Homebrew.
- Brew coexists with the Nix realizer on the same darwin host: config + Nix packages via the realizer
  (default), brew apps via the driver (opt-in).
- Brew follows winget's driver shape (optional `Uninstaller` / `VersionedInstaller` / `BatchDetector`)
  so the engine's per-package machinery is reused, not reinvented.
- A no-Nix adoption path: a manifest that uses **only** brew apps should be installable on a Mac that
  has Homebrew but not Nix.

**Non-Goals:**

- No implementation in this change. No Go, no new driver package, no `select.go` edit, no dependency.
- Not replacing Nix on macOS. Brew is additive and opt-in; the realizer stays the default backend for
  config and for Nix packages.
- Not a Linuxbrew story. Homebrew-on-Linux exists, but on Linux the Nix realizer is the established
  backend; this scopes brew as the **macOS** driver only.
- Not selecting the final ref scheme or routing rule — the recommendation is a starting position; the
  decisions are the human's (see Open Questions + `tasks.md`).

---

## 1. Driver shape — brew as `driver.Driver`, winget's macOS sibling

Brew is a per-package, non-atomic `driver.Driver`. It implements the same surface winget does:

| Interface | Method(s) | Brew mapping |
|---|---|---|
| `Driver` (core) | `Name() → "brew"` | stable identifier |
| `Driver` (core) | `Detect(ref)` | `brew list <name>` / `brew list --cask <name>` exit code → installed; display name from `brew info` or the bare name |
| `Driver` (core) | `Install(ref)` | `brew install <formula>` or `brew install --cask <app>`; non-zero, non-already-installed exit → `StatusFailed`/`ReasonInstallFailed` |
| `Uninstaller` (optional) | `Uninstall(ref)` | `brew uninstall <name>` / `brew uninstall --cask <name>`; already-absent → `StatusAbsent` |
| `VersionedInstaller` (optional) | `InstallVersion` / `ReinstallVersion` | best-effort pin — see §5; brew's pin model is weaker than winget's `--version` |
| `BatchDetector` (optional) | `DetectBatch(refs)` | one `brew list --versions` + one `brew list --cask --versions` parsed into `map[ref]DetectResult` with `Version` filled |

**Best-effort and non-atomic, like winget.** Brew installs one package at a time; a failure on one
package does not abort the others. Rollback is the **winget best-effort pattern**, reused verbatim:
because brew exposes `brew uninstall`, the engine's existing best-effort rollback
(`winget-best-effort-rollback`) extends to brew unchanged — uninstall the union of added refs from
generations after the target, tolerate per-package failure, require confirmation, append a
rollback-produced generation, and surface the untracked-transitive-dependency caveat (brew's
auto-installed dependencies are exactly such a caveat). Brew gets **no** generation/native rollback —
that is a realizer-only (Nix) property and stays so.

This means brew adds **no new core interface** to `driver.go`. It is a second implementation of an
interface set winget already proves out, which is the strongest argument that the driver/realizer
split was the right abstraction.

## 2. Formula vs Cask — how a manifest ref distinguishes them

`brew install <formula>` installs a CLI package; `brew install --cask <app>` installs a GUI `.app`.
The engine must know which from the manifest. Three candidate schemes:

| Scheme | Example | Pros | Cons |
|---|---|---|---|
| **A. `cask:` prefix in the darwin ref** (recommended) | `"refs": { "darwin": "cask:google-chrome" }`; bare `"darwin": "ripgrep"` = formula | Lives entirely in the existing `Refs` map; no schema change; one ref string carries name **and** kind; reads naturally; the bare-name default is the formula (the CLI-package case that matches Nix's package shape) | A magic prefix is a micro-DSL inside a string; the parser must strip and route it |
| **B. A new per-app field** | `"darwin": "google-chrome", "darwinKind": "cask"` | Explicit, typed, validatable | New schema field for a darwin-only concern; redundant with the ref; clutters `App` for every other OS |
| **C. Two ref keys** | `"darwin-cask": "google-chrome"` vs `"darwin": "ripgrep"` | No prefix parsing | Pollutes the GOOS keyspace with a non-GOOS key; `Refs[runtime.GOOS]` lookups (used throughout apply/verify) would miss it |

**Recommendation: Scheme A, the `cask:` prefix.** It keeps Casks first-class while requiring **zero
schema change** — `Refs[runtime.GOOS]` already returns the ref string, and the brew driver strips a
leading `cask:` to decide `--cask`. The bare-name default is the formula, which lines up with how Nix
refs already name CLI packages, so a CLI tool's `darwin` ref can often be written once and routed to
either backend. Because Casks are the headline value, the spec calls them out explicitly and the
prefix makes them visually obvious in the manifest. (If the human prefers an explicit typed field for
validatability, Scheme B is the fallback; the spec is written kind-agnostic so either survives.)

A subtlety: a `cask:` ref is **only meaningful to the brew driver**. An app with `"driver": "brew"`
and `"darwin": "cask:slack"` is unambiguous. An app **without** `driver: "brew"` whose darwin ref is
`cask:...` is a manifest error (the realizer cannot install a Cask) and should fail loudly at load —
see §3 routing.

## 3. THE HARD PART — the selection model on darwin (both backends live)

**The problem.** macOS is the first OS where `selectRealizer` returns a realizer **and**
`selectBackend` would return a driver. Today exactly one backend exists per host and apply/capture/
plan/verify encode that by calling `newRealizerFn()` first and returning its path when it succeeds —
on darwin that always happens, so the winget-style driver loop never runs. Adding a brew driver
without changing that control flow would make brew **unreachable**.

**The routing rule (recommended).**

1. **The Nix realizer stays the default backend for darwin.** Config (`homeManager.*`) and any app
   **without** an explicit driver go through the realizer, exactly as today. Nothing about a
   pure-Nix Mac manifest changes.
2. **`"driver": "brew"` opts an app into the brew driver.** It is per-app and explicit. The value of
   the existing `App.Driver` field selects the backend; absent/empty means "the host default," which
   on darwin is the realizer.
3. **The pipeline splits darwin apps into two lanes, not one.** Instead of "realizer present →
   realizer owns everything," the engine partitions `manifest.Apps` by resolved backend: apps routed
   to brew form the **driver lane**; everything else (config + default apps) forms the **realizer
   lane**. Both lanes run; results merge into one envelope. This is the load-bearing structural change
   the implementation must make and the design's central recommendation.
4. **Manual apps stay manual.** `manual` apps are orthogonal to backend and unaffected.

**How each command coexists:**

- **apply:** run the realizer lane (config + default/Nix apps) and the brew driver lane (best-effort
  per-package installs, batch-detected first). Order: realizer first (it may provide the toolchain),
  then brew. Each lane records its own provisioning, attributed to its backend (§ capture attribution).
- **plan / verify:** compute the realizer diff for its lane and per-package presence (and optional
  version) for the brew lane; emit both into one plan/verify report. `verify` of a brew app is
  `brew list` presence + optional version (§5).
- **capture:** run **both** capture lanes on darwin — the realizer recovery (home-manager + Nix
  packages from provisioning history) **and** a brew enumeration (`brew leaves`, `brew list --cask`).
  Merge into one manifest.

**Capture attribution — which backend did a discovered package come from?** This is the genuinely
hard sub-question, because a Mac may have both Nix-provisioned and brew-installed packages.
Recommended rule, in priority order:

1. **Provisioning history is authoritative for what Endstate installed.** A package recorded in a
   realizer Provisioning Generation is attributed to the realizer; a package recorded in a brew-driver
   generation is attributed to brew. This is exact for Endstate-managed packages and matches how
   winget capture already prefers recorded refs.
2. **`brew leaves` / `brew list --cask` enumerate the brew-owned set directly.** Homebrew has its own
   prefix (`/opt/homebrew` or `/usr/local`) and its own database; `brew leaves` (top-level formulae,
   excluding dependencies) and `brew list --cask` are the authoritative brew inventory. Anything brew
   reports is a brew app, emitted with `driver: "brew"` and a `cask:`-prefixed ref for casks.
3. **Nix packages come from the realizer's recovery path, not from brew.** They are disjoint by
   construction (different stores, different databases), so double-counting is structurally avoided.
4. **Unprovisioned, non-brew, non-Nix apps** (hand-installed `.app`s, Mac App Store) are out of scope —
   capture records what brew and the realizer own, not arbitrary `/Applications` contents.

**Conflict / validation rules to spec:**

- A `cask:` ref on an app **not** routed to brew is a load error (the realizer cannot install a Cask).
- `driver: "brew"` on a non-darwin host: the design recommends treating brew as **darwin-only**; a
  `brew`-driver app with no `darwin` ref is simply not applicable on other hosts (skipped), and on
  Windows `driver: "brew"` with a `windows` ref should fail loudly rather than silently use winget.
- An app may declare refs for multiple OSes; `driver: "brew"` only takes effect when the host is
  darwin and a `darwin` ref is present.

## 4. Capture — `brew leaves` / `brew list` → manifest

- **Formulae:** `brew leaves` lists top-level (explicitly installed) formulae, excluding dependencies
  brew pulled in — the right granularity for a manifest (you declare what you wanted, not its
  closure). Each becomes an app with a bare `darwin` ref and `driver: "brew"`.
- **Casks:** `brew list --cask` lists installed casks. Each becomes an app with a `cask:`-prefixed
  `darwin` ref and `driver: "brew"`.
- **Versions:** `brew list --versions <name>` (and `--cask --versions`) yields the installed version,
  recorded best-effort exactly like winget — an unavailable version is recorded empty and does **not**
  fail capture (`windows-version-capture-pinning` precedent).
- **Round-trip:** capture → manifest → apply must converge. Because `brew leaves` excludes
  dependencies, re-applying the captured manifest reinstalls the same top-level set; brew re-resolves
  the dependency closure. Casks round-trip by bare cask token. The capture attribution rule (§3)
  ensures a brew app is re-emitted as a brew app, not mis-attributed to the realizer.

## 5. Verify + version

- **Presence:** `brew list <formula>` / `brew list --cask <app>` exit code is the presence check
  (the `DetectBatch` fast path runs `brew list --versions` once for all formulae and once for all
  casks).
- **Version pin (weaker than winget):** brew is **not** a precise version-pinning tool. `brew install`
  installs the current formula/cask version; brew has no first-class `--version` flag like winget.
  Pinning a formula to an older version means an explicit `brew pin` (which only **freezes** the
  currently-installed version against upgrade, it does not *select* an arbitrary past version) or
  installing a versioned formula (`name@X`) when the tap provides one, or an `extract`/URL pin —
  all fragile. **Recommendation:** brew implements `VersionedInstaller` only in the weak sense —
  honor a versioned-formula ref (`node@20`) where the tap offers one, and otherwise treat a declared
  version as **advisory**: verify reports drift if the installed version differs, but `apply` does not
  attempt arbitrary downgrades. This mirrors `windows-version-capture-pinning`'s rule that pinning
  applies only where the backend genuinely supports a specific version, and that an unavailable
  pinned version is an install failure rather than a silent substitution. The spec states brew's
  pin is best-effort and may be a no-op for formulae the tap does not version.

## 6. Real-macOS verification plan

The winget lesson (recorded across the Nix-realizer effort): **real package-manager output parsing is
not reasonable-by-inspection.** Winget capture had two real bugs that only surfaced against real
winget v1.28 — exit codes, column layout, and batch-vs-per-ref name differences were all wrong on
paper. Brew has the same traps:

- `brew list` exit codes (0 present / 1 absent) and whether a missing formula prints to stderr.
- `brew list --versions` column layout (name + space-separated versions, possibly multiple).
- Cask vs formula output divergence (`brew list --cask` token format vs formula names).
- `brew leaves` semantics (top-level only) vs `brew list` (everything, including dependencies).
- `brew info --json` shape if used for display names.
- Exit code on "already installed" vs "newly installed" for the install idempotency mapping.

**Plan:** hermetic unit tests (fake `ExecCommand`, like winget's `_test.go` suite) for all parsing,
**plus** a real-brew smoke on a real Mac. The vehicle already exists: `.github/workflows/
nix-integration.yml` runs a `macos-latest` runner (`continue-on-error`, non-blocking). A future
implementation change adds a `brew-realbrew-smoke.sh` step there (install a tiny formula + a tiny
cask, capture, assert round-trip), so the brew parsing is validated against real Homebrew exactly as
the Nix path is validated against real Nix. **No smoke is added by this design-only change** — this
is the recorded plan for the implementation.

## 7. Phased path

- **Phase 1 — formulae + Casks install/capture/verify (the value, together).** Brew `Driver`
  (`Detect`/`Install`) with the `cask:` ref scheme so Casks ship **with** formulae, not after — Casks
  are the headline "rebuild my Mac" value and splitting them out would ship the less-valuable half
  first. `BatchDetector` for fast presence. Capture via `brew leaves` + `brew list --cask`. Verify via
  `brew list`. The darwin two-lane routing (§3) lands here because nothing works without it.
- **Phase 2 — best-effort rollback + version.** Implement `Uninstaller` so the existing
  `winget-best-effort-rollback` pattern extends to brew, and `VersionedInstaller` in the weak sense of
  §5 (versioned-formula refs + advisory drift). Add the real-brew smoke to `nix-integration.yml`.
- **Phase 3 — polish / no-Nix adoption ergonomics.** Make a brew-only manifest installable on a Mac
  with Homebrew but no Nix (the realizer lane is empty, so `newRealizerFn()` must not be a hard
  requirement when no `homeManager`/Nix apps are declared). Tap management, `brew bundle`
  interop, and Mac App Store (`mas`) coverage are explicitly deferred.

**Recommendation:** ship **Phase 1 with Casks included** — formulae-only would deliver the smaller
half of the value. Treat rollback/version (Phase 2) as the immediate follow-up, and the no-Nix
ergonomics (Phase 3) as the adoption unlock once the core is proven.

## 8. Invisible Homebrew bootstrap (install Homebrew if absent)

For the non-technical audience the backend must be **set up by the engine**, not by the user. Today the
engine treats a missing backend as a hard stop (the realizer returns `REALIZER_UNAVAILABLE` when Nix is
absent); a brew driver would similarly fail if `brew` is not on `PATH`. That is the wrong behavior for
someone who just wants their apps back — "go install Homebrew first" is exactly the wall this feature
exists to remove.

**The model: detect → consent → bootstrap → verify → proceed (or fail gracefully).**

- **Detect.** Before the brew lane runs, check for a working `brew` (on `PATH` / at the Homebrew prefix).
  Present and working → no-op, no prompt.
- **Consent — and why it cannot be fully silent.** The official Homebrew install triggers an **Xcode
  Command Line Tools** install and asks for the user's **admin password** (sudo). Those prompts are
  enforced by macOS; the engine cannot and must not suppress them. So "invisible" means the *concepts*
  are hidden (the user never learns the word "Homebrew"), **not** that consent is hidden. The engine
  explains in plain language ("Endstate needs to set up its installer — this is safe; macOS will ask for
  your password") and asks once.
- **Bootstrap.** Run the **official** Homebrew installer (the upstream `install.sh`) — the engine
  *orchestrates* it; it does not vendor or fork the installer. This is a privileged, system-modifying
  step, so it must be **explicit, consented, and inspectable** (the user can see exactly what will run),
  consistent with Endstate's non-destructive / no-silent-mutation posture.
- **Verify.** After install, confirm `brew` works (`brew --version`) before continuing; a failed
  bootstrap surfaces a clear, plain-language error and never leaves the apply silently half-done.
- **Graceful decline.** If the user declines, the brew lane is skipped with a clear message and the rest
  of the apply still runs. Declining is not a crash.

**Scope note.** This is the **Homebrew** half of a broader "the engine installs its own backends"
capability that also covers **Nix** (the Determinate installer — the same one `nix-integration.yml`
already uses in CI) on macOS/Linux. The Nix half is broader (root/daemon, a macOS volume, the
uninstall-the-backend question) and is scoped **separately**; what belongs *here* is the brew-specific
bootstrap. Windows is unaffected (winget ships with the OS). Whether this lands in Phase 1 (it is
arguably part of the "rebuild my Mac with zero prerequisites" promise) or as a fast-follow is an open
question.

## Open Questions (for the human)

1. **Ref scheme.** Confirm the `cask:` prefix (Scheme A) over a typed `darwinKind` field (Scheme B).
   Is a string micro-DSL acceptable, or is an explicit typed field preferred for validatability?
2. **Routing default — the key product decision.** The recommendation above keeps the Nix realizer as
   the darwin default with `driver: "brew"` as per-app opt-in (conservative, backward-compatible). **But
   weigh that against the primary audience:** non-technical users mostly want *apps*, and on macOS those
   come from brew/Casks — so for them, **brew arguably should be the default backend for `apps` on
   darwin**, with the Nix realizer reserved for `homeManager` config and explicitly Nix-routed packages.
   That reverses today's default and is a bigger behavior change, but it is the stronger "rebuild my Mac
   with zero Nix" story and aligns the default with who Endstate is for. A middle path: keep the
   *engine* default at realizer, but have the **GUI / non-technical flow** emit brew-routed app entries
   by default, so the *product* default is brew even if the *engine* default stays nix. Decide:
   realizer-default+brew-opt-in (conservative), brew-default-for-apps+nix-for-config (audience-aligned),
   or the GUI-default middle path.
3. **No-Nix adoption.** How hard a requirement is "installable on a Mac without Nix"? It implies the
   pipeline must run the brew lane even when `newRealizerFn()` would otherwise own the host — i.e. the
   realizer becomes optional when no Nix/config work is declared. Phase 1 or Phase 3?
4. **Capture attribution edge.** Is "provisioning history first, then `brew leaves`/`brew list --cask`"
   the right attribution? What about a package present in **both** Nix and brew (unlikely but possible
   for some CLI tools)? Recommend: report it once, attributed to whichever Endstate provisioning
   recorded it; if neither, attribute to brew (it is in the brew database).
5. **Version semantics.** Accept brew's weak pin (advisory drift + versioned-formula refs only), or
   require precise pinning (and therefore reject formulae the tap does not version)? The winget spec
   makes an unavailable pinned version an install failure; should brew match that strictness or be
   lenient given brew's model?
6. **Cask uninstall destructiveness.** `brew uninstall --cask` removes an app but may leave user data
   (or, with `--zap`, remove it). Best-effort rollback should use the **non-zap** uninstall
   (non-destructive default), consistent with Endstate's backup-before-overwrite / no-silent-deletion
   posture. Confirm.
7. **Invisible bootstrap (§8).** Should the engine bootstrap **Homebrew when absent** (detect → consent
   → official install → verify → graceful decline), and in which phase (Phase 1 as part of the "zero
   prerequisites" promise, or a fast-follow)? Confirm the consent model (one plain-language prompt; the
   macOS password / Xcode-CLT prompts cannot be suppressed), and that the parallel **Nix** bootstrap is a
   separate, broader capability scoped on its own.

## Risks / Verification (of the eventual feature)

- **Real-output drift** — the headline risk (the winget lesson). Mitigation: hermetic parse tests +
  the real-brew smoke on the macOS runner before any claim of correctness.
- **Routing regression on pure-Nix Macs** — the two-lane split must be byte-identical for a manifest
  with no `driver: "brew"` apps. Mitigation: a test asserting a no-brew darwin manifest produces the
  same envelope as today's realizer-only path.
- **Cask write-permission / GUI-app surprises** — casks may prompt, need `/Applications` write access,
  or require Rosetta. These are runtime realities the smoke must surface, not reason about.
- **Best-effort uninstall caveats** — brew auto-installed dependencies are untracked transitive
  installs; rollback must surface the same caveat winget does and default to non-destructive uninstall.
- These are verification *targets for the future implementation*, recorded so the eventual proposal
  inherits them. **No code is verified by this design-only change.**
