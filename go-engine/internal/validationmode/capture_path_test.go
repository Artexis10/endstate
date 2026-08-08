// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
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
