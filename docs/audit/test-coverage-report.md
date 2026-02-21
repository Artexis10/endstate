# Engine Test Coverage Audit Report

Generated: 2026-02-22
Agent: TestCoverage2

## Summary

| Metric | Value |
|--------|-------|
| Total engine files | 26 |
| Engine files with direct tests | 18 |
| Engine files without any tests | 8 |
| Total exported functions | ~159 |
| Functions with test references | ~104 |
| Functions with no test coverage | ~55 |
| Test files | 33 |
| Orphaned test files (no engine source) | 0 |

Overall estimated function coverage: **~65%**

---

## Coverage Matrix

### Fully or Well-Covered Engine Files

| Engine File | Functions | Test File(s) | Tested Functions | Coverage |
|-------------|-----------|-------------|------------------|----------|
| `diff.ps1` | 6 | `Diff.Tests.ps1` | Get-ActionKey, Compare-ProvisioningArtifacts, Read-ArtifactFile, ConvertTo-DiffJson | 67% |
| `discovery.ps1` | 6 | `Discovery.Tests.ps1` | Invoke-Discovery, Invoke-PathDetector, Invoke-RegistryUninstallDetector, Add-WingetOwnership, Get-WingetInstalledPackageIds, Write-ManualIncludeTemplate | 100% |
| `events.ps1` | 10 | `Events.Tests.ps1` | Enable-StreamingEvents, Disable-StreamingEvents, Test-StreamingEventsEnabled, Get-Rfc3339Timestamp, Write-StreamingEvent, Write-PhaseEvent, Write-ItemEvent, Write-SummaryEvent, Write-ErrorEvent, Write-ArtifactEvent | 100% |
| `json-output.ps1` | 9 | `JsonSchema.Tests.ps1`, `Verify.Tests.ps1` | Get-EndstateVersion, Get-SchemaVersion, Get-RunId, New-JsonEnvelope, New-JsonError, ConvertTo-JsonOutput, Get-CapabilitiesData, Get-ErrorCode | 89% |
| `paths.ps1` | 6 | `PathResolver.Tests.ps1`, `DriverRegistry.Tests.ps1` | Get-CurrentPlatform, Expand-EndstatePath, Get-HomeDirectory, ConvertTo-BackupPath, Test-IsAbsolutePath, Get-LogicalTokens | 100% |
| `snapshot.ps1` | 6 | `Snapshot.Tests.ps1`, `SandboxDiscovery.Tests.ps1` | Get-FilesystemSnapshot, Compare-FilesystemSnapshots, Test-PathMatchesExcludePattern, Apply-ExcludeHeuristics, ConvertTo-LogicalToken, Get-ExcludePatterns | 100% |
| `trace.ps1` | 9 | `Trace.Tests.ps1` | New-TraceSnapshot, Read-TraceSnapshot, Compare-TraceSnapshots, Get-TraceRootFolders, Merge-TraceRootsToApp, Get-DefaultExcludePatterns, Test-PathMatchesExclude, New-ModuleDraft | 89% |
| `capture.ps1` | 11 | `Capture.Tests.ps1` | Test-IsRuntimePackage, Test-IsStoreApp, Write-RestoreTemplate, Write-VerifyTemplate | 36% |
| `manifest.ps1` | 18 | `Manifest.Tests.ps1`, `ManifestPathResolution.Tests.ps1`, `ProfileContract.Tests.ps1`, `ProfileComposition.Tests.ps1`, `UpdateCapture.Tests.ps1`, `Planner.Tests.ps1`, `Plan.Tests.ps1` | Read-Manifest, Read-ManifestRaw, Read-ManifestInternal, Normalize-Manifest, Test-ProfileManifest, Merge-ManifestsForUpdate, Get-IncludedAppIds, Resolve-RestoreEntriesFromBundles, Resolve-RestoreEntriesFromModules, Write-Manifest, Read-JsoncFile | 61% |
| `profile-commands.ps1` | 7 | `ProfileCommands.Tests.ps1` | Assert-BareProfile, New-ProfileOverlay, Add-ProfileExclusion, Add-ProfileExcludeConfig, Add-ProfileApp, Get-ProfileSummary, Get-ProfileList | 100% |
| `config-modules.ps1` | 10 | `Bundle.Tests.ps1`, `ProfileComposition.Tests.ps1` | Get-ConfigModuleCatalog, Test-ConfigModuleSchema, Expand-ManifestConfigModules, Get-ConfigModulesForInstalledApps, Format-ConfigModuleDiscoveryOutput, Clear-ConfigModuleCatalogCache, Expand-ConfigPath, Test-PathMatchesExcludeGlobs, Invoke-ConfigModuleCapture, Format-ConfigCaptureOutput | ~80% |
| `bundle.ps1` | 7 | `Bundle.Tests.ps1`, `ProfileComposition.Tests.ps1` | Get-MatchedConfigModulesForApps, Invoke-CollectConfigFiles, New-CaptureMetadata, New-CaptureBundle, Expand-ProfileBundle, Remove-ProfileBundleTemp, Resolve-ProfilePath | ~85% |
| `plan.ps1` | 4 | `Plan.Tests.ps1`, `Planner.Tests.ps1`, `Report.Tests.ps1` | Get-ManifestHash (via state.ps1), New-PlanFromManifest, ConvertTo-ReportJson | 50% |
| `restore.ps1` | 13 | `Restore.Tests.ps1`, `RestoreExclude.Tests.ps1`, `RestoreModelB.Tests.ps1`, `ModuleRestore.Tests.ps1` | Invoke-Restore, Get-RestoreActionId, Test-PathExcluded, Invoke-CopyRestoreAction, Test-ProcessesRunning, Test-SharingViolation | 46% |
| `state.ps1` | 5 | `Plan.Tests.ps1`, `Planner.Tests.ps1`, `Report.Tests.ps1` | Get-ManifestHash | 20% |
| `logging.ps1` | 5 | `Restore.Tests.ps1`, `RestoreModelB.Tests.ps1`, `ModuleRestore.Tests.ps1`, `RestoreExclude.Tests.ps1` | Initialize-ProvisioningLog, Write-ProvisioningLog, Write-ProvisioningSection, Close-ProvisioningLog (all as mocks) | 80% (mocked) |
| `export-capture.ps1` | 2 | `ExportConfig.Tests.ps1` | Get-ExportPath, Invoke-ExportCapture | 100% |
| `export-revert.ps1` | 2 | `RestoreModelB.Tests.ps1`, `ModuleRestore.Tests.ps1` | Invoke-ExportRevert | 50% |

