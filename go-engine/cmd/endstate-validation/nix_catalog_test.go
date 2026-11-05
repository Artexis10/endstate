// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNixMappingsRefusesEmptyAdvertisedCatalog(t *testing.T) {
	// A missing catalog must not become successful zero-mapping release evidence.
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "catalog", "packages"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := runCLICommands([]string{"nix-mappings", "--repo", repo, "--system", "aarch64-linux"}, &stdout, &stderr, nil)
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if code == 0 || !strings.Contains(stdout.String(), "empty package catalog") {
		t.Fatalf("empty advertised catalog was not identified: exit=%d result=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
