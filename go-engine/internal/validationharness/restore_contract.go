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
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

var restoreContractSHA256Pattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type RestoreContractPlan struct {
	ModuleID       string
	ModuleRevision string
	ScenarioID     string
	Inventory      validationmode.Inventory
	Restore        modules.RestoreDef
	Verifiers      []modules.VerifyDef
	Restored       []byte
	Original       []byte
	PayloadPath    string
	ManifestPath   string
	ArtifactPath   string
	context        *validationmode.Context
	root           string
}

func (plan *RestoreContractPlan) materializeArtifact(root string) *Failure {
	if plan == nil || plan.context == nil || root == "" || plan.root != root || len(plan.Restored) == 0 {
		return fail(CodeIsolationFailure, "fixture", "artifact", "restore artifact authority is incomplete")
	}
	manifestRoot := filepath.Join(root, "manifests", "restore-contract")
	manifestPath := filepath.Join(manifestRoot, "manifest.jsonc")
	payloadRelative := strings.TrimPrefix(filepath.ToSlash(plan.Restore.Source), "./")
	payloadPath := filepath.Join(manifestRoot, filepath.FromSlash(payloadRelative))
	artifactPath := filepath.Join(root, "manifests", "restore-contract.zip")
	for _, candidate := range []string{manifestRoot, manifestPath, payloadPath, artifactPath} {
		if plan.context.ValidateSandboxPath(candidate) != nil || !fixtureContained(root, candidate) {
			return fail(CodeIsolationFailure, "fixture", "artifact", "restore artifact left validation authority")
		}
	}
	if err := os.MkdirAll(filepath.Dir(payloadPath), 0o700); err != nil {
		return fail(CodeIsolationFailure, "fixture", "artifact", "create restore artifact payload root")
	}
	if err := safepath.AtomicWriteFile(payloadPath, plan.Restored, 0o600); err != nil {
		return fail(CodeIsolationFailure, "fixture", "artifact", "write copied restore payload")
	}
	value := restoreContractManifest(plan)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fail(CodeArtifactContract, "fixture", "manifest", "encode restore manifest")
	}
	data = append(data, '\n')
	if err := safepath.AtomicWriteFile(manifestPath, data, 0o600); err != nil {
		return fail(CodeIsolationFailure, "fixture", "manifest", "write restore manifest")
	}
	if failure := writeRestoreContractZIP(artifactPath, data, payloadRelative, plan.Restored); failure != nil {
		return failure
	}
	entries, failure := readCaptureArtifactEntries(artifactPath)
	if failure != nil || len(entries) != 2 || !bytes.Equal(entries["manifest.jsonc"], data) || !bytes.Equal(entries[strings.ToLower(payloadRelative)], plan.Restored) {
		return fail(CodeArtifactContract, "fixture", "artifact", "restore artifact differs from the exact manifest and copied payload")
	}
	plan.ManifestPath = manifestPath
	plan.PayloadPath = payloadPath
	plan.ArtifactPath = artifactPath
	return nil
}

func restoreContractManifest(plan *RestoreContractPlan) manifest.Manifest {
	value := manifest.Manifest{
		Version: 1, Name: "Endstate validation " + plan.ModuleID,
		Apps: []manifest.App{{
			ID: plan.Inventory.AppID, DisplayName: plan.Inventory.DisplayName, Driver: plan.Inventory.Driver,
			Source: plan.Inventory.Source, Refs: map[string]string{"windows": plan.Inventory.Ref},
		}},
		ConfigModules: []string{plan.ModuleID},
		Restore: []manifest.RestoreEntry{{
			Type: plan.Restore.Type, Source: plan.Restore.Source, Target: plan.Restore.Target,
			Pattern: plan.Restore.Pattern, Reason: plan.Restore.Reason, Backup: plan.Restore.Backup,
			Optional: plan.Restore.Optional, Exclude: append([]string(nil), plan.Restore.Exclude...),
			FromModule: plan.ModuleID, Key: plan.Restore.Key, ValueName: plan.Restore.ValueName,
			ValueType: plan.Restore.ValueType, Data: plan.Restore.Data,
		}},
	}
	for _, verifier := range plan.Verifiers {
		value.Verify = append(value.Verify, manifest.VerifyEntry{
			Type: verifier.Type, Command: verifier.Command, Path: verifier.Path,
			ValueName: verifier.ValueName, ValueType: verifier.ValueType, Data: verifier.Data,
		})
	}
	return value
}

