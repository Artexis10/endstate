// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type V2FixturePlan struct {
	context              *validationmode.Context
	Compiled             v2CompiledFixture
	Instance             modules.ConfigInstance
	TargetInstance       modules.ConfigInstance
	CaptureID            string
	CaptureTargets       []V2FixtureTarget
	Targets              []V2FixtureTarget
	CaptureValidations   int
	MigrationValidations int
	Validations          int
}

type V2FixtureTarget struct {
	Coordinate   string
	Authored     string
	Destination  string
	Resolved     string
	Directory    bool
	PreserveRoot bool
	Optional     bool
	Members      []V2FixtureFile
	Excluded     []V2ExcludedFixture
}

type V2FixtureFile struct {
	Relative string
	Path     string
	Captured []byte
	Mutated  []byte
}

type V2ExcludedFixture struct {
	Relative        string
	Path            string
	Captured        []byte
	Mutated         []byte
	CapturePatterns []string
	RestorePatterns []string
}

func compileV2FixturePlan(context *validationmode.Context, mod *modules.Module, scenario validationmatrix.Scenario, compiled v2CompiledFixture, inventory validationmode.Inventory) (*V2FixturePlan, *Failure) {
	bad := func(coordinate, detail string) (*V2FixturePlan, *Failure) {
		return nil, fail(CodeGenerationContract, "fixture", coordinate, detail)
	}
	unsupported := func(coordinate, detail string) (*V2FixturePlan, *Failure) {
		return nil, fail(CodeUnsupportedFixture, "fixture", coordinate, detail)
	}
	if context == nil || mod == nil || context.Descriptor().ModuleID != mod.ID || context.Descriptor().ScenarioID != scenario.ID || inventory.Version != compiled.Definition.SourceVersion {
		return bad("authority", "fixture, descriptor, and production module authority differ")
	}
	instances, failure := discoverV2FixtureInstances(context, mod, compiled, inventory)
	if failure != nil {
		return nil, failure
	}
	if len(instances) != 1 || instances[0].DetectorID != compiled.Detector.ID {
		return bad("instance", "production detector must resolve exactly one fixture instance")
	}
	instance := instances[0]
	selected, err := modules.SelectGeneration(&compiled.Set, instance.Version)
	if err != nil || selected == nil || selected.ID != compiled.Generation.ID || selected.Fingerprint != compiled.Generation.Fingerprint {
		return bad("instance.version", "detected instance does not select the compiled production generation")
	}
	targetInventory := inventory
	if compiled.Definition.TargetVersion != "" {
		targetInventory.Version = compiled.Definition.TargetVersion
	}
	targetInstances, failure := discoverV2FixtureInstances(context, mod, compiled, targetInventory)
	if failure != nil {
		return nil, failure
	}
	if len(targetInstances) != 1 || targetInstances[0].DetectorID != compiled.Detector.ID {
		return bad("targetInstance", "production detector must resolve exactly one target fixture instance")
	}
	targetInstance := targetInstances[0]
	targetSelected, err := modules.SelectGeneration(&compiled.Set, targetInstance.Version)
	if err != nil || targetSelected == nil || targetSelected.ID != compiled.TargetGeneration.ID || targetSelected.Fingerprint != compiled.TargetGeneration.Fingerprint {
		return bad("targetInstance.version", "detected target instance does not select the compiled production target generation")
	}
	plan := &V2FixturePlan{
		context: context, Compiled: compiled, Instance: instance, TargetInstance: targetInstance,
		CaptureID: bundle.CaptureID(mod.ID, compiled.Set.ID, instance.ID),
	}
	for index, entry := range compiled.Entries {
		sourcePolicy := validationmode.HostPathPolicy{InstanceRoot: instance.Root}
		if compiled.Detector.Type == "path" {
			sourcePolicy.InstanceAlias = v2DetectorAlias(compiled.Detector.Glob)
		}
		capturePolicy := sourcePolicy
		capturePolicy.AllowRoot = strings.EqualFold(entry.Capture.Source, "${instance.root}")
		source, err := context.ResolveHostPath(entry.Capture.Source, capturePolicy)
		if err != nil {
			return bad(entry.Shape.CaptureCoordinate, "capture source did not resolve inside detector authority")
		}
		targetPolicy := validationmode.HostPathPolicy{InstanceRoot: targetInstance.Root}
		if compiled.Detector.Type == "path" {
			targetPolicy.InstanceAlias = v2DetectorAlias(compiled.Detector.Glob)
		}
		restorePolicy := targetPolicy
		restorePolicy.AllowRoot = strings.EqualFold(entry.Restore.Target, "${instance.root}")
		target, err := context.ResolveHostPath(entry.Restore.Target, restorePolicy)
		if err != nil {
			return bad(entry.Shape.RestoreCoordinate, "restore target did not resolve inside detector authority")
		}
		if compiled.Migration == nil && filepath.Clean(target) != filepath.Clean(source) {
			return bad(entry.Shape.RestoreCoordinate, "direct capture/restore targets did not resolve identically")
		}
		if compiled.Migration != nil && filepath.Clean(target) == filepath.Clean(source) {
			return nil, fail(CodeMigrationContract, "fixture", entry.Shape.RestoreCoordinate, "migration source and target host coordinates are not distinct")
		}
		capturePlan := V2FixtureTarget{
			Coordinate: entry.Shape.CaptureCoordinate, Authored: entry.Capture.Source,
			Destination: catalogPath(entry.Capture.Dest), Resolved: source,
			Directory:    entry.Shape.Kind == fixtureKindDirectory,
			PreserveRoot: strings.Contains(entry.Capture.Source, "${instance.root}"),
			Optional:     entry.Capture.Optional,
		}
		targetPlan := V2FixtureTarget{
			Coordinate: entry.Shape.RestoreCoordinate, Authored: entry.Restore.Target,
			Destination: catalogPath(entry.Restore.Source), Resolved: target,
			Directory:    entry.Shape.Kind == fixtureKindDirectory,
			PreserveRoot: strings.Contains(entry.Restore.Target, "${instance.root}"),
			Optional:     entry.Capture.Optional || entry.Restore.Optional,
		}
		if targetPlan.Directory {
			for memberIndex, member := range entry.Shape.Members {
				captured, mutated, err := v2FixtureContents(mod.ID, scenario.ID, fmt.Sprintf("entries[%d].members[%d]", index, memberIndex), member.Format)
				if err != nil {
					return unsupported(entry.Shape.CaptureCoordinate, "fixture member content cannot satisfy its declared format")
				}
				capturePlan.Members = append(capturePlan.Members, V2FixtureFile{
					Relative: catalogPath(member.Path), Path: filepath.Join(source, filepath.FromSlash(catalogPath(member.Path))),
					Captured: captured, Mutated: mutated,
				})
				targetPlan.Members = append(targetPlan.Members, V2FixtureFile{
					Relative: catalogPath(member.Path), Path: filepath.Join(target, filepath.FromSlash(catalogPath(member.Path))),
					Captured: captured, Mutated: mutated,
				})
			}
			captureWitnesses, witnessFailure := compileV2ExcludeWitnesses(mod.ID, scenario.ID, source, entry)
			if witnessFailure != nil {
				return nil, witnessFailure
			}
			targetWitnesses, witnessFailure := compileV2ExcludeWitnesses(mod.ID, scenario.ID, target, entry)
			if witnessFailure != nil {
				return nil, witnessFailure
			}
			capturePlan.Excluded = captureWitnesses
			targetPlan.Excluded = targetWitnesses
		} else {
			if failure := proveV2SingleFileExcludes(entry); failure != nil {
				return nil, failure
			}
			captured, sourceMutated, err := v2FixtureContents(mod.ID, scenario.ID, entry.Shape.CaptureCoordinate, entry.Shape.Format)
			if err != nil {
				return unsupported(entry.Shape.CaptureCoordinate, "fixture content cannot satisfy its declared format")
			}
			targetMutated := sourceMutated
			if compiled.Migration != nil {
				_, targetMutated, err = v2FixtureContents(mod.ID, scenario.ID, entry.Shape.RestoreCoordinate, entry.Shape.Format)
				if err != nil {
					return unsupported(entry.Shape.RestoreCoordinate, "target fixture content cannot satisfy its declared format")
				}
			}
			capturePlan.Members = []V2FixtureFile{{Relative: ".", Path: source, Captured: captured, Mutated: sourceMutated}}
			targetPlan.Members = []V2FixtureFile{{Relative: ".", Path: target, Captured: captured, Mutated: targetMutated}}
		}
		if context.ValidateSandboxPath(source) != nil || context.ValidateSandboxPath(target) != nil {
			return bad(entry.Shape.CaptureCoordinate, "resolved fixture target left validation authority")
		}
		plan.CaptureTargets = append(plan.CaptureTargets, capturePlan)
		plan.Targets = append(plan.Targets, targetPlan)
		plan.CaptureValidations += len(entry.Validations)
		plan.MigrationValidations += len(entry.MigrationValidations)
		plan.Validations += len(entry.TargetValidations)
		if compiled.Migration == nil {
			plan.Validations += len(entry.Validations)
		}
	}
	if len(plan.CaptureTargets) == 0 || len(plan.CaptureTargets) != len(plan.Targets) ||
		plan.CaptureValidations != len(compiled.Generation.Validate) || plan.CaptureValidations == 0 ||
		plan.Validations != len(compiled.TargetGeneration.Validate) || plan.Validations == 0 ||
		compiled.Migration != nil && plan.MigrationValidations != len(compiled.Migration.Validate) {
		return unsupported("operations", "fixture plan is vacuous or omits production validation")
	}
	return plan, nil
}

