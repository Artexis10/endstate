// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import "testing"

// Portable desktop evidence must not be relabeled as legacy or package authority.
func TestDesktopCaptureEvidenceRequiresSchemaV3Linux(t *testing.T) {
	for _, test := range []struct {
		name                    string
		legacy, packageIdentity bool
		invalid                 bool
	}{
		{name: "valid"},
		{name: "legacy", legacy: true, invalid: true},
		{name: "package", packageIdentity: true, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			value := validManifestV2Value("capture-clock")
			capture := firstCapture(value)
			capture["sourcePlatform"] = "linux"
			capture["captureModule"].(map[string]any)["schemaVersion"] = 3
			evidence := map[string]any{"type": "desktop", "backend": "gsettings", "platform": "linux"}
			capture["sourceInstance"].(map[string]any)["evidence"] = evidence
			if test.legacy {
				capture["captureModule"].(map[string]any)["schemaVersion"] = 2
			}
			if test.packageIdentity {
				evidence["ref"] = "vendor.package"
			}
			_, err := LoadManifest(writeManifestValue(t, value))
			if (err != nil) != test.invalid {
				t.Fatalf("desktop source evidence: %v", err)
			}
		})
	}
}
