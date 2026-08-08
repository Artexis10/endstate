// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func TestLoadValidationCommandManifestBindsValidationDriverDisplayName(t *testing.T) {
	context := validationContext(t, validationmode.Inventory{
		AppID: "studio-one", Driver: "validation", Ref: "studio-one", DisplayName: "PreSonus Studio One",
		Version: "7", InitialState: "present",
	})
	original := currentValidationMode
	currentValidationMode = context
	t.Cleanup(func() { currentValidationMode = original })

	writeManifest := func(t *testing.T, displayName string) string {
		t.Helper()
		path := filepath.Join(t.TempDir(), "captured.jsonc")
		data := []byte(`{"version":1,"apps":[{"id":"studio-one","refs":{"windows":"studio-one"},"driver":"validation","displayName":"` + displayName + `"}]}`)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	if _, envelopeErr := loadValidationCommandManifest(writeManifest(t, "PreSonus Studio One")); envelopeErr != nil {
		t.Fatalf("matching displayName rejected: %+v", envelopeErr)
	}
	if _, envelopeErr := loadValidationCommandManifest(writeManifest(t, "Different Studio One")); envelopeErr == nil {
		t.Fatal("wrong displayName accepted")
	}

	projected := filepath.Join(t.TempDir(), "captured-projected.jsonc")
	projectedData := []byte(`{
  "version":1,
  "apps":[{"id":"studio-one","refs":{"windows":"studio-one"},"driver":"validation","displayName":"PreSonus Studio One"}],
  "configModules":["apps.studio-one"],
  "restore":[{"type":"copy","source":"./configs/apps.studio-one/settings.json","target":"%APPDATA%\\PreSonus\\settings.json","backup":true,"fromModule":"apps.studio-one"}]
}`)
	if err := os.WriteFile(projected, projectedData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, envelopeErr := loadValidationCommandManifest(projected); envelopeErr != nil {
		t.Fatalf("strict projected validation manifest rejected: %+v", envelopeErr)
	}
}
