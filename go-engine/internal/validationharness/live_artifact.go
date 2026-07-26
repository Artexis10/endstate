// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

const (
	maxLiveArtifactBytes       = 100 << 20
	maxLiveArtifactMembers     = 32
	maxLiveArtifactNameBytes   = 512
	maxLiveArtifactNameDepth   = 8
	maxLiveArtifactCompression = 100
)

// liveTargetSnapshot is the independent pre-capture observation for one
// definition mapping. Identity is semantic; a snapshot never carries a host
// path. The Notepad++ schema-v1 roundtrip accepts regular files only.
type liveTargetSnapshot struct {
	Identity string
	Absent   bool
	Mode     os.FileMode
	Size     int64
	SHA256   string
	Bytes    []byte
}

// liveReceiptArtifactPathClaim is intentionally a typed placeholder until the
// receipt decoder supplies the output argument claim directly.
type liveReceiptArtifactPathClaim struct {
	Path string
}

// liveCaptureArtifactClaims are decoder-owned, path-bearing inputs. The
// verifier compares them but never includes their values in its output.
type liveCaptureArtifactClaims struct {
	OutputPath        string
	EventPath         string
	Receipt           liveReceiptArtifactPathClaim
	ModuleRevision    string
	MachineName       string
	CapturedAt        string
	EndstateVersion   string
	OS                string
	RestoreProjection []modules.RestoreDef
	VerifyProjection  []modules.VerifyDef
}

// liveArtifactEvidence is safe to retain with live validation state: it binds
// the exact captured byte slice without retaining a personal filesystem path.
type liveArtifactEvidence struct {
	SHA256 string
	Size   int64
	Mode   os.FileMode
}

func inspectLiveCaptureArtifact(definition LiveDefinition, snapshots []liveTargetSnapshot, claims liveCaptureArtifactClaims, artifactPath string) (liveArtifactEvidence, *Failure) {
	if err := validateLiveDefinition(definition); err != nil {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "definition", "live artifact definition is invalid")
	}
	if claims.ModuleRevision != definition.ModuleRevision || claims.MachineName == "" || claims.CapturedAt == "" || claims.EndstateVersion == "" || claims.OS == "" {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "claims", "capture decoder claims are incomplete or stale")
	}
	if _, err := time.Parse(time.RFC3339, claims.CapturedAt); err != nil {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "claims", "capture decoder timestamp is invalid")
	}
	if failure := validateLiveArtifactProjection(definition, claims); failure != nil {
		return liveArtifactEvidence{}, failure
	}
	if !sameLiveArtifactPathClaims(artifactPath, claims) {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture artifact path claims do not identify one file")
	}
	expected, failure := liveSnapshotSet(definition, snapshots)
	if failure != nil {
		return liveArtifactEvidence{}, failure
	}
	data, mode, err := safepath.ReadRegularFileBounded(artifactPath, maxLiveArtifactBytes)
	if err != nil || !mode.IsRegular() {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture artifact is absent, linked, changed, oversized, or not regular")
	}
	digest := sha256.Sum256(data)
	entries, failure := readLiveArtifactEntries(data)
	if failure != nil {
		return liveArtifactEvidence{}, failure
	}
	if failure := validateLiveArtifactManifest(entries.files["manifest.jsonc"], definition, claims, expected); failure != nil {
		return liveArtifactEvidence{}, failure
	}
	if failure := validateLiveArtifactMetadata(entries.files["metadata.json"], definition, claims); failure != nil {
		return liveArtifactEvidence{}, failure
	}
	allowed := map[string]string{"manifest.jsonc": "manifest.jsonc", "metadata.json": "metadata.json"}
	for identity, snapshot := range expected {
		member := "configs/" + strings.TrimPrefix(identity, "apps/")
		key := strings.ToLower(member)
		if snapshot.Absent {
			if _, exists := entries.files[key]; exists {
				return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", identity, "absent target has a payload member")
			}
			continue
		}
		payload, exists := entries.files[key]
		if !exists || int64(len(payload)) != snapshot.Size || !bytes.Equal(payload, snapshot.Bytes) || liveSHA256(payload) != snapshot.SHA256 {
			return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", identity, "payload bytes, size, or digest differ from the independent target snapshot")
		}
		allowed[key] = member
	}
	if len(entries.files) != len(allowed) {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains an extra or missing member")
	}
	for name := range entries.files {
		want, ok := allowed[name]
		if !ok {
			return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains an unrelated member")
		}
		if entries.names[name] != want {
			return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP member spelling is not canonical")
		}
	}
	for directory := range entries.directories {
		valid := false
		for _, member := range allowed {
			if strings.HasPrefix(member, entries.names[directory]+"/") {
				valid = true
				break
			}
		}
		if !valid {
			return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains an unsupported directory member")
		}
	}
	expectedDirectories := map[string]string{"configs": "configs", "configs/notepad-plus-plus": "configs/notepad-plus-plus"}
	if len(entries.directories) != len(expectedDirectories) {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP lacks the exact production directory members")
	}
	for key, expectedName := range expectedDirectories {
		if _, ok := entries.directories[key]; !ok || entries.names[key] != expectedName {
			return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP directory members differ from production")
		}
	}
	return liveArtifactEvidence{SHA256: hex.EncodeToString(digest[:]), Size: int64(len(data)), Mode: mode}, nil
}

