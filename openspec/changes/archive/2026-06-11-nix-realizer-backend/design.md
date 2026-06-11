## Context

`platform-backend-foundation` left `selectBackend(goos)` returning `ErrNoBackend` on non-Windows hosts. The pipeline above the driver layer (planner, events, envelope, manifest) is platform-agnostic, but the install path is per-package: `apply`/`verify` loop over apps calling `driver.Driver.Detect(ref)` / `Install(ref)` and emit one item event per app. Nix's model is the opposite — **whole-set declarative**: you state the desired set, Nix computes a closure and commits it as one **profile generation**, atomically.

The spike (Determinate Nix 3.21.0) validated the load-bearing premise and was **re-confirmed against live Nix during this change** (isolated `/tmp` profiles, `--log-format internal-json`):

**Atomicity — confirmed.** A generation advances only on full success.

| Probe | exit | generation |
|-------|------|-----------|
| `nix profile add nixpkgs#hello` (success) | 0 | 0 → **1** |
| `nix profile add nixpkgs#hello nixpkgs#<bad-attr>` (partial) | 1 | 0 → **0** (valid pkg NOT partially applied) |
| permission failure at commit (after a successful build) | 1 | 0 → **0** (prior generation intact) |

**Failure classification — anchor map (re-harvested verbatim from Nix 3.21.0):**

| Class | Structural signal | Anchor (real text) | Engine code |
|-------|-------------------|--------------------|-------------|
| eval / bad attr | no build activity | `does not provide attribute` | `INSTALL_FAILED` (subcode `eval`) |
| network / fetch | `FileTransfer`, no build | `unable to download` / `HTTP error` / `while fetching` | `INSTALL_FAILED` (subcode `network`) |
| daemon / store unavailable | no real activity | `opening a connection to remote store` (lvl 0) / `cannot connect to socket` (lvl 1) | **`REALIZER_UNAVAILABLE`** |
| permission | build runs, **commit fails** | `Permission denied` / `read-only file system` | `PERMISSION_DENIED` |
| collision | build runs, gen does **not** advance | `an existing package already provides the following file` *(see note)* | `INSTALL_FAILED` (subcode `collision`) |
| spawn (nix missing/unrunnable) | — | — | **`REALIZER_UNAVAILABLE`** |
| _unrecognised_ | — | — | `INSTALL_FAILED`, raw text → `error.detail` |

> **Collision note:** at default priority Determinate Nix **auto-resolves** colliding paths (re-confirmed: `coreutils` + `uutils-coreutils-noprefix` → exit 0, gen advanced). A real collision only surfaces at **equal forced priority**; structurally it is `build-without-gen-advance` → `INSTALL_FAILED` regardless. The collision anchor is therefore **spike-sourced and best-effort**; the contract test must **force** an equal-priority collision to harvest it, or rely on the structural signal.
>
> **The spike's #1 lesson:** reasoned anchors are wrong (it guessed 0/3 collision anchors correctly). Every anchor is harvested from real pinned-Nix output and locked behind a contract test that re-asserts the mapping; a Nix upgrade that rewords a message fails CI loudly rather than silently mis-classifying.

## Goals / Non-Goals

**Goals:**
- A real whole-set `Realizer` beside `driver.Driver`; one atomic generation switch per apply.
- Nix failures → stable engine codes; raw Nix text confined to `error.detail` (the moat: the user never has to read Nix).
- The per-item event contract (phases, statuses, summaries) is preserved unchanged.
- **Zero Windows behavior change**, provable by regression tests.

**Non-Goals:**
- **No config/restore/verify-module changes** — Nix owns packages only in v1; config modules keep their existing (path/verify-based) behavior on Linux.
- **No uninstall / `Diff.ToRemove`** — v1 is additive (`nix profile add` only). Removal/convergence-to-exact-set is a follow-up.
- **No rollback / restore-as-generation-switch yet** — atomicity makes it a clean later addition.
- **No per-manifest pin override and no channel→rev resolution** — a single engine-owned pinned revision in v1.
- **No `App.Driver` per-app backend override**, no bootstrap/release changes, no GUI changes.

## Decisions

