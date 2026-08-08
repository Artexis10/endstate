// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import "testing"

func TestSemanticFilesystemScratchPreservesSemanticPathDomain(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   string
	}{
		{
			name:   "windows authored alias",
			target: `%APPDATA%\Vendor\settings.json`,
			want:   `%APPDATA%\Vendor\.settings.json.endstate-revert-0123456789abcdef-stage`,
		},
		{
			name:   "slash domain alias",
			target: `${instance.root}/Vendor/settings.json`,
			want:   `${instance.root}/Vendor/.settings.json.endstate-revert-0123456789abcdef-stage`,
		},
		{
			name:   "mixed authored separators",
			target: `%APPDATA%\Vendor/settings.json`,
			want:   `%APPDATA%\Vendor/.settings.json.endstate-revert-0123456789abcdef-stage`,
		},
		{
			name:   "bare relative identity",
			target: `settings.json`,
			want:   `.settings.json.endstate-revert-0123456789abcdef-stage`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := semanticFilesystemScratch(test.target, "0123456789abcdef", "stage")
			if got != test.want {
				t.Fatalf("semantic scratch = %q, want %q", got, test.want)
			}
		})
	}
}
