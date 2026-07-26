// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAppFingerprintJSONRoundTripAndLegacyCompatibility(t *testing.T) {
	legacy := []byte(`{"id":"chrome","refs":{"windows":"Google.Chrome"}}`)
	var app App
	if err := json.Unmarshal(legacy, &app); err != nil {
		t.Fatal(err)
	}
	if app.Fingerprint != nil {
		t.Fatalf("legacy app fingerprint = %#v, want nil", app.Fingerprint)
	}
	app.Fingerprint = &InstallFingerprint{Key: "Google Chrome", Publisher: "Google LLC", Version: "136"}
	encoded, err := json.Marshal(app)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"fingerprint":{"key":"Google Chrome","publisher":"Google LLC","version":"136"}`) {
		t.Fatalf("encoded app = %s", encoded)
	}
	var roundTripped App
	if err := json.Unmarshal(encoded, &roundTripped); err != nil {
		t.Fatal(err)
	}
	if roundTripped.Fingerprint == nil || *roundTripped.Fingerprint != *app.Fingerprint {
		t.Fatalf("round-tripped fingerprint = %#v", roundTripped.Fingerprint)
	}
	var legacyReader struct {
		ID   string            `json:"id"`
		Refs map[string]string `json:"refs"`
	}
	if err := json.Unmarshal(encoded, &legacyReader); err != nil {
		t.Fatal(err)
	}
	if legacyReader.ID != app.ID || legacyReader.Refs["windows"] != "Google.Chrome" {
		t.Fatalf("legacy reader lost known fields: %#v", legacyReader)
	}
}
