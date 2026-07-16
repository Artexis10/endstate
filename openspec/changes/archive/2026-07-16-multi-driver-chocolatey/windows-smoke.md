## Windows maintainer smoke

Run this on a disposable Windows VM from a clean Endstate build. Keep the VM snapshot so absent/present backend paths can be repeated. Use packages and versions that are currently available from the VM's configured sources; do not add or replace a Chocolatey source for this test.

1. Record the pre-test Chocolatey source/config state without copying credentials into logs:
   - If Chocolatey is present, save `choco source list --limit-output`.
   - Hash `%ChocolateyInstall%\config\chocolatey.config` when that file exists.
2. Run `endstate capabilities --json`. Confirm Windows advertises `drivers: ["winget", "chocolatey"]`, with Winget first, and advertises capture `--driver` plus the existing apply/rebuild bootstrap flags.
3. From a snapshot without Chocolatey:
   - Run an unfiltered capture and confirm Winget data succeeds with one structured `optional_driver_unavailable` warning for Chocolatey.
   - Run `capture --driver chocolatey` and confirm it fails rather than returning an empty successful capture.
   - Run mixed apply as a dry-run, with no bootstrap flag, and with `--no-bootstrap`; confirm Chocolatey is visible but never installed or retried through Winget.
   - Run mixed apply with `--bootstrap-backends`; inspect the single consent event, accept only in the disposable VM, and confirm the official PowerShell installer is followed by a working `choco --version` probe.
4. Use a mixed manifest containing one Winget app and one explicit Chocolatey app. Apply it and confirm:
   - Each ref is sent only to its declared manager.
   - Results and item events carry the resolved driver.
   - The run writes separate Winget and Chocolatey generations sharing one `runId`.
5. Run unfiltered `capture --pin` after both managers are present. Confirm Chocolatey apps declare `driver: "chocolatey"`, versions are retained when exposed, ordering is stable across two captures, and cross-manager name collisions keep both entries with `possible_duplicate` rather than suppression.
6. Rebuild the captured profile with the bootstrap flags. Confirm rebuild passes consent through to apply, installs both lanes independently, restores configuration, and verifies the resulting apps through their original managers.
7. Pick a Chocolatey package with two versions available from the already-configured source:
   - Apply an exact version and confirm the requested version is recorded.
   - Change the manifest to the other version, preview `--repin --dry-run`, then run `--repin --confirm`; confirm Chocolatey's downgrade-capable upgrade path converges to the declared version.
8. Roll back the mixed apply without `--to`. Confirm both generations sharing the newest apply `runId` are selected, Winget refs go only to Winget, Chocolatey refs go only to Chocolatey, and Chocolatey uninstall does not request dependency removal. Repeat with one manager temporarily unavailable and confirm the other lane completes with a partial result and no fallback.
9. Compare the post-test source listing and Chocolatey config hash with step 1. They must be unchanged by capture, apply, rebuild, bootstrap, pin/repin, and rollback. Inspect logs to confirm no source credentials were serialized.
