// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeSourceManifest(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profile.jsonc")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadManifest_NormalizesWingetSource(t *testing.T) {
	path := writeSourceManifest(t, `{"version":1,"apps":[{"id":"store","driver":" WINGET ","source":" MSStore ","refs":{"windows":"9NBLGGH4NNS1"}}]}`)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Apps[0].Source; got != "msstore" {
		t.Fatalf("source = %q, want msstore", got)
	}
}

func TestLoadManifest_RejectsUnsupportedOrNonWingetSource(t *testing.T) {
	tests := []struct {
		name string
		app  string
		want string
	}{
		{"unsupported", `{"id":"bad","driver":"winget","source":"private","refs":{"windows":"Vendor.App"}}`, "UNSUPPORTED_WINGET_SOURCE"},
		{"non-winget", `{"id":"bad","driver":"chocolatey","source":"msstore","refs":{"windows":"vendor.app"}}`, "SOURCE_REQUIRES_WINGET_DRIVER"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := LoadManifest(writeSourceManifest(t, `{"version":1,"apps":[`+tc.app+`]}`))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want code %s", err, tc.want)
			}
		})
	}
}
