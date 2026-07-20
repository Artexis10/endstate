# Tasks: capture-share-mode

## 1. Share flag

- [x] 1.1 `CaptureFlags.Share`; `validateShareFlags` rejecting `--share` without `--only` and
      `--share` with `--sanitize`, before any capture work
- [x] 1.2 `main.go` parse + help; `capabilities.go` advertises `--share` (PROTECTED areas,
      modified under explicit instruction)
- [x] 1.3 Tests: all five validation cases

## 2. Bundle metadata

- [x] 2.1 `BundleMetadata` gains `OS`, `Share`, `Name` (additive, `omitempty`)
- [x] 2.2 `CaptureBundleRequest` gains `Share`, `Name`; threaded from capture flags
- [x] 2.3 `machineName` blanked in share mode

## 3. Merge-preferring restore

- [x] 3.1 `preferMergeForShare` — forces backup, retypes conservatively
- [x] 3.2 JSON: retype only for a strict JSON **object**. Unmarshalling into
      `map[string]interface{}` rejects JSONC *and* arrays/scalars in one step
- [x] 3.3 INI: retype only for `.ini`, never git config (duplicate-key collapse)
- [x] 3.4 Declared types left alone
- [x] 3.5 Tests: object merges; JSONC stays copy; **array stays copy**; scalar/null stay copy;
      `.ini` merges; `.gitconfig`/`gitconfig`/`config` stay copy; declared types preserved;
      unreadable payload stays copy

## 4. Cross-OS refusal

- [x] 4.1 `refuseCrossOSBundle` + `rebuildGOOSFn` seam; wired after `readBundleMetadata`
- [x] 4.2 Tests: same OS proceeds; both cross-OS directions refused with `NOT_SUPPORTED`;
      bundle without a recorded OS accepted

## 5. Verification

- [x] 5.1 `go build ./...`, `go vet ./...` clean; touched files gofmt-clean and LF
- [x] 5.2 Full suite `go test ./...` — 0 failures
- [x] 5.3 End-to-end: `--share` without `--only` rejected with `MANIFEST_VALIDATION_ERROR`;
      a real share bundle records `os=windows`, `share=true`, `name`, empty `machineName`
- [x] 5.4 End-to-end retyping across obsidian + VSCodium: `obsidian.json` and `settings.json`
      became `merge-json`; `keybindings.json`, `tasks.json` and a directory stayed `copy`;
      every entry `backup=true`
- [x] 5.5 `npm run openspec:validate`

## 6. Not in this change

- [x] 6.1 **Redaction — DONE.** Three layers (account-bound module deny list, pattern pass,
      git identity strip) plus a `metadata.redaction` report naming every payload that could
      not be decoded. Known limits are documented in the contract and asserted in tests:
      bare usernames outside a path context, licence-key shapes, non-`Users` drive paths,
      and undecodable payloads.
- [ ] 6.2 Recipient-side backup directory: when no repo root resolves, restorers fall back to a
      CWD-relative `state/backups/<runID>`, so a recipient's pre-overwrite backups land wherever
      they happened to run from. Share mode forces backup on, which makes this more visible.
- [ ] 6.3 Dead flags `--discover` / `--minimize` still parsed and advertised but never read.
