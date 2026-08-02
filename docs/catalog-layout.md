# Catalog Layout

**Status:** Stable  
**Purpose:** Define the structure and purpose of modules, bundles, and manifests in Endstate.

---

## Overview

Endstate organizes configuration portability through three artifact types:

- **Modules**: Reusable configuration templates (single source of truth)
- **Bundles**: Collections of modules
- **Manifests**: Executable restore specifications

---

## Config Modules

**Location:** `./modules/apps/<app-id>/module.jsonc`

**Purpose:** Define reusable configuration restore entries for a specific application or tool.

**Schema:**
```jsonc
{
  "id": "string",           // Unique identifier (e.g., "apps.msi-afterburner")
  "displayName": "string",  // Human-readable name
  "notes": "string",        // Optional description
  "restore": [              // Array of restore entries
    {
      "type": "copy",
      "source": "./configs/...",
      "target": "C:\\...",
      "backup": true
    }
  ]
}
```

**Characteristics:**
- Modules are **templates** that define what to restore, not when or how
- Source paths are relative and portable
- Target paths are absolute system paths
- Modules do not execute directly; they are referenced by manifests or bundles
- Each production module has a sibling `validation.jsonc` with its canonical
  `moduleRevision` pin. Use `endstate-validation sync-revisions --repo <path>`
  to check drift; `--write` is required to repair that pin.

---

## Bundles

**Location:** `./bundles/`

**Purpose:** Group multiple modules into logical collections.

**Schema (v1):**
```jsonc
{
  "version": 1,
  "id": "string",           // Unique identifier (e.g., "core-utilities")
  "name": "string",         // Human-readable name
  "modules": [              // Array of module IDs
    "msi-afterburner",
    "powertoys"
  ]
}
```

**Characteristics:**
- Bundles reference modules by ID
- No overrides or customization in v1
- `endstate catalog-plan --bundle bundles/<id>.jsonc --json --events jsonl` strictly resolves one tracked bundle into ordered catalog-module actions
- The command is read-only and catalog-only: it does not install packages, synthesize manifest apps, or execute restore/verify work

---

## Manifests

**Location:** `./manifests/`

**Purpose:** Executable specifications consumed by the Endstate engine.

**Schema (v1):**
```jsonc
{
  "version": 1,
  "name": "string",
  "captured": "ISO8601",
  "apps": [],
  // Optional: Inline restore entries
  "restore": [
    {
      "type": "copy",
      "source": "./configs/...",
      "target": "C:\\...",
      "backup": true
    }
  ],
  
  "verify": []
}
```

**Characteristics:**
- Manifests are what the engine executes
- Manifest `bundles` and `modules` composition is not implemented by the ordinary manifest planner
- Use `catalog-plan` to validate a tracked bundle; explicit app and restore declarations remain the executable manifest authority
- Manifests live in `./manifests/examples/` (examples) or `./manifests/local/` (user-specific)

---

## Current State

**Engine behavior:** The engine resolves a tracked bundle only through `catalog-plan`. Ordinary manifest planning does not expand `bundles` or `modules` fields.

**Architecture:** Modules under `modules/apps/*/module.jsonc` are the single source of truth for app configuration.

---

## Future Direction

1. GUI will manage modules and bundles in user directories (`%USERPROFILE%\Documents\Endstate\`)
2. Module parameterization and overrides (v2)
3. Bundle composition and nesting (v2)

---

End of catalog layout.
