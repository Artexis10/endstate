# Change: Sandbox Validation Loop

## Why

Module authors need a way to validate that capture/restore cycles work correctly without touching the host machine. Currently, sandbox discovery exists but there's no structured validation workflow that tests the full install → seed → capture → wipe → restore → verify cycle with deterministic PASS/FAIL output.

## What Changes

- **Host-side single-app validation script** (`scripts/sandbox-validate.ps1`)
- **Host-side batch validation script** (`scripts/sandbox-validate-batch.ps1`)
- **Golden queue file** (`sandbox-tests/golden-queue.jsonc`) with existing modules
- **Documentation** (`docs/VALIDATION.md`) for running validation

## Impact

- Affected specs: None (new capability)
- Affected code: `scripts/`, `sandbox-tests/`, `docs/`
- No changes to module schema
- No changes to engine core
- Reuses existing `sandbox-tests/discovery-harness/sandbox-validate.ps1`
