// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	stdBase64Pkg "encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

var stdBase64 = stdBase64Pkg.StdEncoding

// Authenticator owns the substrate auth API surface (contract §5–§6) for
// one issuer + audience pair. It coordinates between the OIDC discovery
// client (for endpoint URLs and JWKS), the HTTP client (for transport),
// the SessionStore (for in-memory + keychain persistence), and the
// crypto package (for KDF / DEK wrap, currently STUBS).
type Authenticator struct {
	issuer  Issuer
	oidc    *oidc.Client
	httpc   *client.Client
	session *SessionStore
}

// NewAuthenticator constructs an Authenticator. The supplied client.Client
// MUST be configured with a TokenProvider that defers to session — see
// auth.MakeTokenProvider helper.
func NewAuthenticator(issuer Issuer, o *oidc.Client, c *client.Client, s *SessionStore) *Authenticator {
	a := &Authenticator{issuer: issuer, oidc: o, httpc: c, session: s}
	// Wire the refresh callback so the HTTP client's 401-refresh hook
	// reaches into our /api/auth/refresh handler instead of looping.
	s.WithRefreshFn(a.refreshAccessToken)
	return a
}

// Issuer returns the configured issuer.
func (a *Authenticator) Issuer() Issuer { return a.issuer }

// Session returns the underlying SessionStore.
func (a *Authenticator) Session() *SessionStore { return a.session }

// MakeTokenProvider returns a client.TokenProvider that draws access and
// refresh from the supplied SessionStore. Provided here so the wiring in
// command handlers reads in one place.
func MakeTokenProvider(s *SessionStore) client.TokenProvider { return s }

// preHandshakeRequest matches the contract §5 step-1 body shape.
type preHandshakeRequest struct {
	Email string `json:"email"`
}

// preHandshakeResponse matches the contract §5 step-1 response shape.
type preHandshakeResponse struct {
	Salt      string          `json:"salt"`
	KDFParams crypto.KDFParams `json:"kdfParams"`
}

// PreHandshake performs login step 1: fetches the user salt + kdfParams
// so the client can derive the same serverPassword and masterKey that
// were derived at signup.
//
// Errors map to envelope codes the command handler can return verbatim:
//   - 404 → ErrNotFound (no such user)
//   - other → BACKEND_* / SCHEMA_INCOMPATIBLE etc.
func (a *Authenticator) PreHandshake(ctx context.Context, email string) (*preHandshakeResponse, *envelope.Error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	var resp preHandshakeResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      doc.EndstateExtensions.AuthLoginEndpoint,
		Body:     preHandshakeRequest{Email: strings.ToLower(email)},
		ReadOnly: false,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	if !resp.KDFParams.MeetsFloor() {
		return nil, envelope.NewError(envelope.ErrBackendIncompatible,
			fmt.Sprintf("Server-advertised KDF parameters are below the v1 floor (memory=%d iterations=%d parallelism=%d).",
				resp.KDFParams.Memory, resp.KDFParams.Iterations, resp.KDFParams.Parallelism)).
			WithRemediation("This account was created with parameters this engine refuses on principle. Update the engine or contact support.")
	}
	return &resp, nil
}

// completeLoginRequest matches the contract §5 step-2 body shape.
type completeLoginRequest struct {
	Email          string `json:"email"`
	ServerPassword string `json:"serverPassword"`
}

// CompleteLoginResponse matches the contract §5 step-2 response shape.
type CompleteLoginResponse struct {
	UserID             string `json:"userId"`
	AccessToken        string `json:"accessToken"`
	RefreshToken       string `json:"refreshToken"`
	WrappedDEK         string `json:"wrappedDEK"`
	SubscriptionStatus string `json:"subscriptionStatus,omitempty"`
}

