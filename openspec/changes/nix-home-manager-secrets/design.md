# Design — Home-manager secrets, Phase-1 documented boundary

## Framing invariant

**Referenced, never embedded.** The engine NEVER reads, holds, encrypts, or stores secret
material. It emits only a Nix *reference* to where the secret is expected at activation: a file
path the user provisions out-of-band, or an environment variable name the user supplies. This is
the "documented boundary": the engine documents (in the generated, inspectable artifact) where the
secret goes, and the user owns the material on the other side of that boundary.

## Locked decisions (ratified)

1. **Boundary-only backend.** No agenix/sops in Phase 1. A `backend` field exists and defaults to
   `"boundary"`; any other value is rejected at load (fail loud, never degrade to embedding).
2. **`homeManager.secrets` is a sibling** of `Flake`/`Config`/`Settings` on `HomeManagerConfig`. It
   composes with the engine-generated modes (`settings`/`config`); it is NOT part of their mutual
   exclusivity. **Phase 1 is PATH-ONLY**: each entry declares a `path`; the `Env` field exists for a
   future phase but is rejected at load (the boundary model can't set an env var it has no value for).
3. **The reference sink.** `path` → `home.file.<homeRelTarget(name)>.source =
   config.lib.file.mkOutOfStoreSymlink <path>;` — an out-of-store symlink resolved at activation
   (verified by real-nix smoke: no `/nix/store` import, pure-eval-safe). NOT the raw
   `programs`/`files` passthrough.
4. **Capture = reference only**, never the material.
5. **Detect-and-warn on a missing key.** Phase 1 = load-time validation + don't swallow activation
   errors. The engine never generates or stores a key.
6. **macOS default age-identity.** No Phase-1 code impact; noted here for the future managed
   backend.

## The structural keystone (no-embed BY CONSTRUCTION)

The catalog `files` map is the anti-pattern to NOT replicate: `CompileHomeNix` `os.ReadFile`s each
`files` source at compile time and puts the content into the `staged map[string][]byte` that
`writeHomeFlake` materializes beside the flake. A secret routed through that path would embed
plaintext.

A secret entry therefore MUST NEVER:

- enter the `staged map[string][]byte`, and
- be `os.ReadFile`'d.

`compileSecretsModule(secrets)` builds the `secrets.nix` module purely from the manifest's
`name`/`path`/`env` strings. There is **no code path from a `HomeManagerSecret` to file content**.
No-embed is thus true by construction, not by a runtime check. A test asserts this structurally: a
sentinel written at a secret's `path` is absent from every byte of the generated tree.

## Why a separate `secrets.nix` module

Secrets must compose with BOTH `settings` (engine-compiled `home.nix`) and `config` (the user's
own `home.nix`, copied in verbatim). Appending statements into the user's `home.nix` is fragile
(we cannot safely splice into an arbitrary user attrset) and into the compiled `home.nix` is
needless coupling. Instead the engine stages a second module `./secrets.nix` and adds it to the
generated flake's `modules` list. This wires uniformly for both modes and leaves the user's
`home.nix` untouched. The path reference is emitted as a Nix string (home-manager coerces a string
`source` to a path at activation), which keeps evaluation pure (no absolute-path import of a tree
outside the flake at eval time) and is unambiguously a reference, never content.

## Capture round-trip

`apply` records the declared `homeManager.secrets` on the provisioning generation
(`HomeGenRef.Secrets`) — references only, since `HomeManagerSecret` holds no material. `capture`'s
`recoverHomeManager` carries those references through alongside the recovered `settings`/`config`.
The captured manifest therefore re-activates the same reference wiring on apply, and the
apply↔capture loop is not a leak path (a sentinel at a secret's content location never reaches the
captured manifest).

## Flake mode rejection

A pure `homeManager.flake` is the user's external flake; the engine generates nothing to inject a
`secrets.nix` module into. Combining `homeManager.secrets` with `flake` is rejected at load with a
clear error directing the user to `settings`/`config` (or to manage secrets inside their own
flake).

## Relationship to the scope change

`nix-home-manager-secrets-scope` is design-only and owns capability `nix-home-manager-secrets`
(backend-agnostic invariants). This implementation owns a distinct capability,
`nix-home-manager-secrets-boundary`, stating the concrete Phase-1 boundary behavior. The two
capabilities are complementary, not conflicting; the scope change is not archived by this change.
