// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package auth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
)

const (
	testIssuer   = "https://substratesystems.io"
	testAudience = "endstate-backup"
)

// stubResolver is a JWKSResolver that returns a fixed JWKS and counts
// invalidations.
type stubResolver struct {
	keys           *oidc.JWKS
	invalidations  int
}

func (s *stubResolver) JWKS(context.Context) (*oidc.JWKS, error) { return s.keys, nil }
func (s *stubResolver) InvalidateJWKS()                          { s.invalidations++ }

// signTestToken mints an Ed25519-signed access token with the supplied
// claim overrides.
func signTestToken(t *testing.T, kid string, priv ed25519.PrivateKey, override func(c *auth.Claims)) string {
	t.Helper()
	now := time.Now()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    testIssuer,
			Subject:   "user-1",
			Audience:  jwt.ClaimStrings{testAudience},
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
			ID:        "jti-1",
		},
		SubscriptionStatus: "active",
	}
	if override != nil {
		override(claims)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	tok.Header["kid"] = kid
	signed, err := tok.SignedString(priv)
	if err != nil {
		t.Fatalf("SignedString: %v", err)
	}
	return signed
}

// testJWKS builds a JWK set containing a single Ed25519 public key with
// the given kid.
func testJWKS(t *testing.T, kid string, pub ed25519.PublicKey) *oidc.JWKS {
	t.Helper()
	return &oidc.JWKS{
		Keys: []oidc.JWK{{
			Kty: "OKP",
			Crv: "Ed25519",
			Kid: kid,
			Alg: "EdDSA",
			Use: "sig",
			X:   base64.RawURLEncoding.EncodeToString(pub),
		}},
	}
}

func newKeyPair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	return pub, priv
}

func TestVerify_ValidToken(t *testing.T) {
	pub, priv := newKeyPair(t)
	keys := testJWKS(t, "k1", pub)
	resolver := &stubResolver{keys: keys}
	tokenStr := signTestToken(t, "k1", priv, nil)

	claims, err := auth.Verify(context.Background(), tokenStr, resolver, auth.VerifyOptions{
		ExpectedIssuer:   testIssuer,
		ExpectedAudience: testAudience,
		JWKS:             keys,
		Now:              time.Now(),
	})
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("sub = %q, want user-1", claims.Subject)
	}
	if claims.SubscriptionStatus != "active" {
		t.Errorf("subscription_status = %q, want active", claims.SubscriptionStatus)
	}
	if resolver.invalidations != 0 {
		t.Errorf("invalidations = %d, want 0 on success", resolver.invalidations)
	}
}

func TestVerify_ExpiredToken(t *testing.T) {
	pub, priv := newKeyPair(t)
	keys := testJWKS(t, "k1", pub)
	resolver := &stubResolver{keys: keys}
	tokenStr := signTestToken(t, "k1", priv, func(c *auth.Claims) {
		past := time.Now().Add(-2 * time.Hour)
		c.IssuedAt = jwt.NewNumericDate(past)
		c.NotBefore = jwt.NewNumericDate(past)
		c.ExpiresAt = jwt.NewNumericDate(past.Add(15 * time.Minute))
	})
	_, err := auth.Verify(context.Background(), tokenStr, resolver, auth.VerifyOptions{
		ExpectedIssuer:   testIssuer,
		ExpectedAudience: testAudience,
		JWKS:             keys,
		Now:              time.Now(),
	})
	if err == nil || !errors.Is(err, jwt.ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestVerify_WrongAudience(t *testing.T) {
	pub, priv := newKeyPair(t)
	keys := testJWKS(t, "k1", pub)
	resolver := &stubResolver{keys: keys}
	tokenStr := signTestToken(t, "k1", priv, func(c *auth.Claims) {
		c.Audience = jwt.ClaimStrings{"some-other-aud"}
	})
	_, err := auth.Verify(context.Background(), tokenStr, resolver, auth.VerifyOptions{
		ExpectedIssuer:   testIssuer,
		ExpectedAudience: testAudience,
		JWKS:             keys,
		Now:              time.Now(),
	})
	if err == nil || !errors.Is(err, jwt.ErrTokenInvalidAudience) {
		t.Errorf("expected ErrTokenInvalidAudience, got %v", err)
	}
}

func TestVerify_WrongIssuer(t *testing.T) {
	pub, priv := newKeyPair(t)
	keys := testJWKS(t, "k1", pub)
	resolver := &stubResolver{keys: keys}
	tokenStr := signTestToken(t, "k1", priv, func(c *auth.Claims) {
		c.Issuer = "https://attacker.example.com"
	})
	_, err := auth.Verify(context.Background(), tokenStr, resolver, auth.VerifyOptions{
		ExpectedIssuer:   testIssuer,
		ExpectedAudience: testAudience,
		JWKS:             keys,
		Now:              time.Now(),
	})
	if err == nil || !errors.Is(err, jwt.ErrTokenInvalidIssuer) {
		t.Errorf("expected ErrTokenInvalidIssuer, got %v", err)
	}
}

func TestVerify_BadSignature_InvalidatesJWKS(t *testing.T) {
	// Sign with one key; verify against a JWKS containing a different key.
	_, priv := newKeyPair(t)
	otherPub, _ := newKeyPair(t)
	keys := testJWKS(t, "k1", otherPub)
	resolver := &stubResolver{keys: keys}

	tokenStr := signTestToken(t, "k1", priv, nil)

	_, err := auth.Verify(context.Background(), tokenStr, resolver, auth.VerifyOptions{
		ExpectedIssuer:   testIssuer,
		ExpectedAudience: testAudience,
		JWKS:             keys,
		Now:              time.Now(),
	})
	if err == nil {
		t.Fatal("expected error on bad signature")
	}
	if resolver.invalidations != 1 {
		t.Errorf("invalidations = %d, want 1 (signature failure should invalidate JWKS)", resolver.invalidations)
	}
}

func TestVerify_RotatedKid(t *testing.T) {
	// JWKS contains key kid="k2" only; token signed with key kid="k1".
	// The verifier must fail to find a matching kid.
	_, priv := newKeyPair(t)
	otherPub, _ := newKeyPair(t)
	keys := testJWKS(t, "k2", otherPub)
	resolver := &stubResolver{keys: keys}

	tokenStr := signTestToken(t, "k1", priv, nil)
	_, err := auth.Verify(context.Background(), tokenStr, resolver, auth.VerifyOptions{
		ExpectedIssuer:   testIssuer,
		ExpectedAudience: testAudience,
		JWKS:             keys,
		Now:              time.Now(),
	})
	if err == nil {
		t.Fatal("expected error when no matching kid")
	}
}
