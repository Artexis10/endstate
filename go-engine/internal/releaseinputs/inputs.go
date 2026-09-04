// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package releaseinputs owns the immutable Nixpkgs/Home Manager input pair
// compiled into an Endstate release.
package releaseinputs

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
)

//go:embed nix-inputs.json
var embeddedInputs []byte

var gitRevision = regexp.MustCompile(`^[0-9a-f]{40}$`)

// Input is one immutable flake input and its fetched-content identity.
type Input struct {
	Revision                  string `json:"revision"`
	FlakeRef                  string `json:"flakeRef"`
	NarHash                   string `json:"narHash"`
	CompatibleNixpkgsRevision string `json:"compatibleNixpkgsRevision,omitempty"`
}

// Inputs is the one compatible pair used for package realization, generated
// Home Manager flakes, metadata harvesting, and catalog validation.
type Inputs struct {
	SchemaVersion int   `json:"schemaVersion"`
	Nixpkgs       Input `json:"nixpkgs"`
	HomeManager   Input `json:"homeManager"`
}

// Load parses and validates the release-embedded input pair.
func Load() (Inputs, error) {
	return Parse(embeddedInputs)
}

// Parse applies the same strict validation to an input manifest supplied by a
// test or release tool.
func Parse(data []byte) (Inputs, error) {
	var inputs Inputs
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&inputs); err != nil {
		return Inputs{}, fmt.Errorf("parse immutable Nix inputs: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return Inputs{}, err
	}
	if inputs.SchemaVersion != 1 {
		return Inputs{}, fmt.Errorf("unsupported immutable Nix input schema %d", inputs.SchemaVersion)
	}
	if err := validateInput("nixpkgs", inputs.Nixpkgs); err != nil {
		return Inputs{}, err
	}
	if err := validateInput("home-manager", inputs.HomeManager); err != nil {
		return Inputs{}, err
	}
	if inputs.HomeManager.CompatibleNixpkgsRevision != inputs.Nixpkgs.Revision {
		return Inputs{}, fmt.Errorf("home-manager expects nixpkgs revision %q, release pins %q", inputs.HomeManager.CompatibleNixpkgsRevision, inputs.Nixpkgs.Revision)
	}
	return inputs, nil
}

func validateInput(name string, input Input) error {
	if !gitRevision.MatchString(input.Revision) {
		return fmt.Errorf("%s revision %q is not a full lowercase Git revision", name, input.Revision)
	}
	if !strings.HasSuffix(input.FlakeRef, "/"+input.Revision) || !strings.HasPrefix(input.FlakeRef, "github:") {
		return fmt.Errorf("%s flakeRef %q is not revision-qualified", name, input.FlakeRef)
	}
	if !strings.HasPrefix(input.NarHash, "sha256-") || len(input.NarHash) <= len("sha256-") {
		return fmt.Errorf("%s narHash %q is not an SRI SHA-256 hash", name, input.NarHash)
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parse immutable Nix inputs: multiple JSON values")
		}
		return fmt.Errorf("parse immutable Nix inputs: %w", err)
	}
	return nil
}
