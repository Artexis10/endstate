## Context

A backup's identity is its backend-assigned **id** (a UUID returned by `CreateBackup` → substrate). Its `name` is a display label set once at create and never changed — `--name` is only consulted on creation (see `resolveBackupID`), and there is no rename in the storage client or backend. So the GUI's machine auto-backup is stuck at `"This computer"`, and there's no way to relabel.

The GUI must not derive or fabricate a label (thin-presentation-layer contract). So the engine + backend own the label, and the label must be mutable.

## Goals / Non-Goals

**Goals:**
- Make a backup's label mutable: `backup rename --backup-id <id> --name <label>` → `PATCH /api/backups/:id`.
- Replace the meaningless `"default"` create label with the device (host) name.
- Keep the change a reusable foundation: the backend route is a partial-metadata update, so future fields are additive.
- Keep `resolveBackupID` deterministically unit-testable (inject the default name).

**Non-Goals:**
- Per-profile auto-backup *unification* (silent + explicit host converging on one backup) — still deferred; it needs a stable manifest-embedded machine/profile id, not just a mutable label.
- Renaming versions, or any non-`name` metadata field today (the shape allows it; no field is added now — YAGNI).
- Changing the push-resolution order or the append-to-first convenience.
- Global name uniqueness (names stay labels; id is the key).

## Decisions

**D1 — Mutable label via a partial-metadata PATCH, exposed as `backup rename`.**
The backend endpoint is `PATCH /api/backups/:id` taking a partial body (today `{ name }`); the engine wraps it as `Client.UpdateBackup(ctx, id, name)` and the CLI surfaces it as `backup rename`. Rationale: a partial PATCH is the general "update backup metadata" primitive, so adding `description`/`tags`/… later is a column + a field, no new route/command/wrapper. Alternative (a one-off `/rename` route) rejected as narrow.

**D2 — Inject the create-default name into `resolveBackupID`.**
`resolveBackupID(ctx, store, backupID, name, defaultName)` — the caller passes `deviceLabel()`; tests pass a fixed value. Rationale: calling `os.Hostname()` inside the resolver would make the "create default" unit test machine-dependent. The pure `deviceLabelFrom(host, err)` is unit-tested for the trim/fallback logic.

**D3 — Advertise rename as a capability flag, not a probe.**
Rename reuses `--backup-id`/`--name`, so the GUI cannot detect it by flag. Add `features.hostedBackup.rename = true`; the GUI gates its rename UI on it.

**D4 — Rename access mirrors delete (backend).**
The substrate route uses read-access (allowed in any non-`none` subscription state), matching DELETE — rename is strictly less destructive than delete, so gating it harder would be inconsistent. (Specced in the substrate change.)

## Risks / Trade-offs

- **Ugly host names** (`DESKTOP-7F3K2P1`) become the create-default label verbatim. Mitigation: identity is the id; and now that labels are mutable, the user/GUI can rename. (Whether to prettify the Windows auto-generated pattern is deferred — verbatim is simplest and accurate.)
- **Consumer/backend skew**: the engine emits `PATCH` before substrate deploys it → 405. Mitigation: ship substrate first; the GUI gates on `hostedBackup.rename` + the engine pin. The device-label default is engine-internal and safe regardless.
- **`os.Hostname()` error** → falls back to `"default"`; never fails a push.

## Migration Plan

Backward-compatible. No data migration: existing backups keep their labels (including legacy `"This computer"`/`"default"`). Only newly-created default-named backups get the device label; rename is opt-in. Order: substrate `PATCH` deploys → engine releases (GUI pin auto-bumps via `engine-drift-check`) → GUI adds the rename affordance. Rollback = revert; the create-default reverts to `"default"` and rename calls 405 (or are hidden by the capability gate).

## Open Questions

- Prettify ugly host names now, or rely on user rename? (Recommend: rely on rename.)
- Eventually attach a stable machine **id** to the backup (for the deferred unification)? Out of scope here.
