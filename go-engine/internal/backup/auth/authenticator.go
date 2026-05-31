// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package auth

import (
	"context"
	stdBase64Pkg "encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/golang-jwt/jwt/v5"

	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/backup/crypto"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
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
	issuer      Issuer
	oidc        *oidc.Client
	httpc       *client.Client
	session     *SessionStore
	refreshLock string // absolute path to the cross-process refresh lock file (F5)
}

// NewAuthenticator constructs an Authenticator. The supplied client.Client
// MUST be configured with a TokenProvider that defers to session — see
// auth.MakeTokenProvider helper.
func NewAuthenticator(issuer Issuer, o *oidc.Client, c *client.Client, s *SessionStore) *Authenticator {
	a := &Authenticator{issuer: issuer, oidc: o, httpc: c, session: s}
	a.refreshLock = defaultRefreshLockPath()
	// Wire the refresh callback so the HTTP client's 401-refresh hook
	// reaches into our /api/auth/refresh handler instead of looping.
	s.WithRefreshFn(a.refreshAccessToken)
	return a
}

// WithRefreshLockDir overrides the directory used for the cross-process
// refresh lock file (F5). The lock file is created at `<dir>/refresh.lock`.
// Primarily a test seam — production callers should leave this at the
// default (per-user config dir) so all subprocesses for the same user
// converge on the same lock. Returns the receiver for chaining.
func (a *Authenticator) WithRefreshLockDir(dir string) *Authenticator {
	if dir == "" {
		return a
	}
	a.refreshLock = filepath.Join(dir, "refresh.lock")
	return a
}

// refreshLockPath returns the absolute path to the cross-process refresh
// lock file. Falls back to a process-scoped temp path if construction
// fails (which degrades to per-process serialization — strictly worse
// than cross-process but still safe).
func (a *Authenticator) refreshLockPath() string {
	if a.refreshLock != "" {
		return a.refreshLock
	}
	return defaultRefreshLockPath()
}

