// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// compile-time assertion: the Nix backend implements realizer.HomeActivator, so
// the apply path can discover home-manager config support by type-assertion.
var _ realizer.HomeActivator = (*Backend)(nil)

// compile-time assertion: the Nix backend implements realizer.HomeGenerationReader,
// so the verify path can discover home-manager generation reading by type-assertion.
var _ realizer.HomeGenerationReader = (*Backend)(nil)

// ActiveHomeGeneration returns the active home-manager generation number by
// delegating to homeGen(), which reads the profile symlink (or the injected
// homeGenFn in tests). Returns 0 when no home-manager generation is active.
func (b *Backend) ActiveHomeGeneration() int { return b.homeGen() }

// ActivateHome prepares an exact native activation for direct callers. The
// command path prepares it before package mutation and reuses that built output.
func (b *Backend) ActivateHome(flake string) (generation int, resultErr error) {
	defer func() {
		committed := generation > 0 || resultErr == nil
		if failure, ok := resultErr.(*realizer.Error); ok && failure.Stage == "commit" {
			committed = true
		}
		b.FinalizePreparation(committed)
	}()
	r, err := b.PrepareHome(flake, true)
	if err != nil {
		return 0, err
	}
	return b.ActivatePreparedHome(r)
}

// normalizeHomeFlakeRef makes generated absolute directories explicit path
// references. Nix 3 otherwise searches for a containing Git repository and
// rejects an Endstate state directory that correctly lives outside one.
func normalizeHomeFlakeRef(flake string) string {
	if strings.HasPrefix(flake, "path:") {
		return flake
	}
	location, fragment, hasFragment := strings.Cut(flake, "#")
	normalizedPath, absolute := portableAbsoluteHomePath(location)
	if !absolute {
		return flake
	}
	normalized := "path:" + normalizedPath
	if hasFragment {
		normalized += "#" + fragment
	}
	return normalized
}

func portableAbsoluteHomePath(value string) (string, bool) {
	// Generated flakes use POSIX paths on Linux and Darwin. Recognise them
	// independently of the host compiling or testing the reference.
	if strings.HasPrefix(value, "/") {
		return strings.ReplaceAll(value, "\\", "/"), true
	}
	if filepath.IsAbs(value) {
		return filepath.ToSlash(value), true
	}
	// Keep drive-letter paths deterministic when manifests or tests cross an
	// OS boundary; filepath.IsAbs intentionally follows only the current host.
	if len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) &&
		value[1] == ':' && (value[2] == '/' || value[2] == '\\') {
		return strings.ReplaceAll(value, "\\", "/"), true
	}
	return "", false
}

// homeGen returns the active home-manager generation number, via homeGenFn when
// set (tests) else by reading the home-manager profile symlink.
func (b *Backend) homeGen() int {
	if b.homeGenFn != nil {
		return b.homeGenFn()
	}
	return homeGeneration()
}

