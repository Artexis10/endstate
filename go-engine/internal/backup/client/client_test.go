// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package client_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// fastRetry returns a retry policy with near-zero waits so tests don't
// burn real wall time exercising 5xx/429 paths.
func fastRetry() client.RetryPolicy {
	return client.RetryPolicy{
		MaxRetries:  3,
		InitialWait: time.Millisecond,
		Multiplier:  1,
		JitterFrac:  0,
		MaxWait:     5 * time.Millisecond,
	}
}

// versionV1 is the header value all test responses include unless a test
// is specifically exercising the version-mismatch path.
func versionV1(h http.Header) {
	h.Set("X-Endstate-API-Version", "1.0")
}

// staticTokens is a TokenProvider that returns whatever the test sets.
type staticTokens struct {
	mu          sync.Mutex
	access      string
	refresh     string
	refreshErr  error
	refreshHits int32
}

func (s *staticTokens) AccessToken(context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.access, nil
}

func (s *staticTokens) RefreshAccessToken(context.Context) (string, error) {
	atomic.AddInt32(&s.refreshHits, 1)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.refreshErr != nil {
		return "", s.refreshErr
	}
	s.access = s.refresh
	return s.access, nil
}

func newClient(t *testing.T, tokens client.TokenProvider) *client.Client {
	t.Helper()
	rp := fastRetry()
	return client.New(client.Options{
		Tokens: tokens,
		Retry:  &rp,
	})
}

func TestDo_Success_DecodesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		versionV1(w.Header())
		_ = json.NewEncoder(w).Encode(map[string]string{"hello": "world"})
	}))
	defer srv.Close()

	c := newClient(t, client.Anonymous{})
	var out struct {
		Hello string `json:"hello"`
	}
	if err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if out.Hello != "world" {
		t.Errorf("decoded body: hello = %q, want %q", out.Hello, "world")
	}
}

func TestDo_BearerTokenInjected(t *testing.T) {
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		versionV1(w.Header())
		w.WriteHeader(204)
	}))
	defer srv.Close()

	c := newClient(t, &staticTokens{access: "tok-abc"})
	if err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, nil); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if seenAuth != "Bearer tok-abc" {
		t.Errorf("Authorization header = %q, want %q", seenAuth, "Bearer tok-abc")
	}
}

func TestDo_401_RefreshThenRetrySucceeds(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		versionV1(w.Header())
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// Second request must carry the refreshed token.
		if r.Header.Get("Authorization") != "Bearer fresh-token" {
			t.Errorf("second request authz = %q, want Bearer fresh-token", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "yes"})
	}))
	defer srv.Close()

	tokens := &staticTokens{access: "stale-token", refresh: "fresh-token"}
	c := newClient(t, tokens)
	var out struct {
		Ok string `json:"ok"`
	}
	if err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, &out); err != nil {
		t.Fatalf("Do after refresh: %v", err)
	}
	if atomic.LoadInt32(&tokens.refreshHits) != 1 {
		t.Errorf("RefreshAccessToken hits = %d, want 1", tokens.refreshHits)
	}
}

func TestDo_401_TwiceReturnsAuthRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		versionV1(w.Header())
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	tokens := &staticTokens{access: "stale", refresh: "still-stale"}
	c := newClient(t, tokens)
	err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrAuthRequired {
		t.Fatalf("expected ErrAuthRequired, got %+v", err)
	}
}

func TestDo_402_SubscriptionRequired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		versionV1(w.Header())
		w.WriteHeader(http.StatusPaymentRequired)
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	err := c.Do(context.Background(), client.Request{Method: "POST", URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrSubscriptionRequired {
		t.Errorf("got %+v, want ErrSubscriptionRequired", err)
	}
}

func TestDo_404_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		versionV1(w.Header())
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrNotFound {
		t.Errorf("got %+v, want ErrNotFound", err)
	}
}

func TestDo_429_RetriesUntilLimit(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		versionV1(w.Header())
		w.Header().Set("Retry-After", "0") // immediate retry permitted
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrRateLimited {
		t.Errorf("got %+v, want ErrRateLimited", err)
	}
	if got := atomic.LoadInt32(&hits); got != 4 { // initial + 3 retries
		t.Errorf("hits = %d, want 4 (initial + 3 retries)", got)
	}
}

func TestDo_5xxThenSuccessSucceeds(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&hits, 1)
		versionV1(w.Header())
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	var out struct {
		OK bool `json:"ok"`
	}
	if err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, &out); err != nil {
		t.Fatalf("Do: %v", err)
	}
	if !out.OK {
		t.Errorf("decoded body: ok = %v, want true", out.OK)
	}
}

