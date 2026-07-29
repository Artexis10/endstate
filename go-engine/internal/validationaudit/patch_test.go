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

func TestLoadCandidatePatchAcceptsEligibleProductionPatches(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{name: "production go", path: "go-engine/internal/planner/plan.go"},
		{name: "module jsonc", path: "modules/apps/example/module.jsonc"},
		{name: "bundle jsonc", path: "bundles/core.jsonc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, candidate, raw := writeCandidatePatch(t, patchFor(tt.path))

			got, err := LoadCandidatePatch(root, "v1", candidate)
			if err != nil {
				t.Fatalf("LoadCandidatePatch() error = %v", err)
			}
			wantDigest := digest(raw)
			if got.SHA256 != wantDigest || !sameStrings(got.TouchedPaths, []string{tt.path}) {
				t.Fatalf("LoadCandidatePatch() = %#v, want digest %q and path %q", got, wantDigest, tt.path)
			}
		})
	}
}

func TestLoadCandidatePatchBindsDigestAndTouchedPaths(t *testing.T) {
	root, candidate, _ := writeCandidatePatch(t, patchFor("go-engine/internal/planner/plan.go"))

	t.Run("wrong digest", func(t *testing.T) {
		candidate.PatchSHA256 = strings.Repeat("f", 64)
		if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrPatchIdentityMismatch) {
			t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrPatchIdentityMismatch)
		}
	})

	t.Run("touched path drift", func(t *testing.T) {
		candidate.PatchSHA256 = digestForRawPatch(t, root, candidate)
		candidate.TouchedPaths = []string{"go-engine/internal/planner/other.go"}
		if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrPatchIdentityMismatch) {
			t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrPatchIdentityMismatch)
		}
	})

	t.Run("touched path order", func(t *testing.T) {
		pathA := "go-engine/internal/planner/plan.go"
		pathB := "modules/apps/example/module.jsonc"
		root, candidate, _ = writeCandidatePatch(t, patchFor(pathA)+patchFor(pathB))
		candidate.TouchedPaths = []string{pathB, pathA}
		if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrPatchIdentityMismatch) {
			t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrPatchIdentityMismatch)
		}
	})
}

func TestLoadCandidatePatchPreservesMultipleChangedPathOrder(t *testing.T) {
	pathA := "go-engine/internal/planner/plan.go"
	pathB := "modules/apps/example/module.jsonc"
	root, candidate, raw := writeCandidatePatch(t, patchFor(pathA)+patchFor(pathB))
	candidate.TouchedPaths = []string{pathA, pathB}

	got, err := LoadCandidatePatch(root, "v1", candidate)
	if err != nil {
		t.Fatalf("LoadCandidatePatch() error = %v", err)
	}
	if got.SHA256 != digest(raw) || !sameStrings(got.TouchedPaths, candidate.TouchedPaths) {
		t.Fatalf("LoadCandidatePatch() = %#v, want exact ordered identity", got)
	}
}

func TestLoadCandidatePatchRejectsIneligibleScope(t *testing.T) {
	tests := []string{
		"go-engine/internal/planner/plan_test.go",
		"go-engine/internal/.hidden/plan.go",
		"go-engine/internal/planner/.hidden.go",
		"go-engine/internal/validationaudit/patch.go",
		"go-engine/internal/validationci/check.go",
		"go-engine/internal/validationharness/check.go",
		"go-engine/internal/validationmatrix/check.go",
		"go-engine/internal/validationmode/check.go",
		"go-engine/cmd/endstate-validation/main.go",
		"go-engine/internal/planner/testdata/case.go",
		"go-engine/internal/planner/fixtures/case.go",
		"go-engine/internal/planner/golden.go",
		".github/workflows/audit.yml",
		"docs/audit.md",
		"payload/apps/example/config.json",
		"validation/ci-efficacy/v1/candidates.json",
		"scripts/check.go",
		"go-engine/internal/planner/plan.txt",
		"modules/apps/example/validation.jsonc",
		"modules/apps/example/manifest.jsonc",
		"modules/apps/.hidden/module.jsonc",
		"modules/apps/example/.hidden/module.jsonc",
		"bundles/.hidden.jsonc",
		"bundles/local.jsonc",
		"bundles/config.jsonc",
		"bundles/state.jsonc",
		"bundles/runtime.jsonc",
		"bundles/manifests.jsonc",
		"bundles/payload.jsonc",
		"bundles/CONFIG.jsonc",
		"bundles/Local.jsonc",
		"bundles/Runtime.jsonc",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			root, candidate, _ := writeCandidatePatch(t, patchFor(path))
			if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrIneligiblePatch) {
				t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrIneligiblePatch)
			}
		})
	}
}

