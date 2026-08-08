// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// RevisionSyncResult reports the deterministic outcome of a revision sync.
type RevisionSyncResult struct {
	Stale   int `json:"stale"`
	Updated int `json:"updated"`
}

type revisionSyncPlan struct {
	moduleID string
	path     string
	before   []byte
	mode     os.FileMode
	start    int
	revision string
}

type revisionSyncOperations struct {
	resolve func(string, string) (string, error)
	read    func(string) ([]byte, os.FileMode, error)
	write   func(string, []byte, os.FileMode) error
	reload  func(string, time.Time) (*Catalog, error)
}

var defaultRevisionSyncOperations = revisionSyncOperations{
	resolve: safepath.Resolve,
	read:    safepath.ReadRegularFile,
	write:   safepath.AtomicWriteFile,
	reload:  LoadCatalog,
}

// SyncRevisions checks production module revision pins and, when write is
// explicit, replaces only stale moduleRevision value bytes after full preflight.
func SyncRevisions(repoRoot string, write bool, now time.Time) (RevisionSyncResult, error) {
	plans, err := preflightRevisionSync(repoRoot, now)
	if err != nil {
		return RevisionSyncResult{}, err
	}
	result := RevisionSyncResult{Stale: len(plans)}
	if !write {
		if len(plans) != 0 {
			return result, validationError(CodeStaleSidecar, plans[0].moduleID, plans[0].path, "moduleRevision does not match current production revision")
		}
		return result, nil
	}

	return applyRevisionSyncPlans(repoRoot, plans, now, defaultRevisionSyncOperations)
}

func applyRevisionSyncPlans(repoRoot string, plans []revisionSyncPlan, now time.Time, operations revisionSyncOperations) (RevisionSyncResult, error) {
	result := RevisionSyncResult{Stale: len(plans)}
	for _, plan := range plans {
		portable, err := filepath.Rel(repoRoot, plan.path)
		if err != nil {
			return result, validationErrorWithCause(CodeInvalidSidecar, plan.moduleID, plan.path, "resolve validation sidecar", err)
		}
		path, err := operations.resolve(repoRoot, filepath.ToSlash(portable))
		if err != nil {
			return result, validationErrorWithCause(CodeInvalidSidecar, plan.moduleID, plan.path, "resolve validation sidecar", err)
		}
		data, mode, err := operations.read(path)
		if err != nil {
			return result, validationErrorWithCause(CodeInvalidSidecar, plan.moduleID, path, "read validation sidecar", err)
		}
		if !bytes.Equal(data, plan.before) {
			return result, validationError(CodeInvalidSidecar, plan.moduleID, path, "validation sidecar changed during synchronization")
		}
		copy(data[plan.start:plan.start+64], plan.revision)
		if err := operations.write(path, data, mode); err != nil {
			return result, validationErrorWithCause(CodeInvalidSidecar, plan.moduleID, path, "write validation sidecar", err)
		}
		result.Updated++
	}
	if _, err := operations.reload(repoRoot, now); err != nil {
		return result, err
	}
	return result, nil
}

