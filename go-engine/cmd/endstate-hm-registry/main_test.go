// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

func TestRegistryProbeTimeoutPreservesFileDespiteWriteConsent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux release-tool subprocess boundary")
	}
	inputs, err := releaseinputs.Load()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	git := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' '%s'\n", inputs.HomeManager.Revision)
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(git), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte("#!/bin/sh\nexec /bin/sleep 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	source := filepath.Join(root, "source")
	declaration := filepath.Join(source, "modules", "programs", "ripgrep.nix")
	if err := os.MkdirAll(filepath.Dir(declaration), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(declaration, []byte("{ }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	docsPath := filepath.Join(root, "options.json")
	docs := []byte(`{"programs.ripgrep.arguments":{"type":"list of string","declarations":[{"name":"<home-manager/modules/programs/ripgrep.nix>"}]}}`)
	if err := os.WriteFile(docsPath, docs, 0600); err != nil {
		t.Fatal(err)
	}
	registry := hmregistry.Registry{Entries: []hmregistry.Entry{{
		ID: "ripgrep", Program: "ripgrep", DisplayName: "ripgrep",
		PackageID: "ripgrep", ModuleID: "apps.ripgrep",
		Disposition: hmregistry.FileRoundTrip, Codec: "bounded-regular-file-v1",
		Probe:   &hmregistry.Probe{Values: map[string]any{"programs.ripgrep.arguments": []any{"--hidden"}}},
		Options: []hmregistry.Option{{Name: "programs.ripgrep.arguments"}},
	}}}
	original, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "registry.jsonc")
	if err := os.WriteFile(registryPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	err = run(context.Background(), registryPath, docsPath, source, "x86_64-linux", 25*time.Millisecond, true, true)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("refresh error = %v; want evaluation deadline", err)
	}
	after, err := os.ReadFile(registryPath)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("timed-out refresh changed registry: %v", err)
	}
	leftovers, err := filepath.Glob(filepath.Join(root, ".registry-*.tmp"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("timed-out refresh staged publication: %v, %v", leftovers, err)
	}
}

func TestRegistryRejectsUnboundedProbeBeforeReadingFiles(t *testing.T) {
	err := run(context.Background(), "missing", "missing", "missing", "x86_64-linux", 0, false, false)
	if err == nil || !strings.Contains(err.Error(), "--probe-timeout must be positive") {
		t.Fatalf("unbounded probe accepted: %v", err)
	}
}
