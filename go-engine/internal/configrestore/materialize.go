// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/configdoc"
	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

var errTargetNotRegular = errors.New("target is not a regular file")

type resolvedRestore struct {
	definition    modules.RestoreDef
	source        string
	target        string
	sourceInfo    os.FileInfo
	sourceMissing bool
	registry      *RegistryValue
	canonical     string
	registryClaim bool
}

// Materialize performs read-only preflight and resolves every selected restore
// declaration into concrete actions. It creates no backups or journals and
// performs no target mutation.
func Materialize(ctx context.Context, request Request) (*MaterializedSet, error) {
	target, generation, err := validateRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	if err := PreflightAppClosure(
		ctx,
		generation.RequiresAppClosed,
		request.ProcessPatterns,
		request.ProcessObserver,
	); err != nil {
		return nil, err
	}

	instance := targetConfigInstance(target)
	resolved := make([]resolvedRestore, 0, len(generation.Restore))
	for index, definition := range generation.Restore {
		entry, err := resolveDefinition(request.Stage.Root, definition, instance, index)
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, entry)
	}
	if err := rejectOverlappingTargets(resolved); err != nil {
		return nil, err
	}

	actions := make([]Action, 0, len(resolved))
	for index, entry := range resolved {
		concrete, err := materializeOne(entry, index)
		if err != nil {
			return nil, err
		}
		actions = append(actions, concrete...)
	}
	validations, err := resolveValidations(generation.Validate, resolved)
	if err != nil {
		return nil, err
	}
	return &MaterializedSet{Actions: actions, Validations: validations}, nil
}

func validateRequest(ctx context.Context, request Request) (planner.TargetInstance, *modules.GenerationDef, error) {
	if ctx == nil {
		return planner.TargetInstance{}, nil, newError(CodeInvalidRequest, -1, "", fmt.Errorf("context is nil"))
	}
	if err := ctx.Err(); err != nil {
		return planner.TargetInstance{}, nil, newError(CodeInvalidRequest, -1, "", err)
	}
	if request.Stage == nil || request.Plan.TargetGenerationDef == nil {
		return planner.TargetInstance{}, nil, newError(CodeInvalidRequest, -1, "", fmt.Errorf("stage and pinned target generation are required"))
	}
	if err := safepath.ValidateRoot(request.Stage.Root); err != nil {
		return planner.TargetInstance{}, nil, newError(CodeInvalidRequest, -1, request.Stage.Root, err)
	}
	generation := request.Plan.TargetGenerationDef
	if generation.ID == "" || request.Stage.TargetGeneration != generation.ID ||
		request.Plan.Resolution.TargetGeneration != generation.ID {
		return planner.TargetInstance{}, nil, newError(
			CodeInvalidRequest,
			-1,
			"",
			fmt.Errorf("stage, plan, and pinned generation identities differ"),
		)
	}
	if request.Plan.Resolution.Resolution != planner.ResolutionDirect &&
		request.Plan.Resolution.Resolution != planner.ResolutionMigrate {
		return planner.TargetInstance{}, nil, newError(CodeInvalidRequest, -1, "", fmt.Errorf("plan is not a direct or migrate selection"))
	}

	var selected planner.TargetInstance
	matches := 0
	for _, target := range request.Plan.TargetInstances {
		if target.ID == request.Plan.Resolution.TargetInstanceID {
			selected = target
			matches++
		}
	}
	if matches != 1 || selected.Generation != generation.ID {
		return planner.TargetInstance{}, nil, newError(CodeInvalidRequest, -1, "", fmt.Errorf("plan must pin exactly one matching target instance"))
	}
	if selected.ModuleID != request.Plan.Source.ModuleID ||
		request.Plan.Resolution.ModuleID != request.Plan.Source.ModuleID ||
		request.Plan.Resolution.ConfigSetID != request.Plan.Source.ConfigSetID {
		return planner.TargetInstance{}, nil, newError(CodeInvalidRequest, -1, "", fmt.Errorf("plan identities are inconsistent"))
	}
	return selected, generation, nil
}