### Engine Files With NO Test Coverage

| Engine File | Functions | Risk Level | Notes |
|-------------|-----------|------------|-------|
| `apply.ps1` | 2 (Invoke-Apply, Invoke-ApplyFromPlan) | **HIGH** | Core orchestration -- drives the entire install pipeline. No direct unit tests found. `ApplyFromPlan.Tests.ps1` exists but does not call these functions directly. |
| `verify.ps1` | 2 (Invoke-Verify, Invoke-VerifyItem) | **HIGH** | Core verification pipeline. `Verify.Tests.ps1` tests json-output functions used by verify but does not test Invoke-Verify or Invoke-VerifyItem directly. |
| `external.ps1` | 8 (Invoke-WingetList, Invoke-WingetInstall, etc.) | **MEDIUM** | External command wrappers. These call real system binaries (winget, registry) so unit testing requires heavy mocking. Functions are referenced as mocks in Discovery.Tests.ps1. |
| `parallel.ps1` | 4 (Test-AppParallelSafe, Split-AppsForParallel, etc.) | **MEDIUM** | Parallel install orchestration. No tests found. |
| `progress.ps1` | 14 (New-ProgressState, Show-Progress, etc.) | **LOW** | UI/progress bar display functions. Low risk but high function count. |
| `report.ps1` | 9 (Get-ProvisioningReport, Format-ReportJson, etc.) | **MEDIUM** | Report.Tests.ps1 tests ConvertTo-ReportJson (from plan.ps1) and Get-ProvisioningReport + Format-ReportJson extensively. Other report functions (Get-ReportVersion, Get-ReportGitSha, Get-StateFiles, Read-StateFile, Format-ReportHuman, Format-ReportCompact, Write-ReportHuman) are untested. |
| `shim-template.ps1` | 1 (Get-RepoRootPath) | **LOW** | Simple path resolution utility. |
| `export-validate.ps1` | 2 (Test-PathWritable, Invoke-ExportValidate) | **MEDIUM** | Export validation path. No test coverage found. |

