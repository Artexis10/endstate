// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationpilot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestV1ControllerRejectsUnsafePostFreezeDiff(t *testing.T) {
	manifest, _ := validV1Proof()
	validator := V1AuthorityValidator{
		Resolve: func(reference string) (V1Reference, error) {
			return manifest.Authorities.Freeze, nil
		},
		IsAncestor: func(ancestor, descendant string) (bool, error) { return true, nil },
		ChangedPaths: func(from, to string) ([]string, error) {
			if to == manifest.Authorities.Corpus.Commit {
				return []string{"go-engine/internal/validationpilot/controller.go"}, nil
			}
			return []string{V1CorpusRoot + "/manifest.json"}, nil
		},
	}
	if err := validator.Validate(manifest); err == nil {
		t.Fatal("Validate() accepted a post-freeze controller change")
	}
}

func TestAggregateV1RejectsForeignEvidenceInventory(t *testing.T) {
	manifest, evidence := validV1Proof()
	root := t.TempDir()
	for _, leaf := range v1EvidenceInventory {
		if err := os.WriteFile(filepath.Join(root, leaf), mustV1Evidence(t, evidence), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "foreign.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AggregateV1Evidence(manifest, manifest.Authorities.Dispatch.Commit, root); !errors.Is(err, ErrV1EvidenceInventory) {
		t.Fatalf("AggregateV1Evidence() error = %v, want foreign inventory rejection", err)
	}
}

func TestWriteV1AggregateCreatesOneNewCanonicalOutput(t *testing.T) {
	manifest, evidence := validV1Proof()
	aggregate, err := ClassifyV1(manifest, evidence)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "aggregate.json")
	if err := WriteV1AggregateNew(path, aggregate); err != nil {
		t.Fatal(err)
	}
	if err := WriteV1AggregateNew(path, aggregate); err == nil {
		t.Fatal("WriteV1AggregateNew() overwrote an existing aggregate")
	}
	if _, err := DecodeV1Aggregate(mustReadV1(t, path)); err != nil {
		t.Fatalf("DecodeV1Aggregate() = %v", err)
	}
}

func TestV1ComparatorRunsVetThenTestWithStrippedEnvironment(t *testing.T) {
	var commands []V1ChildCommand
	runner := func(command V1ChildCommand) V1ChildResult {
		commands = append(commands, command)
		return V1ChildResult{}
	}
	if result := RunV1Comparator(runner, []string{"PATH=test", "GITHUB_TOKEN=secret", "GH_TOKEN=secret", "UNRELATED=value"}); result.Infrastructure != "" {
		t.Fatalf("RunV1Comparator() = %#v", result)
	}
	if len(commands) != 2 || commands[0].Name != "go" || commands[0].Args[0] != "vet" || commands[1].Args[0] != "test" {
		t.Fatalf("comparator commands = %#v", commands)
	}
	for _, command := range commands {
		for _, value := range command.Env {
			if value == "GITHUB_TOKEN=secret" || value == "GH_TOKEN=secret" || value == "UNRELATED=value" {
				t.Fatalf("child environment leaked %q", value)
			}
		}
	}
}

func mustV1Evidence(t *testing.T, evidence V1Evidence) []byte {
	t.Helper()
	raw, _, err := EncodeV1Evidence(evidence)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func mustReadV1(t *testing.T, path string) []byte {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
