# Catalog Layout

**Status:** Draft  
**Purpose:** Define the structure and purpose of recipes, bundles, and manifests in Endstate.

---

## Overview

Endstate organizes configuration portability through three artifact types:

- **Recipes**: Reusable configuration templates
- **Bundles**: Collections of recipes
- **Manifests**: Executable restore specifications

---

## Recipes

**Location:** `./recipes/`

**Purpose:** Define reusable configuration restore entries for a specific application or tool.

**Schema:**
```jsonc
{
  "id": "string",           // Unique identifier (e.g., "msi-afterburner")
  "name": "string",         // Human-readable name
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
- Recipes are **templates** that define what to restore, not when or how
- Source paths are relative and portable
- Target paths are absolute system paths
- Recipes do not execute directly; they are referenced by manifests or bundles

---

## Bundles

**Location:** `./bundles/`

**Purpose:** Group multiple recipes into logical collections.

**Schema (v1):**
```jsonc
{
  "version": 1,
  "id": "string",           // Unique identifier (e.g., "core-utilities")
  "name": "string",         // Human-readable name
  "recipes": [              // Array of recipe IDs
    "recipe-id-1",
    "recipe-id-2"
  ]
}
```

**Characteristics:**
- Bundles reference recipes by ID
- No overrides or customization in v1
- Bundles simplify multi-recipe workflows

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
  
  // Optional: Reference recipes from ./recipes/
  "recipes": [
    "recipe-id-1",
    "recipe-id-2"
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
- Manifests can reference bundles, recipes, or contain inline restore entries
- All three approaches can be combined in a single manifest
- Manifests live in `./manifests/examples/` (examples) or `./manifests/local/` (user-specific)

**Restore Entry Resolution Order:**

When a manifest contains `bundles`, `recipes`, and/or inline `restore` entries, the engine expands them in the following order:

1. **Bundle recipes** (in bundle order, recipe order within each bundle)
2. **Manifest recipes** (in order)
3. **Manifest inline restore[]** (appended last)

This ordering ensures predictable behavior and allows manifests to override or extend bundle/recipe configurations.

**Example:**
```jsonc
{
  "version": 1,
  "name": "my-setup",
  "bundles": ["core-utilities"],  // Expands to msi-afterburner + powertoys
  "recipes": ["custom-app"],       // Adds custom-app restore entries
  "restore": [                     // Adds final inline entry
    { "type": "copy", "source": "./configs/override.cfg", ... }
  ]
}
```

**Error Handling:**
- If a referenced bundle or recipe file does not exist, the engine fails with a clear error message
- If a recipe exists but has no restore entries, it is treated as empty (no error)

---

## Current State

**Engine behavior:** The engine now supports recipe and bundle references in manifests. Catalogs are resolved at manifest load time and expanded into a single restore[] array.

**Demo manifests:** Manifests can now reference recipes directly instead of duplicating restore entries.

---

## Future Direction

1. GUI will manage recipes and bundles in user directories (`%USERPROFILE%\Documents\Endstate\`)
2. Recipe parameterization and overrides (v2)
3. Bundle composition and nesting (v2)

---

End of catalog layout.
