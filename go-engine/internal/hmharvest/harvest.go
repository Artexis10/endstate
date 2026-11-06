// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package hmharvest refreshes the release-owned Home Manager adapter registry
// from the exact pinned docs-json and source tree. It is release tooling only;
// runtime discovery never imports or executes this package.
package hmharvest

import (
	"bytes"
	"context"
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
	"strconv"
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
	ProbeTargets(ctx context.Context, program string, values map[string]any) ([]string, error)
}

// Refresh observes pinned metadata on a deep copy. Only a complete result has
// a candidate; prior review fingerprints remain untouched until CLI acceptance.
func Refresh(ctx context.Context, registry *hmregistry.Registry, docsJSON []byte, sourceRoot string, inputs releaseinputs.Inputs, prober TargetProber) (result Result, returnErr error) {
	observation := Observation{Field: SchemaVersionField}
	defer func() {
		result.Report.sort()
		if returnErr != nil {
			result.Candidate = nil
			result.Report.Stopped = &observation
			returnErr = &ObservationError{Observation: observation, Err: returnErr}
		}
	}()
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if registry == nil {
		return result, fmt.Errorf("Home Manager registry is nil")
	}
	data, err := json.Marshal(registry)
	if err != nil {
		return result, fmt.Errorf("copy Home Manager registry: %w", err)
	}
	var candidate hmregistry.Registry
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&candidate); err != nil {
		return result, fmt.Errorf("copy Home Manager registry: %w", err)
	}
	observation = Observation{Field: DocsHashField}
	var rawOptions map[string]json.RawMessage
	if err := json.Unmarshal(docsJSON, &rawOptions); err != nil {
		return result, fmt.Errorf("parse pinned Home Manager docs-json: %w", err)
	}
	if rawOptions == nil {
		return result, fmt.Errorf("pinned Home Manager docs-json must be an option object")
	}
	candidate.SchemaVersion = hmregistry.SchemaVersion
	result.Report.change("", SchemaVersionField, "", present(strconv.Itoa(registry.SchemaVersion)), present(strconv.Itoa(candidate.SchemaVersion)))
	candidate.Inputs = hmregistry.InputIdentity{
		NixpkgsRevision: inputs.Nixpkgs.Revision, HomeManagerRevision: inputs.HomeManager.Revision,
	}
	result.Report.change("", NixpkgsField, "", present(registry.Inputs.NixpkgsRevision), present(candidate.Inputs.NixpkgsRevision))
	result.Report.change("", HomeManagerField, "", present(registry.Inputs.HomeManagerRevision), present(candidate.Inputs.HomeManagerRevision))
	docsHash := sha256.Sum256(docsJSON)
	candidate.DocsJSONSHA256 = hex.EncodeToString(docsHash[:])
	result.Report.change("", DocsHashField, "", present(registry.DocsJSONSHA256), present(candidate.DocsJSONSHA256))
	candidate.Programs = programIndex(rawOptions)
	result.Report.members("", ProgramField, registry.Programs, candidate.Programs)

	observation = Observation{Field: SourceHashField, Subject: "."}
	root, err := filepath.Abs(sourceRoot)
	if err != nil {
		return result, fmt.Errorf("resolve Home Manager source root: %w", err)
	}
	info, err := os.Stat(root)
	if err != nil {
		return result, fmt.Errorf("Home Manager source root is unavailable: %w", err)
	}
	if !info.IsDir() {
		return result, fmt.Errorf("Home Manager source root is not a directory")
	}

	sort.Slice(candidate.Entries, func(i, j int) bool { return candidate.Entries[i].ID < candidate.Entries[j].ID })
	for entryIndex := range candidate.Entries {
		entry := &candidate.Entries[entryIndex]
		observation = Observation{Adapter: entry.ID, Field: OptionField}
		if err := ctx.Err(); err != nil {
			return result, err
		}
		sort.Slice(entry.Options, func(i, j int) bool { return entry.Options[i].Name < entry.Options[j].Name })
		sort.Slice(entry.Targets, func(i, j int) bool { return entry.Targets[i].Coordinate < entry.Targets[j].Coordinate })
		sourceHashes := make(map[string]string)
		for optionIndex := range entry.Options {
			option := &entry.Options[optionIndex]
			observation = Observation{Adapter: entry.ID, Field: OptionField, Subject: option.Name}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			raw, ok := rawOptions[option.Name]
			if !ok {
				result.Report.change(entry.ID, OptionField, option.Name, present(option.Name), Value{})
				return result, fmt.Errorf("tracked option is absent from pinned docs-json")
			}
			var documented docsOption
			if err := json.Unmarshal(raw, &documented); err != nil {
				return result, fmt.Errorf("decode Home Manager option: %w", err)
			}
			if strings.TrimSpace(documented.Type) == "" || len(documented.Declarations) == 0 {
				return result, fmt.Errorf("Home Manager option has no type or declaration")
			}
			result.Report.change(entry.ID, OptionTypeField, option.Name, present(option.Type), present(documented.Type))
			option.Type = documented.Type
			observation.Field = DeclarationField
			declarations := make([]string, 0, len(documented.Declarations))
			for _, declaration := range documented.Declarations {
				relative, err := normalizeDeclaration(declaration.Name)
				if err != nil {
					return result, err
				}
				declarations = append(declarations, relative)
			}
			sort.Strings(declarations)
			declarations = uniqueStrings(declarations)
			before, _ := json.Marshal(option.Declarations)
			after, _ := json.Marshal(declarations)
			result.Report.change(entry.ID, DeclarationField, option.Name, present(string(before)), present(string(after)))
			option.Declarations = declarations
			for _, declaration := range option.Declarations {
				if _, exists := sourceHashes[declaration]; exists {
					continue
				}
				observation = Observation{Adapter: entry.ID, Field: SourceHashField, Subject: declaration}
				hash, err := hashSourceAuthority(root, declaration)
				if err != nil {
					return result, err
				}
				old, existed := entry.SourceHashes[declaration]
				result.Report.change(entry.ID, SourceHashField, declaration, Value{Present: existed, Value: old}, present(hash))
				sourceHashes[declaration] = hash
			}
		}
		for declaration, hash := range entry.SourceHashes {
			if _, exists := sourceHashes[declaration]; !exists {
				result.Report.change(entry.ID, SourceHashField, declaration, present(hash), Value{})
			}
		}
		entry.SourceHashes = sourceHashes
		if entry.Disposition != hmregistry.Excluded {
			observation = Observation{Adapter: entry.ID, Field: ProbeTargetField, Subject: entry.Program}
			if prober == nil {
				return result, fmt.Errorf("Home Manager adapter requires a pure target prober")
			}
			if entry.Probe == nil {
				return result, fmt.Errorf("Home Manager adapter has no reviewed target probe")
			}
			observed, err := prober.ProbeTargets(ctx, entry.Program, entry.Probe.Values)
			if err != nil {
				return result, err
			}
			if err := ctx.Err(); err != nil {
				return result, err
			}
			coordinates, err := normalizeProbeTargets(observed)
			if err != nil {
				return result, err
			}
			result.Report.members(entry.ID, ProbeTargetField, entry.Probe.Targets, coordinates)
			entry.Probe.Targets = coordinates
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
					entry.Targets = append(entry.Targets, hmregistry.Target{Coordinate: coordinate, Optional: optional})
					before := Value{}
					if existed {
						before = present("optional=" + strconv.FormatBool(optionalByCoordinate[coordinate]))
					}
					result.Report.change(entry.ID, TargetField, coordinate, before, present("optional="+strconv.FormatBool(optional)))
					delete(optionalByCoordinate, coordinate)
				}
				for coordinate, optional := range optionalByCoordinate {
					result.Report.change(entry.ID, TargetField, coordinate, present("optional="+strconv.FormatBool(optional)), Value{})
				}
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Report.Complete = true
	result.Candidate = &candidate
	return result, nil
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
