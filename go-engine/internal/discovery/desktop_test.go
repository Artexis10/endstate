// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestDesktopAdapterHonorsPrecedenceVisibilityAndDesktopIDs(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "user", "applications")
	system := filepath.Join(root, "system", "applications")
	for _, directory := range []string{user, filepath.Join(system, "nested")} {
		if err := os.MkdirAll(directory, 0755); err != nil {
			t.Fatal(err)
		}
	}
	writeDesktopFixture(t, filepath.Join(user, "code.desktop"), "Visual Studio Code", false, false, "Application")
	writeDesktopFixture(t, filepath.Join(user, "hidden.desktop"), "Hidden override", true, false, "Application")
	writeDesktopFixture(t, filepath.Join(system, "hidden.desktop"), "Must stay masked", false, false, "Application")
	writeDesktopFixture(t, filepath.Join(system, "nodisplay.desktop"), "Background helper", false, true, "Application")
	writeDesktopFixture(t, filepath.Join(system, "nested", "firefox.desktop"), "Firefox", false, false, "Application")
	writeDesktopFixture(t, filepath.Join(system, "link.desktop"), "Website", false, false, "Link")

	inventory, err := (DesktopAdapter{Roots: []string{user, system}}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := []InventoryItem{
		{Source: "desktop", Kind: EvidenceDesktop, Ref: "code.desktop", DisplayName: "Visual Studio Code", UserFacing: true},
		{Source: "desktop", Kind: EvidenceDesktop, Ref: "nested-firefox.desktop", DisplayName: "Firefox", UserFacing: true},
	}
	if !reflect.DeepEqual(inventory.Items, want) {
		t.Fatalf("items = %#v, want %#v", inventory.Items, want)
	}
	if inventory.Ignored != 4 {
		t.Fatalf("ignored = %d, want 4", inventory.Ignored)
	}
}

func TestDesktopAdapterUnavailableWhenNoDataRootExists(t *testing.T) {
	_, err := (DesktopAdapter{Roots: []string{filepath.Join(t.TempDir(), "missing")}}).Inventory(context.Background())
	if !errors.Is(err, ErrNotAvailable) {
		t.Fatalf("error = %v, want ErrNotAvailable", err)
	}
}

func TestDesktopAdapterUsesDefaultHomeForRelativeXDGOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG inventory uses Unix paths")
	}
	home := t.TempDir()
	user := filepath.Join(home, ".local", "share", "applications")
	system := filepath.Join(t.TempDir(), "applications")
	for _, directory := range []string{user, system} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeDesktopFixture(t, filepath.Join(user, "editor.desktop"), "User editor", false, false, "Application")
	writeDesktopFixture(t, filepath.Join(system, "editor.desktop"), "System editor", false, false, "Application")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "relative-data")
	t.Setenv("XDG_DATA_DIRS", filepath.Dir(system))
	inventory, err := (DesktopAdapter{}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Items) != 1 || inventory.Items[0].DisplayName != "User editor" || inventory.Ignored != 1 {
		t.Fatalf("relative XDG override lost user precedence: %+v", inventory)
	}
}

func TestDesktopAdapterPreservesSpacesInAbsoluteXDGPaths(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG inventory uses Unix paths")
	}
	root := t.TempDir()
	user := filepath.Join(root, "user data ", "applications")
	system := filepath.Join(root, "system data ", "applications")
	for _, directory := range []string{user, system} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeDesktopFixture(t, filepath.Join(user, "editor.desktop"), "Editor", false, false, "Application")
	writeDesktopFixture(t, filepath.Join(system, "browser.desktop"), "Browser", false, false, "Application")
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", filepath.Dir(user))
	t.Setenv("XDG_DATA_DIRS", "relative-data"+string(os.PathListSeparator)+filepath.Dir(system))
	inventory, err := (DesktopAdapter{}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Items) != 2 || inventory.Items[0].Ref != "browser.desktop" || inventory.Items[1].Ref != "editor.desktop" {
		t.Fatalf("absolute XDG paths lost literal spaces: %+v", inventory)
	}
}

func TestDesktopAdapterPreservesDefaultHomeTombstone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG inventory uses Unix paths")
	}
	root := t.TempDir()
	home := filepath.Join(root, "home ")
	user := filepath.Join(home, ".local", "share", "applications")
	system := filepath.Join(root, "system", "applications")
	for _, directory := range []string{user, system} {
		if err := os.MkdirAll(directory, 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeDesktopFixture(t, filepath.Join(user, "editor.desktop"), "Hidden editor", true, false, "Application")
	writeDesktopFixture(t, filepath.Join(system, "editor.desktop"), "System editor", false, false, "Application")
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_DATA_DIRS", filepath.Dir(system))
	inventory, err := (DesktopAdapter{}).Inventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inventory.Items) != 0 || inventory.Ignored != 2 {
		t.Fatalf("default home lost the user tombstone: %+v", inventory)
	}
}

func TestDesktopAdapterRejectsOversizedAndMalformedEntries(t *testing.T) {
	for name, content := range map[string][]byte{
		"oversized": make([]byte, maxDesktopEntrySize+1),
		"malformed": []byte("[Desktop Entry]\nType=Application\nnot-a-pair\n"),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "bad.desktop"), content, 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := (DesktopAdapter{Roots: []string{root}}).Inventory(context.Background()); err == nil {
				t.Fatal("malformed desktop entry was accepted")
			}
		})
	}
}

func writeDesktopFixture(t *testing.T, path, name string, hidden, noDisplay bool, entryType string) {
	t.Helper()
	data := "[Desktop Entry]\nType=" + entryType + "\nName=" + name + "\n"
	if hidden {
		data += "Hidden=true\n"
	}
	if noDisplay {
		data += "NoDisplay=true\n"
	}
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
}