func preflightRevisionSync(repoRoot string, now time.Time) ([]revisionSyncPlan, error) {
	appsRoot, err := safepath.Resolve(repoRoot, "modules/apps")
	if err != nil {
		return nil, validationErrorWithCause(CodeInvalidModuleCatalog, "", filepath.Join(repoRoot, "modules", "apps"), "resolve production module catalog", err)
	}
	info, err := os.Lstat(appsRoot)
	if err != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() {
		return nil, validationErrorWithCause(CodeInvalidModuleCatalog, "", appsRoot, "read production module catalog", err)
	}
	entries, err := os.ReadDir(appsRoot)
	if err != nil {
		return nil, validationErrorWithCause(CodeInvalidModuleCatalog, "", appsRoot, "scan production module catalog", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	plans := make([]revisionSyncPlan, 0)
	seen := make(map[string]string)
	for _, entry := range entries {
		moduleDir := filepath.Join(appsRoot, entry.Name())
		entryInfo, err := os.Lstat(moduleDir)
		if err != nil || safepath.IsLinkOrReparse(entryInfo) {
			return nil, validationErrorWithCause(CodeInvalidModuleCatalog, "", moduleDir, "read production module directory", err)
		}
		if !entryInfo.IsDir() {
			continue
		}
		modulePath, err := safepath.Resolve(repoRoot, filepath.ToSlash(filepath.Join("modules", "apps", entry.Name(), "module.jsonc")))
		if err != nil {
			return nil, validationErrorWithCause(CodeInvalidModuleCatalog, "", moduleDir, "resolve production module", err)
		}
		moduleData, _, err := safepath.ReadRegularFile(modulePath)
		if err != nil {
			return nil, validationErrorWithCause(CodeInvalidModuleCatalog, "", modulePath, "read production module", err)
		}
		mod, err := modules.ParseAndValidateModuleJSON(moduleData, modulePath)
		if err != nil {
			return nil, validationErrorWithCause(CodeInvalidModuleCatalog, "", modulePath, "parse production module", err)
		}
		revision, err := modules.ComputeModuleRevision(moduleData)
		if err != nil {
			return nil, validationErrorWithCause(CodeInvalidModuleCatalog, mod.ID, modulePath, "compute production module revision", err)
		}
		mod.Revision = revision
		mod.FilePath = modulePath

		sidecarPath, err := safepath.Resolve(repoRoot, filepath.ToSlash(filepath.Join("modules", "apps", entry.Name(), "validation.jsonc")))
		if err != nil {
			return nil, validationErrorWithCause(CodeInvalidSidecar, mod.ID, filepath.Join(moduleDir, "validation.jsonc"), "resolve validation sidecar", err)
		}
		sidecarData, mode, err := safepath.ReadRegularFile(sidecarPath)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return nil, validationErrorWithCause(CodeInvalidSidecar, mod.ID, sidecarPath, "read validation sidecar", err)
			}
			return nil, validationErrorWithCause(CodeMissingSidecar, mod.ID, sidecarPath, "read validation sidecar", err)
		}
		record, err := parseValidationJSONC(sidecarData)
		if err != nil {
			return nil, validationErrorWithCause(CodeInvalidSidecar, mod.ID, sidecarPath, "parse validation sidecar", err)
		}
		record.FilePath = sidecarPath
		if prior, ok := seen[record.ModuleID]; ok {
			return nil, validationError(CodeDuplicateSidecar, record.ModuleID, sidecarPath, "validation identity is already declared by %s", prior)
		}
		seen[record.ModuleID] = sidecarPath
		if record.ModuleID != mod.ID {
			return nil, validationError(CodeMismatchedSidecar, record.ModuleID, sidecarPath, "sibling module identity is %q", mod.ID)
		}
		start, rawRevision, err := topLevelRevisionToken(sidecarData)
		if err != nil || rawRevision != record.ModuleRevision || !lowerSHA256Pattern.MatchString(rawRevision) {
			return nil, validationErrorWithCause(CodeInvalidSidecar, record.ModuleID, sidecarPath, "locate safe moduleRevision token", err)
		}
		checked := record
		checked.ModuleRevision = revision
		resolveDefaults(&checked, mod)
		if err := validateRecord(&checked, mod, now); err != nil {
			return nil, err
		}
		if rawRevision != revision {
			plans = append(plans, revisionSyncPlan{moduleID: mod.ID, path: sidecarPath, before: sidecarData, mode: mode, start: start, revision: revision})
		}
	}
	return plans, nil
}

func topLevelRevisionToken(data []byte) (int, string, error) {
	count := 0
	start := 0
	value := ""
	depth := 0
	for i := 0; i < len(data); {
		i = skipJSONCSpaceAndComments(data, i)
		if i >= len(data) {
			break
		}
		switch data[i] {
		case '{', '[':
			depth++
			i++
		case '}', ']':
			depth--
			i++
		case '"':
			key, next, err := jsonStringToken(data, i)
			if err != nil {
				return 0, "", err
			}
			afterKey := skipJSONCSpaceAndComments(data, next)
			if depth == 1 && key == "moduleRevision" && afterKey < len(data) && data[afterKey] == ':' {
				valueStart := skipJSONCSpaceAndComments(data, afterKey+1)
				if valueStart >= len(data) || data[valueStart] != '"' {
					return 0, "", fmt.Errorf("moduleRevision must be a JSON string")
				}
				raw, valueEnd, err := jsonStringToken(data, valueStart)
				if err != nil {
					return 0, "", err
				}
				if valueEnd-valueStart != 66 {
					return 0, "", fmt.Errorf("moduleRevision must be an unescaped 64-byte value")
				}
				count++
				start, value = valueStart+1, raw
				i = valueEnd
				continue
			}
			i = next
		default:
			i++
		}
	}
	if count != 1 {
		return 0, "", fmt.Errorf("expected exactly one top-level moduleRevision token")
	}
	return start, value, nil
}

func skipJSONCSpaceAndComments(data []byte, i int) int {
	for i < len(data) {
		switch data[i] {
		case ' ', '\t', '\r', '\n':
			i++
		case '/':
			if i+1 >= len(data) {
				return i
			}
			switch data[i+1] {
			case '/':
				i += 2
				for i < len(data) && data[i] != '\n' {
					i++
				}
			case '*':
				i += 2
				for i+1 < len(data) && (data[i] != '*' || data[i+1] != '/') {
					i++
				}
				if i+1 >= len(data) {
					return len(data)
				}
				i += 2
			default:
				return i
			}
		default:
			return i
		}
	}
	return i
}

func jsonStringToken(data []byte, start int) (string, int, error) {
	for end := start + 1; end < len(data); end++ {
		if data[end] == '\\' {
			end++
			continue
		}
		if data[end] == '"' {
			value, err := strconv.Unquote(string(data[start : end+1]))
			return value, end + 1, err
		}
	}
	return "", 0, fmt.Errorf("unterminated JSON string")
}
