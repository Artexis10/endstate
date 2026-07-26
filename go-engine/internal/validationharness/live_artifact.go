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
	OutputPath      string
	EventPath       string
	Receipt         liveReceiptArtifactPathClaim
	ModuleRevision  string
	MachineName     string
	CapturedAt      string
	EndstateVersion string
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
	if claims.ModuleRevision != definition.ModuleRevision || claims.MachineName == "" || claims.CapturedAt == "" || claims.EndstateVersion == "" {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "claims", "capture decoder claims are incomplete or stale")
	}
	if _, err := time.Parse(time.RFC3339, claims.CapturedAt); err != nil {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "claims", "capture decoder timestamp is invalid")
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
	if failure := validateLiveArtifactManifest(entries.files["manifest.jsonc"], definition, expected); failure != nil {
		return liveArtifactEvidence{}, failure
	}
	if failure := validateLiveArtifactMetadata(entries.files["metadata.json"], definition, claims); failure != nil {
		return liveArtifactEvidence{}, failure
	}
	allowed := map[string]struct{}{"manifest.jsonc": {}, "metadata.json": {}}
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
		allowed[key] = struct{}{}
	}
	if len(entries.files) != len(allowed) {
		return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains an extra or missing member")
	}
	for name := range entries.files {
		if _, ok := allowed[name]; !ok {
			return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains an unrelated member")
		}
	}
	for directory := range entries.directories {
		valid := false
		for member := range allowed {
			if strings.HasPrefix(member, directory+"/") {
				valid = true
				break
			}
		}
		if !valid {
			return liveArtifactEvidence{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP contains an unsupported directory member")
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

type liveArtifactEntries struct {
	files       map[string][]byte
	directories map[string]struct{}
}

func readLiveArtifactEntries(data []byte) (liveArtifactEntries, *Failure) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil || len(reader.File) > maxLiveArtifactMembers {
		return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP is malformed or exceeds member limits")
	}
	entries := liveArtifactEntries{files: make(map[string][]byte, len(reader.File)), directories: make(map[string]struct{})}
	var compressed, expanded uint64
	for _, file := range reader.File {
		name := filepath.ToSlash(file.Name)
		directory := strings.HasSuffix(name, "/") || file.FileInfo().IsDir()
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
			if file.UncompressedSize64 != 0 {
				return liveArtifactEntries{}, fail(CodeArtifactContract, "capture", "artifact", "capture ZIP directory member contains data")
			}
			entries.directories[key] = struct{}{}
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
	}
	return entries, nil
}

func liveUnsafeZipMode(mode os.FileMode, directory bool) bool {
	unsafe := mode & (os.ModeSymlink | os.ModeDevice | os.ModeNamedPipe | os.ModeSocket | os.ModeCharDevice | os.ModeIrregular)
	return unsafe != 0 || (!directory && !mode.IsRegular())
}

func validateLiveArtifactManifest(raw []byte, definition LiveDefinition, snapshots map[string]liveTargetSnapshot) *Failure {
	fields, err := liveOpenObject(manifest.StripJsoncComments(raw))
	if err != nil {
		return fail(CodeArtifactContract, "capture", "manifest", "capture manifest is malformed")
	}
	for _, forbidden := range []string{"configCaptures", "legacyConfigLanes"} {
		for field := range fields {
			if strings.EqualFold(field, forbidden) {
				return fail(CodeArtifactContract, "capture", "manifest", "schema-v2 manifest fields are forbidden")
			}
		}
	}
	var captured manifest.Manifest
	if json.Unmarshal(manifest.StripJsoncComments(raw), &captured) != nil {
		return fail(CodeArtifactContract, "capture", "manifest", "capture manifest is malformed")
	}
	version, ok := captured.Version.(float64)
	if !ok || version != 1 || len(captured.Apps) != 1 || len(captured.ConfigModules) != 1 || captured.ConfigModules[0] != definition.ModuleID || len(captured.Restore) != len(snapshots) {
		return fail(CodeArtifactContract, "capture", "manifest", "capture manifest is not the exact schema-v1 projection")
	}
	app := captured.Apps[0]
	if app.ID != liveAppID(definition.ModuleID) || app.Refs["windows"] != definition.WingetRef || !strings.EqualFold(app.Driver, "winget") || !strings.EqualFold(app.Source, "winget") {
		return fail(CodeArtifactContract, "capture", "manifest.apps", "capture manifest app identity differs from the live definition")
	}
	used := make([]bool, len(captured.Restore))
	for _, mapping := range definition.Comparator.Mappings {
		expectedSource := "./configs/" + strings.TrimPrefix(filepath.ToSlash(mapping.Identity), "apps/")
		matched := -1
		for index, restore := range captured.Restore {
			if used[index] {
				continue
			}
			if restore.Type == "copy" && filepath.ToSlash(restore.Source) == expectedSource && restore.Target == mapping.RestoreTemplate && restore.Backup && restore.Optional == mapping.Optional && len(restore.Exclude) == 0 && restore.Pattern == "" && restore.FromModule == "" && restore.LegacyCaptureID == "" {
				if matched >= 0 {
					return fail(CodeArtifactContract, "capture", mapping.Identity, "capture restore projection is ambiguous")
				}
				matched = index
			}
		}
		if matched < 0 {
			return fail(CodeArtifactContract, "capture", mapping.Identity, "capture restore projection differs from the live definition")
		}
		used[matched] = true
	}
	return nil
}

func validateLiveArtifactMetadata(raw []byte, definition LiveDefinition, claims liveCaptureArtifactClaims) *Failure {
	fields, err := liveOpenObject(raw)
	if err != nil {
		return fail(CodeArtifactContract, "capture", "metadata", "capture metadata is malformed")
	}
	for _, forbidden := range []string{"manifestVersion", "configCapturesIncluded"} {
		for field := range fields {
			if strings.EqualFold(field, forbidden) {
				return fail(CodeArtifactContract, "capture", "metadata", "schema-v2 metadata fields are forbidden")
			}
		}
	}
	var metadata bundle.BundleMetadata
	if json.Unmarshal(raw, &metadata) != nil || metadata.SchemaVersion != "1.0" || metadata.CapturedAt != claims.CapturedAt || metadata.MachineName != claims.MachineName || metadata.EndstateVersion != claims.EndstateVersion || !exactStrings(metadata.ConfigModulesIncluded, []string{strings.TrimPrefix(definition.ModuleID, "apps.")}) || len(metadata.ConfigModulesSkipped) != 0 || len(metadata.CaptureWarnings) != 0 {
		return fail(CodeArtifactContract, "capture", "metadata", "capture metadata differs from the schema-v1 capture claims")
	}
	return nil
}

func liveSHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
