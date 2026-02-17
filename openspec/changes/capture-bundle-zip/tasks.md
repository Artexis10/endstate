# Tasks: Capture Bundle — Zip-Based Profile Packaging

## Implementation Order

1. [x] OpenSpec change artifacts
2. [x] OpenSpec spec (`openspec/specs/capture-bundle-zip.md`)
3. [x] Contract updates (profile-contract, capture-artifact-contract, cli-json-contract)
4. [x] Engine: `engine/bundle.ps1` — config module matcher, config collector, metadata generator, zip bundler
5. [x] Engine: `engine/capture.ps1` — wire bundler into capture pipeline
6. [x] Engine: profile discovery — zip → folder → bare manifest resolution
7. [x] Engine: apply zip integration — extract, apply, cleanup
8. [x] Tests — unit tests for bundler, profile discovery (32/32 passing)
9. [x] `docs/ai/PROJECT_SHADOW.md` — update profile format documentation
