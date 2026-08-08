// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

// InstallContractPlan pins the package and verifier authority exercised by an
// install-contract scenario. It is compiled from the selected production
// module; the journey must not infer either authority from fixture data.
type InstallContractPlan struct {
	ModuleID          string
	ModuleRevision    string
	ScenarioID        string
	Inventory         validationmode.Inventory
	Verifiers         []modules.VerifyDef
	CommandExecutable string
	ManifestPath      string
	context           *validationmode.Context
}

func compileInstallContract(mod *modules.Module, scenario validationmatrix.Scenario) (*InstallContractPlan, *Failure) {
	reject := func(coordinate, detail string) (*InstallContractPlan, *Failure) {
		return nil, fail(CodeUnsupportedFixture, "fixture", coordinate, detail)
	}
	if mod == nil || mod.ID == "" || mod.Revision == "" || len(mod.CanonicalSnapshot()) == 0 {
		return reject("module", "install contract requires one immutable production module")
	}
	authority, err := modules.ParseModuleJSON(mod.CanonicalSnapshot())
	if err != nil || authority.Revision != mod.Revision || !bytes.Equal(declarativeModuleJSON(mod), declarativeModuleJSON(authority)) {
		return reject("module", "install contract module differs from its immutable catalog snapshot")
	}
	if mod.EffectiveSchemaVersion() != 1 || scenario.Mode != validationmatrix.ScenarioInstallContract {
		return reject("schema", "install contract requires a schema-v1 install-contract scenario")
	}
	if scenario.Fixture.Type != validationmatrix.FixtureAuto || scenario.Fixture.Path != "" || scenario.Fixture.SHA256 != "" {
		return reject("fixture.type", "install contract requires an authority-free auto fixture")
	}
	if len(mod.Restore) != 0 || mod.Config != nil || mod.Capture != nil && (len(mod.Capture.Files) != 0 || len(mod.Capture.RegistryKeys) != 0 || len(mod.Capture.RegistryValues) != 0) {
		return reject("operations", "install contract cannot claim configuration operations")
	}

	packageReferences := len(mod.Matches.Winget) + len(mod.Matches.Chocolatey)
	if packageReferences != 1 {
		return reject("matches", "install contract requires exactly one Winget or Chocolatey package reference")
	}
	if len(mod.Matches.Winget) == 1 && strings.TrimSpace(mod.Matches.Winget[0]) != mod.Matches.Winget[0] || len(mod.Matches.Chocolatey) == 1 && strings.TrimSpace(mod.Matches.Chocolatey[0]) != mod.Matches.Chocolatey[0] {
		return reject("matches", "install package reference must be non-empty and canonical")
	}
	inventory := validationInventory(mod)
	if inventory.Driver != "winget" && inventory.Driver != "chocolatey" || inventory.Ref == "" || inventory.AppID == "" || inventory.InitialState != "present" {
		return reject("inventory", "install inventory is not an exact present package authority")
	}

	if len(mod.Verify) != 1 {
		return reject("verify", "install contract requires exactly one production verifier")
	}
	command, failure := compileInstallVerifier(mod.Verify[0])
	if failure != nil {
		return reject(failure.coordinate, failure.detail)
	}

	return &InstallContractPlan{
		ModuleID: mod.ID, ModuleRevision: mod.Revision, ScenarioID: scenario.ID,
		Inventory: inventory, Verifiers: append([]modules.VerifyDef(nil), mod.Verify...), CommandExecutable: command,
	}, nil
}

type installVerifierFailure struct {
	coordinate string
	detail     string
}