---

## Detailed Function Coverage

### diff.ps1 (4/6 tested)
- [x] `Get-ActionKey` -- Diff.Tests.ps1
- [x] `Compare-ProvisioningArtifacts` -- Diff.Tests.ps1
- [x] `Read-ArtifactFile` -- Diff.Tests.ps1
- [ ] `Resolve-RunIdToPath` -- **UNTESTED**
- [ ] `Format-DiffOutput` -- **UNTESTED**
- [x] `ConvertTo-DiffJson` -- Diff.Tests.ps1

### discovery.ps1 (6/6 tested)
- [x] `Invoke-Discovery` -- Discovery.Tests.ps1
- [x] `Invoke-PathDetector` -- Discovery.Tests.ps1
- [x] `Invoke-RegistryUninstallDetector` -- Discovery.Tests.ps1
- [x] `Add-WingetOwnership` -- Discovery.Tests.ps1
- [x] `Get-WingetInstalledPackageIds` -- Discovery.Tests.ps1 (indirectly)
- [x] `Write-ManualIncludeTemplate` -- Discovery.Tests.ps1

### events.ps1 (10/10 tested)
- [x] `Enable-StreamingEvents` -- Events.Tests.ps1
- [x] `Disable-StreamingEvents` -- Events.Tests.ps1
- [x] `Test-StreamingEventsEnabled` -- Events.Tests.ps1
- [x] `Get-Rfc3339Timestamp` -- Events.Tests.ps1
- [x] `Write-StreamingEvent` -- Events.Tests.ps1
- [x] `Write-PhaseEvent` -- Events.Tests.ps1
- [x] `Write-ItemEvent` -- Events.Tests.ps1
- [x] `Write-SummaryEvent` -- Events.Tests.ps1
- [x] `Write-ErrorEvent` -- Events.Tests.ps1
- [x] `Write-ArtifactEvent` -- Events.Tests.ps1

### export-capture.ps1 (2/2 tested)
- [x] `Get-ExportPath` -- ExportConfig.Tests.ps1
- [x] `Invoke-ExportCapture` -- ExportConfig.Tests.ps1

### export-revert.ps1 (1/2 tested)
- [ ] `Get-LastRestoreJournal` -- **UNTESTED**
- [x] `Invoke-ExportRevert` -- RestoreModelB.Tests.ps1, ModuleRestore.Tests.ps1

### export-validate.ps1 (0/2 tested)
- [ ] `Test-PathWritable` -- **UNTESTED**
- [ ] `Invoke-ExportValidate` -- **UNTESTED**

### external.ps1 (0/8 directly tested, used as mocks)
- [ ] `Invoke-WingetList` -- **UNTESTED** (mocked in other tests)
- [ ] `Invoke-WingetInstall` -- **UNTESTED** (mocked in other tests)
- [ ] `Invoke-WingetExportWrapper` -- **UNTESTED**
- [ ] `Test-CommandExists` -- **UNTESTED** (mocked in Verify.Tests.ps1)
- [ ] `Get-RegistryValue` -- **UNTESTED**
- [ ] `Get-CommandInfo` -- **UNTESTED** (mocked in Discovery.Tests.ps1)
- [ ] `Get-CommandVersion` -- **UNTESTED** (mocked in Discovery.Tests.ps1)
- [ ] `Get-RegistryUninstallEntries` -- **UNTESTED** (mocked in Discovery.Tests.ps1)

