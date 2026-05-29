// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

// ResolveRef returns the package reference for an app on the given operating
// system. It prefers app.Refs[goos]; if that key is absent or blank it falls
// back to the first non-empty ref in map iteration order. Returns "" when no
// non-empty ref exists.
//
// This is the single, platform-aware ref resolver shared by the planner and the
// command handlers. On Windows with a refs["windows"] entry it is identical to
// the historical Windows-only behavior.
func ResolveRef(app App, goos string) string {
	if ref, ok := app.Refs[goos]; ok && ref != "" {
		return ref
	}
	for _, ref := range app.Refs {
		if ref != "" {
			return ref
		}
	}
	return ""
}
