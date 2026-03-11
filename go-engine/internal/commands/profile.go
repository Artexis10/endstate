// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// ProfileFlags holds the parsed CLI flags for profile subcommands.
type ProfileFlags struct {
	// Subcommand is the profile subcommand: "list", "path", or "validate".
	Subcommand string
	// Args holds remaining positional arguments after the subcommand.
	Args []string
	// Events controls streaming event output. "jsonl" enables it; "" disables.
	Events string
}

// ---------------------------------------------------------------------------
// Profile List
// ---------------------------------------------------------------------------

// ProfileListResult is the data payload for the "profile list" subcommand.
type ProfileListResult struct {
	Profiles []ProfileEntry `json:"profiles"`
}

// ProfileEntry describes a single profile discovered in the profiles directory.
type ProfileEntry struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	AppCount    int    `json:"appCount"`
	Valid       bool   `json:"valid"`
}

// ---------------------------------------------------------------------------
// Profile Path
// ---------------------------------------------------------------------------

// ProfilePathResult is the data payload for the "profile path" subcommand.
type ProfilePathResult struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
}

// ---------------------------------------------------------------------------
// Profile Validate
// ---------------------------------------------------------------------------

// ProfileValidateResult is the data payload for the "profile validate" subcommand.
type ProfileValidateResult struct {
	Valid   bool                       `json:"valid"`
	Errors  []manifest.ValidationError `json:"errors"`
	Summary ProfileValidateSummary     `json:"summary"`
}

// ProfileValidateSummary provides a quick overview of the manifest contents.
type ProfileValidateSummary struct {
	AppCount   int  `json:"appCount"`
	HasRestore bool `json:"hasRestore"`
	HasVerify  bool `json:"hasVerify"`
}

// ---------------------------------------------------------------------------
// Router
// ---------------------------------------------------------------------------

// RunProfile dispatches to the appropriate profile subcommand handler.
func RunProfile(flags ProfileFlags) (interface{}, *envelope.Error) {
	switch flags.Subcommand {
	case "list":
		return runProfileList()
	case "path":
		if len(flags.Args) == 0 {
			return nil, envelope.NewError(envelope.ErrInternalError, "profile path requires a name argument")
		}
		return runProfilePath(flags.Args[0])
	case "validate":
		if len(flags.Args) == 0 {
			return nil, envelope.NewError(envelope.ErrInternalError, "profile validate requires a path argument")
		}
		return runProfileValidate(flags.Args[0])
	default:
		return nil, envelope.NewError(envelope.ErrInternalError, "unknown profile subcommand: "+flags.Subcommand)
	}
}

// ---------------------------------------------------------------------------
// Profile List implementation
// ---------------------------------------------------------------------------

// runProfileList lists all profiles found in the configured profiles directory.
func runProfileList() (interface{}, *envelope.Error) {
	return runProfileListFromDir(config.ProfileDir())
}

// runProfileListFromDir is the testable implementation of profile list that
// accepts the profiles directory as a parameter.
func runProfileListFromDir(dir string) (interface{}, *envelope.Error) {
	if dir == "" {
		return &ProfileListResult{Profiles: []ProfileEntry{}}, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		// Directory doesn't exist or can't be read — return empty list.
		return &ProfileListResult{Profiles: []ProfileEntry{}}, nil
	}

	var profiles []ProfileEntry

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		ext := filepath.Ext(name)

		// Only consider profile extensions: .json, .jsonc, .json5
		if ext != ".json" && ext != ".jsonc" && ext != ".json5" {
			continue
		}

		// Skip meta.json files — they are not profiles.
		if strings.HasSuffix(name, ".meta.json") {
			continue
		}

		fullPath := filepath.Join(dir, name)
		stem := strings.TrimSuffix(name, ext)

		// Validate the profile
		vResult := manifest.ValidateProfile(fullPath)

		pe := ProfileEntry{
			Path:  fullPath,
			Name:  stem,
			Valid: vResult.Valid,
		}

		// If valid, load the manifest to get name and app count
		if vResult.Valid {
			if mf, loadErr := manifest.LoadManifest(fullPath); loadErr == nil {
				pe.AppCount = len(mf.Apps)
				// Use manifest name for display label if no meta.json
				if mf.Name != "" {
					pe.DisplayName = mf.Name
				}
			}
		}

		// Check for meta.json displayName (highest priority)
		if metaName := readMetaDisplayName(fullPath); metaName != "" {
			pe.DisplayName = metaName
		}

		// Fallback: use filename stem if no display name resolved
		if pe.DisplayName == "" {
			pe.DisplayName = stem
		}

		profiles = append(profiles, pe)
	}

	// Sort by name for deterministic output
	sort.Slice(profiles, func(i, j int) bool {
		return profiles[i].Name < profiles[j].Name
	})

	// Ensure non-nil slice in JSON output
	if profiles == nil {
		profiles = []ProfileEntry{}
	}

	return &ProfileListResult{Profiles: profiles}, nil
}

