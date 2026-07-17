// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

func TestBuildPayloadManifestUsesExactBytesAndSortedRelativePaths(t *testing.T) {
	root := t.TempDir()
	first := []byte("line one\r\nline two\x00")
	second := []byte{0xff, 0x00, 0x01, '\n'}
	writeCaptureFile(t, filepath.Join(root, "z", "same.bin"), second)
	writeCaptureFile(t, filepath.Join(root, "a", "same.bin"), first)

	entries, err := BuildPayloadManifest(root)
	if err != nil {
		t.Fatalf("BuildPayloadManifest: %v", err)
	}
	if len(entries) != 2 || entries[0].RelativePath != "a/same.bin" || entries[1].RelativePath != "z/same.bin" {
		t.Fatalf("manifest order/hierarchy = %+v", entries)
	}
	if entries[0].Size != int64(len(first)) || entries[0].SHA256 != testSHA256(first) {
		t.Fatalf("first exact metadata = %+v", entries[0])
	}
	if entries[1].Size != int64(len(second)) || entries[1].SHA256 != testSHA256(second) {
		t.Fatalf("second exact metadata = %+v", entries[1])
	}
	if got, err := os.ReadFile(filepath.Join(root, "a", "same.bin")); err != nil || string(got) != string(first) {
		t.Fatalf("BuildPayloadManifest mutated bytes: %x err=%v", got, err)
	}
}

func TestBuildPayloadManifestRejectsLinks(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	writeCaptureFile(t, outside, []byte("outside"))
	requireCaptureSymlink(t, outside, filepath.Join(root, "linked.json"))
	if _, err := BuildPayloadManifest(root); IntegrityDiagnosticCode(err) != IntegrityLinkUnsupported {
		t.Fatalf("link build error = %T %v code=%q", err, err, IntegrityDiagnosticCode(err))
	}
}

func TestBuildPayloadManifestRejectsRelativeRoot(t *testing.T) {
	if _, err := BuildPayloadManifest("."); IntegrityDiagnosticCode(err) != IntegrityUnsafePath {
		t.Fatalf("relative root error = %T %v code=%q", err, err, IntegrityDiagnosticCode(err))
	}
}

func TestBuildPayloadManifestReturnsNonNilEmptyArray(t *testing.T) {
	entries, err := BuildPayloadManifest(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if entries == nil || len(entries) != 0 {
		t.Fatalf("empty manifest = %#v, want non-nil empty slice", entries)
	}
}

func TestPortablePayloadPathSetRejectsCaseFoldedAndParentChildCollisions(t *testing.T) {
	tests := [][]string{
		{"prefs.json", "PREFS.JSON"},
		{"prefs", "prefs/theme.json"},
	}
	for _, paths := range tests {
		if err := validatePortablePayloadPathSet(paths); IntegrityDiagnosticCode(err) != IntegrityPayloadDuplicate {
			t.Fatalf("paths %q collision = %T %v code=%q", paths, err, err, IntegrityDiagnosticCode(err))
		}
	}
}

func TestVerifyPayloadManifestAcceptsExactClosedWorldPayload(t *testing.T) {
	root := t.TempDir()
	data := []byte("exact\r\nbytes")
	writeCaptureFile(t, filepath.Join(root, "nested", "prefs.json"), data)
	entries, err := BuildPayloadManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyPayloadManifest(root, entries); err != nil {
		t.Fatalf("VerifyPayloadManifest: %v", err)
	}
	if got, err := os.ReadFile(filepath.Join(root, "nested", "prefs.json")); err != nil || string(got) != string(data) {
		t.Fatalf("verification mutated source payload: %q err=%v", got, err)
	}
}

func TestVerifyPayloadManifestRejectsMissingExtraTamperedAndSizeMismatch(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
		code   string
	}{
		{"missing", func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "prefs.json")); err != nil {
				t.Fatal(err)
			}
		}, IntegrityPayloadMissing},
		{"extra", func(t *testing.T, root string) {
			writeCaptureFile(t, filepath.Join(root, "extra.json"), []byte("extra"))
		}, IntegrityPayloadExtra},
		{"tampered same size", func(t *testing.T, root string) {
			writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("other"))
		}, IntegrityPayloadHashMismatch},
		{"size mismatch", func(t *testing.T, root string) {
			writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("longer"))
		}, IntegrityPayloadSizeMismatch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeCaptureFile(t, filepath.Join(root, "prefs.json"), []byte("value"))
			entries, err := BuildPayloadManifest(root)
			if err != nil {
				t.Fatal(err)
			}
			tt.mutate(t, root)
			err = VerifyPayloadManifest(root, entries)
			if IntegrityDiagnosticCode(err) != tt.code {
				t.Fatalf("integrity code = %q, want %q: %T %v", IntegrityDiagnosticCode(err), tt.code, err, err)
			}
			var integrityErr *IntegrityError
			if !errors.As(err, &integrityErr) {
				t.Fatalf("integrity error type = %T", err)
			}
		})
	}
}

