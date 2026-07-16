# Authoring config-generation modules

Use module schema v2 when an application's settings layout or format can vary by installed version, when multiple versions can exist side by side, or when settings need an explicit forward migration. Keep schema v1 for genuinely unversioned settings; the engine still supports it as `legacy_unverified` during restore.

The three reference modules are:

- `modules/apps/windows-terminal/module.jsonc`: one stable generation and one package instance.
- `modules/apps/studio-one/module.jsonc`: one generation discovered through side-by-side versioned directories.
- `modules/apps/owncloud/module.jsonc`: two generations with a real `g1 -> g2` migration.

## Mental model

A module owns one or more independently evolving config sets. A set such as `preferences` can be at `g2` while another set in the same app remains at `g1`.

Installed application versions are evidence used to select exactly one generation. They are not themselves compatibility rules. Restore is direct only when source and target use the same generation. Moving to a later generation requires one explicit forward migration path. A lower-order target is an unsupported downgrade.

The bundle freezes source identity and bytes. The current trusted catalog supplies target discovery, current generation definitions, and migration logic. Never use the captured module snapshot as current execution authority.

## Minimal stable-layout module

```jsonc
{
  "moduleSchemaVersion": 2,
  "id": "apps.example",
  "displayName": "Example",
  "sensitivity": "low",
  "matches": {
    "winget": ["Vendor.Example"],
    "exe": ["example.exe"],
    "uninstallDisplayName": ["^Example"]
  },
  "config": {
    "instanceDetectors": [
      { "id": "installed", "type": "package" }
    ],
    "sets": [
      {
        "id": "preferences",
        "displayName": "Settings",
        "generations": [
          {
            "id": "g1",
            "order": 1,
            "capture": {
              "files": [
                {
                  "source": "%APPDATA%\\Example\\settings.json",
                  "dest": "settings.json",
                  "optional": true
                }
              ],
              "excludeGlobs": ["**\\Cache\\**", "**\\*.lock"]
            },
            "restore": [
              {
                "type": "copy",
                "source": "settings.json",
                "target": "%APPDATA%\\Example\\settings.json",
                "backup": true,
                "optional": true
              }
            ],
            "validate": [
              { "type": "json-parse", "path": "settings.json" }
            ]
          }
        ]
      }
    ]
  }
}
```

Generation-relative `source`, `dest`, and validation paths are portable paths. They must be clean, relative, and contained. Host destinations may use the existing environment-variable allowlist or `${instance.*}` placeholders supported by the engine.

## Instance detectors

Package detection is right when one installed package corresponds to one config root:

```jsonc
{ "id": "installed", "type": "package" }
```

The engine supplies fresh package evidence and preserves the raw vendor version. The detector declaration does not run a package-manager command.

Use path detection when versions can coexist:

```jsonc
{
  "id": "versions",
  "type": "path",
  "glob": "%APPDATA%\\Vendor\\Example *",
  "versionPattern": "^Example (?P<version>[0-9]+)$"
}
```

The engine expands and globs the path, extracts the named `version` group, and keeps every detected root as a distinct instance. It never silently chooses the newest instance. Restore requires an unambiguous target or an explicit `--restore-target <captureId>=<targetInstanceId>` mapping.

Detector IDs are stable identity. Do not rename a released detector just to improve wording.

## Generation matching

Each generation has a stable `id`, a positive unique `order`, and zero or more match rules:

```jsonc
{
  "id": "g2",
  "order": 2,
  "matches": [
    { "versionRange": ">=2.5 <4" }
  ]
}
```

Numeric dotted ranges use the engine comparator. If a vendor version is irregular, use an anchored `versionPattern` instead of pretending it is numeric. Rules for one target must match exactly one generation; overlapping or missing matches remain unknown.

Generation `order` establishes forward direction only. It does not create compatibility and it does not create a migration edge.

## Forward migrations

Declare every supported edge explicitly inside its config set:

```jsonc
"migrations": [
  {
    "from": "g1",
    "to": "g2",
    "operations": [
      {
        "type": "file-move",
        "source": "old/settings.json",
        "target": "new/settings.json"
      },
      {
        "type": "json-set",
        "path": "new/settings.json",
        "jsonPath": "$.schemaVersion",
        "value": 2
      }
    ],
    "validate": [
      { "type": "json-parse", "path": "new/settings.json" },
      {
        "type": "json-path-exists",
        "path": "new/settings.json",
        "jsonPath": "$.schemaVersion"
      }
    ]
  }
]
```

Only engine-owned declarative operations are supported:

- `file-copy`, `file-move`, `file-delete`
- `json-set`, `json-delete`, `json-move`
- `ini-set`, `ini-delete`, `ini-move`

There is deliberately no shell, PowerShell, command, executable, plugin, generic-regex, or host-absolute escape hatch. If a format cannot be transformed with the allowlisted operations, leave the transition unsupported. Do not encode executable behavior in module data.

Each migration edge needs validation, and the target generation needs final validation where the format supports it. Available validation primitives are:

- `file-exists`
- `json-parse`, `json-path-exists`
- `ini-parse`, `ini-key-exists`

Migration runs only in a disposable staging tree. The source bundle is immutable, every edge is validated in order, and target files are not touched until staging and transaction preflight succeed.

## Stable IDs and released fingerprints

Treat these values as public identity once released:

- module ID
- detector ID
- config-set ID
- generation ID
- generation order and semantic definition

The loader computes a fingerprint from the parsed generation definition. Cosmetic JSONC edits do not change it; semantic edits do. Released fingerprints are recorded in `modules/generation-history.json`.

Do not silently repurpose a released generation ID. If a historical definition is still safe as source input after a reviewed change:

1. retain its fingerprint in generation history;
2. add that fingerprint to the generation's `acceptsSourceFingerprints` list;
3. add or update tests proving the historical input is accepted intentionally.

Otherwise introduce a new generation ID and an explicit forward migration.

## Capture, restore, and validation symmetry

For each generation:

- Every restore source must come from that generation's captured payload or from a declared migration output.
- Every validation path must map unambiguously through a restore declaration to a concrete target.
- Preserve nested relative paths; do not collapse files to basenames.
- Keep secrets, credentials, caches, logs, locks, machine-bound license files, and volatile databases excluded just as strictly as in schema v1.
- `backup: true` remains required in module data, although the generation transaction snapshots every target unconditionally.
- Use `requiresAppClosed` only with trusted module executable patterns; the engine reports `app_running` and never kills the application.

## Unsupported formats and transitions

It is valid for a generation to be capturable but not migratable to every later generation. Prefer a clear `incompatible` result over a lossy guess.

Keep a transition unsupported when:

- the settings are binary or proprietary and cannot be validated;
- migration would require running vendor code or arbitrary scripts;
- multiple possible routes would produce different results;
- the target layout cannot be resolved to one detected instance;
- preserving unknown fields or bytes cannot be guaranteed.

Schema v1 remains available for unversioned legacy modules, but it must not be used as a fallback for invalid schema-v2 provenance.

## Validation workflow

Before committing a schema-v2 module:

```powershell
cd go-engine
go test ./internal/modules ./internal/planner ./internal/migration ./internal/configrestore -count=1
```

Then run strict OpenSpec validation from the repository root:

```powershell
npm run openspec:validate
```

Add focused fixtures for stable direct restore, every supported migration edge, boundary versions, ambiguous or unmatched versions, side-by-side targets, payload tampering, and excluded sensitive files. If the module introduces a released generation or changes a released definition, update and validate `modules/generation-history.json` in the same change.
