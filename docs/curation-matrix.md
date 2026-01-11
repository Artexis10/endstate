# Endstate Curation Matrix

> **Core Principle**: Endstate ONLY curates applications where restoring local state/configuration provides HIGH, irreplaceable user value. Apps with weak or replaceable state are excluded, even if popular.

**Target**: 30–50 apps MAX | Opinionated, defensible, premium list | Professionals, power users, serious hobbyists

---

## Assessment Criteria

| Criterion | Definition |
|-----------|------------|
| **Local State Depth** | Volume and complexity of local configuration, data, and customization |
| **Cloud Gap** | How much state is NOT reliably restored via accounts/cloud sync |
| **User Pain** | Impact of losing this state (time to recreate, institutional knowledge, irreplaceable work) |

**Scoring**: High (H) / Medium (M) / Low (L)

**Inclusion Threshold**: Must score H on at least 2 of 3 criteria, with no L scores.

---

## Curation Matrix by Field

### 1. Development

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| Code Editor | **VS Code** | H | M | H | 1 |
| Code Editor | **VSCodium** | H | H | H | 1 |
| Code Editor | **Cursor** | H | H | H | 1 |
| Code Editor | **Windsurf** | H | H | H | 1 |
| Version Control | **Git** | H | H | H | 1 |
| JetBrains IDE | **IntelliJ IDEA** | H | M | H | 1 |
| JetBrains IDE | **PyCharm** | H | M | H | 2 |
| JetBrains IDE | **WebStorm** | H | M | H | 2 |
| Text Editor | **Sublime Text** | H | H | M | 2 |
| Text Editor | **Notepad++** | M | H | M | 2 |
| Terminal | **Windows Terminal** | M | H | M | 2 |

**State Examples**:
- **VS Code family**: `settings.json`, `keybindings.json`, extensions list, workspace configs, snippets, launch.json, tasks.json, theme customizations
- **Git**: `.gitconfig`, credential helpers, aliases, hooks templates, ignore patterns, GPG signing config
- **JetBrains**: IDE settings, keymaps, live templates, database connections, run configurations, code style schemes, plugins

---

### 2. Creative — Photography

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| RAW Workflow | **Lightroom Classic** | H | H | H | 1 |
| RAW Workflow | **Capture One** | H | H | H | 1 |
| RAW Viewer | **FastRawViewer** | M | H | M | 2 |
| Photo Editor | **Affinity Photo** | H | H | M | 2 |
| RAW Processor | **DxO PhotoLab** | H | H | H | 1 |

**State Examples**:
- **Lightroom Classic**: Catalogs (years of work!), develop presets, export presets, keyword hierarchies, import presets, identity plates, watermarks, print/book templates
- **Capture One**: Sessions, styles, presets, keyboard shortcuts, workspace layouts, process recipes
- **DxO PhotoLab**: Presets, database, film pack settings, optical corrections

**Critical Insight**: Photography apps have EXTREMELY high restore value. Catalogs represent years of curation. Presets encode professional color science. Cloud sync is minimal or nonexistent.

---

### 3. Creative — Video

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| NLE / Color | **DaVinci Resolve** | H | H | H | 1 |
| Streaming | **OBS Studio** | H | H | H | 1 |
| NLE | **Premiere Pro** | H | M | H | 2 |
| Motion Graphics | **After Effects** | H | M | H | 2 |

**State Examples**:
- **DaVinci Resolve**: Project databases, PowerGrades, LUTs, effects presets, keyboard layouts, render presets, Fusion macros
- **OBS Studio**: Scenes, sources, profiles, output settings, hotkeys, plugin configs, streaming service connections

---

### 4. Creative — Audio

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| DAW | **Reaper** | H | H | H | 1 |
| DAW | **FL Studio** | H | H | H | 2 |
| DAW | **Ableton Live** | H | M | H | 2 |
| Audio Editor | **Audacity** | M | H | M | 2 |
| Audiophile Player | **foobar2000** | H | H | M | 2 |

**State Examples**:
- **Reaper**: Actions, scripts, themes, track templates, FX chains, menus, toolbars, project templates
- **FL Studio**: Plugin database, mixer states, templates, MIDI mappings
- **foobar2000**: Layouts, components, playlists, DSP chains, media library

---

### 5. Browsers

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| Primary Browser | **Chrome** | H | M | H | 1 |
| Primary Browser | **Firefox** | H | M | H | 1 |
| Privacy Browser | **Brave** | H | H | H | 1 |

