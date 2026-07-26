//go:build windows

// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package arp

import (
	"errors"
	"testing"
)

type fixtureKey struct {
	children map[string]*fixtureKey
	strings  map[string]string
	integers map[string]uint64
}

func (key *fixtureKey) ReadSubKeyNames(int) ([]string, error) {
	names := make([]string, 0, len(key.children))
	for name := range key.children {
		names = append(names, name)
	}
	return names, nil
}
func (key *fixtureKey) OpenKey(path string) (registryValueKey, error) {
	child, ok := key.children[path]
	if !ok {
		return nil, errors.New("missing fixture key")
	}
	return child, nil
}
func (key *fixtureKey) GetStringValue(name string) (string, uint32, error) {
	value, ok := key.strings[name]
	if !ok {
		return "", 0, errors.New("missing fixture value")
	}
	return value, 0, nil
}
func (key *fixtureKey) GetIntegerValue(name string) (uint64, uint32, error) {
	value, ok := key.integers[name]
	if !ok {
		return 0, 0, errors.New("missing fixture value")
	}
	return value, 0, nil
}
func (*fixtureKey) Close() error { return nil }

func TestReadHivesIncludesWowAndCurrentUserEntries(t *testing.T) {
	visible := func(name string) *fixtureKey {
		return &fixtureKey{strings: map[string]string{"DisplayName": name, "DisplayVersion": "1.0", "Publisher": "Endstate", "InstallLocation": `C:\Apps`}}
	}
	entries := readHives([]hive{
		{root: &fixtureKey{children: map[string]*fixtureKey{"uninstall": {children: map[string]*fixtureKey{"Machine": visible("Machine App")}}}}, path: "uninstall"},
		{root: &fixtureKey{children: map[string]*fixtureKey{"wow": {children: map[string]*fixtureKey{"Wow": visible("WOW App")}}}}, path: "wow"},
		{root: &fixtureKey{children: map[string]*fixtureKey{"uninstall": {children: map[string]*fixtureKey{"User": visible("User App"), "Hidden": {strings: map[string]string{"DisplayName": "Hidden"}, integers: map[string]uint64{"SystemComponent": 1}}}}}}, path: "uninstall"},
	})
	if len(entries) != 3 {
		t.Fatalf("entries = %#v, want machine, WOW, and current-user entries", entries)
	}
	seen := map[string]bool{}
	for _, entry := range entries {
		seen[entry.Key] = true
	}
	for _, name := range []string{"Machine", "Wow", "User"} {
		if !seen[name] {
			t.Errorf("missing %s entry in %#v", name, entries)
		}
	}
}

func TestArchitectureForGOARCH(t *testing.T) {
	for _, test := range []struct{ goarch, want string }{
		{"386", "X86"}, {"amd64", "X64"}, {"arm64", "ARM64"}, {"mips", "X64"},
	} {
		if got := architectureForGOARCH(test.goarch); got != test.want {
			t.Errorf("architectureForGOARCH(%q) = %q, want %q", test.goarch, got, test.want)
		}
	}
}