func proveV2SingleFileExcludes(entry v2CompiledEntry) *Failure {
	candidates := []string{
		path.Base(catalogPath(entry.Capture.Source)), path.Base(catalogPath(entry.Capture.Dest)),
		path.Base(catalogPath(entry.Restore.Source)), path.Base(catalogPath(entry.Restore.Target)),
	}
	patterns := append(append(append([]string(nil), entry.CaptureOnly...), entry.RestoreOnly...), entry.Overlapping...)
	for _, pattern := range patterns {
		for _, candidate := range candidates {
			matched, err := bundle.ConfigPathMatchesExcludeGlob(candidate, pattern)
			if err != nil {
				return fail(CodeUnsupportedFixture, "fixture", "exclude", "single-file exclude is not understood by the production matcher")
			}
			if matched {
				return fail(CodeUnsupportedFixture, "fixture", "exclude", "single-file exclude applies to the exact authored source or destination")
			}
		}
	}
	return nil
}

func discoverV2FixtureInstances(context *validationmode.Context, mod *modules.Module, compiled v2CompiledFixture, inventory validationmode.Inventory) ([]modules.ConfigInstance, *Failure) {
	bad := func(coordinate, detail string) ([]modules.ConfigInstance, *Failure) {
		return nil, fail(CodeGenerationContract, "fixture", coordinate, detail)
	}
	if compiled.Detector.Type == "package" {
		instances, err := modules.DiscoverInstances(mod, []modules.PackageEvidence{{
			AppID: inventory.AppID, Backend: inventory.Driver, Platform: "windows", Ref: inventory.Ref,
			Driver: inventory.Driver, RawVersion: inventory.Version,
		}}, modules.DiscoveryOptions{})
		if err != nil {
			return bad("detector", "production package detector rejected fixture evidence")
		}
		return instances, nil
	}
	alias := v2DetectorAlias(compiled.Detector.Glob)
	if alias == "" || path.Base(catalogPath(compiled.Definition.InstanceLocator)) != compiled.Definition.InstanceLocator || strings.ContainsAny(compiled.Definition.InstanceLocator, `\/`) {
		return bad("instanceLocator", "path fixture locator is not one stable basename")
	}
	pattern, err := context.ResolveHostPattern(compiled.Detector.Glob, validationmode.HostPathPolicy{})
	if err != nil {
		return bad("detector.glob", "production detector glob left validation authority")
	}
	anchor := pattern
	if wildcard := strings.IndexAny(anchor, "*?["); wildcard >= 0 {
		prefix := anchor[:wildcard]
		separator := strings.LastIndexAny(prefix, `\/`)
		if separator < 0 {
			return bad("detector.glob", "production detector glob has no contained anchor")
		}
		anchor = strings.TrimRight(prefix[:separator], `\/`)
	}
	root := filepath.Join(anchor, compiled.Definition.InstanceLocator)
	if context.ValidateSandboxPath(anchor) != nil || !fixtureContained(anchor, root) {
		return bad("instanceLocator", "path fixture root left the production detector anchor")
	}
	if matched, matchErr := path.Match(path.Base(catalogPath(pattern)), path.Base(catalogPath(root))); matchErr != nil || !matched {
		return bad("instanceLocator", "path fixture locator does not match the production detector glob")
	}
	if compiled.Detector.VersionPattern != "" {
		versionPattern, err := regexp.Compile(compiled.Detector.VersionPattern)
		if err != nil || !versionPattern.MatchString(compiled.Definition.InstanceLocator) {
			return bad("instanceLocator", "path fixture locator does not match production version extraction")
		}
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return bad("instanceLocator", "materialize detector root")
	}
	instances, err := modules.DiscoverInstances(mod, nil, modules.DiscoveryOptions{Glob: func(_ string) ([]string, error) {
		return context.GlobSandboxPattern(pattern)
	}})
	if err != nil {
		return bad("detector", "production path detector rejected fixture root")
	}
	if len(instances) != 1 || instances[0].Root != root || instances[0].Evidence.Type != "path" || instances[0].Version.Raw != inventory.Version {
		return bad("detector", "production path detector evidence differs from fixture authority")
	}
	return instances, nil
}

