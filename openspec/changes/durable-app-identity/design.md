## Context

Presence is decided in two places, both keyed on the package ref:

- `internal/commands/verify.go` — `route.drv.Detect(ref)` (or the batch equivalent) yields
  an `installed bool`, which the status ladder below it turns into pass/fail.
- `internal/commands/verify_plan_realizer.go` — manifest apps become
  `realizer.Installable{ID, Ref}` and `r.Plan(desired)` partitions them into
  `diff.Present` / `diff.ToAdd`.

Each driver answers only from its own ledger. Winget's enumerator starts from
`winget export` and enriches those rows from `winget list`; chocolatey reads
`choco list --local-only`. Both ledgers can lose an app that is still installed — winget's
export is a correlation against its package index, and a self-updating browser drops out of
that correlation.

Windows itself does not lose the app. Every Windows installer — winget, chocolatey, and
vendor `.exe`/MSI alike — registers under the Uninstall hives:

- `HKLM\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`
- `HKLM\SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`
- `HKCU\SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

## Goals / Non-Goals

**Goals**

- An installed app stays `present` after it leaves its package manager's tracking.
- One mechanism serving every Windows package manager, not a winget special case.
- Capture stops silently omitting software that is outside a ledger.
- Additive to the manifest; old and new engines read each other's bundles.

**Non-Goals**

- Repairing tracking. Endstate does not re-register apps with winget.
- Rescuing profiles captured before the fingerprint existed (see Migration).
- MSIX/Store apps. They live in the AppX catalogue, not ARP.
- Reinstall-on-drift. An app present under a different identity plans `none`.

## Implementation amendment: observation and refresh

The shared observation result carries both presence and a best-effort version.
Manager-ledger evidence wins; ARP supplies the version only for a fingerprint
fallback. If multiple exact ARP entries disagree, presence remains true but the
version is unknown unless one entry equals the manifest's desired version. Apply
refreshes ARP before post-apply verification only after an install or repin was
actually attempted. Capture update joins both the manager ref and fingerprint,
preserving an existing install ref when a current inventory-only row has none.
WinGet details can expose multiple local identifiers for one package ID (such as
parallel X86 and X64 registrations), so capture retains every binding and consumes
all of them before filtering. The legacy zero-export guard stays intact: a non-empty
ARP inventory is not proof that an empty WinGet export was authoritative.

## Decisions

### Decision: the ARP key is the identity; the display name is not

Observed on a real machine:

| Registry key | DisplayName | DisplayVersion | Publisher |
|---|---|---|---|
| `7-Zip` | `7-Zip 25.01 (x64)` | `25.01` | Igor Pavlov |
| `BraveSoftware Brave-Browser` | `Brave` | `150.1.92.144` | Brave Software Inc |
| `Google Chrome` | `Google Chrome` | `150.0.7871.182` | Google LLC |

The key is vendor-chosen and version-free even where the display name embeds a version.
Measured across the ARP-only entries on that machine, 10 of 43 display names carry a
version or architecture token; none of the keys do.

This matters because the product's whole purpose is cross-machine. A display name captured
on one machine will not equal the name on another running a different version, so
name-based matching has uneven recall by construction. Key-based matching does not, and it
needs no version normalisation — which is fortunate, because no safe global normaliser
exists: stripping version tokens conflates Python 3.11 with 3.12, .NET 8 with 9, x86 with
x64, and Community with Professional.

### Decision: publisher must match too

Display names and even keys can collide across unrelated software. voidtools Everything and
the Steam game Everything both present as `Everything`; Dolphin Emulator and KDE's Dolphin
both present as `Dolphin`. Publisher separates them, and it is recorded in the same registry
values, so requiring both costs nothing.

A match therefore requires key AND publisher. Neither alone is sufficient.

### Decision: no regex, no catalog bridge, no prefix matching

The rejected earlier design reused the module catalog's `uninstallDisplayName` patterns for
older profiles. 376 of the 379 patterns are prefix-only, so they match adjacent editions —
`^Brave` matches Brave Beta/Dev/Nightly, VS Code's matches Insiders. One pattern,
`^IntelliJ IDEA(?! Community)`, cannot even compile under Go's RE2. And a ref does not map
to a single module: `Git.Git` resolves to both `apps.git` and `apps.git-bash`.

Exact key + publisher equality replaces all of it. Comparison is case-insensitive and
whitespace-trimmed, and nothing else.

### Decision: capture unions the ledger with the ARP inventory

Capture currently iterates ledger rows, so an app already outside tracking is absent from
new profiles entirely — the quiet half of the bug. Capture takes the union instead. An ARP
entry that resolves to no package ref is recorded as detected-but-not-installable, the same
shape already used for config-only apps; it is never invented into an installable ref.

### Decision: evidence establishes presence and nothing else

An ARP match makes an app `present`. It never produces an install target: Endstate installs
by package ref, and ARP entries carry none.

This is deliberately *not* load-bearing for safety. The earlier design relied on the claim
that a false match is harmless, which review disproved — config restore still runs when an
install is skipped, restoring settings against a wrong or absent edition, and
`--repin --confirm` calls `ReinstallVersion` against the recorded ref. Key + publisher
matching is precise enough not to need that argument; the asymmetry is a second line of
defence, not the first.

## Risks / Trade-offs

- **A vendor changes its ARP key between versions.** The match falls through to today's
  behaviour: reported missing. No regression, and the profile self-heals on re-capture.
- **Per-user vs per-machine installs** produce different hives and can differ in key. Both
  hives are read; a mismatch degrades to missing rather than guessing.
- **Registry read cost.** Three hive walks per run, comparable to the `winget list` call
  already made. Read once and cache for the run.
- **Manifest growth.** Three short strings per app.
- **MSIX/Store apps remain unsolved**, and the design does not pretend otherwise.

## Migration Plan

1. Engine reads the fingerprint when present; absent means today's ref-only behaviour.
2. Capture starts writing it. New profiles are self-sufficient immediately.
3. Older engines ignore the unknown block — bundles stay forward-compatible.
4. Pre-existing profiles are **not** repaired by heuristics. Re-capturing fixes a profile,
   and the product should say that rather than guessing an identity it never recorded.

## Open Questions

- Should the fingerprint also record MSI `ProductCode`/`UpgradeCode` where present?
  `UpgradeCode` is stable across major versions and would be stronger than the key for MSI
  packages, at the cost of another field. Deferred until the key+publisher path is proven.
- Should a detected-but-not-installable ARP entry be offered to the user at capture time as
  something to install manually on the target? A product question, not a correctness one.
