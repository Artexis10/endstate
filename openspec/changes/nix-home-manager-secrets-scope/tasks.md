> DESIGN-ONLY change. These are decision/scoping tasks, not code tasks. No Go is written, no engine
> behavior ships. "Done" items are the comparison work completed in `design.md`; "open" items are
> the decisions the human makes on review before any implementation proposal is opened.

## 1. Comparison (done — captured in design.md)

- [x] 1.1 Establish the governing invariant: **secrets are referenced, never embedded** in the
      generated flake/home.nix/staged files.
- [x] 1.2 Compare sops-nix vs agenix vs documented-boundary across: generated-flake fit, engine-
      generated vs user-owned split, inspectability/auditability, dependency cost, cross-OS
      (Linux/macOS), and capture/round-trip safety (the 3-way table).
- [x] 1.3 Produce a recommendation with rationale tied to invisible-but-inspectable
      (boundary now → agenix later → sops-nix on demonstrated need).
- [x] 1.4 Sketch the delta-spec requirements (no-embed; capture-no-material; optional pluggable
      backend) in `specs/nix-home-manager-secrets/spec.md`.
- [x] 1.5 Define the phased path (Phase 1 boundary, Phase 2 agenix, Phase 3 optional sops-nix).

## 2. Decisions (open — the human ratifies)

- [ ] 2.1 **Decide the default backend.** Confirm boundary-first, or choose to ship a managed
      backend (agenix / sops-nix) in Phase 1.
- [ ] 2.2 **Decide the typed input shape.** Whether `homeManager.secrets` is adopted, its schema
      (`{name, path|env, backend?, source?}`), and whether it sits under `settings` or as a sibling
      of `Flake`/`Config`/`Settings` on `HomeManagerConfig`.
- [ ] 2.3 **Decide the reference surface.** Which `home.*` targets a secret reference may flow into
      (narrow: `home.file` source + `home.sessionVariables`; or broader incl. raw `programs`).
- [ ] 2.4 **Decide capture handling of ciphertext** (managed backends): record the ciphertext file
      *path* only, or also copy the (safe-at-rest) ciphertext into the captured bundle.
- [ ] 2.5 **Decide key-bootstrap UX** for a managed backend: fail-fast warn on a missing user-owned
      identity at activation, or stay silent (pure boundary).
- [ ] 2.6 **Confirm the macOS default** (age-identity path, not SSH host keys) for any managed
      backend.

## 3. Spec hardening (open — before implementation)

- [ ] 3.1 **Spec the no-embed invariant** as a testable requirement (no secret material in
      `flake.nix` / `home.nix` / staged files for any input).
- [ ] 3.2 **Spec capture redaction** as a testable requirement (a captured manifest from a
      secrets-bearing machine references the source, never the plaintext).
- [ ] 3.3 If a typed input is adopted, **spec the pluggable/declared backend** (backend named in the
      manifest, not inferred).
- [ ] 3.4 Graduate the ratified subset of `specs/nix-home-manager-secrets/spec.md` into an
      implementation proposal (separate, non-design-only change).

## 4. Non-tasks (explicitly out of scope here)

- [ ] 4.1 (NOT in this change) Any Go / engine / generator / capture implementation.
- [ ] 4.2 (NOT in this change) Adding `sops-nix` / `agenix` flake inputs or crypto dependencies.
- [ ] 4.3 (NOT in this change) Real-nix smoke / round-trip verification — there is nothing to run.
