## Why

A manifest records exactly one identity per app: the package-manager id it was captured
under (`refs.windows: "Google.Chrome.EXE"`). Presence detection is a membership test of
that id against the driver's own ledger — `winget export` for winget, `choco list
--local-only` for chocolatey. A package-manager id is a handle for *installing* something,
not an identity for *recognising* it, and when it stops resolving there is nothing else to
match on, so an installed app is reported missing by construction.

Reproduced on a real machine, 2026-07-25:

- A capture taken 2026-07-20 recorded `Brave.Brave` and `Google.Chrome.EXE`. Both were
  winget-tracked then, so the manifest is correct.
- By 2026-07-25 neither appeared in `winget export`. Both browsers had self-updated out of
  winget's tracking.
- Applying that profile planned installs for both, ran `winget install Google.Chrome.EXE`
  against an already-installed Chrome, and reported **FAILED**.

There is a second, quieter half to the same defect: capture enumerates *from* the ledger
and only enriches those rows, so an app that has already fallen out of tracking is not
captured at all. On the machine above, a fresh capture contains neither browser. Old
profiles mis-plan; new profiles silently lose the apps.

Self-updating software leaving its package manager's tracking is normal, not exotic.
Chocolatey has the same property: its ledger records what choco installed, not what is on
the machine.

## What Changes

Identity comes from where Windows actually records installed software — the Uninstall
registry hives (ARP) — rather than from any one package manager's bookkeeping.

- Capture records an **install fingerprint** per app alongside the existing ref: the ARP
  key, publisher, and version observed at capture time. Additive; `refs` is untouched.
- Presence matches on the package id first (unchanged fast path), then on **ARP key +
  publisher**. Both must match. Nothing else.
- Capture enumerates the union of the driver ledger and the ARP inventory, so software
  outside a package manager's tracking stops disappearing from new profiles.

The ARP **key** is the identity, not the display name. Observed on a real machine:

| Registry key | DisplayName | DisplayVersion | Publisher |
|---|---|---|---|
| `7-Zip` | `7-Zip 25.01 (x64)` | `25.01` | Igor Pavlov |
| `BraveSoftware Brave-Browser` | `Brave` | `150.1.92.144` | Brave Software Inc |
| `Google Chrome` | `Google Chrome` | `150.0.7871.182` | Google LLC |

The key is version-free even where the display name is not, and the version arrives as a
structured field rather than embedded in a label.

### Superseding the previous proposal

This change originally proposed matching on the captured **display name**, with the module
catalog's `uninstallDisplayName` patterns as a compatibility bridge for older profiles.
Independent review rejected that design and the objections were verified:

- **The bridge is unsafe.** 376 of 379 `uninstallDisplayName` patterns are prefix-only, so
  `^Brave` matches Brave Beta/Dev/Nightly, `^PyCharm( Professional)?` matches Community,
  and `^Jellyfin` matches Jellyfin Media Player.
- **The bridge does not fix the reported case.** No Chrome module exists, so an old Chrome
  profile misses every rung of that ladder.
- **`^IntelliJ IDEA(?! Community)` cannot compile under Go's RE2** — negative lookahead is
  unsupported, so the bridge would fail outright on it.
- **A ref does not map to one module**: `Git.Git` resolves to both `apps.git` and
  `apps.git-bash`.
- **Display names are not unique.** voidtools Everything collides exactly with the Steam
  game Everything; Dolphin Emulator collides with KDE's Dolphin.
- **The safety argument was wrong.** The old design leaned on "a false match can only
  suppress an install, never cause harm". It can cause harm: config restore still runs when
  an install is skipped, so settings are restored against a wrong or absent edition, and
  `--repin --confirm` calls `ReinstallVersion` against the recorded ref.

Key + publisher matching removes each of these rather than mitigating them, and does not
depend on the false-match-is-harmless argument being true.

## Impact

- Affected specs: `multi-driver-package-management`
- Affected code: a new ARP inventory reader over the `HKLM`, `HKLM\WOW6432Node` and `HKCU`
  Uninstall hives (`golang.org/x/sys/windows/registry` is already a dependency, already used
  in `internal/commands` and `internal/restore`); `internal/manifest` (optional per-app
  fingerprint); `internal/commands/capture.go` (record it, and union the inventory); the
  presence partition used by verify and apply.
- Manifest change is additive; older engines ignore the new block, so bundles stay readable
  in both directions.
- **Covers every Windows package manager**, because winget, chocolatey and vendor installers
  all register in ARP. It is not a winget-specific patch.
- **Does not cover MSIX/Store apps**, which register in the AppX catalogue rather than ARP.
  Out of scope here, and called out rather than papered over.
- Profiles captured before this change carry no fingerprint and cannot be repaired reliably
  — every technique for guessing one is what the review above dismantled. Re-capturing a
  profile fixes it, and the product should say so plainly.
