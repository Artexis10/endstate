// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

type ValidationError struct {
	Code     string
	ModuleID string
	FilePath string
	Detail   string
	Cause    error
}

func (e *ValidationError) Error() string {
	location := e.ModuleID
	if location == "" {
		location = e.FilePath
	}
	if location == "" {
		location = "validation matrix"
	}
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", location, e.Detail, e.Cause)
	}
	return fmt.Sprintf("%s: %s", location, e.Detail)
}

func (e *ValidationError) Unwrap() error { return e.Cause }

func ErrorCode(err error) string {
	var validationError *ValidationError
	if errors.As(err, &validationError) {
		return validationError.Code
	}
	return ""
}

func validationError(code, moduleID, filePath, format string, args ...any) error {
	return &ValidationError{
		Code: code, ModuleID: moduleID, FilePath: filePath,
		Detail: fmt.Sprintf(format, args...),
	}
}

func validationErrorWithCause(code, moduleID, filePath, detail string, cause error) error {
	return &ValidationError{Code: code, ModuleID: moduleID, FilePath: filePath, Detail: detail, Cause: cause}
}

func LoadCatalog(repoRoot string, now time.Time) (*Catalog, error) {
	productionModules, diagnostics, err := modules.GetCatalogWithDiagnostics(repoRoot)
	if err != nil {
		return nil, validationErrorWithCause(CodeInvalidModuleCatalog, "", "", "load production module catalog", err)
	}
	if len(diagnostics) > 0 {
		first := diagnostics[0]
		return nil, validationError(CodeInvalidModuleCatalog, first.ModuleID, first.FilePath, "%s", first.Message)
	}

	moduleIDs := make([]string, 0, len(productionModules))
	for moduleID := range productionModules {
		moduleIDs = append(moduleIDs, moduleID)
	}
	sort.Slice(moduleIDs, func(left, right int) bool {
		leftPath := productionModules[moduleIDs[left]].FilePath
		rightPath := productionModules[moduleIDs[right]].FilePath
		return leftPath < rightPath
	})

	records := make(map[string]ValidationRecord, len(productionModules))
	seenIdentities := make(map[string]string, len(productionModules))
	moduleDirectories := make(map[string]struct{}, len(productionModules))
	for _, moduleID := range moduleIDs {
		mod := productionModules[moduleID]
		moduleDir := filepath.Dir(mod.FilePath)
		moduleDirectories[filepath.Clean(moduleDir)] = struct{}{}
		sidecarPath := filepath.Join(moduleDir, "validation.jsonc")
		data, readErr := os.ReadFile(sidecarPath)
		if errors.Is(readErr, os.ErrNotExist) {
			return nil, validationError(CodeMissingSidecar, moduleID, sidecarPath, "missing sibling validation.jsonc")
		}
		if readErr != nil {
			return nil, validationErrorWithCause(CodeInvalidSidecar, moduleID, sidecarPath, "read validation sidecar", readErr)
		}

		record, parseErr := parseValidationJSONC(data)
		if parseErr != nil {
			return nil, validationErrorWithCause(CodeInvalidSidecar, moduleID, sidecarPath, "parse validation sidecar", parseErr)
		}
		record.FilePath = sidecarPath
		record.sourceSnapshot = append([]byte(nil), data...)
		if previousPath, exists := seenIdentities[record.ModuleID]; exists {
			return nil, validationError(CodeDuplicateSidecar, record.ModuleID, sidecarPath, "validation identity is already declared by %s", previousPath)
		}
		seenIdentities[record.ModuleID] = sidecarPath
		if record.ModuleID != moduleID {
			return nil, validationError(CodeMismatchedSidecar, record.ModuleID, sidecarPath, "sibling module identity is %q", moduleID)
		}
		if err := validateRecord(&record, mod, now); err != nil {
			return nil, err
		}
		records[moduleID] = record
	}

	appsRoot := filepath.Join(repoRoot, "modules", "apps")
	entries, readErr := os.ReadDir(appsRoot)
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return nil, validationErrorWithCause(CodeInvalidSidecar, "", appsRoot, "scan validation sidecars", readErr)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(appsRoot, entry.Name())
		if _, known := moduleDirectories[filepath.Clean(dir)]; known {
			continue
		}
		path := filepath.Join(dir, "validation.jsonc")
		if _, statErr := os.Stat(path); statErr == nil {
			return nil, validationError(CodeMismatchedSidecar, "", path, "validation sidecar has no discovered production module")
		}
	}

	return &Catalog{Modules: productionModules, Records: records}, nil
}

func parseValidationJSONC(data []byte) (ValidationRecord, error) {
	decoder := json.NewDecoder(bytes.NewReader(manifest.StripJsoncComments(data)))
	decoder.DisallowUnknownFields()
	var record ValidationRecord
	if err := decoder.Decode(&record); err != nil {
		return ValidationRecord{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return ValidationRecord{}, fmt.Errorf("multiple JSON values")
		}
		return ValidationRecord{}, err
	}
	return record, nil
}
