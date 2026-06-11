# Compatibility Matrix

## Platform Support

Endstate provisions all three desktop platforms. Windows uses the winget driver; Linux and macOS use
the Nix realizer (packages + home-manager configuration), with an additional per-app Homebrew driver
lane on macOS (`driver: "brew"` opt-in; `cask:` darwin references auto-route to brew).

| Capability | Windows (winget) | Linux (Nix) | macOS (Nix + brew lane) |
|------------|------------------|-------------|-------------------------|
| Apply (packages) | ✅ | ✅ atomic generation | ✅ atomic generation + best-effort brew lane |
| Configuration (apply) | ✅ config modules | ✅ home-manager (`settings`/`config`/`flake`) | ✅ home-manager (`settings`/`config`/`flake`) |
| Capture | ✅ | ✅ packages + home-manager config | ✅ packages, home-manager config, brew formulae/casks |
| Verify | ✅ | ✅ packages + config presence/drift | ✅ + brew presence (versions advisory) |
| Rollback | ✅ best-effort uninstall | ✅ native generation + config rollback | ✅ native + config + best-effort brew |
| Version pinning | ✅ strict | ✅ pinned installables | ✅ Nix pinned; brew advisory |
| Secrets (boundary) | — | ✅ referenced, never embedded | ✅ referenced, never embedded |
| Backend bootstrap | n/a (winget ships with the OS) | ✅ consent-gated Nix install | ✅ consent-gated Nix / Homebrew install |

CI: hermetic Go tests run on windows/macos/ubuntu; real-Nix integration smokes run on macOS and
Linux; a real-Homebrew smoke runs on macOS. Deferred platform scope is recorded in
[the roadmap](roadmap/roadmap.md) §6.

## Engine CLI <-> Schema Version

| CLI Version | Schema Version | Notes |
|-------------|---------------|-------|
| 0.1.x       | 1.0           | Initial release |

## Schema Version Changes

- **Schema major bump** -> forces CLI major bump (breaking change)
- **Schema minor bump** -> backwards-compatible schema addition

## GUI Compatibility

| GUI Version | Engine Schema | Notes |
|-------------|--------------|-------|
| 0.1.x       | 1.0          | Initial release |
