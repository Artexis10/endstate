// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/config"
)

func TestGlobSandboxPatternReturnsOnlyCanonicalContainedLinkFreeMatches(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "Outside")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	context := activeTestContext(t, "capture-glob")
	appData, _ := context.VirtualRoot("APPDATA")
	first := filepath.Join(appData, "Vendor", "One")
	second := filepath.Join(appData, "Vendor", "Two")
	for _, directory := range []string{first, second} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}

	got, err := context.GlobSandboxPattern(filepath.Join(appData, "Vendor", "*"))
	if err != nil {
		t.Fatalf("GlobSandboxPattern: %v", err)
	}
	want := []string{filepath.Clean(first), filepath.Clean(second)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("matches = %#v, want %#v", got, want)
	}

	if _, err := context.GlobSandboxPattern(filepath.Join(filepath.Dir(outside), "*")); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("outside glob error = %v, want ErrUnsafePath", err)
	}

	linked := filepath.Join(appData, "Vendor", "Linked")
	if err := os.Symlink(outside, linked); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := context.GlobSandboxPattern(filepath.Join(appData, "Vendor", "*")); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("linked glob error = %v, want ErrUnsafePath", err)
	}
}

func TestGlobSandboxPatternKeepsEscapedVirtualRootConfined(t *testing.T) {
	literalTemp := filepath.Join(t.TempDir(), "validation[set]{literal}")
	if err := os.MkdirAll(literalTemp, 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(name, literalTemp)
	}
	context := activeTestContext(t, "capture-literal-glob")
	appData, _ := context.VirtualRoot("APPDATA")
	inside := filepath.Join(appData, "Vendor", "App 1")
	sibling := filepath.Join(appData+"-sibling", "Vendor", "App 9")
	for _, directory := range []string{inside, sibling} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	pattern := filepath.Join(config.QuoteGlobLiteral(appData), "Vendor", "App ?")
	got, err := context.GlobSandboxPattern(pattern)
	if err != nil || !reflect.DeepEqual(got, []string{inside}) {
		t.Fatalf("escaped literal root lost its contained suffix match: %v, %v", got, err)
	}
	outsidePattern := filepath.Join(config.QuoteGlobLiteral(appData+"-sibling"), "Vendor", "App ?")
	if _, err := context.GlobSandboxPattern(outsidePattern); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("escaped sibling escaped confinement: %v", err)
	}
	linked := filepath.Join(appData, "Vendor", "App 2")
	if err := os.Symlink(filepath.Dir(sibling), linked); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := context.GlobSandboxPattern(pattern); !errors.Is(err, ErrUnsafePath) {
		t.Fatalf("escaped glob accepted linked match: %v", err)
	}
}
