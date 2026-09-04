## Why

Endstate can already provision a Nix-managed Linux profile, but a normal Linux machine is effectively invisible: capture reads the Endstate-owned Nix profile and prior Endstate/Home Manager history rather than discovering the applications and settings already on the machine. That makes the current Linux support a substrate integration, not a complete product journey.

Linux is release-ready only when somebody who has never used Nix can install Endstate, discover supported existing applications and user settings, capture them, and reproduce them through the CLI and GUI without learning Nix. Nix and Home Manager remain first-class realization substrates; Endstate supplies the missing discovery, capture, safety, provenance, verification, and cross-platform UX above them.

## What Changes

- Add read-only discovery for ordinary Linux machines, combining Endstate and user Nix profiles with supported native package inventories, desktop entries, executable evidence, and known configuration paths. Discovery works before Nix is installed.
- Normalize only verified catalog mappings into portable Endstate application identities and Nix installables. Preserve source evidence, report unresolved items, and never guess that similarly named packages are equivalent.
- Make configuration modules platform-aware under one portable application identity. Linux and later Darwin variants can declare their own matchers, paths, capture, restore, verification, secret exclusions, and realization strategy without duplicating or weakening the existing Windows catalog.
- Capture live supported Linux settings even when Endstate or Home Manager did not create them. Use Home Manager for declarative user configuration where the module explicitly supports it, and the existing backed-up/revertible restore lane for opaque application files; never rewrite a user-owned Home Manager configuration graph.
- Derive a frozen Linux settings-adapter registry from the release-pinned Home Manager option metadata, module declarations, and evaluated managed-file targets. Add reviewed reversible codecs or safe file-placement capture on top, and use explicit curation only where the upstream declaration is not invertible.
- Make settings-catalog breadth a parity gate: every existing Endstate application module with an applicable Linux counterpart must be classified as supported or excluded for a concrete platform/safety reason, while safely derivable Home Manager-only applications may take Linux beyond the Windows catalogue. The high-value CLI/developer corpus remains the required depth canary, not the breadth ceiling.
- Add value-scoped GNOME and KDE user preferences. Credentials, histories, caches, databases, and machine-bound state remain excluded.
- Extend scheduled drift checks to Linux through short-lived systemd user timers, preserving the existing opt-in `schedule` CLI and no-resident-agent model so Linux does not ship with a Windows-only recurring-value gap.
- Define the Linux release gate as the complete normal-person journey: discover on a non-Nix-managed machine, select and capture, apply through Nix/Home Manager or the safe restore lane, verify, revert/roll back, and receive useful product-language diagnostics. The CLI contract lands first; `endstate-gui` consumes it in a separate dependent OpenSpec change and ships Linux packaging before any Linux release announcement.
- Keep the schema Darwin-shaped so the same module and discovery contracts can power a later macOS/nix-darwin change. Implementing nix-darwin and shipping the macOS GUI are explicitly outside this Linux change.

## Capabilities

### New Capabilities
- `linux-machine-discovery`: Read-only multi-source discovery, evidence reconciliation, safe package normalization, unresolved-item reporting, and the non-Nix-machine onboarding contract.
- `cross-platform-config-modules`: Portable module identity with explicit OS variants, Home Manager-derived adapter metadata, live settings capture, an applicable-Windows parity matrix, per-variant safety/provenance, realization ownership, and backward-compatible interpretation of the existing Windows catalog.
- `linux-scheduled-drift-check`: Linux systemd-user-timer registration and status under the shared short-lived scheduled verification/capture contract.

### Modified Capabilities
- `nix-package-capture`: Capture imports supported packages found outside the Endstate-managed profile and may attach supported configuration captures; source-manager evidence and portable Nix intent are kept distinct.
- `nix-package-backend`: Release defaults resolve through immutable Nixpkgs input revisions, and apply records the resolution input separately from the portable package attribute.
- `nix-package-version-capture`: Observed installed versions remain inspection evidence; external-manager versions are not misrepresented as Nix pins, and reproducibility comes from immutable installable provenance.
- `nix-home-manager-config`: Engine-generated settings use immutable Nixpkgs/Home Manager inputs and an explicit ownership boundary rather than silently taking over a user-owned Home Manager graph.
- `platform-backend-selection`: Native Linux managers and desktop metadata become read-only discovery sources while Nix remains the canonical Linux package realizer; capture sources are never silently treated as mutating apply backends.
- `engine-backend-bootstrap`: Read-only Linux discovery and capture do not require Nix to be installed; consent-gated Nix bootstrap remains an apply-time operation.
- `config-generation-modules`: Configuration generation selection and fingerprints include the explicit platform variant so one portable module can safely represent different Windows, Linux, and Darwin layouts.
- `config-capture-provenance`: Captures record the platform variant and discovery evidence that selected it, and restore refuses incompatible or fabricated cross-platform provenance.
- `capture-zero-apps-failure`: Capture fails only when it finds no useful selected application or configuration state; a valid settings-only capture is no longer rejected merely because package normalization is unresolved.
- `provisioning-generation`: Generation records expose the portable intent and immutable Nixpkgs/Home Manager inputs that actually resolved and activated Linux state.

## Impact

- Engine discovery/capture orchestration, Nix realizer inventory boundaries, package identity/mapping data, module schema/loading/matching, path resolution, configuration generation/provenance, restore strategy selection, Linux schedule registration, capabilities, envelopes, and event payloads.
- A deterministic, reviewed Home Manager-derived adapter registry; Linux module variants/codecs for applicable Windows parity and additional safe Home Manager coverage; value-scoped GNOME/KDE preferences. Legacy flat modules remain valid and Windows-only unless they explicitly opt into another platform.
- Contract and compatibility documentation must stop presenting substrate support as product parity and must distinguish managed-profile capture from ordinary-machine discovery.
- CI gains distro inventory fixtures, a required real-Nix/Home Manager Linux smoke, and an end-to-end fresh-machine journey that starts without an Endstate-managed Nix profile.
- `endstate-gui` requires a separate OpenSpec change for Linux engine bundling, platform-aware onboarding and selection, consent/error presentation, Tauri packaging, updater/signing, and rendered interaction QA.
- A later `add-macos-product-parity` change will reuse these contracts for Homebrew/Mac App Store discovery, Home Manager/nix-darwin user settings, macOS packaging, and real-Mac validation.
