// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package restore

import (
	"os"
	"path/filepath"
	"slices"
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

func TestFilesystemIdentityKeepsHomeManagerSymlinkLeaf(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(root, "managed")
	if err := os.Symlink(filepath.Join(root, "missing-store-source"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	identity, err := FilesystemTargetIdentity(link)
	if err != nil || identity != link {
		t.Fatalf("native leaf lost its lexical identity: %q %v", identity, err)
	}
	if _, err := ConcreteFilesystemTarget(link); err == nil {
		t.Fatal("identity lookup granted direct-restore authority over a symlink")
	}
	if err := ValidateFilesystemTarget(link); err == nil {
		t.Fatal("direct mutation accepted an existing symlink")
	}
}

func TestFilesystemTargetIdentitiesKeepAncestorClaimsAndLeafIdentity(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	shared, physical := filepath.Join(root, "shared"), filepath.Join(root, "physical")
	for _, directory := range []string{shared, physical} {
		if err := os.Mkdir(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(shared, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.Symlink("../physical", filepath.Join(shared, "nested")); err != nil {
		t.Fatal(err)
	}
	referent := filepath.Join(root, "referent")
	if err := os.WriteFile(referent, []byte("prior"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(referent, filepath.Join(physical, "settings")); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"settings", filepath.Join("missing", "settings")} {
		t.Run(suffix, func(t *testing.T) {
			target := filepath.Join(alias, "nested", suffix)
			identities, err := FilesystemTargetIdentities(target)
			if err != nil {
				t.Fatal(err)
			}
			for _, expected := range []string{target, filepath.Join(shared, "nested", suffix), filepath.Join(physical, suffix)} {
				if !slices.Contains(identities, expected) {
					t.Fatalf("ancestor containment identity %q missing from %q", expected, identities)
				}
			}
			if slices.Contains(identities, referent) {
				t.Fatalf("activation leaf claimed its referent: %q", identities)
			}
			if err := ValidateFilesystemTarget(target); err == nil {
				t.Fatal("collision lookup granted direct mutation through an alias")
			}
		})
	}
}
