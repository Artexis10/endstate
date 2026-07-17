// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"errors"
	"fmt"
	"os"

	"github.com/Artexis10/endstate/go-engine/internal/configdoc"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

func applyJSONOperation(root string, operation modules.MigrationOperationDef) error {
	target, err := resolveOperationPath(root, operation.Path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return localError(CodePathNotFound, err)
	}
	if err != nil {
		return localError(CodeIO, err)
	}
	if !info.Mode().IsRegular() {
		return localError(CodeUnsupportedFileType, fmt.Errorf("JSON target is not a regular file"))
	}
	data, mode, err := safepath.ReadRegularFile(target)
	if err != nil {
		return mapPathError(err)
	}
	document, err := configdoc.ParseJSON(data)
	if err != nil {
		return mapDocumentError(err)
	}
	switch operation.Type {
	case "json-set":
		document, err = configdoc.JSONSet(document, operation.JSONPath, operation.Value)
	case "json-delete":
		document, err = configdoc.JSONDelete(document, operation.JSONPath)
	case "json-move":
		document, err = configdoc.JSONMove(document, operation.From, operation.To)
	}
	if err != nil {
		return mapDocumentError(err)
	}
	encoded, err := configdoc.EncodeJSON(document)
	if err != nil {
		return mapDocumentError(err)
	}
	if err := safepath.AtomicWriteFile(target, encoded, mode); err != nil {
		return localError(CodeIO, err)
	}
	return nil
}

func mapDocumentError(err error) error {
	var local *Error
	if errors.As(err, &local) {
		return local
	}
	switch configdoc.CodeOf(err) {
	case configdoc.CodeMalformedJSON:
		return localError(CodeMalformedJSON, err)
	case configdoc.CodeInvalidJSONPath:
		return localError(CodeInvalidJSONPath, err)
	case configdoc.CodeInvalidJSONValue:
		return localError(CodeInvalidJSONValue, err)
	case configdoc.CodeJSONParentMissing:
		return localError(CodeJSONParentMissing, err)
	case configdoc.CodeJSONSourceMissing:
		return localError(CodeJSONSourceMissing, err)
	case configdoc.CodeJSONDestinationExists:
		return localError(CodeJSONDestinationExists, err)
	case configdoc.CodeMalformedINI:
		return localError(CodeMalformedINI, err)
	case configdoc.CodeInvalidINIValue:
		return localError(CodeInvalidINIValue, err)
	case configdoc.CodeINISourceMissing:
		return localError(CodeINISourceMissing, err)
	case configdoc.CodeINIDestinationExists:
		return localError(CodeINIDestinationExists, err)
	default:
		return localError(CodeIO, err)
	}
}
