// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package client provides a thin HTTP wrapper around the substrate Hosted
// Backup API. Responsibilities:
//
//   - bearer-token injection via a TokenProvider
//   - JSON request/response marshaling
//   - X-Endstate-API-Version checking on every response
//   - status → envelope.ErrorCode mapping
//   - retry with exponential backoff and jitter on 5xx + transport errors
//   - one-shot 401 → refresh-then-retry hook
//
// Contract: docs/contracts/hosted-backup-contract.md §7, §11.
//
// The JWT itself is parsed and verified by the auth package. This package
// only carries the bearer string; it does not interpret it.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// TokenProvider supplies access tokens to the client and refreshes them
// when the backend rejects the current one with 401. Implemented by the
// auth package.
type TokenProvider interface {
	// AccessToken returns a current bearer token. May call refresh
	// internally if the cached token is known-expired.
	AccessToken(ctx context.Context) (string, error)
	// RefreshAccessToken forces a refresh and returns the new token. The
	// client invokes this after a single 401 before giving up.
	RefreshAccessToken(ctx context.Context) (string, error)
}

// Anonymous is a TokenProvider that supplies no token. Used for the auth
// endpoints (signup / login pre-handshake) that are unauthenticated.
type Anonymous struct{}

// AccessToken returns an empty string with no error.
func (Anonymous) AccessToken(context.Context) (string, error)        { return "", nil }
// RefreshAccessToken returns an empty string with no error.
func (Anonymous) RefreshAccessToken(context.Context) (string, error) { return "", nil }

// Client is safe for concurrent use.
type Client struct {
	http   *http.Client
	tokens TokenProvider
	retry  RetryPolicy
	rng    *rand.Rand
	logger *slog.Logger
	sleep  func(time.Duration) // overridable for tests
}

// Options configures a new Client.
type Options struct {
	HTTPClient *http.Client    // optional; default 60s timeout
	Tokens     TokenProvider   // required; use Anonymous{} for unauth flows
	Retry      *RetryPolicy    // optional; defaults to DefaultRetryPolicy
	Logger     *slog.Logger    // optional; defaults to slog.Default
	Now        func() time.Time
}

// New constructs a Client.
func New(opts Options) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	rp := DefaultRetryPolicy()
	if opts.Retry != nil {
		rp = *opts.Retry
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		http:   hc,
		tokens: opts.Tokens,
		retry:  rp,
		rng:    rand.New(rand.NewSource(time.Now().UnixNano())),
		logger: logger,
		sleep:  time.Sleep,
	}
}

// Request carries the per-call options for Do.
type Request struct {
	Method   string      // GET, POST, DELETE, PUT
	URL      string      // absolute URL
	Body     interface{} // optional JSON-marshaled body
	ReadOnly bool        // version-mismatch tolerance for minor bumps
	Headers  http.Header // additional headers (caller-supplied)

	// SkipAuthRefresh disables the one-shot 401 → refresh-then-retry hop
	// for this call. Required for the refresh endpoint itself: substrate
	// authenticates `/api/auth/refresh` via the body refresh token, not
	// the bearer header. If a self-host implementation rejected the
	// stale bearer with 401, the default refresh hook would recurse back
	// into RefreshAccessToken indefinitely.
	SkipAuthRefresh bool
}

// Do executes the request with retry + version check + auth refresh.
// On 2xx the response body is decoded into out (when non-nil) as JSON
// and nil is returned. On any other outcome an envelope.Error is
// returned with the appropriate ErrorCode set; command handlers can
// return it verbatim.
func (c *Client) Do(ctx context.Context, req Request, out interface{}) *envelope.Error {
	var lastAPIErr *APIError
	var lastTransportErr error
	refreshed := false

	for attempt := 0; attempt <= c.retry.MaxRetries; attempt++ {
		body, err := encodeBody(req.Body)
		if err != nil {
			return envelope.NewError(envelope.ErrInternalError, err.Error())
		}
		httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
		if err != nil {
			return envelope.NewError(envelope.ErrInternalError, "client: build request: "+err.Error())
		}
		if req.Body != nil {
			httpReq.Header.Set("Content-Type", "application/json")
		}
		httpReq.Header.Set("Accept", "application/json")
		for k, vs := range req.Headers {
			for _, v := range vs {
				httpReq.Header.Add(k, v)
			}
		}

		token, err := c.tokens.AccessToken(ctx)
		if err != nil {
			return envelope.NewError(envelope.ErrAuthRequired, "client: get access token: "+err.Error()).
				WithRemediation(defaultRemediation(envelope.ErrAuthRequired))
		}
		if token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+token)
		}

		resp, err := c.http.Do(httpReq)
		if err != nil {
			lastTransportErr = err
			if attempt < c.retry.MaxRetries {
				wait := c.retry.nextWait(attempt, "", c.rng)
				c.logger.Warn("backup: transport error, retrying",
					"err", err.Error(), "attempt", attempt+1, "wait", wait)
				if !c.waitOrCancel(ctx, wait) {
					return envelope.NewError(envelope.ErrInternalError, "client: context cancelled")
				}
				continue
			}
			return mapTransportError(err)
		}

		apiErr := c.processResponse(resp, req.ReadOnly, out)
		if apiErr == nil {
			return nil
		}
		lastAPIErr = apiErr

		// 401 → one-shot refresh, then retry without consuming a regular retry slot.
		// Suppressed via SkipAuthRefresh on the refresh endpoint itself so we
		// don't recurse back into RefreshAccessToken if the backend rejects
		// the stale bearer.
		if apiErr.Code == envelope.ErrAuthRequired && !refreshed && !req.SkipAuthRefresh {
			if _, rerr := c.tokens.RefreshAccessToken(ctx); rerr == nil {
				refreshed = true
				attempt-- // don't burn a retry on the refresh-then-retry hop
				continue
			}
			return apiErr.AsEnvelopeError()
		}

		if !IsRetryable(apiErr) || attempt >= c.retry.MaxRetries {
			return apiErr.AsEnvelopeError()
		}

		wait := c.retry.nextWait(attempt, apiErr.RetryAfter, c.rng)
		c.logger.Warn("backup: retryable response, retrying",
			"status", apiErr.HTTPStatus, "code", string(apiErr.Code),
			"attempt", attempt+1, "wait", wait)
		if !c.waitOrCancel(ctx, wait) {
			return envelope.NewError(envelope.ErrInternalError, "client: context cancelled")
		}
	}

	if lastAPIErr != nil {
		return lastAPIErr.AsEnvelopeError()
	}
	if lastTransportErr != nil {
		return mapTransportError(lastTransportErr)
	}
	return envelope.NewError(envelope.ErrInternalError, "client: retries exhausted")
}

