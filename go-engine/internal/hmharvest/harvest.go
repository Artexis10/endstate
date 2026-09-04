// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package hmharvest refreshes the release-owned Home Manager adapter registry
// from the exact pinned docs-json and source tree. It is release tooling only;
// runtime discovery never imports or executes this package.
package hmharvest

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

type docsOption struct {
	Type         string `json:"type"`
	Declarations []struct {
		Name string `json:"name"`
	} `json:"declarations"`
}

const probeHome = "/home/endstate-probe"

// TargetProber evaluates one minimal program configuration against the exact
// pinned Home Manager input and returns only home.file targets added above the
// unconfigured baseline. Runtime capture never supplies or invokes a prober.
type TargetProber interface {
	ProbeTargets(program string, values map[string]any) ([]string, error)
}

// Refresh replaces every harvested fact, evaluated target, and review
// fingerprint while preserving the human-reviewed disposition, codec, and
// exclusions. A normal check exposes these replacements as registry drift;
// only the CLI's explicit review-accepting write mode persists them.
func Refresh(registry *hmregistry.Registry, docsJSON []byte, sourceRoot string, inputs releaseinputs.Inputs, prober TargetProber) error {
	if registry == nil {
		return fmt.Errorf("Home Manager registry is nil")
	}
	var rawOptions map[string]json.RawMessage
	if err := json.Unmarshal(docsJSON, &rawOptions); err != nil {
		return fmt.Errorf("parse pinned Home Manager docs-json: %w", err)
	}
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return fmt.Errorf("resolve Home Manager source root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("Home Manager source root is unavailable")
	}

	registry.SchemaVersion = hmregistry.SchemaVersion
	registry.Inputs = hmregistry.InputIdentity{
		NixpkgsRevision: inputs.Nixpkgs.Revision, HomeManagerRevision: inputs.HomeManager.Revision,
	}
	docsHash := sha256.Sum256(docsJSON)
	registry.DocsJSONSHA256 = hex.EncodeToString(docsHash[:])
	registry.Programs = programIndex(rawOptions)

	for entryIndex := range registry.Entries {
		entry := &registry.Entries[entryIndex]
		sort.Slice(entry.Options, func(i, j int) bool { return entry.Options[i].Name < entry.Options[j].Name })
		sort.Slice(entry.Targets, func(i, j int) bool { return entry.Targets[i].Coordinate < entry.Targets[j].Coordinate })
		sourceHashes := make(map[string]string)
		for optionIndex := range entry.Options {
			option := &entry.Options[optionIndex]
			raw, ok := rawOptions[option.Name]
			if !ok {
				return fmt.Errorf("Home Manager adapter %q option %q is absent from pinned docs-json", entry.ID, option.Name)
			}
			var documented docsOption
			if err := json.Unmarshal(raw, &documented); err != nil {
				return fmt.Errorf("decode Home Manager option %q: %w", option.Name, err)
			}
			if strings.TrimSpace(documented.Type) == "" || len(documented.Declarations) == 0 {
				return fmt.Errorf("Home Manager option %q has no type or declaration", option.Name)
			}
			option.Type = documented.Type
			option.Declarations = option.Declarations[:0]
			for _, declaration := range documented.Declarations {
				relative, err := normalizeDeclaration(declaration.Name)
				if err != nil {
					return fmt.Errorf("Home Manager option %q: %w", option.Name, err)
				}
				option.Declarations = append(option.Declarations, relative)
			}
			sort.Strings(option.Declarations)
			option.Declarations = uniqueStrings(option.Declarations)
			for _, declaration := range option.Declarations {
				if _, exists := sourceHashes[declaration]; exists {
					continue
				}
				hash, err := hashSourceAuthority(root, declaration)
				if err != nil {
					return fmt.Errorf("Home Manager option %q declaration %q: %w", option.Name, declaration, err)
				}
				sourceHashes[declaration] = hash
			}
		}
		entry.SourceHashes = sourceHashes
		if entry.Disposition != hmregistry.Excluded {
			if prober == nil {
				return fmt.Errorf("Home Manager adapter %q requires a pure target prober", entry.ID)
			}
			if entry.Probe == nil {
				return fmt.Errorf("Home Manager adapter %q has no reviewed target probe", entry.ID)
			}
			observed, err := prober.ProbeTargets(entry.Program, entry.Probe.Values)
			if err != nil {
				return fmt.Errorf("probe Home Manager adapter %q targets: %w", entry.ID, err)
			}
			coordinates, err := normalizeProbeTargets(observed)
			if err != nil {
				return fmt.Errorf("probe Home Manager adapter %q targets: %w", entry.ID, err)
			}
			entry.Probe.Targets = append(entry.Probe.Targets[:0], coordinates...)
			if entry.Disposition == hmregistry.FileRoundTrip {
				optionalByCoordinate := make(map[string]bool, len(entry.Targets))
				for _, target := range entry.Targets {
					optionalByCoordinate[target.Coordinate] = target.Optional
				}
				entry.Targets = make([]hmregistry.Target, 0, len(coordinates))
				for _, coordinate := range coordinates {
					optional, existed := optionalByCoordinate[coordinate]
					if !existed {
						optional = true
					}
					entry.Targets = append(entry.Targets, hmregistry.Target{
						Coordinate: coordinate,
						Optional:   optional,
					})
				}
			}
		}
	}
	if err := hmregistry.AcceptReviews(registry); err != nil {
		return err
	}
	return hmregistry.Validate(registry, inputs)
}

