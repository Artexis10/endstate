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
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Backend identifies a package backend the engine can bootstrap.
type Backend string

const (
	// BackendBrew is the Homebrew driver (darwin `driver: "brew"` lane).
	BackendBrew Backend = "brew"
	// BackendNix is the Nix realizer (the cross-OS default lane).
	BackendNix Backend = "nix"
	// BackendChocolatey is the optional Chocolatey driver on Windows. Winget is
	// operating-system provided and is never bootstrapped.
	BackendChocolatey Backend = "chocolatey"
)

const chocolateyInstallScript = `Set-ExecutionPolicy Bypass -Scope Process -Force; [System.Net.ServicePointManager]::SecurityProtocol = [System.Net.ServicePointManager]::SecurityProtocol -bor 3072; iwr https://community.chocolatey.org/install.ps1 -UseBasicParsing | iex`

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
	// InstallerStdin remains attached to the host terminal so official installer
	// prompts still work. The output writers may be replaced by the command layer
	// when stderr is reserved for structured JSONL events.
	InstallerStdin  io.Reader
	InstallerStdout io.Writer
	InstallerStderr io.Writer
	provisionErrors map[Backend]error
}

// New returns a Bootstrapper wired to the real detect/install/verify seams.
func New() *Bootstrapper {
	bs := &Bootstrapper{
		Detect:          realDetect,
		Verify:          realVerify,
		InstallerStdin:  os.Stdin,
		InstallerStdout: os.Stderr,
		InstallerStderr: os.Stderr,
	}
	bs.Install = func(b Backend) error {
		return realInstallWithIO(b, bs.InstallerStdin, bs.InstallerStdout, bs.InstallerStderr)
	}
	return bs
}

// ConfigureInstallerIO changes only the official installer process streams.
// Detection and verification remain quiet probes. Callers keep stdin attached
// while redirecting output when their stderr has a machine-readable contract.
func (bs *Bootstrapper) ConfigureInstallerIO(stdin io.Reader, stdout, stderr io.Writer) {
	bs.InstallerStdin = stdin
	bs.InstallerStdout = stdout
	bs.InstallerStderr = stderr
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
	if bs.provisionErrors == nil {
		bs.provisionErrors = make(map[Backend]error)
	}
	for _, b := range absent {
		delete(bs.provisionErrors, b)
		if err := bs.Install(b); err != nil {
			out[b] = OutcomeInstallFailed
			bs.provisionErrors[b] = err
			continue
		}
		ok, err := bs.Verify(b)
		if err != nil || !ok {
			out[b] = OutcomeVerifyFailed
			if err != nil {
				bs.provisionErrors[b] = err
			} else {
				bs.provisionErrors[b] = errors.New("backend verification did not report available")
			}
			continue
		}
		out[b] = OutcomeInstalled
	}
	return out
}

// ProvisionError returns the install or verification error retained for the
// most recent Provision attempt of b. It lets command layers surface bounded,
// structured diagnostics without changing the stable Outcome classification.
func (bs *Bootstrapper) ProvisionError(b Backend) error {
	return bs.provisionErrors[b]
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
	case BackendChocolatey:
		// The official Chocolatey PowerShell bootstrap command
		// (https://docs.chocolatey.org/en-us/choco/setup/).
		return `powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "` + chocolateyInstallScript + `"`
	default:
		return ""
	}
}

// --- Real seams (never exercised under `go test`; validated on a real machine) ---

// realDetect reports whether a backend's binary is present and runnable. The
// version probe prevents a corrupt or non-executable file at a known path from
// being treated as a working backend.
func realDetect(b Backend) (bool, error) {
	return realVerify(b)
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
	case BackendChocolatey:
		if programData := os.Getenv("ProgramData"); programData != "" {
			return []string{filepath.Join(programData, "chocolatey", "bin", "choco.exe")}
		}
		return nil
	default:
		return nil
	}
}

// realInstall shells the backend's official installer, wiring the process stdio
// through so the OS credential / Xcode-CLT prompts the installer forces are NOT
// suppressed (the user answers them directly). It is the only privileged step
// and is never called in tests.
var runInstallerCommandFn = func(cmd *exec.Cmd) error { return cmd.Run() }

func realInstallWithIO(b Backend, stdin io.Reader, stdout, stderr io.Writer) error {
	var cmd *exec.Cmd
	switch b {
	case BackendBrew:
		cmd = exec.Command("/bin/bash", "-c",
			`/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"`)
	case BackendNix:
		cmd = exec.Command("/bin/sh", "-c",
			`curl --proto '=https' --tlsv1.2 -sSf -L https://install.determinate.systems/nix | sh -s -- install --no-confirm`)
	case BackendChocolatey:
		cmd = exec.Command("powershell.exe", "-NoProfile", "-ExecutionPolicy", "Bypass", "-Command", chocolateyInstallScript)
	default:
		return os.ErrInvalid
	}
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return runInstallerCommandFn(cmd)
}

// realVerify confirms a freshly-installed backend actually works by running its
// version command. "Installer exited 0" is not success — the probe is.
var runVersionCommandFn = func(bin string) error {
	return exec.Command(bin, "--version").Run()
}

func realVerify(b Backend) (bool, error) {
	bin := resolveBin(b)
	if bin == "" {
		return false, nil
	}
	if err := runVersionCommandFn(bin); err != nil {
		return false, err
	}
	return true, nil
}

// resolveBin returns a runnable path for a backend's binary: PATH first, then the
// known default locations (a just-installed backend may not be on PATH yet).
func resolveBin(b Backend) string {
	bin := binaryName(b)
	if bin == "" {
		return ""
	}
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

func binaryName(b Backend) string {
	switch b {
	case BackendBrew, BackendNix:
		return string(b)
	case BackendChocolatey:
		return "choco.exe"
	default:
		return ""
	}
}
