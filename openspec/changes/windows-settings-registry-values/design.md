## Context

Endstate's restore strategies are file-based (`copy`, `merge-json`, `merge-ini`, `append`, `delete-glob`) plus the key-scoped `registry-import`. None can safely set a *single* Windows OS-preference value: the preferences live as individual named values inside HKCU keys (`...\Themes\Personalize`, `...\Explorer\Advanced`) that also hold many unrelated co-resident values. A whole-key `.reg` import would clobber those neighbours — unacceptable for OS settings the user did not declare.

This change adds a **value-level** tier: capture/restore/verify that operate on exactly one named value (`key` + `valueName` + `valueType` + `data`) and never read or rewrite anything else under the key.

The existing pipeline is reused wherever possible:
- The HKCU-only gate (`ValidateRegistryTarget`) is shared with `registry-import`.
- The backup-then-revert flow (journal `RestoreType` + `BackupPath`) is reused; `registry-set` writes a small JSON prior-value sidecar instead of a `.reg` file.
- The per-item event stream, `--enable-restore` opt-in gate, and dry-run plumbing are unchanged.

## Goals / Non-Goals

**Goals:**
- A value-level `registry-set` restore: HKCU-only, backup-before-overwrite, idempotent, dry-run-safe, creates-key-if-missing.
- A value-level `registryValues` capture and a `registry-value-equals` verify (DATA comparison).
- Full reversibility: revert restores the exact prior value, or deletes a value that was absent before.
- Seed `windows-settings/*` modules proving the vertical slice.
- Zero change to file-restore behavior; no new CLI flag; no touch to `cmd/endstate/`.

**Non-Goals:**
- No HKLM/HKCR/HKU/HKCC writes (HKCU only).
- No uninstall/debloat, no service or scheduled-task changes — reversible single-value writes only.
- No binary/multi-string registry types in v1 (REG_DWORD / REG_SZ / REG_EXPAND_SZ only).
- No editing of `docs/contracts/restore-safety-contract.md` (protected; see below).

## Decisions

### Value type set
`registry-set` supports `REG_DWORD`, `REG_SZ`, `REG_EXPAND_SZ`. These cover the OS-preference values the seed modules need. The set is deliberately narrow to keep the surface reversible and unambiguous; REG_BINARY / REG_MULTI_SZ are rejected with a clear error and can be added later if a concrete module needs them.

### Backup-and-overwrite (the default-semantics exception)
Unlike file-restore (default: **skip** an existing target), `registry-set` defaults to **backup-and-overwrite**. A single OS-preference value has no meaningful "merge" or "leave-as-is" semantics — the user is declaring a desired value. Reversibility is guaranteed structurally: the prior value (type+data, or "absent") is recorded to `state/backups/<runID>/regset_<key>_<value>.json` **before** any write, and revert consumes that sidecar. The only skip is the idempotent skip-if-already-equal (no backup written in that case, since nothing changes).

### DWORD comparison is numeric
For idempotency and verify, DWORD data is compared numerically so `"0x1"` and `"1"` are equal to a stored `1`. String types compare exactly.

### Capture is value-scoped, not key-scoped
`registryValues` reads only the declared named values and writes a JSON snapshot (`registry-values.json`), recording each value's type/data and whether it existed. It never exports the whole key, so co-resident values are never captured or later clobbered.

### Revert: restore-or-delete
The prior-value sidecar carries `existed`. Revert restores the exact prior type+data when `existed=true`, and deletes the value when `existed=false` (the value was created by the restore).

## Restore-safety-contract wording — PENDING USER APPROVAL

`docs/contracts/restore-safety-contract.md` is a **protected, decision-gated** file. This change does **NOT** edit it. The proposed addition is captured here verbatim for explicit approval before the contract is touched:

> ### Value-level registry-set: backup-and-overwrite (exception to the default-skip rule)
>
> The file-based restore strategies default to **skip**: when a target already
> exists it is left untouched unless the action opts into overwrite. The
> value-level `registry-set` strategy is an explicit, documented **exception**:
> its default is **backup-and-overwrite**.
>
> A `registry-set` action targets exactly one named value under an HKCU key.
> Because a single OS-preference value has no merge or partial-update semantics,
> "skip an existing value" would make the declared desired state unreachable.
> Instead, `registry-set`:
>
> 1. Records the prior value — its REG_* type and data, or the fact that it was
>    absent — to `state/backups/<runID>/` **before** any write. This backup is
>    mandatory and unconditional (it is not gated on a `backup: true` flag).
> 2. Skips the write only when the named value already equals the desired
>    type+data (idempotency); in that case no backup is written because nothing
>    changes.
> 3. Otherwise creates the key if missing and writes the desired value.
>
> Reversibility is guaranteed by construction: `revert` restores the exact prior
> value, or **deletes** the value when it was absent before the write. The scope
> is HKCU-only and Windows-only; non-HKCU targets and unsupported value types are
> rejected before any registry access. `registry-set` performs no uninstall,
> debloat, service, or scheduled-task changes — it is a reversible single-value
> write.

## Risks / Trade-offs

- **A running Explorer may not reflect a change immediately** (e.g. `HideFileExt`, `Hidden`, `TaskbarAl`) until Explorer is restarted or notified. The engine writes the value correctly; surfacing/refresh is out of scope and noted in module `notes`.
- **`TaskbarAl` may be absent on a default install.** Restore creates it; revert deletes it (since it was absent before) — verified by the revert test.
- **Capture snapshot is not auto-imported on restore.** The `registry-values.json` snapshot is a value-level record; restore is driven by the module's explicit `registry-set` actions, not by importing the snapshot. This is intentional: it keeps restore declarative and avoids reintroducing whole-key clobbering.

## Migration / Compatibility

- Additive only: new optional fields on existing types; new restore/verify/capture type strings. Existing manifests, modules, and bundles are unaffected.
- No schema version bump.
- The `windows-settings/*` modules live in a sibling directory to `modules/apps/`; the existing `modules/apps` catalog-integrity test is unaffected, and a dedicated test guards the new category.
