// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadV1CandidatePatchDerivesOnlyApprovedPathAndScope(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("diff --git a/bundles/work.jsonc b/bundles/work.jsonc\nindex 1111111..2222222 100644\n--- a/bundles/work.jsonc\n+++ b/bundles/work.jsonc\n@@ -1 +1 @@\n-{}\n+{\"modules\":[]}\n")
	writeV1Patch(t, root, "catalog-one", raw)
	identity, err := LoadV1CandidatePatch(root, V1PatchRequest{CandidateID: "catalog-one", Family: "catalog", PatchSHA256: v1PatchDigest(raw), BundleID: "work"})
	if err != nil || identity.SHA256 != v1PatchDigest(raw) {
		t.Fatalf("LoadV1CandidatePatch() = %#v, %v", identity, err)
	}
}

func TestLoadV1CandidatePatchRejectsWrongTargetExtraPathAndNonMechanicalSidecar(t *testing.T) {
	tests := []struct {
		name    string
		request V1PatchRequest
		raw     []byte
	}{
		{
			name:    "wrong target",
			request: V1PatchRequest{CandidateID: "module-one", Family: "module", ModuleID: "apps.foo"},
			raw:     []byte("diff --git a/modules/apps/bar/module.jsonc b/modules/apps/bar/module.jsonc\nindex 1111111..2222222 100644\n--- a/modules/apps/bar/module.jsonc\n+++ b/modules/apps/bar/module.jsonc\n@@ -1 +1 @@\n-{}\n+{\"id\":\"apps.bar\"}\n"),
		},
		{
			name:    "forbidden extra path",
			request: V1PatchRequest{CandidateID: "catalog-two", Family: "catalog", BundleID: "work"},
			raw: []byte("diff --git a/bundles/work.jsonc b/bundles/work.jsonc\nindex 1111111..2222222 100644\n--- a/bundles/work.jsonc\n+++ b/bundles/work.jsonc\n@@ -1 +1 @@\n-{}\n+{\"modules\":[]}\ndiff --git a/.github/workflows/x.yml b/.github/workflows/x.yml\nindex 1111111..2222222 100644\n--- a/.github/workflows/x.yml\n+++ b/.github/workflows/x.yml\n@@ -1 +1 @@\n-a\n+b\n"),
		},
		{
			name:    "non mechanical sidecar",
			request: V1PatchRequest{CandidateID: "module-two", Family: "module", ModuleID: "apps.foo"},
			raw: []byte("diff --git a/modules/apps/foo/module.jsonc b/modules/apps/foo/module.jsonc\nindex 1111111..2222222 100644\n--- a/modules/apps/foo/module.jsonc\n+++ b/modules/apps/foo/module.jsonc\n@@ -1 +1 @@\n-{}\n+{\"id\":\"apps.foo\"}\ndiff --git a/modules/apps/foo/validation.jsonc b/modules/apps/foo/validation.jsonc\nindex 1111111..2222222 100644\n--- a/modules/apps/foo/validation.jsonc\n+++ b/modules/apps/foo/validation.jsonc\n@@ -1 +1 @@\n-{\"moduleRevision\":\"old\"}\n+{\"moduleRevision\":\"new\",\"extra\":true}\n"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			tt.request.PatchSHA256 = v1PatchDigest(tt.raw)
			writeV1Patch(t, root, tt.request.CandidateID, tt.raw)
			if _, err := LoadV1CandidatePatch(root, tt.request); !errors.Is(err, ErrInvalidV1PatchScope) {
				t.Fatalf("LoadV1CandidatePatch() error = %v, want scope rejection", err)
			}
		})
	}
}

func writeV1Patch(t *testing.T, root, id string, raw []byte) {
	t.Helper()
	path := filepath.Join(root, "validation", "ci-efficacy", "pilot-v1", "patches", id+".patch")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func v1PatchDigest(raw []byte) string {
	return hexDigest(sha256.Sum256(raw))
}