func sameLiveArtifactPathClaims(artifactPath string, claims liveCaptureArtifactClaims) bool {
	artifact, ok := canonicalLiveArtifactPath(artifactPath)
	if !ok {
		return false
	}
	for _, claim := range []string{claims.OutputPath, claims.EventPath, claims.Receipt.Path} {
		value, ok := canonicalLiveArtifactPath(claim)
		if !ok || !sameLiveArtifactPath(artifact, value) {
			return false
		}
	}
	return true
}

func canonicalLiveArtifactPath(value string) (string, bool) {
	if value == "" {
		return "", false
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", false
	}
	return filepath.Clean(abs), true
}

func sameLiveArtifactPath(left, right string) bool {
	if filepath.Separator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
}

func liveSnapshotSet(definition LiveDefinition, snapshots []liveTargetSnapshot) (map[string]liveTargetSnapshot, *Failure) {
	if len(snapshots) != len(definition.Comparator.Mappings) {
		return nil, fail(CodeArtifactContract, "capture", "snapshots", "target snapshots do not cover the exact comparator mappings")
	}
	expected := make(map[string]liveTargetSnapshot, len(snapshots))
	seen := make(map[string]struct{}, len(snapshots))
	for _, mapping := range definition.Comparator.Mappings {
		expected[mapping.Identity] = liveTargetSnapshot{Identity: mapping.Identity, Absent: true}
	}
	for _, snapshot := range snapshots {
		if _, ok := expected[snapshot.Identity]; !ok {
			return nil, fail(CodeArtifactContract, "capture", "snapshots", "target snapshot does not resolve a comparator identity")
		}
		if _, duplicate := seen[snapshot.Identity]; duplicate {
			return nil, fail(CodeArtifactContract, "capture", "snapshots", "target snapshots contain a duplicate identity")
		}
		seen[snapshot.Identity] = struct{}{}
		if snapshot.Absent {
			if snapshot.Mode != 0 || snapshot.Size != 0 || snapshot.SHA256 != "" || len(snapshot.Bytes) != 0 {
				return nil, fail(CodeArtifactContract, "capture", snapshot.Identity, "absent target snapshot contains file evidence")
			}
			expected[snapshot.Identity] = snapshot
			continue
		}
		if !snapshot.Mode.IsRegular() || snapshot.Size < 0 || int64(len(snapshot.Bytes)) != snapshot.Size || !lowerSHA256(snapshot.SHA256) || liveSHA256(snapshot.Bytes) != snapshot.SHA256 {
			return nil, fail(CodeArtifactContract, "capture", snapshot.Identity, "target snapshot is not an exact regular file")
		}
		expected[snapshot.Identity] = snapshot
	}
	for identity, snapshot := range expected {
		if snapshot.Identity != identity {
			return nil, fail(CodeArtifactContract, "capture", "snapshots", "target snapshot identity is incomplete")
		}
	}
	return expected, nil
}

