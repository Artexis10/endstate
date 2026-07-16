// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configvalidate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/configdoc"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// ResolvedValidation binds one declarative validation to the concrete host
// path selected for that generation target.
type ResolvedValidation struct {
	Definition modules.ValidationDef
	HostPath   string
}

// ValidateStaging applies definitions beneath one isolated staging root in
// declaration order and stops at the first failure.
func ValidateStaging(root string, definitions []modules.ValidationDef) error {
	if err := safepath.ValidateRoot(root); err != nil {
		code, mapped := mapPathError(err)
		return &Error{Code: code, Index: -1, Err: mapped}
	}
	for index, definition := range definitions {
		if err := validateDefinition(definition); err != nil {
			return wrapError(index, definition, "", err)
		}
		target, err := safepath.Resolve(root, definition.Path)
		if err != nil {
			code, mapped := mapPathError(err)
			return wrapError(index, definition, "", fail(code, mapped))
		}
		if err := validateOne(target, definition); err != nil {
			return wrapError(index, definition, target, err)
		}
	}
	return nil
}

// ValidateResolved applies validations to independently resolved host targets.
// HostPath, rather than Definition.Path, selects the file to inspect.
func ValidateResolved(validations []ResolvedValidation) error {
	for index, validation := range validations {
		if err := validateDefinition(validation.Definition); err != nil {
			return wrapError(index, validation.Definition, validation.HostPath, err)
		}
		target, err := resolveHostPath(validation.HostPath)
		if err != nil {
			return wrapError(index, validation.Definition, validation.HostPath, err)
		}
		if err := validateOne(target, validation.Definition); err != nil {
			return wrapError(index, validation.Definition, target, err)
		}
	}
	return nil
}

func validateOne(target string, definition modules.ValidationDef) error {
	switch definition.Type {
	case "file-exists":
		_, err := readRegularFile(target)
		return err
	case "json-parse":
		data, err := readRegularFile(target)
		if err != nil {
			return err
		}
		_, err = configdoc.ParseJSON(data)
		return mapDocumentError(err)
	case "json-path-exists":
		data, err := readRegularFile(target)
		if err != nil {
			return err
		}
		document, err := configdoc.ParseJSON(data)
		if err != nil {
			return mapDocumentError(err)
		}
		exists, err := configdoc.JSONPathExists(document, definition.JSONPath)
		if err != nil {
			return fail(CodeInvalidJSONPath, err)
		}
		if !exists {
			return fail(CodeJSONPathMissing, fmt.Errorf("JSON path %q does not exist", definition.JSONPath))
		}
		return nil
	case "ini-parse":
		data, err := readRegularFile(target)
		if err != nil {
			return err
		}
		_, err = configdoc.ParseINI(data)
		return mapDocumentError(err)
	case "ini-key-exists":
		data, err := readRegularFile(target)
		if err != nil {
			return err
		}
		document, err := configdoc.ParseINI(data)
		if err != nil {
			return mapDocumentError(err)
		}
		exists, err := configdoc.INIKeyExists(document, definition.Section, definition.Key)
		if err != nil {
			return fail(CodeInvalidINIAddress, err)
		}
		if !exists {
			return fail(
				CodeINIKeyMissing,
				fmt.Errorf("INI key %q in section %q does not exist", definition.Key, definition.Section),
			)
		}
		return nil
	default:
		return fail(CodeUnsupportedValidation, fmt.Errorf("unsupported validation %q", definition.Type))
	}
}

func validateDefinition(definition modules.ValidationDef) error {
	switch definition.Type {
	case "file-exists", "json-parse", "ini-parse":
		return nil
	case "json-path-exists":
		if err := configdoc.ValidateJSONPath(definition.JSONPath); err != nil {
			return fail(CodeInvalidJSONPath, err)
		}
		return nil
	case "ini-key-exists":
		if err := configdoc.ValidateINIAddress(definition.Section, definition.Key); err != nil {
			return fail(CodeInvalidINIAddress, err)
		}
		return nil
	default:
		return fail(CodeUnsupportedValidation, fmt.Errorf("unsupported validation %q", definition.Type))
	}
}

func readRegularFile(target string) ([]byte, error) {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return nil, fail(CodePathNotFound, err)
	}
	if err != nil {
		return nil, fail(CodeIO, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fail(CodeUnsupportedFileType, fmt.Errorf("validation target is not a regular file"))
	}
	data, _, err := safepath.ReadRegularFile(target)
	if err != nil {
		code, mapped := mapPathError(err)
		return nil, fail(code, mapped)
	}
	return data, nil
}

func resolveHostPath(hostPath string) (string, error) {
	if hostPath == "" || !filepath.IsAbs(hostPath) || filepath.Clean(hostPath) != hostPath ||
		strings.HasPrefix(hostPath, `\\`) || strings.HasPrefix(hostPath, "//") {
		return "", fail(CodeUnsafePath, fmt.Errorf("resolved host path must be a clean local absolute path"))
	}
	parent := filepath.Dir(hostPath)
	name := filepath.Base(hostPath)
	target, err := safepath.Resolve(parent, name)
	if err != nil {
		code, mapped := mapPathError(err)
		return "", fail(code, mapped)
	}
	if target != hostPath {
		return "", fail(CodeUnsafePath, fmt.Errorf("resolved host path must not require portable-path normalization"))
	}
	return target, nil
}

func wrapError(index int, definition modules.ValidationDef, hostPath string, err error) error {
	var local *localError
	if !errors.As(err, &local) {
		local = &localError{code: CodeIO, err: err}
	}
	return &Error{
		Code:       local.code,
		Index:      index,
		Validation: definition,
		HostPath:   hostPath,
		Err:        local.err,
	}
}

func mapPathError(err error) (Code, error) {
	if os.IsNotExist(err) {
		return CodePathNotFound, err
	}
	var pathError *safepath.Error
	if errors.As(err, &pathError) {
		switch pathError.Code {
		case safepath.CodeUnsafeRoot:
			return CodeUnsafeRoot, err
		case safepath.CodeUnsafePath:
			return CodeUnsafePath, err
		case safepath.CodeLinkUnsupported:
			return CodeLinkUnsupported, err
		case safepath.CodeSourceChanged:
			return CodeSourceChanged, err
		}
	}
	return CodeIO, err
}

func mapDocumentError(err error) error {
	if err == nil {
		return nil
	}
	switch configdoc.CodeOf(err) {
	case configdoc.CodeMalformedJSON:
		return fail(CodeMalformedJSON, err)
	case configdoc.CodeInvalidJSONPath:
		return fail(CodeInvalidJSONPath, err)
	case configdoc.CodeMalformedINI:
		return fail(CodeMalformedINI, err)
	default:
		return fail(CodeIO, err)
	}
}
