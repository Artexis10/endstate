// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

const maxHostedLiveTargetBytes = 1 << 20

func snapshotHostedLiveTargets(definition LiveDefinition, appData string) (hostedLiveTargets, error) {
	root, err := validatedWindowsLiveAppData(appData)
	if err != nil || !hostedLiveTargetDefinition(definition) {
		return hostedLiveTargets{}, fmt.Errorf("hosted live target definition or APPDATA root is invalid")
	}
	resolved, err := resolveWindowsLiveDeclaredTargets(definition, root)
	if err != nil || len(resolved) != 6 {
		return hostedLiveTargets{}, fmt.Errorf("hosted live target resolution is invalid")
	}
	targets := hostedLiveTargets{files: make([]hostedLiveTargetFile, 0, 5)}
	for _, target := range resolved {
		switch target.kind {
		case LiveDeclaredTargetFile:
			file, err := snapshotHostedLiveTargetFile(target.identity, target.path)
			if err != nil {
				return hostedLiveTargets{}, err
			}
			targets.files = append(targets.files, file)
		case LiveDeclaredTargetDirectory:
			if target.identity != "apps/notepad-plus-plus/userDefineLangs" {
				return hostedLiveTargets{}, fmt.Errorf("hosted live target directory is invalid")
			}
			present, err := snapshotHostedLiveTargetDirectory(target.path)
			if err != nil {
				return hostedLiveTargets{}, err
			}
			targets.directory = hostedLiveTargetDirectory{identity: target.identity, present: present}
		default:
			return hostedLiveTargets{}, fmt.Errorf("hosted live target kind is invalid")
		}
	}
	sort.Slice(targets.files, func(left, right int) bool { return targets.files[left].identity < targets.files[right].identity })
	if err := validateHostedLiveTargets(targets); err != nil {
		return hostedLiveTargets{}, err
	}
	return targets, nil
}

func snapshotHostedLiveTargetFile(identity, path string) (hostedLiveTargetFile, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return hostedLiveTargetFile{identity: identity, absent: true}, nil
	}
	if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() {
		return hostedLiveTargetFile{}, fmt.Errorf("hosted live target file is unsafe")
	}
	data, mode, err := safepath.ReadRegularFileBounded(path, maxHostedLiveTargetBytes)
	if err != nil || !mode.IsRegular() {
		return hostedLiveTargetFile{}, fmt.Errorf("hosted live target file is unsafe")
	}
	digest := sha256.Sum256(data)
	return hostedLiveTargetFile{identity: identity, mode: mode, size: int64(len(data)), sha256: hex.EncodeToString(digest[:]), bytes: data}, nil
}

func snapshotHostedLiveTargetDirectory(path string) (bool, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() {
		return false, fmt.Errorf("hosted live target directory is unsafe")
	}
	return true, nil
}

func hostedLiveTargetDefinition(definition LiveDefinition) bool {
	if validateLiveDefinition(definition) != nil || definition.ModuleID != "apps.notepad-plus-plus" || definition.WingetRef != "Notepad++.Notepad++" || len(definition.DeclaredTargets) != 6 || len(definition.Comparator.Mappings) != 5 {
		return false
	}
	expected := map[string]LiveDeclaredTargetKind{
		"apps/notepad-plus-plus/config.xml":      LiveDeclaredTargetFile,
		"apps/notepad-plus-plus/contextMenu.xml": LiveDeclaredTargetFile,
		"apps/notepad-plus-plus/langs.xml":       LiveDeclaredTargetFile,
		"apps/notepad-plus-plus/shortcuts.xml":   LiveDeclaredTargetFile,
		"apps/notepad-plus-plus/stylers.xml":     LiveDeclaredTargetFile,
		"apps/notepad-plus-plus/userDefineLangs": LiveDeclaredTargetDirectory,
	}
	for _, target := range definition.DeclaredTargets {
		kind, exists := expected[target.Identity]
		if !exists || kind != target.Kind {
			return false
		}
		delete(expected, target.Identity)
	}
	if len(expected) != 0 {
		return false
	}
	mapped := make(map[string]struct{}, len(definition.Comparator.Mappings))
	for _, mapping := range definition.Comparator.Mappings {
		if mapping.Identity == "apps/notepad-plus-plus/userDefineLangs" {
			return false
		}
		kind, exists := expectedHostedLiveTargetKind(mapping.Identity)
		if !exists || kind != LiveDeclaredTargetFile {
			return false
		}
		if _, exists := mapped[mapping.Identity]; exists {
			return false
		}
		mapped[mapping.Identity] = struct{}{}
	}
	return len(mapped) == 5
}

func hostedLiveDeclaredTargetPaths(definition LiveDefinition, appData string) ([]string, error) {
	bindings, err := hostedLiveDeclaredTargetBindings(definition, appData)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(bindings))
	for _, target := range definition.DeclaredTargets {
		paths = append(paths, bindings[target.Identity])
	}
	return paths, nil
}

func hostedLiveDeclaredTargetBindings(definition LiveDefinition, appData string) (map[string]string, error) {
	root, err := validatedWindowsLiveAppData(appData)
	if err != nil || !hostedLiveTargetDefinition(definition) {
		return nil, fmt.Errorf("hosted live declared target authority is invalid")
	}
	resolved, err := resolveWindowsLiveDeclaredTargets(definition, root)
	if err != nil || len(resolved) != 6 {
		return nil, fmt.Errorf("hosted live declared target authority is invalid")
	}
	bindings := make(map[string]string, len(resolved))
	for _, target := range resolved {
		if target.identity == "" || target.path == "" {
			return nil, fmt.Errorf("hosted live declared target authority is invalid")
		}
		bindings[target.identity] = target.path
	}
	if len(bindings) != 6 {
		return nil, fmt.Errorf("hosted live declared target authority is invalid")
	}
	return bindings, nil
}

func expectedHostedLiveTargetKind(identity string) (LiveDeclaredTargetKind, bool) {
	for _, target := range []LiveDeclaredTarget{
		{Identity: "apps/notepad-plus-plus/config.xml", Kind: LiveDeclaredTargetFile},
		{Identity: "apps/notepad-plus-plus/contextMenu.xml", Kind: LiveDeclaredTargetFile},
		{Identity: "apps/notepad-plus-plus/langs.xml", Kind: LiveDeclaredTargetFile},
		{Identity: "apps/notepad-plus-plus/shortcuts.xml", Kind: LiveDeclaredTargetFile},
		{Identity: "apps/notepad-plus-plus/stylers.xml", Kind: LiveDeclaredTargetFile},
		{Identity: "apps/notepad-plus-plus/userDefineLangs", Kind: LiveDeclaredTargetDirectory},
	} {
		if target.Identity == identity {
			return target.Kind, true
		}
	}
	return "", false
}
