// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package oidc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
)

// validDiscovery returns a discovery document that satisfies all engine
// requirements. Test cases mutate it to drive the negative paths.
func validDiscovery(issuer string) oidc.Document {
	return oidc.Document{
		Issuer:                           issuer,
		JWKSURI:                          issuer + "/api/.well-known/jwks.json",
		IDTokenSigningAlgValuesSupported: []string{"EdDSA"},
		EndstateExtensions: oidc.EndstateExtensions{
			AuthSignupEndpoint:        issuer + "/api/auth/signup",
			AuthLoginEndpoint:         issuer + "/api/auth/login",
			AuthRefreshEndpoint:       issuer + "/api/auth/refresh",
			AuthLogoutEndpoint:        issuer + "/api/auth/logout",
			AuthRecoverEndpoint:       issuer + "/api/auth/recover",
			BackupAPIBase:             issuer + "/api/backups",
			SupportedKDFAlgorithms:    []string{"argon2id"},
			SupportedEnvelopeVersions: []int{1},
			MinKDFParams:              oidc.MinKDFParams{Memory: 65536, Iterations: 3, Parallelism: 4},
		},
	}
}

// fakeBackend serves a discovery document the test mutates. The same
// httptest server hosts the JWKS endpoint, so the issuer URL inside the
// served body resolves back to the same listener address.
func fakeBackend(t *testing.T, mutate func(d *oidc.Document)) (*httptest.Server, *int32, *int32) {
	t.Helper()
	var discoveryHits int32
	var jwksHits int32
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&discoveryHits, 1)
		doc := validDiscovery(srv.URL)
		if mutate != nil {
			mutate(&doc)
		}
		_ = json.NewEncoder(w).Encode(doc)
	})
	mux.HandleFunc("/api/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&jwksHits, 1)
		_ = json.NewEncoder(w).Encode(oidc.JWKS{
			Keys: []oidc.JWK{{Kty: "OKP", Crv: "Ed25519", Kid: "key-1", Alg: "EdDSA", Use: "sig", X: "AAAA"}},
		})
	})
	t.Cleanup(srv.Close)
	return srv, &discoveryHits, &jwksHits
}

func TestDiscovery_FetchAndCache(t *testing.T) {
	srv, hits, _ := fakeBackend(t, nil)
	c := oidc.NewClient(srv.URL, srv.Client())

	doc, err := c.Discovery(context.Background())
	if err != nil {
		t.Fatalf("first fetch: %v", err)
	}
	if doc.Issuer != srv.URL {
		t.Errorf("issuer = %q, want %q", doc.Issuer, srv.URL)
	}

	// Second call should hit the cache.
	if _, err := c.Discovery(context.Background()); err != nil {
		t.Fatalf("second fetch: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("discovery hits = %d, want 1 (cache miss after first fetch)", got)
	}
}

func TestDiscovery_TTLExpiry(t *testing.T) {
	srv, hits, _ := fakeBackend(t, nil)
	c := oidc.NewClient(srv.URL, srv.Client())

	// Fixed clock the test advances explicitly.
	now := time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC)
	c.SetClock(func() time.Time { return now })

	if _, err := c.Discovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("hits = %d, want 1", got)
	}

	// Just before TTL expiry — still cached.
	now = now.Add(oidc.DiscoveryTTL - time.Second)
	if _, err := c.Discovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Errorf("hits before TTL = %d, want 1", got)
	}

	// One second after TTL — refetched.
	now = now.Add(2 * time.Second)
	if _, err := c.Discovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Errorf("hits after TTL = %d, want 2", got)
	}
}

func TestDiscovery_RejectsMissingExtensions(t *testing.T) {
	srv, _, _ := fakeBackend(t, func(d *oidc.Document) {
		d.EndstateExtensions = oidc.EndstateExtensions{}
	})
	c := oidc.NewClient(srv.URL, srv.Client())
	_, err := c.Discovery(context.Background())
	if !errors.Is(err, oidc.ErrIncompatibleIssuer) {
		t.Errorf("expected ErrIncompatibleIssuer, got %v", err)
	}
}

