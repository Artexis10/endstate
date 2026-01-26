# Tasks: sandbox-winget-bootstrap-windowsappruntime

## Implementation Checklist

- [x] Update `openspec/specs/sandbox-validation/spec.md` with WindowsAppRuntime requirement
- [x] Extend `Ensure-Winget` in `sandbox-tests/discovery-harness/sandbox-validate.ps1`:
  - [x] Download Windows App Runtime 1.8 redistributable
  - [x] Install x64 runtime packages via Start-Process with --quiet
  - [x] Log all steps to winget-bootstrap.log
  - [x] Proceed with existing App Installer install
  - [x] Verify winget availability
- [x] Update `docs/VALIDATION.md` troubleshooting section
- [x] Run verification: `.\scripts\sandbox-validate.ps1 -AppId git`
- [x] Confirm winget-bootstrap.log shows runtime install steps
- [x] Archive OpenSpec change
