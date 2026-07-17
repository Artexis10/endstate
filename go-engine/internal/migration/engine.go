// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"errors"
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// Engine applies declarative migration operations sequentially within one
// disposable staging root. It stops at the first error. Earlier operations are
// not rolled back because the staging owner discards the isolated root when an
// edge fails.
type Engine struct {
	stageCheckpoint stageCheckpointFunc
}

func NewEngine() *Engine { return &Engine{} }

func (e *Engine) Apply(root string, operations []modules.MigrationOperationDef) error {
	if err := safepath.ValidateRoot(root); err != nil {
		mapped := mapPathError(err)
		var typed *Error
		if errors.As(mapped, &typed) {
			return operationError(typed.Code, "", -1, typed.Err)
		}
		return operationError(CodeUnsafeRoot, "", -1, err)
	}
	for index, operation := range operations {
		if !isSupportedOperationType(operation.Type) {
			return operationError(
				CodeUnsupportedOperation,
				operation.Type,
				index,
				fmt.Errorf("operation type is not engine-owned"),
			)
		}
		var err error
		switch operation.Type {
		case "file-copy":
			err = applyFileCopy(root, operation)
		case "file-move":
			err = applyFileMove(root, operation)
		case "file-delete":
			err = applyFileDelete(root, operation)
		case "json-set", "json-delete", "json-move":
			err = applyJSONOperation(root, operation)
		case "ini-set", "ini-delete", "ini-move":
			err = applyINIOperation(root, operation)
		default:
			return operationError(
				CodeUnsupportedOperation,
				operation.Type,
				index,
				fmt.Errorf("operation handler is unavailable"),
			)
		}
		if err != nil {
			var typed *Error
			if errors.As(err, &typed) {
				return operationError(typed.Code, operation.Type, index, typed.Err)
			}
			return operationError(CodeIO, operation.Type, index, err)
		}
	}
	return nil
}