func validateLiveArtifactProjection(definition LiveDefinition, claims liveCaptureArtifactClaims) *Failure {
	if definition.ModuleID != "apps.notepad-plus-plus" || len(claims.RestoreProjection) != len(definition.Comparator.Mappings)+1 || len(claims.VerifyProjection) != 1 {
		return fail(CodeArtifactContract, "capture", "claims", "production schema-v1 projection is incomplete")
	}
	remaining := make(map[string]modules.RestoreDef, len(claims.RestoreProjection))
	for _, restore := range claims.RestoreProjection {
		identity, err := liveRestoreIdentity(restore.Source)
		if err != nil || restore.Type != "copy" || !restore.Backup || !restore.Optional || restore.Pattern != "" || restore.Reason != "" || len(restore.Exclude) != 0 || restore.Key != "" || restore.ValueName != "" || restore.ValueType != "" || restore.Data != "" {
			return fail(CodeArtifactContract, "capture", "claims", "production schema-v1 restore projection is invalid")
		}
		if _, duplicate := remaining[identity]; duplicate {
			return fail(CodeArtifactContract, "capture", "claims", "production schema-v1 restore projection is ambiguous")
		}
		remaining[identity] = restore
	}
	for _, mapping := range definition.Comparator.Mappings {
		restore, ok := remaining[mapping.Identity]
		if !ok || restore.Target != mapping.RestoreTemplate {
			return fail(CodeArtifactContract, "capture", "claims", "comparator restore projection differs from the live definition")
		}
		delete(remaining, mapping.Identity)
	}
	directory, ok := remaining["apps/notepad-plus-plus/userDefineLangs"]
	if !ok || directory.Target != `%APPDATA%\Notepad++\userDefineLangs` || len(remaining) != 1 {
		return fail(CodeArtifactContract, "capture", "claims", "Notepad++ directory restore projection differs from production")
	}
	verify := claims.VerifyProjection[0]
	if verify.Type != "command-exists" || verify.Command != "notepad++" || verify.Path != "" || verify.ValueName != "" || verify.ValueType != "" || verify.Data != "" {
		return fail(CodeArtifactContract, "capture", "claims", "Notepad++ verifier projection differs from production")
	}
	return nil
}

type liveArtifactEntries struct {
	files       map[string][]byte
	directories map[string]struct{}
	names       map[string]string
}