**State Examples**:
- **All browsers**: Bookmarks, extensions + extension settings, site permissions, cookies, saved passwords, autofill, custom search engines, tab groups, pinned tabs
- **Firefox specific**: `about:config` tweaks, container tabs, privacy settings
- **Brave specific**: Shields settings per-site, Brave Wallet (!), Rewards, fingerprinting protection levels

**Cloud Gap Analysis**: Browser sync exists BUT:
- Extension settings often don't sync
- Site-specific permissions don't sync
- `about:config` / flags don't sync
- Sync can corrupt/conflict
- Brave Wallet is LOCAL ONLY (critical!)

---

### 6. Productivity

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| Knowledge Base | **Obsidian** | H | H | H | 1 |
| Knowledge Base | **Logseq** | H | H | H | 2 |
| File Manager | **Total Commander** | H | H | H | 1 |
| File Manager | **Directory Opus** | H | H | H | 2 |

**State Examples**:
- **Obsidian**: Vault settings, plugins (many community!), themes, hotkeys, graph settings, workspace layouts, core plugin configs
- **Total Commander**: Button bar, directory hotlist, custom columns, viewer associations, FTP connections, internal associations, color schemes

**Exclusions**:
- **Notion**: 100% cloud — no local state value
- **OneNote**: Cloud-synced
- **Evernote**: Cloud-synced

---

### 7. Power User / System

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| System Utilities | **PowerToys** | H | H | H | 1 |
| Screenshots | **ShareX** | H | H | H | 1 |
| Automation | **AutoHotkey** | H | H | H | 1 |
| System Monitor | **HWiNFO** | M | H | M | 2 |
| Clipboard Manager | **Ditto** | M | H | M | 2 |

**State Examples**:
- **PowerToys**: FancyZones layouts (!!), Keyboard Manager remaps, PowerRename presets, Run aliases, Awake settings, Color Picker formats
- **ShareX**: Workflows, capture regions, annotation settings, upload destinations, hotkeys, after-capture tasks
- **AutoHotkey**: User scripts (entire automation library)

---

### 8. Hardware / Performance

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| GPU Overclocking | **MSI Afterburner** | H | H | H | 1 |
| GPU Overclocking | **EVGA Precision X1** | H | H | H | 2 |
| RGB / Peripherals | **OpenRGB** | H | H | M | 2 |

**State Examples**:
- **MSI Afterburner**: OC profiles (per-game!), fan curves, voltage curves, monitoring layouts, OSD settings, RivaTuner integration

**Exclusions**:
- **Razer Synapse**: Cloud-synced
- **Logitech G Hub**: Cloud-synced
- **Corsair iCUE**: Cloud-synced (mostly)

---

### 9. Media

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| Media Server | **Plex** | H | M | H | 2 |
| Media Center | **Kodi** | H | H | H | 2 |
| Media Player | **VLC** | M | H | L | Discard |
| Media Player | **mpv** | M | H | M | 2 |

**State Examples**:
- **Plex**: Server settings, library paths, transcoding settings, user accounts, watch states (partially cloud)
- **Kodi**: Add-ons, skins, library database, sources, advanced settings

---

### 10. Security / Privacy

| Capability | App | State Depth | Cloud Gap | User Pain | Tier |
|------------|-----|-------------|-----------|-----------|------|
| Password Manager | **KeePassXC** | M | H | M | 2 |
| VPN | **WireGuard** | M | H | M | 2 |

**Note**: Password databases are user responsibility. Endstate captures app configuration, not vault contents.

---

## Tiered Summary

### Tier 1: Immediate Curation (24 Apps)

| # | App | Field | Primary Restore Value |
|---|-----|-------|----------------------|
| 1 | Git | Development | `.gitconfig`, aliases, credentials, hooks |
| 2 | VS Code | Development | Settings, extensions, snippets, workspaces |
| 3 | VSCodium | Development | Same as VS Code, no built-in sync |
| 4 | Cursor | Development | AI IDE settings, no cloud sync |
| 5 | Windsurf | Development | AI IDE settings, no cloud sync |
| 6 | IntelliJ IDEA | Development | IDE settings, keymaps, run configs |
| 7 | Lightroom Classic | Photography | Catalogs, presets, keywords |
| 8 | Capture One | Photography | Sessions, styles, process recipes |
| 9 | DxO PhotoLab | Photography | Presets, database, corrections |
| 10 | DaVinci Resolve | Video | Projects, LUTs, PowerGrades |
| 11 | OBS Studio | Video/Streaming | Scenes, sources, profiles |
| 12 | Reaper | Audio | Actions, FX chains, templates |
| 13 | Chrome | Browser | Extensions, settings, bookmarks |
| 14 | Firefox | Browser | about:config, containers, extensions |
| 15 | Brave | Browser | Wallet (!), Shields, extensions |
| 16 | Obsidian | Productivity | Plugins, themes, vault settings |
| 17 | Total Commander | Productivity | Button bar, hotlist, associations |
| 18 | PowerToys | Power User | FancyZones, Keyboard Manager |
| 19 | ShareX | Power User | Workflows, capture settings |
| 20 | AutoHotkey | Power User | User script library |
| 21 | MSI Afterburner | Hardware | OC profiles, fan curves |
| 22 | Windows Terminal | Development | Profiles, settings, themes |
| 23 | Notepad++ | Development | Sessions, plugins, macros |
| 24 | Sublime Text | Development | Packages, settings, key bindings |

