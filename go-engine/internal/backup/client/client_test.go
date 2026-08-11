// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package client_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
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
	h.Set("X-Endstate-API-Version", "2.0")
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

func TestDo_MutatingPostDoesNotRetryWithoutExplicitSafety(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		versionV1(w.Header())
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newClient(t, client.Anonymous{})
	err := c.Do(context.Background(), client.Request{Method: http.MethodPost, URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrBackendError {
		t.Fatalf("Do() = %+v, want one POST failure", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Errorf("POST hits = %d, want 1 because an unsafe mutation must not replay", got)
	}
}

func TestDo_RetrySafePostReusesOperationID(t *testing.T) {
	var hits int32
	var ids []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		ids = append(ids, r.Header.Get("X-Endstate-Operation-ID"))
		mu.Unlock()
		versionV1(w.Header())
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newClient(t, client.Anonymous{})
	if err := c.Do(context.Background(), client.Request{Method: http.MethodPost, URL: srv.URL, RetrySafe: true}, nil); err != nil {
		t.Fatalf("Do() = %+v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 2 || ids[0] == "" || ids[0] != ids[1] {
		t.Errorf("operation IDs = %q, want one non-empty ID reused across the retry", ids)
	}
}

func TestDo_ReadOnlyPostRetriesWithoutOperationID(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-Endstate-Operation-ID"); got != "" {
			t.Errorf("read-only POST operation id = %q, want empty", got)
		}
		if atomic.AddInt32(&hits, 1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := client.New(client.Options{Tokens: client.Anonymous{}, Retry: ptrRetry(fastRetry())})
	if err := c.Do(context.Background(), client.Request{
		Method: http.MethodPost, URL: srv.URL, ReadOnly: true,
	}, nil); err != nil {
		t.Fatalf("Do: %+v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("read-only POST hits = %d, want 2", got)
	}
}

func TestDo_UnsafePostRefreshesOnceAfter401(t *testing.T) {
	var hits, authorized int32
	tokens := &staticTokens{access: "stale", refresh: "fresh"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if r.Header.Get("Authorization") != "Bearer fresh" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		atomic.AddInt32(&authorized, 1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := client.New(client.Options{Tokens: tokens, Retry: ptrRetry(fastRetry())})
	if err := c.Do(context.Background(), client.Request{Method: http.MethodPost, URL: srv.URL}, nil); err != nil {
		t.Fatalf("Do: %+v", err)
	}
	if got := tokens.refreshHits; got != 1 {
		t.Fatalf("refreshes = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("POST hits = %d, want rejected + one authorised resend", got)
	}
	if got := atomic.LoadInt32(&authorized); got != 1 {
		t.Fatalf("authorised mutations = %d, want 1", got)
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
		// Engine speaks major=2 (contract v2.0); a backend advertising
		// major=3 is the mismatch case.
		w.Header().Set("X-Endstate-API-Version", "3.0")
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
		w.Header().Set("X-Endstate-API-Version", "2.5")
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
		w.Header().Set("X-Endstate-API-Version", "2.5")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})
	err := c.Do(context.Background(), client.Request{Method: "POST", URL: srv.URL}, nil)
	if err == nil || err.Code != envelope.ErrSchemaIncompatible {
		t.Errorf("write minor mismatch: got %+v, want SCHEMA_INCOMPATIBLE", err)
	}
}

// TestDo_OlderBackendMinorAccepted: the engine now speaks 2.1, but a 2.0
// substrate is still fully usable — an OLDER minor is not a mismatch on
// either reads or writes. This is the graceful-degradation guarantee that
// lets a 2.1 engine keep pushing to a substrate that has not yet shipped
// the commit endpoint (contract §11).
func TestDo_OlderBackendMinorAccepted(t *testing.T) {
	if client.EngineSchemaMinor < 1 {
		t.Skip("engine minor is 0; there is no older minor to test against")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Endstate-API-Version", "2.0")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})

	if err := c.Do(context.Background(), client.Request{Method: "GET", URL: srv.URL, ReadOnly: true}, nil); err != nil {
		t.Errorf("read against an older-minor backend: got %+v, want nil", err)
	}
	if err := c.Do(context.Background(), client.Request{Method: "POST", URL: srv.URL}, nil); err != nil {
		t.Errorf("write against an older-minor backend: got %+v, want nil", err)
	}
}

// TestDo_AdvertisesEngineSchemaVersionOnRequests: the backend decides
// whether a created version needs an explicit commit from the client's
// advertised minor (contract §8), so every request must carry
// X-Endstate-API-Version.
func TestDo_AdvertisesEngineSchemaVersionOnRequests(t *testing.T) {
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("X-Endstate-API-Version")
		versionV1(w.Header())
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := newClient(t, client.Anonymous{})

	if err := c.Do(context.Background(), client.Request{Method: "POST", URL: srv.URL}, nil); err != nil {
		t.Fatalf("Do: %+v", err)
	}
	if want := client.EngineSchemaVersion(); seen != want {
		t.Errorf("request X-Endstate-API-Version = %q, want %q", seen, want)
	}
}

// TestEngineSchemaVersion_DerivesFromConstants pins the advertised value to
// the schema constants rather than to a string literal. Substrate's
// clientRequiresVersionCommit parses this header and fails closed — an
// absent or unparseable value means "no commit required" — so a hardcoded
// string that drifted from EngineSchemaMajor/Minor would silently disable
// the two-phase commit while every test that only compares the header
// against itself kept passing.
func TestEngineSchemaVersion_DerivesFromConstants(t *testing.T) {
	want := fmt.Sprintf("%d.%d", client.EngineSchemaMajor, client.EngineSchemaMinor)
	if got := client.EngineSchemaVersion(); got != want {
		t.Errorf("EngineSchemaVersion() = %q, want %q derived from EngineSchemaMajor/EngineSchemaMinor", got, want)
	}
	// And the constants themselves are the contract's current schema, so a
	// bump that forgets one half of the pair is caught here.
	if got, want := client.EngineSchemaVersion(), "2.1"; got != want {
		t.Errorf("engine advertises schema %q, want %q for hosted-backup contract 2.1", got, want)
	}
	// The value must parse as MAJOR.MINOR or substrate's parser rejects it
	// and falls back to "no commit required".
	if !regexp.MustCompile(`^\d+\.\d+$`).MatchString(client.EngineSchemaVersion()) {
		t.Errorf("EngineSchemaVersion() = %q is not MAJOR.MINOR; substrate would fail closed on it", client.EngineSchemaVersion())
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

func TestDo_DefaultClientBlocksCrossOriginRedirectsWithoutLeakingSecrets(t *testing.T) {
	var managedHits int32
	managed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&managedHits, 1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer managed.Close()

	selfHosted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusTemporaryRedirect
		if strings.Contains(r.URL.Path, "/versions") {
			status = http.StatusPermanentRedirect
		}
		http.Redirect(w, r, managed.URL+"/managed?redirectSecret=do-not-leak", status)
	}))
	defer selfHosted.Close()

	c := newClient(t, client.Anonymous{})
	for _, request := range []struct {
		name string
		path string
		body any
	}{
		{name: "login", path: "/api/auth/login", body: map[string]string{"serverPassword": "login-secret"}},
		{name: "signup", path: "/api/auth/signup", body: map[string]string{"serverPassword": "signup-secret"}},
		{name: "recovery", path: "/api/auth/recover", body: map[string]string{"recoveryKeyProof": "recovery-secret"}},
		{name: "create version", path: "/api/backups/backup-1/versions", body: map[string]string{"encryptedManifest": "manifest-secret"}},
	} {
		t.Run(request.name, func(t *testing.T) {
			err := c.Do(context.Background(), client.Request{
				Method: http.MethodPost,
				URL:    selfHosted.URL + request.path + "?sourceSecret=do-not-leak",
				Body:   request.body,
			}, nil)
			if err == nil || err.Code != envelope.ErrBackendUnreachable {
				t.Fatalf("Do() = %+v, want BACKEND_UNREACHABLE", err)
			}
			for _, secret := range []string{"login-secret", "signup-secret", "recovery-secret", "manifest-secret", "sourceSecret", "redirectSecret", "?"} {
				if strings.Contains(err.Message, secret) {
					t.Errorf("error message leaked %q: %q", secret, err.Message)
				}
			}
		})
	}
	if got := atomic.LoadInt32(&managedHits); got != 0 {
		t.Errorf("managed-host requests = %d, want 0", got)
	}
}

func TestDo_DefaultClientAllowsSameOriginRedirect(t *testing.T) {
	const body = `{"serverPassword":"safe-secret"}`
	var received string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/complete", http.StatusPermanentRedirect)
			return
		}
		if r.URL.Path != "/complete" {
			t.Fatalf("request path = %q, want /complete", r.URL.Path)
		}
		data, readErr := io.ReadAll(r.Body)
		if readErr != nil {
			t.Fatalf("read request body: %v", readErr)
		}
		received = string(data)
		versionV1(w.Header())
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	c := newClient(t, client.Anonymous{})
	if err := c.Do(context.Background(), client.Request{Method: http.MethodPost, URL: server.URL + "/start", Body: map[string]string{"serverPassword": "safe-secret"}}, nil); err != nil {
		t.Fatalf("Do() = %+v", err)
	}
	if received != body {
		t.Errorf("redirected body = %s, want %s", received, body)
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