func readLiveArtifactEntries(data []byte) (liveArtifactEntries, *Failure) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) > maxLiveArtifactMembers {
		return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP is malformed or exceeds member limits")
	}
	entries := liveArtifactEntries{files: make(map[string][]byte, len(reader.File)), directories: make(map[string]struct{}), names: make(map[string]string, len(reader.File))}
	var compressed, expanded uint64
	for _, file := range reader.File {
		name := file.Name
		if strings.Contains(name, `\`) {
			return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP member uses a noncanonical separator")
		}
		directory := strings.HasSuffix(name, "/")
		if file.FileInfo().IsDir() != directory {
			return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP directory member has a noncanonical name")
		}
		portableName := strings.TrimSuffix(name, "/")
		if len(portableName) == 0 || len(portableName) > maxLiveArtifactNameBytes || !safeArtifactName(portableName) || len(strings.Split(portableName, "/")) > maxLiveArtifactNameDepth || liveUnsafeZipMode(file.Mode(), directory) || (file.Method != zip.Store && file.Method != zip.Deflate) {
			return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains an unsafe member")
		}
		if file.CompressedSize64 > maxLiveArtifactBytes || file.UncompressedSize64 > maxLiveArtifactBytes || compressed > maxLiveArtifactBytes-file.CompressedSize64 || expanded > maxLiveArtifactBytes-file.UncompressedSize64 {
			return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP exceeds compressed or uncompressed limits")
		}
		compressed += file.CompressedSize64
		expanded += file.UncompressedSize64
		if file.UncompressedSize64 > 0 && (file.CompressedSize64 == 0 || file.UncompressedSize64/file.CompressedSize64 > maxLiveArtifactCompression) {
			return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP compression ratio exceeds the validation limit")
		}
		key := strings.ToLower(portableName)
		if _, duplicate := entries.files[key]; duplicate {
			return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains duplicate or case-alias members")
		}
		if _, duplicate := entries.directories[key]; duplicate {
			return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains duplicate or case-alias members")
		}
		if directory {
			if file.UncompressedSize64 != 0 || file.Mode().Perm() != 0o666 {
				return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP directory member contains data")
			}
			entries.directories[key] = struct{}{}
			entries.names[key] = portableName
			continue
		}
		stream, err := file.Open()
		if err != nil {
			return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP member cannot be opened")
		}
		payload, err := io.ReadAll(io.LimitReader(stream, maxLiveArtifactBytes+1))
		_ = stream.Close()
		if err != nil || uint64(len(payload)) != file.UncompressedSize64 || len(payload) > maxLiveArtifactBytes {
			return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP member is truncated or oversized")
		}
		entries.files[key] = payload
		entries.names[key] = portableName
	}
	return entries, nil
}

func liveUnsafeZipMode(mode os.FileMode, directory bool) bool {
	unsafe := mode & (os.ModeSymlink | os.ModeDevice | os.ModeNamedPipe | os.ModeSocket | os.ModeCharDevice | os.ModeIrregular)
	return unsafe != 0 || (!directory && !mode.IsRegular())
}

func validateLiveArtifactManifest(raw []byte, definition LiveDefinition, claims liveCaptureArtifactClaims, snapshots map[string]liveTargetSnapshot) *Failure {
	fields, err := liveOpenObject(manifest.StripJsoncComments(raw))
	if err != nil {
		return fail(CodeArtifactContract, "capture", "manifest", "capture manifest is malformed")
	}
	if !liveExactKeySet(fields, "version", "apps", "configModules", "restore", "verify") {
		return fail(CodeArtifactContract, "capture", "manifest", "capture manifest has a non-production schema-v1 shape")
	}
	var captured manifest.Manifest
	if json.Unmarshal(manifest.StripJsoncComments(raw), &captured) != nil {
		return fail(CodeArtifactContract, "capture", "manifest", "capture manifest is malformed")
	}
	version, ok := captured.Version.(float64)
	if !ok || version != 1 || len(captured.Apps) != 1 || len(captured.ConfigModules) != 1 || captured.ConfigModules[0] != definition.ModuleID || len(captured.Restore) != len(claims.RestoreProjection) || len(captured.Verify) != len(claims.VerifyProjection) {
		return fail(CodeArtifactContract, "capture", "manifest", "capture manifest is not the exact schema-v1 projection")
	}
	apps, err := liveArray(fields["apps"])
	if err != nil || len(apps) != 1 {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture manifest app shape differs from production")
	}
	appFields, err := liveObject(apps[0], "id", "refs", "driver", "source")
	if err != nil {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture manifest app shape differs from production")
	}
	refs, err := liveObject(appFields["refs"], "windows")
	if err != nil {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture manifest app references differ from production")
	}
	app := captured.Apps[0]
	if app.ID != liveManifestAppID(definition.WingetRef) || app.Refs["windows"] != definition.WingetRef || !strings.EqualFold(app.Driver, "winget") || !strings.EqualFold(app.Source, "winget") {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture manifest app identity differs from the live definition")
	}
	if !liveStringEquals(appFields["id"], app.ID) || !liveStringEquals(appFields["driver"], "winget") || !liveStringEquals(appFields["source"], "winget") || !liveStringEquals(refs["windows"], definition.WingetRef) {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture manifest app values differ from production")
	}
	rawRestores, err := liveArray(fields["restore"])
	if err != nil || len(rawRestores) != len(claims.RestoreProjection) {
		return fail(CodeArtifactContract, "capture", "manifest.restore", "capture restore shape differs from production")
	}
	for _, rawRestore := range rawRestores {
		if _, err := liveObject(rawRestore, "type", "source", "target", "backup", "optional", "fromModule"); err != nil {
			return fail(CodeArtifactContract, "capture", "manifest.restore", "capture restore shape differs from production")
		}
	}
	used := make([]bool, len(captured.Restore))
	for _, expected := range claims.RestoreProjection {
		expectedSource := "./configs/" + strings.TrimPrefix(definition.ModuleID, "apps.") + "/" + filepath.Base(filepath.ToSlash(expected.Source))
		matched := -1
		for index, restore := range captured.Restore {
			if used[index] {
				continue
			}
			if exactRestoreProjection(restore, expected, definition.ModuleID, expectedSource) {
				if matched >= 0 {
					return fail(CodeArtifactContract, "capture", "manifest.restore", "capture restore projection is ambiguous")
				}
				matched = index
			}
		}
		if matched < 0 {
			return fail(CodeArtifactContract, "capture", "manifest.restore", "capture restore projection differs from the production definition")
		}
		used[matched] = true
	}
	rawVerifies, err := liveArray(fields["verify"])
	if err != nil || len(rawVerifies) != len(claims.VerifyProjection) {
		return fail(CodeArtifactContract, "capture", "manifest.verify", "capture verifier shape differs from production")
	}
	for _, rawVerify := range rawVerifies {
		if _, err := liveObject(rawVerify, "type", "command"); err != nil {
			return fail(CodeArtifactContract, "capture", "manifest.verify", "capture verifier shape differs from production")
		}
	}
	for _, expected := range claims.VerifyProjection {
		matched := 0
		for _, actual := range captured.Verify {
			if exactVerifyProjection(actual, expected) {
				matched++
			}
		}
		if matched != 1 {
			return fail(CodeArtifactContract, "capture", "manifest.verify", "capture verifier projection differs from the production definition")
		}
	}
	return nil
}

func validateLiveArtifactMetadata(raw []byte, definition LiveDefinition, claims liveCaptureArtifactClaims) *Failure {
	fields, err := liveOpenObject(raw)
	if err != nil {
		return fail(CodeArtifactContract, "capture", "metadata", "capture metadata is malformed")
	}
	if !liveExactKeySet(fields, "schemaVersion", "capturedAt", "machineName", "endstateVersion", "configModulesIncluded", "configModulesSkipped", "captureWarnings", "os") {
		return fail(CodeArtifactContract, "capture", "metadata", "capture metadata has a non-production schema-v1 shape")
	}
	var metadata bundle.BundleMetadata
	if liveNull(fields["configModulesSkipped"]) || liveNull(fields["captureWarnings"]) || json.Unmarshal(raw, &metadata) != nil || metadata.SchemaVersion != "1.0" || metadata.CapturedAt != claims.CapturedAt || metadata.MachineName != claims.MachineName || metadata.EndstateVersion != claims.EndstateVersion || metadata.OS != claims.OS || metadata.Share || metadata.Name != "" || metadata.Redaction != nil || metadata.ManifestVersion != 0 || len(metadata.ConfigCapturesIncluded) != 0 || !exactStrings(metadata.ConfigModulesIncluded, []string{strings.TrimPrefix(definition.ModuleID, "apps.")}) || len(metadata.ConfigModulesSkipped) != 0 || len(metadata.CaptureWarnings) != 0 {
		return fail(CodeArtifactContract, "capture", "metadata", "capture metadata differs from the schema-v1 capture claims")
	}
	return nil
}

func liveSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