### Tier 2: Later Curation (18 Apps)

| # | App | Field | Reason for Deferral |
|---|-----|-------|---------------------|
| 1 | PyCharm | Development | Same engine as IntelliJ, lower priority |
| 2 | WebStorm | Development | Same engine as IntelliJ, lower priority |
| 3 | FastRawViewer | Photography | Niche, medium state depth |
| 4 | Affinity Photo | Photography | Lower user base than Adobe |
| 5 | Premiere Pro | Video | Adobe has some cloud sync |
| 6 | After Effects | Video | Adobe has some cloud sync |
| 7 | FL Studio | Audio | High value but niche |
| 8 | Ableton Live | Audio | Has some cloud sync |
| 9 | Audacity | Audio | Simpler config surface |
| 10 | foobar2000 | Media | Niche audiophile tool |
| 11 | Logseq | Productivity | Smaller than Obsidian |
| 12 | Directory Opus | Productivity | Smaller than Total Commander |
| 13 | HWiNFO | Power User | Medium state depth |
| 14 | Ditto | Power User | Medium state depth |
| 15 | EVGA Precision X1 | Hardware | Similar to Afterburner |
| 16 | OpenRGB | Hardware | Niche, medium pain |
| 17 | Plex | Media | Some cloud sync exists |
| 18 | Kodi | Media | Niche power user tool |
| 19 | KeePassXC | Security | Config only, not vaults |
| 20 | WireGuard | Security | Simple config surface |
| 21 | mpv | Media | Config-file based, niche |

### Discarded: Weak Restore Energy

| App | Reason |
|-----|--------|
| **Notion** | 100% cloud-based, zero local state |
| **Slack** | 100% cloud-based |
| **Discord** | 100% cloud-based (minimal local settings) |
| **Microsoft Teams** | 100% cloud-based |
| **Spotify** | Cloud streaming, minimal local config |
| **1Password** | Cloud-synced |
| **Bitwarden** | Cloud-synced |
| **Dropbox** | Sync client, no meaningful local state |
| **OneDrive** | Sync client, minimal config |
| **VLC** | Low user pain — trivial to reconfigure |
| **7-Zip** | Virtually no configuration |
| **WinRAR** | Virtually no configuration |
| **Razer Synapse** | Cloud-synced profiles |
| **Logitech G Hub** | Cloud-synced profiles |
| **Corsair iCUE** | Cloud-synced profiles |
| **Steam** | Cloud saves per-game, client is trivial |
| **Epic Games** | Minimal launcher config |
| **Adobe Creative Cloud** | Launcher only; apps handled individually |

---

## Module Generation Strategy

### Auto-Generation Pipeline

```
┌─────────────────┐     ┌──────────────────┐     ┌─────────────────┐
│  Sandbox        │────▶│  Discovery       │────▶│  Module Draft   │
│  Install        │     │  Diff Engine     │     │  Generator      │
└─────────────────┘     └──────────────────┘     └─────────────────┘
                                                         │
                                                         ▼
                                                 ┌─────────────────┐
                                                 │  Human Review   │
                                                 │  + Enrichment   │
                                                 └─────────────────┘
```

### How Sandbox Curation Enriches Modules

| Discovery Phase | Auto-Generated | Human Enrichment |
|-----------------|----------------|------------------|
| **File paths** | ✓ Detected | Categorize: config vs cache vs data |
| **Registry keys** | ✓ Detected | Identify volatile vs persistent |
| **File patterns** | ✓ Heuristic exclude | Confirm/refine exclude patterns |
| **Portability** | ✗ Unknown | Mark portable vs machine-specific |
| **Restore order** | ✗ Unknown | Define dependencies, pre/post hooks |
| **Sensitive data** | ✗ Unknown | Flag credentials, tokens, keys |

### Sandbox-Curation Workflow

