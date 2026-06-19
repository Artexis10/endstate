// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// fieldKind classifies the type of a curated concept's optional second field (the
// one beyond the bare `.enable` toggle), so a single generic emit loop can render
// every uniform concept from the curatedTable below.
type fieldKind int

const (
	kindNone      fieldKind = iota // no second field (bare .enable toggle, e.g. fzf)
	kindString                     // raw string → StableField = "..."   (e.g. tmux.extraConfig)
	kindStringMap                  // map[string]string → attrset        (e.g. bat.config)
	kindAnyMap                     // map[string]any → nested attrset     (e.g. gh.settings)
	kindStringSlice                // []string → Nix list                 (e.g. eza.extraOptions)
)

// curatedProgram is one row of the data-driven catalog: a concept name (== the
// home-manager `programs.<Name>` it owns), the STABLE second-field option the engine
// targets (insulating the user from per-feature option renames), that field's kind,
// and a getter that pulls (present, enable, secondField) off the settings struct.
// Concepts whose shape is genuinely non-uniform (git's nested user/init, shell's
// home.* mapping) are handled bespoke in CompileHomeNix and are NOT table rows.
type curatedProgram struct {
	Name        string    // home-manager programs.<Name>; also the curated/raw-overlap key
	StableField string    // the rename-proof second option (e.g. "extraConfig", "settings"); "" ⇒ none
	Kind        fieldKind // how to render the second field's value
	get         func(s *manifest.HomeManagerSettings) (present, enable bool, second any)
}

// curatedTable is the single source of truth for every uniform curated concept.
// Emission order here is the emission order in the generated home.nix (deterministic).
// To add a program: add a typed field + struct in manifest/types.go and one row here.
var curatedTable = []curatedProgram{
	{"direnv", "", kindNone, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Direnv == nil {
			return false, false, nil
		}
		return true, s.Direnv.Enable, nil
	}},
	{"starship", "", kindNone, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Starship == nil {
			return false, false, nil
		}
		return true, s.Starship.Enable, nil
	}},
	{"fzf", "", kindNone, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Fzf == nil {
			return false, false, nil
		}
		return true, s.Fzf.Enable, nil
	}},
	{"zoxide", "", kindNone, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Zoxide == nil {
			return false, false, nil
		}
		return true, s.Zoxide.Enable, nil
	}},
	{"bat", "config", kindStringMap, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Bat == nil {
			return false, false, nil
		}
		return true, s.Bat.Enable, s.Bat.Config
	}},
	{"tmux", "extraConfig", kindString, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Tmux == nil {
			return false, false, nil
		}
		return true, s.Tmux.Enable, s.Tmux.ExtraConfig
	}},
	{"ssh", "extraConfig", kindString, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.SSH == nil {
			return false, false, nil
		}
		return true, s.SSH.Enable, s.SSH.ExtraConfig
	}},
	{"eza", "extraOptions", kindStringSlice, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Eza == nil {
			return false, false, nil
		}
		return true, s.Eza.Enable, s.Eza.ExtraOptions
	}},
	{"gh", "settings", kindAnyMap, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Gh == nil {
			return false, false, nil
		}
		return true, s.Gh.Enable, s.Gh.Settings
	}},
	{"lazygit", "settings", kindAnyMap, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Lazygit == nil {
			return false, false, nil
		}
		return true, s.Lazygit.Enable, s.Lazygit.Settings
	}},
	{"neovim", "extraConfig", kindString, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Neovim == nil {
			return false, false, nil
		}
		return true, s.Neovim.Enable, s.Neovim.ExtraConfig
	}},
	// Dotfiles/CLI tier.
	{"ripgrep", "arguments", kindStringSlice, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Ripgrep == nil {
			return false, false, nil
		}
		return true, s.Ripgrep.Enable, s.Ripgrep.Arguments
	}},
	{"fd", "extraOptions", kindStringSlice, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Fd == nil {
			return false, false, nil
		}
		return true, s.Fd.Enable, s.Fd.ExtraOptions
	}},
	{"zsh", "initContent", kindString, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Zsh == nil {
			return false, false, nil
		}
		return true, s.Zsh.Enable, s.Zsh.InitContent
	}},
	{"bash", "initExtra", kindString, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Bash == nil {
			return false, false, nil
		}
		return true, s.Bash.Enable, s.Bash.InitExtra
	}},
	{"helix", "settings", kindAnyMap, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Helix == nil {
			return false, false, nil
		}
		return true, s.Helix.Enable, s.Helix.Settings
	}},
	{"kitty", "settings", kindAnyMap, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Kitty == nil {
			return false, false, nil
		}
		return true, s.Kitty.Enable, s.Kitty.Settings
	}},
	{"alacritty", "settings", kindAnyMap, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Alacritty == nil {
			return false, false, nil
		}
		return true, s.Alacritty.Enable, s.Alacritty.Settings
	}},
	{"wezterm", "extraConfig", kindString, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Wezterm == nil {
			return false, false, nil
		}
		return true, s.Wezterm.Enable, s.Wezterm.ExtraConfig
	}},
	{"jujutsu", "settings", kindAnyMap, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Jujutsu == nil {
			return false, false, nil
		}
		return true, s.Jujutsu.Enable, s.Jujutsu.Settings
	}},
	{"atuin", "settings", kindAnyMap, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Atuin == nil {
			return false, false, nil
		}
		return true, s.Atuin.Enable, s.Atuin.Settings
	}},
	{"yazi", "settings", kindAnyMap, func(s *manifest.HomeManagerSettings) (bool, bool, any) {
		if s.Yazi == nil {
			return false, false, nil
		}
		return true, s.Yazi.Enable, s.Yazi.Settings
	}},
}

