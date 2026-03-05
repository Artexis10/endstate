## 1. Simplify Release Workflow

- [ ] 1.1 Remove the `test` job and `needs: test` dependency from `.github/workflows/release.yml`
- [ ] 1.2 Change runner from `windows-latest` to `ubuntu-latest`
- [ ] 1.3 Remove version stamping, packaging, and zip artifact steps
- [ ] 1.4 Add changelog extraction step using `awk` to parse `## [x.y.z]` section from `CHANGELOG.md`
- [ ] 1.5 Add fallback body ("See CHANGELOG.md") when changelog section is not found
- [ ] 1.6 Update `softprops/action-gh-release` from v1 to v2 with `body_path`, `make_latest: true`, and explicit `prerelease: false`
- [ ] 1.7 Remove zip artifact from release `files` config

## 2. Verify

- [ ] 2.1 Validate the workflow YAML syntax is correct
- [ ] 2.2 Confirm workflow triggers only on `v*` tags
- [ ] 2.3 Confirm no Windows-specific steps remain
