## Why

The Phase-1 documented-boundary secrets implementation (PR #112) shipped PATH-ONLY references: a
`homeManager.secrets` entry declares a `path` and the engine emits
`home.file.<homeRelTarget>.source = config.lib.file.mkOutOfStoreSymlink <path>;` — a reference,
never the material. The `Env` field already exists on `HomeManagerSecret` but is **rejected at
load** (`HOMEMANAGER_SECRET_ENV_UNSUPPORTED`), because the boundary model never holds a secret's
value and so could not meaningfully set an env var.

Many real secrets are consumed as an **environment variable that points at a file** — the
`*_FILE` convention (`AWS_WEB_IDENTITY_TOKEN_FILE`, `*_PASSWORD_FILE`, agenix/sops sinks). That
shape fits the boundary model exactly: the variable holds the **file path**, the consumer reads the
file. This change ships Phase 2 — env-exposed secrets as a **path reference** — closing the
deferred half of the secrets arc without ever embedding a value.

## What Changes

- **`env` becomes valid WHEN combined with `path`.** An `env` entry now ALSO carries a `path`. The
  engine emits `home.sessionVariables.<env> = "<path>";` — referencing the FILE PATH, never the
  value (no-embed by construction, mirroring the Phase-1 path-only guarantee). This is the `*_FILE`
  path-reference convention and is agenix-forward-compatible (an agenix-decrypted path drops in).
- **Validation rewrite.** `env` REQUIRES `path` → reject `env`-without-`path`
  (`HOMEMANAGER_SECRET_ENV_REQUIRES_PATH`); an `env` name must match
  `^[A-Za-z_][A-Za-z0-9_]*$` → reject otherwise (`HOMEMANAGER_SECRET_INVALID_ENV_NAME`, which also
  blocks Nix-attr injection since the compiler emits `env` as a bare attribute); neither `path` nor
  `env` → keep `HOMEMANAGER_SECRET_MISSING_REF`. The old `HOMEMANAGER_SECRET_ENV_UNSUPPORTED`
  rejection is REMOVED. Net: path-only ✓, env+path ✓ (NEW), env-only ✗, neither ✗.
- **Compiler.** `compileSecretsModule` gains a `case s.Env != "":` (checked first, since env+path is
  unambiguously an env entry) emitting the sessionVariable; the path-only `home.file` case is
  unchanged. Output stays sorted (deterministic), and a secret is still NEVER `os.ReadFile`'d
  (no-embed preserved).
- **Capture = reference only (no production change).** `recoverHomeManager` and
  `HomeGenRef.Secrets` already carry the whole `HomeManagerSecret` (including `Env`) verbatim, so an
  env+path secret round-trips through capture with no code change — proven by a round-trip test.

## Capabilities

### New Capabilities

- None. This change extends the existing Phase-1 capability `nix-home-manager-secrets-boundary` with
  the env-exposed (path-reference) shape; it does not introduce a new capability.

### Modified Capabilities

- `nix-home-manager-secrets-boundary`: the documented-boundary backend now accepts an env-exposed
  secret that references a file path. The "referenced, never embedded" requirement drops its
  "Phase 1 is path-only; env deferred" parenthetical, and the backend requirement un-rejects
  env+path while adding reject-env-without-path and reject-invalid-env-name scenarios. A new
  requirement states that an env-exposed secret references the file path, never the value.

## Impact

- `go-engine/internal/manifest/validator.go` — load-time validation rewrite (`regexp` import,
  `envNamePattern`, env-requires-path / invalid-env-name; remove `..._ENV_UNSUPPORTED`).
- `go-engine/internal/manifest/types.go` — `HomeManagerSecret` doc-comment (truthful env
  path-reference semantics).
- `go-engine/internal/realizer/nix/home_secrets.go` — `compileSecretsModule` env case + doc-comment.
- `go-engine/internal/manifest/home_secrets_test.go`,
  `go-engine/internal/realizer/nix/home_secrets_test.go`,
  `go-engine/internal/commands/capture_realizer_test.go` — tests.
- No schema change (`Env` already exists); no capture production change.