// homeGeneration reads the active home-manager generation number from the
// home-manager nix-profile symlink (<...>/nix/profiles/home-manager ->
// home-manager-<N>-link). Returns 0 when the profile does not exist. The path
// and `-<N>-link` naming were confirmed against real home-manager.
func homeGeneration() int {
	target, err := os.Readlink(homeProfilePath())
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

// homeProfilePath returns the home-manager nix-profile path under the XDG state
// dir ($XDG_STATE_HOME/nix/profiles/home-manager, default
// ~/.local/state/nix/profiles/home-manager), following the same XDG convention
// as DefaultProfile rather than a hardcoded absolute path.
func homeProfilePath() string {
	return filepath.Join(profileStateHome(), "nix", "profiles", "home-manager")
}

// parsePlainLog builds a parsedLog from home-manager's PLAIN-TEXT switch stderr
// (not nix internal-json). Any `@nix {...}` lines the outer build emits are
// folded in via parseInternalJSON (so those structural signals are kept), then
// every remaining non-empty plain line becomes an error candidate so the locked
// anchor table can match systemic classes and the raw text is retained for
// error.detail. This is the home-manager analogue of parseInternalJSON, which
// alone would miss home-manager's plain output.
func parsePlainLog(stderr []byte) parsedLog {
	p := parseInternalJSON(stderr)
	var sb strings.Builder
	sb.WriteString(p.blob)
	for _, ln := range strings.Split(string(stderr), "\n") {
		t := stripANSI(strings.TrimSpace(ln))
		if t == "" || strings.HasPrefix(t, "@nix ") {
			continue
		}
		p.errorMsgs = append(p.errorMsgs, t)
		sb.WriteString(strings.ToLower(t))
		sb.WriteByte('\n')
	}
	p.blob = sb.String()
	return p
}

// PrepareHome uses native installable selection, then builds only the frozen
// activation derivation. The out-link retains the output through finalization.
func (b *Backend) PrepareHome(flake string, build bool) (*realizer.InputResolution, error) {
	normalized := normalizeHomeFlakeRef(flake)
	root, name, ok := strings.Cut(normalized, "#")
	if !ok || name == "" {
		return nil, unresolved(fmt.Errorf("home configuration name is required"))
	}
	target := root + "#homeConfigurations." + name + ".activationPackage"
	r, err := b.resolve(target)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(filepath.Dir(b.Profile), "activation-root-")
	if err == nil {
		b.rootDirs = append(b.rootDirs, dir)
	}
	if err != nil {
		return nil, unresolved(err)
	}
	if build {
		value, err := b.native("build", "--json", "--out-link", filepath.Join(dir, "activation"), r.MutationRef)
		if err != nil {
			return nil, unresolved(err)
		}
		var built []struct {
			DrvPath string            `json:"drvPath"`
			Outputs map[string]string `json:"outputs"`
		}
		if json.Unmarshal(value, &built) != nil || len(built) != 1 || built[0].DrvPath != r.Derivation {
			return nil, unresolved(fmt.Errorf("frozen activation build changed its derivation"))
		}
		for _, name := range r.Outputs {
			path := built[0].Outputs[name]
			if !storePath(path) {
				return nil, unresolved(fmt.Errorf("activation output unavailable"))
			}
			r.OutputPaths[name] = path
		}
	}
	return r, nil
}

func (b *Backend) ActivatePreparedHome(r *realizer.InputResolution) (int, error) {
	if r == nil || len(r.Outputs) != 1 || r.OutputPaths[r.Outputs[0]] == "" {
		return 0, unresolved(fmt.Errorf("activation has no single frozen output"))
	}
	output := r.OutputPaths[r.Outputs[0]]
	if !storePath(output) {
		return 0, unresolved(fmt.Errorf("invalid activation output"))
	}
	return b.activateHomeOutput(output)
}

// Failed settings attempts invalidate completion even if the intended output
// was already active. Observe advancement separately from that active identity.
func (b *Backend) activateHomeOutput(output string) (int, error) {
	beforeGeneration := b.homeGen()
	beforeOutput := b.ActiveHomeOutput()
	_, stderr, exit, err := b.execScript(path.Join(output, "activate"))
	generation := b.homeGen()
	activeOutput := b.ActiveHomeOutput()
	if err != nil || exit != 0 {
		if err != nil {
			exit = -1
		}
		failure := classify(exit, parsePlainLog(stderr), false)
		if generation > 0 && activeOutput == output {
			failure.Stage = "activation"
			if generation != beforeGeneration || (beforeOutput != "" && activeOutput != beforeOutput) {
				failure.Stage = "commit"
			}
			return generation, failure
		}
		return 0, failure
	}
	if activeOutput != output {
		return 0, &realizer.Error{Code: envelope.ErrInstallFailed, Subcode: "activation-provenance", Stage: "commit", Raw: "activation did not commit the prepared output"}
	}
	if generation <= 0 {
		return 0, &realizer.Error{Code: envelope.ErrInstallFailed, Subcode: "activation-provenance", Stage: "commit", Raw: "active native generation could not be observed"}
	}
	return generation, nil
}

// ActiveHomeOutput checks actual ownership, independently of generation labels.
func (b *Backend) ActiveHomeOutput() string {
	// A single native opaque path has one canonical result. Plain path output
	// avoids the versioned path-info JSON shapes without parsing Nix source.
	data, err := b.native("path-info", homeProfilePath())
	if err != nil {
		return ""
	}
	path := strings.TrimSpace(string(data))
	if !storePath(path) {
		return ""
	}
	return path
}
