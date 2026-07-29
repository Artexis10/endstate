// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationaudit

import (
	"fmt"
	"strings"
	"testing"
)

const (
	testCommit = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testTree   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	testDigest = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
)

func TestDecodeCandidateSet(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "valid ordered queues", input: candidateSetJSON(), wantErr: nil},
		{name: "duplicate nested key", input: strings.Replace(candidateSetJSON(), `"class":"contract"`, `"class":"contract","class":"other"`, 1), wantErr: ErrDuplicateJSONKey},
		{name: "unknown field", input: strings.Replace(candidateSetJSON(), `"schemaVersion":1,`, `"schemaVersion":1,"extra":true,`, 1), wantErr: ErrUnknownField},
		{name: "trailing value", input: candidateSetJSON() + ` true`, wantErr: ErrTrailingJSONValue},
		{name: "unsupported schema", input: strings.Replace(candidateSetJSON(), `"schemaVersion":1`, `"schemaVersion":2`, 1), wantErr: ErrUnsupportedSchema},
		{name: "uppercase commit", input: strings.Replace(candidateSetJSON(), testCommit, strings.ToUpper(testCommit), 1), wantErr: ErrInvalidManifest},
		{name: "uppercase tree", input: strings.Replace(candidateSetJSON(), testTree, strings.ToUpper(testTree), 1), wantErr: ErrInvalidManifest},
		{name: "uppercase digest", input: strings.Replace(candidateSetJSON(), testDigest, strings.ToUpper(testDigest), 1), wantErr: ErrInvalidManifest},
		{name: "duplicate candidate id", input: strings.Replace(candidateSetJSON(), `"id":"engine-lifecycle-01"`, `"id":"module-data-01"`, 1), wantErr: ErrInvalidManifest},
		{name: "duplicate patch path", input: strings.Replace(candidateSetJSON(), `patches/engine-lifecycle-01.patch`, `patches/module-data-01.patch`, 1), wantErr: ErrInvalidManifest},
		{name: "duplicate patch digest", input: strings.Replace(candidateSetJSON(), digestFor("engine-lifecycle", 1), digestFor("module-data", 1), 1), wantErr: ErrInvalidManifest},
		{name: "unknown detector", input: strings.Replace(candidateSetJSON(), `"detector":"module-contract"`, `"detector":"unknown-detector"`, 1), wantErr: ErrInvalidManifest},
		{name: "absolute patch path", input: strings.Replace(candidateSetJSON(), `validation/ci-efficacy/v1/patches/module-data-01.patch`, `/candidate.patch`, 1), wantErr: ErrInvalidManifest},
		{name: "backslash touched path", input: strings.Replace(candidateSetJSON(), `go-engine/internal/example/example.go`, `go-engine\\internal\\example\\example.go`, 1), wantErr: ErrInvalidManifest},
		{name: "traversal patch path", input: strings.Replace(candidateSetJSON(), `validation/ci-efficacy/v1/patches/module-data-01.patch`, `../candidate.patch`, 1), wantErr: ErrInvalidManifest},
		{name: "windows device patch path", input: strings.Replace(candidateSetJSON(), `validation/ci-efficacy/v1/patches/module-data-01.patch`, `validation/ci-efficacy/v1/patches/CON.patch`, 1), wantErr: ErrInvalidManifest},
		{name: "windows device touched path", input: strings.Replace(candidateSetJSON(), `go-engine/internal/example/example.go`, `go-engine/internal/example/LPT9.txt`, 1), wantErr: ErrInvalidManifest},
		{name: "trailing dot touched path", input: strings.Replace(candidateSetJSON(), `go-engine/internal/example/example.go`, `go-engine/internal/example/example.`, 1), wantErr: ErrInvalidManifest},
		{name: "trailing space touched path", input: strings.Replace(candidateSetJSON(), `go-engine/internal/example/example.go`, `go-engine/internal/example/example `, 1), wantErr: ErrInvalidManifest},
		{name: "empty rationale", input: strings.Replace(candidateSetJSON(), `"rationale":"plausible product regression"`, `"rationale":""`, 1), wantErr: ErrInvalidManifest},
		{name: "oversized rationale", input: strings.Replace(candidateSetJSON(), `"rationale":"plausible product regression"`, `"rationale":"`+strings.Repeat("a", 513)+`"`, 1), wantErr: ErrInvalidManifest},
		{name: "empty violated behavior", input: strings.Replace(candidateSetJSON(), `"violatedBehavior":"declared product behavior"`, `"violatedBehavior":""`, 1), wantErr: ErrInvalidManifest},
		{name: "oversized violated behavior", input: strings.Replace(candidateSetJSON(), `"violatedBehavior":"declared product behavior"`, `"violatedBehavior":"`+strings.Repeat("a", 513)+`"`, 1), wantErr: ErrInvalidManifest},
		{name: "malformed expected failure", input: strings.Replace(candidateSetJSON(), `"class":"contract"`, `"class":"bad class"`, 1), wantErr: ErrInvalidManifest},
		{name: "oversized expected failure", input: strings.Replace(candidateSetJSON(), `"coordinate":"modules[0]"`, `"coordinate":"`+strings.Repeat("a", 129)+`"`, 1), wantErr: ErrInvalidManifest},
		{name: "wrong criticality", input: strings.Replace(candidateSetJSON(), `"critical":false`, `"critical":true`, 1), wantErr: ErrInvalidManifest},
		{name: "unknown legacy lane", input: strings.Replace(candidateSetJSON(), `"id":"windows-go"`, `"id":"foreign-lane"`, 1), wantErr: ErrInvalidManifest},
		{name: "wrong legacy runner", input: strings.Replace(candidateSetJSON(), `"id":"windows-go","runner":"windows-latest"`, `"id":"windows-go","runner":"ubuntu-latest"`, 1), wantErr: ErrInvalidManifest},
		{name: "invalid legacy timeout", input: strings.Replace(candidateSetJSON(), `"timeoutSeconds":900`, `"timeoutSeconds":0`, 1), wantErr: ErrInvalidManifest},
		{name: "candidate ceiling", input: candidateSetJSONWithCount("module-data", 46), wantErr: ErrInvalidManifest},
		{name: "quota incomplete", input: candidateSetJSONWithCount("critical-safety", 5), wantErr: ErrInvalidManifest},
		{name: "exact size boundary", input: candidateSetJSON() + strings.Repeat(" ", MaxManifestSize-len(candidateSetJSON())), wantErr: nil},
		{name: "oversized input", input: candidateSetJSON() + strings.Repeat(" ", MaxManifestSize), wantErr: ErrInputTooLarge},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeCandidateSet([]byte(tt.input))
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("DecodeCandidateSet() error = %v", err)
				}
				if got.SchemaVersion != SchemaVersion || got.Reference.Commit != testCommit || len(got.Queues) != 4 {
					t.Fatalf("DecodeCandidateSet() = %#v, want schema-bound ordered candidate set", got)
				}
				return
			}
			if err != tt.wantErr {
				t.Fatalf("DecodeCandidateSet() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestDecodeFrozenManifest(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{name: "valid quota filled manifest", input: frozenManifestJSON(), wantErr: nil},
		{name: "unknown field", input: strings.Replace(frozenManifestJSON(), `"schemaVersion":1,`, `"schemaVersion":1,"extra":true,`, 1), wantErr: ErrUnknownField},
		{name: "duplicate key", input: strings.Replace(frozenManifestJSON(), `"phase":"validation"`, `"phase":"validation","phase":"other"`, 1), wantErr: ErrDuplicateJSONKey},
		{name: "foreign item reference", input: strings.Replace(frozenManifestJSON(), `"reference":{"commit":"`+testCommit+`","tree":"`+testTree+`"}`, `"reference":{"commit":"dddddddddddddddddddddddddddddddddddddddd","tree":"`+testTree+`"}`, 1), wantErr: ErrInvalidManifest},
		{name: "wrong category order", input: strings.Replace(frozenManifestJSON(), `"category":"module-data"`, `"category":"engine-lifecycle"`, 1), wantErr: ErrInvalidManifest},
		{name: "duplicate frozen id", input: strings.Replace(frozenManifestJSON(), `"id":"engine-lifecycle-01"`, `"id":"module-data-01"`, 1), wantErr: ErrInvalidManifest},
		{name: "duplicate frozen digest", input: strings.Replace(frozenManifestJSON(), digestFor("engine-lifecycle", 1), digestFor("module-data", 1), 1), wantErr: ErrInvalidManifest},
		{name: "empty violated behavior", input: strings.Replace(frozenManifestJSON(), `"violatedBehavior":"declared product behavior"`, `"violatedBehavior":""`, 1), wantErr: ErrInvalidManifest},
		{name: "oversized violated behavior", input: strings.Replace(frozenManifestJSON(), `"violatedBehavior":"declared product behavior"`, `"violatedBehavior":"`+strings.Repeat("a", 513)+`"`, 1), wantErr: ErrInvalidManifest},
		{name: "malformed qualification run", input: strings.Replace(frozenManifestJSON(), `"runId":"qualification-v1"`, `"runId":"bad run"`, 1), wantErr: ErrInvalidManifest},
		{name: "empty qualification run", input: strings.Replace(frozenManifestJSON(), `"runId":"qualification-v1"`, `"runId":""`, 1), wantErr: ErrInvalidManifest},
		{name: "uppercase qualification digest", input: strings.Replace(frozenManifestJSON(), `"evidenceSha256":"`+testDigest+`"`, `"evidenceSha256":"`+strings.ToUpper(testDigest)+`"`, 1), wantErr: ErrInvalidManifest},
		{name: "uppercase tree", input: strings.Replace(frozenManifestJSON(), testTree, strings.ToUpper(testTree), 1), wantErr: ErrInvalidManifest},
		{name: "uppercase patch digest", input: strings.Replace(frozenManifestJSON(), digestFor("module-data", 1), strings.ToUpper(digestFor("module-data", 1)), 1), wantErr: ErrInvalidManifest},
		{name: "unsupported schema", input: strings.Replace(frozenManifestJSON(), `"schemaVersion":1`, `"schemaVersion":2`, 1), wantErr: ErrUnsupportedSchema},
		{name: "incomplete quota", input: frozenManifestJSONWithCount("critical-safety", 5), wantErr: ErrInvalidManifest},
		{name: "exact size boundary", input: frozenManifestJSON() + strings.Repeat(" ", MaxManifestSize-len(frozenManifestJSON())), wantErr: nil},
		{name: "oversized input", input: frozenManifestJSON() + strings.Repeat(" ", MaxManifestSize), wantErr: ErrInputTooLarge},
		{name: "trailing value", input: frozenManifestJSON() + ` false`, wantErr: ErrTrailingJSONValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeFrozenManifest([]byte(tt.input))
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("DecodeFrozenManifest() error = %v", err)
				}
				if got.SchemaVersion != SchemaVersion || len(got.Items) != 30 || got.Items[0].Qualification.EvidenceSHA256 != testDigest {
					t.Fatalf("DecodeFrozenManifest() = %#v, want quota-filled frozen manifest", got)
				}
				return
			}
			if err != tt.wantErr {
				t.Fatalf("DecodeFrozenManifest() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func candidateSetJSON() string {
	return candidateSetJSONWithCount("", 0)
}

func candidateSetJSONWithCount(category string, count int) string {
	counts := map[string]int{
		"module-data": 10, "engine-lifecycle": 8, "artifact-config": 6, "critical-safety": 6,
	}
	if category != "" {
		counts[category] = count
	}
	return fmt.Sprintf(`{"schemaVersion":1,"reference":%s,"detectors":[{"id":"module-contract","contractSha256":"%s"}],"legacyLanes":[%s],"queues":[%s]}`,
		referenceJSON(), testDigest, legacyLanesJSON(), queuesJSON(counts))
}

func frozenManifestJSON() string {
	return frozenManifestJSONWithCount("", 0)
}

func frozenManifestJSONWithCount(category string, count int) string {
	counts := map[string]int{
		"module-data": 10, "engine-lifecycle": 8, "artifact-config": 6, "critical-safety": 6,
	}
	if category != "" {
		counts[category] = count
	}
	items := make([]string, 0, 30)
	for _, entry := range categoryOrder {
		for index := 1; index <= counts[entry]; index++ {
			items = append(items, frozenItemJSON(entry, index))
		}
	}
	return fmt.Sprintf(`{"schemaVersion":1,"reference":%s,"detectors":[{"id":"module-contract","contractSha256":"%s"}],"items":[%s]}`,
		referenceJSON(), testDigest, strings.Join(items, ","))
}

var categoryOrder = []string{"module-data", "engine-lifecycle", "artifact-config", "critical-safety"}

func queuesJSON(counts map[string]int) string {
	queues := make([]string, 0, len(categoryOrder))
	for _, category := range categoryOrder {
		candidates := make([]string, 0, counts[category])
		for index := 1; index <= counts[category]; index++ {
			candidates = append(candidates, candidateJSON(category, index))
		}
		queues = append(queues, fmt.Sprintf(`{"category":%q,"candidates":[%s]}`, category, strings.Join(candidates, ",")))
	}
	return strings.Join(queues, ",")
}

func candidateJSON(category string, index int) string {
	critical := category == "critical-safety"
	return fmt.Sprintf(`{"id":%q,"reference":%s,"patchPath":%q,"patchSha256":%q,"touchedPaths":["go-engine/internal/example/example.go"],"rationale":"plausible product regression","violatedBehavior":"declared product behavior","critical":%t,"detector":"module-contract","expectedFailure":{"class":"contract","phase":"validation","coordinate":"modules[0]"}}`,
		candidateID(category, index), referenceJSON(), "validation/ci-efficacy/v1/patches/"+candidateID(category, index)+".patch", digestFor(category, index), critical)
}

func frozenItemJSON(category string, index int) string {
	critical := category == "critical-safety"
	return fmt.Sprintf(`{"id":%q,"category":%q,"reference":%s,"patchSha256":%q,"critical":%t,"detector":"module-contract","expectedFailure":{"class":"contract","phase":"validation","coordinate":"modules[0]"},"violatedBehavior":"declared product behavior","qualification":{"runId":"qualification-v1","evidenceSha256":%q}}`,
		candidateID(category, index), category, referenceJSON(), digestFor(category, index), critical, testDigest)
}

func referenceJSON() string {
	return fmt.Sprintf(`{"commit":%q,"tree":%q}`, testCommit, testTree)
}

func legacyLanesJSON() string {
	lanes := []string{
		`{"id":"windows-go","runner":"windows-latest","commandSha256":"` + digestFor("module-data", 1) + `","timeoutSeconds":900}`,
		`{"id":"ubuntu-go","runner":"ubuntu-latest","commandSha256":"` + digestFor("module-data", 2) + `","timeoutSeconds":900}`,
		`{"id":"macos-go","runner":"macos-latest","commandSha256":"` + digestFor("module-data", 3) + `","timeoutSeconds":900}`,
		`{"id":"windows-integration","runner":"windows-latest","commandSha256":"` + digestFor("module-data", 4) + `","timeoutSeconds":1200}`,
		`{"id":"ubuntu-nix","runner":"ubuntu-latest","commandSha256":"` + digestFor("module-data", 5) + `","timeoutSeconds":1200}`,
		`{"id":"macos-nix","runner":"macos-latest","commandSha256":"` + digestFor("module-data", 6) + `","timeoutSeconds":1200}`,
	}
	return strings.Join(lanes, ",")
}

func candidateID(category string, index int) string {
	return fmt.Sprintf("%s-%02d", category, index)
}

func digestFor(category string, index int) string {
	categoryIndex := 0
	for index, value := range categoryOrder {
		if category == value {
			categoryIndex = index + 1
			break
		}
	}
	return strings.Repeat(string(rune('a'+categoryIndex-1)), 62) + fmt.Sprintf("%02x", index)
}