1. **`Realizer` lives in a new package `internal/realizer`, not in `driver`** (decision 3: *beside* `Driver`). It is **not** a `driver.Driver` — Nix is never shoehorned into `Driver.Install`. Putting it in its own package keeps the `driver` package (which the Windows binary compiles) free of Nix-shaped types.

   ```go
   type Installable struct { ID, Ref string }           // App.ID + pinned flake installable
   type Element     struct { Name, AttrPath string; StorePaths []string }
   type Set         struct { Generation int; Elements map[string]Element } // parsed `nix profile list --json`
   type Diff        struct { ToAdd, Present []Installable }                  // Plan output
   type Result      struct { Advanced bool; FromGeneration, ToGeneration int; After Set; Err *Error }
   type Error       struct { Code envelope.ErrorCode; Subcode, Stage, Raw string } // Raw → error.detail ONLY

   type Realizer interface {
       Name() string                                  // "nix"
       Current() (Set, error)                         // `nix profile list --json`
       Plan(desired []Installable) (Diff, error)      // diff desired vs Current; NO mutation
       Realize(toAdd []Installable) (Result, error)   // ONE `nix profile add <toAdd...>`; atomic gen switch
   }
   ```

2. **`Realize` operates on `ToAdd` (the diff), never the whole desired set.** `nix profile add` is *append*, not set-reconcile; re-adding already-present installables risks a collision that would fail the entire atomic switch. `Plan` computes `ToAdd = desired − Current`; `Realize(diff.ToAdd)` adds only those.

3. **`internal/realizer/nix` implements `Realizer`** via an injected runner (`Run func(args ...string) (stdout, stderr []byte, exit int, err error)`) so tests are hermetic — mirroring winget's `ExecCommand` seam. It calls `nix profile add <toAdd...> --log-format internal-json` (decision 1: supported verb, not the deprecated `install` alias) and parses `nix profile list --json` (version 3 name-keyed object; legacy array tolerated). Build-tag split `nix_other.go` (`//go:build !windows`, real exec) / `nix_windows.go` (`//go:build windows`, stub returning `REALIZER_UNAVAILABLE`) — following `keychain_windows.go`/`keychain_other.go` — so windows-latest links the package without ever spawning `nix`.

4. **`classify(exitCode, events, generationAdvanced) → *realizer.Error` is the single source of the code** — including the spawn/binary-missing → `REALIZER_UNAVAILABLE` path (so the one contract test covers every path; decision 5). Two tiers: **structural** signals (ActivityType + gen-advanced) decide the pipeline stage (`fetch`/`build`/`commit`) and the `network`/`collision` subcodes without msg text; **anchors** (msg-text substrings, priority-ordered) carry the top-level class only for the daemon/permission/eval classes the spike proved are not structurally separable. The anchor table is locked by a hermetic contract test over `testdata/*.stderr` captured from real Nix 3.21.0 (runs on windows-latest too — canned input, no real nix).

5. **Selection: `selectBackend(goos)` is byte-unchanged; a sibling `selectRealizer(goos)` returns the realizer** (linux/darwin → `nix.New()`; else `ErrNoRealizer`), with a `newRealizerFn` injection seam beside `newDriverFn`. *Reconciliation:* the handoff suggested wiring Nix into `selectBackend`, but because decision 3 keeps Nix off `driver.Driver`, selection stays GOOS-keyed and centralized in `select.go` via a sibling selector rather than overloading `selectBackend`'s return type — honoring the intent (one place decides the backend) without a leaky union type.

6. **Strict ref resolution on the realizer path.** Resolve `App.Refs[runtime.GOOS]` **directly**; an app with no `linux`/`darwin` ref is **skipped** (status `skipped`) — it must never fall back to the first-non-empty ref, which would feed a winget id like `Mozilla.Firefox` to `nix profile add`. A **bare attribute** (`ripgrep`) is expanded against an engine-owned pinned revision into `github:NixOS/nixpkgs/<pinned-rev>#ripgrep`; a string already in flakeref form passes through verbatim (power-user escape hatch). Manifest authors write `"ripgrep"`, never Nix syntax (the moat).

