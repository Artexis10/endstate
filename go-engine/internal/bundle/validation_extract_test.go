// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"archive/zip"
	"errors"
	"fmt"
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

func TestExtractBundleWithValidationRejectsDuplicateDestinationPlan(t *testing.T) {
	tests := []struct {
		name    string
		entries []validationZipEntry
	}{
		{
			name: "exact duplicate",
			entries: []validationZipEntry{
				{name: "manifest.jsonc", value: `{"version":1,"apps":[]}`},
				{name: "manifest.jsonc", value: `{"version":1,"apps":[]}`},
			},
		},
		{
			name: "slash alias",
			entries: []validationZipEntry{
				{name: "manifest.jsonc", value: `{"version":1,"apps":[]}`},
				{name: "configs/example.txt", value: "first"},
				{name: `configs\example.txt`, value: "second"},
			},
		},
		{
			name: "windows case alias",
			entries: []validationZipEntry{
				{name: "manifest.jsonc", value: `{"version":1,"apps":[]}`},
				{name: "Configs/Example.txt", value: "first"},
				{name: "configs/example.TXT", value: "second"},
			},
		},
		{
			name: "ancestor file before child",
			entries: []validationZipEntry{
				{name: "manifest.jsonc", value: `{"version":1,"apps":[]}`},
				{name: "configs", value: "file"},
				{name: "configs/example.txt", value: "child"},
			},
		},
		{
			name: "child before ancestor file",
			entries: []validationZipEntry{
				{name: "manifest.jsonc", value: `{"version":1,"apps":[]}`},
				{name: "configs/example.txt", value: "child"},
				{name: "configs", value: "file"},
			},
		},
		{
			name: "file directory conflict",
			entries: []validationZipEntry{
				{name: "manifest.jsonc", value: `{"version":1,"apps":[]}`},
				{name: "configs", value: "file"},
				{name: "configs/", directory: true},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			context := activeBundleValidationContext(t, "apps.example")
			input := filepath.Join(context.Root(), "state", "capture.zip")
			writeOrderedValidationExtractZip(t, input, test.entries)

			if _, err := ExtractBundleWithValidation(input, context); err == nil {
				t.Fatal("ambiguous extraction plan unexpectedly succeeded")
			}
			parent := filepath.Join(context.Root(), "state", "extractions")
			children, err := os.ReadDir(parent)
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(children) != 0 {
				t.Fatalf("failed preflight left extraction roots: %v", children)
			}
		})
	}
}