1. **Install in Sandbox** → Capture pre/post snapshots
2. **Diff Analysis** → Identify touched paths
3. **Heuristic Filtering** → Remove temp, cache, logs
4. **Generate Draft Module** → Skeleton with paths
5. **Human Review**:
   - Confirm file categories
   - Add restorer strategies
   - Define portability rules
   - Mark sensitive paths
   - Add verification steps

---

## Module Metadata Schema

A curated module MUST include:

```jsonc
{
  // === IDENTITY ===
  "id": "git",
  "displayName": "Git",
  "version": "1.0.0",
  "wingetId": "Git.Git",  // optional, for install verification
  
  // === CURATION METADATA ===
  "curation": {
    "tier": 1,
    "field": "Development",
    "capability": "Version Control",
    "restoreValue": "High",
    "cloudGap": "High",
    "userPain": "High",
    "curator": "human",  // "auto" | "human" | "hybrid"
    "lastReviewed": "2025-01-10"
  },
  
  // === CAPTURE TARGETS ===
  "capture": {
    "files": [
      {
        "path": "%USERPROFILE%\\.gitconfig",
        "category": "config",
        "portable": true,
        "sensitive": false
      },
      {
        "path": "%USERPROFILE%\\.git-credentials",
        "category": "credentials",
        "portable": false,
        "sensitive": true,
        "restorer": "warn-only"  // don't auto-restore, warn user
      }
    ],
    "registry": [],
    "excludePatterns": [
      "*.lock",
      "*.tmp"
    ]
  },
  
  // === RESTORE STRATEGY ===
  "restore": {
    "strategy": "copy",  // "copy" | "merge-ini" | "merge-json" | "append" | "custom"
    "preHooks": [],
    "postHooks": [],
    "requiresAppClosed": false,
    "requiresElevation": false
  },
  
  // === VERIFICATION ===
  "verify": {
    "commands": [
      { "type": "command-exists", "command": "git" },
      { "type": "file-exists", "path": "%USERPROFILE%\\.gitconfig" }
    ]
  },
  
  // === PROVENANCE ===
  "provenance": {
    "discoveredVia": "sandbox",  // "sandbox" | "manual" | "community"
    "sandboxRun": "2025-01-10T14:30:00Z",
    "diffHash": "abc123...",
    "manualEnrichments": [
      "Added .git-credentials as sensitive",
      "Confirmed portable across machines"
    ]
  }
}
```

### Required Metadata Fields

| Field | Purpose |
|-------|---------|
| `curation.tier` | Priority for maintenance and support |
| `curation.cloudGap` | Justifies inclusion — what cloud DOESN'T restore |
| `capture[].category` | Enables selective restore (config only, data only, etc.) |
| `capture[].portable` | Flags machine-specific paths needing transformation |
| `capture[].sensitive` | Prevents accidental credential leakage |
| `restore.strategy` | Determines which restorer to invoke |
| `verify.commands` | Post-restore validation |
| `provenance` | Audit trail for how module was created |

---

## Strategic Recommendations

### 1. Start with Tier 1 "Anchor" Apps

Focus initial modules on apps with:
- Highest user pain if lost
- Largest professional user bases
- Most complex local state

**Recommended first 10**:
1. Git (universal developer tool)
2. VS Code (dominant editor)
3. PowerToys (universal Windows power user)
4. Chrome (dominant browser)
5. Firefox (privacy-focused users)
6. Obsidian (knowledge workers)
7. Lightroom Classic (photographers)
8. OBS Studio (streamers/creators)
9. MSI Afterburner (enthusiasts)
10. Windows Terminal (developers)

### 2. Leverage Sandbox for Discovery, Humans for Curation

- **Sandbox**: Fast, repeatable discovery of touched paths
- **Humans**: Semantic understanding of what matters

Never ship auto-generated modules without human review.

### 3. Build Confidence Scores

Track module reliability:
- How many successful restores?
- Any reported failures?
- Last verification date?

### 4. Community Contributions

For Tier 2 apps, enable community-submitted modules with:
- Required metadata schema
- Automated validation
- Human review gate before promotion

---

## Conclusion

This matrix defines **24 Tier 1** and **21 Tier 2** apps (45 total), staying within the 30–50 target.

**Selection Principles Applied**:
- ✅ Deep local state with real user pain
- ✅ Significant cloud sync gaps
- ✅ Professional/power user focus
- ❌ No "popular but shallow" apps
- ❌ No fully cloud-synced apps
- ❌ No trivial configuration surfaces

The module generation strategy combines sandbox-based discovery with human curation to produce high-confidence, production-ready restore modules.
