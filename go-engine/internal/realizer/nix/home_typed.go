// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// TypedSetting is one captured settings value that Home Manager renders through a
// program's structured settings option. Canonical is the codec's canonical
// encoding of the value; the generated module reads it with builtins.fromTOML, so
// no Nix value renderer is involved.
type TypedSetting struct {
	// Program is the Home Manager program name: programs.<Program>.
	Program string
	// Option is the full option path that receives the value.
	Option string
	// NullPackage lets the module set programs.<Program>.package to null, so the
	// package lane stays the only owner of the program's package.
	NullPackage bool
	Canonical   []byte
}

// Fixed Nix names of the module that carries typed settings.
const (
	typedModule    = "typed.nix"
	typedDirectory = "typed"
)

// typedSettingsOption is the only option a typed module renders. The decision 28
// projection reads definitions of exactly enable, package and settings.
func typedSettingsOption(program string) string { return "programs." + program + ".settings" }

// typedAttributePath quotes a program name so a reviewed registry value can never
// change the shape of the generated Nix.
func typedAttributePath(program, option string) string {
	return "programs." + nixString(program) + "." + option
}

// compileTypedModule returns the typed Home Manager module and the canonical files
// it reads, keyed by their path beside the flake. File names are the SHA-256 of
// their content, so equal payloads share a file.
func compileTypedModule(settings []TypedSetting) ([]byte, map[string][]byte, error) {
	staged := map[string][]byte{}
	ordered := append([]TypedSetting(nil), settings...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Program < ordered[j].Program })
	var module strings.Builder
	module.WriteString("{ ... }:\n{\n")
	seen := map[string]bool{}
	for _, setting := range ordered {
		if setting.Program == "" || setting.Option != typedSettingsOption(setting.Program) {
			return nil, nil, fmt.Errorf("typed setting for %q does not render %s", setting.Program, typedSettingsOption(setting.Program))
		}
		if seen[setting.Program] {
			return nil, nil, fmt.Errorf("typed settings repeat program %q", setting.Program)
		}
		seen[setting.Program] = true
		sum := sha256.Sum256(setting.Canonical)
		relative := typedDirectory + "/" + hex.EncodeToString(sum[:]) + ".toml"
		staged[relative] = setting.Canonical
		module.WriteString("  " + typedAttributePath(setting.Program, "enable") + " = true;\n")
		if setting.NullPackage {
			module.WriteString("  " + typedAttributePath(setting.Program, "package") + " = null;\n")
		}
		module.WriteString("  " + typedAttributePath(setting.Program, "settings") + " = builtins.fromTOML (builtins.readFile ./" + relative + ");\n")
	}
	module.WriteString("}\n")
	return []byte(module.String()), staged, nil
}
