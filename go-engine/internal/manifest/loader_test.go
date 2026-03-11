// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
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