func v2DetectorAlias(glob string) string {
	if !strings.HasPrefix(glob, "%") {
		return ""
	}
	closing := strings.Index(glob[1:], "%")
	if closing < 0 {
		return ""
	}
	return glob[1 : closing+1]
}

func compileV2ExcludeWitnesses(moduleID, scenarioID, root string, entry v2CompiledEntry) ([]V2ExcludedFixture, *Failure) {
	type roles struct{ capture, restore []string }
	byRelative := map[string]*roles{}
	order := []string{}
	add := func(pattern string, capture bool) *Failure {
		relative, ok := v2ExcludeWitness(pattern)
		path := filepath.Join(root, filepath.FromSlash(relative))
		if !ok || !safeArtifactName(catalogPath(relative)) || filepath.Clean(path) == filepath.Clean(root) || !fixtureContained(root, path) || !v2MatchesExclude(relative, pattern) {
			return fail(CodeUnsupportedFixture, "fixture", "exclude", "production exclude has no deterministic witness")
		}
		key := strings.ToLower(catalogPath(relative))
		role := byRelative[key]
		if role == nil {
			role = &roles{}
			byRelative[key] = role
			order = append(order, relative)
		}
		if capture {
			role.capture = append(role.capture, pattern)
		} else {
			role.restore = append(role.restore, pattern)
		}
		return nil
	}
	for _, pattern := range append(append([]string(nil), entry.CaptureOnly...), entry.Overlapping...) {
		if failure := add(pattern, true); failure != nil {
			return nil, failure
		}
	}
	for _, pattern := range append(append([]string(nil), entry.RestoreOnly...), entry.Overlapping...) {
		if failure := add(pattern, false); failure != nil {
			return nil, failure
		}
	}
	result := make([]V2ExcludedFixture, 0, len(order))
	for index, relative := range order {
		key := strings.ToLower(catalogPath(relative))
		role := byRelative[key]
		captured, mutated, err := v2FixtureContents(moduleID, scenarioID, fmt.Sprintf("exclude[%d]", index), v2FormatFile)
		if err != nil {
			return nil, fail(CodeUnsupportedFixture, "fixture", "exclude", "build exclude witness")
		}
		result = append(result, V2ExcludedFixture{
			Relative: catalogPath(relative), Path: filepath.Join(root, filepath.FromSlash(catalogPath(relative))),
			Captured: captured, Mutated: mutated,
			CapturePatterns: append([]string(nil), role.capture...), RestorePatterns: append([]string(nil), role.restore...),
		})
	}
	return result, nil
}

