## Why

Capture failed for a whole module when one of its registry captures overlapped the module's own secret declaration. The Windows settings modules did not load, so their settings were never captured or restored. When they were restored, the engine wrote module presets instead of the values from the old PC.

Capture also listed one installed program twice when winget reported it under both the community and the Microsoft Store source, so a new PC showed the app twice.

Restore results also misled users. A preview said "settings restored". An optional cleanup directory that was absent read as a file missing from the saved setup. A preview of a registry import wrote a backup file, and a generation-aware preview reported a backup that it had already deleted.

## What Changes

- Capture filters declared secrets out of exported registry data and skips only an entry that still conflicts. The module gets a `config_secret_scope_conflict` warning that names it.
- The catalog loads every module tier. Windows settings modules capture the old PC's value type and data, and restore and verify use that data. A value that was absent at capture is not restored.
- Verify checks a captured setting only when the same run restored it. Otherwise the item is skipped with the reason `not_restored`.
- `capture --only` reads a token as a module ID only when the text before its first dot names a catalog tier.
- Capture keeps one entry when rows of one package manager share a full install fingerprint, and prefers the default source.
- Restore statuses keep their names, and each status has one meaning. An absent optional delete-glob directory reports `skipped_up_to_date`.
- Restore-item messages use plain language. A preview says what would happen and never claims a restore.
- A preview writes no backup file and reports no backup.

## Capabilities

### New Capabilities

- `capture-registry-secrets`: registry capture honours a module's secret declaration without failing the module.

### Modified Capabilities

- `registry-values`: captured data binds restore and verify, the catalog loads every tier, and verify is scoped to restored values.
- `apply-restore-envelope`: restore item statuses have one meaning each.
- `apply-restore-streaming`: restore-item messages use plain language and a preview never claims a restore.
- `backup-before-overwrite`: a preview writes no backup.
- `capture-app-subset`: module tokens in `capture --only` are recognized by catalog tier.
- `multi-driver-package-management`: one installed program that one package manager lists under several sources is captured once.

## Impact

The change affects capture, the module catalog, restore, verify, and the restore events. The CLI JSON contract, the event contract, and the restore safety contract change additively; no schema version changes. The GUI renders restore items by status and reason, and shows the engine message as secondary text. The GUI mirror of the contracts changes in the GUI repository.
