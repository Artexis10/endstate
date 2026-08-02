// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"encoding/json"
	"testing"
)

func TestSecretsDefDeserializesRegistryKeys(t *testing.T) {
	var secret SecretsDef
	if err := json.Unmarshal([]byte(`{"files":["%APPDATA%\\App\\settings.json"],"registryKeys":["HKCU\\Software\\App\\Token"]}`), &secret); err != nil {
		t.Fatal(err)
	}
	if len(secret.RegistryKeys) != 1 || secret.RegistryKeys[0] != `HKCU\Software\App\Token` {
		t.Fatalf("registryKeys = %#v", secret.RegistryKeys)
	}
}

func TestClassifySecretCoordinate(t *testing.T) {
	tests := []struct {
		name, authored, normalized string
		want                       SecretCoordinateKind
	}{
		{name: "filesystem", authored: `%APPDATA%\App\token.json`, want: SecretCoordinateFilesystem},
		{name: "hkcu", authored: `hKcU:/Software/App/Token`, normalized: `HKCU\Software\App\Token`, want: SecretCoordinateRegistry},
		{name: "hklm", authored: `HKEY_LOCAL_MACHINE\Software\App`, normalized: `HKLM\Software\App`, want: SecretCoordinateRegistry},
		{name: "unsupported hive", authored: `HKU\Software\App`, want: SecretCoordinateRegistryInvalid},
		{name: "malformed hive prefix", authored: `HKCUApp\Token`, want: SecretCoordinateRegistryInvalid},
		{name: "wildcard", authored: `HKCU\Software\App\*`, want: SecretCoordinateRegistryInvalid},
		{name: "ambiguous traversal", authored: `HKCU\Software\..\Token`, want: SecretCoordinateRegistryInvalid},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, normalized := ClassifySecretCoordinate(test.authored)
			if got != test.want || normalized != test.normalized {
				t.Fatalf("ClassifySecretCoordinate(%q) = (%v, %q), want (%v, %q)", test.authored, got, normalized, test.want, test.normalized)
			}
		})
	}
}