// processResponse handles version + status mapping for a single response.
// The returned *APIError is nil on full success.
func (c *Client) processResponse(resp *http.Response, readOnly bool, out interface{}) *APIError {
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 5<<20))

	if v := resp.Header.Get(versionHeader); v != "" {
		if pv, err := parseVersionHeader(v); err == nil {
			if mismatch, warn := versionMismatch(pv, readOnly); mismatch != nil {
				mismatch.HTTPStatus = resp.StatusCode
				return mismatch
			} else if warn {
				c.logger.Warn("backup: backend minor version newer than engine",
					"backendVersion", v, "engineMajor", EngineSchemaMajor,
					"engineMinor", EngineSchemaMinor)
			}
		} else {
			c.logger.Warn("backup: invalid X-Endstate-API-Version header", "value", v, "err", err.Error())
		}
	}

	if resp.StatusCode/100 == 2 {
		if out != nil && len(body) > 0 {
			if err := json.Unmarshal(body, out); err != nil {
				return &APIError{
					Code:           envelope.ErrInternalError,
					HTTPStatus:     resp.StatusCode,
					BackendMessage: fmt.Sprintf("decode response body: %v", err),
				}
			}
		}
		return nil
	}

	return parseAPIError(resp, body)
}

// parseAPIError maps a non-2xx response to an APIError. It tries to
// decode the standard error envelope in the body to surface backend
// remediation/docsKey verbatim; on parse failure it falls back to a
// status-only error.
func parseAPIError(resp *http.Response, body []byte) *APIError {
	ae := &APIError{
		HTTPStatus: resp.StatusCode,
		RetryAfter: resp.Header.Get("Retry-After"),
	}

	// Map the status code first; backend body can refine the code (e.g.
	// 409 → STORAGE_QUOTA_EXCEEDED) by setting a known code value.
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		ae.Code = envelope.ErrAuthRequired
	case resp.StatusCode == http.StatusPaymentRequired:
		ae.Code = envelope.ErrSubscriptionRequired
	case resp.StatusCode == http.StatusNotFound:
		ae.Code = envelope.ErrNotFound
	case resp.StatusCode == http.StatusTooManyRequests:
		ae.Code = envelope.ErrRateLimited
	case resp.StatusCode == http.StatusConflict:
		ae.Code = envelope.ErrBackendError
	case resp.StatusCode/100 == 5:
		ae.Code = envelope.ErrBackendError
	case resp.StatusCode/100 == 4:
		ae.Code = envelope.ErrBackendError
	default:
		ae.Code = envelope.ErrBackendError
	}

	if len(body) > 0 {
		var env struct {
			Success bool `json:"success"`
			Error   *struct {
				Code        string      `json:"code"`
				Message     string      `json:"message"`
				Detail      interface{} `json:"detail"`
				Remediation string      `json:"remediation"`
				DocsKey     string      `json:"docsKey"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &env); err == nil && env.Error != nil {
			ae.BackendCode = env.Error.Code
			ae.BackendMessage = env.Error.Message
			ae.Detail = env.Error.Detail
			ae.Remediation = env.Error.Remediation
			ae.DocsKey = env.Error.DocsKey
			// Substrate may surface STORAGE_QUOTA_EXCEEDED on a 409 (or 403).
			// Honour their code when it's one we recognise.
			if up := strings.ToUpper(env.Error.Code); up == "STORAGE_QUOTA_EXCEEDED" {
				ae.Code = envelope.ErrStorageQuotaExceeded
			}
		}
	}
	return ae
}

func encodeBody(b interface{}) (io.Reader, error) {
	if b == nil {
		return nil, nil
	}
	if r, ok := b.(io.Reader); ok {
		return r, nil
	}
	buf, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("client: encode body: %w", err)
	}
	return bytes.NewReader(buf), nil
}

func (c *Client) waitOrCancel(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}

func mapTransportError(err error) *envelope.Error {
	return envelope.NewError(envelope.ErrBackendUnreachable,
		"Could not reach the Endstate backup service: "+err.Error()).
		WithRemediation(defaultRemediation(envelope.ErrBackendUnreachable))
}
