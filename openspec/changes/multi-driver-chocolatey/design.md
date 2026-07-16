## Context

`driver.Driver` already abstracts per-package detect/install behavior and optional interfaces provide batch detection, version convergence, and uninstall. The command layer is less generic: `selectBackend` returns only Winget on Windows, while Brew has separate apply/verify/capture orchestration and Windows capture directly calls Winget snapshot functions. This change completes the abstraction above those implementations and adds Chocolatey without depending on UniGetUI or changing manifest schema version 1.

## Goals / Non-Goals

**Goals:**

- Make multiple per-package drivers a normal platform capability rather than a command-specific special case.
- Preserve byte-compatible default Winget behavior for manifests without `app.driver`.
- Give Chocolatey the lifecycle capabilities required for apply, verify, pin/repin, capture, rebuild, and best-effort rollback.
- Keep capture deterministic and provenance-preserving while refusing heuristic data loss.
- Make package-manager installation explicit, plan-visible, and suitable for GUI consent.

**Non-Goals:**

- Using UniGetUI as an adapter or runtime dependency.
- Treating Nix realization as a per-package driver.
- Automatic fallback between package managers.
- Managing Chocolatey feeds, credentials, licensed features, or package trust policy.
- Solving fuzzy cross-repository identity globally or adding Scoop in this change.

## Decisions

### Platform backend registry

Introduce a command-layer backend set containing an ordered list/map of named `driver.Driver` instances, an optional default driver, and an optional `realizer.Realizer`. Platform construction is the single source used by capabilities and Windows per-package commands. Commands resolve an omitted app driver to the default, reject unsupported explicit drivers, and partition apps before invoking shared per-driver orchestration.

The first implementation generalizes the Windows Winget/Chocolatey per-package lanes while retaining the shipped Nix+Brew composition and its byte-compatibility tests. Brew is registered for capability reporting and shares neutral driver capabilities, but its proven orchestration is not rewritten merely for symmetry. Nix stays beside the map because its atomic whole-set contract cannot satisfy `Driver.Install` honestly.

### Existing manifest shape remains authoritative

No schema bump is needed. Each app still has one platform ref and an optional driver; `refs.windows` is interpreted by the chosen Windows driver. One app entry selects one package manager. Alternative refs and automatic fallback would make rebuild results depend on runtime catalog availability and are excluded.

### Installed enumeration is an optional driver capability

Add a shared `InstalledEnumerator` interface returning ref, display name, and version. Winget snapshot parsing moves behind the Winget implementation; Brew's existing enumeration adapts to it; Chocolatey parses its local package ledger. Capture iterates the selected driver set and merges normalized results in deterministic driver/ref order.

Chocolatey enumeration records the ledger as reported, including dependency and meta-packages, because the public CLI does not expose a reliable top-level-vs-dependency distinction.

### Driver-aware uniqueness and duplicate warnings

The capture uniqueness key is `(driver, ref)`. Chocolatey and Winget expose no stable public cross-repository identity mapping, so v1 never suppresses one manager's entry because another manager reports a similar identifier, display name, or version. Case-insensitive exact equality of two non-empty display names from different drivers emits `possible_duplicate`, but both entries remain user-editable. Other fuzzy similarity produces no automatic conclusion. Manifest IDs are derived normally and receive a deterministic driver suffix only when two captured entries would otherwise collide.

Manifest update merging uses the same `(driver, ref)` key, with an omitted driver normalized to the platform default. This prevents a Chocolatey entry from overwriting a Winget entry that happens to use the same ref text.

### Chocolatey execution and compatibility

The driver wraps `choco.exe` behind the same injected command seam used by Winget/Brew. It uses non-interactive CLI operations, parses limit-output records, classifies documented success/reboot/failure exits, and never selects or mutates a source. Local enumeration uses current Chocolatey syntax with a version-aware fallback for legacy installations whose local-only flags differ.

Version convergence uses exact-version install for absent packages and Chocolatey's downgrade-capable upgrade path for installed drift. Uninstall omits recursive dependency removal.

### Existing consent-gated backend bootstrap

