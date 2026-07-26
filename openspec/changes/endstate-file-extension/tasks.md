# Tasks: endstate-file-extension

## 1. One bundle-extension predicate

- [x] 1.1 `manifest.BundleExt` (`.endstate`), `manifest.LegacyBundleExt` (`.zip`), and
      `manifest.BundleExtensions` as the single source of truth
- [x] 1.2 `manifest.IsBundlePath` matches the whole set, case-insensitively
- [x] 1.3 `bundle.IsBundle` delegates to `manifest.IsBundlePath` instead of re-deriving the rule
- [x] 1.4 Tests: `.endstate` and `.zip` load identically through `LoadManifestOrBundle`; mixed
      casing matches; `.endstate.jsonc` / `.zip.jsonc` do not; a non-zip `.endstate` is rejected
      rather than parsed as JSONC; `ExtractBundle` round-trips both extensions

## 2. Capture output naming

- [x] 2.1 `captureBundleOutputPath` defaults to `.endstate` for `--profile` and for the
      manifest-derived path
- [x] 2.2 An explicit `--out` that already names a bundle is written verbatim, casing included
- [x] 2.3 An `--out` with no extension or a non-bundle extension is normalized to `.endstate`
- [x] 2.4 `outputFormat` stays `"zip"` — the container did not change
- [x] 2.5 Tests: table over all six cases; existing explicit-`.zip` capture test still passes

## 3. Profile resolution

- [x] 3.1 `.endstate` ahead of `.zip`, both ahead of loose folder and bare manifest
- [x] 3.2 Tests: `.endstate` found; `.endstate` wins over a co-located `.zip`; `.zip` still wins
      over a bare manifest

## 4. User-facing text

- [x] 4.1 `main.go` help for `rebuild --from` (PROTECTED area, explicit instruction)
- [x] 4.2 `rebuild.go` remediation strings name `.endstate` with `.zip` as legacy
- [x] 4.3 Contract docs: `profile-contract.md`, `capture-artifact-contract.md`,
      `cli-json-contract.md`

## 5. Verification

- [x] 5.1 `go test ./...` green
- [x] 5.2 `openspec validate --all --strict` green
- [x] 5.3 End-to-end: a real capture writes `.endstate`, and `verify --manifest <that file>`
      reads it