func compileInstallVerifier(verifier modules.VerifyDef) (string, *installVerifierFailure) {
	reject := func(coordinate, detail string) (string, *installVerifierFailure) {
		return "", &installVerifierFailure{coordinate: coordinate, detail: detail}
	}
	switch verifier.Type {
	case "command-exists":
		if verifier.Command == "" || verifier.Path != "" || verifier.ValueName != "" || verifier.ValueType != "" || verifier.Data != "" {
			return reject("verify[0]", "command verifier has foreign fields")
		}
		command := filepath.Base(verifier.Command)
		if command != verifier.Command || strings.ContainsAny(command, `\/:`) {
			return reject("verify[0].command", "command verifier must be one contained executable name")
		}
		if filepath.Ext(command) == "" {
			command += ".exe"
		}
		if !strings.EqualFold(filepath.Ext(command), ".exe") {
			return reject("verify[0].command", fmt.Sprintf("command verifier executable %q is not a Windows executable", command))
		}
		return command, nil
	case "file-exists":
		if verifier.Path == "" || verifier.Command != "" || verifier.ValueName != "" || verifier.ValueType != "" || verifier.Data != "" {
			return reject("verify[0]", "file verifier has foreign fields")
		}
		if !safeInstallFileVerifierPath(verifier.Path) {
			return reject("verify[0].path", "file verifier path is not a contained validation alias path")
		}
		return "", nil
	case "registry-key-exists":
		if verifier.Path == "" || verifier.Command != "" || verifier.ValueName != "" || verifier.ValueType != "" || verifier.Data != "" {
			return reject("verify[0]", "registry key verifier has foreign fields")
		}
		if _, err := validationmode.NormalizeHKCU(verifier.Path); err != nil {
			return reject("verify[0].path", "registry key verifier is not an exact HKCU key")
		}
		return "", nil
	default:
		return reject("verify[0]", "install contract verifier type is unsupported")
	}
}

func safeInstallFileVerifierPath(path string) bool {
	if path == "" || path != strings.TrimSpace(path) || strings.ContainsRune(path, '\x00') {
		return false
	}
	path = validationmode.NormalizeProductionAuthoredPath(path)
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") || filepath.IsAbs(path) ||
		(len(path) >= 2 && path[1] == ':') || !strings.HasPrefix(path, "%") {
		return false
	}
	closing := strings.Index(path[1:], "%")
	if closing < 0 {
		return false
	}
	closing++
	if closing == 1 || !installFileVerifierAlias(path[1:closing]) || closing+1 >= len(path) || (path[closing+1] != '\\' && path[closing+1] != '/') {
		return false
	}
	suffix := path[closing+2:]
	if suffix == "" || strings.ContainsAny(suffix, `%$~:<>`) || strings.Contains(suffix, `\\`) || strings.Contains(suffix, "//") {
		return false
	}
	for _, component := range strings.FieldsFunc(suffix, func(value rune) bool { return value == '\\' || value == '/' }) {
		if component == "." || component == ".." || component != strings.TrimSpace(component) {
			return false
		}
	}
	return true
}

func installFileVerifierAlias(alias string) bool {
	switch strings.ToLower(alias) {
	case "appdata", "localappdata", "userprofile", "programfiles", "programw6432", "programfiles(x86)", "programdata", "public", "systemroot", "windir", "temp", "tmp":
		return true
	default:
		return false
	}
}

func declarativeModuleJSON(mod *modules.Module) []byte {
	data, _ := json.Marshal(mod)
	return data
}

func (plan *InstallContractPlan) materializeManifest(root string) *Failure {
	if plan == nil || plan.context == nil || root != plan.context.Root() {
		return fail(CodeIsolationFailure, "fixture", "manifest", "install manifest lacks validation root authority")
	}
	path := filepath.Join(root, "manifests", "install-v1.jsonc")
	if !fixtureContained(root, path) {
		return fail(CodeIsolationFailure, "fixture", "manifest", "install manifest escaped validation root")
	}
	value := installManifestProjection(plan)
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fail(CodeArtifactContract, "fixture", "manifest", "install manifest cannot be encoded")
	}
	data = append(data, '\n')
	if err := safepath.MkdirParent(root, "manifests/install-v1.jsonc", 0o700); err != nil {
		return fail(CodeIsolationFailure, "fixture", "manifest", "install manifest parent cannot be created")
	}
	if err := safepath.AtomicWriteFile(path, data, 0o600); err != nil {
		return fail(CodeIsolationFailure, "fixture", "manifest", "install manifest cannot be materialized")
	}
	read, _, err := safepath.ReadRegularFile(path)
	if err != nil || !bytes.Equal(read, data) {
		return fail(CodeArtifactContract, "fixture", "manifest", "install manifest bytes changed during materialization")
	}
	plan.ManifestPath = path
	return nil
}

func installManifestProjection(plan *InstallContractPlan) manifest.Manifest {
	value := manifest.Manifest{
		Version: 1, Name: "Endstate validation " + plan.ModuleID,
		Apps: []manifest.App{{
			ID: plan.Inventory.AppID, DisplayName: plan.Inventory.DisplayName, Driver: plan.Inventory.Driver,
			Source: plan.Inventory.Source, Refs: map[string]string{"windows": plan.Inventory.Ref},
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