Chocolatey extends the shipped `--bootstrap-backends` / `--no-bootstrap` abstraction; no second consent flag is added. Preflight resolves selected app drivers before package mutation. With no answered consent, the existing combined consent event is emitted and the unavailable Chocolatey lane is visibly skipped; `--no-bootstrap` skips it without an install attempt. With `--bootstrap-backends`, the bootstrapper invokes Chocolatey's documented official install path, resolves `choco.exe` from PATH or its known installation path, verifies it, then releases the lane. Rebuild propagates the same flags into apply.

### Additive warning and result facts

Command envelopes use an additive `warnings` array of `{code, message, driver?, ref?}` objects for `optional_driver_unavailable` and `possible_duplicate`. Capture also keeps its legacy bundle-metadata `captureWarnings` strings unchanged. Chocolatey reboot-success is carried as `rebootRequired: true` on the driver result, apply item, and item event; it is not represented as a warning or failure and is never inferred from a message. Apply, plan, and verify app items expose the resolved `driver`; non-package verification items omit it.

### Driver-aware module correlation

Module definitions gain optional `matches.chocolatey` refs. Existing `configModuleMap` remains the legacy bare Winget-ref map. A new additive `packageModuleMap` uses namespaced `driver:ref` keys for every manager and arrays of matching module IDs as values, and capture module metadata adds `chocolateyRefs`. This preserves multiple valid module matches while letting a Chocolatey-captured known app retain settings capture/restore without breaking existing GUI consumers.

### Mixed-backend generation and rollback

Each per-package driver writes its own generation with `Backend` equal to its stable driver name. An explicit `rollback --to N` removes additions from every later generation. Bare rollback resolves the newest non-rollback generation's `runId` and removes additions from every generation in that run, so one mixed-driver apply is one rollback unit. Best-effort rollback groups selected generations by recorded backend, processes backend groups in latest-generation order and refs deterministically within each group, and sends each ref only to that backend's `Uninstaller`. Partial failures remain backend-scoped and are aggregated; an unknown or unavailable recorded backend fails only its refs and is never substituted. Chocolatey uninstall never requests dependency removal.

### Driver validation and availability states

Manifest validation recognizes the global driver names `winget`, `chocolatey`, and `brew` case-insensitively. A globally unknown name is a preflight manifest validation error. A globally known driver unsupported on the current host is a visible skipped lane, preserving Brew's shipped cross-platform contract. A supported driver absent with unanswered or denied consent is also a visible skip. If an explicitly consented bootstrap install or verification probe fails, that lane is marked failed with structured diagnostics, the top-level command continues independent lanes, and no fallback driver is attempted.

### Compatibility and rollout

Capabilities add Chocolatey and the new flags additively. Existing manifests, Winget events, and successful capture output remain unchanged when Chocolatey is absent, apart from the explicit optional-driver warning. The implementation lands behind hermetic command seams and requires a maintainer-side mixed Windows smoke before release.

## Risks / Trade-offs

- **Chocolatey packages are arbitrary PowerShell automation and community packages can be less reliable than controlled feeds** → preserve configured sources and package-manager errors; do not market the driver as a trust layer.
- **Winget may recognize software installed by Chocolatey without an ID mapping** → use `(driver, ref)` identity, retain both entries, and warn without suppression.
- **Generalizing command routing can regress mature Winget behavior** → establish registry/partition tests first and keep omitted-driver behavior identical.
- **Bootstrap executes privileged external installation logic** → require explicit flag consent, preflight before mutation, official endpoint only, and post-install verification.
- **Legacy Chocolatey CLI syntax varies** → detect the installed major version and cover both enumeration forms hermetically.
- **The scope crosses several commands** → land capability interfaces/registry first, then driver, then command lanes, capture, bootstrap, and contracts with full-suite gates between stages.

## Migration Plan

1. Add registry and capability interfaces while routing existing Winget/Brew/Nix behavior through them.
2. Add the Chocolatey driver and hermetic tests without advertising it.
3. Add explicit multi-driver command/capture routing and extend the existing backend-bootstrap preflight.
4. Keep capabilities and public contracts synchronized as each additive wire field lands, then run the full engine suite and Windows cross-build.
5. Run real-Windows mixed-manager smoke tests before release. Rollback is code-only: removing Chocolatey registration restores prior behavior without changing existing manifests.

## Open Questions

None. Feed management, automatic fallback, and broader identity mapping are explicitly deferred.
