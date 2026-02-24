## 1. Extract Display Name in Capture Engine

- [x] 1.1 In `engine/capture.ps1` `Get-InstalledAppsViaWingetList`, compute `$nameIndex` from the header line (index 0 or `$headerLine.IndexOf('Name')`), extract the display name substring from each data line (`$line.Substring($nameIndex, $idIndex - $nameIndex).Trim()`), and store as `_name` on the app hashtable
- [x] 1.2 In `engine/capture.ps1` `Get-InstalledAppsViaWinget` (export path), set `_name = $null` on each app object (placeholder for future export JSON support)

## 2. Forward Display Name to Capture Envelope

- [x] 2.1 In `bin/endstate.ps1` at the `appsIncluded` builder (~line 2755 and ~line 2634), add `name = $_._name` to `$appEntry` when `$_._name` is non-null
- [x] 2.2 In `bin/endstate.ps1` at the second `appsIncluded` builder (~line 2634), apply the same conditional `name` inclusion

## 3. Add Name Parameter to Write-ItemEvent

- [x] 3.1 In `engine/events.ps1` `Write-ItemEvent`, add optional parameter `[Parameter(Mandatory = $false)] [string]$Name = $null` and conditionally include `name` in the event hashtable when non-null

## 4. Pass Display Name in Capture Item Events

- [x] 4.1 In `bin/endstate.ps1` at the capture item event loop (~line 2768-2772), read `_name` from the app object and pass `-Name` to `Write-ItemEvent` when non-null
- [x] 4.2 In `engine/capture.ps1` at the filter/detect item event calls (~lines 264, 279, 318), pass `-Name $app._name` when the app object has `_name`

## 5. Update Spec and Tests

- [x] 5.1 Update `openspec/specs/capture-artifact-contract.md` JSON schema example to show optional `name` field in `appsIncluded` entry
- [x] 5.2 Add Pester tests for name extraction in fallback parser (mock `winget list` output with Name column, verify `_name` on parsed apps)
- [x] 5.3 Add Pester tests for `Write-ItemEvent -Name` parameter (verify event JSON includes `name` when provided, omits when not)
- [x] 5.4 Add Pester test for `appsIncluded` envelope entry containing `name` field when `_name` is present on app object
