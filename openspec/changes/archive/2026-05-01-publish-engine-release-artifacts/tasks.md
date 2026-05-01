## 1. Workflow — Add publish-artifacts job to release.yml

- [x] 1.1 Add `publish-artifacts` job to `.github/workflows/release.yml` that depends on the existing release creation step and runs on `windows-latest`
- [x] 1.2 Add `actions/checkout@v5` and `actions/setup-go@v5` (pin Go 1.22) steps to the new job
- [x] 1.3 Add build step: `go build -ldflags "-X .../config.version=${VERSION} -X .../config.schemaVersion=${SCHEMA_VERSION}" -o endstate.exe ./cmd/endstate` where `VERSION` is `${{ github.ref_name }}` with the `v` prefix stripped
- [x] 1.4 Add checksum step: `CertUtil -hashfile endstate.exe SHA256 | findstr /v "hash\|CertUtil" > endstate.exe.sha256`
- [x] 1.5 Add upload step using `softprops/action-gh-release@v2` with `tag_name: ${{ github.ref_name }}` and `files: endstate.exe\nendstate.exe.sha256`
- [x] 1.6 Add post-upload verify step using `gh release view ${{ github.ref_name }} --json assets` piped through `jq` to assert both filenames present; exit 1 if either is missing
- [x] 1.7 Ensure the job has `permissions: contents: write` and `GITHUB_TOKEN` available for both upload and `gh` CLI operations

## 2. Spec — Update auto-release-on-tag main spec

- [x] 2.1 In `openspec/specs/auto-release-on-tag/spec.md`, remove the "No release artifacts" requirement
- [x] 2.2 Add the "Binary artifacts attached to every release" requirement (as defined in the delta spec) to `openspec/specs/auto-release-on-tag/spec.md`

## 3. Spec — Create engine-release-artifact-publish main spec

- [x] 3.1 Create `openspec/specs/engine-release-artifact-publish/spec.md` with all five requirements from the change spec (binary artifact, checksum, post-upload verification, stable URLs, windows-latest runner)

## 4. Verification

- [x] 4.1 Create a test tag `v0.0.0-test` on a branch (or via workflow_dispatch) and confirm the workflow runs end-to-end — `endstate.exe` and `endstate.exe.sha256` appear in the release assets
- [x] 4.2 Download `endstate.exe` from the release URL and run `endstate --version` (or `endstate capabilities --json`) to confirm the embedded version matches the tag
- [x] 4.3 Download `endstate.exe.sha256` and verify the hex digest matches a local `sha256sum` of `endstate.exe`
- [x] 4.4 Confirm `https://github.com/Artexis10/endstate/releases/download/v{VERSION}/endstate.exe` returns HTTP 200 (not a redirect loop or 404)
- [x] 4.5 Delete the test tag and its release after verification
