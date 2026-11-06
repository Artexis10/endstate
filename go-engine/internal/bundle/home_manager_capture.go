// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
)

const maxHomeManagerCaptureFileSize = settingscodec.MaxContentSize

// SettingsFileCapturePlan joins a frozen portable target to one live host
// source. Source is execution-only and is replaced by a bundle-relative ref.
type SettingsFileCapturePlan struct {
	CandidateID    string
	Codec          string
	Target         string
	Source         string
	Optional       bool
	ObservedSize   int64
	ObservedSHA256 string
}

func validateHomeManagerCapturePlan(plan SettingsFileCapturePlan) error {
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

func readStableHomeManagerSource(plan SettingsFileCapturePlan) ([]byte, error) {
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
