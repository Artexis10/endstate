// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type CaptureContractTarget struct {
	Coordinate     string
	AuthoredSource string
	Destination    string
	Optional       bool
	Content        []byte
	Resolved       string
}

// CaptureContractPlan pins the exact reviewed production capture declaration.
// Runtime resolution and materialization happen only after validation mode has
// created its contained host roots.
type CaptureContractPlan struct {
	ModuleID       string
	ModuleRevision string
	ScenarioID     string
	Inventory      validationmode.Inventory
	Targets        []CaptureContractTarget
	Verifiers      []modules.VerifyDef
	Restores       []modules.RestoreDef
	context        *validationmode.Context
	root           string
}

func (plan *CaptureContractPlan) MaterializeCaptured() *Failure {
	if plan == nil || len(plan.Targets) == 0 {
		return fail(CodeUnsupportedFixture, "fixture", "operations", "capture contract fixture plan is empty")
	}
	for index := range plan.Targets {
		target := &plan.Targets[index]
		if failure := plan.validateTarget(target); failure != nil {
			return failure
		}
		if failure := prepareFixtureFile(plan.context, target.Resolved, target.Coordinate); failure != nil {
			return failure
		}
		if err := os.MkdirAll(filepath.Dir(target.Resolved), 0o700); err != nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "create capture fixture parent")
		}
		if failure := prepareFixtureFile(plan.context, target.Resolved, target.Coordinate); failure != nil {
			return failure
		}
		if err := os.WriteFile(target.Resolved, target.Content, 0o600); err != nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "write capture fixture payload")
		}
	}
	return nil
}

func (plan *CaptureContractPlan) HasOptionalTargets() bool {
	if plan == nil {
		return false
	}
	for _, target := range plan.Targets {
		if target.Optional {
			return true
		}
	}
	return false
}

func (plan *CaptureContractPlan) MaterializeOptionalAbsent() *Failure {
	if plan == nil || !plan.HasOptionalTargets() {
		return fail(CodeUnsupportedFixture, "fixture", "operations", "capture contract has no optional target to prove absent")
	}
	for index := range plan.Targets {
		target := &plan.Targets[index]
		if !target.Optional {
			continue
		}
		if failure := plan.validateTarget(target); failure != nil {
			return failure
		}
		info, err := os.Lstat(target.Resolved)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "optional capture target changed type before removal")
		}
		if err := os.Remove(target.Resolved); err != nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "remove optional capture target")
		}
	}
	return nil
}

func (plan *CaptureContractPlan) validateTarget(target *CaptureContractTarget) *Failure {
	if plan.context == nil || plan.root == "" || target == nil || target.Resolved == "" || !filepath.IsAbs(target.Resolved) || filepath.Clean(target.Resolved) != target.Resolved {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "capture fixture has no exact runtime authority")
	}
	if err := plan.context.ValidateSandboxPath(target.Resolved); err != nil || !fixtureContained(plan.root, target.Resolved) {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "capture fixture left validation authority")
	}
	return nil
}

func compileCaptureContract(mod *modules.Module, scenario validationmatrix.Scenario) (*CaptureContractPlan, *Failure) {
	return compileCaptureContractAt("", mod, scenario)
}