func targetConfigInstance(target planner.TargetInstance) modules.ConfigInstance {
	version := modules.NewVersionEvidence(target.RawVersion)
	if target.NormalizedVersion != "" {
		version = modules.VersionEvidence{Raw: target.RawVersion, Normalized: target.NormalizedVersion, Numeric: true}
	}
	return modules.ConfigInstance{
		ID: target.ID, ModuleID: target.ModuleID, DetectorID: target.DetectorID,
		Root: target.Root, Version: version,
		Evidence: modules.InstanceEvidence{
			Type: target.Evidence.Type, AppID: target.Evidence.AppID, Backend: target.Evidence.Backend,
			Platform: target.Evidence.Platform, Ref: target.Evidence.Ref, Driver: target.Evidence.Driver,
		},
	}
}

func resolveDefinition(
	stageRoot string,
	definition modules.RestoreDef,
	instance modules.ConfigInstance,
	index int,
) (resolvedRestore, error) {
	entry := resolvedRestore{definition: definition}
	switch definition.Type {
	case "copy", "merge-json", "merge-ini", "append", "delete-glob":
		target, err := modules.ExpandInstancePath(definition.Target, instance, modules.HostPath)
		if err != nil {
			return entry, newError(CodeUnsafePath, index, definition.Target, err)
		}
		if err := validateConcreteHostPath(target); err != nil {
			return entry, newError(CodeUnsafePath, index, target, err)
		}
		if err := rejectExistingTargetLinks(target); err != nil {
			return entry, newError(CodeUnsafePath, index, target, err)
		}
		entry.target = target
		entry.canonical = canonicalFilesystemTarget(target)
	case "registry-set":
		registryValue, err := resolveRegistryValue(definition, instance)
		if err != nil {
			var typed *Error
			if errors.As(err, &typed) {
				typed.ActionIndex = index
			}
			return entry, err
		}
		entry.registry = registryValue
		entry.target = registryValue.Key + `\` + registryValue.ValueName
		entry.canonical = strings.ToLower(registryValue.Key) + "\x00" + strings.ToLower(registryValue.ValueName)
		entry.registryClaim = true
		return entry, nil
	case "registry-import":
		return entry, newError(CodeUnsupportedRestore, index, definition.Target, fmt.Errorf("whole-key registry-import is unsupported for schema-v2 config restore"))
	default:
		return entry, newError(CodeUnsupportedRestore, index, definition.Target, fmt.Errorf("unsupported restore type %q", definition.Type))
	}

	if definition.Type == "delete-glob" {
		if err := validateRelativeGlob(definition.Pattern); err != nil {
			return entry, newError(CodeUnsafePath, index, entry.target, err)
		}
		info, err := os.Lstat(entry.target)
		if err == nil && !info.IsDir() {
			return entry, newError(CodeUnsupportedFileType, index, entry.target, fmt.Errorf("delete-glob target must be a directory"))
		}
		if err != nil && !os.IsNotExist(err) {
			return entry, newError(CodeMaterialization, index, entry.target, err)
		}
		return entry, nil
	}
	if definition.Source == "" {
		return entry, newError(CodeUnsafePath, index, entry.target, fmt.Errorf("restore source is empty"))
	}
	source, err := safepath.Resolve(stageRoot, definition.Source)
	if err != nil {
		return entry, newError(CodeUnsafePath, index, entry.target, err)
	}
	entry.source = source
	info, err := os.Lstat(source)
	if os.IsNotExist(err) && definition.Optional {
		entry.sourceMissing = true
		return entry, nil
	}
	if os.IsNotExist(err) {
		return entry, newError(CodeSourceMissing, index, entry.target, err)
	}
	if err != nil {
		return entry, newError(CodeMaterialization, index, entry.target, err)
	}
	if definition.Type != "copy" && !info.Mode().IsRegular() {
		return entry, newError(CodeUnsupportedFileType, index, entry.target, fmt.Errorf("%s source must be a regular file", definition.Type))
	}
	if definition.Type == "copy" && !info.Mode().IsRegular() && !info.IsDir() {
		return entry, newError(CodeUnsupportedFileType, index, entry.target, fmt.Errorf("copy source must be a regular file or directory"))
	}
	entry.sourceInfo = info
	return entry, nil
}

func validateConcreteHostPath(target string) error {
	if target == "" || !filepath.IsAbs(target) || filepath.Clean(target) != target ||
		strings.HasPrefix(target, `\\`) || strings.HasPrefix(target, "//") {
		return fmt.Errorf("resolved target must be a clean local absolute path")
	}
	return nil
}

func rejectOverlappingTargets(entries []resolvedRestore) error {
	for left := 0; left < len(entries); left++ {
		for right := left + 1; right < len(entries); right++ {
			if entries[left].registryClaim != entries[right].registryClaim {
				continue
			}
			overlaps := entries[left].canonical == entries[right].canonical
			if !entries[left].registryClaim {
				overlaps = filesystemTargetsOverlap(entries[left].canonical, entries[right].canonical)
			}
			if overlaps {
				return &Error{
					Code: CodeTargetOverlap, ActionIndex: right, ValidationIndex: -1, MappingCount: -1,
					Target: entries[right].target,
					Err:    fmt.Errorf("target overlaps restore[%d] target %q", left, entries[left].target),
				}
			}
		}
	}
	return nil
}

func canonicalFilesystemTarget(target string) string {
	canonical := filepath.ToSlash(filepath.Clean(target))
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}
	return strings.TrimSuffix(canonical, "/")
}

func filesystemTargetsOverlap(left, right string) bool {
	return left == right || strings.HasPrefix(left, right+"/") || strings.HasPrefix(right, left+"/")
}

func materializeOne(entry resolvedRestore, index int) ([]Action, error) {
	if entry.sourceMissing {
		return []Action{}, nil
	}
	switch entry.definition.Type {
	case "copy":
		return []Action{{
			Kind: ActionCopy, Strategy: "copy", Source: entry.source, Target: entry.target,
			SourceMode: entry.sourceInfo.Mode(), SourceIsDirectory: entry.sourceInfo.IsDir(),
			Exclude: append([]string(nil), entry.definition.Exclude...), SnapshotRequired: true,
		}}, nil
	case "merge-json":
		content, err := materializeJSONMerge(entry.source, entry.target)
		if err != nil {
			return nil, newError(materializationCode(err, CodeMalformedJSON), index, entry.target, err)
		}
		return []Action{writeAction(entry, content)}, nil
	case "merge-ini":
		content, err := materializeINIMerge(entry.source, entry.target)
		if err != nil {
			return nil, newError(materializationCode(err, CodeMaterialization), index, entry.target, err)
		}
		return []Action{writeAction(entry, content)}, nil
	case "append":
		content, err := materializeAppend(entry.source, entry.target)
		if err != nil {
			return nil, newError(materializationCode(err, CodeMaterialization), index, entry.target, err)
		}
		return []Action{writeAction(entry, content)}, nil
	case "delete-glob":
		return materializeDeleteGlob(entry, index)
	case "registry-set":
		value := *entry.registry
		return []Action{{
			Kind: ActionRegistrySet, Strategy: "registry-set", Target: entry.target,
			RegistryValue: &value, SnapshotRequired: true,
		}}, nil
	default:
		return nil, newError(CodeUnsupportedRestore, index, entry.target, fmt.Errorf("unsupported restore type %q", entry.definition.Type))
	}
}

func writeAction(entry resolvedRestore, content []byte) Action {
	return Action{
		Kind: ActionWriteFile, Strategy: entry.definition.Type, Source: entry.source, Target: entry.target,
		DesiredContent: append([]byte(nil), content...), SnapshotRequired: true,
	}
}

func materializeJSONMerge(source, target string) ([]byte, error) {
	sourceData, _, err := safepath.ReadRegularFile(source)
	if err != nil {
		return nil, err
	}
	sourceDocument, err := configdoc.ParseJSON(sourceData)
	if err != nil {
		return nil, err
	}
	targetData, exists, err := readOptionalRegularFile(target)
	if err != nil {
		return nil, err
	}
	var targetDocument any = map[string]any{}
	if exists && len(targetData) > 0 {
		targetDocument, err = configdoc.ParseJSON(targetData)
		if err != nil {
			return nil, err
		}
	}
	return configdoc.EncodeJSON(restore.DeepMerge(targetDocument, sourceDocument))
}

func materializeINIMerge(source, target string) ([]byte, error) {
	sourceData, _, err := safepath.ReadRegularFile(source)
	if err != nil {
		return nil, err
	}
	targetData, _, err := readOptionalRegularFile(target)
	if err != nil {
		return nil, err
	}
	return []byte(restore.FormatIni(restore.MergeIni(
		restore.ParseIni(string(targetData)),
		restore.ParseIni(string(sourceData)),
	))), nil
}

func materializeAppend(source, target string) ([]byte, error) {
	sourceData, _, err := safepath.ReadRegularFile(source)
	if err != nil {
		return nil, err
	}
	targetData, targetExists, err := readOptionalRegularFile(target)
	if err != nil {
		return nil, err
	}
	if !targetExists {
		result := append([]byte(nil), sourceData...)
		if len(result) > 0 && result[len(result)-1] != '\n' {
			result = append(result, '\n')
		}
		return result, nil
	}

	targetContent := string(targetData)
	present := make(map[string]struct{})
	for _, line := range splitNonEmpty(targetContent) {
		present[line] = struct{}{}
	}
	missing := make([]string, 0)
	for _, line := range splitNonEmpty(string(sourceData)) {
		if _, exists := present[line]; !exists {
			missing = append(missing, line)
		}
	}
	merged := targetContent
	if len(missing) > 0 {
		if merged != "" && !strings.HasSuffix(merged, "\n") {
			merged += "\n"
		}
		merged += strings.Join(missing, "\n") + "\n"
	}
	if merged != "" && !strings.HasSuffix(merged, "\n") {
		merged += "\n"
	}
	return []byte(merged), nil
}

func splitNonEmpty(content string) []string {
	lines := strings.Split(content, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func readOptionalRegularFile(target string) ([]byte, bool, error) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, errTargetNotRegular
	}
	data, _, err := safepath.ReadRegularFile(target)
	return data, true, err
}

func materializationCode(err error, fallback Code) Code {
	if errors.Is(err, errTargetNotRegular) {
		return CodeUnsupportedFileType
	}
	var pathError *safepath.Error
	if errors.As(err, &pathError) {
		return CodeUnsafePath
	}
	if configdoc.CodeOf(err) == configdoc.CodeMalformedJSON {
		return CodeMalformedJSON
	}
	return fallback
}

func materializeDeleteGlob(entry resolvedRestore, index int) ([]Action, error) {
	pattern := strings.ReplaceAll(entry.definition.Pattern, `\`, "/")
	matches, err := expandDeleteGlob(entry.target, strings.Split(pattern, "/"))
	if err != nil {
		return nil, newError(CodeUnsafePath, index, entry.target, err)
	}
	sort.Slice(matches, func(left, right int) bool { return stablePathLess(matches[left], matches[right]) })
	actions := make([]Action, 0, len(matches))
	for _, match := range matches {
		if !pathContained(entry.target, match) {
			return nil, newError(CodeUnsafePath, index, match, fmt.Errorf("glob match escaped target directory"))
		}
		if err := rejectExistingTargetLinks(match); err != nil {
			return nil, newError(CodeUnsafePath, index, match, err)
		}
		info, err := os.Lstat(match)
		if err != nil {
			return nil, newError(CodeMaterialization, index, match, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		actions = append(actions, Action{
			Kind: ActionDeleteFile, Strategy: "delete-glob", Target: filepath.Clean(match), SnapshotRequired: true,
		})
	}
	return actions, nil
}

// expandDeleteGlob resolves one component at a time and checks every matched
// directory entry before descending. filepath.Glob cannot be used here because
// it may enumerate through a directory symlink before the resulting path can
// be rejected.
func expandDeleteGlob(root string, components []string) ([]string, error) {
	if err := rejectExistingTargetLinks(root); err != nil {
		return nil, err
	}
	if len(components) == 0 {
		return []string{}, nil
	}
	var matches []string
	var visit func(string, int) error
	visit = func(current string, componentIndex int) error {
		entries, err := os.ReadDir(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		for _, directoryEntry := range entries {
			matched, err := filepath.Match(components[componentIndex], directoryEntry.Name())
			if err != nil {
				return err
			}
			if !matched {
				continue
			}
			candidate := filepath.Join(current, directoryEntry.Name())
			info, err := directoryEntry.Info()
			if err != nil {
				return err
			}
			if isLinkOrReparse(info) {
				return fmt.Errorf("delete-glob path component %q is a link or reparse point", candidate)
			}
			if componentIndex == len(components)-1 {
				if info.Mode().IsRegular() {
					matches = append(matches, candidate)
				}
				continue
			}
			if info.IsDir() {
				if err := visit(candidate, componentIndex+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(root, 0); err != nil {
		return nil, err
	}
	if matches == nil {
		return []string{}, nil
	}
	return matches, nil
}

func validateRelativeGlob(pattern string) error {
	if pattern == "" || pattern != strings.TrimSpace(pattern) || filepath.IsAbs(pattern) || filepath.VolumeName(pattern) != "" ||
		strings.ContainsAny(pattern, "$%~\x00") {
		return fmt.Errorf("delete-glob pattern must be a portable relative glob")
	}
	normalized := strings.ReplaceAll(pattern, `\`, "/")
	if strings.HasPrefix(normalized, "/") {
		return fmt.Errorf("delete-glob pattern must be relative")
	}
	for _, component := range strings.Split(normalized, "/") {
		if component == "" || component == "." || component == ".." || component != strings.TrimSpace(component) {
			return fmt.Errorf("delete-glob pattern contains an unsafe path component")
		}
	}
	if _, err := filepath.Match(filepath.FromSlash(normalized), "probe"); err != nil {
		return fmt.Errorf("invalid delete-glob pattern: %w", err)
	}
	return nil
}

func stablePathLess(left, right string) bool {
	leftFolded := strings.ToLower(filepath.ToSlash(left))
	rightFolded := strings.ToLower(filepath.ToSlash(right))
	if leftFolded != rightFolded {
		return leftFolded < rightFolded
	}
	return left < right
}

func pathContained(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func resolveValidations(definitions []modules.ValidationDef, entries []resolvedRestore) ([]configvalidate.ResolvedValidation, error) {
	resolved := make([]configvalidate.ResolvedValidation, 0, len(definitions))
	for index, definition := range definitions {
		mappings := make([]string, 0, 1)
		for _, entry := range entries {
			if entry.sourceMissing || entry.source == "" || entry.registryClaim || entry.definition.Type == "delete-glob" {
				continue
			}
			hostPath, matches, err := mapValidationTarget(definition.Path, entry)
			if err != nil {
				return nil, &Error{
					Code: CodeValidationMapping, ActionIndex: -1, ValidationIndex: index, MappingCount: len(mappings),
					Err: err,
				}
			}
			if matches {
				mappings = append(mappings, hostPath)
			}
		}
		if len(mappings) != 1 {
			return nil, &Error{
				Code: CodeValidationMapping, ActionIndex: -1, ValidationIndex: index, MappingCount: len(mappings),
				Err: fmt.Errorf("validation path %q must map through exactly one restore source", definition.Path),
			}
		}
		resolved = append(resolved, configvalidate.ResolvedValidation{Definition: definition, HostPath: mappings[0]})
	}
	return resolved, nil
}

func mapValidationTarget(validationPath string, entry resolvedRestore) (string, bool, error) {
	validation := canonicalPortablePath(validationPath)
	source := canonicalPortablePath(entry.definition.Source)
	if validation == "" || source == "" {
		return "", false, nil
	}
	if validation == source {
		return entry.target, true, nil
	}
	if entry.definition.Type != "copy" || entry.sourceInfo == nil || !entry.sourceInfo.IsDir() ||
		!strings.HasPrefix(validation, source+"/") {
		return "", false, nil
	}
	suffix := strings.TrimPrefix(validation, source+"/")
	candidate := filepath.Clean(filepath.Join(entry.target, filepath.FromSlash(suffix)))
	if !pathContained(entry.target, candidate) {
		return "", false, fmt.Errorf("validation suffix escapes concrete copy target")
	}
	return candidate, true, nil
}

func canonicalPortablePath(value string) string {
	normalized := filepath.ToSlash(filepath.Clean(strings.ReplaceAll(value, `\`, "/")))
	if runtime.GOOS == "windows" {
		normalized = strings.ToLower(normalized)
	}
	return normalized
}

func resolveRegistryValue(definition modules.RestoreDef, instance modules.ConfigInstance) (*RegistryValue, error) {
	key, err := modules.ExpandInstanceTemplate(definition.Key, instance)
	if err != nil {
		return nil, newError(CodeInvalidRegistryTarget, -1, definition.Key, err)
	}
	valueName, err := modules.ExpandInstanceTemplate(definition.ValueName, instance)
	if err != nil {
		return nil, newError(CodeInvalidRegistryValue, -1, definition.Key, err)
	}
	key, err = normalizeHKCUKey(key)
	if err != nil {
		return nil, newError(CodeInvalidRegistryTarget, -1, definition.Key, err)
	}
	if valueName == "" || valueName != strings.TrimSpace(valueName) || containsControl(valueName) {
		return nil, newError(CodeInvalidRegistryValue, -1, key, fmt.Errorf("registry-set requires a non-empty clean value name"))
	}
	valueType := strings.ToUpper(strings.TrimSpace(definition.ValueType))
	switch valueType {
	case "REG_DWORD":
		if _, err := parseDWORD(definition.Data); err != nil {
			return nil, newError(CodeInvalidRegistryValue, -1, key, err)
		}
	case "REG_SZ", "REG_EXPAND_SZ":
		if strings.ContainsRune(definition.Data, '\x00') {
			return nil, newError(CodeInvalidRegistryValue, -1, key, fmt.Errorf("registry string data contains NUL"))
		}
	default:
		return nil, newError(CodeInvalidRegistryValue, -1, key, fmt.Errorf("unsupported registry value type %q", definition.ValueType))
	}
	return &RegistryValue{Key: key, ValueName: valueName, ValueType: valueType, Data: definition.Data}, nil
}

func normalizeHKCUKey(key string) (string, error) {
	if key == "" || key != strings.TrimSpace(key) || containsControl(key) {
		return "", fmt.Errorf("registry key must be non-empty and clean")
	}
	normalized := strings.ReplaceAll(key, "/", `\`)
	components := strings.Split(normalized, `\`)
	if len(components) < 2 || (!strings.EqualFold(components[0], "HKCU") && !strings.EqualFold(components[0], "HKEY_CURRENT_USER")) {
		return "", fmt.Errorf("registry-set only supports an HKCU subkey")
	}
	for _, component := range components[1:] {
		if component == "" || component == "." || component == ".." || component != strings.TrimSpace(component) {
			return "", fmt.Errorf("registry key contains an invalid component")
		}
	}
	return "HKCU" + `\` + strings.Join(components[1:], `\`), nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if character < 0x20 {
			return true
		}
	}
	return false
}

func parseDWORD(value string) (uint64, error) {
	trimmed := strings.TrimSpace(value)
	base := 10
	if strings.HasPrefix(strings.ToLower(trimmed), "0x") {
		base = 0
	}
	parsed, err := strconv.ParseUint(trimmed, base, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid REG_DWORD data %q: %w", value, err)
	}
	return parsed, nil
}
