# Tasks: apply-app-subset

## 1. Engine

- [ ] 1.1 `apply.go`: parse/normalize the id list; validate against manifest apps (unknown ids named in the error, zero-selection rejected); reject `--only` + `--prune`; filter the manifest app set before planning
- [ ] 1.2 `cmd/endstate/main.go`: `--only` flag parsing + help text (protected area — this change is the explicit instruction)
- [ ] 1.3 `capabilities.go`: add `--only` to `commands.apply.flags`
- [ ] 1.4 Tests: subset plan/execution counts, dry-run subset, restore scope follows subset, unknown-id error, empty-selection error, only+prune rejection, capabilities flag advert — hermetic, table-driven

## 2. Contract docs

- [ ] 2.1 `docs/contracts/cli-json-contract.md`: apply synopsis + `--only` flag documentation

## 3. Verification

- [ ] 3.1 `cd go-engine && go test ./...`
- [ ] 3.2 `npm run openspec:validate`

## 4. GUI (separate endstate-gui change, after engine ships)

- [ ] 4.1 Per-app checkboxes in setup-flow preview (reuse the save-flow capture-curation checkbox pattern), gated on `commands.apply.flags` containing `--only`
- [ ] 4.2 Pass `--only` with the checked ids on apply
