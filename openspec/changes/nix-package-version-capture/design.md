## Context

The Nix realizer capture path (`runCaptureRealizer`) already emits each installed element as a
`capturedApp{ID, Refs, Name}`. The `manifest.App` struct has a `Version string` field
(`json:"version,omitempty"`) that the winget path uses for declared-version pinning and that
capture can populate to enable round-trip version pinning on re-apply. The `provision.ProvItem`
struct has `Version string` (best-effort, always `""` on the Nix path today).

Every Nix profile element has `StorePaths []string` populated by `parseProfileList`. A real
install yields paths like:

```
/nix/store/2rwsbbpn5p76jf35rv7cb9qlhpxnp83p-ripgrep-14.1.0
/nix/store/0b4y5p2qadabfmjmr2zczbzjfvpjydrq-ripgrep-14.1.0-man
```

The version is always the token between `<name>-` and the end (or a known output suffix like
`-bin`, `-man`, `-dev`, `-doc`, `-lib`, `-etc`, `-info`, `-out`). The hash prefix is always 32
lowercase hex chars followed by `-`.

## Goals / Non-Goals

**Goals:**
- Parse the version from `StorePaths` without any additional CLI call.
- Record parsed version in `ProvItem.Version` (Provisioning Generation) on the Nix apply path.
- Emit parsed version into `manifest.App.Version` in the captured manifest so re-apply can pin it.
- Best-effort: unparseable store path → empty string, never fails the run.

**Non-Goals:**
- No version pinning on the Nix apply path (Nix pins via its ref/flakeref, not per-app version).
  `App.Version` on the Nix path is informational only — `apply_realizer.go` never reads it.
- No winget capture changes; home-manager paths unchanged.
- No new realizer interface methods (`StorePathVersion` is a pure package-level function).

## Decisions

- **Parser placement**: `internal/realizer/nix/versions.go` (package `nix`), a pure function
  `StorePathVersion(name string, storePaths []string) string`. No interface change.
- **Store path format**: strip the 32-char hex hash prefix + `-`, strip the element name + `-`,
  take the leading version token (up to the next `-` that begins a known output suffix or until
  end). Prefer the store path whose base starts with `<hash>-<name>-` (exact name match); fall
  back to any path when no exact match.
- **Output suffix handling**: known suffixes treated as non-version segment starts: `-bin`,
  `-man`, `-dev`, `-doc`, `-lib`, `-etc`, `-info`, `-out`, `-small`, `-debug`, `-light`,
  `-full`, `-wrapped`. A segment starting with a digit is part of the version.
- **Apply path**: after `Realize` returns, re-read `Current()` (already done) and for each
  element in the resulting set whose ID matches an action, call `StorePathVersion` and set
  `action.Version`. Already-present actions get their version from `Current()` before the
  install phase (they are in the set already).

## Design

### `internal/realizer/nix/versions.go`

```go
// StorePathVersion extracts the version from element store paths.
// It prefers the path whose base starts with "<hash>-<name>-" (exact name
// match), falling back to the first parseable path. Returns "" on parse miss.
func StorePathVersion(name string, storePaths []string) string
```

Algorithm:
1. For each store path, call `parseStorePathVersion(name, path)`.
2. Return the first non-empty result from a path whose base starts with
   `<32hex>-<name>-` (exact-name match), then first non-empty from any path.
3. Return "" if no path yields a version.

`parseStorePathVersion(name, path) string`:
1. Base = `filepath.Base(path)`.
2. Strip the 32-char lowercase hex hash + `-` prefix. If the base does not match
   `[0-9a-f]{32}-<rest>`, return "".
3. Strip the element name + `-` prefix from `rest`. If `rest` does not start with
   `name + "-"`, return "" (not an exact-name path — caller handles fallback).
4. `versionPart = rest[len(name)+1:]`. Take the leading version segment: scan
   forward while the current `-`-delimited token starts with a digit or is part of
   a version-looking token (e.g. `14`, `1.0.0`, `2025-04-01`). Stop when a token
   is a known output suffix.
5. Return the joined version tokens.

### `capture_realizer.go`

In the emit loop, after building `el = cur.Elements[name]`:

```go
version := nix.StorePathVersion(el.Name, el.StorePaths)
captured = append(captured, capturedApp{
    ID:      name,
    Refs:    map[string]string{goos: ref},
    Name:    name,
    Version: version,
})
```

`capturedApp` gains `Version string \`json:"version,omitempty"\``.
`cleanApp` gains the same field so sanitized output also carries it.

### `apply_realizer.go`

After the present-check loop (Phase 1: Plan), for each `present` entry, look up the element in
`cur` (read once via `r.Current()` before the loop, as already done for the plan diff) and set
`action.Version = nix.StorePathVersion(e.ins.Ref_leaf, el.StorePaths)`.

After Phase 2 (Realize succeeds), for each newly-installed entry, look up the element in
`res.After` and set `action.Version`.

This flows to `ProvItem.Version` via the existing `writeProvisioningGeneration`.

## Risks / Verification

- **Store-path name segment**: if the element `Name` differs from the store-path base name
  segment (rare for nixpkgs packages), the exact-match fails and the fallback picks the first
  parseable path. The version is still best-effort correct.
- **Multi-version store paths**: an element can have multiple store paths (e.g. `ripgrep-14.1.0`
  and `ripgrep-14.1.0-man`). The exact-match preference picks the base output path (no suffix),
  which is the most stable.
- **Date-versioned packages**: some packages use `YYYY-MM-DD` versions. The parser treats
  `-`-separated numeric tokens as part of the version, so `2025-04-01` parses correctly.
- **Real-nix smoke**: coordinator applies 2-3 packages → captures → asserts `App.Version`
  non-empty and `ProvItem.Version` non-empty in the generation; confirms the store-path format
  matches the parser assumptions.