func TestDiscovery_RejectsMissingSecurityFieldsAsIncompatible(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*oidc.Document)
	}{
		{
			name: "missing jwks URI",
			mutate: func(d *oidc.Document) {
				d.JWKSURI = ""
			},
		},
		{
			name: "missing EdDSA support",
			mutate: func(d *oidc.Document) {
				d.IDTokenSigningAlgValuesSupported = nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv, _, _ := fakeBackend(t, tt.mutate)
			_, err := oidc.NewClient(srv.URL, srv.Client()).Discovery(context.Background())
			if !errors.Is(err, oidc.ErrIncompatibleIssuer) {
				t.Errorf("Discovery() error = %v, want ErrIncompatibleIssuer", err)
			}
		})
	}
}

func TestDiscovery_RejectsMissingArgon2id(t *testing.T) {
	srv, _, _ := fakeBackend(t, func(d *oidc.Document) {
		d.EndstateExtensions.SupportedKDFAlgorithms = []string{"argon2i"}
	})
	c := oidc.NewClient(srv.URL, srv.Client())
	_, err := c.Discovery(context.Background())
	if !errors.Is(err, oidc.ErrIncompatibleIssuer) {
		t.Errorf("expected ErrIncompatibleIssuer, got %v", err)
	}
}

func TestDiscovery_RejectsWeakerKDFFloor(t *testing.T) {
	srv, _, _ := fakeBackend(t, func(d *oidc.Document) {
		d.EndstateExtensions.MinKDFParams.Memory = 32768
	})
	c := oidc.NewClient(srv.URL, srv.Client())
	_, err := c.Discovery(context.Background())
	if !errors.Is(err, oidc.ErrIncompatibleIssuer) {
		t.Errorf("expected ErrIncompatibleIssuer for weak memory floor, got %v", err)
	}
}

func TestDiscovery_RejectsMissingEnvelopeV1(t *testing.T) {
	srv, _, _ := fakeBackend(t, func(d *oidc.Document) {
		d.EndstateExtensions.SupportedEnvelopeVersions = []int{2}
	})
	c := oidc.NewClient(srv.URL, srv.Client())
	_, err := c.Discovery(context.Background())
	if !errors.Is(err, oidc.ErrIncompatibleIssuer) {
		t.Errorf("expected ErrIncompatibleIssuer when v1 envelope missing, got %v", err)
	}
}

func TestDiscovery_RejectsIssuerMismatch(t *testing.T) {
	srv, _, _ := fakeBackend(t, func(d *oidc.Document) {
		d.Issuer = "https://attacker.example.com"
	})
	c := oidc.NewClient(srv.URL, srv.Client())
	_, err := c.Discovery(context.Background())
	if err == nil || errors.Is(err, oidc.ErrIncompatibleIssuer) {
		t.Errorf("expected non-incompatible issuer mismatch error, got %v", err)
	}
}

func TestDiscovery_NetworkErrorBubbles(t *testing.T) {
	c := oidc.NewClient("http://127.0.0.1:1", &http.Client{Timeout: 100 * time.Millisecond})
	_, err := c.Discovery(context.Background())
	if err == nil {
		t.Fatal("expected error on unreachable backend")
	}
	if !errors.Is(err, oidc.ErrDiscoveryTransport) {
		t.Errorf("Discovery() error = %v, want ErrDiscoveryTransport", err)
	}
}

func TestDiscovery_DefaultClientBlocksCrossOriginRedirectWithoutLeakingURL(t *testing.T) {
	var managedHits int32
	managed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&managedHits, 1)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer managed.Close()

	selfHosted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, managed.URL+"/openid?redirectSecret=do-not-leak", http.StatusPermanentRedirect)
	}))
	defer selfHosted.Close()

	_, err := oidc.NewClient(selfHosted.URL, nil).Discovery(context.Background())
	if err == nil {
		t.Fatal("Discovery() error = nil, want blocked redirect")
	}
	for _, secret := range []string{"redirectSecret", "?"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("error leaked %q: %q", secret, err)
		}
	}
	if got := atomic.LoadInt32(&managedHits); got != 0 {
		t.Errorf("managed-host requests = %d, want 0", got)
	}
}

func TestDiscovery_DefaultClientAllowsSameOriginRedirect(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/discovery", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/discovery", func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewEncoder(w).Encode(validDiscovery(server.URL)); err != nil {
			t.Fatalf("encode discovery: %v", err)
		}
	})

	doc, err := oidc.NewClient(server.URL, nil).Discovery(context.Background())
	if err != nil {
		t.Fatalf("Discovery(): %v", err)
	}
	if doc.Issuer != server.URL {
		t.Errorf("issuer = %q, want %q", doc.Issuer, server.URL)
	}
}

