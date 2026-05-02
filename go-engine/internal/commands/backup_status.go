// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// StatusResult is the data payload for `backup status`.
//
// Field shape locked in the plan §"Envelope shapes (Question 6)":
//
//   {
//     "signedIn":            bool,
//     "email":               string?,
//     "userId":              string?,
//     "subscriptionStatus":  string?,
//     "issuerUrl":           string,
//     "lastBackupAt":        string?
//   }
//
// Optional fields are present only when the user is signed in. issuerUrl
// is always present so the GUI can show "you're configured to talk to <X>"
// even when signed out.
type StatusResult struct {
	SignedIn           bool   `json:"signedIn"`
	Email              string `json:"email,omitempty"`
	UserID             string `json:"userId,omitempty"`
	SubscriptionStatus string `json:"subscriptionStatus,omitempty"`
	IssuerURL          string `json:"issuerUrl"`
	LastBackupAt       string `json:"lastBackupAt,omitempty"`
}

func runBackupStatus(flags BackupFlags) (interface{}, *envelope.Error) {
	a := newBackupStack().Auth
	res := &StatusResult{
		IssuerURL: a.Issuer().URL,
	}

	// If we have nothing in the keychain to talk to, return signed-out
	// without making any network calls.
	if !a.Session().SignedIn() {
		return res, nil
	}

	// Hit /api/account/me to confirm the session is live and read the
	// authoritative subscription status. Any error here propagates to the
	// caller — the user wants to know if the connection is broken.
	me, err := a.Me(context.Background())
	if err != nil {
		return nil, err
	}
	res.SignedIn = true
	res.Email = me.Email
	res.UserID = me.UserID
	res.SubscriptionStatus = me.SubscriptionStatus
	return res, nil
}