func compileCaptureContractAt(repoRoot string, mod *modules.Module, scenario validationmatrix.Scenario) (*CaptureContractPlan, *Failure) {
	reject := func(coordinate, detail string) (*CaptureContractPlan, *Failure) {
		return nil, fail(CodeUnsupportedFixture, "fixture", coordinate, detail)
	}
	if mod == nil || mod.ID == "" || mod.Revision == "" || len(mod.CanonicalSnapshot()) == 0 {
		return reject("module", "capture contract requires one immutable production module")
	}
	authority, err := modules.ParseModuleJSON(mod.CanonicalSnapshot())
	if err != nil || authority.Revision != mod.Revision || !bytes.Equal(declarativeModuleJSON(mod), declarativeModuleJSON(authority)) {
		return reject("module", "capture contract module differs from its immutable catalog snapshot")
	}
	if mod.EffectiveSchemaVersion() != 1 || scenario.Mode != validationmatrix.ScenarioCaptureContract {
		return reject("schema", "capture contract requires a schema-v1 capture-contract scenario")
	}
	if scenario.Review == nil || scenario.Review.Decision != "approved-one-way" {
		return reject("review", "capture contract requires approved one-way review authority")
	}
	if mod.Config != nil || mod.Capture == nil || len(mod.Capture.Files) == 0 || len(mod.Capture.RegistryKeys) != 0 || len(mod.Capture.RegistryValues) != 0 {
		return reject("operations", "capture contract requires direct file captures with no registry or generation lane")
	}
	if len(mod.Matches.Winget) != 1 || len(mod.Matches.Chocolatey) != 0 {
		return reject("matches", "capture contract requires exactly one Winget app reference")
	}
	inventory := validationInventory(mod)
	if inventory.AppID == "" || inventory.Ref == "" || inventory.InitialState != "present" || inventory.Driver != "winget" || inventory.Source != "winget" {
		return reject("inventory", "capture contract inventory is not one exact present Winget authority")
	}

	prefix := "apps/" + strings.TrimPrefix(mod.ID, "apps.") + "/"
	targets := make([]CaptureContractTarget, 0, len(mod.Capture.Files))
	seenPayloads := make(map[string]struct{}, len(mod.Capture.Files))
	for index, file := range mod.Capture.Files {
		coordinate := "capture.files[" + strconv.Itoa(index) + "]"
		source := strings.ReplaceAll(file.Source, `\`, "/")
		if !canonicalDirectCaptureSource(file.Source) {
			return reject(coordinate+".source", "capture contract requires one direct canonical file source")
		}
		if !file.Optional {
			return reject(coordinate+".optional", "capture contract requires every direct file capture to be optional")
		}
		destination := strings.ReplaceAll(file.Dest, `\`, "/")
		if destination != file.Dest || path.Clean(destination) != destination || !strings.HasPrefix(destination, prefix) || strings.TrimPrefix(destination, prefix) == "" || strings.Contains(destination, ":") {
			return reject(coordinate+".dest", "capture destination must be canonical and module-contained")
		}
		filename := path.Base(source)
		for globIndex, glob := range mod.Capture.ExcludeGlobs {
			matched, matchErr := bundle.ConfigPathMatchesExcludeGlob(filename, glob)
			if matchErr != nil {
				return reject("capture.excludeGlobs["+strconv.Itoa(globIndex)+"]", "capture exclude glob is malformed")
			}
			if matched {
				return reject("capture.excludeGlobs["+strconv.Itoa(globIndex)+"]", "capture exclude glob applies to a direct capture filename")
			}
		}
		if mod.Secrets != nil && bundle.CapturePathMatchesSecrets(file.Source, mod.Secrets.Files) {
			return reject(coordinate+".source", "capture source is shadowed by the production secrets matcher")
		}
		payload := strings.ToLower(v1ArtifactPayloadPath(mod.ID, destination))
		if _, duplicate := seenPayloads[payload]; duplicate {
			return reject(coordinate+".dest", "capture destinations collide after the production flattened payload rewrite")
		}
		seenPayloads[payload] = struct{}{}
		targets = append(targets, CaptureContractTarget{
			Coordinate: coordinate, AuthoredSource: file.Source, Destination: destination,
			Optional: file.Optional, Content: captureContractContent(mod.ID, scenario.ID, coordinate),
		})
	}
	if len(mod.Verify) != 1 {
		return reject("verify", "capture contract requires exactly one supported production verifier")
	}
	if _, verifierFailure := compileInstallVerifier(mod.Verify[0]); verifierFailure != nil {
		return reject(verifierFailure.coordinate, verifierFailure.detail)
	}
	consumedTargets := make(map[string]struct{}, len(mod.Restore))
	for index, restore := range mod.Restore {
		coordinate := "restore[" + strconv.Itoa(index) + "]"
		if restore.Type != "copy" || !restore.Backup || restore.Pattern != "" || restore.Reason != "" || restore.Key != "" || restore.ValueName != "" || restore.ValueType != "" || restore.Data != "" || len(restore.Exclude) != 0 {
			return reject(coordinate, "capture contract supports only exact backup-enabled copy restore projections")
		}
		captureIndex := -1
		for targetIndex, target := range targets {
			if restore.Target == target.AuthoredSource && payloadDestination(restore.Source) == target.Destination {
				captureIndex = targetIndex
				break
			}
		}
		if captureIndex < 0 {
			return reject(coordinate, "copy restore must project exactly one captured file")
		}
		key := targets[captureIndex].Coordinate
		if _, duplicate := consumedTargets[key]; duplicate {
			return reject(coordinate, "multiple copy restores project one captured file")
		}
		consumedTargets[key] = struct{}{}
	}
	switch scenario.Fixture.Type {
	case validationmatrix.FixtureAuto:
		if scenario.Fixture.Path != "" || scenario.Fixture.SHA256 != "" || len(targets) != 1 {
			return reject("fixture.type", "auto capture fixtures support exactly one direct file")
		}
	case validationmatrix.FixtureDeclarative:
		if failure := validateCaptureContractFixture(repoRoot, scenario, targets); failure != nil {
			return nil, failure
		}
	default:
		return reject("fixture.type", "capture contract fixture type is unsupported")
	}
	return &CaptureContractPlan{
		ModuleID: mod.ID, ModuleRevision: mod.Revision, ScenarioID: scenario.ID,
		Inventory: inventory, Targets: targets, Verifiers: append([]modules.VerifyDef(nil), mod.Verify...), Restores: append([]modules.RestoreDef(nil), mod.Restore...),
	}, nil
}

func captureContractContent(moduleID, scenarioID, coordinate string) []byte {
	return []byte(fixtureSentinel(moduleID, scenarioID, coordinate, "capture"))
}

func validateCaptureContractFixture(repoRoot string, scenario validationmatrix.Scenario, targets []CaptureContractTarget) *Failure {
	if repoRoot == "" || !filepath.IsAbs(repoRoot) || filepath.Clean(repoRoot) != repoRoot || scenario.Fixture.Path == "" || scenario.Fixture.SHA256 == "" {
		return fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative capture fixture requires repository authority and a pinned path")
	}
	fixturePath, err := safepath.Resolve(repoRoot, filepath.FromSlash(scenario.Fixture.Path))
	if err != nil {
		return fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative capture fixture path left repository authority")
	}
	raw, _, err := safepath.ReadRegularFile(fixturePath)
	if err != nil {
		return fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative capture fixture is absent or not regular")
	}
	digest := sha256.Sum256(raw)
	if fmt.Sprintf("%x", digest) != scenario.Fixture.SHA256 {
		return fail(CodeUnsupportedFixture, "fixture", "fixture.sha256", "declarative capture fixture hash differs from the sidecar")
	}
	jsonc := manifest.StripJsoncComments(raw)
	if rejectDuplicateJSONFields(jsonc) != nil {
		return fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative capture fixture JSONC contains duplicate fields")
	}
	decoder := json.NewDecoder(bytes.NewReader(jsonc))
	decoder.DisallowUnknownFields()
	var shape declarativeFixtureShape
	if err := decoder.Decode(&shape); err != nil {
		return fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative capture fixture JSONC is malformed")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fail(CodeUnsupportedFixture, "fixture", "fixture.path", "declarative capture fixture must contain one JSON object")
	}
	if shape.SchemaVersion != 1 || len(shape.Entries) != len(targets) {
		return fail(CodeUnsupportedFixture, "fixture", "fixture.entries", "declarative capture fixture must cover every capture coordinate exactly once")
	}
	seen := make(map[string]struct{}, len(shape.Entries))
	for _, entry := range shape.Entries {
		if entry.Kind != fixtureKindFile {
			return fail(CodeUnsupportedFixture, "fixture", entry.Coordinate, "declarative capture fixture requires direct file kinds")
		}
		if _, duplicate := seen[entry.Coordinate]; duplicate {
			return fail(CodeUnsupportedFixture, "fixture", entry.Coordinate, "declarative capture fixture coordinate is duplicated")
		}
		seen[entry.Coordinate] = struct{}{}
	}
	for _, target := range targets {
		if _, exists := seen[target.Coordinate]; !exists {
			return fail(CodeUnsupportedFixture, "fixture", target.Coordinate, "declarative capture fixture coordinate is absent")
		}
	}
	return nil
}

func canonicalDirectCaptureSource(authored string) bool {
	if authored == "" || authored != strings.TrimSpace(authored) {
		return false
	}
	normalized := strings.ReplaceAll(authored, `\`, "/")
	if strings.ContainsAny(normalized, "*?[:") || strings.Contains(normalized, "//") || !strings.HasPrefix(normalized, "%") {
		return false
	}
	closing := strings.Index(normalized[1:], "%")
	if closing <= 0 {
		return false
	}
	closing++
	alias := normalized[1:closing]
	if strings.Trim(alias, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_") != "" || closing+1 >= len(normalized) || normalized[closing+1] != '/' {
		return false
	}
	components := strings.Split(normalized[closing+2:], "/")
	if len(components) == 0 {
		return false
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}
