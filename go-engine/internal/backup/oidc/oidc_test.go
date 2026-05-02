// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package oidc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
)

// validDiscovery returns a discovery document that satisfies all engine
// requirements. Test cases mutate it to drive the negative paths.
func validDiscovery(issuer string) oidc.Document {
	return oidc.Document{
		Issuer:                            issuer,
		JWKSURI:                           issuer + "/api/.well-known/jwks.json",
		IDTokenSigningAlgValuesSupported:  []string{"EdDSA"},
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
