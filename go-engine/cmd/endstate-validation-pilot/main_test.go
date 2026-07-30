// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/validationpilot"
)

func TestV1CommandsRejectMissingRequiredFlags(t *testing.T) {
	for _, args := range [][]string{{"validate-v1"}, {"run-v1-lane"}, {"aggregate-v1"}} {
		if code := run(args); code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
	}
}

func TestDetectorRejectsAuthorityBeforeRunningOrWritingEvidence(t *testing.T) {
	root := t.TempDir()
	output := filepath.Join(root, "evidence.json")
	steps := []string{}
	ranDetector := false

	originalLoad, originalAuthority, originalDetector := loadV0Manifest, validateV0DetectorAuthority, runV0Detector
	t.Cleanup(func() {
		loadV0Manifest = originalLoad
		validateV0DetectorAuthority = originalAuthority
		runV0Detector = originalDetector
	})
	loadV0Manifest = func(path string) (validationpilot.Manifest, error) {
		steps = append(steps, "load")
		want := filepath.Join(root, "validation", "ci-efficacy", "pilot-v0", "manifest.json")
		if path != want {
			t.Errorf("manifest path = %q, want %q", path, want)
		}
		return validationpilot.Manifest{}, nil
	}
	validateV0DetectorAuthority = func(gotRoot string, _ validationpilot.Manifest) error {
		steps = append(steps, "authority")
		if gotRoot != root {
			t.Errorf("authority root = %q, want %q", gotRoot, root)
		}
		return errors.New("detector authority rejected")
	}
	runV0Detector = func(context.Context, validationpilot.DetectorRequest) (validationpilot.Attempt, error) {
		ranDetector = true
		return validationpilot.Attempt{Status: "passed"}, nil
	}

	if code := run([]string{"detector", "--catalog", "--engine", "engine", "--repo", root, "--commit", validationpilot.DetectorRef, "--output", output}); code != 1 {
		t.Fatalf("run(detector) = %d, want authority rejection", code)
	}
	if got := strings.Join(steps, ","); got != "load,authority" {
		t.Fatalf("steps = %q, want load,authority", got)
	}
	if ranDetector {
		t.Fatal("detector ran after authority rejection")
	}
	if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("evidence output err = %v, want not exist", err)
	}
}

func TestValidateCorpusRunsAuthorityBeforeCorpusValidation(t *testing.T) {
	root := t.TempDir()
	steps := []string{}

	originalLoad, originalAuthority, originalCorpus := loadV0Manifest, validateV0DetectorAuthority, validateV0Corpus
	t.Cleanup(func() {
		loadV0Manifest = originalLoad
		validateV0DetectorAuthority = originalAuthority
		validateV0Corpus = originalCorpus
	})
	loadV0Manifest = func(string) (validationpilot.Manifest, error) {
		steps = append(steps, "load")
		return validationpilot.Manifest{}, nil
	}
	validateV0DetectorAuthority = func(string, validationpilot.Manifest) error {
		steps = append(steps, "authority")
		return nil
	}
	validateV0Corpus = func(string, validationpilot.Manifest) error {
		if got := strings.Join(steps, ","); got != "load,authority" {
			t.Errorf("steps before corpus = %q, want load,authority", got)
		}
		steps = append(steps, "corpus")
		return errors.New("corpus rejected")
	}

	if code := run([]string{"validate-corpus", root}); code != 1 {
		t.Fatalf("run(validate-corpus) = %d, want corpus rejection", code)
	}
	if got := strings.Join(steps, ","); got != "load,authority,corpus" {
		t.Fatalf("steps = %q, want load,authority,corpus", got)
	}
}
