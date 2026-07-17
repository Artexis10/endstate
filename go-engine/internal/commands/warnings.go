// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

// CommandWarning is a non-fatal, machine-readable command outcome. Driver and
// Ref are omitted when a warning does not belong to a specific package lane.
type CommandWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Driver  string `json:"driver,omitempty"`
	Ref     string `json:"ref,omitempty"`
}
