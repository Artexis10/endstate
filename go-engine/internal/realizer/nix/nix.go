// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package nix implements realizer.Realizer for Nix via `nix profile`. It uses
// `nix profile add` (the supported verb, not the deprecated `install` alias) and
// reads `nix profile list --json`, classifying failures to engine error codes
// from the process exit code + internal-json activity + whether the profile
// generation advanced. Installs go into an Endstate-managed profile so the
// engine owns the generation lineage.
package nix

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// Runner executes `nix <args...>` and returns captured stdout/stderr, the
// process exit code, and a non-nil error ONLY for spawn failures (binary
// missing/unrunnable) — an expected non-zero exit is reported via exitCode with
// a nil error. It is an injection point so tests stay hermetic.
type Runner func(args ...string) (stdout, stderr []byte, exitCode int, err error)

// Backend implements realizer.Realizer over the Nix CLI.
type Backend struct {
	// Profile is the Endstate-managed nix profile path.
	Profile string
	// Pin is the base flakeref a bare attribute is expanded against (e.g.
	// "nixpkgs" or a rev-pinned "github:NixOS/nixpkgs/<rev>").
	Pin string
	// Run executes nix; defaults to the real exec runner.
	Run Runner
	// genFn overrides generation reading; nil means read the profile symlink.
	// Tests (in-package) set this to drive Realize's atomicity detection
	// hermetically without a real generation switch.
	genFn func() int
}

// gen returns the active generation number, via genFn when set (tests) else by
// reading the profile symlink.
func (b *Backend) gen() int {
	if b.genFn != nil {
		return b.genFn()
	}
	return b.generation()
}

// New returns a Backend with the default Endstate-managed profile, pin, and a
// real exec runner.
func New() *Backend {
	return &Backend{Profile: DefaultProfile(), Pin: defaultPin(), Run: defaultRunner}
}

// Name satisfies realizer.Realizer.
func (b *Backend) Name() string { return "nix" }

// DefaultProfile returns the Endstate-managed nix profile path, overridable via
// ENDSTATE_NIX_PROFILE. It follows the XDG state-dir convention rather than a
// hardcoded absolute path.
func DefaultProfile() string {
	if p := os.Getenv("ENDSTATE_NIX_PROFILE"); p != "" {
		return p
	}
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "endstate", "nix-profile")
}

func defaultPin() string {
	if p := os.Getenv("ENDSTATE_NIXPKGS_PIN"); p != "" {
		return p
	}
	// Default base flakeref. For full reproducibility a production pin is a
	// rev-locked "github:NixOS/nixpkgs/<rev>"; the registry "nixpkgs" is the
	// pragmatic default (on Determinate Nix it resolves via the pinned weekly
	// registry).
	return "nixpkgs"
}

// nixBin resolves the nix binary: ENDSTATE_NIX_BIN, else PATH, else the
// Determinate default install location.
func nixBin() string {
	if p := os.Getenv("ENDSTATE_NIX_BIN"); p != "" {
		return p
	}
	if _, err := exec.LookPath("nix"); err == nil {
		return "nix"
	}
	return "/nix/var/nix/profiles/default/bin/nix"
}

