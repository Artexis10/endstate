//go:build !windows

// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package catalogplan

func isReparsePoint(string) bool { return false }
