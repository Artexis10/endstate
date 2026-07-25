## Why

A manifest records exactly one identity per app: the package-manager id it was
captured under (`refs.windows: "Google.Chrome.EXE"`). Presence detection matches
that id against the driver's own ledger — `winget export` + `winget list` for
winget, `choco list --local-only` for chocolatey. When the id stops resolving,
there is nothing else to match on, so the app is reported **missing** by
construction.

That is not hypothetical. Reproduced on a real machine, 2026-07-25:

- A capture taken 2026-07-20 recorded `Brave.Brave` and `Google.Chrome.EXE`.
  Both were winget-tracked at the time, so `winget export` listed them and the
  manifest is correct.
- By 2026-07-25 neither appeared in `winget export` at all. `winget list` showed
  them only as ARP entries:

  ```
  Brave           ARP\Machine\X86\BraveSoftware Brave-Browser   150.1.92.144
  Google Chrome   ARP\Machine\X86\Google Chrome                 150.0.7871.182
  ```

- Applying that profile planned installs for both, ran
  `winget install Google.Chrome.EXE` against an already-installed Chrome, and
  reported **FAILED**.

Self-updating browsers routinely migrate out of their package manager's
tracking. Chocolatey has the same structural property — its ledger records what
choco installed, and an app installed or replaced outside choco is absent from
it. So this is not a winget quirk; it is the consequence of a single-identity
manifest matched against one ledger.

The user-visible damage is worse than a wrong count: Endstate proposes to
install software the user already has, then reports a failure for refusing to
reinstall it. That directly undermines the product's core claim.

## What Changes

- Capture records a **second, ledger-independent identity** for each app: the
  installed display name the enumeration already observed. Existing manifest
  fields are untouched; this is additive.
- Presence detection matches on the package id **or** the recorded display
  name, so an app that leaves its package manager's tracking is still seen.
- Profiles captured before this change carry no display name. For those, the
  module catalog's existing `uninstallDisplayName` matchers provide a
  compatibility bridge where a module exists (`apps.brave` already declares
  both `"winget": ["Brave.Brave"]` and `"uninstallDisplayName": ["^Brave"]`).
  This is explicitly a fallback, not the mechanism — it only covers apps
  someone wrote a module for, and there is no Chrome module today.
- Display-name matching is anchored and case-insensitive, never substring, so
  "Git" cannot match "GitHub Desktop".

## Impact

- Affected specs: `multi-driver-package-management`
- Affected code: `internal/commands/capture.go` (record the observed name),
  `internal/manifest` (new optional app field), the realizer's presence
  partition, and the winget/chocolatey enumerators (surface ARP/ledger-external
  evidence).
- Manifest change is additive and older engines ignore the new field, so
  bundles stay readable both directions.
- No event-contract change: statuses and phases are unchanged. Apps that were
  wrongly reported `missing`/`install` now report `present`/`none`, which is the
  correction, not a new shape.
