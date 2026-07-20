// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// ---------------------------------------------------------------------------
// --share validation
// ---------------------------------------------------------------------------

func TestValidateShareFlags(t *testing.T) {
	tests := []struct {
		name    string
		flags   CaptureFlags
		wantErr bool
		because string
	}{
		{
			name:  "share off is always fine",
			flags: CaptureFlags{},
		},
		{
			name:  "share with a selection is accepted",
			flags: CaptureFlags{Share: true, Only: "git-git"},
		},
		{
			name:    "share without a selection is rejected",
			flags:   CaptureFlags{Share: true},
			wantErr: true,
			because: "an unscoped share attaches every matched module's config, the opposite of a curated setup",
		},
		{
			name:    "share with a blank selection is rejected",
			flags:   CaptureFlags{Share: true, Only: "   "},
			wantErr: true,
			because: "whitespace is not a selection",
		},
		{
			name:    "share with sanitize is rejected",
			flags:   CaptureFlags{Share: true, Only: "git-git", Sanitize: true},
			wantErr: true,
			because: "--sanitize attaches no config, leaving nothing to share",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateShareFlags(tt.flags)
			if tt.wantErr && err == nil {
				t.Fatalf("expected rejection: %s", tt.because)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected rejection: %v", err)
			}
			if tt.wantErr && err.Code != envelope.ErrManifestValidationError {
				t.Errorf("code = %q, want MANIFEST_VALIDATION_ERROR", err.Code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Cross-OS refusal
// ---------------------------------------------------------------------------

// TestRefuseCrossOSBundle covers the boundary decided during design: a bundle
// from another OS transfers almost nothing, because the catalog carries no
// non-Windows package identity and module paths are Windows-shaped. Refusing is
// more honest than a report whose every skip reads "wrong OS".
func TestRefuseCrossOSBundle(t *testing.T) {
	tests := []struct {
		name     string
		bundleOS string
		hostOS   string
		wantErr  bool
	}{
		{name: "same OS proceeds", bundleOS: "windows", hostOS: "windows"},
		{name: "cross OS refused", bundleOS: "windows", hostOS: "darwin", wantErr: true},
		{name: "cross OS refused the other way", bundleOS: "darwin", hostOS: "windows", wantErr: true},
		{
			name:     "bundle predating the field is accepted",
			bundleOS: "",
			hostOS:   "windows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := refuseCrossOSBundle(tt.bundleOS, tt.hostOS)
			if tt.wantErr && err == nil {
				t.Fatal("expected a refusal")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected refusal: %v", err)
			}
			if tt.wantErr {
				if err.Code != envelope.ErrNotSupported {
					t.Errorf("code = %q, want NOT_SUPPORTED", err.Code)
				}
				// The message must name both sides; "not supported" alone leaves the
				// user guessing which machine is wrong.
				if err.Message == "" || err.Remediation == "" {
					t.Error("refusal must explain and remediate")
				}
			}
		})
	}
}
