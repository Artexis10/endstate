## Context

The release workflow (`.github/workflows/release.yml`) currently contains a full test suite run, version stamping, zip artifact packaging, and GitHub Release creation — all on a `windows-latest` runner. Unit tests already run on every push via the CI workflow, making the release test job redundant. The zip artifact is unused since users install via `endstate bootstrap` (git clone). The workflow can be reduced to a single job that creates a GitHub Release with changelog-derived notes.

## Goals / Non-Goals

**Goals:**
- Simplify the release workflow to a single lightweight job
- Automatically populate release notes from `CHANGELOG.md`
- Run on `ubuntu-latest` for faster startup and lower cost
- Use `softprops/action-gh-release@v2` (current version)

**Non-Goals:**
- Adding new release artifact types (MSI, installer, etc.)
- Automated changelog generation (changelog is maintained manually)
- Automated version bumping or tagging

## Decisions

**1. Remove test job from release workflow**
- Rationale: Tests already run on push/PR via CI. Running them again on tag push adds latency without value.
- Alternative: Keep tests as a gate. Rejected because it duplicates CI and delays releases.

**2. Switch from `windows-latest` to `ubuntu-latest`**
- Rationale: No remaining Windows-specific steps. Ubuntu runners start faster and cost less.
- Alternative: Keep Windows. Rejected — no PowerShell execution needed in the workflow.

**3. Extract changelog section via `awk`**
- Rationale: Simple, no dependencies. Parses `## [x.y.z]` headings to find the matching version block.
- Alternative: Use a changelog parsing action. Rejected — adds an external dependency for a trivial operation.

**4. Fallback body when changelog section is missing**
- Rationale: Prevents empty release bodies if the changelog isn't updated before tagging.
- Fallback text: "See CHANGELOG.md"

**5. Drop zip artifact attachment**
- Rationale: Users install via `endstate bootstrap` (git clone), not zip download. The artifact was unused.
- Alternative: Keep generating it. Rejected — unnecessary build complexity.

## Risks / Trade-offs

- [No pre-release gating on tests] → Tests run on CI before merge; only tag-push is unguarded. Mitigated by tagging only after CI passes on main.
- [No release artifact] → Users who relied on zip downloads (if any) lose that option. Mitigated by the fact that bootstrap is the documented install method.
- [Changelog must be manually maintained] → If changelog section is missing, release body falls back to "See CHANGELOG.md". Acceptable trade-off for simplicity.
