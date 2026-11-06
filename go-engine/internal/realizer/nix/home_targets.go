// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// Native Home Manager option names are schema tokens. Evaluation supplies all
// enabled flat targets, including program and XDG outputs, without reading sources.
const homeTargetsProjection = `p: {
  drvPath = p.activationPackage.drvPath;
  targets = if p ? config && p.config ? home && p.config.home ? file && p.config.home ? homeDirectory
    then map (f: p.config.home.homeDirectory + "/" + f.target)
      (builtins.filter (f: f.enable && !f.recursive) (builtins.attrValues p.config.home.file))
    else null;
}`

func (b *Backend) projectHomeTargets(r *realizer.InputResolution, name string) ([]string, error) {
	dir, err := os.MkdirTemp(filepath.Dir(b.Profile), "home-targets-")
	if err != nil {
		return nil, unresolved(err)
	}
	defer os.RemoveAll(dir)
	lock := filepath.Join(dir, "flake.lock")
	if err := os.WriteFile(lock, r.LockGraph, 0600); err != nil {
		return nil, unresolved(err)
	}
	value, err := b.native("eval", "--json", "--no-write-lock-file", "--reference-lock-file", lock,
		"--apply", homeTargetsProjection, r.Source.URL+"#homeConfigurations."+name)
	if err != nil {
		return nil, unresolved(err)
	}
	var projected struct {
		Derivation string          `json:"drvPath"`
		Targets    json.RawMessage `json:"targets"`
	}
	if err := json.Unmarshal(value, &projected); err != nil || projected.Derivation != r.Derivation || len(projected.Targets) == 0 {
		// Mixing evaluations could withhold unrelated settings or miss a writer.
		// Refuse mismatched evidence before mutation; a retry obtains one target.
		return nil, unresolved(fmt.Errorf("Home Manager target evaluation selected a different or unknown derivation"))
	}
	if string(projected.Targets) == "null" {
		// Activation-only flakes need not expose the optional parent config.
		return nil, nil
	}
	var targets []string
	if err := json.Unmarshal(projected.Targets, &targets); err != nil || targets == nil {
		return nil, unresolved(fmt.Errorf("Home Manager target evaluation has invalid flat targets"))
	}
	for i, target := range targets {
		if !path.IsAbs(target) || strings.ContainsRune(target, '\x00') {
			return nil, unresolved(fmt.Errorf("Home Manager target evaluation has an invalid absolute target"))
		}
		// Pinned files.nix inserts even leading-slash target values below its
		// output tree. Concatenation above preserves that placement under home.
		targets[i] = path.Clean(target)
	}
	return targets, nil
}
