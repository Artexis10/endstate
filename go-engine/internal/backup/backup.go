// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package backup wires the hosted-backup component stack from environment
// configuration. Command handlers call NewAuthenticator() to obtain a
// fully configured authenticator without re-doing the env-var plumbing.
//
// Two env vars are read here, both per docs/contracts/hosted-backup-contract.md
// §9:
//
//   - ENDSTATE_OIDC_ISSUER_URL — defaults to https://substratesystems.io
//   - ENDSTATE_OIDC_AUDIENCE  — defaults to "endstate-backup"
//
// A third implementation-detail var is also honoured here:
//
//   - ENDSTATE_BACKUP_CONCURRENCY — worker pool size for upload/download,
//     clamped to [1,16], default 4. Read by the upload/download
//     subpackages via Concurrency().
package backup

import (
	"os"
	"strconv"

	"github.com/Artexis10/endstate/go-engine/internal/backup/auth"
	"github.com/Artexis10/endstate/go-engine/internal/backup/client"
	"github.com/Artexis10/endstate/go-engine/internal/backup/keychain"
	"github.com/Artexis10/endstate/go-engine/internal/backup/oidc"
)

// IssuerURL returns the configured OIDC issuer URL with no trailing slash.
func IssuerURL() string { return envOrDefault("ENDSTATE_OIDC_ISSUER_URL", oidc.DefaultIssuerURL) }

// Audience returns the configured JWT audience claim.
func Audience() string { return envOrDefault("ENDSTATE_OIDC_AUDIENCE", oidc.DefaultAudience) }

// Concurrency returns the upload/download worker count, clamped to [1,16].
func Concurrency() int {
	v := os.Getenv("ENDSTATE_BACKUP_CONCURRENCY")
	if v == "" {
		return 4
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 4
	}
	if n > 16 {
		return 16
	}
	return n
}

// Stack groups every component a hosted-backup command handler needs.
// Components share the same SessionStore + OIDC + HTTP client so a
// refresh that happens during one call is visible to the next.
//
// The storage client is added by `add-backup-storage-client`.
type Stack struct {
	Auth    *auth.Authenticator
	Issuer  string
	OIDC    *oidc.Client
	HTTP    *client.Client
	Session *auth.SessionStore
}

// NewStack builds the full hosted-backup component stack using the
// platform-native keychain. Each invocation returns a fresh stack with
// an empty in-memory session; callers Hydrate from the keychain when
// they know the userID they are acting as.
func NewStack() *Stack {
	return newStack(keychain.NewSystem())
}

// NewStackForTest is a test seam: substitutes the keychain (typically
// keychain.NewMemory()).
func NewStackForTest(kc keychain.Keychain) *Stack {
	return newStack(kc)
}

func newStack(kc keychain.Keychain) *Stack {
	issuer := IssuerURL()
	audience := Audience()
	store := auth.NewSessionStore(kc)
	oc := oidc.NewClient(issuer, nil)
	hc := client.New(client.Options{Tokens: store})
	a := auth.NewAuthenticator(auth.Issuer{URL: issuer, Audience: audience}, oc, hc, store)
	return &Stack{
		Auth:    a,
		Issuer:  issuer,
		OIDC:    oc,
		HTTP:    hc,
		Session: store,
	}
}

// NewAuthenticator is retained as the convenience constructor existing
// code uses; equivalent to `NewStack().Auth`.
func NewAuthenticator() *auth.Authenticator {
	return NewStack().Auth
}

func envOrDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
