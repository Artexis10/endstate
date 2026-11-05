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
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

// Runner executes `nix <args...>` and returns captured stdout/stderr, the
// process exit code, and a non-nil error ONLY for spawn failures (binary
// missing/unrunnable) — an expected non-zero exit is reported via exitCode with
// a nil error. It is an injection point so tests stay hermetic.
type Runner func(args ...string) (stdout, stderr []byte, exitCode int, err error)

// ScriptRunner executes a store-path script (e.g. a home-manager generation's
// `activate`) and follows the same exit/err convention as Runner. It is a
// SEPARATE seam from Runner because `activate` is a store-path executable, not a
// `nix` subcommand, so it is invoked directly rather than through `nix`.
type ScriptRunner func(path string, args ...string) (stdout, stderr []byte, exitCode int, err error)

// Backend implements realizer.Realizer over the Nix CLI.
type Backend struct {
	prepared []realizer.Installable
	rootDirs []string
	// Script executes an activation script; nil selects the native process.
	Script ScriptRunner
	// Profile is the Endstate-managed nix profile path.
	Profile string
	// Pin is the base flakeref a bare attribute is expanded against (e.g.
	// "nixpkgs" or a rev-pinned "github:NixOS/nixpkgs/<rev>").
	Pin string
	// HomePin exposes the Home Manager input paired with the release's Nixpkgs
	// input. Generated configurations use ENDSTATE_HOME_MANAGER_PIN when set.
	HomePin string
	// Run executes nix; defaults to the real exec runner.
	Run Runner
	// runScript executes a store-path script (a home-manager generation's
	// `activate`) for RollbackHome; nil means the real exec runner. Tests inject a
	// stub so the re-activation path is exercised hermetically.
	runScript ScriptRunner
	// genFn overrides generation reading; nil means read the profile symlink.
	// Tests (in-package) set this to drive Realize's atomicity detection
	// hermetically without a real generation switch.
	genFn func() int
	// homeGenFn overrides home-manager generation reading; nil means read the
	// home-manager profile symlink. Tests set it so ActivateHome's success path
	// is exercised hermetically without a real home-manager switch.
	homeGenFn func() int
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
	return &Backend{Profile: DefaultProfile(), Pin: defaultPin(), HomePin: defaultHomePin(), Run: defaultRunner}
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
	return filepath.Join(profileStateHome(), "endstate", "nix-profile")
}

