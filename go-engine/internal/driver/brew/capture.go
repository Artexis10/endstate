// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"errors"
	"os/exec"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

var _ driver.InstalledEnumerator = (*BrewDriver)(nil)

// EnumerateInstalled lists the brew packages to record in a captured manifest:
// top-level formulae (`brew leaves` — excludes dependency-only formulae) and
// installed Casks (`brew list --cask`). Versions come from `brew list --versions`
// (formulae) and `brew list --cask --versions` (Casks), matched by name; a
// package with no exposed version is recorded with an empty Version rather than
// failing the capture.
//
// REAL-OUTPUT ANCHORS ARE ASSUMPTIONS (the winget lesson — see brew.go header):
//   - `brew leaves` prints one top-level formula per line.
//   - `brew list --cask` prints one installed Cask token per line.
//   - both `--versions` variants follow the `<name> <version> ...` shape parsed
//     by parseVersions.
//
// These are validated ONLY by the real-macOS smoke; the hermetic tests lock the
// assumed shapes. A missing brew binary surfaces as ErrBrewNotAvailable so the
// caller treats it as backend-unavailable rather than per-package.
func (b *BrewDriver) EnumerateInstalled() ([]driver.InstalledPackage, error) {
	formulae, err := b.listLines("leaves")
	if err != nil {
		return nil, err
	}
	casks, err := b.listLines("list", "--cask")
	if err != nil {
		return nil, err
	}

	// Versions, best-effort: a non-fatal parse, keyed case-insensitively by name.
	var formulaVersions, caskVersions map[string]string
	if len(formulae) > 0 {
		formulaVersions, _ = b.listVersions(false)
	}
	if len(casks) > 0 {
		caskVersions, _ = b.listVersions(true)
	}

	sort.Strings(formulae)
	sort.Strings(casks)
	apps := make([]driver.InstalledPackage, 0, len(formulae)+len(casks))
	for _, name := range formulae {
		apps = append(apps, driver.InstalledPackage{
			DisplayName: name,
			Ref:         name,
			Version:     formulaVersions[strings.ToLower(name)],
		})
	}
	for _, name := range casks {
		apps = append(apps, driver.InstalledPackage{
			DisplayName: name,
			Ref:         caskPrefix + name,
			Version:     caskVersions[strings.ToLower(name)],
		})
	}
	return apps, nil
}

// listLines runs `brew <args...>` and returns the non-empty, trimmed stdout
// lines (each a package token). A missing brew binary surfaces as
// ErrBrewNotAvailable; a non-zero exit with usable output is parsed best-effort
// (mirroring listVersions), and no output yields an empty slice.
func (b *BrewDriver) listLines(args ...string) ([]string, error) {
	cmd := b.ExecCommand("brew", args...)
	var stdout strings.Builder
	cmd.Stdout = &stdout

	if err := cmd.Run(); err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && execErr.Err == exec.ErrNotFound {
			return nil, ErrBrewNotAvailable
		}
		// Non-zero exit (not a missing binary): parse whatever was emitted.
	}

	var out []string
	for _, raw := range strings.Split(stdout.String(), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// `brew leaves`/`brew list --cask` print a bare token per line, but guard
		// against any trailing columns by taking the first field.
		out = append(out, strings.Fields(line)[0])
	}
	return out, nil
}
