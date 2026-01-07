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
- Bundles simplify multi-module workflows

---

## Manifests

**Location:** `./manifests/`

**Purpose:** Executable specifications consumed by the Endstate engine.

**Schema (v1 with catalog support):**
```jsonc
{
  "version": 1,
  "name": "string",
  "captured": "ISO8601",
  "apps": [],
  
  // Optional: Reference bundles from ./bundles/
  "bundles": [
    "bundle-id-1",
    "bundle-id-2"
  ],
  
  // Optional: Reference modules from ./modules/apps/
  "modules": [
    "msi-afterburner",
    "powertoys"
  ],
  
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
- Manifests can reference bundles, modules, or contain inline restore entries
- All three approaches can be combined in a single manifest
- Manifests live in `./manifests/examples/` (examples) or `./manifests/local/` (user-specific)

**Restore Entry Resolution Order:**

When a manifest contains `bundles`, `modules`, and/or inline `restore` entries, the engine expands them in the following order:

1. **Bundle modules** (in bundle order, module order within each bundle)
2. **Manifest modules** (in order)
3. **Manifest inline restore[]** (appended last)

This ordering ensures predictable behavior and allows manifests to override or extend bundle/module configurations.

**Example:**
```jsonc
{
  "version": 1,
  "name": "my-setup",
  "bundles": ["core-utilities"],  // Expands to msi-afterburner + powertoys
  "modules": ["custom-app"],      // Adds custom-app restore entries
  "restore": [                    // Adds final inline entry
    { "type": "copy", "source": "./configs/override.cfg", ... }
  ]
}
```

**Error Handling:**
- If a referenced bundle or module file does not exist, the engine fails with a clear error message
- If a module exists but has no restore entries, it is treated as empty (no error)

---

## Current State

**Engine behavior:** The engine supports module and bundle references in manifests. Catalogs are resolved at manifest load time and expanded into a single restore[] array.

**Architecture:** Modules under `modules/apps/*/module.jsonc` are the single source of truth for app configuration.

---

## Future Direction

1. GUI will manage modules and bundles in user directories (`%USERPROFILE%\Documents\Endstate\`)
2. Module parameterization and overrides (v2)
3. Bundle composition and nesting (v2)

---

End of catalog layout.
