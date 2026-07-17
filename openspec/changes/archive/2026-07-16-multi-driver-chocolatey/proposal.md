## Why

Endstate's engine exposes a package-manager `Driver` interface, but its Windows command and capture paths still assume a single Winget backend while macOS adds Brew through a dedicated special lane. Windows users need Chocolatey's complementary catalog without sacrificing deterministic manifests, headless rebuilds, or direct engine control.

## What Changes

- Generalize backend selection into a platform registry with one optional whole-set realizer, named per-package drivers, and an optional default driver.
- Add Chocolatey as an additive Windows driver with detect, install, exact-version install/repin, uninstall, and installed-package enumeration capabilities.
- Route manifest apps by explicit `app.driver`; preserve Winget as the Windows default when the field is omitted and never silently fall back between drivers.
- Generalize capture so installed-package drivers enumerate their own packages, preserve driver provenance, and suppress only duplicates proven by authoritative identity.
- Extend the existing consent-gated backend bootstrap flow so a missing Chocolatey backend can be installed through the shipped `--bootstrap-backends` / `--no-bootstrap` surface.
- Advertise every supported platform backend and expose additive machine-readable duplicate/unavailable-driver warnings plus an explicit reboot-required success fact.

## Capabilities

### New Capabilities

- `multi-driver-package-management`: Platform backend registration, explicit/default app routing, Chocolatey lifecycle parity, multi-driver capture, conservative deduplication, and consent-gated package-manager bootstrap.

### Modified Capabilities

- `platform-backend-selection`: Windows exposes an additive Chocolatey driver while Winget remains the default.
- `engine-backend-bootstrap`: the existing generic consent flow gains the official Chocolatey bootstrap on Windows; Winget remains operating-system provided.
- `version-drift-enforcement`: Chocolatey joins Winget as a per-package backend that supports drift detection and confirmed convergence.
- `windows-version-capture-pinning`: installed-version recording and exact pins apply to Chocolatey as well as Winget.
- `provisioning-generation`: mixed per-package drivers write backend-scoped generations and rollback dispatches by recorded backend.
- `macos-brew-apply-wiring`: capture uniqueness becomes driver-aware and no longer discards cross-backend identifier collisions without proof.

## Impact

- Engine driver and command-routing packages, Windows capture, apply/verify/plan/rollback/rebuild orchestration, capabilities output, JSON envelopes, and streaming events.
- Manifest schema remains version 1 and reuses the existing optional `app.driver` field and platform-keyed refs.
- No UniGetUI dependency is introduced; Endstate continues to invoke package-manager CLIs directly.
- Chocolatey uses only sources already configured on the machine; feed provisioning and credentials remain out of scope.
