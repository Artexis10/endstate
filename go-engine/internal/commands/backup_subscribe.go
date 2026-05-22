// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"context"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// SubscribeResult is the data payload for `backup subscribe`.
//
//	{
//	  "checkoutUrl":   string,
//	  "transactionId": string
//	}
//
// The GUI opens checkoutUrl in the system browser (substrate renders the
// Paddle _ptxn overlay there). The engine never opens a browser itself.
type SubscribeResult struct {
	CheckoutURL   string `json:"checkoutUrl"`
	TransactionID string `json:"transactionId"`
}

// runBackupSubscribe mints a Paddle checkout transaction via substrate and
// returns the checkout URL for the GUI to open. Requires a signed-in
// session; when signed out it returns AUTH_REQUIRED without a network call,
// mirroring runBackupStatus's gating.
func runBackupSubscribe(flags BackupFlags) (interface{}, *envelope.Error) {
	a := newBackupStack().Auth
	if !a.Session().SignedIn() {
		return nil, envelope.NewError(envelope.ErrAuthRequired,
			"backup subscribe requires a signed-in session").
			WithRemediation("Run `endstate backup login` first, then retry.")
	}
	resp, err := a.Subscribe(context.Background())
	if err != nil {
		return nil, err
	}
	return &SubscribeResult{
		CheckoutURL:   resp.CheckoutURL,
		TransactionID: resp.TransactionID,
	}, nil
}