// defaultRefreshLockPath returns the absolute path to the canonical
// refresh lock file under the per-user OS config dir
// (`%APPDATA%/Endstate/refresh.lock` on Windows, `~/.config/endstate/refresh.lock`
// on Unix). Ensures the parent directory exists. Falls back to the OS
// temp dir on environments where UserConfigDir fails (CI sandboxes, etc).
func defaultRefreshLockPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "Endstate")
	// Best-effort: if MkdirAll fails the flock TryLock below will surface
	// the underlying error and the caller treats it as a transient
	// refresh failure (the existing 401-refresh path retries on next call).
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "refresh.lock")
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
	Salt      string           `json:"salt"`
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
//
// Email is populated only by `/api/auth/claim` — substrate has the
// canonical email from Paddle and returns it so the engine can display
// the same identity the buyer purchased under. Other endpoints
// (signup, login, recover-finalize, refresh) leave it empty.
type CompleteLoginResponse struct {
	UserID             string `json:"userId"`
	Email              string `json:"email,omitempty"`
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
	a.session.SetTokens(resp.UserID, strings.ToLower(email), resp.AccessToken, resp.RefreshToken, resp.SubscriptionStatus, parseAccessExpiry(resp.AccessToken))
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
//
// F5: serialized cross-process via a file lock at refreshLockPath().
// Sliding-window refresh-token rotation (contract §5.3) means two
// concurrent processes that read the same persisted RT and both POST it
// will race — substrate burns the RT after the first call and the
// second persists a now-stale RT to the keychain, surfacing as
// AUTH_REQUIRED on the next subprocess. The lock collapses the
// read-rotate-persist window into a single critical section across
// every process for the user.
//
// After acquiring the lock, we re-read the session snapshot: another
// process may have completed a refresh while we were waiting, in which
// case the in-memory access token is now fresh and we return it
// without hitting substrate. This is the optimisation that keeps a
// burst of N concurrent subprocesses to 1 substrate round-trip rather
// than N serial ones.
func (a *Authenticator) refreshAccessToken(ctx context.Context) (string, error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return "", err
	}

	// Acquire the cross-process refresh lock before reading the RT. The
	// 50ms poll is generous enough not to thrash; the wait is bounded by
	// ctx so cancellation propagates.
	lock := flock.New(a.refreshLockPath())
	locked, lerr := lock.TryLockContext(ctx, 50*time.Millisecond)
	if lerr != nil {
		return "", fmt.Errorf("auth: refresh: acquire lock %q: %w", a.refreshLockPath(), lerr)
	}
	if !locked {
		// ctx expired before we could acquire — propagate as a transient
		// error. The 401-refresh hook surfaces this to the user as a
		// retryable failure rather than burning the cached RT.
		if cerr := ctx.Err(); cerr != nil {
			return "", fmt.Errorf("auth: refresh: %w", cerr)
		}
		return "", errors.New("auth: refresh: could not acquire refresh lock")
	}
	defer func() { _ = lock.Unlock() }()

	// Re-hydrate from the keychain so a sibling process's rotated RT
	// becomes visible to this process. Without this, the in-memory snapshot
	// here is the pre-wait copy and we'd POST a now-stale RT, defeating the
	// lock entirely.
	//
	// ErrNotFound at the pointer is the expected signed-out shape and falls
	// through to the original snapshot (no-op for fresh test keychains).
	// Any other error means we cannot trust the in-memory snapshot to match
	// what's in the keychain — proceeding would risk POSTing a stale RT,
	// which substrate's reuse-detection would burn, re-introducing exactly
	// the AUTH_REQUIRED symptom F5 is meant to eliminate. Fail closed.
	if uid := a.session.Snapshot().UserID; uid != "" {
		if err := a.session.Hydrate(uid); err != nil && !errors.Is(err, keychain.ErrNotFound) {
			return "", fmt.Errorf("auth: refresh: hydrate before rotation: %w", err)
		}
	}
	snap := a.session.Snapshot()

	// Second-waiter optimisation: if a sibling process already rotated
	// the access token while we were waiting, the AccessToken() check
	// will see a still-valid cached token and short-circuit. This drops
	// N-way concurrent refreshes to a single substrate round-trip.
	if cached, _ := a.session.AccessToken(ctx); cached != "" {
		return cached, nil
	}

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
	// Contract guardrail (hosted-backup-contract.md §5.3 line 205-207): the
	// refresh response must carry both tokens. Sliding-window rotation
	// means the refresh token we just sent is now invalid server-side;
	// accepting a response without a fresh refreshToken would leave the
	// stale-and-invalid one in the keychain to fail on the next subprocess.
	if resp.AccessToken == "" {
		return "", errors.New("auth: refresh: substrate response missing accessToken")
	}
	if resp.RefreshToken == "" {
		return "", errors.New("auth: refresh: substrate response missing refreshToken (sliding-window rotation requires a new RT; keychain RT is now stale)")
	}
	a.session.SetTokens(snap.UserID, snap.Email, resp.AccessToken, resp.RefreshToken, snap.SubscriptionStatus, parseAccessExpiry(resp.AccessToken))
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
	a.session.SetTokens(resp.UserID, body.Email, resp.AccessToken, resp.RefreshToken, resp.SubscriptionStatus, parseAccessExpiry(resp.AccessToken))
	if perr := a.session.Persist(); perr != nil {
		return &resp, envelope.NewError(envelope.ErrInternalError,
			"signup succeeded but refresh token could not be saved to the OS keychain: "+perr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	return &resp, nil
}

// ClaimBody is the POST /api/auth/claim request body. Structurally
// identical to SignupBody minus Email — substrate identifies the user
// from the claim-token row (keyed by the bearer token), so the email
// on the wire would be ignored anyway and surfacing one in the GUI
// would invite identity-mismatch confusion. The credential block is
// byte-identical to signup's; the KDF floor check is enforced by
// substrate on the receiving side.
type ClaimBody struct {
	ServerPassword        string           `json:"serverPassword"`
	Salt                  string           `json:"salt"`
	KDFParams             crypto.KDFParams `json:"kdfParams"`
	WrappedDEK            string           `json:"wrappedDEK"`
	RecoveryKeyVerifier   string           `json:"recoveryKeyVerifier"`
	RecoveryKeyWrappedDEK string           `json:"recoveryKeyWrappedDEK"`
}

// Claim performs `POST /api/auth/claim` with `Authorization: Bearer
// <claimToken>`. Mirrors Signup's success path: on 200 the session is
// updated with the returned tokens (email is server-supplied) and the
// refresh token is persisted to the keychain. The caller is
// responsible for caching the unwrapped DEK via SessionStore.StoreDEK
// separately — Claim does not see plaintext keys.
//
// SkipAuthRefresh is required: the bearer IS the claim token, not an
// access token. A 401 here means the claim token is invalid, expired,
// or already consumed; recursing into the refresh hook would loop
// without progress AND risk clobbering the intended substrate
// identity.
//
// URL: prefer discovery's `auth_claim_endpoint` when non-empty; fall
// back to `<issuer>/api/auth/claim` otherwise (substrate v1 does not
// advertise the discovery field — see `oidc.EndstateExtensions`).
func (a *Authenticator) Claim(ctx context.Context, claimToken string, body ClaimBody) (*CompleteLoginResponse, *envelope.Error) {
	if strings.TrimSpace(claimToken) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"claim: empty claimToken — caller must pass the token from the buyer's claim link")
	}
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	url := doc.EndstateExtensions.AuthClaimEndpoint
	if url == "" {
		url = strings.TrimRight(a.issuer.URL, "/") + "/api/auth/claim"
	}
	var resp CompleteLoginResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:          "POST",
		URL:             url,
		Body:            body,
		Headers:         http.Header{"Authorization": []string{"Bearer " + claimToken}},
		ReadOnly:        false,
		SkipAuthRefresh: true,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	a.session.SetTokens(resp.UserID, strings.ToLower(resp.Email), resp.AccessToken, resp.RefreshToken, resp.SubscriptionStatus, parseAccessExpiry(resp.AccessToken))
	if perr := a.session.Persist(); perr != nil {
		return &resp, envelope.NewError(envelope.ErrInternalError,
			"claim succeeded but refresh token could not be saved to the OS keychain: "+perr.Error()).
			WithRemediation("Re-run `endstate backup login` after addressing the keychain access issue.")
	}
	return &resp, nil
}

