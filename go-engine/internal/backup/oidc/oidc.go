// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package oidc fetches and caches the OIDC discovery document advertised
// by the substrate backend (or any self-host equivalent), validates the
// `endstate_extensions` block, and exposes the discovered endpoints +
// JWKS to the auth and client packages.
//
// Contract: docs/contracts/hosted-backup-contract.md §4 (JWKS), §9 (OIDC).
package oidc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultIssuerURL is the Endstate Cloud production issuer.
const DefaultIssuerURL = "https://substratesystems.io"

// DefaultAudience is the JWT `aud` claim Endstate Cloud signs.
const DefaultAudience = "endstate-backup"

// DiscoveryTTL is how long a successfully fetched discovery document is
// reused before refetch (contract §9: cached for 1 hour).
const DiscoveryTTL = time.Hour

// JWKSTTL governs how long a fetched JWKS is reused. Kept distinct from
// DiscoveryTTL so a key rotation triggered by the backend is picked up
// faster than the discovery document.
const JWKSTTL = 15 * time.Minute

// Required minimums advertised by `endstate_extensions.min_kdf_params`.
// Engine refuses to derive keys below these values regardless of what the
// server says (contract §2 floor).
const (
	requiredKDFMemory      = 65536
	requiredKDFIterations  = 3
	requiredKDFParallelism = 4
)

// Document is the subset of the OIDC discovery response Endstate consumes.
type Document struct {
	Issuer                            string             `json:"issuer"`
	JWKSURI                           string             `json:"jwks_uri"`
	IDTokenSigningAlgValuesSupported  []string           `json:"id_token_signing_alg_values_supported"`
	EndstateExtensions                EndstateExtensions `json:"endstate_extensions"`
}

// EndstateExtensions is the namespaced extension block required of any
// backend Endstate talks to (contract §9). Missing or invalid → engine
// refuses to use the backend.
type EndstateExtensions struct {
	AuthSignupEndpoint        string         `json:"auth_signup_endpoint"`
	AuthLoginEndpoint         string         `json:"auth_login_endpoint"`
	AuthRefreshEndpoint       string         `json:"auth_refresh_endpoint"`
	AuthLogoutEndpoint        string         `json:"auth_logout_endpoint"`
	AuthRecoverEndpoint       string         `json:"auth_recover_endpoint"`
	BackupAPIBase             string         `json:"backup_api_base"`
	SupportedKDFAlgorithms    []string       `json:"supported_kdf_algorithms"`
	SupportedEnvelopeVersions []int          `json:"supported_envelope_versions"`
	MinKDFParams              MinKDFParams   `json:"min_kdf_params"`
}

// MinKDFParams matches the contract §9 sub-block inside endstate_extensions.
type MinKDFParams struct {
	Memory      uint32 `json:"memory"`
	Iterations  uint32 `json:"iterations"`
	Parallelism uint8  `json:"parallelism"`
}

// JWKS is the JSON Web Key Set returned by the JWKS endpoint (contract §4).
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK is one EdDSA key in a JWKS. Endstate only supports EdDSA / Ed25519;
// other kty/crv combinations are ignored on parse so a future migration
// can add new keys to the set without breaking older clients.
type JWK struct {
	Kty string `json:"kty"`
	Crv string `json:"crv"`
	Kid string `json:"kid"`
	Alg string `json:"alg"`
	Use string `json:"use"`
	X   string `json:"x"` // base64url-encoded Ed25519 public key
}

// HTTPDoer abstracts the http.Client surface oidc needs. Lets tests inject
// httptest servers and explicit transports without depending on net/http
// global state.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// Client fetches and caches discovery + JWKS for one issuer. Safe for
// concurrent use.
type Client struct {
	issuerURL string
	http      HTTPDoer
	now       func() time.Time

	mu       sync.Mutex
	doc      *Document
	docExp   time.Time
	jwks     *JWKS
	jwksExp  time.Time
	jwksURI  string // remembered separately so a stale jwks does not cross-pollute when the doc URL changes
}

