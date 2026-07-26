// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

func TestCaptureArtifactPathUsesCanonicalBundleExtension(t *testing.T) {
	root := t.TempDir()
	want := filepath.Join(root, "manifests", "captured"+manifest.BundleExt)
	if got := captureArtifactPath(root, "captured"); got != want {
		t.Fatalf("captureArtifactPath() = %q, want %q", got, want)
	}
}