func TestLoadCandidatePatchRejectsAmbiguousPatchSyntax(t *testing.T) {
	tests := []struct {
		name  string
		patch string
	}{
		{name: "double diff spacing", patch: strings.Replace(patchFor("go-engine/internal/planner/plan.go"), "diff --git", "diff  --git", 1)},
		{name: "double path spacing", patch: strings.Replace(patchFor("go-engine/internal/planner/plan.go"), " --git a/", " --git  a/", 1)},
		{name: "zero start with implicit count", patch: strings.Replace(patchFor("go-engine/internal/planner/plan.go"), "@@ -1 +1 @@", "@@ -0 +1 @@", 1)},
		{name: "incoherent first hunk coordinates", patch: strings.Replace(patchFor("go-engine/internal/planner/plan.go"), "@@ -1 +1 @@", "@@ -1 +999 @@", 1)},
		{name: "overlapping hunk ranges", patch: patchWithHunks("go-engine/internal/planner/plan.go", "@@ -2 +2 @@\n-old\n+new\n@@ -2 +2 @@\n-old\n+new\n")},
		{name: "nonmonotonic hunk ranges", patch: patchWithHunks("go-engine/internal/planner/plan.go", "@@ -3 +3 @@\n-old\n+new\n@@ -1 +1 @@\n-old\n+new\n")},
		{name: "mismatched hunk gaps", patch: patchWithHunks("go-engine/internal/planner/plan.go", "@@ -1 +1 @@\n-old\n+new\n@@ -3 +4 @@\n-old\n+new\n")},
		{name: "repeated no newline marker", patch: patchFor("go-engine/internal/planner/plan.go") + "\\ No newline at end of file\n\\ No newline at end of file\n"},
		{name: "no newline marker before content", patch: strings.Replace(patchFor("go-engine/internal/planner/plan.go"), "-old\n", "\\ No newline at end of file\n-old\n", 1)},
		{name: "eof removed side consumed again", patch: patchWithHunks("go-engine/internal/planner/plan.go", "@@ -1,2 +1,2 @@\n-old\n\\ No newline at end of file\n-old-again\n+new\n+new-again\n")},
		{name: "eof context side consumed again", patch: patchWithHunks("go-engine/internal/planner/plan.go", "@@ -1,2 +1,2 @@\n context\n\\ No newline at end of file\n-old\n+new\n")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, candidate, _ := writeCandidatePatch(t, tt.patch)
			if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrInvalidPatch) {
				t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrInvalidPatch)
			}
		})
	}
}

func TestLoadCandidatePatchAcceptsCoherentZeroAndEOFHunks(t *testing.T) {
	tests := []string{
		patchWithHunks("go-engine/internal/planner/plan.go", "@@ -0,0 +1 @@\n+new\n"),
		patchWithHunks("go-engine/internal/planner/plan.go", "@@ -1 +0,0 @@\n-old\n"),
		patchWithHunks("go-engine/internal/planner/plan.go", "@@ -1 +1 @@\n-old\n\\ No newline at end of file\n+new\n\\ No newline at end of file\n"),
	}

	for _, patch := range tests {
		root, candidate, _ := writeCandidatePatch(t, patch)
		if _, err := LoadCandidatePatch(root, "v1", candidate); err != nil {
			t.Fatalf("LoadCandidatePatch() error = %v", err)
		}
	}
}

