## Context

Presence is decided in `verify_plan_realizer.go`: the manifest's apps become
`realizer.Installable{ID, Ref}`, `r.Plan(desired)` partitions them into
`diff.Present` / `diff.ToAdd`, and the partition is a set-membership test of
`Ref` against `driver.EnumerateInstalled()`.

Each driver enumerates only its own ledger:

- winget — `winget export` (authoritative), enriched by `winget list` for names
  and versions, keyed by exact package id.
- chocolatey — `choco list --local-only --limit-output`, keyed by choco package
  id.

Both ledgers can lose an app that is still installed. Winget's export is derived
by correlating ARP with its package index, so a self-updating browser drops out
of correlation. Chocolatey's ledger is more stable, but an app replaced by a
vendor installer is equally absent from it.

## Goals / Non-Goals

**Goals**

- An app that is installed remains `present` after it leaves its package
  manager's tracking.
- Driver-agnostic: the same mechanism serves winget and chocolatey.
- Additive to the manifest; old and new engines read each other's bundles.

**Non-Goals**

- Repairing tracking. Endstate does not re-register apps with winget.
- Reinstall-on-drift. If an app is present under a different identity, the
  correct plan is `none`, not "install anyway".
- Version comparison. This change is about existence only.

## Decisions

### Decision: a second identity in the manifest, not a catalog lookup

The alternative considered first was matching ARP entries via the module
catalog's `uninstallDisplayName`. Rejected as the primary mechanism because
coverage is accidental: it works only where a module exists. `apps.brave`
declares both identities and would be fixed; Chrome has no module at all and
would stay broken. A fix whose coverage depends on unrelated catalog authorship
looks general and is not.

The manifest, by contrast, is written at capture time from the very enumeration
that saw the app — the display name is already in hand (`winget list` supplies
it, and `EnumerateInstalled` already carries `Name`). Recording it costs one
field and is complete by construction for every app captured going forward.

The catalog bridge is kept, demoted to a compatibility path for profiles
captured before this change.

### Decision: anchored, case-insensitive name matching

Display names are user-facing strings and prone to false positives. Matching is
therefore full-string and case-insensitive after trimming — never substring.
"Git" must not match "GitHub Desktop"; "Brave" must not match "Brave Browser
Beta" unless that is the recorded name.

Where the catalog bridge applies, its `uninstallDisplayName` values are already
anchored regexes (`^Brave`) authored for this purpose and are used as written.

### Decision: identity is evidence, not a new install target

A display-name match makes an app `present`. It never becomes an install target:
Endstate cannot install "Google Chrome" by display name, only by package id. So
the new identity can suppress a wrong install but can never cause one.

That asymmetry is deliberate and is what keeps the blast radius small — the
worst failure mode of a bad match is skipping an install the user could still
perform manually, not installing the wrong software.

## Risks / Trade-offs

- **False `present`.** A wrong name match hides a genuinely missing app.
  Mitigated by full-string anchored matching and by the asymmetry above. Tests
  cover the "Git" vs "GitHub Desktop" case explicitly.
- **Stale recorded name.** An app renamed by its vendor between capture and
  apply falls back to the package id, i.e. today's behaviour. No regression.
- **Manifest growth.** One short string per app. Negligible against payloads.

## Migration Plan

1. Engine reads the new field if present; absent means "fall back to catalog
   bridge, then package id alone". No migration step for existing bundles.
2. Capture starts writing the field. Newly captured profiles are self-sufficient.
3. Older engines ignore the unknown field — bundles stay forward-compatible.

## Open Questions

- Should ARP evidence also be surfaced for **chocolatey** on Windows, given
  choco apps also appear in ARP? Leaning yes for symmetry, but winget is where
  the reproduced failure is, so chocolatey parity can follow once the winget
  path proves out.
