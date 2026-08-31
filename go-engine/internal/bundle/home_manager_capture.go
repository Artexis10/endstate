// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const maxHomeManagerCaptureFileSize int64 = 1 << 20

// HomeManagerFileCapturePlan joins a frozen portable target to one live host
// source. Source is execution-only and is replaced by a bundle-relative ref.
type HomeManagerFileCapturePlan struct {
	CandidateID    string
	Codec          string
	Target         string
	Source         string
	Optional       bool
	ObservedSize   int64
	ObservedSHA256 string
}

func stageHomeManagerFiles(base *manifest.Manifest, plans []HomeManagerFileCapturePlan, stagingRoot string, context *validationmode.Context) (int, []string, error) {
	if len(plans) == 0 {
		return 0, nil, nil
	}
	if base == nil {
		return 0, nil, fmt.Errorf("capture bundle: Home Manager file capture has no manifest")
	}
	if base.HomeManager != nil && (base.HomeManager.Flake != "" || base.HomeManager.Config != "") {
		return 0, []string{"Live settings were detected but not added because the captured manifest already names a Home Manager configuration owned outside the settings catalog."}, nil
	}

	ordered := append([]HomeManagerFileCapturePlan(nil), plans...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Target != ordered[j].Target {
			return ordered[i].Target < ordered[j].Target
		}
		if ordered[i].CandidateID != ordered[j].CandidateID {
			return ordered[i].CandidateID < ordered[j].CandidateID
		}
		return ordered[i].Source < ordered[j].Source
	})
	for index, plan := range ordered {
		if err := validateHomeManagerCapturePlan(plan); err != nil {
			return 0, nil, err
		}
		if index > 0 && ordered[index-1].Target == plan.Target {
			return 0, nil, fmt.Errorf("capture bundle: duplicate Home Manager target %q", plan.Target)
		}
	}

	createdHomeManager := base.HomeManager == nil
	if createdHomeManager {
		base.HomeManager = &manifest.HomeManagerConfig{}
	}
	if base.HomeManager.Settings == nil {
		base.HomeManager.Settings = &manifest.HomeManagerSettings{}
	}
	if base.HomeManager.Settings.Files == nil {
		base.HomeManager.Settings.Files = make(map[string]string)
	}

	included := 0
	warnings := make([]string, 0)
	for _, plan := range ordered {
		data, err := readStableHomeManagerSource(plan)
		if os.IsNotExist(err) && plan.Optional {
			warnings = append(warnings, "A detected optional settings file disappeared before it could be bundled.")
			continue
		}
		if err != nil {
			return 0, nil, err
		}
		data, err = settingscodec.Transform(plan.Codec, plan.Target, data)
		if err != nil {
			return 0, nil, fmt.Errorf("capture bundle: transform Home Manager settings: %w", err)
		}
		relative := homeManagerStagedPath(plan)
		destination, err := resolveCapturePortable(context, plan.CandidateID, "homeManager.settings.files", stagingRoot, relative)
		if err != nil {
			return 0, nil, err
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return 0, nil, fmt.Errorf("capture bundle: create Home Manager settings directory: %w", err)
		}
		if err := ensureNoLinksInExistingPath(filepath.Dir(destination)); err != nil {
			return 0, nil, fmt.Errorf("capture bundle: unsafe Home Manager settings destination: %w", err)
		}
		if err := os.WriteFile(destination, data, 0o600); err != nil {
			return 0, nil, fmt.Errorf("capture bundle: stage Home Manager settings: %w", err)
		}
		base.HomeManager.Settings.Files[plan.Target] = "./" + relative
		included++
	}
	if included == 0 && createdHomeManager {
		base.HomeManager = nil
	}
	sort.Strings(warnings)
	return included, warnings, nil
}

func validateHomeManagerCapturePlan(plan HomeManagerFileCapturePlan) error {
	if strings.TrimSpace(plan.CandidateID) == "" || sanitizeConfigDirSegment(plan.CandidateID) == "" {
		return fmt.Errorf("capture bundle: Home Manager settings candidate id is invalid")
	}
	if err := config.ValidateEnginePath(plan.Target, "linux"); err != nil {
		return fmt.Errorf("capture bundle: unsafe Home Manager target %q: %w", plan.Target, err)
	}
	if strings.TrimSpace(plan.Source) == "" || !filepath.IsAbs(plan.Source) {
		return fmt.Errorf("capture bundle: Home Manager settings source must be absolute")
	}
	if !settingscodec.Supported(plan.Codec) {
		return fmt.Errorf("capture bundle: Home Manager settings codec %q is not implemented", plan.Codec)
	}
	if !settingscodec.TargetSupported(plan.Codec, plan.Target) {
		return fmt.Errorf("capture bundle: Home Manager settings codec %q does not own target %q", plan.Codec, plan.Target)
	}
	if plan.ObservedSize < 0 || plan.ObservedSize > maxHomeManagerCaptureFileSize {
		return fmt.Errorf("capture bundle: Home Manager settings source exceeds the capture bound")
	}
	if plan.ObservedSHA256 != "" {
		decoded, err := hex.DecodeString(plan.ObservedSHA256)
		if err != nil || len(decoded) != sha256.Size || plan.ObservedSHA256 != strings.ToLower(plan.ObservedSHA256) {
			return fmt.Errorf("capture bundle: Home Manager settings source observation is not a lowercase SHA-256")
		}
	}
	return nil
}

func readStableHomeManagerSource(plan HomeManagerFileCapturePlan) ([]byte, error) {
	info, err := os.Lstat(plan.Source)
	if err != nil {
		return nil, fmt.Errorf("capture bundle: inspect Home Manager settings source: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Size() < 0 || info.Size() > maxHomeManagerCaptureFileSize {
		return nil, fmt.Errorf("capture bundle: Home Manager settings source is linked, non-regular, or too large")
	}
	if plan.ObservedSize > 0 && info.Size() != plan.ObservedSize {
		return nil, fmt.Errorf("capture bundle: Home Manager settings source changed after discovery")
	}
	file, err := os.Open(plan.Source)
	if err != nil {
		return nil, fmt.Errorf("capture bundle: open Home Manager settings source: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("capture bundle: Home Manager settings source changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxHomeManagerCaptureFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("capture bundle: read Home Manager settings source: %w", err)
	}
	if int64(len(data)) > maxHomeManagerCaptureFileSize || int64(len(data)) != opened.Size() {
		return nil, fmt.Errorf("capture bundle: Home Manager settings source changed or exceeded the capture bound")
	}
	if plan.ObservedSHA256 != "" {
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != plan.ObservedSHA256 {
			return nil, fmt.Errorf("capture bundle: Home Manager settings source changed after discovery")
		}
	}
	return data, nil
}

func homeManagerStagedPath(plan HomeManagerFileCapturePlan) string {
	id := sanitizeConfigDirSegment(strings.TrimPrefix(plan.CandidateID, "apps."))
	sum := sha256.Sum256([]byte(plan.Target))
	leaf := sanitizeConfigDirSegment(path.Base(strings.TrimRight(plan.Target, "/")))
	if leaf == "" {
		leaf = "settings"
	}
	return path.Join("configs", "home-manager", id, hex.EncodeToString(sum[:6])+"-"+leaf)
}
