// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package restore

import "github.com/Artexis10/endstate/go-engine/internal/validationmode"

func describeRegistryTargetExists(RestoreAction) bool { return false }

func describeValidationRegistryImportTargetExists(*validationmode.Context, RestoreAction) bool {
	return false
}