func TestLoadCandidatePatchRejectsInvalidPatchForms(t *testing.T) {
	tests := []struct {
		name  string
		patch string
	}{
		{name: "binary marker", patch: patchFor("go-engine/internal/planner/plan.go") + "GIT binary patch\n"},
		{name: "nul byte", patch: patchFor("go-engine/internal/planner/plan.go") + "\x00"},
		{name: "empty", patch: ""},
		{name: "malformed header", patch: "diff --git a/go-engine/internal/planner/plan.go b/go-engine/internal/planner/plan.go\n--- a/go-engine/internal/planner/plan.go\n+++ b/go-engine/internal/planner/plan.go\nnot a hunk\n"},
		{name: "missing file headers", patch: "diff --git a/go-engine/internal/planner/plan.go b/go-engine/internal/planner/plan.go\n@@ -1 +1 @@\n-old\n+new\n"},
		{name: "no hunk", patch: "diff --git a/go-engine/internal/planner/plan.go b/go-engine/internal/planner/plan.go\n--- a/go-engine/internal/planner/plan.go\n+++ b/go-engine/internal/planner/plan.go\n"},
		{name: "duplicate changed path", patch: patchFor("go-engine/internal/planner/plan.go") + patchFor("go-engine/internal/planner/plan.go")},
		{name: "foreign trailing data", patch: patchFor("go-engine/internal/planner/plan.go") + "foreign data\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, candidate, _ := writeCandidatePatch(t, tt.patch)
			if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrInvalidPatch) {
				t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrInvalidPatch)
			}
		})
	}

	t.Run("oversized", func(t *testing.T) {
		root, candidate, _ := writeCandidatePatch(t, patchFor("go-engine/internal/planner/plan.go")+strings.Repeat("x", MaxPatchSize))
		if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrInvalidPatch) {
			t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrInvalidPatch)
		}
	})
}

func TestLoadCandidatePatchRejectsUnsupportedGitDeclarations(t *testing.T) {
	tests := []string{
		"new file mode 100644\n",
		"deleted file mode 100644\n",
		"old mode 100644\n",
		"new mode 100755\n",
		"similarity index 100%\nrename from go-engine/internal/planner/old.go\nrename to go-engine/internal/planner/plan.go\n",
		"similarity index 100%\ncopy from go-engine/internal/planner/old.go\ncopy to go-engine/internal/planner/plan.go\n",
		"new file mode 120000\n",
		"new file mode 160000\n",
	}

	for _, declaration := range tests {
		t.Run(strings.Fields(declaration)[0], func(t *testing.T) {
			patch := strings.Replace(patchFor("go-engine/internal/planner/plan.go"), "--- a/", declaration+"--- a/", 1)
			root, candidate, _ := writeCandidatePatch(t, patch)
			if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrInvalidPatch) {
				t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrInvalidPatch)
			}
		})
	}
}

func TestLoadCandidatePatchRejectsUnsafePatchPaths(t *testing.T) {
	tests := []string{
		"/go-engine/internal/planner/plan.go",
		"go-engine\\internal\\planner\\plan.go",
		"go-engine/internal/planner/../plan.go",
		"\"go-engine/internal/planner/plan.go\"",
		"go-engine/internal/planner/CON.go",
		"go-engine/internal/planner/plan.go.",
		"go-engine/internal/planner/plan.go ",
	}

	for _, path := range tests {
		t.Run(path, func(t *testing.T) {
			root, candidate, _ := writeCandidatePatch(t, patchWithPath(path))
			if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrInvalidPatch) {
				t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrInvalidPatch)
			}
		})
	}

	t.Run("header disagreement", func(t *testing.T) {
		patch := strings.Replace(patchFor("go-engine/internal/planner/plan.go"), "+++ b/go-engine/internal/planner/plan.go", "+++ b/go-engine/internal/planner/other.go", 1)
		root, candidate, _ := writeCandidatePatch(t, patch)
		if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrInvalidPatch) {
			t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrInvalidPatch)
		}
	})
}

