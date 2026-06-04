// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package brew

import "github.com/Artexis10/endstate/go-engine/internal/driver"

// compile-time assertion: the brew driver implements the optional
// VersionedInstaller capability, so the apply path can discover version-pinning
// eligibility by type-assertion — exactly as it does for winget.
var _ driver.VersionedInstaller = (*BrewDriver)(nil)

// InstallVersion installs ref, honoring a version in the WEAK / advisory sense.
//
// Homebrew is fundamentally a rolling, latest-only package manager: it has no
// general `--version <v>` flag and cannot install an arbitrary historical
// version of a formula. The ONLY supported pinning mechanism is a
// versioned-formula ref (e.g. "node@20", "python@3.11"), where the version is
// encoded in the package NAME and selected as a distinct formula. Such a ref
// flows through unchanged (parseRef keeps the "@version" suffix on the name).
//
// For a bare ref with a separately-declared version, the version is ADVISORY:
// we install the latest and record the requested version in the message rather
// than attempting an unsupported downgrade. We deliberately do NOT try to force
// an arbitrary version — that is the documented weakness of this backend versus
// winget's true `--version` pinning. A real-macOS smoke (later increment) should
// confirm versioned-formula refs install the expected major version.
func (b *BrewDriver) InstallVersion(ref, version string) (*driver.InstallResult, error) {
	return b.install(ref, version, false)
}

// ReinstallVersion force-reinstalls ref (skipping the detect-before-install
// short-circuit) via `brew install --force`. It is the convergence step of
// `apply --repin`; like InstallVersion, the version is honored only weakly
// (versioned-formula ref) — brew cannot force an arbitrary historical version.
func (b *BrewDriver) ReinstallVersion(ref, version string) (*driver.InstallResult, error) {
	return b.install(ref, version, true)
}
