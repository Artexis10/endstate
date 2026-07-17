// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConcreteFilesystemTargetCanonicalizesCaseAliasesOnInsensitiveVolume(t *testing.T) {
	root := t.TempDir()
	actualParent := filepath.Join(root, "Preferences")
	if err := os.MkdirAll(actualParent, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(root, "pREFERENCES")
	actualInfo, err := os.Lstat(actualParent)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Lstat(aliasParent)
	if err != nil || !os.SameFile(actualInfo, aliasInfo) {
		t.Skip("test volume is case-sensitive")
	}
	left, err := ConcreteFilesystemTarget(filepath.Join(actualParent, "Theme.JSON"))
	if err != nil {
		t.Fatal(err)
	}
	right, err := ConcreteFilesystemTarget(filepath.Join(aliasParent, "tHEME.json"))
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("case aliases differ: %q != %q", left, right)
	}
}

func TestConcreteFilesystemTargetRejectsLinkAlias(t *testing.T) {
	root := t.TempDir()
	realTarget := filepath.Join(root, "real")
	if err := os.MkdirAll(realTarget, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "linked")
	if err := os.Symlink(realTarget, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := ConcreteFilesystemTarget(filepath.Join(link, "prefs.json"))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "link") {
		t.Fatalf("ConcreteFilesystemTarget() error = %v", err)
	}
}
