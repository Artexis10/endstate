// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package provision

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func TestHomeFileExpectationsRejectInvalidEvidence(t *testing.T) {
	file := fmt.Sprintf(`{"target":"/home/example/settings","sha256":%q}`, strings.Repeat("a", 64))
	for name, data := range map[string]string{
		"missing list":       `{}`,
		"null list":          `{"files":null}`,
		"unknown field":      `{"files":[],"sources":[]}`,
		"unknown file field": `{"files":[{"target":"/a","sha256":"a","source":"/b"}]}`,
		"relative target":    fmt.Sprintf(`{"files":[{"target":"relative","sha256":%q}]}`, strings.Repeat("a", 64)),
		"unclean target":     fmt.Sprintf(`{"files":[{"target":"/home/example/../settings","sha256":%q}]}`, strings.Repeat("a", 64)),
		"invalid hash":       `{"files":[{"target":"/a","sha256":"wrong"}]}`,
		"repeated target":    `{"files":[` + file + `,` + file + `]}`,
		"trailing JSON":      `{"files":[]} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			var value HomeFileExpectations
			if err := json.Unmarshal([]byte(data), &value); err == nil {
				t.Fatal("invalid expectations were accepted")
			}
		})
	}
}

func TestHomeFileExpectationsPersistEmptyScopeAndLegacyAbsence(t *testing.T) {
	dir := t.TempDir()
	for _, ref := range []*HomeGenRef{{Generation: 1}, {Generation: 2, FileExpectations: &HomeFileExpectations{Files: []HomeFileExpectation{}}}} {
		if err := WriteTo(dir, &Generation{Backend: "nix", HomeManager: ref}); err != nil {
			t.Fatal(err)
		}
	}
	values, err := ListFrom(dir)
	if err != nil || len(values) != 2 || values[0].HomeManager.FileExpectations == nil || values[0].HomeManager.FileExpectations.Files == nil || values[1].HomeManager.FileExpectations != nil {
		t.Fatalf("history lost unavailable versus empty evidence: %+v %v", values, err)
	}
}
