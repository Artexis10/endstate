## ADDED Requirements

### Requirement: Copy strategy restores files and directories
The Go engine SHALL implement a copy restore strategy that copies source files/directories to target paths. For files, it copies source to target. For directories, it copies recursively. When backup=true and target exists, a backup SHALL be created before overwriting. Exclude glob patterns SHALL skip matching files silently during directory copy. Locked files (sharing violations) SHALL be caught and added as warnings without failing the operation. Up-to-date detection via SHA256 hash comparison SHALL skip files where source and target are identical.

#### Scenario: Copy file with backup
- **WHEN** a copy restore entry has backup=true and the target file exists
- **THEN** the engine creates a backup of the target, copies source to target, and reports action "restored" with backupCreated=true

#### Scenario: Copy directory with exclude patterns
- **WHEN** a copy restore entry targets a directory with exclude=["**\\Logs\\**"]
- **THEN** the engine copies all files except those matching the exclude pattern and skips excluded files silently

#### Scenario: Skip up-to-date file
- **WHEN** source and target files have identical SHA256 hashes
- **THEN** the engine skips the copy and reports action "skipped_up_to_date"

#### Scenario: Locked file during directory copy
- **WHEN** a file within the source directory is locked by another process on the target
- **THEN** the engine adds a warning for that file, continues copying other files, and does not fail the operation

### Requirement: Merge-JSON strategy deep merges objects
The Go engine SHALL implement a merge-json restore strategy that deep merges a source JSON file into an existing target JSON file. Objects merge recursively (source keys overwrite target keys), arrays replace entirely, scalars overwrite from source. Output SHALL have sorted keys and 2-space indentation. Backup SHALL be created before merging when backup=true.

#### Scenario: Deep merge nested objects
- **WHEN** a merge-json entry merges source {"a":{"b":2,"c":3}} into target {"a":{"b":1,"d":4}}
- **THEN** the result is {"a":{"b":2,"c":3,"d":4}} with target keys preserved and source keys overwriting

#### Scenario: Array replacement in merge
- **WHEN** a merge-json entry merges source with an array field into target with a different array
- **THEN** the source array replaces the target array entirely

### Requirement: Merge-INI strategy performs section-aware merge
The Go engine SHALL implement a merge-ini strategy that merges source INI sections and keys into a target INI file. Keys in matching sections overwrite; new sections and keys are added. Existing keys not in source are preserved. Global keys (before any [section] header) SHALL be supported.

#### Scenario: Merge INI with new and existing sections
- **WHEN** source INI has [editor] font=Consolas and target has [editor] theme=dark
- **THEN** the result has [editor] with both font=Consolas and theme=dark

#### Scenario: Preserve keys not in source
- **WHEN** target INI has [settings] key1=val1 key2=val2 and source only has [settings] key1=newval
- **THEN** the result has [settings] key1=newval key2=val2

### Requirement: Append strategy adds content to files
The Go engine SHALL implement an append strategy that reads source content and appends it to the target file. If the target does not exist, it SHALL be created. Backup SHALL be created before appending when backup=true and target exists.

#### Scenario: Append to existing file
- **WHEN** an append entry targets an existing file
- **THEN** the source content is appended to the end of the target file and a backup is created

#### Scenario: Append creates new file
- **WHEN** an append entry targets a non-existent file
- **THEN** the target file is created with the source content and no backup is created

### Requirement: Restore orchestrator dispatches by strategy type
The Go engine SHALL provide a RunRestore function that iterates restore entries, dispatches each to the correct strategy by type field (copy, merge-json, merge-ini, append), and collects RestoreResult structs. It SHALL expand environment variables in source and target paths. It SHALL resolve sources per Model B (ExportRoot first, then ManifestDir fallback). Optional entries with missing sources SHALL be skipped with "skipped_missing_source" status. Sensitive target paths (.ssh, .aws, .gnupg, credentials, secrets, tokens) SHALL trigger a warning but not block restore.

#### Scenario: Dispatch to correct strategy
- **WHEN** a restore entry has type "merge-json"
- **THEN** the orchestrator dispatches to the merge-json strategy handler

#### Scenario: Optional entry with missing source
- **WHEN** a restore entry has optional=true and the source file does not exist
- **THEN** the result has action "skipped_missing_source" and no error

#### Scenario: Sensitive path detection
- **WHEN** a restore entry targets a path containing ".ssh"
- **THEN** the result includes a warning about sensitive path but restore proceeds

#### Scenario: Environment variable expansion
- **WHEN** a restore entry has target "%APPDATA%\\App\\config.json"
- **THEN** the engine expands %APPDATA% to the actual path before processing

### Requirement: Dry-run mode previews without modifying
The Go engine restore SHALL support a DryRun option. When DryRun=true, the engine SHALL report what would happen (which entries would be restored, skipped, or backed up) without creating backups, copying files, or modifying the filesystem.

#### Scenario: Dry-run reports preview
- **WHEN** RunRestore is called with DryRun=true
- **THEN** results are returned with expected actions but no filesystem modifications occur
