# Proposal: capture-share-mode

## Why

Endstate's distinctive capability is moving *settings*, not just app lists. The motivating use is handing a curated setup to someone else — but a bundle built for self-rebuild is the wrong artifact for that, in three ways: it replaces the recipient's existing config, it embeds the sender's machine name, and nothing marks it as intended for another machine.

The collision behaviour is the substantive one. Self-rebuild and sharing want *opposite* semantics:

- **Rebuild** wants the captured config to win outright. Merging would let stale local keys survive a restore meant to return the machine to a known state.
- **Sharing** wants the reverse. The recipient has their own settings and did not ask to lose them.

So merge-preferring restore cannot be a global change to how config restores; it has to be a property of the sharing path.

## What Changes

- **`capture --share`**, requiring `--only` and refusing `--sanitize`. An unscoped share attaches every matched module's config, which is the opposite of a curated setup; `--sanitize` attaches none, leaving nothing to share. Both are rejected before anything is captured.
- **Merge-preferring restore entries** in share bundles, with `backup` forced on so any merge is revertable.
- **`machineName` omitted** from share metadata — it identifies the sender.
- **New metadata: `os`, `share`, `name`.** All additive.
- **Cross-OS refusal on `rebuild`** driven by `metadata.os`.

### Why the retyping is conservative

A wrong merge silently corrupts a config file. An honest replace is backed up and revertable. So a `copy` entry is retyped only when the bundled payload proves it is safe:

- **`merge-json` only for a strict JSON object.** Two independent reasons: `RestoreMergeJson` parses with `json.Unmarshal`, which rejects the comments and trailing commas common in editor settings; and `DeepMerge` merges only when *both* sides are objects, replacing wholesale otherwise. A JSON array — VS Code's `keybindings.json` — would pass a naive "is it valid JSON" check and then silently overwrite the recipient's file *under a merge label*. Unmarshalling into `map[string]interface{}` enforces both conditions at once.
- **`merge-ini` only for `.ini` targets, never git config.** `MergeIni` stores values in a `map[string]string` and so collapses duplicate keys, while git legitimately repeats them (multiple fetch refspecs, `insteadOf` entries). Merging there drops data with no error.
- **Declared types are never retyped.** A module author who chose `append` or `registry-set` knows something the inspection does not.

The decision is made at capture time and encoded in the bundled restore `type`, so an older engine applying a newer share bundle still merges.

### Why cross-OS is refused rather than degraded

`modules.MatchCriteria` has no non-Windows package identity (winget/chocolatey only) and module paths are Windows-shaped. A cross-OS apply installs nothing and restores to paths that do not exist. A per-item report whose every skip reads "wrong OS" is less useful than refusing with both operating systems named.

## Capabilities

### New Capabilities
- `capture-share-mode`: capture can produce a bundle intended for another person, with merge-preferring restore and sender identity omitted.
- `cross-os-bundle-refusal`: rebuild refuses a bundle captured on a different OS.

### Modified Capabilities
<!-- none — self-rebuild capture and apply are unchanged -->

## Impact

- `go-engine/internal/bundle/share_merge.go` — new: `preferMergeForShare`, the payload inspection, and the git-config carve-out.
- `go-engine/internal/bundle/capture_bundle.go` — `Share`/`Name` on the request; retype hook; `os`/`share`/`name` metadata; blanked machine name.
- `go-engine/internal/bundle/create.go` — `BundleMetadata` gains `OS`, `Share`, `Name` (additive).
- `go-engine/internal/commands/capture.go` — `CaptureFlags.Share`, `validateShareFlags`.
- `go-engine/internal/commands/rebuild.go` — `refuseCrossOSBundle`, `rebuildGOOSFn` seam.
- `go-engine/cmd/endstate/main.go`, `capabilities.go`, `docs/contracts/cli-json-contract.md` — PROTECTED; modified under explicit instruction.
- Backward-compatible: without `--share`, every path is unchanged. Bundles with no recorded `os` are still accepted. No schema bump.

## Not in this change

**Redaction of identity-bearing values.** The design settles that a share bundle should strip identity (git `user.email`, absolute user paths, hostnames) while self-rebuild keeps full fidelity. `machineName` is omitted and that is all — payload contents are not yet inspected. Until redaction lands, **a share bundle can carry personal data from the captured config files**, and that limitation must be stated wherever `--share` is surfaced to users.
