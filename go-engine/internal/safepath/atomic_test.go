// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package safepath

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAtomicCopyFileRejectsSourceSwappedToLinkAfterPreflight(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	outside := filepath.Join(t.TempDir(), "outside")
	destination := filepath.Join(dir, "destination")
	if err := os.WriteFile(source, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(destination, []byte("unchanged"), 0o600); err != nil {
		t.Fatal(err)
	}
	probe := filepath.Join(dir, "probe-link")
	if err := os.Symlink(outside, probe); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("creating Windows reparse-point symlink requires local privilege: %v", err)
		}
		t.Fatal(err)
	}
	if err := os.Remove(probe); err != nil {
		t.Fatal(err)
	}

	previousOpen := openAtomicCopySource
	openAtomicCopySource = func(path string) (*os.File, error) {
		if err := os.Remove(path); err != nil {
			return nil, err
		}
		if err := os.Symlink(outside, path); err != nil {
			return nil, err
		}
		return os.Open(path)
	}
	defer func() { openAtomicCopySource = previousOpen }()

	err := AtomicCopyFile(source, destination, 0o600)
	if !errors.Is(err, ErrLinkUnsupported) {
		t.Fatalf("AtomicCopyFile error = %v, want ErrLinkUnsupported", err)
	}
	data, readErr := os.ReadFile(destination)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != "unchanged" {
		t.Fatalf("destination = %q, want unchanged", data)
	}
	entries, readDirErr := os.ReadDir(dir)
	if readDirErr != nil {
		t.Fatal(readDirErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".endstate-write-") {
			t.Fatalf("temporary file remains: %s", entry.Name())
		}
	}
}
