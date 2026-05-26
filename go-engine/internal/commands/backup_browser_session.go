// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// BrowserSessionResult is the data payload for `backup browser-session`.
//
//	{
//	  "sessionToken": string,  // 60s EdDSA JWT, aud=endstate-account
//	  "accountUrl":   string   // <issuer>/account/start (or self-host override)
//	}
//
// The GUI composes `${accountUrl}?session=${sessionToken}` and opens it in
// the system browser. Substrate's /account/start route swaps the JWT for
// an HttpOnly cookie and redirects to the cookie-only /account page.
// See hosted-backup-contract.md §5 and the Endstate Account Portal
// Architecture decision.
type BrowserSessionResult struct {
	SessionToken string `json:"sessionToken"`
	AccountURL   string `json:"accountUrl"`
}

// runBackupBrowserSession mints a short-lived account-portal handoff token
// via substrate. Requires a signed-in session; when signed out it returns
// AUTH_REQUIRED without a network call, mirroring runBackupSubscribe and
// runBackupStatus's gating.
func runBackupBrowserSession(flags BackupFlags) (interface{}, *envelope.Error) {
	a := newBackupStack().Auth
	if !a.Session().SignedIn() {
		return nil, envelope.NewError(envelope.ErrAuthRequired,
			"backup browser-session requires a signed-in session").
			WithRemediation("Run `endstate backup login` first, then retry.")
	}
	resp, err := a.BrowserSession(context.Background())
	if err != nil {
		return nil, err
	}
	return &BrowserSessionResult{
		SessionToken: resp.SessionToken,
		AccountURL:   resp.AccountURL,
	}, nil
}