// NewClient returns a discovery client for the given issuer URL.
// Trailing slashes are tolerated.
func NewClient(issuerURL string, httpDoer HTTPDoer) *Client {
	if httpDoer == nil {
		httpDoer = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{
		issuerURL: strings.TrimRight(issuerURL, "/"),
		http:      httpDoer,
		now:       time.Now,
	}
}

// IssuerURL returns the configured issuer URL with no trailing slash.
func (c *Client) IssuerURL() string { return c.issuerURL }

// Discovery returns the (possibly cached) discovery document.
//
// Errors:
//   - any transport / parse / non-2xx response → wrapped error
//   - empty or invalid endstate_extensions → ErrIncompatibleIssuer
//
// Callers map ErrIncompatibleIssuer → envelope.ErrBackendIncompatible and
// other errors → envelope.ErrBackendUnreachable.
func (c *Client) Discovery(ctx context.Context) (*Document, error) {
	c.mu.Lock()
	if c.doc != nil && c.now().Before(c.docExp) {
		doc := c.doc
		c.mu.Unlock()
		return doc, nil
	}
	c.mu.Unlock()

	doc, err := c.fetchDiscovery(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateDocument(doc, c.issuerURL); err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.doc = doc
	c.docExp = c.now().Add(DiscoveryTTL)
	// If the JWKS URI changed, blow away any cached JWKS for the previous
	// URI so we don't validate against stale keys.
	if c.jwksURI != doc.JWKSURI {
		c.jwks = nil
		c.jwksExp = time.Time{}
		c.jwksURI = doc.JWKSURI
	}
	c.mu.Unlock()
	return doc, nil
}

// JWKS returns the (possibly cached) JWK set.
func (c *Client) JWKS(ctx context.Context) (*JWKS, error) {
	doc, err := c.Discovery(ctx)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	if c.jwks != nil && c.now().Before(c.jwksExp) {
		ks := c.jwks
		c.mu.Unlock()
		return ks, nil
	}
	c.mu.Unlock()

	keys, err := c.fetchJWKS(ctx, doc.JWKSURI)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.jwks = keys
	c.jwksExp = c.now().Add(JWKSTTL)
	c.mu.Unlock()
	return keys, nil
}

// InvalidateJWKS forces the next JWKS call to refetch. Auth callers invoke
// this after a signature verification failure so a freshly rotated key is
// picked up without waiting out JWKSTTL.
func (c *Client) InvalidateJWKS() {
	c.mu.Lock()
	c.jwks = nil
	c.jwksExp = time.Time{}
	c.mu.Unlock()
}

// SetClock overrides the time source. Used by tests.
func (c *Client) SetClock(now func() time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now
}

// ErrIncompatibleIssuer is returned by Discovery when the backend either
// fails to advertise the `endstate_extensions` block or advertises values
// the engine refuses (KDF floor, missing envelope version 1, missing
// argon2id algorithm).
var ErrIncompatibleIssuer = errors.New("oidc: backend does not advertise required endstate_extensions")

func (c *Client) fetchDiscovery(ctx context.Context) (*Document, error) {
	url := c.issuerURL + "/.well-known/openid-configuration"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: build discovery request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: fetch discovery: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("oidc: discovery returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc: read discovery body: %w", err)
	}
	var doc Document
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("oidc: decode discovery: %w", err)
	}
	return &doc, nil
}

func (c *Client) fetchJWKS(ctx context.Context, jwksURI string) (*JWKS, error) {
	if jwksURI == "" {
		return nil, errors.New("oidc: discovery document has empty jwks_uri")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURI, nil)
	if err != nil {
		return nil, fmt.Errorf("oidc: build jwks request: %w", err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("oidc: fetch jwks: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("oidc: jwks returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("oidc: read jwks body: %w", err)
	}
	var keys JWKS
	if err := json.Unmarshal(body, &keys); err != nil {
		return nil, fmt.Errorf("oidc: decode jwks: %w", err)
	}
	return &keys, nil
}

func validateDocument(doc *Document, expectedIssuer string) error {
	if doc.Issuer == "" || strings.TrimRight(doc.Issuer, "/") != expectedIssuer {
		return fmt.Errorf("oidc: issuer mismatch (got %q, want %q)", doc.Issuer, expectedIssuer)
	}
	if doc.JWKSURI == "" {
		return errors.New("oidc: discovery document missing jwks_uri")
	}
	if !contains(doc.IDTokenSigningAlgValuesSupported, "EdDSA") {
		return errors.New("oidc: backend does not advertise EdDSA support")
	}

	ext := doc.EndstateExtensions
	if ext.AuthSignupEndpoint == "" || ext.AuthLoginEndpoint == "" ||
		ext.AuthRefreshEndpoint == "" || ext.AuthLogoutEndpoint == "" ||
		ext.AuthRecoverEndpoint == "" || ext.BackupAPIBase == "" {
		return ErrIncompatibleIssuer
	}
	if !contains(ext.SupportedKDFAlgorithms, "argon2id") {
		return ErrIncompatibleIssuer
	}
	if !containsInt(ext.SupportedEnvelopeVersions, 1) {
		return ErrIncompatibleIssuer
	}
	if ext.MinKDFParams.Memory < requiredKDFMemory ||
		ext.MinKDFParams.Iterations < requiredKDFIterations ||
		ext.MinKDFParams.Parallelism < requiredKDFParallelism {
		return ErrIncompatibleIssuer
	}
	return nil
}

func contains(s []string, want string) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}

func containsInt(s []int, want int) bool {
	for _, v := range s {
		if v == want {
			return true
		}
	}
	return false
}