func v2ExcludeWitness(raw string) (string, bool) {
	pattern := catalogPath(raw)
	stripped := strings.TrimPrefix(pattern, "**/")
	if strings.ContainsAny(stripped, "?[") || stripped == "" {
		return "", false
	}
	if strings.HasSuffix(stripped, "/**") {
		directory := strings.TrimSuffix(stripped, "/**")
		if directory == "" || strings.Contains(directory, "*") {
			return "", false
		}
		return directory + "/endstate-validation-excluded.txt", true
	}
	if strings.Contains(stripped, "/") {
		return "", false
	}
	name := strings.ReplaceAll(stripped, "*", "endstate")
	if name == "" || name == "." {
		return "", false
	}
	return name, true
}

func v2MatchesExclude(relative, rawPattern string) bool {
	normalized := catalogPath(relative)
	pattern := catalogPath(rawPattern)
	stripped := strings.TrimPrefix(pattern, "**/")
	if strings.HasSuffix(stripped, "/**") {
		directory := strings.TrimSuffix(stripped, "/**")
		bounded := "/" + strings.Trim(normalized, "/") + "/"
		return strings.Contains(strings.ToLower(bounded), "/"+strings.ToLower(directory)+"/")
	}
	for _, segment := range strings.Split(normalized, "/") {
		if matched, _ := path.Match(stripped, segment); matched {
			return true
		}
	}
	return false
}

