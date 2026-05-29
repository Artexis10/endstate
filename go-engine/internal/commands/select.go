// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/driver/winget"
)

// ErrNoBackend indicates that no package backend is implemented for the host
// operating system. Non-Windows hosts return this until a platform backend
// (e.g. a Nix realizer) is added.
var ErrNoBackend = errors.New("no package backend available for this platform")

// selectBackend returns the package-manager driver for the given OS. Windows
// uses the winget driver; other platforms have no backend yet and return
// ErrNoBackend so callers fail explicitly rather than attempting installs.
func selectBackend(goos string) (driver.Driver, error) {
	switch goos {
	case "windows":
		return winget.New(), nil
	default:
		return nil, ErrNoBackend
	}
}
