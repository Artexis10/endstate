// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
)

// Native desktop support remains unavailable until the separate runtime and
// real-session gates pass. Tests inject a hermetic backend through this seam.
var desktopSettingsBackend desktopsettings.Mutator

func desktopSettingsContext(ctx context.Context) context.Context {
	return desktopsettings.WithBackend(ctx, desktopSettingsBackend)
}