// curatedPrograms maps each curated concept that owns a home-manager `programs.*`
// entry to that program name, so a raw `programs` passthrough cannot also target it
// (a double definition Nix would reject). Derived from curatedTable plus the bespoke
// program-owning concept `git` (shell maps to home.*, not a programs.* entry).
var curatedPrograms = buildCuratedPrograms()

func buildCuratedPrograms() map[string]bool {
	m := map[string]bool{"git": true}
	for _, c := range curatedTable {
		m[c.Name] = true
	}
	return m
}

// secondFieldEmpty reports whether a curated concept's optional second field carries
// no value (so the engine omits the second statement and emits only `.enable`).
func secondFieldEmpty(kind fieldKind, v any) bool {
	switch kind {
	case kindNone:
		return true
	case kindString:
		return v.(string) == ""
	case kindStringMap:
		return len(v.(map[string]string)) == 0
	case kindAnyMap:
		return len(v.(map[string]any)) == 0
	case kindStringSlice:
		return len(v.([]string)) == 0
	}
	return true
}

// renderSecondField renders a curated concept's second-field value as a Nix expression
// (mirrors the per-kind handling the per-concept blocks used before the table existed).
func renderSecondField(kind fieldKind, v any) string {
	switch kind {
	case kindString:
		return nixValue(v.(string))
	case kindStringMap:
		return nixValue(stringMapToAny(v.(map[string]string)))
	case kindAnyMap:
		return nixValue(v.(map[string]any))
	case kindStringSlice:
		return nixValue(stringSliceToAny(v.([]string)))
	}
	return ""
}

// GenerateHomeFlakeFromSettings compiles a declarative catalog (settings) into a
// home.nix, stages any declared files beside it, and reuses the wrapper's flake
// generation — returning the <dir>#<name> flakeref the existing ActivateHome
// consumes. File sources are resolved relative to manifestDir.
func GenerateHomeFlakeFromSettings(stateDir string, s *manifest.HomeManagerSettings, manifestDir string, secrets []manifest.HomeManagerSecret) (string, error) {
	homeNix, staged, err := CompileHomeNix(s, manifestDir)
	if err != nil {
		return "", err
	}
	return writeHomeFlake(stateDir, homeNix, staged, secrets)
}