func TestLoadCandidatePatchRejectsUnsafeFilesystemAuthority(t *testing.T) {
	t.Run("relative repository root", func(t *testing.T) {
		_, candidate, _ := writeCandidatePatch(t, patchFor("go-engine/internal/planner/plan.go"))
		if _, err := LoadCandidatePatch("relative", "v1", candidate); !errors.Is(err, ErrUnsafePatchPath) {
			t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrUnsafePatchPath)
		}
	})

	t.Run("leaf symbolic link", func(t *testing.T) {
		root, candidate, raw := writeCandidatePatch(t, patchFor("go-engine/internal/planner/plan.go"))
		outside := filepath.Join(t.TempDir(), "outside.patch")
		if err := os.WriteFile(outside, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		patchPath := filepath.Join(root, filepath.FromSlash(candidate.PatchPath))
		if err := os.Remove(patchPath); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, patchPath); err != nil {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrUnsafePatchPath) {
			t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrUnsafePatchPath)
		}
	})

	t.Run("intermediate symbolic link", func(t *testing.T) {
		root, candidate, raw := writeCandidatePatch(t, patchFor("go-engine/internal/planner/plan.go"))
		patches := filepath.Join(root, "validation", "ci-efficacy", "v1", "patches")
		outside := filepath.Join(t.TempDir(), "patches")
		if err := os.MkdirAll(outside, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(outside, "candidate.patch"), raw, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.RemoveAll(patches); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, patches); err != nil {
			t.Skipf("symbolic links unavailable: %v", err)
		}
		if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrUnsafePatchPath) {
			t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrUnsafePatchPath)
		}
	})
}

func TestLoadCandidatePatchReturnsPathFreeErrors(t *testing.T) {
	root, candidate, _ := writeCandidatePatch(t, patchFor("docs/private-input.md"))
	candidate.PatchPath = "validation/ci-efficacy/v1/patches/private-input.patch"

	_, err := LoadCandidatePatch(root, "v1", candidate)
	if !errors.Is(err, ErrUnsafePatchPath) {
		t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrUnsafePatchPath)
	}
	if strings.Contains(err.Error(), root) || strings.Contains(err.Error(), candidate.PatchPath) || strings.Contains(err.Error(), "private-input") {
		t.Fatalf("LoadCandidatePatch() leaked user input: %q", err)
	}
}

func writeCandidatePatch(t *testing.T, patch string) (string, Candidate, []byte) {
	t.Helper()
	root := t.TempDir()
	path := "validation/ci-efficacy/v1/patches/candidate.patch"
	fullPath := filepath.Join(root, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o700); err != nil {
		t.Fatal(err)
	}
	raw := []byte(patch)
	if err := os.WriteFile(fullPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return canonicalRoot, Candidate{PatchPath: path, PatchSHA256: digest(raw), TouchedPaths: patchPaths(patch)}, raw
}

func patchFor(path string) string {
	return patchWithPaths(path, path, path)
}

func patchWithPath(path string) string {
	return patchWithPaths(path, path, path)
}

func patchWithPaths(diffPath, oldPath, newPath string) string {
	return "diff --git a/" + diffPath + " b/" + diffPath + "\n" +
		"--- a/" + oldPath + "\n" +
		"+++ b/" + newPath + "\n" +
		"@@ -1 +1 @@\n" +
		"-old\n" +
		"+new\n"
}

func patchWithHunks(path, hunks string) string {
	return "diff --git a/" + path + " b/" + path + "\n" +
		"--- a/" + path + "\n" +
		"+++ b/" + path + "\n" + hunks
}

func patchPaths(patch string) []string {
	var paths []string
	for _, line := range strings.Split(patch, "\n") {
		if strings.HasPrefix(line, "diff --git a/") {
			parts := strings.Fields(line)
			if len(parts) == 4 {
				paths = append(paths, strings.TrimPrefix(parts[2], "a/"))
			}
		}
	}
	if len(paths) == 0 {
		return []string{"go-engine/internal/planner/plan.go"}
	}
	return paths
}

func digestForRawPatch(t *testing.T, root string, candidate Candidate) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(candidate.PatchPath)))
	if err != nil {
		t.Fatal(err)
	}
	return digest(raw)
}

func digest(raw []byte) string {
	return fmtDigest(sha256.Sum256(raw))
}

func fmtDigest(sum [sha256.Size]byte) string {
	const hex = "0123456789abcdef"
	result := make([]byte, len(sum)*2)
	for index, value := range sum {
		result[index*2] = hex[value>>4]
		result[index*2+1] = hex[value&0x0f]
	}
	return string(result)
}

func sameStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
