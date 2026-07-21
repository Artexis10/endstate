// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configtarget

import (
	"path/filepath"
	"testing"
)

func TestClaimsOverlap(t *testing.T) {
	fs := func(target string) Claim { return Claim{Kind: Filesystem, Canonical: CanonicalFilesystem(target)} }
	reg := func(key, valueName string) Claim {
		return Claim{Kind: Registry, Canonical: CanonicalRegistry(key, valueName)}
	}

	// Filesystem targets are built with OS-native separators via filepath.Join,
	// mirroring the pre-refactor planner tests: CanonicalFilesystem leans on
	// filepath.Clean, which is host-dependent (it only treats `\` as a separator
	// on Windows), and the planner only ever runs it on host-expanded paths. The
	// portable, cross-platform properties are case-folding and equal/nested
	// overlap — not separator style — so the fixtures exercise those.
	root := filepath.Join("home", "me")

	tests := []struct {
		name  string
		left  Claim
		right Claim
		want  bool
	}{
		{"equal filesystem, case-insensitive", fs(filepath.Join(root, ".WSLConfig")), fs(filepath.Join(root, ".wslconfig")), true},
		{"nested filesystem", fs(filepath.Join(root, ".config")), fs(filepath.Join(root, ".config", "app", "settings.json")), true},
		{"sibling filesystem no overlap", fs(filepath.Join(root, ".config-a")), fs(filepath.Join(root, ".config-b")), false},
		{"prefix but not path boundary", fs(filepath.Join(root, ".config")), fs(filepath.Join(root, ".config-extra")), false},
		{"equal registry value, case/slash-insensitive", reg(`HKCU\Software\App`, "Setting"), reg(`hkcu/software/app`, "setting"), true},
		{"different registry value", reg(`HKCU\Software\App`, "A"), reg(`HKCU\Software\App`, "B"), false},
		{"cross-kind never overlaps", fs(filepath.Join(root, "x")), reg(filepath.Join(root, "x"), ""), false},
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
