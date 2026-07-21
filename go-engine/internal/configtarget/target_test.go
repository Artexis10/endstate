// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configtarget

import "testing"

func TestClaimsOverlap(t *testing.T) {
	fs := func(target string) Claim { return Claim{Kind: Filesystem, Canonical: CanonicalFilesystem(target)} }
	reg := func(key, valueName string) Claim {
		return Claim{Kind: Registry, Canonical: CanonicalRegistry(key, valueName)}
	}

	tests := []struct {
		name  string
		left  Claim
		right Claim
		want  bool
	}{
		{"equal filesystem, case/slash-insensitive", fs(`C:\Users\me\.wslconfig`), fs("c:/users/me/.wslconfig"), true},
		{"nested filesystem", fs(`C:\Users\me\.config`), fs(`C:\Users\me\.config\app\settings.json`), true},
		{"sibling filesystem no overlap", fs(`C:\Users\me\.config-a`), fs(`C:\Users\me\.config-b`), false},
		{"prefix but not path boundary", fs(`C:\Users\me\.config`), fs(`C:\Users\me\.config-extra`), false},
		{"equal registry value", reg(`HKCU\Software\App`, "Setting"), reg(`hkcu/software/app`, "setting"), true},
		{"different registry value", reg(`HKCU\Software\App`, "A"), reg(`HKCU\Software\App`, "B"), false},
		{"cross-kind never overlaps", fs(`C:\x`), reg(`C:\x`, ""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ClaimsOverlap(tt.left, tt.right); got != tt.want {
				t.Fatalf("ClaimsOverlap(%+v, %+v) = %v, want %v", tt.left, tt.right, got, tt.want)
			}
			if got := ClaimsOverlap(tt.right, tt.left); got != tt.want {
				t.Fatalf("ClaimsOverlap is not symmetric for %q", tt.name)
			}
		})
	}
}
