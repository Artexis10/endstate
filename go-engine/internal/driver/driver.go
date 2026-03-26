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

// DetectResult holds the outcome of a batch detection check for a single ref.
type DetectResult struct {
	// Installed is true if the package is currently installed.
	Installed bool
	// DisplayName is the human-readable name from the package manager output.
	DisplayName string
}

// BatchDetector is an optional interface that drivers can implement to detect
// multiple packages in a single operation. Callers should type-assert their
// Driver to BatchDetector; if unsupported, fall back to per-ref Detect calls.
type BatchDetector interface {
	// DetectBatch checks multiple refs at once. Returns a map of ref →
	// DetectResult. A ref absent from the map means not installed.
	DetectBatch(refs []string) (map[string]DetectResult, error)
}
