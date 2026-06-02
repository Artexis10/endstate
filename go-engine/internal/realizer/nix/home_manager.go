// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

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

// ActivateHome activates a declared home-manager configuration as the realizer's
// config stage of apply. The engine owns the invocation — it runs the
// home-manager CLI via `nix run <HomePin> -- switch --flake <flake> -b
// endstate-backup` so the user never installs or learns home-manager (the moat).
// `-b endstate-backup` makes home-manager move any pre-existing file it would
// replace to `<file>.endstate-backup` instead of failing (honors
// backup-before-overwrite — confirmed against real home-manager).
//
// On success it returns the resulting home-manager generation number (read from
// the home-manager profile symlink, the same way the package generation is
// read). On failure it returns a classified *realizer.Error through the existing
// anchor path: spawn/daemon → REALIZER_UNAVAILABLE, permission →
// PERMISSION_DENIED, anything else → INSTALL_FAILED (the config-activation
// failure class), with raw home-manager/Nix text confined to Error.Raw (destined
// only for envelope error.detail). home-manager's switch output is plain text
// (not nix internal-json), so classification anchors against the raw stderr.
func (b *Backend) ActivateHome(flake string) (int, error) {
	pin := b.HomePin
	if pin == "" {
		pin = defaultHomePin()
	}
	// `--` separates nix's args from the home-manager program's args; the runner
	// inserts the experimental-features flag before it (see nixArgs).
	args := []string{"run", pin, "--", "switch", "--flake", flake, "-b", "endstate-backup"}

	_, stderr, exit, err := b.Run(args...)
	if err != nil { // spawn failure (nix missing/unrunnable)
		return 0, classify(-1, parsePlainLog(stderr), false)
	}
	if exit != 0 {
		return 0, classify(exit, parsePlainLog(stderr), false)
	}
	return b.homeGen(), nil
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
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, "nix", "profiles", "home-manager")
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
