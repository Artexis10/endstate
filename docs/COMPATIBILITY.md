# Compatibility Matrix

## Platform Support

Endstate provisions all three desktop platforms. Windows uses Winget by default and supports explicit
Chocolatey package lanes; Linux and macOS use
the Nix realizer (packages + home-manager configuration), with an additional per-app Homebrew driver
lane on macOS (`driver: "brew"` opt-in; `cask:` darwin references auto-route to brew).

| Capability | Windows (Winget + Chocolatey) | Linux (Nix) | macOS (Nix + brew lane) |
|------------|------------------|-------------|-------------------------|
| Apply (packages) | ✅ | ✅ atomic generation | ✅ atomic generation + best-effort brew lane |
| Configuration (apply) | ✅ config modules | ✅ home-manager (`settings`/`config`/`flake`) | ✅ home-manager (`settings`/`config`/`flake`) |
| Capture | ✅ | ✅ packages + home-manager config | ✅ packages, home-manager config, brew formulae/casks |
| Verify | ✅ | ✅ packages + config presence/drift | ✅ + brew presence (versions advisory) |
| Rollback | ✅ best-effort uninstall | ✅ native generation + config rollback | ✅ native + config + best-effort brew |
| Version pinning | ✅ strict | ✅ pinned installables | ✅ Nix pinned; brew advisory |
| Secrets (boundary) | — | ✅ referenced, never embedded | ✅ referenced, never embedded |
| Backend bootstrap | ✅ Winget ships with the OS; Chocolatey is consent-gated | ✅ consent-gated Nix install | ✅ consent-gated Nix / Homebrew install |

CI: hermetic Go tests run on windows/macos/ubuntu; real-Nix integration smokes run on macOS and
Linux; a real-Homebrew smoke runs on macOS. Deferred platform scope is recorded in
[the roadmap](roadmap/roadmap.md) §6.

Distribution: GitHub Releases ship `endstate.exe` for Windows plus per-platform Unix binaries
(`endstate-linux-amd64`, `endstate-linux-arm64`, `endstate-darwin-amd64`, `endstate-darwin-arm64`),
each with a `.sha256` checksum. `endstate bootstrap` self-installs the running binary on all three
platforms: on Windows it installs to `%LOCALAPPDATA%\Endstate\bin` with a `.cmd` shim and a PATH
entry; on Linux/macOS it installs to `${XDG_DATA_HOME:-$HOME/.local/share}/endstate/bin` and creates
a `$HOME/.local/bin/endstate` symlink (no shell PATH is edited).

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
