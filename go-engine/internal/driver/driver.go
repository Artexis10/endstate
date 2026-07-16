// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package driver defines the abstract Driver interface used by all package
// manager adapters (e.g. winget) in the Endstate engine.
package driver

// Status values for InstallResult. These match the item-event status values
// defined in the Endstate event contract v1.
const (
	// StatusInstalled means the package was freshly installed this run.
	StatusInstalled = "installed"
	// StatusPresent means the package was already present; nothing was done.
	StatusPresent = "present"
	// StatusSkipped means the package was intentionally skipped (filtered, etc.).
	StatusSkipped = "skipped"
	// StatusFailed means the installation attempt failed.
	StatusFailed = "failed"
)

// Reason values for InstallResult. These match the reason codes defined in the
// Endstate event contract v1.
const (
	// ReasonAlreadyInstalled means the package was detected before install was attempted.
	ReasonAlreadyInstalled = "already_installed"
	// ReasonInstallFailed means winget returned a non-zero, non-already-installed exit code.
	ReasonInstallFailed = "install_failed"
	// ReasonUserDenied means heuristic output analysis detected a cancellation.
	// NOTE: per event-contract.md this detection is unreliable — winget provides
	// no standardised exit code for user cancellation.
	ReasonUserDenied = "user_denied"
	// ReasonMissing means the package was not detected during a verify check.
	ReasonMissing = "missing"
	// ReasonFiltered means the package was excluded by a filter/policy.
	ReasonFiltered = "filtered"
	// ReasonVersionDrift means the package is installed but at a version that
	// differs from the one declared by the manifest (a verify failure distinct
	// from "missing"). Only evaluated for apps that declare a version.
	ReasonVersionDrift = "version_drift"
	// ReasonConfigDrift means the active home-manager generation differs from the
	// most-recently recorded one: the configuration has drifted from the declared
	// state. Only evaluated on the realizer path when the manifest declares a
	// home-manager input.
	ReasonConfigDrift = "config_drift"
)

// InstallResult is returned by Driver.Install and carries the outcome of a
// single package installation attempt.
type InstallResult struct {
	// Status is one of the Status* constants above.
	Status string
	// Reason is one of the Reason* constants above, or empty when not applicable.
	Reason string
	// Message is a human-readable description of what happened.
	Message string
	// RebootRequired is true when the package manager completed successfully but
	// reported that Windows must reboot before the change is fully effective.
	RebootRequired bool
}

// Driver is the interface that all package-manager adapters must implement.
// Each driver wraps one package manager (e.g. winget) and exposes detection
// and installation capabilities in a uniform way.
type Driver interface {
	// Name returns the stable driver identifier (e.g. "winget").
	Name() string
	// Detect reports whether the package identified by ref is currently
	// installed on the system.  The second return value is the human-readable
	// display name extracted from the package manager output (empty string
	// when unavailable or package is not installed).  It returns a non-nil
	// error only for infrastructure failures (e.g. the tool binary is
	// missing); a package that is simply not installed returns (false, "", nil).
	Detect(ref string) (bool, string, error)
	// Install attempts to install the package identified by ref.  It never
	// returns a non-nil error for an expected failure (e.g. package not found);
	// those cases are encoded in InstallResult.  A non-nil error signals an
	// infrastructure problem (e.g. the driver binary is unavailable).
	Install(ref string) (*InstallResult, error)
}

// Status values for UninstallResult.
const (
	// StatusUninstalled means the package was uninstalled this call.
	StatusUninstalled = "uninstalled"
	// StatusAbsent means the package was already not installed (a successful
	// no-op for rollback idempotency).
	StatusAbsent = "absent"
	// StatusFailed (defined above) is reused for an uninstall that failed.
)

// UninstallResult is returned by Uninstaller.Uninstall and carries the outcome
// of a single package uninstall attempt.
type UninstallResult struct {
	// Status is one of StatusUninstalled, StatusAbsent, or StatusFailed.
	Status string
	// Message is a human-readable description of what happened.
	Message string
}

// Uninstaller is an optional interface that drivers can implement to remove a
// package. Callers type-assert their Driver to Uninstaller, exactly like
// BatchDetector; a driver that does not implement it has no uninstall path (and
// therefore no best-effort rollback). It is deliberately kept off the core
// Driver interface so install and uninstall stay distinct capabilities.
type Uninstaller interface {
	// Uninstall removes the package identified by ref. It returns a non-nil
	// error only for an infrastructure failure (e.g. the tool binary is
	// missing); an already-absent package is reported as StatusAbsent, not an
	// error, and a refused/failed uninstall is reported as StatusFailed.
	Uninstall(ref string) (*UninstallResult, error)
}

// VersionedInstaller is an optional interface that drivers can implement to
// install a specific version of a package (version pinning). Callers
// type-assert their Driver to VersionedInstaller, exactly like BatchDetector /
// Uninstaller; a driver that does not implement it has no pinning path and the
// caller falls back to Install (latest). It is kept off the core Driver
// interface so pinning stays an opt-in capability that not every backend needs.
type VersionedInstaller interface {
	// InstallVersion installs the exact given version of the package identified
	// by ref. It follows Install's contract: a non-nil error signals an
	// infrastructure problem (e.g. the tool binary is missing); an expected
	// failure — including the requested version being unavailable — is encoded
	// in InstallResult as StatusFailed/ReasonInstallFailed.
	InstallVersion(ref, version string) (*InstallResult, error)
	// ReinstallVersion force-reinstalls the exact given version over an already-
	// installed (drifted) one — the convergence step of `apply --repin`. It is
	// InstallVersion plus a force flag, so a package installed at a different
	// version is changed to the declared version rather than reported as already
	// installed. Same failure contract as InstallVersion.
	ReinstallVersion(ref, version string) (*InstallResult, error)
}

// DetectResult holds the outcome of a batch detection check for a single ref.
type DetectResult struct {
	// Installed is true if the package is currently installed.
	Installed bool
	// DisplayName is the human-readable name from the package manager output.
	DisplayName string
	// Version is the installed version reported by the package manager, or ""
	// when the manager exposes none (best-effort capture).
	Version string
}

// BatchDetector is an optional interface that drivers can implement to detect
// multiple packages in a single operation. Callers should type-assert their
// Driver to BatchDetector; if unsupported, fall back to per-ref Detect calls.
type BatchDetector interface {
	// DetectBatch checks multiple refs at once. Returns a map of ref →
	// DetectResult. A ref absent from the map means not installed.
	DetectBatch(refs []string) (map[string]DetectResult, error)
}

// InstalledPackage is the package-manager-neutral record returned by installed
// package enumeration. Ref is the stable manager-specific package identifier;
// DisplayName and Version are best-effort evidence exposed by that manager.
type InstalledPackage struct {
	Ref         string
	DisplayName string
	Version     string
}

// InstalledEnumerator is an optional driver capability for capture. It reports
// the manager's installed-package ledger without applying cross-manager
// deduplication or changing the manager's configured sources.
type InstalledEnumerator interface {
	EnumerateInstalled() ([]InstalledPackage, error)
}
