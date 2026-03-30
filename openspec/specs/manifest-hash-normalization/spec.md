# manifest-hash-normalization Specification

## Purpose
Defines the line-ending normalization contract for manifest file hashing, ensuring that manifest hashes are stable across platforms and editors regardless of whether the file uses CRLF or LF line endings.

## Requirements

### Requirement: CRLF-to-LF Normalization Before Hashing

The engine SHALL normalize all CRLF (`\r\n`) sequences to LF (`\n`) in manifest file content before computing the SHA256 hash. This ensures that the same logical manifest content produces the same hash regardless of the platform or editor that wrote the file.

#### Scenario: Same content with different line endings produces the same hash

- **WHEN** a manifest file uses CRLF line endings (Windows default)
- **AND** the same content exists in a file with LF line endings (Unix/macOS)
- **THEN** both files produce the same manifest hash

#### Scenario: Hash is computed on raw file content after normalization

- **WHEN** a manifest hash is computed
- **THEN** the engine reads the raw file bytes, normalizes CRLF to LF, then computes SHA256
- **AND** the hash is NOT computed on the parsed/expanded manifest (it reflects the source file)

#### Scenario: Hash is truncated to 16 hex characters

- **WHEN** a manifest hash is computed
- **THEN** the result is the first 16 characters of the lowercase hex-encoded SHA256 digest

#### Scenario: Missing manifest file returns empty or null hash

- **WHEN** a manifest file does not exist at the specified path
- **THEN** the hash result is empty (or null/nil) rather than an error

### Requirement: Manifest Hash in Envelopes

The apply, verify, and plan command envelopes SHALL include a manifest hash field computed using the normalized hashing algorithm above.

#### Scenario: Apply envelope includes manifest hash

- **WHEN** `apply --json` is run
- **THEN** the JSON envelope's `manifest.hash` field contains the normalized hash of the manifest file

#### Scenario: Consistent hash across commands

- **GIVEN** the same manifest file
- **WHEN** apply, verify, and plan each compute the manifest hash
- **THEN** all three produce the same hash value

### Requirement: Cross-Engine Hash Consistency

The PowerShell engine and Go engine SHALL produce identical manifest hashes for the same file content. Both engines use the same algorithm: read raw bytes, replace `\r\n` with `\n`, compute SHA256, truncate to 16 lowercase hex characters.

#### Scenario: PowerShell and Go engines produce matching hashes

- **GIVEN** a manifest file with mixed or CRLF line endings
- **WHEN** the PowerShell engine computes the manifest hash
- **AND** the Go engine computes the manifest hash
- **THEN** both hashes are identical

## Invariants

### INV-HASH-1: Normalization Is CRLF-to-LF Only

- Only `\r\n` sequences are replaced with `\n`
- Bare `\r` characters (classic Mac line endings) are NOT normalized
- Bare `\n` characters are preserved as-is

### INV-HASH-2: Hash Reflects Source File, Not Expanded Manifest

- The manifest hash is computed from the raw file content on disk (after CRLF normalization)
- It does NOT reflect include resolution, configModules expansion, or any other post-load transformation
- A separate expanded-manifest hash exists for drift detection of the effective configuration

### INV-HASH-3: Algorithm Is SHA256 Truncated to 16 Hex

- Algorithm: SHA256
- Encoding: UTF-8 bytes of the normalized content
- Output: first 16 characters of the lowercase hexadecimal digest
- Example: `"a1b2c3d4e5f6a7b8"`

### INV-HASH-4: Platform-Independent

- The same manifest content produces the same hash on Windows, macOS, and Linux
- Git autocrlf settings, editor defaults, and CI environments do not affect the hash

## Affected Commands
- apply (manifest.hash in envelope)
- verify (manifest.hash in envelope)
- plan (manifest.hash in envelope)
- report (manifest.hash in state records)

## Implementation
- PowerShell: `engine/state.ps1` (Get-ManifestHash function)
- Go: manifest hash computation in command execution (apply, verify, plan)
