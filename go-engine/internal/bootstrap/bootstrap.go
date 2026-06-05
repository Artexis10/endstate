// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package bootstrap implements the backend-agnostic "the engine installs its own
// package backend when it is absent" contract: detect → consent → official
// install → verify → proceed-or-decline. It is the mechanism layer; the consent
// decision (flags + the streamed consent-request event) is orchestrated by the
// commands package. The detect/install/verify steps are injectable seams so the
// contract is hermetically testable — NO real installer ever runs in `go test`
// (the install path can only be validated on a real clean machine, not reasoned
// about in CI; the same lesson the winget driver learned for real command output).
package bootstrap

import (
	"os"
	"os/exec"
)

// Backend identifies a package backend the engine can bootstrap on macOS/Linux.
// Windows is never bootstrapped (winget ships with the OS).
type Backend string

const (
	// BackendBrew is the Homebrew driver (darwin `driver: "brew"` lane).
	BackendBrew Backend = "brew"
	// BackendNix is the Nix realizer (the cross-OS default lane).
	BackendNix Backend = "nix"
)

// Outcome is the per-backend result of Provision (install + verify).
type Outcome int

const (
	// OutcomeInstalled means the official installer ran and the verify probe
	// passed: the backend is available for use.
	OutcomeInstalled Outcome = iota
	// OutcomeInstallFailed means the official installer returned an error; verify
	// was not attempted. The backend is unavailable.
	OutcomeInstallFailed
	// OutcomeVerifyFailed means the installer completed but the verify probe did
	// not confirm a working backend. The backend is treated as unavailable rather
	// than used half-configured (verification-first: "exit 0" is not success).
	OutcomeVerifyFailed
)

// Available reports whether a Provision outcome leaves the backend usable. Only a
// freshly-installed-and-verified backend is available; an install or verify
// failure is not.
func (o Outcome) Available() bool { return o == OutcomeInstalled }

// Bootstrapper runs the detect/install/verify contract for a set of backends
// behind injectable seams. New() wires the real seams; tests construct a
// Bootstrapper with fakes and never touch a real installer.
type Bootstrapper struct {
	// Detect reports whether a backend is already present and working.
	Detect func(b Backend) (present bool, err error)
	// Install shells the backend's OFFICIAL upstream installer. It is the only
	// place a privileged installer is run, and it is never called under `go test`.
	Install func(b Backend) error
	// Verify runs the backend's post-install probe (e.g. `<backend> --version`).
	Verify func(b Backend) (ok bool, err error)
}

// New returns a Bootstrapper wired to the real detect/install/verify seams.
func New() *Bootstrapper {
	return &Bootstrapper{
		Detect:  realDetect,
		Install: realInstall,
		Verify:  realVerify,
	}
}

// Probe detects each needed backend and partitions the set into absent (needs
// install) and present (already working), preserving input order. A detect error
// is treated as absent: the engine would rather offer to install than wrongly
// assume a backend is present and then hard-fail mid-run.
func (bs *Bootstrapper) Probe(needed []Backend) (absent, present []Backend) {
	for _, b := range needed {
		ok, err := bs.Detect(b)
		if err == nil && ok {
			present = append(present, b)
		} else {
			absent = append(absent, b)
		}
	}
	return absent, present
}

// Provision installs and verifies each given backend, returning a per-backend
// Outcome. It is called ONLY after consent has been granted for the combined set.
// Each backend is independent: install error → OutcomeInstallFailed (verify
// skipped); installer ok but verify not-working/error → OutcomeVerifyFailed;
// install ok and verify ok → OutcomeInstalled.
func (bs *Bootstrapper) Provision(absent []Backend) map[Backend]Outcome {
	out := make(map[Backend]Outcome, len(absent))
	for _, b := range absent {
		if err := bs.Install(b); err != nil {
			out[b] = OutcomeInstallFailed
			continue
		}
		ok, err := bs.Verify(b)
		if err != nil || !ok {
			out[b] = OutcomeVerifyFailed
			continue
		}
		out[b] = OutcomeInstalled
	}
	return out
}

