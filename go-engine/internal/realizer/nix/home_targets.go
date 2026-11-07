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
// A recursive declaration names a root, not exact leaves, so it is reported apart
// instead of silently dropped; only the built tree lists its leaves.
//
// definedOutside lists the programs for which a file outside Home Manager's modules
// path defines enable, package or settings, which are the three options a typed
// module sets. An option's files include its declaration default, so the modules path
// separates the user's definitions from Home Manager's own. A program whose files
// cannot be read counts as defined: a collision is reported rather than missed. The
// expression is constant; no data enters the Nix source.
const homeTargetsProjection = `p: let
  files = if p ? config && p.config ? home && p.config.home ? file && p.config.home ? homeDirectory
    then builtins.filter (f: f.enable) (builtins.attrValues p.config.home.file)
    else null;
  modules = toString p._module.specialArgs.modulesPath + "/";
  own = file: let name = toString file; in builtins.substring 0 (builtins.stringLength modules) name == modules;
  outside = program: builtins.any (name:
    program ? ${name} && program.${name} ? files && builtins.any (file: !own file) program.${name}.files
  ) [ "enable" "package" "settings" ];
  defined = program: let read = builtins.tryEval (outside program); in !read.success || read.value;
  programs = p.options.programs;
in {
  drvPath = p.activationPackage.drvPath;
  targets = if files == null then null
    else map (f: p.config.home.homeDirectory + "/" + f.target) (builtins.filter (f: !f.recursive) files);
  recursive = files != null && builtins.any (f: f.recursive) files;
  definedOutside = if p ? options && p.options ? programs && p ? _module && p._module ? specialArgs && p._module.specialArgs ? modulesPath
    then builtins.filter (name: defined programs.${name}) (builtins.attrNames programs)
    else null;
}`

// homeProjection is what one evaluation of the parent configuration reports.
type homeProjection struct {
	targets        []string
	recursive      bool
	definedOutside []string
}

// projectHomeTargets returns the evaluated flat targets, nil when the optional
// parent configuration is unavailable, whether any enabled declaration is
// recursive, and the programs that files outside Home Manager define.
func (b *Backend) projectHomeTargets(r *realizer.InputResolution, name string) (homeProjection, error) {
	dir, err := os.MkdirTemp(filepath.Dir(b.Profile), "home-targets-")
	if err != nil {
		return homeProjection{}, unresolved(err)
	}
	defer os.RemoveAll(dir)
	lock := filepath.Join(dir, "flake.lock")
	if err := os.WriteFile(lock, r.LockGraph, 0600); err != nil {
		return homeProjection{}, unresolved(err)
	}
	value, err := b.native("eval", "--json", "--no-write-lock-file", "--reference-lock-file", lock,
		"--apply", homeTargetsProjection, r.Source.URL+"#homeConfigurations."+name)
	if err != nil {
		return homeProjection{}, unresolved(err)
	}
	var projected struct {
		Derivation     string          `json:"drvPath"`
		Targets        json.RawMessage `json:"targets"`
		Recursive      bool            `json:"recursive"`
		DefinedOutside []string        `json:"definedOutside"`
	}
	if err := json.Unmarshal(value, &projected); err != nil || projected.Derivation != r.Derivation || len(projected.Targets) == 0 {
		// Mixing evaluations could withhold unrelated settings or miss a writer.
		// Refuse mismatched evidence before mutation; a retry obtains one target.
		return homeProjection{}, unresolved(fmt.Errorf("Home Manager target evaluation selected a different or unknown derivation"))
	}
	result := homeProjection{recursive: projected.Recursive, definedOutside: projected.DefinedOutside}
	if string(projected.Targets) == "null" {
		// Activation-only flakes need not expose the optional parent config.
		result.recursive = false
		return result, nil
	}
	var targets []string
	if err := json.Unmarshal(projected.Targets, &targets); err != nil || targets == nil {
		return homeProjection{}, unresolved(fmt.Errorf("Home Manager target evaluation has invalid flat targets"))
	}
	for i, target := range targets {
		if !path.IsAbs(target) || strings.ContainsRune(target, '\x00') {
			return homeProjection{}, unresolved(fmt.Errorf("Home Manager target evaluation has an invalid absolute target"))
		}
		// Pinned files.nix inserts even leading-slash target values below its
		// output tree. Concatenation above preserves that placement under home.
		targets[i] = path.Clean(target)
	}
	result.targets = targets
	return result, nil
}
