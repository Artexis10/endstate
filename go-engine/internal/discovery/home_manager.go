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
	"sort"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
)

const (
	HomeManagerSettingsSource            = "home-manager-live-settings"
	maxHomeManagerSettingsFileSize int64 = 1 << 20
)

// HomeManagerAdapter inverts only the release-reviewed file targets in the
// frozen Home Manager registry. It reads live files directly and never calls
// Nix, Home Manager, a shell, or the user configuration graph.
type HomeManagerAdapter struct {
	Registry    *hmregistry.Registry
	Environment config.PathEnvironment
	FS          homeManagerFilesystem
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
	for _, entry := range adapter.Registry.Entries {
		if err := ctx.Err(); err != nil {
			return AdapterInventory{}, err
		}
		if entry.Disposition == hmregistry.Excluded {
			continue
		}
		if !runtimeHomeManagerCodecSupported(entry.Disposition, entry.Codec) {
			result.Ignored++
			result.Warnings = append(result.Warnings, Warning{
				Code: "settings_codec_unavailable", Source: HomeManagerSettingsSource, ItemID: entry.ID,
				Message: "This reviewed settings surface requires a codec that is not available in this engine.",
			})
			continue
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
				return AdapterInventory{}, err
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
			observedHash, err := observeHomeManagerSettingsFile(filesystem, source, info)
			if err != nil {
				result.Ignored++
				result.Warnings = append(result.Warnings, Warning{
					Code: "settings_path_unreadable", Source: HomeManagerSettingsSource, ItemID: entry.ID,
					Message: "A reviewed settings path exists but could not be read consistently.",
				})
				continue
			}
			plans = append(plans, SettingsFilePlan{
				CandidateID: candidateID, AdapterID: HomeManagerSettingsSource,
				Codec: entry.Codec, Target: target.Coordinate, Source: source, Optional: target.Optional,
				ObservedSize: info.Size(), ObservedSHA256: observedHash,
			})
			evidence = append(evidence, Evidence{Source: HomeManagerSettingsSource, Kind: EvidenceConfig, Ref: target.Coordinate})
		}
		if len(plans) == 0 {
			continue
		}
		result.Settings = append(result.Settings, SettingsCandidate{
			ID: candidateID, ModuleID: entry.ModuleID, DisplayName: entry.DisplayName + " settings",
			PackageID: entry.PackageID, Portability: PortabilityConfigOnly,
			Actions:  []SettingsAction{SettingsCapture, SettingsRestore, SettingsVerify, SettingsRevert},
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

func runtimeHomeManagerCodecSupported(disposition hmregistry.Disposition, codec string) bool {
	return (disposition == hmregistry.FileRoundTrip && codec == settingscodec.BoundedRegularFileV1) ||
		(disposition == hmregistry.CuratedCodec && codec == settingscodec.GitConfigSafeV1)
}

func observeHomeManagerSettingsFile(filesystem homeManagerFilesystem, source string, before os.FileInfo) (string, error) {
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
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
