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

func TestDetectorCommandsAreDecommissionedBeforeOutputHandling(t *testing.T) {
	root := t.TempDir()
	ranDetector := false

	originalDetector := runV0Detector
	t.Cleanup(func() {
		runV0Detector = originalDetector
	})
	runV0Detector = func(context.Context, validationpilot.DetectorRequest) (validationpilot.Attempt, error) {
		ranDetector = true
		return validationpilot.Attempt{Status: "passed"}, nil
	}

	for _, test := range []struct {
		name    string
		command func([]string) int
	}{
		{name: "top-level", command: run},
		{name: "legacy", command: runV0},
	} {
		t.Run(test.name, func(t *testing.T) {
			output := filepath.Join(root, test.name+".json")
			if code := test.command([]string{"detector", "--output", output}); code != 1 {
				t.Fatalf("detector command = %d, want decommissioned rejection", code)
			}
			if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("evidence output err = %v, want not exist", err)
			}
		})
	}
	if ranDetector {
		t.Fatal("detector ran after decommissioned command rejection")
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