### json-output.ps1 (8/9 tested)
- [x] `Get-EndstateVersion` -- JsonSchema.Tests.ps1
- [x] `Get-SchemaVersion` -- JsonSchema.Tests.ps1
- [x] `Get-RunId` -- JsonSchema.Tests.ps1, Plan.Tests.ps1
- [x] `New-JsonEnvelope` -- JsonSchema.Tests.ps1, Verify.Tests.ps1
- [x] `New-JsonError` -- JsonSchema.Tests.ps1, Verify.Tests.ps1
- [x] `ConvertTo-JsonOutput` -- JsonSchema.Tests.ps1
- [ ] `Write-JsonOutput` -- **UNTESTED**
- [x] `Get-CapabilitiesData` -- JsonSchema.Tests.ps1
- [x] `Get-ErrorCode` -- JsonSchema.Tests.ps1, Verify.Tests.ps1

### logging.ps1 (4/5 tested, all via mocks)
- [x] `Initialize-ProvisioningLog` -- mocked in Restore/RestoreModelB/ModuleRestore/RestoreExclude
- [x] `Write-ProvisioningLog` -- mocked in Restore/RestoreModelB/ModuleRestore/RestoreExclude
- [x] `Write-ProvisioningSection` -- mocked in Restore/RestoreModelB/ModuleRestore/RestoreExclude
- [x] `Close-ProvisioningLog` -- mocked in Restore/RestoreModelB/ModuleRestore/RestoreExclude
- [ ] `Get-RunId` -- **UNTESTED** (note: json-output.ps1 also has a Get-RunId; the logging version may shadow it)

### parallel.ps1 (0/4 tested)
- [ ] `Test-AppParallelSafe` -- **UNTESTED**
- [ ] `Get-ParallelUnsafePatterns` -- **UNTESTED**
- [ ] `Invoke-ParallelAppInstall` -- **UNTESTED**
- [ ] `Split-AppsForParallel` -- **UNTESTED**

### paths.ps1 (6/6 tested)
- [x] `Get-CurrentPlatform` -- PathResolver.Tests.ps1, DriverRegistry.Tests.ps1
- [x] `Expand-EndstatePath` -- PathResolver.Tests.ps1
- [x] `Get-HomeDirectory` -- PathResolver.Tests.ps1
- [x] `ConvertTo-BackupPath` -- PathResolver.Tests.ps1
- [x] `Test-IsAbsolutePath` -- PathResolver.Tests.ps1
- [x] `Get-LogicalTokens` -- PathResolver.Tests.ps1

### plan.ps1 (2/4 tested)
- [ ] `Invoke-Plan` -- **UNTESTED** (orchestrator; calls New-PlanFromManifest)
- [ ] `Get-InstalledAppsFromWinget` -- **UNTESTED**
- [x] `New-PlanFromManifest` -- Plan.Tests.ps1, Planner.Tests.ps1, Report.Tests.ps1
- [x] `ConvertTo-ReportJson` -- Report.Tests.ps1

### progress.ps1 (0/14 tested)
- [ ] `New-ProgressEventQueue` -- **UNTESTED**
- [ ] `Add-ProgressEvent` -- **UNTESTED**
- [ ] `Get-ProgressEvents` -- **UNTESTED**
- [ ] `New-ProgressState` -- **UNTESTED**
- [ ] `Update-ProgressState` -- **UNTESTED**
- [ ] `Get-ProgressBar` -- **UNTESTED**
- [ ] `Format-ProgressLine` -- **UNTESTED**
- [ ] `Format-RunningApps` -- **UNTESTED**
- [ ] `Show-Progress` -- **UNTESTED**
- [ ] `Clear-Progress` -- **UNTESTED**
- [ ] `Start-ProgressTracking` -- **UNTESTED**
- [ ] `Update-ProgressTracking` -- **UNTESTED**
- [ ] `Complete-ProgressTracking` -- **UNTESTED**

Note: progress.ps1 has 14 functions but the 14th (unnamed helper) is internal.

### report.ps1 (2/9 tested)
- [ ] `Get-ReportVersion` -- **UNTESTED**
- [ ] `Get-ReportGitSha` -- **UNTESTED**
- [ ] `Get-StateFiles` -- **UNTESTED**
- [ ] `Read-StateFile` -- **UNTESTED**
- [x] `Get-ProvisioningReport` -- Report.Tests.ps1
- [ ] `Format-ReportHuman` -- **UNTESTED**
- [ ] `Format-ReportCompact` -- **UNTESTED**
- [x] `Format-ReportJson` -- Report.Tests.ps1
- [ ] `Write-ReportHuman` -- **UNTESTED**

