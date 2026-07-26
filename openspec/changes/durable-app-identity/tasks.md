## 0. Done already

- [x] 0.1 Capture writes an app's observed display name under `displayName` rather than the
      unbindable `_name`, so it survives the bundle writer's round-trip through
      `manifest.App`. Merged as Artexis10/endstate#198. The field stays — it is a human
      label — but it is NOT an identity and is not matched on.

## 1. ARP inventory reader

- [x] 1.1 New reader over the Uninstall hives: `HKLM\SOFTWARE\...\Uninstall`,
      `HKLM\SOFTWARE\WOW6432Node\...\Uninstall`, `HKCU\SOFTWARE\...\Uninstall`.
      `golang.org/x/sys/windows/registry` is already a dependency and already used in
      `internal/commands` and `internal/restore`.
- [x] 1.2 Each entry carries: uninstall key (the subkey name), `DisplayName`,
      `DisplayVersion`, `Publisher`, `InstallLocation`.
- [x] 1.3 Skip entries Windows itself hides: `SystemComponent=1`, and entries with no
      `DisplayName`.
- [x] 1.4 Read once per observation phase and refresh only after an install/repin attempt;
      each read is three hive walks, comparable to the existing `winget list` call.
- [x] 1.5 Non-Windows builds compile against a stub returning an empty inventory.
- [x] 1.6 Test against fixture hives, including a per-user (`HKCU`) entry and a
      WOW6432Node entry.

## 2. Manifest fingerprint

- [x] 2.1 Optional per-app fingerprint block on `manifest.App` — key, publisher, version —
      `omitempty` so existing bundles round-trip byte-identically.
- [x] 2.2 Confirm tolerance both directions: a bundle carrying the block loads on an engine
      without it, and vice versa.

## 3. Capture records it, and stops omitting

- [x] 3.1 Correlate each captured app to its inventory entry and record the fingerprint.
- [x] 3.2 Enumerate the union of driver ledger and inventory only when every ledger
      relationship is known. An inventory entry with no package ref is recorded as
      detected-but-not-installable — never invent a ref.
- [x] 3.3 Test: capture of a ledger-tracked app records ref + fingerprint.
- [x] 3.4 Test: an app absent from the ledger but present in the inventory still appears in
      the capture.
- [x] 3.5 Test: an app with no inventory entry captures with its ref alone.

## 4. Presence matching

- [x] 4.1 Extend the two presence sites — `verify.go`'s `Detect` verdict and the
      `verify_plan_realizer.go` partition — with the fingerprint rung. ONE shared predicate
      used by both; duplicated rules drifting apart is the recurring bug class here.
- [x] 4.2 Match requires key AND publisher, full-string, case-insensitive, trimmed. No
      prefix, no substring, no regex, no catalog lookup.
- [x] 4.3 Version excluded from identity; it stays available to drift evaluation.
- [x] 4.4 Inventory evidence must not reach any installable set.
- [x] 4.5 Test: the reproduced case — `Google.Chrome.EXE` absent from export, fingerprint
      matches inventory — plans `none`.
- [x] 4.6 Test: same key, different publisher (`Everything` / voidtools vs Valve) → missing.
- [x] 4.7 Test: version-bearing display name does not affect the match.
- [x] 4.8 Test: a genuinely absent app is still planned for install.
- [x] 4.9 Test: a pre-fingerprint profile reports missing and infers nothing.
- [x] 4.10 Prove red-before-green on 4.5. A test that passes before the fix is testing the
      wrong thing — that has already happened once on this change.

## 5. Chocolatey

- [x] 5.1 No chocolatey-specific code. Choco packages register in ARP like everything else,
      so 4.1 covers them. Add a test with a chocolatey-driver app whose ref is absent from
      the choco ledger but whose fingerprint matches, and confirm it reports present.

## 6. Verification

- [x] 6.1 `go test ./...` green.
- [x] 6.2 Against the real reproduction on the affected machine: capture, confirm Chrome and
      Brave now appear with fingerprints, then apply and confirm both report `present`, zero
      installs planned, no failure row.
- [x] 6.3 Confirm no event-contract change: statuses and phases unchanged, only which status
      a given app receives.

## 6a. Authoritative binding correction

- [x] 6a.1 Verify on a WinGet 1.29 host that export plus details expose local ARP bindings,
      including details blocks without an ARP identifier.
- [ ] 6a.2 Verify the real Chrome/Brave reproduction and the exact-name Everything collision.
- [x] 6a.3 Run the full Go suite outside the restricted sandbox; the local sandbox blocks
      the profile write and HKCU registry round-trip tests.

## 7. Out of scope, stated explicitly

- [x] 7.1 MSIX/Store apps register in the AppX catalogue, not ARP, and are not covered.
      Record this as a known gap rather than letting it look solved.
- [x] 7.2 Profiles captured before the fingerprint existed are not repaired. Re-capture
      fixes them; the product should say so rather than guess.
