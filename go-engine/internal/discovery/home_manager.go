// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
)

const (
	HomeManagerSettingsSource      = "home-manager-live-settings"
	maxHomeManagerSettingsFileSize = settingscodec.MaxContentSize
)

var errSettingsContentPolicy = errors.New("settings content is outside the reviewed portable policy")

// HomeManagerAdapter observes reviewed registry targets and collects only
// settings authorized by the bound Linux generation. It never calls
// Nix, Home Manager, a shell, or the user configuration graph.
type HomeManagerAdapter struct {
	Registry     *hmregistry.Registry
	Modules      map[string]*modules.Module
	Packages     []*packagecatalog.Entry
	Environment  config.PathEnvironment
	FS           homeManagerFilesystem
	MetadataOnly bool
}

type homeManagerFile interface {
	io.Reader
	io.Closer
	Stat() (os.FileInfo, error)
}

type homeManagerFilesystem interface {
	Lstat(string) (os.FileInfo, error)
	Open(string) (homeManagerFile, error)
}

type hostHomeManagerFilesystem struct{}

func (hostHomeManagerFilesystem) Lstat(path string) (os.FileInfo, error)    { return os.Lstat(path) }
func (hostHomeManagerFilesystem) Open(path string) (homeManagerFile, error) { return os.Open(path) }

func (HomeManagerAdapter) ID() string { return HomeManagerSettingsSource }

func (adapter HomeManagerAdapter) Inventory(ctx context.Context) (AdapterInventory, error) {
	if adapter.Registry == nil {
		return AdapterInventory{}, ErrNotAvailable
	}
	if adapter.Environment.GOOS != "linux" {
		return AdapterInventory{}, ErrNotAvailable
	}
	filesystem := adapter.FS
	if filesystem == nil {
		filesystem = hostHomeManagerFilesystem{}
	}
	result := AdapterInventory{}
	authorities := hmregistry.Join(adapter.Registry, adapter.Modules, adapter.Packages)
	for _, entry := range adapter.Registry.Entries {
		if err := ctx.Err(); err != nil {
			return AdapterInventory{}, err
		}
		authority := hmregistry.Authority{}
		for _, candidate := range authorities {
			if candidate.AdapterID == entry.ID && (candidate.ModuleID == entry.ModuleID || entry.ModuleID == "") {
				authority = candidate
				break
			}
		}
		candidateID := entry.ModuleID
		if candidateID == "" {
			candidateID = "home-manager." + entry.ID
		}
		plans := make([]SettingsFilePlan, 0, len(entry.Targets))
		evidence := make([]Evidence, 0, len(entry.Targets))
		for _, target := range entry.Targets {
			source, err := config.ResolveEnginePath(target.Coordinate, adapter.Environment)
			if err != nil {
				result.Warnings = append(result.Warnings, Warning{Code: "settings_path_unavailable", Source: HomeManagerSettingsSource, ItemID: entry.ID, Message: "A reviewed settings coordinate could not be resolved."})
				continue
			}
			info, err := filesystem.Lstat(source)
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				result.Ignored++
				result.Warnings = append(result.Warnings, Warning{
					Code: "settings_path_unreadable", Source: HomeManagerSettingsSource, ItemID: entry.ID,
					Message: "A reviewed settings path exists but could not be inspected.",
				})
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxHomeManagerSettingsFileSize {
				result.Ignored++
				result.Warnings = append(result.Warnings, Warning{
					Code: "settings_path_unsafe", Source: HomeManagerSettingsSource, ItemID: entry.ID,
					Message: "A reviewed settings path was skipped because it is linked, non-regular, or too large.",
				})
				continue
			}
			evidence = append(evidence, Evidence{Source: HomeManagerSettingsSource, Kind: EvidenceConfig, Ref: target.Coordinate})
			if adapter.MetadataOnly || !authority.Supports(hmregistry.Capture) {
				continue
			}
			observedHash, err := observeHomeManagerSettingsFile(filesystem, source, info, entry.Codec)
			if err != nil {
				result.Ignored++
				code := "settings_path_unreadable"
				message := "A reviewed settings path exists but could not be read consistently."
				if errors.Is(err, errSettingsContentPolicy) {
					code = "settings_content_unsupported"
					message = "This settings file contains arguments outside its reviewed portable policy."
				}
				result.Warnings = append(result.Warnings, Warning{
					Code: code, Source: HomeManagerSettingsSource, ItemID: entry.ID, Message: message,
				})
				continue
			}
			plans = append(plans, SettingsFilePlan{
				CandidateID: candidateID, AdapterID: HomeManagerSettingsSource,
				Realization: authority.Realization,
				Codec:       entry.Codec, Target: target.Coordinate, Source: source, Optional: target.Optional,
				ObservedSize: info.Size(), ObservedSHA256: observedHash,
			})
		}
		if len(evidence) == 0 {
			continue
		}
		actions := make([]SettingsAction, 0, len(authority.Actions))
		if len(plans) > 0 || adapter.MetadataOnly {
			for _, action := range authority.Actions {
				actions = append(actions, SettingsAction(action))
			}
		}
		if authority.Reason != "" {
			result.Warnings = append(result.Warnings, Warning{Code: authority.Reason, Source: HomeManagerSettingsSource, ItemID: entry.ID, Message: "This settings candidate is unavailable for automatic capture and restore under the current adapter authority."})
		}
		result.Settings = append(result.Settings, SettingsCandidate{
			ID: candidateID, ModuleID: entry.ModuleID, DisplayName: entry.DisplayName + " settings",
			PackageID: entry.PackageID, Portability: PortabilityConfigOnly,
			Actions:  actions,
			Evidence: evidence,
		})
		result.SettingsFiles = append(result.SettingsFiles, plans...)
	}
	sort.Slice(result.Settings, func(i, j int) bool { return result.Settings[i].ID < result.Settings[j].ID })
	sort.Slice(result.SettingsFiles, func(i, j int) bool {
		if result.SettingsFiles[i].CandidateID != result.SettingsFiles[j].CandidateID {
			return result.SettingsFiles[i].CandidateID < result.SettingsFiles[j].CandidateID
		}
		return result.SettingsFiles[i].Target < result.SettingsFiles[j].Target
	})
	return result, nil
}

func observeHomeManagerSettingsFile(filesystem homeManagerFilesystem, source string, before os.FileInfo, codec string) (string, error) {
	if _, err := safepath.Resolve(filepath.Dir(source), filepath.Base(source)); err != nil {
		return "", err
	}
	file, err := filesystem.Open(source)
	if err != nil {
		return "", err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) || opened.Size() != before.Size() {
		return "", fmt.Errorf("settings source changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxHomeManagerSettingsFileSize+1))
	if err != nil {
		return "", err
	}
	if int64(len(data)) > maxHomeManagerSettingsFileSize || int64(len(data)) != opened.Size() {
		return "", fmt.Errorf("settings source changed or exceeded the read bound")
	}
	if err := settingscodec.Validate(codec, data); err != nil {
		return "", errSettingsContentPolicy
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
