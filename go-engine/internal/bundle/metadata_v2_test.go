// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"encoding/json"
	"testing"
)

func TestBundleMetadataCanRepresentSchemaTwoWithoutChangingV1Defaults(t *testing.T) {
	v2 := BundleMetadata{
		SchemaVersion:          "2.0",
		ManifestVersion:        2,
		ConfigCapturesIncluded: []string{"capture-a", "capture-b"},
	}
	data, err := json.Marshal(v2)
	if err != nil {
		t.Fatal(err)
	}
	var decoded BundleMetadata
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.ManifestVersion != 2 || len(decoded.ConfigCapturesIncluded) != 2 {
		t.Fatalf("schema-v2 metadata did not round-trip: %+v", decoded)
	}

	legacy, err := json.Marshal(BundleMetadata{SchemaVersion: "1.0"})
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(legacy, &raw); err != nil {
		t.Fatal(err)
	}
	if _, exists := raw["manifestVersion"]; exists {
		t.Fatalf("zero-value manifestVersion changed v1 metadata: %s", legacy)
	}
	if _, exists := raw["configCapturesIncluded"]; exists {
		t.Fatalf("zero-value configCapturesIncluded changed v1 metadata: %s", legacy)
	}
}