// Profile locations use native host paths and ignore relative XDG roots.
func profileStateHome() string {
	if base := os.Getenv("XDG_STATE_HOME"); filepath.IsAbs(base) {
		return base
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state")
}

func defaultPin() string {
	if p := os.Getenv("ENDSTATE_NIXPKGS_PIN"); p != "" {
		return p
	}
	inputs, err := releaseinputs.Load()
	if err != nil {
		panic(fmt.Sprintf("invalid embedded Nix input manifest: %v", err))
	}
	return inputs.Nixpkgs.FlakeRef
}

// defaultHomePin selects the Home Manager input for generated configurations.
// The release owns its compatible revision-locked pair; an environment override
// remains inspectable in the generated configuration's effective resolution.
func defaultHomePin() string {
	if p := os.Getenv("ENDSTATE_HOME_MANAGER_PIN"); p != "" {
		return p
	}
	inputs, err := releaseinputs.Load()
	if err != nil {
		panic(fmt.Sprintf("invalid embedded Nix input manifest: %v", err))
	}
	return inputs.HomeManager.FlakeRef
}

// nixArgs returns the full nix argv with the experimental features the
// flake/nix-command verbs require. They are inserted BEFORE the first bare `--`
// separator so a `nix run <pin> -- <prog args>` invocation passes the flag to
// nix rather than to the downstream program (e.g. home-manager, which would
// reject it). When there is no bare `--` (the `nix profile` verbs) the flag is
// appended at the end, exactly as before.
func nixArgs(args []string) []string {
	feat := []string{"--extra-experimental-features", "nix-command flakes"}
	for i, a := range args {
		if a == "--" {
			out := make([]string, 0, len(args)+len(feat))
			out = append(out, args[:i]...)
			out = append(out, feat...)
			out = append(out, args[i:]...)
			return out
		}
	}
	return append(append([]string{}, args...), feat...)
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
	cmd := exec.Command(nixBin(), nixArgs(args)...)
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

// defaultScriptRunner runs a store-path script directly (e.g. a home-manager
// generation's `activate`), inheriting the engine's environment, and follows the
// same exit/err convention as defaultRunner.
func defaultScriptRunner(path string, args ...string) ([]byte, []byte, int, error) {
	cmd := exec.Command(path, args...)
	// Home Manager's native driver 0 owns its profile at writeBoundary. Preserve
	// ambient driver hooks and set only its supported backup variable per process.
	cmd.Env = append(os.Environ(), "HOME_MANAGER_BACKUP_EXT=endstate-backup")
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	runErr := cmd.Run()
	if runErr == nil {
		return out.Bytes(), errb.Bytes(), 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(runErr, &exitErr) {
		return out.Bytes(), errb.Bytes(), exitErr.ExitCode(), nil
	}
	return out.Bytes(), errb.Bytes(), -1, runErr
}

// execScript runs a store-path script via runScript when set (tests) else the
// real exec runner.
func (b *Backend) execScript(path string, args ...string) ([]byte, []byte, int, error) {
	if b.Script != nil {
		return b.Script(path, args...)
	}
	if b.runScript != nil {
		return b.runScript(path, args...)
	}
	return defaultScriptRunner(path, args...)
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
	// The URI parser recognises schemes without maintaining a second Nix
	// protocol registry. Native Nix still validates the complete installable.
	if parsed, err := url.Parse(r); err == nil && parsed.Scheme != "" {
		return r
	}
	if strings.HasPrefix(r, "/") || strings.HasPrefix(r, ".") {
		return r
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
	candidates := append([]realizer.Installable(nil), b.prepared...)
	if generations, err := provision.List(); err == nil {
		for _, generation := range generations {
			if generation.Backend != "nix" {
				continue
			}
			for _, item := range generation.Items {
				candidates = append(candidates, realizer.Installable{ID: item.ID, Ref: item.Ref, Resolution: item.Resolution})
			}
		}
	}
	for key, element := range set.Elements {
		for _, item := range candidates {
			if realizer.ResolutionMatches(element, item.Resolution) {
				element.ID = item.ID
				element.RequestedRef = item.Ref
				element.Resolution = item.Resolution
				set.Elements[key] = element
				break
			}
		}
	}
	return set, nil
}

// Plan computes the diff between desired installables and the current set,
// without mutating state. An installable is considered present when its leaf
// attribute matches an installed element name or attrPath.
func (b *Backend) Plan(desired []realizer.Installable) (realizer.Diff, error) {
	var err error
	if needsPreparation(desired) {
		desired, err = b.Prepare(desired)
		if err != nil {
			return realizer.Diff{}, err
		}
	}
	cur, err := b.Current()
	if err != nil {
		return realizer.Diff{}, err
	}
	var d realizer.Diff
	for _, ins := range desired {
		if installablePresent(cur, ins) {
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
	if element, ok := set.Elements[leaf]; ok && element.ID == "" {
		return true
	}
	for _, e := range set.Elements {
		if e.ID == "" && (e.Name == leaf || attrLeaf(e.AttrPath) == leaf) {
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

	if needsPreparation(toAdd) {
		defer func() { b.FinalizePreparation(res.Advanced) }()
		var err error
		toAdd, err = b.Prepare(toAdd)
		if err != nil {
			if rerr, ok := err.(*realizer.Error); ok {
				res.Err = rerr
				return res, nil
			}
			return res, err
		}
	}
	// Only this backend's native preflight authorizes a profile mutation. A
	// foreign DTO must be prepared first; otherwise the caller retries explicitly.
	for _, item := range toAdd {
		owned := false
		for _, prepared := range b.prepared {
			if item.ID == prepared.ID && item.Ref == prepared.Ref && item.Resolution == prepared.Resolution {
				owned = true
				break
			}
		}
		if !owned {
			res.Err = unresolved(fmt.Errorf("mutation has no prepared native resolution"))
			return res, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(b.Profile), 0o755); err != nil {
		res.Err = &realizer.Error{Code: envelope.ErrInternalError, Subcode: "profile-dir", Stage: "commit", Raw: err.Error()}
		return res, nil
	}

	before := b.gen()
	res.FromGeneration = before

	args := []string{"profile", "add", "--profile", b.Profile}
	for _, ins := range toAdd {
		r := ins.Resolution
		source := strings.TrimPrefix(r.WrapperURL, "path:")
		if !storePath(source) || !strings.HasPrefix(r.AttrPath, "packages."+nixSystem()+".p") || len(r.Outputs) == 0 {
			res.Err = unresolved(fmt.Errorf("invalid prepared native identity"))
			return res, nil
		}
		_, attr, _ := strings.Cut(r.AttrPath, "packages."+nixSystem()+".")
		mutation := r.WrapperURL + "#" + attr + "^" + strings.Join(r.Outputs, ",")
		if mutation != r.MutationRef {
			res.Err = unresolved(fmt.Errorf("prepared mutation reference changed"))
			return res, nil
		}
		args = append(args, mutation)
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

	if res.Advanced {
		for _, item := range toAdd {
			if err := b.observeOutputs(item.Resolution); err != nil {
				res.Err = &realizer.Error{Code: envelope.ErrInstallFailed, Subcode: "output-provenance", Stage: "commit", Raw: err.Error()}
				break
			}
		}
	}
	cur, _ := b.Current()
	res.After = cur
	return res, nil
}

func needsPreparation(items []realizer.Installable) bool {
	for _, item := range items {
		if item.Resolution == nil {
			return true
		}
	}
	return false
}

func installablePresent(set realizer.Set, ins realizer.Installable) bool {
	for _, element := range set.Elements {
		if realizer.Matches(element, ins) {
			return true
		}
	}
	return isPresent(set, ins.Ref)
}
