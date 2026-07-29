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
		{name: "duplicate candidate id", input: strings.Replace(candidateSetJSON(), `"id":"engine-lifecycle-01"`, `"id":"module-data-01"`, 1), wantErr: ErrInvalidManifest},
		{name: "candidate ceiling", input: candidateSetJSONWithCount("module-data", 46), wantErr: ErrInvalidManifest},
		{name: "quota incomplete", input: candidateSetJSONWithCount("critical-safety", 5), wantErr: ErrInvalidManifest},
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
		{name: "incomplete quota", input: frozenManifestJSONWithCount("critical-safety", 5), wantErr: ErrInvalidManifest},
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
	return fmt.Sprintf(`{"schemaVersion":1,"reference":%s,"detectors":[{"id":"module-contract","contractSha256":"%s"}],"queues":[%s]}`,
		referenceJSON(), testDigest, queuesJSON(counts))
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
	return fmt.Sprintf(`{"id":%q,"category":%q,"reference":%s,"patchSha256":%q,"critical":%t,"detector":"module-contract","expectedFailure":{"class":"contract","phase":"validation","coordinate":"modules[0]"},"qualification":{"runId":"qualification-v1","evidenceSha256":%q}}`,
		candidateID(category, index), category, referenceJSON(), digestFor(category, index), critical, testDigest)
}

func referenceJSON() string {
	return fmt.Sprintf(`{"commit":%q,"tree":%q}`, testCommit, testTree)
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
	return fmt.Sprintf("%064x", categoryIndex*100+index)
}