// CompileHomeNix renders a home-manager module (home.nix) from the declared
// settings and returns it plus the files to stage beside it (relative path within
// the flake dir → content). It maps curated concepts to home-manager options,
// forwards the raw `programs` block verbatim, and turns `files` entries into
// home.file placements (the sources are read here, resolved relative to
// manifestDir). Output is deterministic (sorted keys/targets).
func CompileHomeNix(s *manifest.HomeManagerSettings, manifestDir string) ([]byte, map[string][]byte, error) {
	staged := map[string][]byte{}
	var stmts []string

	// A raw programs key must not collide with a curated concept.
	for name := range s.Programs {
		if curatedPrograms[name] {
			return nil, nil, fmt.Errorf("homeManager.settings: programs.%s conflicts with the curated %q concept — set one or the other", name, name)
		}
	}

	// git → the STABLE programs.git.extraConfig (insulates the user from
	// home-manager option renames — the moat the curated layer buys).
	if s.Git != nil {
		stmts = append(stmts, "programs.git.enable = true;")
		extra := map[string]any{}
		user := map[string]any{}
		if s.Git.UserName != "" {
			user["name"] = s.Git.UserName
		}
		if s.Git.UserEmail != "" {
			user["email"] = s.Git.UserEmail
		}
		if len(user) > 0 {
			extra["user"] = user
		}
		if s.Git.DefaultBranch != "" {
			extra["init"] = map[string]any{"defaultBranch": s.Git.DefaultBranch}
		}
		if len(extra) > 0 {
			stmts = append(stmts, "programs.git.extraConfig = "+nixValue(extra)+";")
		}
	}

	// shell → shell-agnostic home.* options (bespoke: maps to home.*, not a
	// programs.<name> entry, so it is not a curatedTable row).
	if s.Shell != nil {
		if len(s.Shell.Aliases) > 0 {
			stmts = append(stmts, "home.shellAliases = "+nixValue(stringMapToAny(s.Shell.Aliases))+";")
		}
		if len(s.Shell.SessionVariables) > 0 {
			stmts = append(stmts, "home.sessionVariables = "+nixValue(stringMapToAny(s.Shell.SessionVariables))+";")
		}
	}

	// Every uniform curated concept: emit `programs.<name>.enable` (explicit even when
	// false, so the user can pin a program off) and, when the optional second field
	// carries a value, the STABLE second option it maps to (insulating the user from
	// home-manager per-feature option renames). Driven entirely by curatedTable —
	// adding a program is one struct + one table row, no new emit branch.
	for _, c := range curatedTable {
		present, enable, second := c.get(s)
		if !present {
			continue
		}
		stmts = append(stmts, "programs."+c.Name+".enable = "+nixValue(enable)+";")
		if c.StableField != "" && !secondFieldEmpty(c.Kind, second) {
			stmts = append(stmts, "programs."+c.Name+"."+c.StableField+" = "+renderSecondField(c.Kind, second)+";")
		}
	}

	// raw programs passthrough, verbatim, sorted for determinism.
	for _, name := range sortedKeys(s.Programs) {
		stmts = append(stmts, "programs."+name+" = "+nixValue(s.Programs[name])+";")
	}

	// files → staged beside the flake + placed via home.file (sorted by target).
	if len(s.Files) > 0 {
		targets := make([]string, 0, len(s.Files))
		for target := range s.Files {
			targets = append(targets, target)
		}
		sort.Strings(targets)
		for _, target := range targets {
			srcRel := s.Files[target]
			srcPath := srcRel
			if !filepath.IsAbs(srcPath) {
				srcPath = filepath.Join(manifestDir, srcPath)
			}
			content, err := os.ReadFile(srcPath)
			if err != nil {
				return nil, nil, fmt.Errorf("homeManager.settings.files: read source %q: %w", srcRel, err)
			}
			homeRel := homeRelTarget(target)
			stagedRel := "files/" + sanitizeTarget(homeRel)
			staged[stagedRel] = content
			stmts = append(stmts, fmt.Sprintf("home.file.%s.source = ./%s;", nixString(homeRel), stagedRel))
		}
	}

	var b strings.Builder
	b.WriteString("{ ... }:\n{\n")
	for _, st := range stmts {
		b.WriteString("  " + st + "\n")
	}
	b.WriteString("}\n")
	return []byte(b.String()), staged, nil
}

// nixValue encodes a JSON-decoded value (from the raw `programs` passthrough) as a
// Nix expression: bools, numbers (integer-valued without a decimal point), escaped
// double-quoted strings, space-separated lists, and sorted-key attrsets — so a
// user's raw home-manager block is forwarded with correct, deterministic Nix.
func nixValue(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		if x == math.Trunc(x) && !math.IsInf(x, 0) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case string:
		return nixString(x)
	case []any:
		parts := make([]string, len(x))
		for i, e := range x {
			parts[i] = nixValue(e)
		}
		return "[ " + strings.Join(parts, " ") + " ]"
	case map[string]any:
		var b strings.Builder
		b.WriteString("{ ")
		for _, k := range sortedKeys(x) {
			b.WriteString(nixString(k) + " = " + nixValue(x[k]) + "; ")
		}
		b.WriteString("}")
		return b.String()
	default:
		return nixString(fmt.Sprintf("%v", x))
	}
}

// nixString renders s as a Nix double-quoted string literal, escaping backslashes,
// quotes, antiquotation (${ → \${), and common control characters.
func nixString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "${", `\${`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	return `"` + s + `"`
}

// homeRelTarget normalizes a declared file target to a home-relative path (home.file
// keys are relative to $HOME): strips a leading "~/" and any leading "/".
func homeRelTarget(target string) string {
	target = strings.TrimPrefix(target, "~/")
	target = strings.TrimPrefix(target, "/")
	return target
}

// sanitizeTarget turns a home-relative target into a single staged filename by
// replacing path separators with underscores (readable + collision-free for
// distinct targets).
func sanitizeTarget(homeRel string) string {
	return strings.ReplaceAll(homeRel, "/", "_")
}

// stringMapToAny lifts a map[string]string to map[string]any for nixValue.
func stringMapToAny(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// stringSliceToAny lifts a []string to []any for nixValue (renders as a Nix list).
func stringSliceToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// sortedKeys returns the keys of a map[string]any in sorted order (determinism).
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