7. **`apply`/`verify`/`plan` fork early on backend kind; the whole-set result fans out into the existing per-item event stream.** New `*_realizer.go` files hold the fan-out so the originals' diffs stay tiny (an early `if r,err := newRealizerFn(); err == nil { return runXxxRealizer(...) }`). On Windows `selectRealizer` errors, so the existing winget loop runs byte-identically. Fan-out rules:
   - **Plan:** `Plan(desired)` once → emit `present` for `Diff.Present`, `to_install` for `Diff.ToAdd`; one `plan` summary. Dry-run stops here.
   - **Apply:** emit `installing` for each `ToAdd`, call `Realize(ToAdd)` **once**. On `Advanced` → each `ToAdd` emits `installed`. On failure (gen not advanced → nothing installed) → **every** `ToAdd` emits `failed`; naming the single culprit is **best-effort** (it depends on unstable attr-path text), not guaranteed.
   - **Verify:** re-read `Current()`; present → `present`, absent → `failed`/`missing`.
   - Item `message` fields are **engine-authored**; raw Nix text lands **only** in `error.detail` (decision 8). For systemic classes (`REALIZER_UNAVAILABLE`, `PERMISSION_DENIED`) the command returns a top-level `*envelope.Error`; this **truncates** the per-item stream (no further item/summary events) — specced explicitly.

8. **`capabilities.driversFor` consults `selectRealizer`** → `["nix"]` on linux/darwin, byte-identical `["winget"]` on Windows. `PlatformInfo` shape is unchanged (the `drivers` value `"nix"` is additive).

9. **Additive `ErrRealizerUnavailable = "REALIZER_UNAVAILABLE"`** in `envelope/errors.go`, beside `ErrWingetNotAvailable`. Distinct from `WINGET_NOT_AVAILABLE` so the GUI can give Nix-specific remediation without leaking Nix internals beyond `error.detail`.

## Alternatives Considered

- **Driver-adapter (minimal-diff):** wrap Nix so `Driver.Install(ref)` does a per-ref `nix profile add`, reusing the existing loop untouched. *Rejected:* gives per-**add** atomicity (N generations per run; a mid-run failure leaves a partial multi-generation state), not the whole-**set** atomic switch decision 7 requires, and it shoehorns Nix into `Driver.Install` (decision 3). Smallest diff, wrong invariant.
- **Unified `Realizer` as an optional interface on `driver.Driver`** (discovered by type-assertion like `BatchDetector`): nix.New satisfies `Driver`+`Realizer`; `selectBackend` and `driversFor` stay byte-identical; apply branches on `d.(Realizer)`. *Strong* (idiomatic, structural Windows guard) and documented here as the runner-up — but it forces Nix to implement a **vestigial** `Driver.Install` and couples the `driver` package to Nix-shaped types, both of which decision 3 argues against. The chosen design keeps the whole-set model fully separate at the cost of a sibling selector and `*_realizer.go` files. **If the reviewer prefers the smaller diff over interface purity, this is the swap.**

## Risks / Trade-offs

- **[Risk] Anchor drift across Nix versions** → Mitigation: structural-first classification; anchors only for non-separable classes; locked contract test over real fixtures; unrecognised → `INSTALL_FAILED` + raw detail (a degraded-but-safe moat, never a confidently-wrong code).
- **[Risk] Coarser progress cadence** — one `Realize` over the set means per-item spinners flip together (a long `installing`, then a burst), unlike winget's sequential installs. Contract-legal (no `installing`-per-item is mandated) but a GUI UX delta. *Accepted for v1;* intermediate progress events from internal-json activity are a follow-up.
- **[Risk] Lossy failure attribution** — atomic failure means nothing installed, so all `ToAdd` are `failed`; the offender is named best-effort only. Specced as `failed`-for-all + raw-in-detail, with offender-naming a `MAY`.
- **[Risk] Ref-keying mismatch** — matching a flake ref against `nix profile list --json` element names is heuristic (URL/attr normalization); a mismatch causes a false `missing` → spurious reinstall. Mitigated by idempotency (non-destructive) and a real-output fixture test.
- **[Risk] More new plumbing than the unified alternative** (sibling selector, `newRealizerFn`, three `*_realizer.go`, `driversFor` edit). *Accepted* as the cost of honoring decision 3; each touched original file keeps a minimal early-return diff.
- **[Risk] Windows leak** → Mitigation: `selectBackend` untouched and reached first; the realizer fork is a leading early-return never taken on Windows; the nix package is build-tag-split and never spawned on Windows; `REALIZER_UNAVAILABLE` is an inert const there. Regression tests assert capabilities, selection, and the winget apply path are byte-identical.