// ConsentMessage returns the plain-language, product-neutral consent ask for a
// combined set of absent backends. It deliberately names NO backend product
// ("Nix"/"Homebrew") — the concepts stay invisible — while staying honest about
// the privileged footprint (an administrator password, a background helper, a
// dedicated storage area) and pointing at the inspectable details for the exact
// commands. The product names live only in the Details (InstallerCommand).
func ConsentMessage(absent []Backend) string {
	noun := "the tool it uses"
	if len(absent) > 1 {
		noun = "the tools it uses"
	}
	return "Endstate needs to set up " + noun + " to install and configure your software. " +
		"You may be asked for your administrator password, and a background helper and a dedicated " +
		"storage area may be created. See the details for the exact commands that will run."
}

// InstallerCommand returns the exact, inspectable command the engine would run to
// install the given backend via its OFFICIAL upstream installer. It is surfaced
// in the consent-request event's details so a user who looks can see precisely
// what the privileged step does. The engine orchestrates these — it never
// vendors, forks, or re-implements them.
func InstallerCommand(b Backend) string {
	switch b {
	case BackendBrew:
		// The official Homebrew installer (https://brew.sh).
		return `/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`
	case BackendNix:
		// The official Determinate Nix installer, multi-user (matches CI's
		// DeterminateSystems/nix-installer-action). --no-confirm because the
		// engine owns the single up-front consent.
		return `curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install --no-confirm`
	default:
		return ""
	}
}

// --- Real seams (never exercised under `go test`; validated on a real machine) ---

// realDetect reports whether a backend's binary is present and runnable. It
// checks PATH first, then the backend's well-known default install locations
// (so a backend installed this session but not yet on the shell PATH still reads
// as present).
func realDetect(b Backend) (bool, error) {
	bin := string(b) // "brew" / "nix"
	if _, err := exec.LookPath(bin); err == nil {
		return true, nil
	}
	for _, p := range knownBinPaths(b) {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return true, nil
		}
	}
	return false, nil
}

// knownBinPaths returns the well-known absolute binary locations for a backend,
// covering the case where the backend is installed but not yet on PATH.
func knownBinPaths(b Backend) []string {
	switch b {
	case BackendBrew:
		return []string{
			"/opt/homebrew/bin/brew",              // Apple Silicon macOS
			"/usr/local/bin/brew",                 // Intel macOS
			"/home/linuxbrew/.linuxbrew/bin/brew", // Linuxbrew
		}
	case BackendNix:
		return []string{"/nix/var/nix/profiles/default/bin/nix"} // Determinate default
	default:
		return nil
	}
}

// realInstall shells the backend's official installer, wiring the process stdio
// through so the OS credential / Xcode-CLT prompts the installer forces are NOT
// suppressed (the user answers them directly). It is the only privileged step
// and is never called in tests.
func realInstall(b Backend) error {
	var cmd *exec.Cmd
	switch b {
	case BackendBrew:
		cmd = exec.Command("/bin/bash", "-c",
			`/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`)
	case BackendNix:
		cmd = exec.Command("/bin/sh", "-c",
			`curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install --no-confirm`)
	default:
		return os.ErrInvalid
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stderr // installer chatter is diagnostics, not the JSON envelope on stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// realVerify confirms a freshly-installed backend actually works by running its
// version command. "Installer exited 0" is not success — the probe is.
func realVerify(b Backend) (bool, error) {
	bin := resolveBin(b)
	if bin == "" {
		return false, nil
	}
	if err := exec.Command(bin, "--version").Run(); err != nil {
		return false, err
	}
	return true, nil
}

// resolveBin returns a runnable path for a backend's binary: PATH first, then the
// known default locations (a just-installed backend may not be on PATH yet).
func resolveBin(b Backend) string {
	bin := string(b)
	if p, err := exec.LookPath(bin); err == nil {
		return p
	}
	for _, p := range knownBinPaths(b) {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}
