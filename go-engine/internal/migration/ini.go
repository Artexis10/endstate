// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"fmt"
	"os"

	"github.com/Artexis10/endstate/go-engine/internal/configdoc"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

func applyINIOperation(root string, operation modules.MigrationOperationDef) error {
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
		return localError(CodeUnsupportedFileType, fmt.Errorf("INI target is not a regular file"))
	}
	data, mode, err := safepath.ReadRegularFile(target)
	if err != nil {
		return mapPathError(err)
	}
	document, err := configdoc.ParseINI(data)
	if err != nil {
		return mapDocumentError(err)
	}
	switch operation.Type {
	case "ini-set":
		value, ok := operation.Value.(string)
		if !ok {
			return localError(CodeInvalidINIValue, fmt.Errorf("ini-set value must be a string"))
		}
		document, err = configdoc.INISet(document, operation.Section, operation.Key, value)
	case "ini-delete":
		document, err = configdoc.INIDelete(document, operation.Section, operation.Key)
	case "ini-move":
		document, err = configdoc.INIMove(
			document,
			operation.FromSection,
			operation.FromKey,
			operation.ToSection,
			operation.ToKey,
		)
	}
	if err != nil {
		return mapDocumentError(err)
	}
	if err := safepath.AtomicWriteFile(target, configdoc.EncodeINI(document), mode); err != nil {
		return localError(CodeIO, err)
	}
	return nil
}
