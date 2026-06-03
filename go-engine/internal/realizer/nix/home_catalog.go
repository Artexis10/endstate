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

// curatedPrograms maps each curated concept that owns a home-manager `programs.*`
// entry to that program name, so a raw `programs` passthrough cannot also target
// it (a double definition Nix would reject).
var curatedPrograms = map[string]bool{
	"git":      true,
	"direnv":   true,
	"starship": true,
	"fzf":      true,
	"zoxide":   true,
	"bat":      true,
	"tmux":     true,
	"ssh":      true,
}

// GenerateHomeFlakeFromSettings compiles a declarative catalog (settings) into a
// home.nix, stages any declared files beside it, and reuses the wrapper's flake
// generation — returning the <dir>#<name> flakeref the existing ActivateHome
// consumes. File sources are resolved relative to manifestDir.
func GenerateHomeFlakeFromSettings(stateDir string, s *manifest.HomeManagerSettings, manifestDir string) (string, error) {
	homeNix, staged, err := CompileHomeNix(s, manifestDir)
	if err != nil {
		return "", err
	}
	return writeHomeFlake(stateDir, homeNix, staged)
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

	// shell → shell-agnostic home.* options.
	if s.Shell != nil {
		if len(s.Shell.Aliases) > 0 {
			stmts = append(stmts, "home.shellAliases = "+nixValue(stringMapToAny(s.Shell.Aliases))+";")
		}
		if len(s.Shell.SessionVariables) > 0 {
			stmts = append(stmts, "home.sessionVariables = "+nixValue(stringMapToAny(s.Shell.SessionVariables))+";")
		}
	}

	if s.Direnv != nil {
		stmts = append(stmts, "programs.direnv.enable = "+nixValue(s.Direnv.Enable)+";")
	}
	if s.Starship != nil {
		stmts = append(stmts, "programs.starship.enable = "+nixValue(s.Starship.Enable)+";")
	}
	if s.Fzf != nil {
		stmts = append(stmts, "programs.fzf.enable = "+nixValue(s.Fzf.Enable)+";")
	}
	if s.Zoxide != nil {
		stmts = append(stmts, "programs.zoxide.enable = "+nixValue(s.Zoxide.Enable)+";")
	}

	// bat → enable + the STABLE programs.bat.config attrset (key→value forwarded
	// verbatim, sorted for determinism).
	if s.Bat != nil {
		stmts = append(stmts, "programs.bat.enable = "+nixValue(s.Bat.Enable)+";")
		if len(s.Bat.Config) > 0 {
			stmts = append(stmts, "programs.bat.config = "+nixValue(stringMapToAny(s.Bat.Config))+";")
		}
	}

	// tmux → enable + the STABLE programs.tmux.extraConfig (raw tmux.conf string —
	// insulates the user from home-manager tmux option renames).
	if s.Tmux != nil {
		stmts = append(stmts, "programs.tmux.enable = "+nixValue(s.Tmux.Enable)+";")
		if s.Tmux.ExtraConfig != "" {
			stmts = append(stmts, "programs.tmux.extraConfig = "+nixValue(s.Tmux.ExtraConfig)+";")
		}
	}

	// ssh → enable + the STABLE programs.ssh.extraConfig (raw ssh config string —
	// insulates the user from home-manager ssh option renames).
	if s.SSH != nil {
		stmts = append(stmts, "programs.ssh.enable = "+nixValue(s.SSH.Enable)+";")
		if s.SSH.ExtraConfig != "" {
			stmts = append(stmts, "programs.ssh.extraConfig = "+nixValue(s.SSH.ExtraConfig)+";")
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

// sortedKeys returns the keys of a map[string]any in sorted order (determinism).
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
