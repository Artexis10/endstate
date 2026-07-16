// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/configdoc"
)

const (
	DiagnosticInvalidJSON                = "invalid_json"
	DiagnosticDuplicateModuleID          = "duplicate_module_id"
	DiagnosticSchemaVersionRequired      = "module_schema_version_required"
	DiagnosticUnsupportedSchema          = "unsupported_module_schema"
	DiagnosticInvalidID                  = "invalid_id"
	DiagnosticDuplicateID                = "duplicate_id"
	DiagnosticUnknownDetector            = "unknown_instance_detector"
	DiagnosticInvalidDetector            = "invalid_instance_detector"
	DiagnosticMissingConfigSet           = "missing_config_set"
	DiagnosticInvalidGenerationOrder     = "invalid_generation_order"
	DiagnosticInvalidVersionSelector     = "invalid_version_selector"
	DiagnosticInvalidVersionRange        = "invalid_version_range"
	DiagnosticInvalidVersionPattern      = "invalid_version_pattern"
	DiagnosticInvalidPlaceholder         = "invalid_placeholder"
	DiagnosticUnsafePath                 = "unsafe_path"
	DiagnosticUnknownGeneration          = "unknown_generation"
	DiagnosticInvalidMigrationEdge       = "invalid_migration_edge"
	DiagnosticDuplicateMigrationEdge     = "duplicate_migration_edge"
	DiagnosticMigrationCycle             = "migration_cycle"
	DiagnosticAmbiguousMigrationRoute    = "ambiguous_migration_route"
	DiagnosticUnknownMigrationOperation  = "unknown_migration_operation"
	DiagnosticInvalidMigrationOperation  = "invalid_migration_operation"
	DiagnosticMissingMigrationValidation = "missing_migration_validation"
	DiagnosticUnknownValidation          = "unknown_validation"
	DiagnosticInvalidValidation          = "invalid_validation"
)

