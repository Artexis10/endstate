// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package backup

import (
	"context"
	"errors"
	"net/http"
	"os"
	"time"
)

const defaultTransferTimeout = 10 * time.Minute

// TransferTimeout returns the per-request deadline for presigned object
// transfers. An explicit dependency value wins; the environment override is
// useful for constrained networks without making a stalled transfer unbounded.
func TransferTimeout(override time.Duration) time.Duration {
	if override > 0 {
		return override
	}
	if value, err := time.ParseDuration(os.Getenv("ENDSTATE_BACKUP_TRANSFER_TIMEOUT")); err == nil && value > 0 {
		return value
	}
	return defaultTransferTimeout
}

// WithTransferTimeout scopes one presigned request, rather than the whole
// upload/download operation, so each retry receives a full transfer window.
func WithTransferTimeout(ctx context.Context, override time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, TransferTimeout(override))
}

// TransportError deliberately hides the underlying net/url error string: Go's
// default transport error includes the full presigned URL and its query secret.
// The cause remains unwrap-able for retry and error-classification logic.
type TransportError struct{ cause error }

func (e *TransportError) Error() string { return "presigned object transfer failed" }
func (e *TransportError) Unwrap() error { return e.cause }

func RedactTransportError(err error) error {
	if err == nil {
		return nil
	}
	var transport *TransportError
	if errors.As(err, &transport) {
		return err
	}
	return &TransportError{cause: err}
}

func IsTransportError(err error) bool {
	var transport *TransportError
	return errors.As(err, &transport)
}

// PresignedClient returns a shallow client copy that never follows redirects.
// A redirect can replay ciphertext to an unintended endpoint or turn a PUT
// into a successful landing-page GET, neither of which proves object storage.
// API and OIDC clients deliberately do not use this helper.
func PresignedClient(hc *http.Client) *http.Client {
	if hc == nil {
		hc = http.DefaultClient
	}
	copy := *hc
	copy.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("presigned object redirect refused")
	}
	return &copy
}
