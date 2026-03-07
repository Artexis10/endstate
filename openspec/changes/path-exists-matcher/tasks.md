# Tasks: pathExists Matcher for Non-Winget Apps

## Implementation Order

1. [x] OpenSpec spec and change artifacts
2. [x] Engine: `Get-ConfigModulesForInstalledApps` — add pathExists matching block
3. [x] Engine: `Test-ConfigModuleSchema` — add optional pathExists validation
4. [x] Module: lightroom-classic — add pathExists to matches
5. [x] Modules: scan all `"winget": []` modules and add pathExists where appropriate
6. [x] Tests: unit tests for pathExists matcher (10/10 passing)
7. [x] Verification: run capture and confirm Lightroom matches via pathExists
8. [x] Version bump to 1.2.0 and changelog update
