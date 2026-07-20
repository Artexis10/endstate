# Tasks: unigetui-import

## 1. Parser (internal/importer)

- [ ] 1.1 Create `internal/importer` package with `.ubundle` types (`Bundle`, `Package`, `InstallOptions`, `IncompatiblePackage`) matching UniGetUI's SerializableBundle v3 JSON
- [ ] 1.2 Implement `ParseUniGetUI(r io.Reader)` — strict on required fields, tolerant of unknown fields, warning (not error) on `export_version != 3`
- [ ] 1.3 Add a real-world fixture `.ubundle` (anonymized, multi-manager: winget + chocolatey + pip + incompatible_packages) under `tests/fixtures/`
- [ ] 1.4 Parser unit tests: valid v3, future version warning, malformed JSON error, missing-fields behavior

## 2. Mapper

- [ ] 2.1 Implement winget-package → manifest-app mapping (Id → refs.windows, Name → displayName, deterministic slug for app id with collision de-dup)
- [ ] 2.2 Implement `--pin` policy: InstallationOptions.Version wins over observed Version; no version field without `--pin`
- [ ] 2.3 Implement skip report: non-winget managers with reasons, incompatible_packages pass-through, slug collisions noted
- [ ] 2.4 Mapper unit tests: mapping table, pin precedence, no-pin default, skip transparency (no silent drops), deterministic output byte-equality

## 3. Command + wiring

- [ ] 3.1 Implement `commands.RunImport(Flags)`: `--from unigetui`, `--path`, `--out` (default `manifests/local/imported-unigetui.jsonc`), `--pin`, `--json` envelope with imported/skipped/incompatible counts
- [ ] 3.2 JSONC emission with generated header comment (source file, tool version) and manifest-loader round-trip validity gate before write
- [ ] 3.3 Register `import` command in `cmd/endstate/main.go` dispatcher (protected area — explicit instruction 2026-07-10)
- [ ] 3.4 Command tests: end-to-end fixture → manifest loads via `manifest.LoadManifest`; abort-on-invalid; summary contents

## 4. Verification + docs

- [ ] 4.1 Run `cd go-engine && go test ./internal/importer/... ./internal/commands/...` and full `go test ./...`
- [ ] 4.2 Run `npm run openspec:validate`
- [ ] 4.3 Manual check: import a real UniGetUI backup, `endstate plan` against the result, confirm module catalog matching lights up config modules for known apps
- [ ] 4.4 README: short "Import from UniGetUI" section (feeds the interop Discussion post)
