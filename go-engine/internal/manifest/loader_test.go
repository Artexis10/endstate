// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// StripJsoncComments
// ---------------------------------------------------------------------------

func TestStripJsoncComments(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "no comments passthrough",
			input: `{"version":1}`,
			want:  `{"version":1}`,
		},
		{
			name:  "single-line comment stripped",
			input: "{\n  // this is a comment\n  \"version\": 1\n}",
			want:  "{\n  \n  \"version\": 1\n}",
		},
		{
			name:  "block comment stripped",
			input: "{\n  /* block */\n  \"version\": 1\n}",
			want:  "{\n  \n  \"version\": 1\n}",
		},
		{
			name:  "multi-line block comment stripped",
			input: "{\n  /* line one\n     line two */\n  \"version\": 1\n}",
			want:  "{\n  \n  \"version\": 1\n}",
		},
		{
			name:  "double-slash inside string preserved",
			input: `{"url":"https://example.com/path"}`,
			want:  `{"url":"https://example.com/path"}`,
		},
		{
			name:  "block comment opener inside string preserved",
			input: `{"key":"value /* not a comment */"}`,
			want:  `{"key":"value /* not a comment */"}`,
		},
		{
			name:  "escaped quote inside string handled",
			input: `{"key":"say \"hello\" // not a comment"}`,
			want:  `{"key":"say \"hello\" // not a comment"}`,
		},
		{
			name:  "trailing comment on same line as value",
			input: "{\n  \"version\": 1 // trailing\n}",
			want:  "{\n  \"version\": 1 \n}",
		},
		{
			name:  "block comment between fields",
			input: "{\n  \"a\": 1,/* comment */\"b\": 2\n}",
			want:  "{\n  \"a\": 1,\"b\": 2\n}",
		},
		{
			name:  "empty input",
			input: "",
			want:  "",
		},
		{
			name:  "CRLF line endings preserved after comment removal",
			input: "{\r\n  // comment\r\n  \"v\": 1\r\n}",
			want:  "{\r\n  \r\n  \"v\": 1\r\n}",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := string(StripJsoncComments([]byte(tc.input)))
			if got != tc.want {
				t.Errorf("StripJsoncComments(%q)\n  got  %q\n  want %q", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LoadManifest - basic loading
// ---------------------------------------------------------------------------

func TestLoadManifest(t *testing.T) {
	tests := []struct {
		name        string
		fixture     string
		wantErr     bool
		wantErrPart string
		check       func(t *testing.T, m *Manifest)
	}{
		{
			name:    "valid JSONC profile loads without error",
			fixture: "testdata/valid-profile.jsonc",
			check: func(t *testing.T, m *Manifest) {
				if m.Name != "test-profile" {
					t.Errorf("Name = %q, want %q", m.Name, "test-profile")
				}
				if len(m.Apps) != 1 {
					t.Fatalf("len(Apps) = %d, want 1", len(m.Apps))
				}
				if m.Apps[0].ID != "test-app" {
					t.Errorf("Apps[0].ID = %q, want %q", m.Apps[0].ID, "test-app")
				}
			},
		},
		{
			name:        "missing file returns error",
			fixture:     "testdata/nonexistent.jsonc",
			wantErr:     true,
			wantErrPart: "cannot read",
		},
		{
			name:        "invalid JSON returns parse error",
			fixture:     "testdata/invalid-json.json",
			wantErr:     true,
			wantErrPart: "JSON parse error",
		},
		{
			name:    "URLs containing // inside strings are not stripped",
			fixture: "testdata/with-url-strings.jsonc",
			check: func(t *testing.T, m *Manifest) {
				if len(m.Apps) == 0 {
					t.Fatal("expected at least one app")
				}
				got := m.Apps[0].Refs["windows"]
				want := "https://example.com/app"
				if got != want {
					t.Errorf("refs.windows = %q, want %q", got, want)
				}
			},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			m, err := LoadManifest(tc.fixture)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tc.wantErrPart)
				}
				if tc.wantErrPart != "" && !strings.Contains(err.Error(), tc.wantErrPart) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tc.wantErrPart)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.check != nil {
				tc.check(t, m)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LoadManifest - includes resolution
// ---------------------------------------------------------------------------

func TestLoadManifestIncludes(t *testing.T) {
	t.Run("included apps are merged into parent", func(t *testing.T) {
		m, err := LoadManifest("testdata/include-parent.jsonc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ids := make(map[string]bool)
		for _, a := range m.Apps {
			ids[a.ID] = true
		}
		if !ids["parent-app"] {
			t.Error("expected parent-app in merged apps")
		}
		if !ids["child-app"] {
			t.Error("expected child-app from included manifest")
		}
	})

	t.Run("circular include returns error", func(t *testing.T) {
		// Build two temp files that include each other.
		dir := t.TempDir()
		fileA := filepath.Join(dir, "a.jsonc")
		fileB := filepath.Join(dir, "b.jsonc")

		contentA := `{"version":1,"apps":[],"includes":["./b.jsonc"]}`
		contentB := `{"apps":[],"includes":["./a.jsonc"]}`

		if err := os.WriteFile(fileA, []byte(contentA), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fileB, []byte(contentB), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadManifest(fileA)
		if err == nil {
			t.Fatal("expected circular include error, got nil")
		}
		if !strings.Contains(err.Error(), "circular") {
			t.Errorf("error = %q, want it to contain 'circular'", err.Error())
		}
	})

	t.Run("missing include file returns error", func(t *testing.T) {
		dir := t.TempDir()
		fileA := filepath.Join(dir, "main.jsonc")
		content := `{"version":1,"apps":[],"includes":["./does-not-exist.jsonc"]}`
		if err := os.WriteFile(fileA, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}

		_, err := LoadManifest(fileA)
		if err == nil {
			t.Fatal("expected error for missing include, got nil")
		}
	})
}

// ---------------------------------------------------------------------------
// ValidateProfile
// ---------------------------------------------------------------------------

func TestValidateProfile(t *testing.T) {
	tests := []struct {
		name       string
		fixture    string
		wantValid  bool
		wantCodes  []string
	}{
		{
			name:      "valid profile passes",
			fixture:   "testdata/valid-profile.jsonc",
			wantValid: true,
		},
		{
			name:      "missing file returns FILE_NOT_FOUND",
			fixture:   "testdata/does-not-exist.json",
			wantValid: false,
			wantCodes: []string{"FILE_NOT_FOUND"},
		},
		{
			name:      "invalid JSON returns PARSE_ERROR",
			fixture:   "testdata/invalid-json.json",
			wantValid: false,
			wantCodes: []string{"PARSE_ERROR"},
		},
		{
			name:      "missing version returns MISSING_VERSION",
			fixture:   "testdata/no-version.json",
			wantValid: false,
			wantCodes: []string{"MISSING_VERSION"},
		},
		{
			name:      "string version returns INVALID_VERSION_TYPE",
			fixture:   "testdata/string-version.json",
			wantValid: false,
			wantCodes: []string{"INVALID_VERSION_TYPE"},
		},
		{
			name:      "missing apps returns MISSING_APPS",
			fixture:   "testdata/no-apps.json",
			wantValid: false,
			wantCodes: []string{"MISSING_APPS"},
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			res := ValidateProfile(tc.fixture)
			if res.Valid != tc.wantValid {
				t.Errorf("Valid = %v, want %v (errors: %v)", res.Valid, tc.wantValid, res.Errors)
			}
			codes := make(map[string]bool)
			for _, e := range res.Errors {
				codes[e.Code] = true
			}
			for _, wc := range tc.wantCodes {
				if !codes[wc] {
					t.Errorf("expected error code %q not found in %v", wc, res.Errors)
				}
			}
		})
	}
}

// TestValidateProfileAppsObject verifies INVALID_APPS_TYPE is returned when
// "apps" is a JSON object rather than an array. This exercises the inline
// fixture path rather than a file to keep the test self-contained.
func TestValidateProfileAppsObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "apps-object.json")
	content := `{"version":1,"apps":{}}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	res := ValidateProfile(path)
	if res.Valid {
		t.Fatal("expected invalid, got valid")
	}
	found := false
	for _, e := range res.Errors {
		if e.Code == "INVALID_APPS_TYPE" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected INVALID_APPS_TYPE, got %v", res.Errors)
	}
}

// TestValidateProfileWrongVersion verifies UNSUPPORTED_VERSION is returned when
// version is a number but not 1.
func TestValidateProfileWrongVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "wrong-version.json")
	content := `{"version":2,"apps":[]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	res := ValidateProfile(path)
	if res.Valid {
		t.Fatal("expected invalid, got valid")
	}
	found := false
	for _, e := range res.Errors {
		if e.Code == "UNSUPPORTED_VERSION" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected UNSUPPORTED_VERSION, got %v", res.Errors)
	}
}

// ---------------------------------------------------------------------------
// Gap tests ported from Pester: JSONC comment stripping edge cases
// (Manifest.Tests.ps1 - JSONC comment stripping)
// ---------------------------------------------------------------------------

func TestStripJsoncComments_InlineCommentAfterValue(t *testing.T) {
	// Pester: "Should parse JSONC with inline comments after values"
	input := `{
  "version": 1, // version comment
  "name": "test",
  "apps": [
    {
      "id": "test-app",
      "refs": {
        "windows": "Test.App" // platform ref
      }
    }
  ]
}`
	clean := StripJsoncComments([]byte(input))
	var m Manifest
	if err := json.Unmarshal(clean, &m); err != nil {
		t.Fatalf("failed to unmarshal after stripping inline comments: %v", err)
	}
	if m.Name != "test" {
		t.Errorf("Name = %q, want %q", m.Name, "test")
	}
	if len(m.Apps) != 1 || m.Apps[0].ID != "test-app" {
		t.Errorf("Apps not parsed correctly after inline comment stripping")
	}
}

func TestStripJsoncComments_HeaderComments(t *testing.T) {
	// Pester: "Should parse JSONC with header comments like my-desktop.jsonc"
	// Regression test for manifests with comments at lines 2-6.
	input := `{
  // Comment at top
  // Another header comment
  "version": 1,
  "name": "header-test",
  "apps": []
}`
	clean := StripJsoncComments([]byte(input))
	var m Manifest
	if err := json.Unmarshal(clean, &m); err != nil {
		t.Fatalf("failed to parse JSONC with header comments: %v", err)
	}
	if m.Name != "header-test" {
		t.Errorf("Name = %q, want %q", m.Name, "header-test")
	}
}

func TestStripJsoncComments_HttpAndHttpsURLsPreserved(t *testing.T) {
	// Pester: "Should preserve http:// URLs inside strings (do not strip as comment)"
	input := `{
  "version": 1,
  "name": "test",
  "homepage": "http://example.com",
  "docs": "https://example.com/docs",
  "apps": []
}`
	clean := StripJsoncComments([]byte(input))
	var parsed map[string]interface{}
	if err := json.Unmarshal(clean, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed["homepage"] != "http://example.com" {
		t.Errorf("homepage = %v, want %q", parsed["homepage"], "http://example.com")
	}
	if parsed["docs"] != "https://example.com/docs" {
		t.Errorf("docs = %v, want %q", parsed["docs"], "https://example.com/docs")
	}
}

func TestStripJsoncComments_CRLFParsesCorrectly(t *testing.T) {
	// Pester: "Should handle CRLF line endings correctly"
	input := "{\r\n  // Comment with CRLF\r\n  \"version\": 1,\r\n  \"name\": \"test\",\r\n  \"apps\": []\r\n}"
	clean := StripJsoncComments([]byte(input))
	var m Manifest
	if err := json.Unmarshal(clean, &m); err != nil {
		t.Fatalf("failed to parse JSONC with CRLF endings: %v", err)
	}
	if m.Name != "test" {
		t.Errorf("Name = %q, want %q", m.Name, "test")
	}
}

func TestStripJsoncComments_MultiLineBlockComment(t *testing.T) {
	// Pester: "Should parse JSONC with multi-line /* */ comments"
	input := `{
  /* This is a multi-line comment
     spanning multiple lines */
  "version": 1,
  "name": "test",
  "apps": []
}`
	clean := StripJsoncComments([]byte(input))
	var m Manifest
	if err := json.Unmarshal(clean, &m); err != nil {
		t.Fatalf("failed to parse JSONC with multi-line block comment: %v", err)
	}
	if m.Name != "test" {
		t.Errorf("Name = %q, want %q", m.Name, "test")
	}
}

// ---------------------------------------------------------------------------
// Gap tests ported from Pester: Include resolution
// (Manifest.Tests.ps1, ProfileComposition.Tests.ps1)
// ---------------------------------------------------------------------------

func TestLoadManifestIncludes_MergesRestoreAndVerify(t *testing.T) {
	// Pester: Include should merge restore and verify entries, not just apps.
	dir := t.TempDir()

	child := filepath.Join(dir, "child.jsonc")
	childContent := `{
  "version": 1,
  "apps": [
    { "id": "child-app", "refs": { "windows": "Child.App" } }
  ],
  "restore": [
    { "type": "copy", "source": "./a.conf", "target": "~/.a.conf" }
  ],
  "verify": [
    { "type": "file-exists", "path": "~/.a.conf" }
  ]
}`
	if err := os.WriteFile(child, []byte(childContent), 0644); err != nil {
		t.Fatal(err)
	}

	parent := filepath.Join(dir, "parent.jsonc")
	parentContent := `{
  "version": 1,
  "name": "parent",
  "apps": [
    { "id": "parent-app", "refs": { "windows": "Parent.App" } }
  ],
  "restore": [
    { "type": "copy", "source": "./b.conf", "target": "~/.b.conf" }
  ],
  "verify": [
    { "type": "command-exists", "command": "git" }
  ],
  "includes": ["./child.jsonc"]
}`
	if err := os.WriteFile(parent, []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(parent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Apps) != 2 {
		t.Errorf("len(Apps) = %d, want 2", len(m.Apps))
	}
	if len(m.Restore) != 2 {
		t.Errorf("len(Restore) = %d, want 2 (merged from parent+child)", len(m.Restore))
	}
	if len(m.Verify) != 2 {
		t.Errorf("len(Verify) = %d, want 2 (merged from parent+child)", len(m.Verify))
	}
}

func TestLoadManifestIncludes_ParentAppsBeforeChild(t *testing.T) {
	// Pester: "Should contain local app from root manifest" +
	//         "Should contain apps from included base-apps.jsonc"
	// Included apps are appended *after* the parent's own apps.
	dir := t.TempDir()

	child := filepath.Join(dir, "child.jsonc")
	childContent := `{
  "apps": [
    { "id": "child-1", "refs": { "windows": "Child.One" } },
    { "id": "child-2", "refs": { "windows": "Child.Two" } }
  ]
}`
	if err := os.WriteFile(child, []byte(childContent), 0644); err != nil {
		t.Fatal(err)
	}

	parent := filepath.Join(dir, "parent.jsonc")
	parentContent := `{
  "version": 1,
  "name": "parent",
  "apps": [
    { "id": "local-app", "refs": { "windows": "Local.App" } }
  ],
  "includes": ["./child.jsonc"]
}`
	if err := os.WriteFile(parent, []byte(parentContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(parent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Apps) != 3 {
		t.Fatalf("len(Apps) = %d, want 3", len(m.Apps))
	}
	// Parent's app must come first
	if m.Apps[0].ID != "local-app" {
		t.Errorf("Apps[0].ID = %q, want %q (parent app should be first)", m.Apps[0].ID, "local-app")
	}
	// Included apps follow
	if m.Apps[1].ID != "child-1" {
		t.Errorf("Apps[1].ID = %q, want %q", m.Apps[1].ID, "child-1")
	}
	if m.Apps[2].ID != "child-2" {
		t.Errorf("Apps[2].ID = %q, want %q", m.Apps[2].ID, "child-2")
	}
}

func TestLoadManifestIncludes_ChainedIncludes(t *testing.T) {
	// Multi-level includes: A -> B -> C should merge all apps.
	dir := t.TempDir()

	fileC := filepath.Join(dir, "c.jsonc")
	contentC := `{ "apps": [{ "id": "c-app", "refs": { "windows": "C.App" } }] }`
	if err := os.WriteFile(fileC, []byte(contentC), 0644); err != nil {
		t.Fatal(err)
	}

	fileB := filepath.Join(dir, "b.jsonc")
	contentB := `{ "apps": [{ "id": "b-app", "refs": { "windows": "B.App" } }], "includes": ["./c.jsonc"] }`
	if err := os.WriteFile(fileB, []byte(contentB), 0644); err != nil {
		t.Fatal(err)
	}

	fileA := filepath.Join(dir, "a.jsonc")
	contentA := `{ "version": 1, "apps": [{ "id": "a-app", "refs": { "windows": "A.App" } }], "includes": ["./b.jsonc"] }`
	if err := os.WriteFile(fileA, []byte(contentA), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(fileA)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Apps) != 3 {
		t.Fatalf("len(Apps) = %d, want 3 (A + B + C)", len(m.Apps))
	}
	ids := make(map[string]bool)
	for _, a := range m.Apps {
		ids[a.ID] = true
	}
	for _, want := range []string{"a-app", "b-app", "c-app"} {
		if !ids[want] {
			t.Errorf("expected %q in merged apps", want)
		}
	}
}

func TestLoadManifest_SelfIncludeDetected(t *testing.T) {
	// A file that includes itself should be detected as circular.
	dir := t.TempDir()
	file := filepath.Join(dir, "self.jsonc")
	content := `{ "version": 1, "apps": [], "includes": ["./self.jsonc"] }`
	if err := os.WriteFile(file, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(file)
	if err == nil {
		t.Fatal("expected circular include error for self-referencing file, got nil")
	}
	if !strings.Contains(err.Error(), "circular") {
		t.Errorf("error = %q, want it to contain 'circular'", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Gap tests ported from Pester: Manifest normalization
// (Manifest.Tests.ps1 - Normalization)
// ---------------------------------------------------------------------------

func TestLoadManifest_DefaultsEmptyArrays(t *testing.T) {
	// Pester: "Should initialize missing arrays to empty"
	// When restore/verify are absent, the loaded manifest should have nil/empty slices.
	dir := t.TempDir()
	path := filepath.Join(dir, "bare.jsonc")
	content := `{
  "version": 1,
  "name": "bare",
  "apps": [
    { "id": "app1", "refs": { "windows": "App.One" } }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Go's omitempty means the slices will be nil when absent from JSON.
	// This is fine because nil slices behave identically to empty slices in Go.
	if m.Restore == nil {
		// nil is equivalent to empty in Go - this is the expected behavior
	}
	if len(m.Restore) != 0 {
		t.Errorf("len(Restore) = %d, want 0 (should default to empty)", len(m.Restore))
	}
	if len(m.Verify) != 0 {
		t.Errorf("len(Verify) = %d, want 0 (should default to empty)", len(m.Verify))
	}
}

func TestLoadManifest_MultiPlatformRefs(t *testing.T) {
	// Pester: "Should parse multi-platform refs"
	dir := t.TempDir()
	path := filepath.Join(dir, "multi-ref.jsonc")
	content := `{
  "version": 1,
  "name": "multi-ref",
  "apps": [
    {
      "id": "cross-plat",
      "refs": {
        "windows": "Cross.Plat",
        "linux": "cross-plat"
      }
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Apps) != 1 {
		t.Fatalf("len(Apps) = %d, want 1", len(m.Apps))
	}
	if m.Apps[0].Refs["windows"] != "Cross.Plat" {
		t.Errorf("refs.windows = %q, want %q", m.Apps[0].Refs["windows"], "Cross.Plat")
	}
	if m.Apps[0].Refs["linux"] != "cross-plat" {
		t.Errorf("refs.linux = %q, want %q", m.Apps[0].Refs["linux"], "cross-plat")
	}
}

func TestLoadManifest_RepeatedParsingProducesSameResult(t *testing.T) {
	// Pester: "Should produce identical output on repeated parsing"
	m1, err := LoadManifest("testdata/valid-profile.jsonc")
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	m2, err := LoadManifest("testdata/valid-profile.jsonc")
	if err != nil {
		t.Fatalf("second load: %v", err)
	}

	if m1.Name != m2.Name {
		t.Errorf("Name mismatch: %q vs %q", m1.Name, m2.Name)
	}
	if len(m1.Apps) != len(m2.Apps) {
		t.Fatalf("Apps count mismatch: %d vs %d", len(m1.Apps), len(m2.Apps))
	}
	for i := range m1.Apps {
		if m1.Apps[i].ID != m2.Apps[i].ID {
			t.Errorf("Apps[%d].ID mismatch: %q vs %q", i, m1.Apps[i].ID, m2.Apps[i].ID)
		}
	}
}

// ---------------------------------------------------------------------------
// Gap tests ported from Pester: Profile validation edge cases
// (ProfileContract.Tests.ps1)
// ---------------------------------------------------------------------------

func TestValidateProfile_MinimalValid(t *testing.T) {
	// Pester: "Should validate a minimal valid manifest (version + empty apps)"
	dir := t.TempDir()
	path := filepath.Join(dir, "minimal.json")
	content := `{"version": 1, "apps": []}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	res := ValidateProfile(path)
	if !res.Valid {
		t.Errorf("expected valid, got errors: %v", res.Errors)
	}
}

func TestValidateProfile_AppsAsString(t *testing.T) {
	// Pester: "Should fail when apps is a string"
	dir := t.TempDir()
	path := filepath.Join(dir, "apps-string.json")
	content := `{"version":1,"apps":"not-an-array"}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	res := ValidateProfile(path)
	if res.Valid {
		t.Fatal("expected invalid for apps-as-string, got valid")
	}
	found := false
	for _, e := range res.Errors {
		if e.Code == "INVALID_APPS_TYPE" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected INVALID_APPS_TYPE, got %v", res.Errors)
	}
}

func TestValidateProfile_JSoncWithComments(t *testing.T) {
	// Pester: "Should validate JSONC files with comments"
	dir := t.TempDir()
	path := filepath.Join(dir, "commented.jsonc")
	content := `{
  // This is a comment
  "version": 1,
  "name": "commented-profile",
  /* Multi-line
     comment */
  "apps": []
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	res := ValidateProfile(path)
	if !res.Valid {
		t.Errorf("expected valid for JSONC with comments, got errors: %v", res.Errors)
	}
}

// ---------------------------------------------------------------------------
// Gap tests ported from Pester: ProfileComposition.Tests.ps1
// ---------------------------------------------------------------------------

// TestLoadManifestIncludes_IncludeWithExtension verifies that include paths
// with file extensions (.jsonc) are resolved as file paths relative to the
// parent manifest.
// (Pester: ProfileComposition - "Should resolve include with .jsonc extension
// as file path")
func TestLoadManifestIncludes_IncludeWithExtension(t *testing.T) {
	dir := t.TempDir()

	// Create included file
	child := filepath.Join(dir, "extras.jsonc")
	childContent := `{
  "version": 1,
  "apps": [
    { "id": "file-app", "refs": { "windows": "File.App" } }
  ]
}`
	if err := os.WriteFile(child, []byte(childContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create root manifest with file path include
	root := filepath.Join(dir, "root.jsonc")
	rootContent := `{
  "version": 1,
  "name": "root",
  "includes": ["./extras.jsonc"],
  "apps": [
    { "id": "root-app", "refs": { "windows": "Root.App" } }
  ]
}`
	if err := os.WriteFile(root, []byte(rootContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Apps) != 2 {
		t.Fatalf("len(Apps) = %d, want 2", len(m.Apps))
	}

	refs := make(map[string]bool)
	for _, a := range m.Apps {
		refs[a.Refs["windows"]] = true
	}
	if !refs["Root.App"] {
		t.Error("expected Root.App in merged apps")
	}
	if !refs["File.App"] {
		t.Error("expected File.App from included manifest")
	}
}

// TestLoadManifestIncludes_MissingIncludeReturnsError verifies that a
// reference to a nonexistent include produces a clear error.
// (Pester: ProfileComposition - "Should throw clear error when profile name
// cannot be resolved")
func TestLoadManifestIncludes_MissingIncludeReturnsError(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root.jsonc")
	content := `{
  "version": 1,
  "name": "root",
  "includes": ["./nonexistent.jsonc"],
  "apps": []
}`
	if err := os.WriteFile(root, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := LoadManifest(root)
	if err == nil {
		t.Fatal("expected error for missing include, got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent") {
		t.Errorf("error = %q, want it to mention the missing file", err.Error())
	}
}

// TestLoadManifest_ManifestFieldsPreserved verifies that the loaded manifest
// preserves all declared fields: name, version, configModules.
// (Pester: ProfileComposition - "Should preserve excludeConfigs array" and
// similar field-preservation tests)
func TestLoadManifest_ManifestFieldsPreserved(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "full.jsonc")
	content := `{
  "version": 1,
  "name": "my-manifest",
  "captured": "2025-01-15T10:30:00Z",
  "configModules": ["apps.git", "apps.vscode"],
  "apps": [
    { "id": "app1", "refs": { "windows": "App.One" } }
  ],
  "restore": [
    { "type": "copy", "source": "./a", "target": "./b" }
  ],
  "verify": [
    { "type": "command-exists", "command": "git" }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m.Name != "my-manifest" {
		t.Errorf("Name = %q, want %q", m.Name, "my-manifest")
	}
	if m.Captured != "2025-01-15T10:30:00Z" {
		t.Errorf("Captured = %q, want %q", m.Captured, "2025-01-15T10:30:00Z")
	}
	if len(m.ConfigModules) != 2 {
		t.Fatalf("len(ConfigModules) = %d, want 2", len(m.ConfigModules))
	}
	if m.ConfigModules[0] != "apps.git" || m.ConfigModules[1] != "apps.vscode" {
		t.Errorf("ConfigModules = %v, want [apps.git, apps.vscode]", m.ConfigModules)
	}
	if len(m.Apps) != 1 {
		t.Errorf("len(Apps) = %d, want 1", len(m.Apps))
	}
	if len(m.Restore) != 1 {
		t.Errorf("len(Restore) = %d, want 1", len(m.Restore))
	}
	if len(m.Verify) != 1 {
		t.Errorf("len(Verify) = %d, want 1", len(m.Verify))
	}
}

// TestLoadManifest_AbsoluteIncludePath verifies that absolute include paths
// are resolved correctly.
// (Pester: ManifestPathResolution - "returns absolute path unchanged")
func TestLoadManifest_AbsoluteIncludePath(t *testing.T) {
	dir := t.TempDir()
	subdir := filepath.Join(dir, "sub")
	if err := os.MkdirAll(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create included file in subdir
	child := filepath.Join(subdir, "child.jsonc")
	childContent := `{ "apps": [{ "id": "child-app", "refs": { "windows": "Child.App" } }] }`
	if err := os.WriteFile(child, []byte(childContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create root manifest referencing child by absolute path
	absChild := child
	// Use forward slashes in JSON for portability
	absChildJSON := strings.ReplaceAll(absChild, `\`, `\\`)
	root := filepath.Join(dir, "root.jsonc")
	rootContent := `{
  "version": 1,
  "apps": [{ "id": "root-app", "refs": { "windows": "Root.App" } }],
  "includes": ["` + absChildJSON + `"]
}`
	if err := os.WriteFile(root, []byte(rootContent), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Apps) != 2 {
		t.Fatalf("len(Apps) = %d, want 2", len(m.Apps))
	}
	ids := make(map[string]bool)
	for _, a := range m.Apps {
		ids[a.ID] = true
	}
	if !ids["root-app"] || !ids["child-app"] {
		t.Errorf("expected both root-app and child-app in merged result, got IDs: %v", ids)
	}
}

// TestLoadManifest_RestoreEntryFields verifies that restore entries preserve
// all fields when loaded from a manifest.
func TestLoadManifest_RestoreEntryFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "restore.jsonc")
	content := `{
  "version": 1,
  "name": "restore-test",
  "apps": [],
  "restore": [
    {
      "type": "copy",
      "source": "./src/config.json",
      "target": "~/.config/app/config.json",
      "backup": true,
      "optional": true
    }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Restore) != 1 {
		t.Fatalf("len(Restore) = %d, want 1", len(m.Restore))
	}

	r := m.Restore[0]
	if r.Type != "copy" {
		t.Errorf("Restore[0].Type = %q, want %q", r.Type, "copy")
	}
	if r.Source != "./src/config.json" {
		t.Errorf("Restore[0].Source = %q, want %q", r.Source, "./src/config.json")
	}
	if r.Target != "~/.config/app/config.json" {
		t.Errorf("Restore[0].Target = %q, want %q", r.Target, "~/.config/app/config.json")
	}
	if !r.Backup {
		t.Error("Restore[0].Backup = false, want true")
	}
	if !r.Optional {
		t.Error("Restore[0].Optional = false, want true")
	}
}

// TestLoadManifest_VerifyEntryFields verifies that verify entries preserve
// all fields when loaded from a manifest.
func TestLoadManifest_VerifyEntryFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "verify.jsonc")
	content := `{
  "version": 1,
  "name": "verify-test",
  "apps": [],
  "verify": [
    { "type": "file-exists", "path": "~/.config.conf" },
    { "type": "command-exists", "command": "git" }
  ]
}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.Verify) != 2 {
		t.Fatalf("len(Verify) = %d, want 2", len(m.Verify))
	}

	v0 := m.Verify[0]
	if v0.Type != "file-exists" {
		t.Errorf("Verify[0].Type = %q, want %q", v0.Type, "file-exists")
	}
	if v0.Path != "~/.config.conf" {
		t.Errorf("Verify[0].Path = %q, want %q", v0.Path, "~/.config.conf")
	}

	v1 := m.Verify[1]
	if v1.Type != "command-exists" {
		t.Errorf("Verify[1].Type = %q, want %q", v1.Type, "command-exists")
	}
	if v1.Command != "git" {
		t.Errorf("Verify[1].Command = %q, want %q", v1.Command, "git")
	}
}

// ---------------------------------------------------------------------------
// HashManifest — CRLF→LF normalization
// ---------------------------------------------------------------------------

// TestHashManifest_CRLFAndLFProduceSameHash verifies the core invariant:
// the same manifest content with CRLF line endings and with LF line endings
// must produce identical hashes.
func TestHashManifest_CRLFAndLFProduceSameHash(t *testing.T) {
	dir := t.TempDir()

	content := `{"version":1,"name":"test","apps":[]}` + "\n"

	// Write LF version.
	lfPath := filepath.Join(dir, "lf.jsonc")
	if err := os.WriteFile(lfPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	// Write CRLF version.
	crlfContent := strings.ReplaceAll(content, "\n", "\r\n")
	crlfPath := filepath.Join(dir, "crlf.jsonc")
	if err := os.WriteFile(crlfPath, []byte(crlfContent), 0644); err != nil {
		t.Fatal(err)
	}

	lfHash, err := HashManifest(lfPath)
	if err != nil {
		t.Fatalf("HashManifest(lf) error: %v", err)
	}

	crlfHash, err := HashManifest(crlfPath)
	if err != nil {
		t.Fatalf("HashManifest(crlf) error: %v", err)
	}

	if lfHash != crlfHash {
		t.Errorf("CRLF and LF hashes differ: LF=%q CRLF=%q", lfHash, crlfHash)
	}
}

// TestHashManifest_DifferentContentProducesDifferentHash verifies that
// genuinely different manifest content produces different hashes.
func TestHashManifest_DifferentContentProducesDifferentHash(t *testing.T) {
	dir := t.TempDir()

	aPath := filepath.Join(dir, "a.jsonc")
	bPath := filepath.Join(dir, "b.jsonc")
	os.WriteFile(aPath, []byte(`{"version":1,"name":"a","apps":[]}`), 0644)
	os.WriteFile(bPath, []byte(`{"version":1,"name":"b","apps":[]}`), 0644)

	hashA, err := HashManifest(aPath)
	if err != nil {
		t.Fatalf("HashManifest(a) error: %v", err)
	}
	hashB, err := HashManifest(bPath)
	if err != nil {
		t.Fatalf("HashManifest(b) error: %v", err)
	}

	if hashA == hashB {
		t.Errorf("expected different hashes for different content, both got %q", hashA)
	}
}

// TestHashManifest_SameContentProducesSameHash verifies that identical
// manifest files always produce the same hash (determinism).
func TestHashManifest_SameContentProducesSameHash(t *testing.T) {
	dir := t.TempDir()
	content := []byte(`{"version":1,"name":"stable","apps":[]}`)

	aPath := filepath.Join(dir, "a.jsonc")
	bPath := filepath.Join(dir, "b.jsonc")
	os.WriteFile(aPath, content, 0644)
	os.WriteFile(bPath, content, 0644)

	hashA, err := HashManifest(aPath)
	if err != nil {
		t.Fatalf("HashManifest(a) error: %v", err)
	}
	hashB, err := HashManifest(bPath)
	if err != nil {
		t.Fatalf("HashManifest(b) error: %v", err)
	}

	if hashA != hashB {
		t.Errorf("identical files produced different hashes: %q vs %q", hashA, hashB)
	}
}

// TestHashManifest_MissingFileReturnsError verifies that a missing file
// returns an error rather than panicking or returning an empty hash.
func TestHashManifest_MissingFileReturnsError(t *testing.T) {
	_, err := HashManifest("/nonexistent/path/missing.jsonc")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// TestHashManifest_MultilineCRLFNormalized verifies multi-line JSONC content
// with CRLF line endings hashes identically to the same content with LF.
func TestHashManifest_MultilineCRLFNormalized(t *testing.T) {
	dir := t.TempDir()

	lfContent := "{\n  \"version\": 1,\n  \"name\": \"multi\",\n  \"apps\": []\n}\n"
	crlfContent := strings.ReplaceAll(lfContent, "\n", "\r\n")

	lfPath := filepath.Join(dir, "lf.jsonc")
	crlfPath := filepath.Join(dir, "crlf.jsonc")
	os.WriteFile(lfPath, []byte(lfContent), 0644)
	os.WriteFile(crlfPath, []byte(crlfContent), 0644)

	hashLF, err := HashManifest(lfPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hashCRLF, err := HashManifest(crlfPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if hashLF != hashCRLF {
		t.Errorf("multi-line CRLF and LF hashes differ: LF=%q CRLF=%q", hashLF, hashCRLF)
	}
}
