// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
)

// Claims is the EdDSA-signed access-token payload shape from contract §4.
type Claims struct {
	jwt.RegisteredClaims
	SubscriptionStatus string `json:"subscription_status,omitempty"`
}

// VerifyOptions captures the per-call expectations the engine enforces on
// every access token: the audience the client requires, the issuer the
// engine is talking to, and the JWKS to verify the signature against.
type VerifyOptions struct {
	ExpectedIssuer   string
	ExpectedAudience string
	JWKS             *oidc.JWKS
	Now              time.Time
}

// JWKSResolver fetches the JWK set the verifier uses. Implemented by
// oidc.Client (real) or a stub in tests.
type JWKSResolver interface {
	JWKS(ctx context.Context) (*oidc.JWKS, error)
	InvalidateJWKS()
}

// Verify parses and validates the supplied access token. The token must
// be signed with EdDSA, contain the expected `iss` and `aud`, be within
// its `nbf` and `exp` window (±60s clock skew), and carry a `kid` that
// resolves to a key in the JWKS.
//
// On signature failure the JWKS cache is invalidated so the next call
// refreshes from the backend (handles key rotation).
func Verify(ctx context.Context, tokenStr string, resolver JWKSResolver, opts VerifyOptions) (*Claims, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithIssuer(opts.ExpectedIssuer),
		jwt.WithAudience(opts.ExpectedAudience),
		jwt.WithLeeway(60*time.Second),
	)

	keyFunc := func(token *jwt.Token) (interface{}, error) {
		return lookupKey(opts.JWKS, token)
	}

	tok, err := parser.ParseWithClaims(tokenStr, &Claims{}, keyFunc)
	if err != nil {
		// Signature failures are most often a rotated key; invalidate the
		// JWKS cache so the next request fetches fresh keys.
		if errors.Is(err, jwt.ErrTokenSignatureInvalid) || errors.Is(err, jwt.ErrTokenUnverifiable) {
			resolver.InvalidateJWKS()
		}
		return nil, err
	}
	if !tok.Valid {
		return nil, errors.New("auth: token marked invalid by parser")
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok {
		return nil, errors.New("auth: claims type assertion failed")
	}

	// jwt/v5 already validates iss/aud/exp/nbf via the parser options. We
	// double-check exp and nbf against the supplied time so tests can
	// exercise the boundary deterministically.
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	if claims.ExpiresAt != nil && now.After(claims.ExpiresAt.Time.Add(60*time.Second)) {
		return nil, jwt.ErrTokenExpired
	}
	if claims.NotBefore != nil && now.Add(60*time.Second).Before(claims.NotBefore.Time) {
		return nil, jwt.ErrTokenNotValidYet
	}
	return claims, nil
}

// lookupKey finds the JWK matching the token's `kid` and returns its
// ed25519.PublicKey. Returns an error if no key is found or the key is
// not a usable Ed25519 OKP key.
func lookupKey(set *oidc.JWKS, token *jwt.Token) (interface{}, error) {
	if set == nil {
		return nil, errors.New("auth: empty JWKS")
	}
	kid, _ := token.Header["kid"].(string)
	for _, k := range set.Keys {
		if kid != "" && k.Kid != kid {
			continue
		}
		if !strings.EqualFold(k.Kty, "OKP") || !strings.EqualFold(k.Crv, "Ed25519") {
			continue
		}
		raw, err := base64URLDecode(k.X)
		if err != nil {
			return nil, fmt.Errorf("auth: decode JWK x: %w", err)
		}
		if len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("auth: JWK x has wrong length %d (want %d)", len(raw), ed25519.PublicKeySize)
		}
		return ed25519.PublicKey(raw), nil
	}
	return nil, fmt.Errorf("auth: no Ed25519 key matching kid %q", kid)
}

func base64URLDecode(s string) ([]byte, error) {
	// Try unpadded URL-safe first; some JWKS implementations include
	// padding, which the standard URL encoding accepts when explicitly
	// padded. RawURLEncoding with auto-padding handled.
	s = strings.TrimRight(s, "=")
	return base64.RawURLEncoding.DecodeString(s)
}