func TestJWKS_FetchAndCache(t *testing.T) {
	srv, _, jHits := fakeBackend(t, nil)
	c := oidc.NewClient(srv.URL, srv.Client())

	if _, err := c.JWKS(context.Background()); err != nil {
		t.Fatalf("first jwks fetch: %v", err)
	}
	if _, err := c.JWKS(context.Background()); err != nil {
		t.Fatalf("second jwks fetch: %v", err)
	}
	if got := atomic.LoadInt32(jHits); got != 1 {
		t.Errorf("jwks hits = %d, want 1 (second call should be cached)", got)
	}
}

func TestJWKS_InvalidatePicksUpNewKey(t *testing.T) {
	srv, _, jHits := fakeBackend(t, nil)
	c := oidc.NewClient(srv.URL, srv.Client())

	if _, err := c.JWKS(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.InvalidateJWKS()
	if _, err := c.JWKS(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(jHits); got != 2 {
		t.Errorf("jwks hits after invalidation = %d, want 2", got)
	}
}

// TestDiscovery_IssuerMismatch_ReturnsErrIssuerMismatch covers the
// engine-substrate env-var-disagreement failure mode. When substrate's
// ENDSTATE_OIDC_ISSUER_URL drifts from the engine's, substrate stamps
// the wrong issuer claim into the discovery doc; the engine refuses
// loudly via ErrIssuerMismatch instead of the generic transport error.
func TestDiscovery_IssuerMismatch_ReturnsErrIssuerMismatch(t *testing.T) {
	srv, _, _ := fakeBackend(t, func(d *oidc.Document) {
		// Substrate is misconfigured — discovery says one issuer, engine
		// is configured for another.
		d.Issuer = "https://misconfigured.example.com"
	})
	c := oidc.NewClient(srv.URL, srv.Client())
	_, err := c.Discovery(context.Background())
	if err == nil {
		t.Fatal("expected an error from issuer mismatch, got nil")
	}
	if !errors.Is(err, oidc.ErrIssuerMismatch) {
		t.Errorf("err = %v, want errors.Is ErrIssuerMismatch", err)
	}
}

// TestDiscovery_BackupAPIBase_PassesThroughCustomURL covers the
// self-host case where backup_api_base advertises a non-default path.
// The engine must accept the custom value verbatim — downstream callers
// (storage.Client) consume it.
func TestDiscovery_BackupAPIBase_PassesThroughCustomURL(t *testing.T) {
	const customBase = "https://files.example.com/v1/backups"
	srv, _, _ := fakeBackend(t, func(d *oidc.Document) {
		d.EndstateExtensions.BackupAPIBase = customBase
	})
	c := oidc.NewClient(srv.URL, srv.Client())
	doc, err := c.Discovery(context.Background())
	if err != nil {
		t.Fatalf("Discovery: %v", err)
	}
	if doc.EndstateExtensions.BackupAPIBase != customBase {
		t.Errorf("BackupAPIBase = %q, want %q", doc.EndstateExtensions.BackupAPIBase, customBase)
	}
}

func TestDiscovery_BackupAPICapabilityRequiresExactReplayToken(t *testing.T) {
	tests := []struct {
		name string
		json string
		want bool
	}{
		{name: "exact token", json: `["version-create-operation-replay-v1"]`, want: true},
		{name: "unknown token", json: `["future-replay-v2"]`, want: false},
		{name: "mixed token", json: `["future-replay-v2","version-create-operation-replay-v1"]`, want: true},
		{name: "malformed capability", json: `"version-create-operation-replay-v1"`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var doc oidc.Document
			if err := json.Unmarshal([]byte(`{"endstate_extensions":{"backup_api_capabilities":`+tt.json+`}}`), &doc); err != nil {
				t.Fatalf("decode discovery: %v", err)
			}
			method := reflect.ValueOf(doc.EndstateExtensions).MethodByName("SupportsVersionCreateOperationReplay")
			if !method.IsValid() {
				t.Fatal("EndstateExtensions is missing SupportsVersionCreateOperationReplay")
			}
			got := method.Call(nil)[0].Bool()
			if got != tt.want {
				t.Errorf("capability supported = %v, want %v", got, tt.want)
			}
		})
	}
}
