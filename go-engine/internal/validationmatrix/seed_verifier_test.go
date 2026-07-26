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

func TestVerifyHashBoundSeedRechecksCurrentBytes(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	seedPath := filepath.Join(directory, "seed.ps1")
	if err := os.WriteFile(seedPath, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte("original"))
	record := ValidationRecord{FilePath: filepath.Join(directory, "validation.jsonc"), Live: LivePolicy{Seed: "seed.ps1", Trust: &TrustHashes{SeedSHA256: hex.EncodeToString(digest[:])}}}
	if err := VerifyHashBoundSeed(record); err != nil {
		t.Fatalf("initial verification: %v", err)
	}
	if err := os.WriteFile(seedPath, []byte("swapped"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyHashBoundSeed(record); err == nil {
		t.Fatal("verification accepted swapped seed")
	}
}