func TestDo_5xxRetriesExhausted(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		versionV1(w.Header())
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrBackendError {
		t.Errorf("got %+v, want ErrBackendError", err)
	}
	if got := atomic.LoadInt32(&hits); got != 4 {
		t.Errorf("hits = %d, want 4", got)
	}
}

func TestDo_4xxNotRetried(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		versionV1(w.Header())
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	_ = c.Do(context.Background(), client.Request{Method: "POST", URL: srv.URL}, nil)
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("4xx retried %d times; should never retry (got hits=%d)", got-1, got)
	}
}

func TestDo_VersionMajorMismatch_AlwaysSchemaIncompatible(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})

	// On a read-only request:
	err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL, ReadOnly: true}, nil)
	if err == nil || err.Code != envelope.ErrSchemaIncompatible {
		t.Errorf("read-only major mismatch: got %+v, want SCHEMA_INCOMPATIBLE", err)
	}
	// And on a write:
	err = c.Do(context.Background(), client.Request{Method: "POST", URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrSchemaIncompatible {
		t.Errorf("write major mismatch: got %+v, want SCHEMA_INCOMPATIBLE", err)
	}
}

func TestDo_VersionMinorMismatch_ReadOnlyProceeds(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.5")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	if err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL, ReadOnly: true}, nil); err != nil {
		t.Errorf("read-only minor mismatch: expected nil error, got %+v", err)
	}
}

func TestDo_VersionMinorMismatch_WriteRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "1.5")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	err := c.Do(context.Background(), client.Request{Method: "POST", URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrSchemaIncompatible {
		t.Errorf("write minor mismatch: got %+v, want SCHEMA_INCOMPATIBLE", err)
	}
}

func TestDo_StorageQuotaExceededFromBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		versionV1(w.Header())
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"STORAGE_QUOTA_EXCEEDED","message":"Quota reached"}}`))
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	err := c.Do(context.Background(), client.Request{Method: "POST", URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrStorageQuotaExceeded {
		t.Errorf("got %+v, want STORAGE_QUOTA_EXCEEDED", err)
	}
}

func TestDo_TransportErrorMappedToBackendUnreachable(t *testing.T) {
	c := client.New(client.Options{
		Tokens:     client.Anonymous{},
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
		Retry:      ptrRetry(fastRetry()),
	})
	// Loopback port 1 is reserved & should be closed; immediate connect refusal.
	err := c.Do(context.Background(), client.Request{Method: "GET", URL: "http://127.0.0.1:1"}, nil)
	if err == nil || err.Code != envelope.ErrBackendUnreachable {
		t.Errorf("got %+v, want BACKEND_UNREACHABLE", err)
	}
}

func TestDo_BackendErrorEnvelopePassesRemediation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		versionV1(w.Header())
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"error":{"code":"FORBIDDEN","message":"nope","remediation":"Run a thing","docsKey":"errors/forbidden"}}`))
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Remediation != "Run a thing" {
		t.Errorf("remediation = %q, want %q", err.Remediation, "Run a thing")
	}
	if err.DocsKey != "errors/forbidden" {
		t.Errorf("docsKey = %q, want %q", err.DocsKey, "errors/forbidden")
	}
}

func ptrRetry(p client.RetryPolicy) *client.RetryPolicy { return &p }

// TestRetryAfterHonoured verifies the 429 Retry-After header is parsed and
// (loosely) honoured. We can't easily measure the wall time without
// flaking, so just ensure parsing doesn't panic and the header value is
// surfaced.
func TestRetryAfterHonoured(t *testing.T) {
	t.Run("seconds form", func(t *testing.T) {
		// Smoke-test parseRetryAfter via a 429 response with Retry-After=1 — should still
		// retry (we configure fastRetry; the cap means actual wait is &lt;=5ms).
		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&hits, 1)
			versionV1(w.Header())
			if n < 2 {
				w.Header().Set("Retry-After", strconv.Itoa(1))
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{}`))
		}))
		defer srv.Close()

		// Use a retry policy with MaxWait < 1s so Retry-After=1s doesn't burn real time.
		rp := client.RetryPolicy{
			MaxRetries:  3,
			InitialWait: time.Millisecond,
			Multiplier:  1,
			JitterFrac:  0,
			MaxWait:     5 * time.Millisecond,
		}
		c := client.New(client.Options{
			Tokens: client.Anonymous{},
			Retry:  &rp,
		})
		if err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL}, nil); err != nil {
			t.Fatalf("Do: %+v", err)
		}
		if atomic.LoadInt32(&hits) < 2 {
			t.Errorf("expected at least 2 hits (one retry), got %d", hits)
		}
	})
}