func TestRemoveExtractedBundleWithValidationRejectsUnownedRootShapes(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	parent := filepath.Join(context.Root(), "state", "extractions")
	validRoot := filepath.Join(parent, "bundle-0123456789abcdef0123456789abcdef")
	tests := []struct {
		name string
		path string
	}{
		{name: "arbitrary sibling", path: filepath.Join(parent, "do-not-delete")},
		{name: "uppercase identity", path: filepath.Join(parent, "bundle-0123456789ABCDEF0123456789abcdef")},
		{name: "short identity", path: filepath.Join(parent, "bundle-0123456789abcdef")},
		{name: "nested descendant", path: filepath.Join(validRoot, "nested")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := os.MkdirAll(test.path, 0o700); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(test.path, "marker.txt")
			if err := os.WriteFile(marker, []byte("must-survive"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := RemoveExtractedBundleWithValidation(test.path, context); err == nil {
				t.Fatal("unowned extraction shape unexpectedly removed")
			}
			if payload, err := os.ReadFile(marker); err != nil || string(payload) != "must-survive" {
				t.Fatalf("rejected cleanup changed marker: %q err=%v", payload, err)
			}
		})
	}
}

func TestRemoveExtractedBundleWithValidationValidatesMembersBeforeRemoval(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	parent := filepath.Join(context.Root(), "state", "extractions")
	extractRoot := filepath.Join(parent, "bundle-0123456789abcdef0123456789abcdef")
	if err := os.MkdirAll(filepath.Join(extractRoot, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extractRoot, "nested", "payload.txt"), []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(context.Root(), "state", "outside.txt")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(extractRoot, "nested", "linked.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := RemoveExtractedBundleWithValidation(extractRoot, context); err == nil {
		t.Fatal("cleanup unexpectedly accepted linked member")
	}
	if payload, err := os.ReadFile(filepath.Join(extractRoot, "nested", "payload.txt")); err != nil || string(payload) != "payload" {
		t.Fatalf("failed cleanup partially removed extraction: %q err=%v", payload, err)
	}
	if payload, err := os.ReadFile(outside); err != nil || string(payload) != "outside" {
		t.Fatalf("failed cleanup changed outside target: %q err=%v", payload, err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := RemoveExtractedBundleWithValidation(extractRoot, context); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(extractRoot); !os.IsNotExist(err) {
		t.Fatalf("validated extraction root survived cleanup: %v", err)
	}
}

func TestRemoveValidationExtractionPlanRejectsMemberTypeSwap(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	extractRoot := filepath.Join(context.Root(), "state", "extractions", "bundle-0123456789abcdef0123456789abcdef")
	swapped := filepath.Join(extractRoot, "a-swapped.txt")
	survivor := filepath.Join(extractRoot, "z-survivor.txt")
	if err := os.MkdirAll(extractRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(swapped, []byte("swap-me"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(survivor, []byte("must-survive"), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := planValidationExtractionRemoval(extractRoot, context)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan) != 3 || plan[0].path != swapped || plan[0].directory {
		t.Fatalf("unexpected regular-member removal plan: %+v", plan)
	}
	if err := os.Remove(swapped); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(swapped, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := removeValidationExtractionPlan(plan, context); err == nil {
		t.Fatal("cleanup unexpectedly accepted planned file replaced by directory")
	}
	if info, err := os.Lstat(swapped); err != nil || !info.IsDir() {
		t.Fatalf("rejected cleanup removed swapped member: info=%v err=%v", info, err)
	}
	if payload, err := os.ReadFile(survivor); err != nil || string(payload) != "must-survive" {
		t.Fatalf("rejected cleanup partially removed sibling: %q err=%v", payload, err)
	}
}

func TestRemoveValidationExtractionPlanAllowsDisappearedMember(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	extractRoot := filepath.Join(context.Root(), "state", "extractions", "bundle-0123456789abcdef0123456789abcdef")
	disappeared := filepath.Join(extractRoot, "a-disappeared.txt")
	survivor := filepath.Join(extractRoot, "z-survivor.txt")
	if err := os.MkdirAll(extractRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{disappeared, survivor} {
		if err := os.WriteFile(path, []byte("payload"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := planValidationExtractionRemoval(extractRoot, context)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(disappeared); err != nil {
		t.Fatal(err)
	}

	if err := removeValidationExtractionPlan(plan, context); err != nil {
		t.Fatalf("cleanup rejected already-disappeared planned member: %v", err)
	}
	if _, err := os.Lstat(extractRoot); !os.IsNotExist(err) {
		t.Fatalf("validated extraction root survived cleanup: %v", err)
	}
}

func TestRemoveValidationExtractionPlanRejectsLinkSwap(t *testing.T) {
	context := activeBundleValidationContext(t, "apps.example")
	extractRoot := filepath.Join(context.Root(), "state", "extractions", "bundle-0123456789abcdef0123456789abcdef")
	swapped := filepath.Join(extractRoot, "a-swapped.txt")
	survivor := filepath.Join(extractRoot, "z-survivor.txt")
	outside := filepath.Join(context.Root(), "state", "outside.txt")
	if err := os.MkdirAll(extractRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	for path, value := range map[string]string{
		swapped:  "swap-me",
		survivor: "must-survive",
		outside:  "outside",
	} {
		if err := os.WriteFile(path, []byte(value), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	plan, err := planValidationExtractionRemoval(extractRoot, context)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(swapped); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, swapped); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	if err := removeValidationExtractionPlan(plan, context); err == nil {
		t.Fatal("cleanup unexpectedly accepted link-swapped planned member")
	}
	if _, err := os.Lstat(swapped); err != nil {
		t.Fatalf("rejected cleanup removed swapped link: %v", err)
	}
	if payload, err := os.ReadFile(survivor); err != nil || string(payload) != "must-survive" {
		t.Fatalf("rejected cleanup partially removed sibling: %q err=%v", payload, err)
	}
	if payload, err := os.ReadFile(outside); err != nil || string(payload) != "outside" {
		t.Fatalf("rejected cleanup changed link target: %q err=%v", payload, err)
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

type validationZipEntry struct {
	name      string
	value     string
	directory bool
}

func writeOrderedValidationExtractZip(t *testing.T, output string, entries []validationZipEntry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(output)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, item := range entries {
		header := &zip.FileHeader{Name: item.name, Method: zip.Store}
		if item.directory {
			header.SetMode(os.ModeDir | 0o755)
		}
		entry, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if item.directory {
			continue
		}
		if _, err := fmt.Fprint(entry, item.value); err != nil {
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
