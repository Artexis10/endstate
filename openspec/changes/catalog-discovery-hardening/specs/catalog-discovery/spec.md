## ADDED Requirements

### Requirement: Repo-root resolution finds an installed layout

Repo-root resolution SHALL resolve, in order: the `ENDSTATE_ROOT` environment variable; the
nearest ancestor of the running executable containing `.release-please-manifest.json`; the
nearest ancestor containing a `modules/apps` directory. If no source produces a result it
SHALL return empty and callers SHALL handle the missing-root case.

The installed-layout step SHALL run only where resolution would otherwise return empty, so an
explicit override and a repo checkout both retain their existing results.

#### Scenario: An installed layout resolves without a repo marker

- **WHEN** the running executable is at `<install>/bin/lib/endstate.exe`, `<install>/bin`
  contains `modules/apps`, no `.release-please-manifest.json` exists above it, and
  `ENDSTATE_ROOT` is unset
- **THEN** resolution returns `<install>/bin`

#### Scenario: An explicit override wins

- **WHEN** `ENDSTATE_ROOT` is set
- **THEN** resolution returns its value without consulting the filesystem

#### Scenario: A repo checkout is unaffected

- **WHEN** an ancestor contains `.release-please-manifest.json` and a different ancestor
  contains `modules/apps`
- **THEN** resolution returns the directory containing the marker

#### Scenario: A non-directory does not satisfy the catalog check

- **WHEN** an ancestor contains a file, not a directory, at `modules/apps`
- **THEN** that ancestor is not treated as a root

### Requirement: Bootstrap installs the module catalog

Bootstrap SHALL copy the `modules` and `payload` trees from the resolved source root into the
install directory, and SHALL report which trees were installed.

A tree absent at the source SHALL be skipped without failing bootstrap, so an install that
cannot reach a catalog still receives its shim and PATH entry. Installing SHALL replace the
destination tree rather than merge into it, so modules removed upstream do not persist. When
the resolved source and the destination are the same location, the tree SHALL be left intact
and reported as present.

#### Scenario: Catalog is installed alongside the binary

- **WHEN** bootstrap runs from a source root containing `modules` and `payload`
- **THEN** both trees exist in the install directory
- **AND** the result reports both as installed

#### Scenario: Refresh drops modules removed upstream

- **WHEN** bootstrap runs against an install whose catalog contains a module no longer present
  at the source
- **THEN** that module is absent from the install afterwards

#### Scenario: Re-bootstrapping from an installed binary is not destructive

- **WHEN** the resolved source root and the install directory are the same location
- **THEN** the catalog remains intact and is reported as installed

#### Scenario: A bare binary still bootstraps

- **WHEN** no catalog trees exist at the resolved source root
- **THEN** bootstrap succeeds, reports no trees installed, and still writes the shim

### Requirement: Capture reports an unreachable config module catalog

Capture SHALL emit a `module_catalog_unavailable` warning when config module attachment is not
explicitly disabled and no module catalog can be reached. The warning SHALL state that the
capture records installed apps but none of their settings, and SHALL carry remediation.

A catalog that loads successfully but contains no modules SHALL NOT trigger this warning: that
is a correctly configured install that matched nothing, and conflating it with a
misconfiguration would make the warning routine and ignorable.

#### Scenario: Unresolvable root warns

- **WHEN** capture runs and no repo root resolves
- **THEN** the result carries a `module_catalog_unavailable` warning

#### Scenario: Failed catalog load warns

- **WHEN** a root resolves but loading the catalog returns an error
- **THEN** the result carries a `module_catalog_unavailable` warning

#### Scenario: A wired but empty catalog does not warn

- **WHEN** the catalog loads successfully and contains no modules
- **THEN** no `module_catalog_unavailable` warning is emitted

#### Scenario: Opting out of config modules does not warn

- **WHEN** capture runs with config module attachment explicitly disabled
- **THEN** no `module_catalog_unavailable` warning is emitted
