// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package migration

var supportedOperationTypes = [...]string{
	"file-copy",
	"file-delete",
	"file-move",
	"ini-delete",
	"ini-move",
	"ini-set",
	"json-delete",
	"json-move",
	"json-set",
}

func SupportedOperationTypes() []string {
	return append([]string(nil), supportedOperationTypes[:]...)
}

func isSupportedOperationType(operationType string) bool {
	switch operationType {
	case "file-copy", "file-move", "file-delete",
		"json-set", "json-delete", "json-move",
		"ini-set", "ini-delete", "ini-move":
		return true
	default:
		return false
	}
}
