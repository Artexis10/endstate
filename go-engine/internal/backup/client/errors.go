// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// APIError is returned by the client when the backend responds with a
// non-2xx status. Code is the mapped envelope error code; HTTPStatus is
// the original HTTP status; Backend* fields are the parsed substrate
// error envelope (when present).
type APIError struct {
	Code           envelope.ErrorCode
	HTTPStatus     int
	BackendCode    string
	BackendMessage string
	Detail         interface{}
	Remediation    string
	DocsKey        string
	RetryAfter     string
}

func (e *APIError) Error() string {
	if e.BackendCode != "" {
		return fmt.Sprintf("backend %d %s: %s", e.HTTPStatus, e.BackendCode, e.BackendMessage)
	}
	return fmt.Sprintf("backend %d: %s", e.HTTPStatus, e.BackendMessage)
}

// AsEnvelopeError lifts the APIError into an envelope.Error suitable for
// returning from a command handler. Remediation and docsKey are filled in
// with sensible defaults when the backend did not supply them.
func (e *APIError) AsEnvelopeError() *envelope.Error {
	out := envelope.NewError(e.Code, fallbackMessage(e))
	if e.Detail != nil {
		out.WithDetail(e.Detail)
	}
	if e.Remediation != "" {
		out.WithRemediation(e.Remediation)
	} else if rem := defaultRemediation(e.Code); rem != "" {
		out.WithRemediation(rem)
	}
	if e.DocsKey != "" {
		out.WithDocsKey(e.DocsKey)
	} else if dk := defaultDocsKey(e.Code); dk != "" {
		out.WithDocsKey(dk)
	}
	return out
}

func fallbackMessage(e *APIError) string {
	if e.BackendMessage != "" {
		return e.BackendMessage
	}
	if e.BackendCode != "" {
		return e.BackendCode
	}
	switch e.Code {
	case envelope.ErrAuthRequired:
		return "Authentication required."
	case envelope.ErrSubscriptionRequired:
		return "Active subscription required."
	case envelope.ErrNotFound:
		return "Resource not found."
	case envelope.ErrRateLimited:
		return "Too many requests. Try again shortly."
	case envelope.ErrBackendError:
		return fmt.Sprintf("Backend returned %d.", e.HTTPStatus)
	case envelope.ErrBackendUnreachable:
		return "Could not reach the Endstate backup service."
	case envelope.ErrBackendIncompatible:
		return "The backup backend is not compatible with this engine version."
	case envelope.ErrSchemaIncompatible:
		return "Backend schema version is incompatible with this engine."
	case envelope.ErrStorageQuotaExceeded:
		return "Backup storage quota exceeded."
	}
	return fmt.Sprintf("Request failed (status %d).", e.HTTPStatus)
}

func defaultRemediation(c envelope.ErrorCode) string {
	switch c {
	case envelope.ErrAuthRequired:
		return "Run `endstate backup login` and retry."
	case envelope.ErrSubscriptionRequired:
		return "Subscribe to Endstate Hosted Backup to upload backups; restore remains available during grace and cancelled states."
	case envelope.ErrRateLimited:
		return "Wait a few seconds and retry."
	case envelope.ErrBackendUnreachable:
		return "Check your network connection or override ENDSTATE_OIDC_ISSUER_URL if pointing at a self-host backend."
	case envelope.ErrBackendIncompatible:
		return "Update the engine, or point at a backend that advertises the required `endstate_extensions` block."
	case envelope.ErrSchemaIncompatible:
		return "Update the engine to a version compatible with the backend's schema major."
	case envelope.ErrStorageQuotaExceeded:
		return "Delete old backup versions or remove unused backups, then retry."
	}
	return ""
}

func defaultDocsKey(c envelope.ErrorCode) string {
	return "errors/" + strings.ToLower(strings.ReplaceAll(string(c), "_", "-"))
}

// IsAuthRequired reports whether err is an APIError mapped to AUTH_REQUIRED.
// Helper used by the auth retry-after-refresh path.
func IsAuthRequired(err error) bool {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code == envelope.ErrAuthRequired
	}
	return false
}

// IsRetryable reports whether err is a transport error or a 5xx APIError
// that the retry loop is allowed to attempt again. 4xx (except 429) are
// never retried per the locked policy.
func IsRetryable(err error) bool {
	if err == nil {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) {
		if ae.Code == envelope.ErrRateLimited {
			return true
		}
		return ae.HTTPStatus >= 500 && ae.HTTPStatus < 600
	}
	// Any non-API error (network, timeout, DNS) is retryable up to the cap.
	return true
}