func writeRestoreContractZIP(artifactPath string, manifestBytes []byte, payloadRelative string, payload []byte) (failure *Failure) {
	parent := filepath.Dir(artifactPath)
	temporary, err := os.CreateTemp(parent, "restore-contract-*.zip.tmp")
	if err != nil {
		return fail(CodeIsolationFailure, "fixture", "artifact", "create restore artifact staging file")
	}
	temporaryPath := temporary.Name()
	defer func() { _ = os.Remove(temporaryPath) }()
	writer := zip.NewWriter(temporary)
	for _, entry := range []struct {
		name string
		data []byte
	}{{"manifest.jsonc", manifestBytes}, {payloadRelative, payload}} {
		header := &zip.FileHeader{Name: entry.name, Method: zip.Store}
		header.SetMode(0o600)
		stream, createErr := writer.CreateHeader(header)
		if createErr != nil {
			_ = writer.Close()
			_ = temporary.Close()
			return fail(CodeArtifactContract, "fixture", "artifact", "create restore artifact member")
		}
		if _, writeErr := stream.Write(entry.data); writeErr != nil {
			_ = writer.Close()
			_ = temporary.Close()
			return fail(CodeArtifactContract, "fixture", "artifact", "write restore artifact member")
		}
	}
	if err := writer.Close(); err != nil {
		_ = temporary.Close()
		return fail(CodeArtifactContract, "fixture", "artifact", "close restore artifact")
	}
	if err := temporary.Close(); err != nil {
		return fail(CodeArtifactContract, "fixture", "artifact", "close restore artifact staging file")
	}
	if err := os.Rename(temporaryPath, artifactPath); err != nil {
		return fail(CodeIsolationFailure, "fixture", "artifact", "publish restore artifact")
	}
	return nil
}

type restoreContractFixture struct {
	SchemaVersion   int                           `json:"schemaVersion"`
	Payload         restoreContractFixturePayload `json:"payload"`
	Restore         restoreContractFixtureRestore `json:"restore"`
	RestoredContent string                        `json:"restoredContent"`
	OriginalContent string                        `json:"originalContent"`
}

type restoreContractFixturePayload struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type restoreContractFixtureRestore struct {
	Coordinate string `json:"coordinate"`
	Source     string `json:"source"`
	Target     string `json:"target"`
}

