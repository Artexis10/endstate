// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
)

// typedHomeSetting is one captured set that Home Manager renders through a
// program's settings option instead of placing its file (decision 30).
type typedHomeSetting struct {
	index   int
	target  string
	codec   *settingscodec.TypedCodec
	value   map[string]any
	setting nix.TypedSetting
}

// stageTypedHomeSetting decodes the staged canonical payload of one typed set. A
// non-empty reason means the set must not reach the generated module.
func stageTypedHomeSetting(authority hmregistry.Authority, item preparedConfigRestoreExecution) (*typedHomeSetting, planner.ResolutionReason, planner.TerminalStatus) {
	codec, ok := settingscodec.Typed(authority.Codec)
	if !ok || len(item.homeFiles) != 1 {
		return nil, planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped
	}
	file := item.homeFiles[0]
	if _, err := nix.HomeRelativeTarget(file.Target); err != nil {
		return nil, planner.ReasonSettingsStrategyUnavailable, planner.StatusSkipped
	}
	data, _, err := safepath.ReadRegularFileBounded(file.Source, settingscodec.MaxContentSize)
	if err != nil {
		return nil, planner.ReasonStagingValidationFailed, planner.StatusFailed
	}
	value, err := codec.Decode(data)
	if err != nil {
		return nil, planner.ReasonStagingValidationFailed, planner.StatusFailed
	}
	if len(value) == 0 {
		return nil, planner.ReasonAlreadyUpToDate, planner.StatusSkipped
	}
	canonical, err := codec.Encode(value)
	if err != nil {
		return nil, planner.ReasonStagingValidationFailed, planner.StatusFailed
	}
	return &typedHomeSetting{
		index: item.setIndex, target: file.Target, codec: codec, value: value,
		setting: nix.TypedSetting{Program: authority.Program, Option: authority.Option, NullPackage: authority.NullPackage, Canonical: canonical},
	}, "", ""
}

// definedByExplicitConfiguration reports whether the explicit configuration
// already defines the program that this set would enable. An explicit
// preparation that cannot say counts as defining it, so a collision is reported
// instead of missed.
func (home *preparedHomeManager) definedByExplicitConfiguration(program string) bool {
	if !home.explicitProjected {
		return false
	}
	return home.definedOutside == nil || slices.Contains(home.definedOutside, program)
}

// readBuiltHomeFile returns the bytes of one leaf of the built activation.
func readBuiltHomeFile(resolution *realizer.InputResolution, target string) ([]byte, error) {
	if resolution == nil || len(resolution.Outputs) != 1 {
		return nil, fmt.Errorf("Home Manager activation has no single built output")
	}
	relative, err := nix.HomeRelativeTarget(target)
	if err != nil {
		return nil, err
	}
	return nix.ReadHomeFile(resolution.OutputPaths[resolution.Outputs[0]], homeStoreRoot, relative, settingscodec.MaxContentSize)
}

// builtTypedExpectation proves that the built activation reproduces the captured
// value. The built bytes, not the staged ones, are the file that verify will compare.
func (typed *typedHomeSetting) builtTypedExpectation(resolution *realizer.InputResolution) (provision.HomeFileExpectation, bool) {
	built, err := readBuiltHomeFile(resolution, typed.target)
	if err != nil {
		return provision.HomeFileExpectation{}, false
	}
	value, err := typed.codec.Decode(built)
	if err != nil || !typed.codec.Equal(value, typed.value) {
		return provision.HomeFileExpectation{}, false
	}
	sum := sha256.Sum256(built)
	return provision.HomeFileExpectation{Target: filepath.ToSlash(typed.target), SHA256: hex.EncodeToString(sum[:])}, true
}

// withheldDetail names the targets whose built value differs from the captured one.
func withheldDetail(targets []string) string {
	slices.Sort(targets)
	return "The built Home Manager activation does not reproduce the captured settings value of " +
		strings.Join(targets, ", ") + ", so the captured activation was withheld."
}
