# Design — Home-manager env-exposed secrets (Phase 2, `*_FILE` path-reference)

## Framing invariant (unchanged)

**Referenced, never embedded.** The engine NEVER reads, holds, encrypts, or stores secret material.
Phase 1 emitted only a path reference (`home.file.<…>.source = mkOutOfStoreSymlink <path>`). Phase 2
adds a SECOND reference shape that keeps the exact same guarantee: an env var that holds the FILE
PATH, never the value.

## Locked decision: env semantics = `*_FILE` PATH-REFERENCE

An `env` entry ALSO carries a `path`. The engine emits:

```nix
home.sessionVariables.<env> = "<path>";
```

The session variable holds the **file path**, never the secret value — the `*_FILE` convention used
across the ecosystem (`AWS_WEB_IDENTITY_TOKEN_FILE`, `*_PASSWORD_FILE`, agenix/sops file sinks). This
is no-embed BY CONSTRUCTION (same as Phase 1): the only string emitted is the path, and the engine
never `os.ReadFile`s it. It is agenix-forward-compatible — an agenix-decrypted runtime path drops in
unchanged.

Rejected alternative: emitting the secret VALUE into a sessionVariable. That would embed plaintext
into the committable generated tree — the exact anti-pattern the boundary model forbids. There is no
code path from a `HomeManagerSecret` to file content, so it is structurally impossible here.

## Validation (the injection guard runs at load, before any emission)

Rewrite the per-entry reference switch in `validateHomeManagerSecrets`:

- `hasEnv && !hasPath` → `HOMEMANAGER_SECRET_ENV_REQUIRES_PATH` (tell the user to declare the file
  via `path`). Ordered FIRST so the loader (which surfaces `errs[0]`) shows it before the name check.
- `!hasPath && !hasEnv` → `HOMEMANAGER_SECRET_MISSING_REF` (wording widened to "requires a path
  reference (optionally with env)").
- `hasEnv && !envNameRe.MatchString(s.Env)` → `HOMEMANAGER_SECRET_INVALID_ENV_NAME`.

Remove `HOMEMANAGER_SECRET_ENV_UNSUPPORTED`. The env-name regex `^[A-Za-z_][A-Za-z0-9_]*$` is a
package-level `const envNamePattern` + `var envNameRe = regexp.MustCompile(...)`.

**Injection guard.** The compiler emits `env` as a BARE Nix attribute
(`home.sessionVariables.<env> = …`). A crafted name like `x = "evil"; y` would otherwise inject
arbitrary Nix. The load-time regex rejects any such name BEFORE emission — so by the time the
compiler runs, `s.Env` is provably a single safe identifier. This is the load-before-emit ordering
the risk list calls out.

## Compiler

`compileSecretsModule` adds `case s.Env != "":` FIRST (env+path is unambiguously env), emitting
`"home.sessionVariables." + s.Env + " = " + nixString(s.Path) + ";"`. The existing
`case s.Path != "":` (the `home.file` symlink) stays second, so a path-only entry is unchanged.
Determinism (sort by name) and the no-embed guarantee (never `os.ReadFile`) are preserved.

## Capture (no production change)

`apply` records the declared `homeManager.secrets` on the provisioning generation
(`HomeGenRef.Secrets`) verbatim, and `recoverHomeManager` carries the whole `HomeManagerSecret`
(`Name`/`Path`/`Env`/`Backend`) into the captured manifest. An env+path secret therefore round-trips
with NO code change; a test proves `Env` and `Path` are carried verbatim and that no material leaks.

## Risks

- **Nix-attr injection via env name** — mitigated: the regex runs at load, before any emission. A
  crafted name is rejected at validate time.
- **`sessionVariables` key collision** — the catalog settings emit a FLAT
  `home.sessionVariables = { … }` while secrets emit DOTTED `home.sessionVariables.<X> = …`. Nix
  merges these into one attrset, so the SAME variable declared in both a catalog setting and a
  secret is a Nix evaluation conflict (`attribute already defined`). This is user-controlled and
  surfaces as an activation error, not a silent override. We DOCUMENT it here and do NOT add a
  cross-module pre-check this change (it would couple the secrets compiler to the catalog compiler;
  defer to a future phase if it proves a real footgun).
- **OS-robust assertions** — paths are asserted via `nixString`-encoded expected strings so a
  Windows backslash path does not cause a spurious substring-match failure (the keystone test
  pattern).

## Relationship to the scope change

`nix-home-manager-secrets-scope` is design-only and owns the backend-agnostic capability
`nix-home-manager-secrets`. This change extends the IMPLEMENTED capability
`nix-home-manager-secrets-boundary` (PR #112) and does NOT touch the scope change, so the two do not
collide under `openspec validate --all --strict`.
