## 1. Host-Side Scripts

- [x] 1.1 Create `scripts/sandbox-validate.ps1` (host-side single-app validation)
  - Inputs: `-AppId` (required), `-WingetId` (optional), `-OutDir` (optional)
  - Generates .wsb file and launches Windows Sandbox
  - Waits for DONE.txt or ERROR.txt sentinel
  - Prints one-line PASS/FAIL summary with artifact path

- [x] 1.2 Create `scripts/sandbox-validate-batch.ps1` (host-side batch runner)
  - Reads `sandbox-tests/golden-queue.jsonc`
  - Runs each app sequentially via `sandbox-validate.ps1`
  - Writes `sandbox-tests/validation/summary.json` and `summary.md`

## 2. Configuration Files

- [x] 2.1 Create `sandbox-tests/golden-queue.jsonc` with existing modules:
  - git (Git.Git)
  - vscodium (VSCodium.VSCodium)
  - powertoys (Microsoft.PowerToys)
  - msi-afterburner (Guru3D.Afterburner)

- [x] 2.2 Create `sandbox-tests/validation/.gitkeep` placeholder

## 3. Documentation

- [x] 3.1 Create `docs/VALIDATION.md` documenting:
  - Single-app validation usage
  - Batch validation usage
  - Expected outputs and artifact structure
  - OpenSpec validate/archive commands

## 4. Verification

- [x] 4.1 Run `openspec validate sandbox-validation-loop --strict`
- [x] 4.2 Test single-app: `.\scripts\sandbox-validate.ps1 -AppId git`
  - Script runs correctly, launches sandbox, creates artifacts
  - Note: Winget bootstrap may fail in some sandbox environments
- [x] 4.3 Confirm artifacts exist in `sandbox-tests/validation/`
  - validate.wsb, STARTED.txt, STEP.txt, result.json, ERROR.txt created

## 5. Archive

- [x] 5.1 Run `openspec archive sandbox-validation-loop --yes`
- [x] 5.2 Commit all changes
