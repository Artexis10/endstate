// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

var captureContractDeterministicBytes = []byte("[ports]\nshowFps=1\nmute=0\n")

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
	if scenario.Fixture.Type != validationmatrix.FixtureAuto || scenario.Fixture.Path != "" || scenario.Fixture.SHA256 != "" {
		return reject("fixture.type", "capture contract requires an authority-free auto fixture")
	}
	if scenario.Review == nil || scenario.Review.Decision != "approved-one-way" {
		return reject("review", "capture contract requires approved one-way review authority")
	}
	if len(mod.Restore) != 0 || mod.Config != nil || mod.Capture == nil || len(mod.Capture.Files) != 1 || len(mod.Capture.RegistryKeys) != 0 || len(mod.Capture.RegistryValues) != 0 {
		return reject("operations", "capture contract requires one file capture with no restore or generation lane")
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
	for index, file := range mod.Capture.Files {
		coordinate := "capture.files[" + strconv.Itoa(index) + "]"
		source := strings.ReplaceAll(file.Source, `\`, "/")
		if !canonicalDirectCaptureSource(file.Source) {
			return reject(coordinate+".source", "capture contract requires one direct canonical file source")
		}
		if !file.Optional {
			return reject(coordinate+".optional", "capture contract requires one optional direct file capture")
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
		targets = append(targets, CaptureContractTarget{
			Coordinate: coordinate, AuthoredSource: file.Source, Destination: destination,
			Optional: file.Optional, Content: append([]byte(nil), captureContractDeterministicBytes...),
		})
	}
	if len(mod.Verify) != 1 || mod.Verify[0].Type != "file-exists" || mod.Verify[0].Path != targets[0].AuthoredSource || mod.Verify[0].Command != "" || mod.Verify[0].ValueName != "" || mod.Verify[0].ValueType != "" || mod.Verify[0].Data != "" {
		return reject("verify", "capture contract requires the exact captured-file production verifier")
	}
	return &CaptureContractPlan{
		ModuleID: mod.ID, ModuleRevision: mod.Revision, ScenarioID: scenario.ID,
		Inventory: inventory, Targets: targets, Verifiers: append([]modules.VerifyDef(nil), mod.Verify...),
	}, nil
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