// CompleteLogin performs login step 2: authenticates with the
// pre-derived serverPassword and stores the resulting tokens.
//
// On success the session is updated and the refresh token is persisted
// to the keychain. The caller is responsible for unwrapping the DEK with
// the masterKey it derived in step 1.
func (a *Authenticator) CompleteLogin(ctx context.Context, email string, serverPassword []byte) (*CompleteLoginResponse, *envelope.Error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	var resp CompleteLoginResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      doc.EndstateExtensions.AuthLoginEndpoint,
		Body:     completeLoginRequest{Email: strings.ToLower(email), ServerPassword: base64Encode(serverPassword)},
		ReadOnly: false,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	a.session.SetTokens(resp.UserID, strings.ToLower(email), resp.AccessToken, resp.RefreshToken, resp.SubscriptionStatus, time.Time{})
	if perr := a.session.Persist(); perr != nil {
		// Persisting to the keychain failed — the session is still good
		// in-process but won't survive a restart. Surface as INTERNAL_ERROR
		// because we shouldn't pretend everything is fine.
		return &resp, envelope.NewError(envelope.ErrInternalError,
			"login succeeded but refresh token could not be saved to the OS keychain: "+perr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	return &resp, nil
}

// refreshAccessToken implements the client.TokenProvider refresh hook.
// Returns the new access token on success.
func (a *Authenticator) refreshAccessToken(ctx context.Context) (string, error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return "", err
	}
	snap := a.session.Snapshot()
	if snap.RefreshToken == "" {
		return "", errors.New("auth: no refresh token in session")
	}
	type refreshReq struct {
		RefreshToken string `json:"refreshToken"`
	}
	type refreshResp struct {
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	}
	var resp refreshResp
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:          "POST",
		URL:             doc.EndstateExtensions.AuthRefreshEndpoint,
		Body:            refreshReq{RefreshToken: snap.RefreshToken},
		ReadOnly:        false,
		SkipAuthRefresh: true, // never let a 401 here recurse into a fresh refresh attempt
	}, &resp); cerr != nil {
		return "", fmt.Errorf("auth: refresh: %s: %s", cerr.Code, cerr.Message)
	}
	a.session.SetTokens(snap.UserID, snap.Email, resp.AccessToken, resp.RefreshToken, snap.SubscriptionStatus, time.Time{})
	if perr := a.session.Persist(); perr != nil {
		return "", fmt.Errorf("auth: refresh: persist refresh token: %w", perr)
	}
	return resp.AccessToken, nil
}

// Logout invalidates the refresh token server-side (best-effort) and
// clears all local state.
func (a *Authenticator) Logout(ctx context.Context) *envelope.Error {
	snap := a.session.Snapshot()
	if snap.RefreshToken != "" {
		doc, err := a.oidc.Discovery(ctx)
		if err == nil {
			type logoutReq struct {
				RefreshToken string `json:"refreshToken"`
			}
			// Best-effort: ignore errors. The local state is what matters.
			_ = a.httpc.Do(ctx, client.Request{
				Method: "POST",
				URL:    doc.EndstateExtensions.AuthLogoutEndpoint,
				Body:   logoutReq{RefreshToken: snap.RefreshToken},
			}, nil)
		}
	}
	if err := a.session.Forget(); err != nil {
		return envelope.NewError(envelope.ErrInternalError,
			"logout: clear local session: "+err.Error())
	}
	return nil
}

// SignupBody is the contract §5 POST /api/auth/signup body. All
// byte-typed fields are encoded as standard base64.
type SignupBody struct {
	Email                 string           `json:"email"`
	ServerPassword        string           `json:"serverPassword"`
	Salt                  string           `json:"salt"`
	KDFParams             crypto.KDFParams `json:"kdfParams"`
	WrappedDEK            string           `json:"wrappedDEK"`
	RecoveryKeyVerifier   string           `json:"recoveryKeyVerifier"`
	RecoveryKeyWrappedDEK string           `json:"recoveryKeyWrappedDEK"`
}

