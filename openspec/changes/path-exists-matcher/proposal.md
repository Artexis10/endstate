# Proposal: pathExists Matcher for Non-Winget Apps

## Problem

Apps installed outside winget (Adobe Creative Cloud apps, built-in tools, vendor-only installers) can't match config modules during capture. The current matchers — `winget`, `exe` (PATH lookup), and `uninstallDisplayName` (registry) — miss apps like Lightroom Classic that have no winget ID, aren't on PATH, and may not have standard uninstall registry entries detectable during discovery.

## Solution

Add a `pathExists` matcher that checks filesystem paths for known application artifacts. If any specified path exists, the module matches. Paths use environment variables and are expanded via the existing `Expand-ConfigPath` function.

```jsonc
"matches": {
    "winget": [],
    "exe": ["lightroom.exe"],
    "uninstallDisplayName": ["^Adobe Lightroom Classic"],
    "pathExists": [
        "%ProgramFiles%\\Adobe\\Adobe Lightroom Classic\\lightroom.exe",
        "%APPDATA%\\Adobe\\Lightroom\\Preferences\\Lightroom Classic CC 7 Preferences.agprefs"
    ]
}
```

## Key Design Rules

- **Additive:** pathExists is OR'd with other matchers — it supplements, never replaces
- **Early exit:** First matching path wins; no need to check all paths
- **Reuses existing infrastructure:** `Expand-ConfigPath` and `Test-Path` — no new dependencies
- **Optional field:** Existing modules continue to work unchanged
- **No CLI contract changes:** This is internal module matching, not output schema
