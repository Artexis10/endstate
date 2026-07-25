## 1. Manifest field

- [ ] 1.1 Add an optional installed-display-name field to the manifest app
      shape in `internal/manifest/types.go`, `omitempty` so existing bundles
      round-trip byte-identically.
- [ ] 1.2 Confirm unknown-field tolerance both directions: a bundle carrying the
      field loads on an engine without it, and vice versa.

## 2. Capture writes the second identity

- [ ] 2.1 Record the display name already carried on
      `driver.InstalledPackage.Name` when building manifest apps in
      `internal/commands/capture.go`.
- [ ] 2.2 Trim surrounding whitespace; record nothing when the name is empty.
- [ ] 2.3 Test: capture of a winget-tracked app records both reference and name.
- [ ] 2.4 Test: an app whose enumeration supplies no name captures successfully
      with the reference alone.

## 3. Ledger-external evidence from the drivers

- [ ] 3.1 Surface the installed software `winget list` reports that
      `winget export` does not — the ARP entries — as evidence distinct from
      ledger membership, carrying display name and version.
- [ ] 3.2 Keep it out of the installable set: this evidence establishes presence
      only and must never become an install target.
- [ ] 3.3 Test: an app present only as an ARP entry is surfaced as evidence and
      is absent from the installable set.

## 4. Presence matching

- [ ] 4.1 Extend the presence partition so an app matches on package id, then
      recorded display name, then the catalog `uninstallDisplayName` bridge.
- [ ] 4.2 Full-string, case-insensitive, trimmed comparison for recorded names.
      Catalog patterns are used as the anchored regexes they are authored as.
- [ ] 4.3 Test: the reproduced case — `Google.Chrome.EXE` absent from export,
      `Google Chrome` present in ARP — plans `none`, not `install`.
- [ ] 4.4 Test: the pre-existing-profile case — `Brave.Brave` with no recorded
      name, resolved through `apps.brave`'s `uninstallDisplayName`.
- [ ] 4.5 Test: `Git` does not match `GitHub Desktop`.
- [ ] 4.6 Test: a genuinely absent app is still planned for install.

## 5. Chocolatey parity

- [ ] 5.1 Decide whether ARP evidence applies to chocolatey-driven apps on
      Windows (design open question), and either implement it symmetrically or
      record the decision to defer.

## 6. Verification

- [ ] 6.1 `go test ./...` green.
- [ ] 6.2 Verify against the real reproduction: the 2026-07-20 profile applied
      on the affected machine reports Chrome and Brave `present`, plans zero
      installs, and completes without a failure row.
- [ ] 6.3 Confirm no event-contract change: statuses and phases unchanged, only
      which status a given app receives.