### restore.ps1 (6/13 tested)
- [x] `Get-RestoreActionId` -- Restore.Tests.ps1
- [ ] `Test-SensitivePath` -- **UNTESTED**
- [ ] `Expand-RestorePath` -- **UNTESTED**
- [ ] `Test-FileUpToDate` -- **UNTESTED**
- [ ] `Test-IsElevated` -- **UNTESTED**
- [x] `Test-ProcessesRunning` -- ModuleRestore.Tests.ps1
- [ ] `Invoke-RestoreAction` -- **UNTESTED** (internal dispatcher)
- [x] `Test-PathExcluded` -- RestoreExclude.Tests.ps1
- [x] `Test-SharingViolation` -- ModuleRestore.Tests.ps1
- [ ] `Copy-DirectoryWithExcludes` -- **UNTESTED**
- [x] `Invoke-CopyRestoreAction` -- RestoreExclude.Tests.ps1, ModuleRestore.Tests.ps1
- [ ] `Backup-RestoreTarget` -- **UNTESTED**
- [x] `Invoke-Restore` -- RestoreModelB.Tests.ps1, ModuleRestore.Tests.ps1, Restore.Tests.ps1

### shim-template.ps1 (0/1 tested)
- [ ] `Get-RepoRootPath` -- **UNTESTED**

### snapshot.ps1 (6/6 tested)
- [x] `Get-FilesystemSnapshot` -- Snapshot.Tests.ps1, SandboxDiscovery.Tests.ps1
- [x] `Compare-FilesystemSnapshots` -- Snapshot.Tests.ps1, SandboxDiscovery.Tests.ps1
- [x] `Test-PathMatchesExcludePattern` -- Snapshot.Tests.ps1, SandboxDiscovery.Tests.ps1
- [x] `Apply-ExcludeHeuristics` -- Snapshot.Tests.ps1, SandboxDiscovery.Tests.ps1
- [x] `ConvertTo-LogicalToken` -- Snapshot.Tests.ps1, SandboxDiscovery.Tests.ps1
- [x] `Get-ExcludePatterns` -- Snapshot.Tests.ps1, SandboxDiscovery.Tests.ps1

### state.ps1 (1/5 tested)
- [x] `Get-ManifestHash` -- Plan.Tests.ps1, Planner.Tests.ps1, Report.Tests.ps1
- [ ] `Get-ExpandedManifestHash` -- **UNTESTED**
- [ ] `Save-RunState` -- **UNTESTED**
- [ ] `Get-LastRunState` -- **UNTESTED**
- [ ] `Get-RunHistory` -- **UNTESTED**

### trace.ps1 (8/9 tested)
- [x] `New-TraceSnapshot` -- Trace.Tests.ps1
- [x] `Read-TraceSnapshot` -- Trace.Tests.ps1
- [x] `Compare-TraceSnapshots` -- Trace.Tests.ps1
- [x] `Get-TraceRootFolders` -- Trace.Tests.ps1
- [x] `Merge-TraceRootsToApp` -- Trace.Tests.ps1
- [x] `Get-DefaultExcludePatterns` -- Trace.Tests.ps1
- [x] `Test-PathMatchesExclude` -- Trace.Tests.ps1
- [x] `New-ModuleDraft` -- Trace.Tests.ps1
- [ ] `ConvertTo-ModuleJsonc` -- **UNTESTED**

### verify.ps1 (0/2 tested)
- [ ] `Invoke-Verify` -- **UNTESTED**
- [ ] `Invoke-VerifyItem` -- **UNTESTED**

