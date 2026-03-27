// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import "github.com/Artexis10/endstate/go-engine/internal/manifest"

// resolveItemDisplayName returns the best available human-readable name for an
// app. Resolution order: (1) resolved display name from winget detection,
// (2) manifest displayName, (3) winget ref, (4) manifest id.
// The result is never empty.
func resolveItemDisplayName(resolved string, app manifest.App, ref string) string {
	if resolved != "" {
		return resolved
	}
	if app.DisplayName != "" {
		return app.DisplayName
	}
	if ref != "" {
		return ref
	}
	return app.ID
}
