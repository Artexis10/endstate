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
	"unicode/utf8"

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

func TestRegistryCheckEmitsBoundedDeterministicDriftDiff(t *testing.T) {
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
	if err := os.WriteFile(filepath.Join(bin, "nix"), []byte("#!/bin/sh\nprintf '%s\\n' '[\"/home/endstate-probe/.config/ripgrep/new\"]'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	source := filepath.Join(root, "source")
	declaration := filepath.Join(source, "modules", "programs", "ripgrep-new.nix")
	if err := os.MkdirAll(filepath.Dir(declaration), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(declaration, []byte("{ }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	options := make(map[string]any)
	tracked := make([]hmregistry.Option, 0, 140)
	for index := 0; index < 140; index++ {
		name := fmt.Sprintf("programs.ripgrep.option%03d", index)
		oldDeclaration := "modules/programs/ripgrep-new.nix"
		if index == 0 {
			oldDeclaration = "modules/programs/ripgrep-old.nix"
		}
		tracked = append(tracked, hmregistry.Option{Name: name, Type: strings.Repeat("界", 100) + "old", Declarations: []string{oldDeclaration}})
		options[name] = map[string]any{"type": strings.Repeat("界", 100) + "new", "declarations": []map[string]string{{"name": "<home-manager/modules/programs/ripgrep-new.nix>"}}}
	}
	options["programs.ripgrep.enable"] = map[string]any{"type": "boolean"}
	docs, err := json.Marshal(options)
	if err != nil {
		t.Fatal(err)
	}
	docsPath := filepath.Join(root, "options.json")
	if err := os.WriteFile(docsPath, docs, 0600); err != nil {
		t.Fatal(err)
	}
	registry := hmregistry.Registry{Entries: []hmregistry.Entry{{
		ID: "ripgrep", Program: "ripgrep", DisplayName: "ripgrep",
		Disposition: hmregistry.FileRoundTrip, Codec: "bounded-regular-file-v1",
		Options: tracked, SourceHashes: map[string]string{"modules/programs/ripgrep-old.nix": strings.Repeat("0", 64)},
		Probe:   &hmregistry.Probe{Values: map[string]any{"programs.ripgrep.option000": true}, Targets: []string{"${xdg.config}/ripgrep/old"}},
		Targets: []hmregistry.Target{{Coordinate: "${xdg.config}/ripgrep/old"}},
	}}}
	original, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "registry.jsonc")
	if err := os.WriteFile(registryPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	first := run(context.Background(), registryPath, docsPath, source, "x86_64-linux", time.Second, false, false)
	second := run(context.Background(), registryPath, docsPath, source, "x86_64-linux", time.Second, false, false)
	if first == nil || second == nil || first.Error() != second.Error() {
		t.Fatalf("check did not return deterministic drift: %v; %v", first, second)
	}
	for _, evidence := range []string{"omitted", "shortened"} {
		if !strings.Contains(first.Error(), evidence) {
			t.Fatalf("drift report omitted %q: %v", evidence, first)
		}
	}
	var drift *reportedError
	if !errors.As(first, &drift) || !drift.report.Complete {
		t.Fatalf("check did not retain complete observations: %v", first)
	}
	for _, field := range []string{"option.type", "option.declarations", "sourceHashes", "probe.targets", "targets"} {
		found := false
		for _, change := range drift.report.Changes {
			if string(change.Field) == field {
				found = true
				if field == "option.type" && (change.Before.Value != strings.Repeat("界", 100)+"old" || change.After.Value != strings.Repeat("界", 100)+"new") {
					t.Fatal("display truncation replaced full comparison values")
				}
			}
		}
		if !found {
			t.Fatalf("full drift report omitted field %q", field)
		}
	}
	if strings.Count(first.Error(), "\nchange ") > 128 || !utf8.ValidString(first.Error()) || strings.Contains(first.Error(), strings.Repeat("界", 100)) {
		t.Fatalf("drift report exceeded display bounds: %v", first)
	}
	after, err := os.ReadFile(registryPath)
	if err != nil || !bytes.Equal(original, after) {
		t.Fatalf("check changed registry: %v", err)
	}
}

func TestRegistryWritesOnlyCompleteExplicitlyAcceptedCandidate(t *testing.T) {
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
	nixPath := filepath.Join(bin, "nix")
	success := []byte("#!/bin/sh\nprintf '%s\\n' '[\"/home/endstate-probe/.config/ripgrep/ripgreprc\"]'\n")
	if err := os.WriteFile(nixPath, success, 0700); err != nil {
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
	docs := []byte(`{"programs.ripgrep.enable":{"type":"boolean"},"programs.ripgrep.arguments":{"type":"list of string","declarations":[{"name":"<home-manager/modules/programs/ripgrep.nix>"}]}}`)
	docsPath := filepath.Join(root, "options.json")
	if err := os.WriteFile(docsPath, docs, 0600); err != nil {
		t.Fatal(err)
	}
	registry := &hmregistry.Registry{Entries: []hmregistry.Entry{{
		ID: "ripgrep", Program: "ripgrep", DisplayName: "ripgrep",
		Disposition: hmregistry.FileRoundTrip, Codec: "bounded-regular-file-v1",
		Options:           []hmregistry.Option{{Name: "programs.ripgrep.arguments", Type: "old type"}},
		Probe:             &hmregistry.Probe{Values: map[string]any{"programs.ripgrep.arguments": []any{"--hidden"}}},
		Targets:           []hmregistry.Target{{Coordinate: "${xdg.config}/ripgrep/ripgreprc", Optional: false}},
		ReviewFingerprint: strings.Repeat("0", 64),
	}}}
	original, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := filepath.Join(root, "registry.jsonc")
	if err := os.WriteFile(registryPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := run(context.Background(), registryPath, docsPath, source, "x86_64-linux", time.Second, false, true); err == nil {
		t.Fatal("accept-reviews alone blessed changed facts in check mode")
	}
	for _, failure := range []string{"missing option", "missing source", "failed probe"} {
		t.Run(failure, func(t *testing.T) {
			switch failure {
			case "missing option":
				if err := os.WriteFile(docsPath, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
			case "missing source":
				if err := os.Remove(declaration); err != nil {
					t.Fatal(err)
				}
			case "failed probe":
				if err := os.WriteFile(nixPath, []byte("#!/bin/sh\nprintf '%s' '"+strings.Repeat("界", 1000)+"' >&2\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			err := run(context.Background(), registryPath, docsPath, source, "x86_64-linux", time.Second, true, true)
			var incomplete *reportedError
			if !errors.As(err, &incomplete) || incomplete.report.Complete || incomplete.report.Stopped == nil || len(incomplete.report.Changes) == 0 {
				t.Fatalf("failed write lost partial observations: %v", err)
			}
			if len(err.Error()) > 6000 || !utf8.ValidString(err.Error()) {
				t.Fatalf("failed write has unbounded diagnostics: %v", err)
			}
			after, err := os.ReadFile(registryPath)
			if err != nil || !bytes.Equal(original, after) {
				t.Fatalf("incomplete refresh wrote registry: %v", err)
			}
			leftovers, err := filepath.Glob(filepath.Join(root, ".registry-*.tmp"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("incomplete refresh staged publication: %v; %v", leftovers, err)
			}
			if err := os.WriteFile(docsPath, docs, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(declaration, []byte("{ }\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(nixPath, success, 0700); err != nil {
				t.Fatal(err)
			}
		})
	}
	if err := run(context.Background(), registryPath, docsPath, source, "x86_64-linux", time.Second, true, true); err != nil {
		t.Fatal(err)
	}
	accepted, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := hmregistry.Parse(accepted, inputs)
	if err != nil || parsed.Entries[0].ReviewFingerprint == registry.Entries[0].ReviewFingerprint || parsed.Entries[0].Targets[0].Optional {
		t.Fatalf("explicit acceptance did not publish valid facts with the existing optional flag: %v", err)
	}
	if err := run(context.Background(), registryPath, docsPath, source, "x86_64-linux", time.Second, false, false); err != nil {
		t.Fatalf("accepted frozen registry failed ordinary check: %v", err)
	}
	parsed.Entries[0].ReviewFingerprint = strings.Repeat("0", 64)
	stale, err := json.Marshal(parsed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(registryPath, stale, 0600); err != nil {
		t.Fatal(err)
	}
	err = run(context.Background(), registryPath, docsPath, source, "x86_64-linux", time.Second, false, false)
	var invalid *reportedError
	if !errors.As(err, &invalid) || len(invalid.report.Changes) != 0 || !strings.Contains(err.Error(), "review is stale") {
		t.Fatalf("zero observed changes bypassed frozen review validation: %v", err)
	}
}
