// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"fmt"
	"os"
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
	context     *validationmode.Context
	Compiled    v2CompiledFixture
	Instance    modules.ConfigInstance
	CaptureID   string
	Targets     []V2FixtureTarget
	Validations int
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
	plan := &V2FixturePlan{
		context: context, Compiled: compiled, Instance: instance,
		CaptureID: bundle.CaptureID(mod.ID, compiled.Set.ID, instance.ID),
	}
	for index, entry := range compiled.Entries {
		policy := validationmode.HostPathPolicy{InstanceRoot: instance.Root}
		if compiled.Detector.Type == "path" {
			policy.InstanceAlias = v2DetectorAlias(compiled.Detector.Glob)
		}
		capturePolicy := policy
		capturePolicy.AllowRoot = strings.EqualFold(entry.Capture.Source, "${instance.root}")
		source, err := context.ResolveHostPath(entry.Capture.Source, capturePolicy)
		if err != nil {
			return bad(entry.Shape.CaptureCoordinate, "capture source did not resolve inside detector authority")
		}
		restorePolicy := policy
		restorePolicy.AllowRoot = strings.EqualFold(entry.Restore.Target, "${instance.root}")
		target, err := context.ResolveHostPath(entry.Restore.Target, restorePolicy)
		if err != nil || filepath.Clean(target) != filepath.Clean(source) {
			return bad(entry.Shape.RestoreCoordinate, "direct capture/restore targets did not resolve identically")
		}
		targetPlan := V2FixtureTarget{
			Coordinate: entry.Shape.CaptureCoordinate, Authored: entry.Restore.Target,
			Destination: filepath.ToSlash(entry.Capture.Dest), Resolved: source,
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
				targetPlan.Members = append(targetPlan.Members, V2FixtureFile{
					Relative: filepath.ToSlash(member.Path), Path: filepath.Join(source, filepath.FromSlash(member.Path)),
					Captured: captured, Mutated: mutated,
				})
			}
			witnesses, witnessFailure := compileV2ExcludeWitnesses(mod.ID, scenario.ID, source, entry)
			if witnessFailure != nil {
				return nil, witnessFailure
			}
			targetPlan.Excluded = witnesses
		} else {
			if failure := proveV2SingleFileExcludes(entry); failure != nil {
				return nil, failure
			}
			captured, mutated, err := v2FixtureContents(mod.ID, scenario.ID, entry.Shape.CaptureCoordinate, entry.Shape.Format)
			if err != nil {
				return unsupported(entry.Shape.CaptureCoordinate, "fixture content cannot satisfy its declared format")
			}
			targetPlan.Members = []V2FixtureFile{{Relative: ".", Path: source, Captured: captured, Mutated: mutated}}
		}
		if context.ValidateSandboxPath(source) != nil {
			return bad(entry.Shape.CaptureCoordinate, "resolved fixture target left validation authority")
		}
		plan.Targets = append(plan.Targets, targetPlan)
		plan.Validations += len(entry.Validations)
	}
	if len(plan.Targets) == 0 || plan.Validations != len(compiled.Generation.Validate) || plan.Validations == 0 {
		return unsupported("operations", "fixture plan is vacuous or omits production validation")
	}
	return plan, nil
}

func proveV2SingleFileExcludes(entry v2CompiledEntry) *Failure {
	candidates := []string{
		filepath.Base(entry.Capture.Source), filepath.Base(entry.Capture.Dest),
		filepath.Base(entry.Restore.Source), filepath.Base(entry.Restore.Target),
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
	if alias == "" || filepath.Base(compiled.Definition.InstanceLocator) != compiled.Definition.InstanceLocator || strings.ContainsAny(compiled.Definition.InstanceLocator, `\/`) {
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
	if matched, matchErr := filepath.Match(filepath.Base(pattern), filepath.Base(root)); matchErr != nil || !matched {
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
	if len(instances) != 1 || instances[0].Root != root || instances[0].Evidence.Type != "path" || instances[0].Version.Raw != compiled.Definition.SourceVersion {
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
		if !ok || !safeArtifactName(filepath.ToSlash(relative)) || filepath.Clean(path) == filepath.Clean(root) || !fixtureContained(root, path) || !v2MatchesExclude(relative, pattern) {
			return fail(CodeUnsupportedFixture, "fixture", "exclude", "production exclude has no deterministic witness")
		}
		key := strings.ToLower(filepath.ToSlash(relative))
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
		key := strings.ToLower(filepath.ToSlash(relative))
		role := byRelative[key]
		captured, mutated, err := v2FixtureContents(moduleID, scenarioID, fmt.Sprintf("exclude[%d]", index), v2FormatFile)
		if err != nil {
			return nil, fail(CodeUnsupportedFixture, "fixture", "exclude", "build exclude witness")
		}
		result = append(result, V2ExcludedFixture{
			Relative: filepath.ToSlash(relative), Path: filepath.Join(root, filepath.FromSlash(relative)),
			Captured: captured, Mutated: mutated,
			CapturePatterns: append([]string(nil), role.capture...), RestorePatterns: append([]string(nil), role.restore...),
		})
	}
	return result, nil
}

func v2ExcludeWitness(raw string) (string, bool) {
	pattern := filepath.ToSlash(raw)
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
	normalized := filepath.ToSlash(relative)
	pattern := filepath.ToSlash(rawPattern)
	stripped := strings.TrimPrefix(pattern, "**/")
	if strings.HasSuffix(stripped, "/**") {
		directory := strings.TrimSuffix(stripped, "/**")
		bounded := "/" + strings.Trim(normalized, "/") + "/"
		return strings.Contains(strings.ToLower(bounded), "/"+strings.ToLower(directory)+"/")
	}
	for _, segment := range strings.Split(normalized, "/") {
		if matched, _ := filepath.Match(stripped, segment); matched {
			return true
		}
	}
	return false
}

func (plan *V2FixturePlan) MaterializeCaptured() *Failure { return plan.materialize(false) }
func (plan *V2FixturePlan) Mutate() *Failure              { return plan.materialize(true) }
func (plan *V2FixturePlan) CompareCaptured() *Failure     { return plan.compare(false) }
func (plan *V2FixturePlan) CompareMutated() *Failure      { return plan.compare(true) }

func (plan *V2FixturePlan) materialize(mutated bool) *Failure {
	for index := range plan.Targets {
		target := &plan.Targets[index]
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
