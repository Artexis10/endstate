## Context

Endstate's restore pipeline dispatches on the `type` field of each restore action. The current switch in `RunRestore` (`go-engine/internal/restore/restore.go`) handles four file-based strategies: `copy`, `merge-json`, `merge-ini`, and `append`, plus a special-cased `delete-glob`. All of these operate on filesystem paths. The revert system (`go-engine/internal/restore/revert.go`) similarly assumes file-based operations -- it either copies a backup file/directory back to the target path, or deletes a target that was created during restore.

Windows applications that store configuration in the registry (e.g., FastRawViewer stores preferences under `HKCU\Software\LibRaw LLC\FastRawViewer\Prefs`) have no way to participate in this pipeline today. The `.reg` file format provides a portable, human-readable representation of registry data that can be imported and exported via the built-in `reg` command.

## Goals / Non-Goals

**Goals:**
- Support importing `.reg` files into the Windows Registry as a restore strategy
- Support backing up registry keys to `.reg` files before overwriting
- Support capturing registry keys to `.reg` files during the capture workflow
- Support reverting registry-import operations by re-importing the backup `.reg` file
- Restrict to HKCU keys only for safety (no elevation required)

**Non-Goals:**
- Supporting HKLM or other root keys that require elevation
- Providing a GUI for registry browsing or editing
- Supporting granular per-value registry operations (full key import/export only)
- Supporting non-Windows registry equivalents (macOS plist, Linux dconf)

## Decisions

### 1. Restore: `registry-import` type

The `registry-import` strategy treats the source as a `.reg` file in the payload and the target as a registry key path (e.g., `HKCU\Software\LibRaw LLC\FastRawViewer\Prefs`).

**Restore execution:** Runs `reg import <resolved_source_path>`. The source `.reg` file is resolved using the same `resolveSource` logic as other strategies (ExportRoot fallback to ManifestDir).

**Backup execution:** When `backup: true` and the target key exists, runs `reg export <target_key> <backup_dir>/<sanitized_key_name>.reg /y` before importing. The backup path is recorded in the `RestoreResult.BackupPath` field and propagated to the journal.

**Target existence check:** Uses `reg query <target_key>` to determine whether the target key exists (for `TargetExistedBefore` tracking and backup decisions).

**Non-Windows behavior:** Returns an error `"registry-import is only supported on Windows"` immediately. This is enforced via a build-tag-gated or runtime GOOS check.

### 2. Capture: `registryKeys` field

A new `registryKeys` field is added to the `CaptureDef` struct in `go-engine/internal/modules/types.go`, alongside the existing `files` field. Each entry specifies:

- `key` -- the registry path to export (e.g., `HKCU\Software\LibRaw LLC\FastRawViewer\Prefs`)
- `dest` -- the relative output path for the exported `.reg` file (e.g., `apps/fastrawviewer/Prefs.reg`)
- `optional` -- boolean; if true, a missing key is silently skipped

**Capture execution:** Runs `reg export <key> <dest_path> /y` for each entry. The resulting `.reg` file is included in the capture payload at the specified `dest` path.

**Module capture entry format:**
```json
"registryKeys": [
  {
    "key": "HKCU\\Software\\LibRaw LLC\\FastRawViewer\\Prefs",
    "dest": "apps/fastrawviewer/Prefs.reg",
    "optional": true
  }
]
```

### 3. Revert: registry-import awareness

The current `RunRevert` function in `revert.go` processes journal entries in reverse order. For each entry with `action: "restored"`, it follows this logic:

1. **CASE 1 -- Backup exists:** If `backupCreated` is true and the backup path exists on disk, it copies the backup file or directory back to the target path. For directories, it removes the target first and does a recursive copy. For files, it does a simple file copy.
2. **CASE 2 -- Target was created (no backup):** If `targetExistedBefore` is false, it deletes the target that was created during restore.
3. **CASE 3 -- No backup, target existed:** Skips (nothing to revert).

For `registry-import` entries, the revert logic must differ from the file-based path. Instead of copying the backup `.reg` file to a filesystem target, it must run `reg import <backup.reg>` to restore the previous registry state. The journal entry's `targetPath` field will contain a registry key path (not a filesystem path), so the revert code must detect this and dispatch accordingly.

**Detection strategy:** Journal entries for registry-import operations will have a `targetPath` that starts with `HKCU\` (or `HKEY_CURRENT_USER\`). The revert function checks for this prefix to determine whether to use `reg import` instead of file copy. Alternatively, a `restoreType` field could be added to `JournalEntry` to make the dispatch explicit -- this is the preferred approach as it avoids heuristic-based detection.

**Recommended journal change:** Add an optional `restoreType` field to `JournalEntry`. For backward compatibility, entries without this field default to file-based revert behavior.

### 4. Module restore entry format

```json
{
  "type": "registry-import",
  "source": "./payload/apps/fastrawviewer/Prefs.reg",
  "target": "HKCU\\Software\\LibRaw LLC\\FastRawViewer\\Prefs",
  "backup": true,
  "optional": true
}
```

The `source` field uses the standard payload-relative path convention. The `target` field contains the registry key path. The `backup` and `optional` fields behave identically to other restore types.

### 5. Cross-platform considerations

The `registry-import` type is Windows-only by design. This is acceptable because:

- macOS applications that use property lists (plist files) are already handled by the `copy` restorer
- Linux applications that use dotfiles are already handled by the `copy` restorer
- The module matching system (`matches` field with `pathExists`, `wingetId`, etc.) already isolates platform-specific modules -- a module with a registry-import restore entry will only match on Windows machines where the relevant software is installed

No cross-platform abstraction is needed or desired.

### 6. HKCU-only restriction

For safety, `registry-import` only supports keys under `HKCU` (`HKEY_CURRENT_USER`). Keys under `HKLM` (`HKEY_LOCAL_MACHINE`) and other root keys require administrator elevation and affect all users on the machine, which conflicts with Endstate's non-destructive-defaults invariant.

**Validation:** Before executing any `reg import` or `reg export`, the target key path is checked. If it starts with `HKLM\`, `HKEY_LOCAL_MACHINE\`, `HKCR\`, `HKEY_CLASSES_ROOT\`, `HKU\`, or `HKEY_USERS\`, the operation fails immediately with a clear error: `"registry-import only supports HKCU keys"`.

`HKCU` and `HKEY_CURRENT_USER` are both accepted as valid prefixes.

## Risks / Trade-offs

- [Risk] `reg import` can silently overwrite existing registry values with no undo beyond the backup `.reg` file -> Mitigation: Backup-before-overwrite is the standard pattern; backup `.reg` files are created before any import
- [Risk] `.reg` files can contain values for keys outside the declared target -> Mitigation: This is inherent to the `.reg` format; documentation should advise keeping `.reg` files scoped to the intended key tree
- [Risk] `reg export` output may vary across Windows versions (encoding, header comments) -> Mitigation: The `.reg` file is treated as opaque; Endstate does not parse it, only passes it to `reg import`
- [Risk] Concurrent registry access could cause inconsistent exports -> Mitigation: Same risk as any registry operation; no special handling beyond what Windows provides
- [Trade-off] Adding `restoreType` to `JournalEntry` increases journal format surface -> Acceptable: the field is optional and backward-compatible; it eliminates fragile heuristic-based dispatch in revert