func (plan *V2FixturePlan) MaterializeCaptured() *Failure {
	return plan.materialize(plan.CaptureTargets, false)
}
func (plan *V2FixturePlan) Mutate() *Failure          { return plan.materialize(plan.Targets, true) }
func (plan *V2FixturePlan) CompareCaptured() *Failure { return plan.compare(false) }
func (plan *V2FixturePlan) CompareMutated() *Failure  { return plan.compare(true) }

func (plan *V2FixturePlan) materialize(targets []V2FixtureTarget, mutated bool) *Failure {
	for index := range targets {
		target := &targets[index]
		if failure := plan.clearTarget(target); failure != nil {
			return failure
		}
		if target.Directory {
			if err := os.MkdirAll(target.Resolved, 0o700); err != nil {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "create fixture root")
			}
		}
		for _, member := range target.Members {
			content := member.Captured
			if mutated {
				content = member.Mutated
			}
			if failure := plan.writeFile(member.Path, content, target.Coordinate); failure != nil {
				return failure
			}
		}
		for _, excluded := range target.Excluded {
			content := excluded.Captured
			if mutated {
				content = excluded.Mutated
			}
			if failure := plan.writeFile(excluded.Path, content, target.Coordinate); failure != nil {
				return failure
			}
		}
	}
	return nil
}

