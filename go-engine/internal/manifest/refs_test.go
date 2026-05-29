// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import "testing"

func TestResolveRef_PrefersOSKey(t *testing.T) {
	app := App{Refs: map[string]string{"windows": "Win.App", "linux": "lin-app"}}
	if got := ResolveRef(app, "linux"); got != "lin-app" {
		t.Errorf("ResolveRef(linux) = %q, want lin-app", got)
	}
	if got := ResolveRef(app, "windows"); got != "Win.App" {
		t.Errorf("ResolveRef(windows) = %q, want Win.App", got)
	}
	if got := ResolveRef(app, "darwin"); got == "" {
		t.Errorf("ResolveRef(darwin) = %q, want a fallback ref", got)
	}
}

func TestResolveRef_FallsBackToFirstNonEmpty(t *testing.T) {
	app := App{Refs: map[string]string{"windows": "Win.App"}}
	if got := ResolveRef(app, "linux"); got != "Win.App" {
		t.Errorf("ResolveRef(linux) fallback = %q, want Win.App", got)
	}
}

func TestResolveRef_EmptyReturnsEmpty(t *testing.T) {
	if got := ResolveRef(App{}, "linux"); got != "" {
		t.Errorf("ResolveRef(no refs) = %q, want empty", got)
	}
	if got := ResolveRef(App{Refs: map[string]string{"windows": ""}}, "windows"); got != "" {
		t.Errorf("ResolveRef(blank ref) = %q, want empty", got)
	}
}