// defaultRunner runs the real nix CLI, forcing the experimental features the
// `nix profile`/flake commands require (harmless if already enabled).
func defaultRunner(args ...string) ([]byte, []byte, int, error) {
	full := append([]string{}, args...)
	full = append(full, "--extra-experimental-features", "nix-command flakes")
	cmd := exec.Command(nixBin(), full...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	runErr := cmd.Run()
	if runErr == nil {
		return out.Bytes(), errb.Bytes(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		// Expected non-zero exit; not a spawn failure.
		return out.Bytes(), errb.Bytes(), exitErr.ExitCode(), nil
	}
	// Spawn failure (binary missing, not executable, etc.).
	return out.Bytes(), errb.Bytes(), -1, runErr
}

// ResolveInstallable expands a manifest ref into a concrete nix installable. A
// bare attribute ("ripgrep") is expanded against the pin ("nixpkgs#ripgrep"); a
// ref already in flakeref/installable form (containing '#' or a known scheme) is
// returned verbatim so power users can pin per-app.
func (b *Backend) ResolveInstallable(ref string) string {
	r := strings.TrimSpace(ref)
	if strings.Contains(r, "#") {
		return r
	}
	for _, scheme := range []string{"github:", "git+", "path:", "flake:", "tarball:", "file:", "/", "."} {
		if strings.HasPrefix(r, scheme) {
			return r
		}
	}
	return b.Pin + "#" + r
}

// generation reads the active generation number from the profile symlink
// (<profile> -> <profile>-<N>-link). Returns 0 when the profile does not exist.
func (b *Backend) generation() int {
	target, err := os.Readlink(b.Profile)
	if err != nil {
		return 0
	}
	base := strings.TrimSuffix(filepath.Base(target), "-link")
	if i := strings.LastIndex(base, "-"); i >= 0 {
		if n, err := strconv.Atoi(base[i+1:]); err == nil {
			return n
		}
	}
	return 0
}

// Current reads the installed set via `nix profile list --json`.
func (b *Backend) Current() (realizer.Set, error) {
	empty := realizer.Set{Elements: map[string]realizer.Element{}, Generation: b.generation()}
	stdout, stderr, exit, err := b.Run("profile", "list", "--profile", b.Profile, "--json")
	if err != nil {
		return empty, classify(-1, parseInternalJSON(stderr), false)
	}
	if exit != 0 {
		// A profile that does not exist yet lists as empty rather than an error.
		return empty, nil
	}
	set, perr := parseProfileList(stdout)
	if perr != nil {
		return empty, nil
	}
	set.Generation = b.generation()
	return set, nil
}

// Plan computes the diff between desired installables and the current set,
// without mutating state. An installable is considered present when its leaf
// attribute matches an installed element name or attrPath.
func (b *Backend) Plan(desired []realizer.Installable) (realizer.Diff, error) {
	cur, err := b.Current()
	if err != nil {
		return realizer.Diff{}, err
	}
	var d realizer.Diff
	for _, ins := range desired {
		if isPresent(cur, ins.Ref) {
			d.Present = append(d.Present, ins)
		} else {
			d.ToAdd = append(d.ToAdd, ins)
		}
	}
	return d, nil
}

// isPresent reports whether ref's leaf attribute matches an installed element.
func isPresent(set realizer.Set, ref string) bool {
	leaf := attrLeaf(ref)
	if leaf == "" {
		return false
	}
	if _, ok := set.Elements[leaf]; ok {
		return true
	}
	for _, e := range set.Elements {
		if e.Name == leaf || attrLeaf(e.AttrPath) == leaf {
			return true
		}
	}
	return false
}

// Realize adds the given installables in one `nix profile add`, an atomic
// generation switch. It receives only the to-add set. On any failure the prior
// generation is left intact (spike-confirmed) and Result.Err carries the
// classified engine code.
func (b *Backend) Realize(toAdd []realizer.Installable) (realizer.Result, error) {
	res := realizer.Result{FromGeneration: -1, ToGeneration: -1}
	if len(toAdd) == 0 {
		cur, _ := b.Current()
		res.After = cur
		res.FromGeneration, res.ToGeneration = cur.Generation, cur.Generation
		return res, nil
	}

	if err := os.MkdirAll(filepath.Dir(b.Profile), 0o755); err != nil {
		res.Err = &realizer.Error{Code: envelope.ErrInternalError, Subcode: "profile-dir", Stage: "commit", Raw: err.Error()}
		return res, nil
	}

	before := b.gen()
	res.FromGeneration = before

	args := []string{"profile", "add", "--profile", b.Profile}
	for _, ins := range toAdd {
		args = append(args, b.ResolveInstallable(ins.Ref))
	}
	args = append(args, "--log-format", "internal-json")

	_, stderr, exit, err := b.Run(args...)
	p := parseInternalJSON(stderr)
	if err != nil { // spawn failure
		res.Err = classify(-1, p, false)
		return res, nil
	}

	after := b.gen()
	res.ToGeneration = after
	res.Advanced = after > before
	if exit != 0 || !res.Advanced {
		res.Err = classify(exit, p, res.Advanced)
	}

	cur, _ := b.Current()
	res.After = cur
	return res, nil
}