func (plan *V2FixturePlan) clearTarget(target *V2FixtureTarget) *Failure {
	if plan.context.ValidateSandboxPath(target.Resolved) != nil {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "fixture target left authority")
	}
	info, err := os.Lstat(target.Resolved)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || safepath.IsLinkOrReparse(info) {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "fixture target changed type")
	}
	if target.PreserveRoot {
		if !info.IsDir() {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "detector anchor changed type")
		}
		entries, err := os.ReadDir(target.Resolved)
		if err != nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "read detector anchor")
		}
		for _, entry := range entries {
			path := filepath.Join(target.Resolved, entry.Name())
			postorder, err := fixtureTreePostorder(path)
			if err != nil {
				return fail(CodeIsolationFailure, "fixture", target.Coordinate, "fixture descendant changed type")
			}
			for _, item := range postorder {
				if err := os.Remove(item.Path); err != nil {
					return fail(CodeIsolationFailure, "fixture", target.Coordinate, "remove fixture descendant")
				}
			}
		}
		return nil
	}
	postorder, err := fixtureTreePostorder(target.Resolved)
	if err != nil {
		return fail(CodeIsolationFailure, "fixture", target.Coordinate, "fixture target changed type")
	}
	for _, item := range postorder {
		if err := os.Remove(item.Path); err != nil {
			return fail(CodeIsolationFailure, "fixture", target.Coordinate, "remove fixture target")
		}
	}
	return nil
}

func (plan *V2FixturePlan) writeFile(path string, content []byte, coordinate string) *Failure {
	if plan.context.ValidateSandboxPath(path) != nil {
		return fail(CodeIsolationFailure, "fixture", coordinate, "fixture member left authority")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fail(CodeIsolationFailure, "fixture", coordinate, "create fixture parent")
	}
	if info, err := os.Lstat(path); err == nil && (safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular()) {
		return fail(CodeIsolationFailure, "fixture", coordinate, "fixture member changed type")
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		return fail(CodeIsolationFailure, "fixture", coordinate, "write fixture member")
	}
	return nil
}

func (plan *V2FixturePlan) compare(mutated bool) *Failure {
	for _, target := range plan.Targets {
		expected := map[string]expectedFixtureEntry{}
		if target.Directory {
			expected["."] = expectedFixtureEntry{Directory: true}
		}
		for _, member := range target.Members {
			content := member.Captured
			if mutated {
				content = member.Mutated
			}
			relative := member.Relative
			if !target.Directory {
				relative = "."
			}
			expected[filepath.Clean(relative)] = expectedFixtureEntry{Content: string(content)}
			for parent := filepath.Dir(relative); target.Directory && parent != "."; parent = filepath.Dir(parent) {
				expected[parent] = expectedFixtureEntry{Directory: true}
			}
		}
		for _, excluded := range target.Excluded {
			if !mutated && len(excluded.RestorePatterns) == 0 {
				continue
			}
			relative := filepath.Clean(excluded.Relative)
			expected[relative] = expectedFixtureEntry{Content: string(excluded.Mutated)}
			for parent := filepath.Dir(relative); parent != "."; parent = filepath.Dir(parent) {
				expected[parent] = expectedFixtureEntry{Directory: true}
			}
		}
		seen := map[string]struct{}{}
		err := filepath.Walk(target.Resolved, func(path string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if plan.context.ValidateSandboxPath(path) != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() && !info.Mode().IsRegular() {
				return fmt.Errorf("unsafe fixture member")
			}
			relative, err := filepath.Rel(target.Resolved, path)
			if !target.Directory {
				relative = "."
			}
			if err != nil {
				return err
			}
			entry, ok := expected[relative]
			if !ok || entry.Directory != info.IsDir() {
				return fmt.Errorf("unexpected fixture member")
			}
			if !info.IsDir() {
				data, _, err := safepath.ReadRegularFile(path)
				if err != nil || string(data) != entry.Content {
					return fmt.Errorf("fixture bytes differ")
				}
			}
			seen[relative] = struct{}{}
			return nil
		})
		if err != nil || len(seen) != len(expected) {
			return fail(CodeContentMismatch, "content", target.Coordinate, "fixture tree differs from the exact expected state")
		}
	}
	return nil
}
