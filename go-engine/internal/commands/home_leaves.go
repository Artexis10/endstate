// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/realizer"
	"github.com/Artexis10/endstate/go-engine/internal/realizer/nix"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

// Tests replace the store root with a fake built activation and lower the bound.
var (
	homeStoreRoot  = nix.StoreRoot
	homeEntryLimit = nix.MaxHomeEntries
)

// builtHomeLeaves is the exact leaf inventory of one built activation package.
type builtHomeLeaves struct {
	home string
	dirs []nix.HomeFilesDir
}

type builtHomeLeavesRead struct {
	leaves *builtHomeLeaves
	err    error
}

// readBuiltHomeLeaves lists what activation links below the home directory it
// inherits. Home Manager links `$HOME/<path>` and refuses to run when `$HOME`
// differs from `home.homeDirectory`, so the inherited home is the placement root
// even for an activation-only flake that exposes no parent configuration.
func readBuiltHomeLeaves(resolution *realizer.InputResolution) (*builtHomeLeaves, error) {
	if resolution == nil || len(resolution.Outputs) != 1 {
		return nil, fmt.Errorf("Home Manager activation has no single built output")
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return nil, fmt.Errorf("the home directory that activation inherits is unavailable")
	}
	dirs, err := nix.ReadHomeFiles(resolution.OutputPaths[resolution.Outputs[0]], homeStoreRoot, homeEntryLimit)
	if err != nil {
		return nil, err
	}
	return &builtHomeLeaves{home: filepath.Clean(home), dirs: dirs}, nil
}

// builtLeaves reads each built activation once: the captured placement check and
// the unified collision check then share one walk.
func (home *preparedHomeManager) builtLeaves(resolution *realizer.InputResolution) (*builtHomeLeaves, error) {
	if read, ok := home.leafReads[resolution]; ok {
		return read.leaves, read.err
	}
	leaves, err := readBuiltHomeLeaves(resolution)
	if err != nil && home.leafError == nil {
		home.leafError = err
	}
	if home.leafReads == nil {
		home.leafReads = map[*realizer.InputResolution]builtHomeLeavesRead{}
	}
	home.leafReads[resolution] = builtHomeLeavesRead{leaves: leaves, err: err}
	return leaves, err
}

// blockDiagnostic names why a built tree could not be read; it is empty when no
// read failed.
func (home *preparedHomeManager) blockDiagnostic() string {
	if home == nil || home.leafError == nil {
		return ""
	}
	return home.leafError.Error()
}

// builtLeafClaims returns the file claims of the activation that this apply would
// run. It reads nothing when no activation was prepared.
func (home *preparedHomeManager) builtLeafClaims() ([]string, error) {
	if home.resolution == nil {
		return nil, nil
	}
	leaves, err := home.builtLeaves(home.resolution)
	if err != nil {
		return nil, err
	}
	return leaves.claims(), nil
}

// claims maps every leaf to file claims, one set per lexical or aliased identity
// of its directory. The aliases of each tree directory resolve once, from the
// identities of its parent. Resolving every directory's full ancestor chain with
// restore.FilesystemTargetIdentities instead rescanned the same ancestors for each
// directory and took 17 to 100 seconds for 100,000 leaves in 1,000 directories.
func (leaves *builtHomeLeaves) claims() []string {
	identities := map[string][]string{"": homeDirectoryIdentities(leaves.home)}
	var directory func(dir string) []string
	directory = func(dir string) []string {
		if known, ok := identities[dir]; ok {
			return known
		}
		parent, name := path.Split(dir)
		var own []string
		for _, identity := range directory(strings.TrimSuffix(parent, "/")) {
			own = append(own, identity+"/"+name)
		}
		live := filepath.Join(leaves.home, filepath.FromSlash(dir))
		// A live link in the directory's own place adds the identity of its referent.
		if resolved, err := filepath.EvalSymlinks(live); err == nil && !slices.Contains(own, filepath.ToSlash(resolved)) {
			if identity, err := restore.FilesystemTargetIdentity(resolved); err == nil && !slices.Contains(own, filepath.ToSlash(identity)) {
				own = append(own, filepath.ToSlash(identity))
			}
		}
		identities[dir] = own
		return own
	}
	var claims []string
	for _, dir := range leaves.dirs {
		for _, identity := range directory(dir.Dir) {
			for _, name := range dir.Names {
				claims = append(claims, "file\x00"+identity+"/"+name)
			}
		}
	}
	return claims
}

// homeDirectoryIdentities returns the lexical identity of a directory with every
// alias through its live ancestors, and through the directory itself. Slash
// separated, without a trailing slash.
func homeDirectoryIdentities(directory string) []string {
	identities, err := restore.FilesystemTargetIdentities(directory)
	if err != nil {
		// The declared placement is still a known lexical claim.
		identities = []string{directory}
	} else if resolved, err := filepath.EvalSymlinks(directory); err == nil {
		if identity, err := restore.FilesystemTargetIdentity(resolved); err == nil && !slices.Contains(identities, identity) {
			identities = append(identities, identity)
		}
	}
	for index, identity := range identities {
		identities[index] = strings.TrimSuffix(filepath.ToSlash(identity), "/")
	}
	return identities
}

// missingTargets returns the placements that are not leaves of this activation.
func (leaves *builtHomeLeaves) missingTargets(targets []string) []string {
	wanted := make(map[string]string, len(targets))
	var missing []string
	for _, target := range targets {
		relative, err := nix.HomeRelativeTarget(target)
		if err != nil {
			missing = append(missing, target)
			continue
		}
		wanted[relative] = target
	}
	for _, dir := range leaves.dirs {
		for _, name := range dir.Names {
			delete(wanted, path.Join(dir.Dir, name))
		}
	}
	for _, target := range wanted {
		missing = append(missing, target)
	}
	return missing
}

// anyClaimOverlaps reports whether one claim overlaps any claim in the list.
func anyClaimOverlaps(claims []string, claim string) bool {
	for _, candidate := range claims {
		if concreteConfigRestoreClaimsOverlap(candidate, claim) {
			return true
		}
	}
	return false
}
