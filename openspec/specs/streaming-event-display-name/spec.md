# streaming-event-display-name Specification

## Purpose
Ensures all streaming item events (`event: "item"`) include a `name` field with a human-readable display name so the GUI can show meaningful labels in progress messages instead of raw winget package IDs.

## Requirements

### Requirement: Item Events Include Display Name

All streaming item events for app-scoped items MUST include a `name` field containing a human-readable display name. The field is additive and optional per the event contract (`omitempty`) — no event schema version bump is needed.

#### Scenario: Installed app emits resolved display name

- **WHEN** `apply --events jsonl --json` is run and `Microsoft.VisualStudioCode` is already installed
- **THEN** the `item` event for this app includes `"name": "Visual Studio Code"` (resolved from winget)

#### Scenario: Missing app emits manifest display name, winget ref, or ID

- **WHEN** `apply --events jsonl --json` is run and a winget app is not installed (display name cannot be resolved from winget)
- **THEN** the `item` event's `name` field contains the app's manifest `displayName` if set, otherwise the winget ref (e.g. `"EclipseAdoptium.Temurin.8.JRE"`), otherwise the app's manifest `id`
- **AND** the `name` field is never the internal manifest slug (app.ID) when a winget ref is available

#### Scenario: Manual app uses manifest display name

- **WHEN** any command emits an `item` event for a manual app
- **THEN** the `name` field contains the app's manifest `displayName` if set, otherwise the app's `id`

#### Scenario: Plan command includes display names

- **WHEN** `plan --events jsonl --json` is run
- **THEN** all item events include the `name` field with the same resolution as apply

#### Scenario: Verify command includes display names

- **WHEN** `verify --events jsonl --json` is run
- **THEN** all item events for apps include the `name` field, even for failed/missing apps

#### Scenario: All phases in apply include display names

- **WHEN** `apply --events jsonl --json` runs through plan, apply, and verify phases
- **THEN** item events in every phase include the `name` field

### Requirement: Display Name Resolution Order

The engine SHALL resolve the `name` field for app item events using this precedence:

1. Resolved display name from winget detection (batch or per-ref) — available only when the app is installed
2. App's `displayName` field from the manifest (set explicitly or synthesized from config modules)
3. Winget ref (e.g. `"EclipseAdoptium.Temurin.8.JRE"`) — acceptable fallback, more recognizable than the internal ID
4. App's `id` field from the manifest — last resort, only for non-winget apps with no ref

#### Scenario: Winget display name takes precedence

- **WHEN** an installed winget app has display name `"Visual Studio Code"` from detection and manifest `displayName` is `"VS Code"`
- **THEN** the item event `name` is `"Visual Studio Code"`

#### Scenario: Manifest displayName as fallback

- **WHEN** a winget app is not installed (no winget display name) but manifest has `displayName: "Visual Studio Code"`
- **THEN** the item event `name` is `"Visual Studio Code"`

#### Scenario: Winget ref as fallback

- **WHEN** a winget app is not installed and has no manifest `displayName`
- **THEN** the item event `name` is the winget ref (e.g. `"EclipseAdoptium.Temurin.8.JRE"`)

#### Scenario: Manifest ID as last resort

- **WHEN** a non-winget app has no manifest `displayName` and no ref
- **THEN** the item event `name` is the app's manifest `id`

## Invariants

### INV-EVENT-NAME-1: Name Is Never the Internal Manifest Slug When a Ref Exists
- The `name` field in item events MUST NOT contain the internal manifest slug (app.ID, e.g. `"eclipseadoptium-temurin-8-jre"`) when a winget ref is available
- Winget refs (e.g. `"EclipseAdoptium.Temurin.8.JRE"`) are acceptable as display names when no better name is resolved

### INV-EVENT-NAME-2: Name Is Never Empty for App Items
- For app-scoped item events, `name` MUST be a non-empty string
- The fallback to manifest `id` guarantees this since every app has an `id`

### INV-EVENT-NAME-3: Additive Change
- This is a schema-additive change — the `name` field uses `omitempty` and existing consumers can ignore it
- No event contract version bump required

### INV-EVENT-NAME-4: No New I/O
- Display name resolution MUST NOT introduce new I/O operations
- Resolution uses data already available from winget detection and manifest loading

## Scope
- Applies to all commands that emit app-scoped item events: plan, apply (all phases), verify, capture
- Does NOT apply to non-app item events (restore, revert, export, validate) which use module/path identifiers
- Capture phase emits item events with `name` populated via its own resolution path (`displayNameMap` from snapshot → `app.Name` fallback) since capture operates on `snapshot.CapturedApp`, not `manifest.App`. The invariants above (name is never empty, never a raw ref) still hold.

## Affected Commands
- apply (plan phase, apply phase, verify phase)
- plan
- verify
- capture
