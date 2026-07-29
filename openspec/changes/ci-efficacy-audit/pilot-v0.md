# Six-mutant efficacy preflight

## Question

Before investing in the frozen 30-mutant audit, run one bounded hosted preflight that answers a narrower question: does the validation work on PR #205 catch realistic production regressions that the current `main` merge gate accepts?

This preflight is evidence for unique detection value, not a coverage percentage and not permission to claim deployment, hosted-live, installer, GUI, release, or schedule readiness.

## Fixed references

- Legacy-control reference: `ab8065cd67ab3f4e9e876e07a25facf3100c28c7`, the protected `main` commit that contains only the inert efficacy-workflow bootstrap above the prior main reference.
- Detector reference: `437c0ca4167c09bc9f2de515daa6d55d35257d4f`, the PR #205 stack plus the test-helper-only UTF-16LE control-baseline repair.
- The production files mutated by the pilot are byte-identical at both references before mutation.
- The unfinished 30-mutant scoring engine is excluded from the preflight because independent review found it cannot yet mint trustworthy aggregate proof.

## Fixed candidates

| ID | Production regression | Targeted detector | Expected stable failure |
|---|---|---|---|
| `bundle-duplicate` | Duplicate `bizhawk` in `bundles/gaming.jsonc`. | Catalog Matrix | `execution_failure / catalog-plan / success` with child reason `duplicate_membership` |
| `bundle-missing` | Replace `winbox` with absent `winboxx` in `bundles/remote-access.jsonc`. | Catalog Matrix | `execution_failure / catalog-plan / success` with child reason `missing_module` |
| `bundle-id-drift` | Change the ID inside `bundles/communication.jsonc` so it differs from the filename. | Catalog Matrix | `envelope_contract / catalog-plan / envelope` |
| `vlc-backup-off` | Disable backup on VLC's first restore entry. | `apps.vlc/default-v1` | `unsupported_fixture / fixture / restore[0]` |
| `alacritty-source-drift` | Point Alacritty's first restore source at a non-captured payload identity. | `apps.alacritty/default-v1` | `unsupported_fixture / fixture / capture.files[0]` |
| `obs-target-drift` | Point OBS Studio's profiles restore at a target that no longer matches its captured identity. | `apps.obs-studio/default-v1` | `unsupported_fixture / fixture / restore[1]` |

For a module candidate, the detector-reference patch MAY also update exactly the sibling `validation.jsonc` `moduleRevision` scalar to `ComputeModuleRevision(mutated module)`. No scenario, fixture, expectation, timeout, assertion minimum, or live policy may change. The legacy-reference patch contains only the identical production `module.jsonc` mutation because those validation sidecars do not exist in the legacy authority.

## Hosted execution

The existing default-branch stub remains inert. The audit branch copy uses only `workflow_dispatch`, has no inputs and `permissions: {}`, acquires both public references anonymously by exact SHA, uses no checkout action or repository credential, and never installs a target application.

One baseline job runs Catalog Matrix and the three targeted module scenarios twice on the unmodified detector reference. A fixed candidate matrix runs each mutation against Windows, Ubuntu, and macOS legacy controls. Windows executes the same Go vet/test and built-binary integration contract as the actual required checks; Ubuntu and macOS execute the same Go vet/test contract. Before integration, Windows verifies Notepad++ is already present and treats absence as infrastructure failure rather than installing it.

For each candidate, the Windows leg also creates two fresh detector-reference checkouts, applies the reviewed detector patch, and runs the declared detector once in each checkout. Every job publishes bounded JSON evidence even when a command fails. The aggregate requires exactly one baseline artifact and eighteen candidate/OS artifacts.

## Classification and decision

A `correct-new-only-kill` requires all four merge-gating legacy contracts to pass, both unmodified detector repetitions to pass with the same proof identity, and both mutated detector repetitions to fail with the exact declared failure identity. A candidate is instead `already-covered`, `wrong-kill`, `survivor`, `flake`, or `infrastructure-failure`; job color and raw log text never award kill credit.

The preflight reports `meaningful-signal` only with at least five correct new-only kills, all three module safety/restore candidates correctly killed, at least two detector families represented, and zero wrong kills, flakes, or infrastructure failures. Anything else reports `insufficient-signal` and preserves every row. This threshold is a go/no-go signal for completing the 30-mutant audit, not an estimate that the CI catches 83%, 90%, or any other fraction of future defects.