### capture.ps1 (4/11 tested)
- [ ] `Invoke-Capture` -- **UNTESTED** (orchestrator)
- [ ] `Test-WingetAvailable` -- **UNTESTED**
- [ ] `Invoke-WingetExport` -- **UNTESTED**
- [ ] `Get-InstalledAppsViaWingetList` -- **UNTESTED** (referenced in comments)
- [ ] `Get-InstalledAppsViaWinget` -- **UNTESTED**
- [ ] `Test-SensitivePaths` -- **UNTESTED**
- [x] `Test-IsRuntimePackage` -- Capture.Tests.ps1
- [x] `Test-IsStoreApp` -- Capture.Tests.ps1
- [x] `Write-RestoreTemplate` -- Capture.Tests.ps1
- [x] `Write-VerifyTemplate` -- Capture.Tests.ps1

### apply.ps1 (0/2 tested)
- [ ] `Invoke-Apply` -- **UNTESTED**
- [ ] `Invoke-ApplyFromPlan` -- **UNTESTED**

### manifest.ps1 (11/18 tested)
- [x] `Resolve-RestoreEntriesFromBundles` -- ModuleRestore.Tests.ps1
- [x] `Resolve-RestoreEntriesFromModules` -- ModuleRestore.Tests.ps1
- [x] `Read-Manifest` -- Manifest.Tests.ps1, Plan.Tests.ps1, many others
- [x] `Read-ManifestInternal` -- ProfileComposition.Tests.ps1
- [ ] `Remove-JsoncComments` -- **UNTESTED** (internal helper, exercised indirectly via Read-Manifest)
- [x] `Read-JsoncFile` -- Manifest.Tests.ps1 (indirectly via Read-Manifest)
- [ ] `ConvertFrom-Jsonc` -- **UNTESTED** (internal helper)
- [ ] `Convert-PsObjectToHashtable` -- **UNTESTED** (internal helper)
- [ ] `Resolve-ManifestIncludes` -- **UNTESTED** (exercised indirectly via Read-Manifest with includes)
- [x] `Normalize-Manifest` -- ProfileComposition.Tests.ps1
- [x] `Write-Manifest` -- ProfileCommands.Tests.ps1 (indirect via profile mutation)
- [ ] `ConvertTo-Jsonc` -- **UNTESTED** (internal helper for Write-Manifest)
- [ ] `ConvertFrom-SimpleYaml` -- **UNTESTED**
- [ ] `ConvertTo-SimpleYaml` -- **UNTESTED**
- [x] `Read-ManifestRaw` -- ProfileCommands.Tests.ps1, Bundle.Tests.ps1
- [x] `Get-IncludedAppIds` -- UpdateCapture.Tests.ps1
- [x] `Merge-ManifestsForUpdate` -- UpdateCapture.Tests.ps1
- [x] `Test-ProfileManifest` -- ProfileContract.Tests.ps1

### profile-commands.ps1 (7/7 tested)
- [x] `Assert-BareProfile` -- ProfileCommands.Tests.ps1
- [x] `New-ProfileOverlay` -- ProfileCommands.Tests.ps1
- [x] `Add-ProfileExclusion` -- ProfileCommands.Tests.ps1
- [x] `Add-ProfileExcludeConfig` -- ProfileCommands.Tests.ps1
- [x] `Add-ProfileApp` -- ProfileCommands.Tests.ps1
- [x] `Get-ProfileSummary` -- ProfileCommands.Tests.ps1
- [x] `Get-ProfileList` -- ProfileCommands.Tests.ps1

### config-modules.ps1 (10/10 tested)
- [x] `Get-ConfigModuleCatalog` -- Bundle.Tests.ps1, ProfileComposition.Tests.ps1
- [x] `Test-ConfigModuleSchema` -- Bundle.Tests.ps1
- [x] `Expand-ManifestConfigModules` -- ProfileComposition.Tests.ps1
- [x] `Get-ConfigModulesForInstalledApps` -- Bundle.Tests.ps1
- [x] `Format-ConfigModuleDiscoveryOutput` -- Bundle.Tests.ps1
- [x] `Clear-ConfigModuleCatalogCache` -- Bundle.Tests.ps1
- [x] `Expand-ConfigPath` -- Bundle.Tests.ps1, ProfileComposition.Tests.ps1
- [x] `Test-PathMatchesExcludeGlobs` -- Bundle.Tests.ps1, ProfileComposition.Tests.ps1
- [x] `Invoke-ConfigModuleCapture` -- Bundle.Tests.ps1
- [x] `Format-ConfigCaptureOutput` -- Bundle.Tests.ps1

