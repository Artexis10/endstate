// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"slices"
	"testing"
)

func TestWaveLinkCaptureExcludesSensitiveCaches(t *testing.T) {
	catalog, err := LoadCatalog(productionModulesRoot())
	if err != nil {
		t.Fatal(err)
	}
	mod := catalog["apps.wave-link"]
	if mod == nil || mod.Capture == nil || mod.Secrets == nil {
		t.Fatalf("Wave Link capture security boundary is incomplete: %+v", mod)
	}

	for _, pattern := range []string{`**\EBWebView\**`, `**\AudioPluginCache\**`} {
		if !slices.Contains(mod.Capture.ExcludeGlobs, pattern) {
			t.Errorf("capture.excludeGlobs missing %q", pattern)
		}
		if !slices.Contains(mod.Secrets.Files, pattern) {
			t.Errorf("secrets.files missing %q", pattern)
		}
	}
}
