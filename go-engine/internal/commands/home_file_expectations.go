// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

var readHomeFilesArtifact = safepath.ReadRegularFileBounded

func readHomeFileExpectations(resolution *realizer.InputResolution) (*provision.HomeFileExpectations, error) {
	if resolution == nil || len(resolution.Outputs) != 1 || len(resolution.OutputPaths) != 1 {
		return nil, fmt.Errorf("generated Home Manager activation has no single frozen output")
	}
	output := resolution.OutputPaths[resolution.Outputs[0]]
	if !filepath.IsAbs(output) {
		return nil, fmt.Errorf("generated Home Manager activation output is unavailable")
	}
	// Metadata is a bounded internal artifact, independent of declared file sizes.
	data, _, err := readHomeFilesArtifact(filepath.Join(output, "endstate-home-files.json"), 1<<20)
	if err != nil {
		return nil, fmt.Errorf("read frozen Home Manager file expectations: %w", err)
	}
	var expectations provision.HomeFileExpectations
	if err := json.Unmarshal(data, &expectations); err != nil {
		return nil, fmt.Errorf("invalid frozen Home Manager file expectations: %w", err)
	}
	return &expectations, nil
}

func checkHomeFileExpectations(expectations *provision.HomeFileExpectations) (reason, message string) {
	if expectations == nil {
		return "file_expectations_unavailable", "Declared file contents are unverified because the activation has no recorded file expectations"
	}
	if err := expectations.Validate(); err != nil {
		return "unreadable", "Recorded Home Manager file expectations are invalid"
	}
	for _, file := range expectations.Files {
		resolved, err := filepath.EvalSymlinks(file.Target)
		if err != nil {
			return homeFileReadFailure(file.Target, err)
		}
		// Home Manager uses symlink leaves. Hash the regular destination, then
		// recheck the link resolution without imposing the staging file's mode.
		sum, _, err := safepath.HashRegularFile(resolved, math.MaxInt64)
		if err != nil {
			return homeFileReadFailure(file.Target, err)
		}
		after, err := filepath.EvalSymlinks(file.Target)
		if err != nil {
			return homeFileReadFailure(file.Target, err)
		}
		if resolved != after {
			return driver.ReasonConfigDrift, fmt.Sprintf("Declared Home Manager file changed during verification: %s", file.Target)
		}
		if hex.EncodeToString(sum[:]) != file.SHA256 {
			return driver.ReasonConfigDrift, fmt.Sprintf("Declared Home Manager file contents differ: %s", file.Target)
		}
	}
	return "", fmt.Sprintf("The activation and %d declared files match recorded expectations; typed settings were not checked", len(expectations.Files))
}

func homeFileReadFailure(target string, err error) (reason, message string) {
	if os.IsNotExist(err) {
		return driver.ReasonMissing, fmt.Sprintf("Declared Home Manager file is missing: %s", target)
	}
	if errors.Is(err, safepath.ErrSourceChanged) {
		return driver.ReasonConfigDrift, fmt.Sprintf("Declared Home Manager file changed during verification: %s", target)
	}
	return "unreadable", fmt.Sprintf("Declared Home Manager file cannot be read: %s", target)
}
