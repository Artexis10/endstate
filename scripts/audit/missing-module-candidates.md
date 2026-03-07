# Missing Module Candidates

Generated: 2026-03-07

## High-Value Candidates (meaningful portable config)

### PowerToys
- **Winget ID:** `XP89DCGQ3K6VLD` (Microsoft Store)
- **Config:** `%LOCALAPPDATA%\Microsoft\PowerToys\` — extensive per-utility settings (FancyZones layouts, keyboard manager remaps, Run plugins, etc.)
- **Module exists:** Yes (in module catalog) but NOT detected as installed (Store ID not matching winget list format)
- **Action:** Fix the winget match in the existing powertoys module

### Warp Terminal
- **Winget ID:** `Warp.Warp`
- **Config:** Likely `%APPDATA%\Warp\` or `%USERPROFILE%\.warp\` — themes, keybindings, AI config
- **Recommendation:** HIGH value — modern terminal with AI features and extensive config

### Bitwarden
- **Winget ID:** `Bitwarden.Bitwarden`
- **Config:** `%APPDATA%\Bitwarden\` — app settings (NOT vault data, that's server-side)
- **Recommendation:** MEDIUM value — only captures UI preferences, not credentials

### GitHub CLI
- **Winget ID:** `GitHub.cli`
- **Config:** `%APPDATA%\GitHub CLI\config.yml` and `hosts.yml`
- **Recommendation:** MEDIUM value — aliases, editor preference, protocol settings
- **Sensitive:** `hosts.yml` contains auth tokens (exclude from capture, add to sensitive)

### DBeaver
- **Winget ID:** `DBeaver.DBeaver.Community`
- **Config:** `%APPDATA%\DBeaverData\` — connections, SQL templates, preferences
- **Recommendation:** MEDIUM value — connection profiles are very useful to restore
- **Sensitive:** Connection passwords may be stored (check encryption settings)

### digiKam
- **Winget ID:** `KDE.digiKam`
- **Config:** `%LOCALAPPDATA%\digikam\` — database paths, tags, UI preferences
- **Recommendation:** MEDIUM value for photo workflow

### Steam
- **Winget ID:** `Valve.Steam`
- **Config:** `%ProgramFiles(x86)%\Steam\config\` — library folders, login users
- **Recommendation:** LOW value — library folder paths useful, but Steam has its own cloud sync

### yt-dlp
- **Winget ID:** `yt-dlp.yt-dlp`
- **Config:** `%APPDATA%\yt-dlp\config.txt` — download preferences, format selectors
- **Recommendation:** LOW-MEDIUM value — small config file but saves setup time

## Dev Tool Configs (not app-specific modules)

### SSH Config
- **Path:** `%USERPROFILE%\.ssh\config` (97 bytes, EXISTS)
- **Recommendation:** HIGH value for dev workflow
- **CRITICAL:** Never capture keys (`id_rsa`, `id_ed25519`, etc.) — only `config` file
- **Action:** Could be part of a `dev-tools` or `ssh` module

### PowerShell Profiles
- **PS7:** `%USERPROFILE%\Documents\PowerShell\Microsoft.PowerShell_profile.ps1` (156 bytes, EXISTS)
- **PS5:** `%USERPROFILE%\Documents\WindowsPowerShell\Microsoft.PowerShell_profile.ps1` (156 bytes, EXISTS)
- **Content:** Currently only contains OpenSpec completion sourcing
- **Recommendation:** HIGH value — profiles grow over time and are painful to recreate

### Docker CLI Config
- **Path:** `%USERPROFILE%\.docker\config.json` — Docker CLI auth (NOT Docker Desktop)
- **Sensitive:** Contains registry auth tokens
- **Recommendation:** MEDIUM but mostly sensitive

## Low-Priority / No Meaningful Config

| App | Winget ID | Why Low Priority |
|-----|-----------|-----------------|
| CrystalDiskInfo | CrystalDewWorld.CrystalDiskInfo | Minimal user config |
| CrystalDiskMark | CrystalDewWorld.CrystalDiskMark | Benchmark tool, no persistent config |
| IrfanView | IrfanSkiljan.IrfanView | Registry-based config, INI file |
| MKVToolNix | MoritzBunkus.MKVToolNix | Job queue is session-specific |
| MediaInfo | MediaArea.MediaInfo.GUI | Minimal config |
| TreeSize Free | JAMSoftware.TreeSize.Free | No portable config |
| WinRAR | RARLab.WinRAR | Registry-based settings |
| XnConvert | XnSoft.XnConvert | Preset files possible but rarely customized |
| Zoom | Zoom.Zoom | Cloud-synced settings |
| NordVPN | NordSecurity.NordVPN | Auth-only, nothing portable |
| Tailscale | Tailscale.Tailscale | Auth-only, nothing portable |
| Google Drive | Google.GoogleDrive | Auth-only |
| Revo Uninstaller | RevoUninstaller.RevoUninstaller | No portable config |
| ImgBurn | LIGHTNINGUK.ImgBurn | Minimal config |
| SeaTools | Seagate.SeaTools | Diagnostic tool, no config |
| Cryptomator | Cryptomator.Cryptomator | Vault paths only, vaults are on disk |
| Epic Games | EpicGames.EpicGamesLauncher | Auth-only, library is cloud |
| Google Chrome | Google.Chrome.EXE | Profile sync via Google account |
| Microsoft Edge | Microsoft.Edge | Profile sync via MS account |

## Existing Module Bugs Found

### PowerToys
- Module exists in `modules/apps/powertoys/` but shows as "not installed"
- Likely winget ID mismatch: module may use `Microsoft.PowerToys` but actual ID in winget list is the Store ID `XP89DCGQ3K6VLD`
- **Action needed:** Verify and fix winget match

### Notepad++
- Module has a duplicate winget match issue: the ARP ID `Notepad++.Notepad++` appears both in module and as an unmatched app
- Module verify uses hardcoded path `C:\Program Files\Notepad++\notepad++.exe` instead of command-exists
- **Action:** Consider changing verify to `command-exists` with `notepad++`