func compileRestoreContractAt(repoRoot string, mod *modules.Module, scenario validationmatrix.Scenario) (*RestoreContractPlan, *Failure) {
	reject := func(coordinate, detail string) (*RestoreContractPlan, *Failure) {
		return nil, fail(CodeUnsupportedFixture, "fixture", coordinate, detail)
	}
	if repoRoot == "" || !filepath.IsAbs(repoRoot) || filepath.Clean(repoRoot) != repoRoot {
		return reject("fixture.path", "restore contract requires one canonical repository authority")
	}
	if mod == nil || mod.ID == "" || mod.Revision == "" || len(mod.CanonicalSnapshot()) == 0 {
		return reject("module", "restore contract requires one immutable module")
	}
	authority, err := modules.ParseModuleJSON(mod.CanonicalSnapshot())
	if err != nil || authority.Revision != mod.Revision || !bytes.Equal(declarativeModuleJSON(mod), declarativeModuleJSON(authority)) {
		return reject("module", "restore contract module differs from its immutable catalog snapshot")
	}
	if mod.EffectiveSchemaVersion() != 1 || scenario.Mode != validationmatrix.ScenarioRestoreContract {
		return reject("schema", "restore contract requires a schema-v1 restore-contract scenario")
	}
	if scenario.Fixture.Type != validationmatrix.FixtureDeclarative || !portableRestoreContractPath(scenario.Fixture.Path) || !restoreContractSHA256Pattern.MatchString(scenario.Fixture.SHA256) {
		return reject("fixture", "restore contract requires one SHA-pinned declarative fixture")
	}
	if scenario.Review == nil || scenario.Review.Decision != "approved-one-way" {
		return reject("review", "restore contract requires approved one-way review authority")
	}
	if mod.Capture != nil || mod.Config != nil || len(mod.Restore) != 1 {
		return reject("operations", "restore contract requires exactly one restore and no capture or generation lane")
	}
	if len(mod.Matches.Winget) != 1 || len(mod.Matches.Chocolatey) != 0 || len(mod.Matches.Exe) != 0 || len(mod.Matches.UninstallDisplayName) != 0 || len(mod.Matches.PathExists) != 0 {
		return reject("matches", "restore contract requires exactly one winget app reference")
	}
	restore := mod.Restore[0]
	if restore.Type != "copy" || !restore.Backup || restore.Optional || len(restore.Exclude) != 0 || restore.Pattern != "" || restore.Reason != "" || restore.Key != "" || restore.ValueName != "" || restore.ValueType != "" || restore.Data != "" {
		return reject("restore[0]", "restore contract requires one required backed-up direct copy")
	}
	wantSourcePrefix := "./payload/apps/" + strings.TrimPrefix(mod.ID, "apps.") + "/"
	if restore.Source == "" || strings.ReplaceAll(restore.Source, `\`, "/") != restore.Source || path.Clean(restore.Source) != strings.TrimPrefix(restore.Source, "./") || !strings.HasPrefix(restore.Source, wantSourcePrefix) || strings.Contains(restore.Source, ":") {
		return reject("restore[0].source", "restore source must be canonical and module-contained")
	}
	if !canonicalDirectCaptureSource(restore.Target) {
		return reject("restore[0].target", "restore target must be one canonical contained host path")
	}
	if len(mod.Verify) != 1 || mod.Verify[0].Type != "file-exists" || mod.Verify[0].Path != restore.Target || mod.Verify[0].Command != "" || mod.Verify[0].ValueName != "" || mod.Verify[0].ValueType != "" || mod.Verify[0].Data != "" {
		return reject("verify", "restore contract requires the exact restored-file verifier")
	}

	fixturePath, err := safepath.Resolve(repoRoot, filepath.FromSlash(scenario.Fixture.Path))
	if err != nil {
		return reject("fixture.path", "restore fixture left repository authority")
	}
	rawFixture, _, err := safepath.ReadRegularFile(fixturePath)
	if err != nil || sha256Hex(rawFixture) != scenario.Fixture.SHA256 {
		return reject("fixture.sha256", "restore fixture is absent or differs from its sidecar hash")
	}
	if err := rejectDuplicateJSONFields(manifest.StripJsoncComments(rawFixture)); err != nil {
		return reject("fixture.path", "restore fixture contains duplicate JSON fields")
	}
	decoder := json.NewDecoder(bytes.NewReader(manifest.StripJsoncComments(rawFixture)))
	decoder.DisallowUnknownFields()
	var fixture restoreContractFixture
	if err := decoder.Decode(&fixture); err != nil {
		return reject("fixture.path", "restore fixture JSONC is malformed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return reject("fixture.path", "restore fixture must contain one JSON object")
	}
	if fixture.SchemaVersion != 1 || fixture.Restore.Coordinate != "restore[0]" || fixture.Restore.Source != restore.Source || fixture.Restore.Target != restore.Target {
		return reject("fixture.restore", "restore fixture does not bind the exact authored restore")
	}
	fixtureDirectory := path.Dir(scenario.Fixture.Path)
	if !portableRestoreContractPath(fixture.Payload.Path) || !strings.HasPrefix(fixture.Payload.Path, fixtureDirectory+"/payload/") || !restoreContractSHA256Pattern.MatchString(fixture.Payload.SHA256) {
		return reject("fixture.payload", "restore payload requires one contained SHA-pinned path")
	}
	payloadPath, err := safepath.Resolve(repoRoot, filepath.FromSlash(fixture.Payload.Path))
	if err != nil {
		return reject("fixture.payload.path", "restore payload left repository authority")
	}
	payload, _, err := safepath.ReadRegularFile(payloadPath)
	if err != nil || sha256Hex(payload) != fixture.Payload.SHA256 {
		return reject("fixture.payload.sha256", "restore payload is absent or differs from its descriptor hash")
	}
	restored := []byte(fixture.RestoredContent)
	original := []byte(fixture.OriginalContent)
	if len(restored) == 0 || len(original) == 0 || bytes.Equal(restored, original) || !bytes.Equal(restored, payload) {
		return reject("fixture.content", "restore fixture bytes are empty, ambiguous, or differ from the tracked payload")
	}
	inventory := validationInventory(mod)
	if inventory.AppID == "" || inventory.Driver != "winget" || inventory.Ref != mod.Matches.Winget[0] || inventory.Source != "winget" || inventory.InitialState != "present" {
		return reject("inventory", "restore contract inventory is not one exact present winget authority")
	}
	return &RestoreContractPlan{
		ModuleID: mod.ID, ModuleRevision: mod.Revision, ScenarioID: scenario.ID,
		Inventory: inventory, Restore: restore, Verifiers: append([]modules.VerifyDef(nil), mod.Verify...),
		Restored: append([]byte(nil), restored...), Original: append([]byte(nil), original...), PayloadPath: payloadPath,
	}, nil
}

func portableRestoreContractPath(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || strings.Contains(value, `\`) || strings.HasPrefix(value, "/") || regexp.MustCompile(`^[A-Za-z]:`).MatchString(value) {
		return false
	}
	return path.Clean(value) == value && value != "." && !strings.HasPrefix(value, "../") && !strings.Contains(value, "/../") && !strings.Contains(value, "//")
}

func sha256Hex(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}
