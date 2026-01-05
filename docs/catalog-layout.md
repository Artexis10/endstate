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

**Schema:**
```jsonc
{
  "version": 1,
  "name": "string",
  "captured": "ISO8601",
  "apps": [],
  "restore": [              // Restore entries (same structure as recipes)
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
- Currently, manifests contain inline restore entries
- Future: Manifests may reference recipes instead of duplicating entries
- Manifests live in `./manifests/examples/` (examples) or `./manifests/local/` (user-specific)

---

## Current State

**Engine behavior:** The engine currently only consumes manifests. Recipes and bundles are **catalog artifacts** for future engine integration.

**Demo manifests:** Some manifests (e.g., `manifests/examples/msi-afterburner.jsonc`) are marked as "generated from recipes" but currently duplicate the restore entries until the engine supports recipe references.

---

## Future Direction

1. Engine will support recipe references in manifests
2. Bundles will generate manifests dynamically
3. GUI will manage recipes and bundles in user directories (`%USERPROFILE%\Documents\Endstate\`)

---

End of catalog layout.
