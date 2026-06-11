> DESIGN-ONLY change. These are decision/scoping tasks, not code tasks. No Go is written, no engine
> behavior ships. "Done" items are the comparison work completed in `design.md`; "open" items are
> the decisions the human makes on review before any implementation proposal is opened.

> CLOSED 2026-06-11: the boundary-first decisions were ratified 2026-06-03 and shipped as the
> `nix-home-manager-secrets-boundary` spec (#112) plus env-exposed `*_FILE` secrets (#115). The
> managed-backend tier (agenix / sops-nix: ciphertext capture, key-bootstrap UX, macOS age-identity
> default) is consciously deferred — recorded in docs/roadmap/roadmap.md. At closure, the one
> ratified-and-implemented requirement the main specs lacked (capture-side redaction) graduates as a
> delta against `nix-home-manager-secrets-boundary`; the managed-backend scenarios from the original
> sketch travel with the deferred roadmap entry, not the spec.

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

- [x] 2.1 **Decide the default backend.** RATIFIED: boundary-first — shipped #112, env tier #115;
      managed backends (agenix / sops-nix) deferred.
- [x] 2.2 **Decide the typed input shape.** RATIFIED: `homeManager.secrets` adopted, as a sibling of
      `Flake`/`Config`/`Settings` on `HomeManagerConfig` — shipped #112.
- [x] 2.3 **Decide the reference surface.** RATIFIED: narrow — `home.file` source references (#112)
      plus env-exposed `*_FILE` path references (#115); raw `programs` flow-through not opened.
- [x] 2.4 **Decide capture handling of ciphertext.** DEFERRED with the managed-backend tier (no
      ciphertext exists in the boundary model) — recorded in roadmap.
- [x] 2.5 **Decide key-bootstrap UX.** DEFERRED with the managed-backend tier — recorded in roadmap.
- [x] 2.6 **Confirm the macOS default** (age-identity path). DEFERRED with the managed-backend tier —
      recorded in roadmap.

## 3. Spec hardening (open — before implementation)

- [x] 3.1 **Spec the no-embed invariant** — graduated as `nix-home-manager-secrets-boundary`
      ("Documented-boundary secrets are referenced, never embedded", sentinel scenario included).
- [x] 3.2 **Spec capture redaction** — graduated at closure via this change's delta against
      `nix-home-manager-secrets-boundary` ("Capture never emits secret material"); implementation is
      reference-only by construction (capture sources the recorded provisioning input).
- [x] 3.3 **Spec the pluggable/declared backend** — graduated as the boundary spec's "Secrets backend
      is explicitly declared and defaults to boundary".
- [x] 3.4 Graduate into an implementation proposal — shipped as `nix-home-manager-secrets` (#112) and
      `nix-home-manager-secrets-env` (#115).

## 4. Non-tasks (explicitly out of scope here)

- [ ] 4.1 (NOT in this change) Any Go / engine / generator / capture implementation.
- [ ] 4.2 (NOT in this change) Adding `sops-nix` / `agenix` flake inputs or crypto dependencies.
- [ ] 4.3 (NOT in this change) Real-nix smoke / round-trip verification — there is nothing to run.