// Signup performs `POST /api/auth/signup`. On success the session is
// updated with the returned tokens and the refresh token is persisted to
// the keychain. The caller is responsible for caching the unwrapped DEK
// via SessionStore.StoreDEK separately — Signup does not see plaintext
// keys.
func (a *Authenticator) Signup(ctx context.Context, body SignupBody) (*CompleteLoginResponse, *envelope.Error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	body.Email = strings.ToLower(body.Email)
	var resp CompleteLoginResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      doc.EndstateExtensions.AuthSignupEndpoint,
		Body:     body,
		ReadOnly: false,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	a.session.SetTokens(resp.UserID, body.Email, resp.AccessToken, resp.RefreshToken, resp.SubscriptionStatus, time.Time{})
	if perr := a.session.Persist(); perr != nil {
		return &resp, envelope.NewError(envelope.ErrInternalError,
			"signup succeeded but refresh token could not be saved to the OS keychain: "+perr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	return &resp, nil
}

// RecoverBody is the contract §6 POST /api/auth/recover body.
type RecoverBody struct {
	Email             string `json:"email"`
	RecoveryKeyProof  string `json:"recoveryKeyProof"`
}

// RecoverResponse mirrors the substrate response. The wrapped DEK is the
// recoveryKey-wrapped variant; the salt is returned for symmetry — the
// client already has it from PreHandshake but the server may rotate it
// during recovery (contract is silent so we accept both shapes).
type RecoverResponse struct {
	RecoveryKeyWrappedDEK string `json:"recoveryKeyWrappedDEK"`
	Salt                  string `json:"salt,omitempty"`
}

// Recover performs `POST /api/auth/recover` (contract §6 step 3–4).
// On success returns the recoveryKey-wrapped DEK so the caller can
// unwrap it with the recovery key. Tokens are NOT issued at this step;
// `RecoverFinalize` issues fresh tokens after the new passphrase is set.
func (a *Authenticator) Recover(ctx context.Context, body RecoverBody) (*RecoverResponse, *envelope.Error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	body.Email = strings.ToLower(body.Email)
	var resp RecoverResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      doc.EndstateExtensions.AuthRecoverEndpoint,
		Body:     body,
		ReadOnly: false,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	return &resp, nil
}

// RecoverFinalizeBody is the contract §6 POST /api/auth/recover/finalize body.
type RecoverFinalizeBody struct {
	Email                string           `json:"email"`
	ServerPassword       string           `json:"serverPassword"`
	Salt                 string           `json:"salt"`
	KDFParams            crypto.KDFParams `json:"kdfParams"`
	WrappedDEK           string           `json:"wrappedDEK"`
	RecoveryKeyProof     string           `json:"recoveryKeyProof"`
}

// RecoverFinalize performs `POST /api/auth/recover/finalize` (contract §6
// step 7–8). The server updates the password hash and the wrappedDEK in
// a single transaction and returns fresh tokens. On success the session
// is updated and the refresh token is persisted; the caller is
// responsible for caching the new DEK via SessionStore.StoreDEK.
func (a *Authenticator) RecoverFinalize(ctx context.Context, body RecoverFinalizeBody) (*CompleteLoginResponse, *envelope.Error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	body.Email = strings.ToLower(body.Email)
	var resp CompleteLoginResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      strings.TrimRight(doc.EndstateExtensions.AuthRecoverEndpoint, "/") + "/finalize",
		Body:     body,
		ReadOnly: false,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	a.session.SetTokens(resp.UserID, body.Email, resp.AccessToken, resp.RefreshToken, resp.SubscriptionStatus, time.Time{})
	if perr := a.session.Persist(); perr != nil {
		return &resp, envelope.NewError(envelope.ErrInternalError,
			"recovery finalize succeeded but refresh token could not be saved to the OS keychain: "+perr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	return &resp, nil
}

// MeResponse matches the GET /api/account/me payload (contract §7).
type MeResponse struct {
	UserID             string `json:"userId"`
	Email              string `json:"email"`
	SubscriptionStatus string `json:"subscriptionStatus"`
	CreatedAt          string `json:"createdAt"`
}

// Me fetches the current user's profile from the backend. Used by the
// `backup status` command to report subscription state.
func (a *Authenticator) Me(ctx context.Context) (*MeResponse, *envelope.Error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	// /api/account/me lives off the same base as auth endpoints; substrate
	// places it at <issuer>/api/account/me. We compute the URL from the
	// issuer rather than the backup_api_base so a self-hoster can place
	// account endpoints anywhere they like via the discovery document.
	url := strings.TrimRight(a.issuer.URL, "/") + "/api/account/me"
	var resp MeResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:   "GET",
		URL:      url,
		ReadOnly: true,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	// Update the cached subscription hint.
	snap := a.session.Snapshot()
	a.session.SetTokens(snap.UserID, resp.Email, snap.AccessToken, snap.RefreshToken, resp.SubscriptionStatus, snap.AccessExpiry)
	_ = doc // referenced for symmetry with future endpoints; retained to keep callers in lockstep with Discovery.
	return &resp, nil
}

// mapDiscoveryError converts an oidc package error to the envelope code
// the command handler should return.
func mapDiscoveryError(err error) *envelope.Error {
	if errors.Is(err, oidc.ErrIncompatibleIssuer) {
		return envelope.NewError(envelope.ErrBackendIncompatible,
			"The configured backend does not advertise the required `endstate_extensions` block.").
			WithRemediation("Verify ENDSTATE_OIDC_ISSUER_URL points at a substrate-compatible backend.")
	}
	return envelope.NewError(envelope.ErrBackendUnreachable,
		"Could not fetch OIDC discovery: "+err.Error()).
		WithRemediation("Check your network connection or override ENDSTATE_OIDC_ISSUER_URL.")
}

// base64Encode is a tiny helper that keeps the call sites readable.
func base64Encode(b []byte) string {
	return stdBase64.EncodeToString(b)
}
