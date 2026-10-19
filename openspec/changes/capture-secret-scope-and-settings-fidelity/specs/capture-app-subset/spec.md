## MODIFIED Requirements

### Requirement: Selection tokens are namespaced by kind

A `--only` token SHALL select a config module by its catalog ID when the text before its first dot names a catalog tier, which is a directory under `modules/`, for example `apps.vscode` or `windows-settings.explorer`. Every other token SHALL select a detected app by its capture ID, including a token that contains a dot, such as `Vendor.Tool`. A bare token SHALL NOT be interpreted as a config module ID.

#### Scenario: A bare token selects an app

- **WHEN** `--only vscode` is parsed
- **THEN** `vscode` is treated as an app ID and not as a config module ID

#### Scenario: A prefixed token selects a config module

- **WHEN** `--only git-git,apps.vscode,windows-settings.explorer` is parsed
- **THEN** `git-git` is an app selection
- **AND** `apps.vscode` and `windows-settings.explorer` are config module selections

#### Scenario: A dotted token without a tier prefix selects an app

- **WHEN** `--only Vendor.Tool` is parsed and `Vendor` is not a catalog tier
- **THEN** `Vendor.Tool` is an app selection
