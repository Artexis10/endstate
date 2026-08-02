// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package safepath

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestHashRegularFileHandlesMaximumBudgetWithoutOverflow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "value")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, size, err := HashRegularFile(path, math.MaxInt64)
	if err != nil {
		t.Fatal(err)
	}
	if size != 5 {
		t.Fatalf("size = %d, want 5", size)
	}
}

func TestHashRegularFileRejectsByteLimitBeforeRead(t *testing.T) {
	path := filepath.Join(t.TempDir(), "value")
	if err := os.WriteFile(path, []byte("value"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := HashRegularFile(path, 4); !errors.Is(err, ErrByteLimit) {
		t.Fatalf("HashRegularFile error = %v, want ErrByteLimit", err)
	}
}

func TestVerifyHashedFileReadRejectsTruncationAndMetadataChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "value")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, 2); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyHashedFileRead(before, after, before.Size()); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("truncate error = %v, want ErrSourceChanged", err)
	}
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	future := before.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	after, err = os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyHashedFileRead(before, after, before.Size()); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("metadata error = %v, want ErrSourceChanged", err)
	}
	if err := verifyHashedFileRead(before, before, before.Size()-1); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("short read error = %v, want ErrSourceChanged", err)
	}
}

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

func TestReadRegularFileRejectsSourceSwappedToLinkAfterPreflight(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(source, []byte("inside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
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

	_, _, err := ReadRegularFile(source)
	if !errors.Is(err, ErrLinkUnsupported) {
		t.Fatalf("ReadRegularFile error = %v, want ErrLinkUnsupported", err)
	}
}
