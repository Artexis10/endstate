> TDD: write each test RED first, then implement to green. Hermetic + host-aware (inject a fake
> realizer via the shared `newRealizerFn` seam with `withFakeRealizer`; key fixtures by
> `runtime.GOOS`; for the Windows guard override `newRealizerFn → ErrNoRealizer`).
> Verify: `cd go-engine && go test ./...` (Linux) + `GOOS=windows go build ./...` + `go vet`. The
> real-nix apply→capture→apply round-trip smoke runs on the Linux dev box.

## 1. Nix capture path

- [x] 1.1 RED tests (`capture_realizer_test.go`, `withFakeRealizer` + scripted `currentSet`):
      a profile of two elements → manifest with `apps` sorted by id, each
      `Refs[runtime.GOOS] = <element name>` (bare attr), `version: 1`, no `configModules`; empty
      profile → valid empty-apps manifest (no error); systemic `Current()` error → envelope error
- [x] 1.2 `internal/commands/capture_realizer.go`: `runCaptureRealizer(flags, r, emitter)` —
      `EmitPhase("capture")`; `r.Current()` (systemic error → `realizerEnvelopeError`, else empty);
      per element (sorted) build `capturedApp{ID, Refs:{GOOS: el.Name}, Name}` + captured ItemEvent
- [x] 1.3 `runCaptureRealizer`: write the manifest exactly like the winget path
      (`captureManifestOutput` → `MarshalIndent` → `resolveOutputPath` → `WriteFile` + INV-CAPTURE-2
      non-empty stat); return `*CaptureResult` (Source = `r.Name()`, no config modules/zip)

## 2. Fork

- [x] 2.1 `internal/commands/capture.go`: add the realizer fork at the top of `RunCapture` (after the
      emitter, before `EmitPhase`), mirroring `RunVerify`; leave the winget path below byte-identical
- [x] 2.2 RED test: Windows guard — `newRealizerFn → ErrNoRealizer` (like `withMockDriver`) keeps the
      winget path (no realizer capture). The winget-path test helpers (`withMockSnapshot`,
      `withMockSnapshotSequence`) were updated to force the no-realizer path so the new fork does not
      divert them to the Nix realizer on linux/darwin.

## 3. --update merge (host-keyed)

- [x] 3.1 RED test: `--update` + `--manifest` with an existing host-keyed manifest → newly captured
      elements appended, no duplicates (dedup on `Refs[runtime.GOOS]`)
- [x] 3.2 `runCaptureRealizer`: merge with the existing manifest, host-keyed, mirroring the winget
      merge but on the `runtime.GOOS` key

## 4. Verification

- [x] 4.1 `cd go-engine && go test ./...` green on Linux
- [x] 4.2 `GOOS=windows go build ./...` + `go vet ./...` clean (winget path untouched)
- [x] 4.3 `npm run openspec:validate` (strict, 59/59) + `npx openspec validate nix-package-capture --strict`
- [x] 4.4 Real-nix round-trip smoke (isolated `ENDSTATE_ROOT` + `ENDSTATE_NIX_PROFILE`): `apply`
      `[jq, ripgrep]` → `capture` → `apply` the captured manifest into a fresh profile →
      `nix profile list` shows the same set. **PASS** — both profiles resolve to
      `legacyPackages.x86_64-linux.{jq,ripgrep}`; the captured manifest emitted bare-attr host-keyed
      refs (`"linux": "jq"`, `"linux": "ripgrep"`) that re-applied cleanly.
