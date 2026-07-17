// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package restore

func describeRegistryTargetExists(RestoreAction) bool { return false }
