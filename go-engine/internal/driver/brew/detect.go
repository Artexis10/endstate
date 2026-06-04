// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import (
	"errors"
	"os/exec"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// ErrBrewNotAvailable is returned when the brew binary cannot be found on PATH.
// Callers should surface this as a backend-unavailable condition in the JSON
// envelope rather than treating it as a per-package failure (mirrors winget's
// ErrWingetNotAvailable).
var ErrBrewNotAvailable = errors.New("brew is not installed or not available on PATH")

// Detect reports whether the package identified by ref is currently installed.
//
// It runs (ASSUMPTION: exit 0 == present, non-zero == absent — see brew.go #1):
//
//	brew list <name>            (formula)
//	brew list --cask <name>     (cask)
//
// and interprets the exit code:
//   - 0        → installed (true, name, nil)
//   - non-zero → not installed (false, "", nil)
//   - binary not found → (false, "", ErrBrewNotAvailable)
//
// The display name is simply the bare package name (brew's identifier is the
// human-facing name); we keep this simple rather than parsing `brew info`.
func (b *BrewDriver) Detect(ref string) (bool, string, error) {
	name, isCask := parseRef(ref)

	args := []string{"list"}
	if isCask {
		args = append(args, "--cask")
	}
	args = append(args, name)

	cmd := b.ExecCommand("brew", args...)

	// brew list writes the package contents to stdout; we only need the exit
	// code, so we discard output but still must not let a missing binary slip
	// past as "not installed".
	err := cmd.Run()
	if err == nil {
		return true, name, nil
	}

	// Distinguish "brew not found" from "package not found".
	var execErr *exec.Error
	if errors.As(err, &execErr) && execErr.Err == exec.ErrNotFound {
		return false, "", ErrBrewNotAvailable
	}

	// Any non-zero exit code from brew means the package is not listed.
	return false, "", nil
}

// DetectBatch checks multiple refs by parsing `brew list --versions` (formulae)
// and `brew list --cask --versions` (casks). It runs at most two brew processes
// total regardless of ref count (one per kind that appears in refs), each
// parsed into name→version, then matches each ref against the right map.
//
// ASSUMPTION (see brew.go header #2): `brew list --versions` prints one package
// per line as "<name> <version> [<version> ...]"; the first token is the name,
// the second (if any) is the version we capture.
func (b *BrewDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	// Determine which kinds we actually need to query.
	needFormula, needCask := false, false
	for _, ref := range refs {
		if _, isCask := parseRef(ref); isCask {
			needCask = true
		} else {
			needFormula = true
		}
	}

	var formulaVersions, caskVersions map[string]string

	if needFormula {
		m, err := b.listVersions(false)
		if err != nil {
			return nil, err
		}
		formulaVersions = m
	}
	if needCask {
		m, err := b.listVersions(true)
		if err != nil {
			return nil, err
		}
		caskVersions = m
	}

	results := make(map[string]driver.DetectResult, len(refs))
	for _, ref := range refs {
		name, isCask := parseRef(ref)
		versions := formulaVersions
		if isCask {
			versions = caskVersions
		}
		version, found := versions[strings.ToLower(name)]
		results[ref] = driver.DetectResult{
			Installed:   found,
			DisplayName: name,
			Version:     version,
		}
	}
	return results, nil
}

// listVersions runs `brew list --versions` (or with `--cask`) and parses the
// output into a case-insensitive name→version map. A missing brew binary
// surfaces as ErrBrewNotAvailable; a non-zero exit with output is still parsed
// best-effort (brew may exit non-zero yet emit a usable list), and an empty map
// is returned when nothing parses.
func (b *BrewDriver) listVersions(cask bool) (map[string]string, error) {
	args := []string{"list", "--versions"}
	if cask {
		// `brew list --cask --versions` — kind flag before the query flag,
		// matching the install/detect ordering.
		args = []string{"list", "--cask", "--versions"}
	}

	cmd := b.ExecCommand("brew", args...)

	var stdout strings.Builder
	cmd.Stdout = &stdout

	err := cmd.Run()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) && execErr.Err == exec.ErrNotFound {
			return nil, ErrBrewNotAvailable
		}
		// Non-zero exit (not a missing binary): parse whatever was emitted.
	}

	return parseVersions(stdout.String()), nil
}

// parseVersions parses `brew list --versions` output into a case-insensitive
// name→version map. Each non-empty line is split on whitespace: token[0] is the
// package name, token[1] (if present) is the version. A name-only line is
// recorded with an empty version (still "installed"). Defensive: blank lines and
// all-whitespace lines are skipped.
func parseVersions(output string) map[string]string {
	out := make(map[string]string)
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		version := ""
		if len(fields) > 1 {
			version = fields[1]
		}
		out[strings.ToLower(name)] = version
	}
	return out
}
