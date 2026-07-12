// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package importer

import (
	"fmt"
	"sort"
	"strings"
)

// MapOptions controls how a Bundle is mapped onto manifest app entries.
type MapOptions struct {
	// Pin, when true, records a version on each imported app: the package's
	// InstallationOptions.Version pin when present (authored intent), otherwise
	// the bundle's observed Version. When false, no version is written.
	Pin bool
}

// ImportedApp is a winget-source package mapped to an Endstate manifest app.
// Ref is the winget package Id (the manifest's refs.windows value); ID is the
// deterministic, de-duplicated manifest app id.
type ImportedApp struct {
	ID          string `json:"id"`
	Ref         string `json:"ref"`
	DisplayName string `json:"displayName,omitempty"`
	Version     string `json:"version,omitempty"`
}

// SkippedPackage records a bundle package that was NOT imported, with the
// managing tool and the reason. Skip transparency is a first-class output: no
// package is silently dropped.
type SkippedPackage struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Manager string `json:"manager"`
	Reason  string `json:"reason"`
}

// IncompatibleEntry is a bundle incompatible_packages entry passed through to
// the report verbatim.
type IncompatibleEntry struct {
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`
	Version string `json:"version,omitempty"`
	Source  string `json:"source,omitempty"`
}

// MapResult is the deterministic outcome of mapping a Bundle. Imported, Skipped
// and Incompatible together account for every package and incompatible entry in
// the bundle. Collisions notes any app-id de-duplication that occurred.
type MapResult struct {
	Imported     []ImportedApp
	Skipped      []SkippedPackage
	Incompatible []IncompatibleEntry
	Collisions   []string
}

// MapBundle maps a UniGetUI Bundle onto manifest app entries. Winget-source
// packages become ImportedApp entries (Id → refs.windows, Name → displayName,
// a deterministic slug → app id); every other package is Skipped with its
// manager and a reason; incompatible_packages pass through. Output ordering is
// canonicalised — winget packages are sorted by Id (case-insensitive) before
// slug/de-dup assignment, and skipped/incompatible are sorted before returning —
// so two exports of the same machine that differ only in package order yield
// identical output.
func MapBundle(b *Bundle, opts MapOptions) *MapResult {
	res := &MapResult{
		Imported:     []ImportedApp{},
		Skipped:      []SkippedPackage{},
		Incompatible: []IncompatibleEntry{},
	}

	// Partition first: route non-winget and Id-less packages straight to skipped
	// (order-independent, they are sorted at the end), and collect the winget
	// candidates for a stable, order-independent mapping pass.
	winget := make([]Package, 0, len(b.Packages))
	for _, pkg := range b.Packages {
		if !isWingetPackage(pkg.Source, pkg.ManagerName) {
			res.Skipped = append(res.Skipped, SkippedPackage{
				ID:      strings.TrimSpace(pkg.ID),
				Name:    strings.TrimSpace(pkg.Name),
				Manager: managerLabel(pkg.Source, pkg.ManagerName),
				Reason:  "unsupported package manager (Endstate installs via winget on Windows)",
			})
			continue
		}
		if strings.TrimSpace(pkg.ID) == "" {
			res.Skipped = append(res.Skipped, SkippedPackage{
				Name:    strings.TrimSpace(pkg.Name),
				Manager: "winget",
				Reason:  "winget package has no Id",
			})
			continue
		}
		winget = append(winget, pkg)
	}

	// Sort winget candidates by Id (case-insensitive) so slug assignment and
	// collision suffixes are stable regardless of the bundle's package order.
	sort.SliceStable(winget, func(i, j int) bool {
		return strings.ToLower(strings.TrimSpace(winget[i].ID)) <
			strings.ToLower(strings.TrimSpace(winget[j].ID))
	})

	// seen tracks every app id already assigned so de-duplication is stable and
	// collision-safe even against a synthesized "-N" suffix. slugByRef records
	// the slug assigned to each winget Id (case-insensitive) so a repeated Id is
	// reported as a duplicate of the first occurrence rather than mapped twice.
	seen := make(map[string]bool)
	slugByRef := make(map[string]string)

	for _, pkg := range winget {
		id := strings.TrimSpace(pkg.ID)
		if firstSlug, dup := slugByRef[strings.ToLower(id)]; dup {
			res.Skipped = append(res.Skipped, SkippedPackage{
				ID:      id,
				Name:    strings.TrimSpace(pkg.Name),
				Manager: "winget",
				Reason:  fmt.Sprintf("duplicate of %s", firstSlug),
			})
			continue
		}

		base := slugForID(id)
		slug := uniqueSlug(base, seen)
		if slug != base {
			res.Collisions = append(res.Collisions, fmt.Sprintf(
				"app id %q collided; imported as %q (winget id %q)", base, slug, id))
		}
		slugByRef[strings.ToLower(id)] = slug

		app := ImportedApp{
			ID:          slug,
			Ref:         id,
			DisplayName: strings.TrimSpace(pkg.Name),
		}
		if opts.Pin {
			v := strings.TrimSpace(pkg.InstallationOptions.Version)
			if v == "" {
				v = strings.TrimSpace(pkg.Version)
			}
			app.Version = v
		}
		res.Imported = append(res.Imported, app)
	}

	for _, inc := range b.IncompatiblePackages {
		res.Incompatible = append(res.Incompatible, IncompatibleEntry{
			ID:      strings.TrimSpace(inc.ID),
			Name:    strings.TrimSpace(inc.Name),
			Version: strings.TrimSpace(inc.Version),
			Source:  strings.TrimSpace(inc.Source),
		})
	}

	// Canonical ordering for the transparency lists: skipped by manager then Id,
	// incompatible by Id.
	sort.SliceStable(res.Skipped, func(i, j int) bool {
		if res.Skipped[i].Manager != res.Skipped[j].Manager {
			return res.Skipped[i].Manager < res.Skipped[j].Manager
		}
		return res.Skipped[i].ID < res.Skipped[j].ID
	})
	sort.SliceStable(res.Incompatible, func(i, j int) bool {
		return res.Incompatible[i].ID < res.Incompatible[j].ID
	})

	return res
}

// isWingetPackage reports whether a package should be treated as winget-source.
// Source is authoritative when present (case-insensitive "winget"); when Source
// is empty, ManagerName is the fallback discriminator. A package from another
// source (chocolatey, pip, a scoop bucket, msstore) is therefore not winget even
// if the WinGet manager brokered it.
func isWingetPackage(source, managerName string) bool {
	s := strings.TrimSpace(source)
	if s != "" {
		return strings.EqualFold(s, "winget")
	}
	return strings.EqualFold(strings.TrimSpace(managerName), "winget")
}

// managerLabel returns the human-facing managing tool for a skip report. It
// prefers ManagerName (the manager, e.g. "Scoop") over Source (which may be a
// sub-source or bucket, e.g. "main"), falling back to "unknown".
func managerLabel(source, managerName string) string {
	if m := strings.TrimSpace(managerName); m != "" {
		return m
	}
	if s := strings.TrimSpace(source); s != "" {
		return s
	}
	return "unknown"
}

// slugForID derives a manifest app id from a winget package Id: the lowercased
// last dot-segment, sanitized to [a-z0-9-]. "Microsoft.VisualStudioCode" →
// "visualstudiocode", "Git.Git" → "git". Falls back to "app" if nothing usable
// remains.
func slugForID(id string) string {
	seg := id
	if idx := strings.LastIndex(id, "."); idx >= 0 && idx < len(id)-1 {
		seg = id[idx+1:]
	}
	seg = strings.ToLower(strings.TrimSpace(seg))

	var b strings.Builder
	for _, r := range seg {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	s := b.String()
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if s == "" {
		return "app"
	}
	return s
}

// uniqueSlug returns base if unused, otherwise the first free "base-N" (N≥2),
// marking the returned slug as used. Deterministic given a stable call order.
func uniqueSlug(base string, seen map[string]bool) string {
	if !seen[base] {
		seen[base] = true
		return base
	}
	for n := 2; ; n++ {
		cand := fmt.Sprintf("%s-%d", base, n)
		if !seen[cand] {
			seen[cand] = true
			return cand
		}
	}
}