### bundle.ps1 (7/7 tested)
- [x] `Get-MatchedConfigModulesForApps` -- Bundle.Tests.ps1
- [x] `Invoke-CollectConfigFiles` -- Bundle.Tests.ps1
- [x] `New-CaptureMetadata` -- Bundle.Tests.ps1
- [x] `New-CaptureBundle` -- Bundle.Tests.ps1
- [x] `Expand-ProfileBundle` -- Bundle.Tests.ps1, ProfileComposition.Tests.ps1
- [x] `Remove-ProfileBundleTemp` -- Bundle.Tests.ps1
- [x] `Resolve-ProfilePath` -- Bundle.Tests.ps1, ProfileComposition.Tests.ps1

---

## Test File to Engine File Mapping

| Test File | Engine File(s) Tested | Type |
|-----------|----------------------|------|
| `ApplyFromPlan.Tests.ps1` | (integration-style, no direct engine dot-source) | Indirect |
| `Bundle.Tests.ps1` | bundle.ps1, config-modules.ps1, manifest.ps1, logging.ps1 | Direct |
| `Capture.Tests.ps1` | capture.ps1 | Direct |
| `Curate.Tests.ps1` | (integration-style) | Indirect |
| `Diff.Tests.ps1` | diff.ps1 | Direct |
| `Discovery.Tests.ps1` | discovery.ps1, external.ps1 (mocked) | Direct |
| `DriverRegistry.Tests.ps1` | paths.ps1 | Direct |
| `Events.Tests.ps1` | events.ps1 | Direct |
| `ExportConfig.Tests.ps1` | export-capture.ps1, manifest.ps1, logging.ps1, state.ps1, restore.ps1, events.ps1 | Direct |
| `GitModule.Tests.ps1` | (module-level, no engine dot-source) | Indirect |
| `JsonMode.Tests.ps1` | (integration-style) | Indirect |
| `JsonSchema.Tests.ps1` | json-output.ps1 | Direct |
| `Manifest.Tests.ps1` | manifest.ps1 | Direct |
| `ManifestPathResolution.Tests.ps1` | manifest.ps1 (Resolve-ManifestPath) | Direct |
| `MergeStrategies.Tests.ps1` | (restorer-level, not engine) | Indirect |
| `ModuleCli.Tests.ps1` | (CLI-level) | Indirect |
| `ModuleRestore.Tests.ps1` | manifest.ps1, restore.ps1, logging.ps1, export-revert.ps1 | Direct |
| `PathResolver.Tests.ps1` | paths.ps1 | Direct |
| `Plan.Tests.ps1` | plan.ps1, state.ps1, json-output.ps1, manifest.ps1 | Direct |
| `Planner.Tests.ps1` | plan.ps1, state.ps1, manifest.ps1 | Direct |
| `ProfileCommands.Tests.ps1` | profile-commands.ps1, manifest.ps1, bundle.ps1, logging.ps1 | Direct |
| `ProfileComposition.Tests.ps1` | config-modules.ps1, manifest.ps1 | Direct |
| `ProfileContract.Tests.ps1` | manifest.ps1 (Test-ProfileManifest) | Direct |
| `Report.Tests.ps1` | plan.ps1, state.ps1, manifest.ps1, report.ps1 | Direct |
| `Restore.Tests.ps1` | restore.ps1, manifest.ps1, logging.ps1, state.ps1 | Direct |
| `RestoreExclude.Tests.ps1` | restore.ps1 | Direct |
| `RestoreModelB.Tests.ps1` | restore.ps1, manifest.ps1, logging.ps1, state.ps1, export-capture.ps1 | Direct |
| `SandboxDiscovery.Tests.ps1` | snapshot.ps1 (export validation) | Direct |
| `SandboxHarness.Tests.ps1` | (integration-style) | Indirect |
| `Snapshot.Tests.ps1` | snapshot.ps1 | Direct |
| `Trace.Tests.ps1` | trace.ps1 | Direct |
| `UpdateCapture.Tests.ps1` | manifest.ps1 (Merge-ManifestsForUpdate, Get-IncludedAppIds) | Direct |
| `Verify.Tests.ps1` | json-output.ps1, verify.ps1 (partial) | Direct |

