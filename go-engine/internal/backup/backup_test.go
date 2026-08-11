// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package backup

import "testing"

func TestIssuerURL_NormalizesManagedAndSelfHostedTrailingSlash(t *testing.T) {
	t.Setenv("ENDSTATE_OIDC_ISSUER_URL", "HTTPS://SUBSTRATESYSTEMS.IO/")
	if got := IssuerURL(); got != "https://substratesystems.io" {
		t.Fatalf("managed issuer = %q", got)
	}
	t.Setenv("ENDSTATE_OIDC_ISSUER_URL", "https://cloud.example.test/oidc/")
	if got := IssuerURL(); got != "https://cloud.example.test/oidc" {
		t.Fatalf("self-hosted issuer = %q", got)
	}
}
