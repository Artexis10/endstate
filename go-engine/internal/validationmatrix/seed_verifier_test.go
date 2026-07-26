// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmatrix

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestReadHashBoundSeedReturnsVerifiedDefensiveBytes(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	seedPath := filepath.Join(directory, "seed.ps1")
	if err := os.WriteFile(seedPath, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("original"))
	record := ValidationRecord{FilePath: filepath.Join(directory, "validation.jsonc"), Live: LivePolicy{Seed: "seed.ps1", Trust: &TrustHashes{SeedSHA256: hex.EncodeToString(digest[:])}}}
	seed, err := ReadHashBoundSeed(record)
	if err != nil {
		t.Fatalf("initial verification: %v", err)
	}
	if string(seed) != "original" {
		t.Fatalf("seed bytes = %q", seed)
	}
	if err := os.WriteFile(seedPath, []byte("swapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	if string(seed) != "original" {
		t.Fatalf("returned seed changed after source mutation: %q", seed)
	}
	if _, err := ReadHashBoundSeed(record); err == nil {
		t.Fatal("verification accepted swapped seed")
	}
}

func TestReadHashBoundSeedAcceptsRelativeModuleDirectory(t *testing.T) {
	directory, err := os.MkdirTemp(".", ".relative-seed-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)

	seedPath := filepath.Join(directory, "seed.ps1")
	if err := os.WriteFile(seedPath, []byte("relative"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("relative"))
	record := ValidationRecord{FilePath: filepath.Join(directory, "validation.jsonc"), Live: LivePolicy{Seed: "seed.ps1", Trust: &TrustHashes{SeedSHA256: hex.EncodeToString(digest[:])}}}
	if filepath.IsAbs(record.FilePath) {
		t.Fatal("test record path must stay relative")
	}
	if _, err := ReadHashBoundSeed(record); err != nil {
		t.Fatalf("relative module directory verification: %v", err)
	}
}

func TestReadHashBoundSeedRejectsLinksAndOversizedSeeds(t *testing.T) {
	directory := t.TempDir()
	seedPath := filepath.Join(directory, "seed.ps1")
	oversized := make([]byte, maxHashBoundSeedBytes+1)
	if err := os.WriteFile(seedPath, oversized, 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(oversized)
	record := ValidationRecord{FilePath: filepath.Join(directory, "validation.jsonc"), Live: LivePolicy{Seed: "seed.ps1", Trust: &TrustHashes{SeedSHA256: hex.EncodeToString(digest[:])}}}
	if _, err := ReadHashBoundSeed(record); err == nil {
		t.Fatal("verification accepted an oversized seed")
	}

	external := filepath.Join(t.TempDir(), "external.ps1")
	if err := os.WriteFile(external, []byte("linked"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(seedPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, seedPath); err != nil {
		t.Skipf("host cannot create test link: %v", err)
	}
	linkedDigest := sha256.Sum256([]byte("linked"))
	record.Live.Trust.SeedSHA256 = hex.EncodeToString(linkedDigest[:])
	if _, err := ReadHashBoundSeed(record); err == nil {
		t.Fatal("verification accepted a linked seed")
	}
}