// RecoverBody is the contract §6 POST /api/auth/recover body.
type RecoverBody struct {
	Email            string `json:"email"`
	RecoveryKeyProof string `json:"recoveryKeyProof"`
}

// RecoverResponse mirrors the substrate response per contract §6 v2.0.
// The recoveryToken is the bearer credential for the subsequent
// /finalize call; ttlSeconds is advisory — the server is authoritative
// for expiry — but lets the GUI surface a "you have N minutes" hint.
type RecoverResponse struct {
	RecoveryToken         string `json:"recoveryToken"`
	RecoveryKeyWrappedDEK string `json:"recoveryKeyWrappedDEK"`
	TTLSeconds            int    `json:"ttlSeconds"`
}

// Recover performs `POST /api/auth/recover` (contract §6 step 3–4).
// On success returns the recoveryToken to bear on the finalize call,
// along with the recoveryKey-wrapped DEK so the caller can unwrap it.
// Tokens are NOT issued at this step; `RecoverFinalize` issues fresh
// access/refresh tokens after the new passphrase is set.
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

// RecoverFinalizeBody is the contract §6 v2.0 POST /api/auth/recover/finalize
// body. The recoveryToken is NOT in the body — it is carried as
// `Authorization: Bearer <recoveryToken>`. Substrate identifies the
// user from the bearer's `sub` claim, so `email` is no longer needed.
type RecoverFinalizeBody struct {
	NewServerPassword string           `json:"newServerPassword"`
	NewSalt           string           `json:"newSalt"`
	NewKDFParams      crypto.KDFParams `json:"newKdfParams"`
	NewWrappedDEK     string           `json:"newWrappedDEK"`
}