// metaFile is the JSON structure for <profile>.meta.json files.
type metaFile struct {
	DisplayName string `json:"displayName"`
}

// readMetaDisplayName reads the displayName from the corresponding .meta.json
// file for a profile. For "work.jsonc" it looks for "work.meta.json". Returns
// an empty string if the meta file does not exist or lacks a displayName field.
func readMetaDisplayName(profilePath string) string {
	ext := filepath.Ext(profilePath)
	metaPath := strings.TrimSuffix(profilePath, ext) + ".meta.json"

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return ""
	}

	var meta metaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	return meta.DisplayName
}

// ---------------------------------------------------------------------------
// Profile Path implementation
// ---------------------------------------------------------------------------

// runProfilePath resolves the filesystem path for a named profile.
func runProfilePath(name string) (interface{}, *envelope.Error) {
	return runProfilePathFromDir(config.ProfileDir(), name)
}

// runProfilePathFromDir is the testable implementation of profile path that
// accepts the profiles directory as a parameter. It checks for profiles in the
// resolution order defined in profile-contract.md.
func runProfilePathFromDir(dir, name string) (interface{}, *envelope.Error) {
	// Profile path resolution order per profile-contract.md:
	// 1. <dir>/<name>.zip
	// 2. <dir>/<name>/manifest.jsonc (loose folder)
	// 3. <dir>/<name>.jsonc
	// 4. <dir>/<name>.json
	// 5. <dir>/<name>.json5
	candidates := []string{
		filepath.Join(dir, name+".zip"),
		filepath.Join(dir, name, "manifest.jsonc"),
		filepath.Join(dir, name+".jsonc"),
		filepath.Join(dir, name+".json"),
		filepath.Join(dir, name+".json5"),
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return &ProfilePathResult{
				Path:   candidate,
				Exists: true,
			}, nil
		}
	}

	// None found: return the expected .jsonc path with exists=false
	return &ProfilePathResult{
		Path:   filepath.Join(dir, name+".jsonc"),
		Exists: false,
	}, nil
}

// ---------------------------------------------------------------------------
// Profile Validate implementation
// ---------------------------------------------------------------------------

// runProfileValidate validates a manifest file and returns detailed results.
func runProfileValidate(path string) (interface{}, *envelope.Error) {
	vResult := manifest.ValidateProfile(path)

	result := &ProfileValidateResult{
		Valid:  vResult.Valid,
		Errors: vResult.Errors,
	}

	// Ensure non-nil errors slice for JSON output
	if result.Errors == nil {
		result.Errors = []manifest.ValidationError{}
	}

	// If valid, load the manifest to get summary info.
	if vResult.Valid {
		if mf, err := manifest.LoadManifest(path); err == nil {
			result.Summary.AppCount = len(mf.Apps)
			result.Summary.HasRestore = len(mf.Restore) > 0
			result.Summary.HasVerify = len(mf.Verify) > 0
		}
	}

	return result, nil
}