var (
	stableIDPattern     = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[-._][a-z0-9]+)*$`)
	windowsAbsolutePath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
	placeholderPattern  = regexp.MustCompile(`\$\{([^}]*)\}`)
)

// ModuleValidationError is a structured catalog/schema failure.
type ModuleValidationError struct {
	Code     string
	ModuleID string
	FilePath string
	Detail   string
}

func (e *ModuleValidationError) Error() string {
	location := e.FilePath
	if location == "" {
		location = "<module>"
	}
	return fmt.Sprintf("invalid config module %q at %s: %s", e.ModuleID, location, e.Detail)
}

// DiagnosticCode extracts a stable machine-readable validation code.
func DiagnosticCode(err error) string {
	var validationError *ModuleValidationError
	if errors.As(err, &validationError) {
		return validationError.Code
	}
	return ""
}

func validationError(mod *Module, filePath, code, format string, args ...any) error {
	moduleID := ""
	if mod != nil {
		moduleID = mod.ID
	}
	return &ModuleValidationError{
		Code:     code,
		ModuleID: moduleID,
		FilePath: filePath,
		Detail:   fmt.Sprintf(format, args...),
	}
}

func validateModuleV2(mod *Module, filePath string) error {
	schemaVersion := mod.EffectiveSchemaVersion()
	if schemaVersion != 1 && schemaVersion != 2 {
		return validationError(mod, filePath, DiagnosticUnsupportedSchema, "moduleSchemaVersion %d is not supported", schemaVersion)
	}
	if mod.Config != nil && schemaVersion != 2 {
		return validationError(mod, filePath, DiagnosticSchemaVersionRequired, "config generations require moduleSchemaVersion 2")
	}
	if schemaVersion == 1 || mod.Config == nil {
		return nil
	}
	if err := validateStableID(mod, filePath, "module", mod.ID); err != nil {
		return err
	}

	if len(mod.Config.InstanceDetectors) == 0 {
		return validationError(mod, filePath, DiagnosticInvalidDetector, "config.instanceDetectors must contain at least one detector")
	}
	detectorIDs := make(map[string]struct{}, len(mod.Config.InstanceDetectors))
	for index := range mod.Config.InstanceDetectors {
		detector := &mod.Config.InstanceDetectors[index]
		if err := validateStableID(mod, filePath, "instance detector", detector.ID); err != nil {
			return err
		}
		if _, exists := detectorIDs[detector.ID]; exists {
			return validationError(mod, filePath, DiagnosticDuplicateID, "duplicate instance detector id %q", detector.ID)
		}
		detectorIDs[detector.ID] = struct{}{}
		switch detector.Type {
		case "package":
			if detector.Glob != "" || detector.VersionPattern != "" {
				return validationError(mod, filePath, DiagnosticInvalidDetector, "package detector %q cannot declare glob or versionPattern", detector.ID)
			}
		case "path":
			if strings.TrimSpace(detector.Glob) == "" {
				return validationError(mod, filePath, DiagnosticInvalidDetector, "path detector %q requires glob", detector.ID)
			}
			if err := validateTemplatePlaceholders(detector.Glob); err != nil {
				return validationError(mod, filePath, DiagnosticInvalidPlaceholder, "path detector %q glob: %v", detector.ID, err)
			}
			if hasTraversal(detector.Glob) {
				return validationError(mod, filePath, DiagnosticUnsafePath, "path detector %q glob traverses a parent directory", detector.ID)
			}
			if detector.VersionPattern != "" {
				if err := validateVersionExtractionPattern(detector.VersionPattern); err != nil {
					return validationError(mod, filePath, DiagnosticInvalidVersionPattern, "path detector %q: %v", detector.ID, err)
				}
			}
		default:
			return validationError(mod, filePath, DiagnosticUnknownDetector, "instance detector %q uses unsupported type %q", detector.ID, detector.Type)
		}
	}

	if len(mod.Config.Sets) == 0 {
		return validationError(mod, filePath, DiagnosticMissingConfigSet, "config.sets must contain at least one config set")
	}
	setIDs := make(map[string]struct{}, len(mod.Config.Sets))
	for setIndex := range mod.Config.Sets {
		set := &mod.Config.Sets[setIndex]
		if err := validateStableID(mod, filePath, "config set", set.ID); err != nil {
			return err
		}
		if _, exists := setIDs[set.ID]; exists {
			return validationError(mod, filePath, DiagnosticDuplicateID, "duplicate config set id %q", set.ID)
		}
		setIDs[set.ID] = struct{}{}
		if err := validateConfigSet(mod, filePath, set); err != nil {
			return err
		}
	}
	return nil
}

func validateConfigSet(mod *Module, filePath string, set *ConfigSetDef) error {
	if len(set.Generations) == 0 {
		return validationError(mod, filePath, DiagnosticMissingConfigSet, "config set %q must contain at least one generation", set.ID)
	}
	generationIDs := make(map[string]*GenerationDef, len(set.Generations))
	orders := make(map[int]string, len(set.Generations))
	for generationIndex := range set.Generations {
		generation := &set.Generations[generationIndex]
		if err := validateStableID(mod, filePath, "generation", generation.ID); err != nil {
			return err
		}
		if _, exists := generationIDs[generation.ID]; exists {
			return validationError(mod, filePath, DiagnosticDuplicateID, "config set %q contains duplicate generation id %q", set.ID, generation.ID)
		}
		generationIDs[generation.ID] = generation
		if generation.Order <= 0 {
			return validationError(mod, filePath, DiagnosticInvalidGenerationOrder, "generation %q in config set %q must have positive order", generation.ID, set.ID)
		}
		if other, exists := orders[generation.Order]; exists {
			return validationError(mod, filePath, DiagnosticInvalidGenerationOrder, "generations %q and %q in config set %q share order %d", other, generation.ID, set.ID, generation.Order)
		}
		orders[generation.Order] = generation.ID

		for selectorIndex, selector := range generation.Matches {
			hasRange := selector.VersionRange != ""
			hasPattern := selector.VersionPattern != ""
			if hasRange == hasPattern {
				return validationError(mod, filePath, DiagnosticInvalidVersionSelector, "generation %q selector %d must declare exactly one of versionRange or versionPattern", generation.ID, selectorIndex)
			}
			if hasRange {
				if _, err := MatchNumericVersionRange("0", selector.VersionRange); err != nil {
					return validationError(mod, filePath, DiagnosticInvalidVersionRange, "generation %q has invalid versionRange %q: %v", generation.ID, selector.VersionRange, err)
				}
			} else if err := validateAnchoredPattern(selector.VersionPattern); err != nil {
				return validationError(mod, filePath, DiagnosticInvalidVersionPattern, "generation %q: %v", generation.ID, err)
			}
		}
		if err := validateGenerationPaths(mod, filePath, set.ID, generation); err != nil {
			return err
		}
	}
	return validateMigrationGraph(mod, filePath, set, generationIDs)
}

func validateGenerationPaths(mod *Module, filePath, setID string, generation *GenerationDef) error {
	if generation.Capture != nil {
		for index, captureFile := range generation.Capture.Files {
			if err := validateHostTemplatePath(captureFile.Source); err != nil {
				return validationError(mod, filePath, DiagnosticCodeForPathError(err), "config set %q generation %q capture.files[%d].source: %v", setID, generation.ID, index, err)
			}
			if err := validatePortablePath(captureFile.Dest); err != nil {
				return validationError(mod, filePath, DiagnosticCodeForPathError(err), "config set %q generation %q capture.files[%d].dest: %v", setID, generation.ID, index, err)
			}
		}
		for index, captureKey := range generation.Capture.RegistryKeys {
			if err := validatePortablePath(captureKey.Dest); err != nil {
				return validationError(mod, filePath, DiagnosticCodeForPathError(err), "config set %q generation %q capture.registryKeys[%d].dest: %v", setID, generation.ID, index, err)
			}
		}
	}
	for index, restore := range generation.Restore {
		if restore.Source != "" {
			if err := validateStagingPath(restore.Source); err != nil {
				return validationError(mod, filePath, DiagnosticCodeForPathError(err), "config set %q generation %q restore[%d].source: %v", setID, generation.ID, index, err)
			}
		}
		if restore.Target != "" {
			if err := validateHostTemplatePath(restore.Target); err != nil {
				return validationError(mod, filePath, DiagnosticCodeForPathError(err), "config set %q generation %q restore[%d].target: %v", setID, generation.ID, index, err)
			}
		}
	}
	for index, validation := range generation.Validate {
		if err := validateValidation(validation); err != nil {
			code := DiagnosticCodeForPathError(err)
			if code == "" {
				code = validationDiagnosticCode(validation.Type)
			}
			return validationError(mod, filePath, code, "config set %q generation %q validate[%d]: %v", setID, generation.ID, index, err)
		}
	}
	return nil
}

func validateMigrationGraph(mod *Module, filePath string, set *ConfigSetDef, generations map[string]*GenerationDef) error {
	adjacency := make(map[string][]string, len(generations))
	edges := make(map[string]struct{}, len(set.Migrations))
	for edgeIndex, edge := range set.Migrations {
		from, fromExists := generations[edge.From]
		to, toExists := generations[edge.To]
		if !fromExists || !toExists {
			return validationError(mod, filePath, DiagnosticUnknownGeneration, "config set %q migration[%d] references unknown generation %q -> %q", set.ID, edgeIndex, edge.From, edge.To)
		}
		if edge.From == edge.To {
			return validationError(mod, filePath, DiagnosticInvalidMigrationEdge, "config set %q migration[%d] has same source and target %q", set.ID, edgeIndex, edge.From)
		}
		key := edge.From + "\x00" + edge.To
		if _, exists := edges[key]; exists {
			return validationError(mod, filePath, DiagnosticDuplicateMigrationEdge, "config set %q has duplicate migration edge %q -> %q", set.ID, edge.From, edge.To)
		}
		edges[key] = struct{}{}
		adjacency[edge.From] = append(adjacency[edge.From], edge.To)
		if len(edge.Operations) == 0 {
			return validationError(mod, filePath, DiagnosticInvalidMigrationOperation, "config set %q migration %q -> %q has no operations", set.ID, edge.From, edge.To)
		}
		for operationIndex, operation := range edge.Operations {
			if err := validateMigrationOperation(operation); err != nil {
				code := DiagnosticCodeForPathError(err)
				if code == "" {
					if isAllowedMigrationOperation(operation.Type) {
						code = DiagnosticInvalidMigrationOperation
					} else {
						code = DiagnosticUnknownMigrationOperation
					}
				}
				return validationError(mod, filePath, code, "config set %q migration %q -> %q operation[%d]: %v", set.ID, edge.From, edge.To, operationIndex, err)
			}
		}
		if len(edge.Validate) == 0 {
			return validationError(mod, filePath, DiagnosticMissingMigrationValidation, "config set %q migration %q -> %q requires validation", set.ID, edge.From, edge.To)
		}
		for validationIndex, validation := range edge.Validate {
			if err := validateValidation(validation); err != nil {
				code := DiagnosticCodeForPathError(err)
				if code == "" {
					code = validationDiagnosticCode(validation.Type)
				}
				return validationError(mod, filePath, code, "config set %q migration %q -> %q validate[%d]: %v", set.ID, edge.From, edge.To, validationIndex, err)
			}
		}
		_ = from
		_ = to
	}

	if hasMigrationCycle(generations, adjacency) {
		return validationError(mod, filePath, DiagnosticMigrationCycle, "config set %q migration graph contains a cycle", set.ID)
	}
	for _, edge := range set.Migrations {
		if generations[edge.To].Order <= generations[edge.From].Order {
			return validationError(mod, filePath, DiagnosticInvalidMigrationEdge, "config set %q migration %q -> %q is not forward by generation order", set.ID, edge.From, edge.To)
		}
	}
	if from, to, ambiguous := findAmbiguousRoute(generations, adjacency); ambiguous {
		return validationError(mod, filePath, DiagnosticAmbiguousMigrationRoute, "config set %q has more than one migration route from %q to %q", set.ID, from, to)
	}
	return nil
}

func validateMigrationOperation(operation MigrationOperationDef) error {
	if !isAllowedMigrationOperation(operation.Type) {
		return fmt.Errorf("unsupported migration operation %q", operation.Type)
	}
	validatePaths := func(paths ...string) error {
		for _, path := range paths {
			if err := validateStagingPath(path); err != nil {
				return err
			}
		}
		return nil
	}
	require := func(name, value string) error {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required for %s", name, operation.Type)
		}
		return nil
	}

	switch operation.Type {
	case "file-copy", "file-move":
		if err := require("source", operation.Source); err != nil {
			return err
		}
		if err := require("target", operation.Target); err != nil {
			return err
		}
		return validatePaths(operation.Source, operation.Target)
	case "file-delete":
		if err := require("path", operation.Path); err != nil {
			return err
		}
		return validatePaths(operation.Path)
	case "json-set", "json-delete":
		if err := require("path", operation.Path); err != nil {
			return err
		}
		if err := require("jsonPath", operation.JSONPath); err != nil {
			return err
		}
		if err := validatePaths(operation.Path); err != nil {
			return err
		}
		if err := configdoc.ValidateJSONPath(operation.JSONPath); err != nil {
			return fmt.Errorf("invalid jsonPath for %s: %w", operation.Type, err)
		}
		if operation.Type == "json-delete" && operation.JSONPath == "$" {
			return fmt.Errorf("json-delete cannot delete the document root")
		}
		if operation.Type == "json-set" && !operation.valueSet && operation.Value == nil {
			return fmt.Errorf("value is required for json-set")
		}
		return nil
	case "json-move":
		if err := require("path", operation.Path); err != nil {
			return err
		}
		if err := require("from", operation.From); err != nil {
			return err
		}
		if err := require("to", operation.To); err != nil {
			return err
		}
		if err := validatePaths(operation.Path); err != nil {
			return err
		}
		if err := configdoc.ValidateJSONPath(operation.From); err != nil {
			return fmt.Errorf("invalid from path for json-move: %w", err)
		}
		if err := configdoc.ValidateJSONPath(operation.To); err != nil {
			return fmt.Errorf("invalid to path for json-move: %w", err)
		}
		if operation.From == "$" || operation.To == "$" {
			return fmt.Errorf("json-move cannot move from or to the document root")
		}
		return nil
	case "ini-set":
		if err := require("path", operation.Path); err != nil {
			return err
		}
		if err := require("section", operation.Section); err != nil {
			return err
		}
		if err := require("key", operation.Key); err != nil {
			return err
		}
		if err := validatePaths(operation.Path); err != nil {
			return err
		}
		value, ok := operation.Value.(string)
		if !ok {
			return fmt.Errorf("value must be a string for ini-set")
		}
		if err := configdoc.ValidateINIValue(value); err != nil {
			return fmt.Errorf("invalid value for ini-set: %w", err)
		}
		return nil
	case "ini-delete":
		if err := require("path", operation.Path); err != nil {
			return err
		}
		if err := require("section", operation.Section); err != nil {
			return err
		}
		if err := require("key", operation.Key); err != nil {
			return err
		}
		return validatePaths(operation.Path)
	case "ini-move":
		if err := require("path", operation.Path); err != nil {
			return err
		}
		for name, value := range map[string]string{
			"fromSection": operation.FromSection,
			"fromKey":     operation.FromKey,
			"toSection":   operation.ToSection,
			"toKey":       operation.ToKey,
		} {
			if err := require(name, value); err != nil {
				return err
			}
		}
		return validatePaths(operation.Path)
	}
	return nil
}

func isAllowedMigrationOperation(operationType string) bool {
	switch operationType {
	case "file-copy", "file-move", "file-delete",
		"json-set", "json-delete", "json-move",
		"ini-set", "ini-delete", "ini-move":
		return true
	default:
		return false
	}
}

func validateValidation(validation ValidationDef) error {
	if err := validateStagingPath(validation.Path); err != nil {
		return err
	}
	switch validation.Type {
	case "file-exists", "json-parse", "ini-parse":
	case "json-path-exists":
		if validation.JSONPath == "" {
			return fmt.Errorf("jsonPath is required for json-path-exists")
		}
		if err := configdoc.ValidateJSONPath(validation.JSONPath); err != nil {
			return fmt.Errorf("invalid jsonPath for json-path-exists: %w", err)
		}
	case "ini-key-exists":
		if validation.Section == "" || validation.Key == "" {
			return fmt.Errorf("section and key are required for ini-key-exists")
		}
		if err := configdoc.ValidateINIAddress(validation.Section, validation.Key); err != nil {
			return fmt.Errorf("invalid INI address for ini-key-exists: %w", err)
		}
	default:
		return fmt.Errorf("unsupported validation %q", validation.Type)
	}
	return nil
}

func validationDiagnosticCode(validationType string) string {
	switch validationType {
	case "file-exists", "json-parse", "json-path-exists", "ini-parse", "ini-key-exists":
		return DiagnosticInvalidValidation
	default:
		return DiagnosticUnknownValidation
	}
}

func validateStableID(mod *Module, filePath, kind, id string) error {
	if !stableIDPattern.MatchString(id) {
		return validationError(mod, filePath, DiagnosticInvalidID, "%s id %q is not stable lowercase identifier syntax", kind, id)
	}
	return nil
}

func validateVersionExtractionPattern(pattern string) error {
	if err := validateAnchoredPattern(pattern); err != nil {
		return err
	}
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return err
	}
	versionCaptures := 0
	for _, name := range compiled.SubexpNames() {
		if name == "version" {
			versionCaptures++
		}
	}
	if versionCaptures != 1 {
		return fmt.Errorf("versionPattern must contain exactly one named capture (?P<version>...)")
	}
	return nil
}

type pathValidationError struct {
	code   string
	detail string
}

func (e *pathValidationError) Error() string { return e.detail }

func DiagnosticCodeForPathError(err error) string {
	var pathError *pathValidationError
	if errors.As(err, &pathError) {
		return pathError.code
	}
	return ""
}

func validateTemplatePlaceholders(value string) error {
	allowed := map[string]struct{}{
		"instance.root":    {},
		"instance.version": {},
		"instance.id":      {},
	}
	matches := placeholderPattern.FindAllStringSubmatch(value, -1)
	for _, match := range matches {
		if _, ok := allowed[match[1]]; !ok {
			return &pathValidationError{code: DiagnosticInvalidPlaceholder, detail: fmt.Sprintf("placeholder %q is not allowed", match[0])}
		}
	}
	withoutMatches := placeholderPattern.ReplaceAllString(value, "")
	if strings.Contains(withoutMatches, "${") {
		return &pathValidationError{code: DiagnosticInvalidPlaceholder, detail: "placeholder syntax is malformed"}
	}
	return nil
}

func validateHostTemplatePath(path string) error {
	if strings.TrimSpace(path) == "" {
		return &pathValidationError{code: DiagnosticUnsafePath, detail: "path is empty"}
	}
	if err := validateTemplatePlaceholders(path); err != nil {
		return err
	}
	if hasTraversal(path) {
		return &pathValidationError{code: DiagnosticUnsafePath, detail: fmt.Sprintf("path %q contains parent traversal", path)}
	}
	if isHostAbsolute(path) {
		return &pathValidationError{code: DiagnosticUnsafePath, detail: fmt.Sprintf("path %q is host-absolute; use an environment root or instance.root", path)}
	}
	return nil
}

func validatePortablePath(path string) error {
	if err := validateHostTemplatePath(path); err != nil {
		return err
	}
	if strings.Contains(path, "${instance.root}") || containsHostExpansion(path) {
		return &pathValidationError{code: DiagnosticUnsafePath, detail: fmt.Sprintf("portable path %q can expand to a host path", path)}
	}
	return nil
}

func validateStagingPath(path string) error {
	if strings.TrimSpace(path) == "" {
		return &pathValidationError{code: DiagnosticUnsafePath, detail: "staging path is empty"}
	}
	if err := validateTemplatePlaceholders(path); err != nil {
		return err
	}
	if len(placeholderPattern.FindAllString(path, -1)) > 0 || containsPortableHostExpansion(path) || isPortableAbsoluteOrVolume(path) || hasTraversal(path) {
		return &pathValidationError{code: DiagnosticUnsafePath, detail: fmt.Sprintf("staging path %q must be relative and contained", path)}
	}
	return nil
}

func containsHostExpansion(path string) bool {
	trimmed := strings.TrimSpace(path)
	return strings.HasPrefix(trimmed, "~") || strings.Contains(trimmed, "%") || strings.Contains(trimmed, "${") || strings.HasPrefix(trimmed, "$")
}

func isHostAbsolute(path string) bool {
	trimmed := strings.TrimSpace(path)
	return filepath.IsAbs(trimmed) || windowsAbsolutePath.MatchString(trimmed) || strings.HasPrefix(trimmed, `\\`) || strings.HasPrefix(trimmed, "/") || strings.HasPrefix(trimmed, `\`)
}

func hasTraversal(path string) bool {
	normalized := strings.ReplaceAll(path, `\`, "/")
	for _, part := range strings.Split(normalized, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func hasMigrationCycle(generations map[string]*GenerationDef, adjacency map[string][]string) bool {
	const (
		unseen = iota
		visiting
		visited
	)
	state := make(map[string]int, len(generations))
	var visit func(string) bool
	visit = func(current string) bool {
		if state[current] == visiting {
			return true
		}
		if state[current] == visited {
			return false
		}
		state[current] = visiting
		for _, next := range adjacency[current] {
			if visit(next) {
				return true
			}
		}
		state[current] = visited
		return false
	}
	for id := range generations {
		if state[id] == unseen && visit(id) {
			return true
		}
	}
	return false
}

func findAmbiguousRoute(generations map[string]*GenerationDef, adjacency map[string][]string) (string, string, bool) {
	ids := make([]string, 0, len(generations))
	for id := range generations {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, from := range ids {
		for _, to := range ids {
			if from == to {
				continue
			}
			if countMigrationPaths(from, to, adjacency, 2) > 1 {
				return from, to, true
			}
		}
	}
	return "", "", false
}

func countMigrationPaths(current, target string, adjacency map[string][]string, limit int) int {
	if current == target {
		return 1
	}
	count := 0
	for _, next := range adjacency[current] {
		count += countMigrationPaths(next, target, adjacency, limit-count)
		if count >= limit {
			return count
		}
	}
	return count
}