// RecoverFinalize performs `POST /api/auth/recover/finalize` (contract §6
// v2.0 step 7–8). The recoveryToken is bearer-borne. The server burns
// the token on success; replays return RECOVERY_TOKEN_EXPIRED.
//
// `email` is supplied by the caller (it is not echoed in the substrate
// response) and is stored in the session for display only — userID is
// the authoritative key for keychain entries.
//
// `SkipAuthRefresh: true` is required: there is no access token in play
// here, only the recovery bearer. A 401 means the recoveryToken expired
// or was already consumed; refresh-then-retry would loop without progress.
func (a *Authenticator) RecoverFinalize(ctx context.Context, recoveryToken, email string, body RecoverFinalizeBody) (*CompleteLoginResponse, *envelope.Error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	if strings.TrimSpace(recoveryToken) == "" {
		return nil, envelope.NewError(envelope.ErrInternalError,
			"recover/finalize: empty recoveryToken — caller must pass the token from the prior /recover step")
	}
	var resp CompleteLoginResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:          "POST",
		URL:             strings.TrimRight(doc.EndstateExtensions.AuthRecoverEndpoint, "/") + "/finalize",
		Body:            body,
		Headers:         http.Header{"Authorization": []string{"Bearer " + recoveryToken}},
		ReadOnly:        false,
		SkipAuthRefresh: true,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	a.session.SetTokens(resp.UserID, strings.ToLower(email), resp.AccessToken, resp.RefreshToken, resp.SubscriptionStatus, parseAccessExpiry(resp.AccessToken))
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
	// Backup freshness + quota (issue #59). Populated by substrate; decode to
	// zero values against an older substrate that doesn't send them yet.
	LastBackupAt    string `json:"lastBackupAt"`
	QuotaUsedBytes  int64  `json:"quotaUsedBytes"`
	QuotaTotalBytes int64  `json:"quotaTotalBytes"`
	VersionCount    int    `json:"versionCount"`
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

// CheckoutResponse matches substrate's POST /api/billing/checkout payload.
// The checkoutUrl is the substrate landing that renders the Paddle _ptxn
// overlay; transactionId is Paddle's transaction handle, surfaced so the
// GUI can correlate the opened checkout with webhook-driven state changes.
type CheckoutResponse struct {
	CheckoutURL   string `json:"checkoutUrl"`
	TransactionID string `json:"transactionId"`
}

// Subscribe initiates a Hosted Backup subscription checkout. It POSTs to
// <issuer>/api/billing/checkout with no body — substrate resolves the
// €4/mo price server-side — using the session's persisted access token.
//
// Like Me(), the checkout URL is computed from the issuer rather than a
// discovery field: billing lives off the issuer host (contract §7/§9), and
// running Discovery first keeps the issuer-mismatch guardrail in the path.
//
// The engine returns the URL; it does not open a browser. The GUI opens
// CheckoutURL in the system browser.
func (a *Authenticator) Subscribe(ctx context.Context) (*CheckoutResponse, *envelope.Error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	url := strings.TrimRight(a.issuer.URL, "/") + "/api/billing/checkout"
	var resp CheckoutResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      url,
		ReadOnly: false,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	_ = doc // referenced for symmetry with Me(); retained to keep callers in lockstep with Discovery.
	return &resp, nil
}

// BrowserSessionResponse matches substrate's POST /api/auth/browser-session
// payload (contract §5). SessionToken is a 60s EdDSA JWT (aud=endstate-account);
// AccountUrl is the substrate-advertised portal landing (defaults to
// `${issuer}/account/start`). The GUI opens `${AccountUrl}?session=${SessionToken}`
// in the system browser, where substrate's start route swaps the JWT for an
// HttpOnly cookie and redirects to the cookie-only /account page.
type BrowserSessionResponse struct {
	SessionToken string `json:"sessionToken"`
	AccountURL   string `json:"accountUrl"`
}

// BrowserSession mints a short-lived handoff token for the GUI to open the
// substrate /account portal in the system browser. POSTs to
// <issuer>/api/auth/browser-session with no body, using the session's
// persisted access token. Mirrors Subscribe()'s shape; the token is single-
// use (substrate burns the jti at redeem).
//
// The engine returns the URL + token; it does not open a browser. The GUI
// composes `${AccountURL}?session=${SessionToken}` and opens it externally.
func (a *Authenticator) BrowserSession(ctx context.Context) (*BrowserSessionResponse, *envelope.Error) {
	doc, err := a.oidc.Discovery(ctx)
	if err != nil {
		return nil, mapDiscoveryError(err)
	}
	url := strings.TrimRight(a.issuer.URL, "/") + "/api/auth/browser-session"
	var resp BrowserSessionResponse
	if cerr := a.httpc.Do(ctx, client.Request{
		Method:   "POST",
		URL:      url,
		ReadOnly: false,
	}, &resp); cerr != nil {
		return nil, cerr
	}
	_ = doc // symmetric with Subscribe()/Me(); keeps the issuer-mismatch guardrail in the path.
	return &resp, nil
}

// mapDiscoveryError converts an oidc package error to the envelope code
// the command handler should return.
func mapDiscoveryError(err error) *envelope.Error {
	if errors.Is(err, oidc.ErrIssuerMismatch) {
		return envelope.NewError(envelope.ErrBackendIncompatible,
			"Backend's discovery document advertises a different issuer URL than ENDSTATE_OIDC_ISSUER_URL is configured for. Both engine and substrate must agree on the issuer URL: "+err.Error()).
			WithRemediation("Set ENDSTATE_OIDC_ISSUER_URL to the same value on both sides, or check that your substrate deployment has it set in its server-side env.")
	}
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

// parseAccessExpiry extracts the `exp` claim from a substrate-issued
// access token without verifying the signature. We trust the token
// substrate just handed us over TLS-validated HTTPS; we only need the
// exp to decide when the cached value is no longer safe to send.
//
// Returns time.Time{} for unparseable input (e.g. tests that pass
// opaque strings like "access-1" or a future substrate that issues
// non-JWT access tokens). A zero return is the documented
// "expiry unknown" signal and means SessionStore won't persist the
// token to the keychain — the pre-F4 best-effort behavior continues.
func parseAccessExpiry(token string) time.Time {
	if token == "" {
		return time.Time{}
	}
	parser := jwt.NewParser()
	tok, _, err := parser.ParseUnverified(token, &Claims{})
	if err != nil {
		return time.Time{}
	}
	claims, ok := tok.Claims.(*Claims)
	if !ok || claims.ExpiresAt == nil {
		return time.Time{}
	}
	return claims.ExpiresAt.Time
}