---

## Priority Recommendations

### P0 -- Critical (core pipeline with zero coverage)

1. **`apply.ps1`** -- `Invoke-Apply` and `Invoke-ApplyFromPlan` are the core install orchestration entry points. These have no direct unit test coverage at all. `ApplyFromPlan.Tests.ps1` exists but appears to test at integration level without dot-sourcing apply.ps1.

2. **`verify.ps1`** -- `Invoke-Verify` and `Invoke-VerifyItem` are the core verification pipeline. `Verify.Tests.ps1` tests JSON output helpers but never calls the verify functions themselves.

### P1 -- High (important functions with gaps)

3. **`state.ps1`** -- Only `Get-ManifestHash` is tested. `Save-RunState`, `Get-LastRunState`, `Get-RunHistory`, and `Get-ExpandedManifestHash` have no coverage. State persistence is critical for idempotency.

4. **`restore.ps1` internal functions** -- While `Invoke-Restore` is well-tested, safety-critical functions `Test-SensitivePath`, `Expand-RestorePath`, `Test-FileUpToDate`, `Test-IsElevated`, `Backup-RestoreTarget`, and `Copy-DirectoryWithExcludes` have no direct tests.

5. **`capture.ps1` orchestrator functions** -- `Invoke-Capture`, `Test-WingetAvailable`, `Invoke-WingetExport`, `Get-InstalledAppsViaWingetList`, `Get-InstalledAppsViaWinget`, and `Test-SensitivePaths` are untested. Only filter/template functions are covered.

6. **`export-validate.ps1`** -- `Test-PathWritable` and `Invoke-ExportValidate` have no coverage.

### P2 -- Medium (utility gaps)

7. **`parallel.ps1`** -- All 4 functions untested. Parallel install safety logic should be validated.

8. **`report.ps1` presentation functions** -- `Get-ReportVersion`, `Get-ReportGitSha`, `Get-StateFiles`, `Read-StateFile`, `Format-ReportHuman`, `Format-ReportCompact`, `Write-ReportHuman` are untested.

9. **`manifest.ps1` internal helpers** -- `Remove-JsoncComments`, `ConvertFrom-Jsonc`, `Convert-PsObjectToHashtable`, `Resolve-ManifestIncludes`, `ConvertTo-Jsonc`, `ConvertFrom-SimpleYaml`, `ConvertTo-SimpleYaml` lack direct tests. Some are exercised indirectly.

10. **`external.ps1`** -- All 8 functions are untested directly (they are system wrappers, so coverage requires mocking, but their logic should still be verified).

### P3 -- Low

11. **`progress.ps1`** -- 14 functions, all untested. Low-risk UI code.

12. **`shim-template.ps1`** -- 1 function, simple utility.

13. **`diff.ps1`** -- `Resolve-RunIdToPath` and `Format-DiffOutput` untested.

---

## Observations

1. **Well-tested subsystems**: events, paths, snapshot, trace, profile-commands, config-modules, bundle, and discovery have excellent test coverage (80-100%).

2. **Core pipeline gaps**: The main pipeline stages (apply, verify) lack direct unit tests. These are the highest-risk gaps.

3. **Mocked-only coverage**: logging.ps1 functions appear in many test files but only as mocks (behavior is suppressed). No test verifies their actual output behavior.

4. **Internal helpers**: Several manifest.ps1 internal functions (Remove-JsoncComments, ConvertFrom-Jsonc, etc.) are exercised indirectly through Read-Manifest calls but have no isolation tests. If these break, failures will be hard to localize.

5. **State persistence**: state.ps1 is critically under-tested. Only hash computation is covered; the save/load/history cycle is not.

6. **Test file naming**: Test files generally follow `<Topic>.Tests.ps1` naming but don't always map 1:1 to engine files. Some engine files are covered across multiple test files (e.g., manifest.ps1 is tested by 7+ test files).
