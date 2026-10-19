## 1. Registry secrets in capture

- [x] 1.1 Resolve registry secret coordinates with the dual key and value reading, including `secrets.registryValues`.
- [x] 1.2 Filter secret keys and values out of registry exports, parse the result again, and count removed items in `sensitiveExcludedCount`.
- [x] 1.3 Skip only a conflicting registry entry and add one `config_secret_scope_conflict` warning with `moduleId` and the per-module warning.
- [x] 1.4 Keep the strict boundary in validation mode.
- [x] 1.5 Add release-blocking catalog tests for secret consistency and for module files that do not load.

## 2. Windows settings fidelity

- [x] 2.1 Load every catalog tier and synthesize no app entry for a module outside the `apps` tier.
- [x] 2.2 Bind `registry-set` restore and `registry-value-equals` verify entries to the captured type and data; drop entries for absent or unwritable values with a module warning.
- [x] 2.3 Scope a `fromModule` verify entry to values the same run restored; skip it with reason `not_restored` otherwise.

## 3. Capture selection grammar

- [x] 3.1 Read a `--only` token as a module ID only when the text before its first dot names a catalog tier; remove the claim-back of captured app IDs.
- [x] 3.2 Update the `capture --help` text, the CLI JSON contract, and the tests for the tier-prefix rule.
- [x] 3.3 Collapse rows of one package manager whose install fingerprints share a non-empty key, publisher, and version into one entry; keep the default-source entry; count the program once.

## 4. Restore results

- [x] 4.1 Report an absent optional `delete-glob` directory as `skipped_up_to_date`.
- [x] 4.2 Fail a required input that the saved setup does not include with a plain message; keep the path in `source`.
- [x] 4.3 Author restore-item messages from status and the dry-run flag; a preview never claims a restore. Share the messages with the validation harness.
- [x] 4.4 Check dry-run before any backup write in `registry-import`, in production and validation mode.
- [x] 4.5 Report no backup on a generation-aware preview item, whose prior snapshot lives in a deleted disposable root.

## 5. Contracts and verification

- [x] 5.1 Update `cli-json-contract.md`, `event-contract.md`, and `restore-safety-contract.md`.
- [x] 5.2 Run the scoped and full Go suites, `go vet`, the Linux build, gofmt on changed files, and strict OpenSpec validation.
- [x] 5.3 Run a scratch-profile dry run and live restore and record the restore-item events and envelope.
- [ ] 5.4 Mirror the contract changes in the GUI repository.
- [ ] 5.5 Complete an independent review of the batch and resolve every blocking finding.
