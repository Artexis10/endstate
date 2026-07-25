// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestExtractBundleWithValidationUsesOwnedRootAndRejectsZipSlip(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	input := filepath.Join(context.Root(), "state", "capture.zip")
	writeValidationExtractZip(t, input, map[string]string{
		"manifest.jsonc":               `{"version":1,"apps":[]}`,
		"configs/example/settings.txt": "captured-sentinel",
	})

	manifestPath, err := ExtractBundleWithValidation(input, context)
	if err != nil {
		t.Fatal(err)
	}
	if err := context.ValidateSandboxPath(manifestPath); err != nil {
		t.Fatalf("extracted manifest escaped authority: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(filepath.Dir(manifestPath), "configs", "example", "settings.txt"))
	if err != nil || string(payload) != "captured-sentinel" {
		t.Fatalf("payload = %q err=%v", payload, err)
	}
	if err := RemoveExtractedBundleWithValidation(filepath.Dir(manifestPath), context); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Dir(manifestPath)); !os.IsNotExist(err) {
		t.Fatalf("extraction root survived cleanup: %v", err)
	}

	escape := filepath.Join(context.Root(), "state", "escape.txt")
	malicious := filepath.Join(context.Root(), "state", "malicious.zip")
	writeValidationExtractZip(t, malicious, map[string]string{
		"manifest.jsonc": `{"version":1,"apps":[]}`,
		"../escape.txt":  "must-not-write",
	})
	if _, err := ExtractBundleWithValidation(malicious, context); err == nil {
		t.Fatal("zip slip unexpectedly succeeded")
	}
	if _, err := os.Lstat(escape); !os.IsNotExist(err) {
		t.Fatalf("zip slip wrote outside extraction root: %v", err)
	}
}

func TestValidationExtractionBudgetRejectsArchiveAndStreamOverflow(t *testing.T) {
	if err := validateValidationExtractionBudget(make([]*zip.File, validationExtractionMaxEntries+1)); !errors.Is(err, validationmode.ErrGuardBudget) {
		t.Fatalf("entry overflow = %v, want ErrGuardBudget", err)
	}
	oversized := []*zip.File{{FileHeader: zip.FileHeader{
		Name: "oversized.bin", UncompressedSize64: validationExtractionMaxBytes + 1,
	}}}
	if err := validateValidationExtractionBudget(oversized); !errors.Is(err, validationmode.ErrGuardBudget) {
		t.Fatalf("declared byte overflow = %v, want ErrGuardBudget", err)
	}
	var output strings.Builder
	written, err := copyValidationArchiveMember(&output, strings.NewReader("12345"), 4)
	if written != 5 || !errors.Is(err, validationmode.ErrGuardBudget) {
		t.Fatalf("stream overflow = (%d, %v), want (5, ErrGuardBudget)", written, err)
	}
}

func writeValidationExtractZip(t *testing.T, output string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, value := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(value)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}
