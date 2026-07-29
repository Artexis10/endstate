// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadV1CandidatePatchRejectsObserverDependentAddedLines(t *testing.T) {
	request := V1PatchRequest{CandidateID: "candidate-one", Family: "production-go", ProductionFile: "bundle/create.go", ModuleID: "apps.aida64", ScenarioID: "reviewed-capture-v1", DetectorID: "module-detector-one"}
	for _, added := range []string{
		"//go:build windows",
		"// +build windows",
		"if os.Getenv(\"ENDSTATE_TESTMODE\") != \"\" { return nil }",
		"if os.Getenv(validationmode.TestModeEnvironment) != \"\" { return nil }",
		"if opts.ValidationContext { return nil }",
		"if legacyValidationBoundary { return nil }",
		"if WithValidation(ctx) { return nil }",
		"if name == \"candidate-one\" { return nil }",
		"if module == \"apps.aida64\" { return nil }",
		"if scenario == \"reviewed-capture-v1\" { return nil }",
		"if detector == \"module-detector-one\" { return nil }",
	} {
		t.Run(added, func(t *testing.T) {
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			raw := []byte("diff --git a/go-engine/internal/bundle/create.go b/go-engine/internal/bundle/create.go\nindex 1111111..2222222 100644\n--- a/go-engine/internal/bundle/create.go\n+++ b/go-engine/internal/bundle/create.go\n@@ -1 +1 @@\n-old\n+" + added + "\n")
			request.PatchSHA256 = v1PatchDigest(raw)
			writeV1Patch(t, root, request.CandidateID, raw)
			if _, err := LoadV1CandidatePatch(root, request); !errors.Is(err, ErrInvalidV1PatchScope) {
				t.Fatalf("LoadV1CandidatePatch(%q) = %v", added, err)
			}
		})
	}
}

func TestLoadV1CandidatePatchAllowsOrdinaryProductionChange(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte(strings.Join([]string{
		"diff --git a/go-engine/internal/bundle/create.go b/go-engine/internal/bundle/create.go",
		"index 1111111..2222222 100644",
		"--- a/go-engine/internal/bundle/create.go",
		"+++ b/go-engine/internal/bundle/create.go",
		"@@ -1 +1 @@",
		"-return createBundle(paths)",
		"+return createBundle(normalizePaths(paths))",
		"",
	}, "\n"))
	request := V1PatchRequest{CandidateID: "candidate-one", Family: "production-go", ProductionFile: "bundle/create.go", ModuleID: "apps.aida64", ScenarioID: "reviewed-capture-v1", DetectorID: "module-detector-one", PatchSHA256: v1PatchDigest(raw)}
	writeV1Patch(t, root, request.CandidateID, raw)
	if _, err := LoadV1CandidatePatch(root, request); err != nil {
		t.Fatalf("LoadV1CandidatePatch() = %v", err)
	}
}

func TestLoadV1CandidatePatchDerivesOnlyApprovedProductionGoPathAndScope(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("diff --git a/go-engine/internal/bundle/create.go b/go-engine/internal/bundle/create.go\nindex 1111111..2222222 100644\n--- a/go-engine/internal/bundle/create.go\n+++ b/go-engine/internal/bundle/create.go\n@@ -1 +1 @@\n-a\n+b\n")
	writeV1Patch(t, root, "production-one", raw)
	identity, err := LoadV1CandidatePatch(root, V1PatchRequest{CandidateID: "production-one", Family: "production-go", ProductionFile: "bundle/create.go", PatchSHA256: v1PatchDigest(raw)})
	if err != nil || identity.SHA256 != v1PatchDigest(raw) {
		t.Fatalf("LoadV1CandidatePatch() = %#v, %v", identity, err)
	}
	if _, err := LoadV1CandidatePatch(root, V1PatchRequest{CandidateID: "production-one", Family: "catalog", ProductionFile: "bundle/create.go", PatchSHA256: v1PatchDigest(raw)}); !errors.Is(err, ErrInvalidV1PatchScope) {
		t.Fatalf("LoadV1CandidatePatch() = %v, want catalog rejection", err)
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
			raw:     []byte("diff --git a/bundles/work.jsonc b/bundles/work.jsonc\nindex 1111111..2222222 100644\n--- a/bundles/work.jsonc\n+++ b/bundles/work.jsonc\n@@ -1 +1 @@\n-{}\n+{\"modules\":[]}\ndiff --git a/.github/workflows/x.yml b/.github/workflows/x.yml\nindex 1111111..2222222 100644\n--- a/.github/workflows/x.yml\n+++ b/.github/workflows/x.yml\n@@ -1 +1 @@\n-a\n+b\n"),
		},
		{
			name:    "non mechanical sidecar",
			request: V1PatchRequest{CandidateID: "module-two", Family: "module", ModuleID: "apps.foo"},
			raw:     []byte("diff --git a/modules/apps/foo/module.jsonc b/modules/apps/foo/module.jsonc\nindex 1111111..2222222 100644\n--- a/modules/apps/foo/module.jsonc\n+++ b/modules/apps/foo/module.jsonc\n@@ -1 +1 @@\n-{}\n+{\"id\":\"apps.foo\"}\ndiff --git a/modules/apps/foo/validation.jsonc b/modules/apps/foo/validation.jsonc\nindex 1111111..2222222 100644\n--- a/modules/apps/foo/validation.jsonc\n+++ b/modules/apps/foo/validation.jsonc\n@@ -1 +1 @@\n-{\"moduleRevision\":\"old\"}\n+{\"moduleRevision\":\"new\",\"extra\":true}\n"),
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

func TestLoadV1CandidatePatchAcceptsOnlyOneRegisteredProductionGoFile(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	raw := []byte("diff --git a/go-engine/internal/bundle/capture_bundle.go b/go-engine/internal/bundle/capture_bundle.go\nindex 1111111..2222222 100644\n--- a/go-engine/internal/bundle/capture_bundle.go\n+++ b/go-engine/internal/bundle/capture_bundle.go\n@@ -1 +1 @@\n-a\n+b\n")
	request := V1PatchRequest{CandidateID: "production-one", Family: "production-go", ProductionFile: "bundle/capture_bundle.go", PatchSHA256: v1PatchDigest(raw)}
	writeV1Patch(t, root, request.CandidateID, raw)
	if _, err := LoadV1CandidatePatch(root, request); err != nil {
		t.Fatalf("LoadV1CandidatePatch() = %v", err)
	}
	request.ProductionFile = "bundle/not-allowed.go"
	if _, err := LoadV1CandidatePatch(root, request); !errors.Is(err, ErrInvalidV1PatchScope) {
		t.Fatalf("LoadV1CandidatePatch() = %v, want scope rejection", err)
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