func TestVerifyPayloadManifestRejectsDuplicateAndUnsafeEntries(t *testing.T) {
	root := t.TempDir()
	data := []byte("value")
	writeCaptureFile(t, filepath.Join(root, "prefs.json"), data)
	entry := manifest.PayloadManifestEntry{RelativePath: "prefs.json", Size: int64(len(data)), SHA256: testSHA256(data)}
	tests := []struct {
		name    string
		entries []manifest.PayloadManifestEntry
		code    string
	}{
		{"case folded duplicate", []manifest.PayloadManifestEntry{entry, {RelativePath: "PREFS.JSON", Size: entry.Size, SHA256: entry.SHA256}}, IntegrityPayloadDuplicate},
		{"slash normalized duplicate", []manifest.PayloadManifestEntry{entry, {RelativePath: `.\prefs.json`, Size: entry.Size, SHA256: entry.SHA256}}, IntegrityPayloadDuplicate},
		{"traversal", []manifest.PayloadManifestEntry{{RelativePath: "../prefs.json", Size: entry.Size, SHA256: entry.SHA256}}, IntegrityUnsafePath},
		{"absolute unix", []manifest.PayloadManifestEntry{{RelativePath: "/prefs.json", Size: entry.Size, SHA256: entry.SHA256}}, IntegrityUnsafePath},
		{"absolute windows", []manifest.PayloadManifestEntry{{RelativePath: `C:\prefs.json`, Size: entry.Size, SHA256: entry.SHA256}}, IntegrityUnsafePath},
		{"invalid hash", []manifest.PayloadManifestEntry{{RelativePath: "prefs.json", Size: entry.Size, SHA256: strings.Repeat("A", 64)}}, IntegrityPayloadInvalidEntry},
		{"negative size", []manifest.PayloadManifestEntry{{RelativePath: "prefs.json", Size: -1, SHA256: entry.SHA256}}, IntegrityPayloadInvalidEntry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := VerifyPayloadManifest(root, tt.entries); IntegrityDiagnosticCode(err) != tt.code {
				t.Fatalf("integrity code = %q, want %q: %v", IntegrityDiagnosticCode(err), tt.code, err)
			}
		})
	}
}

func TestVerifyPayloadManifestRejectsPayloadLink(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.json")
	data := []byte("outside")
	writeCaptureFile(t, outside, data)
	requireCaptureSymlink(t, outside, filepath.Join(root, "prefs.json"))
	entries := []manifest.PayloadManifestEntry{{RelativePath: "prefs.json", Size: int64(len(data)), SHA256: testSHA256(data)}}
	if err := VerifyPayloadManifest(root, entries); IntegrityDiagnosticCode(err) != IntegrityLinkUnsupported {
		t.Fatalf("payload link error = %T %v code=%q", err, err, IntegrityDiagnosticCode(err))
	}
}

func testSHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
