// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package redirect holds the one cross-origin redirect policy shared by the
// hosted-backup HTTP clients.
package redirect

import (
	"net/http"
	"net/url"
	"strings"
)

// BlockCrossOrigin returns an http.Client CheckRedirect that prevents a
// self-hosted endpoint from replaying a request body or bearer credential to
// another origin, returning blocked for any redirect whose origin (scheme,
// host, effective port) differs from the original request. Same-origin
// redirects remain supported for ordinary endpoint routing.
func BlockCrossOrigin(blocked error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 || sameOrigin(req.URL, via[0].URL) {
			return nil
		}
		return blocked
	}
}

func sameOrigin(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) &&
		effectivePort(a) == effectivePort(b)
}

func effectivePort(u *url.URL) string {
	if port := u.Port(); port != "" {
		return port
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}