func programIndex(options map[string]json.RawMessage) []string {
	programs := make(map[string]bool)
	for name := range options {
		parts := strings.Split(name, ".")
		if len(parts) == 3 && parts[0] == "programs" && parts[2] == "enable" && parts[1] != "" {
			programs[parts[1]] = true
		}
	}
	result := make([]string, 0, len(programs))
	for program := range programs {
		result = append(result, program)
	}
	sort.Strings(result)
	return result
}

func normalizeProbeTargets(targets []string) ([]string, error) {
	coordinates := make([]string, 0, len(targets))
	seen := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target == "" || strings.ContainsRune(target, '\x00') {
			return nil, fmt.Errorf("target is empty or contains NUL")
		}
		clean := path.Clean(target)
		if clean != target {
			return nil, fmt.Errorf("target %q is not canonical", target)
		}
		relative := clean
		if path.IsAbs(clean) {
			prefix := probeHome + "/"
			if !strings.HasPrefix(clean, prefix) {
				return nil, fmt.Errorf("target %q is outside probe home", target)
			}
			relative = strings.TrimPrefix(clean, prefix)
		}
		if relative == "." || relative == ".." || strings.HasPrefix(relative, "../") {
			return nil, fmt.Errorf("target %q escapes probe home", target)
		}

		coordinate := "${home}/" + relative
		for _, root := range []struct {
			path       string
			coordinate string
		}{
			{path: ".config/", coordinate: "${xdg.config}/"},
			{path: ".local/share/", coordinate: "${xdg.data}/"},
			{path: ".local/state/", coordinate: "${xdg.state}/"},
		} {
			if strings.HasPrefix(relative, root.path) {
				coordinate = root.coordinate + strings.TrimPrefix(relative, root.path)
				break
			}
		}
		if seen[coordinate] {
			continue
		}
		seen[coordinate] = true
		coordinates = append(coordinates, coordinate)
	}
	sort.Strings(coordinates)
	return coordinates, nil
}

func normalizeDeclaration(name string) (string, error) {
	const prefix = "<home-manager/"
	if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ">") {
		return "", fmt.Errorf("declaration %q is not owned by Home Manager", name)
	}
	relative := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ">")
	clean := filepath.ToSlash(filepath.Clean(relative))
	if clean != relative || !strings.HasPrefix(clean, "modules/") {
		return "", fmt.Errorf("declaration %q is not a safe module path", name)
	}
	return clean, nil
}

func hashSourceAuthority(root, relative string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(relative))
	contained, err := filepath.Rel(root, target)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("source authority escapes the pinned tree")
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("source authority is a symbolic link")
	}
	if info.Mode().IsRegular() {
		data, err := os.ReadFile(target)
		if err != nil {
			return "", err
		}
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:]), nil
	}
	if !info.IsDir() {
		return "", fmt.Errorf("source authority is not a regular file or directory")
	}

	type sourceFile struct {
		name string
		data []byte
	}
	files := make([]sourceFile, 0)
	err = filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("source directory contains a symbolic link")
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("source directory contains a non-regular file")
		}
		relativeName, err := filepath.Rel(target, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files = append(files, sourceFile{name: filepath.ToSlash(relativeName), data: data})
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
	hash := sha256.New()
	_, _ = hash.Write([]byte("endstate-home-manager-source-directory-v1\x00"))
	var length [8]byte
	for _, file := range files {
		binary.BigEndian.PutUint64(length[:], uint64(len(file.name)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(file.name))
		binary.BigEndian.PutUint64(length[:], uint64(len(file.data)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write(file.data)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func uniqueStrings(values []string) []string {
	result := values[:0]
	for _, value := range values {
		if len(result) == 0 || result[len(result)-1] != value {
			result = append(result, value)
		}
	}
	return result
}
