## MODIFIED Requirements

### Requirement: Apply from zip extracts to temp directory and cleans up

When a zip profile is consumed by the Go engine, the supported path is `endstate rebuild --from <bundle.zip>`. The zip SHALL be extracted to a temporary directory, the manifest read and applied from there (install, then config restore from the extracted `configs/<module>/` payloads), and the temporary directory removed after the full rebuild pipeline (install → restore → verify) completes. The temporary directory SHALL remain available for the entire restore step so that restore `source` paths rewritten to `./configs/<module>/<leaf>` resolve against the extraction directory.

#### Scenario: Rebuild from zip extracts, applies, and cleans up
- **WHEN** `endstate rebuild --from "Name.zip" --confirm` is run
- **THEN** the zip SHALL be extracted to a temporary directory
- **AND** apps SHALL be installed from the extracted manifest
- **AND** configuration SHALL be restored from the extracted `configs/<module>/` payloads
- **AND** the temporary directory SHALL be removed after the rebuild finishes
