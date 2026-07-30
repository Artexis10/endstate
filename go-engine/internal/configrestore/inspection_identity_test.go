// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configrestore

import "testing"

func TestInspectionIdentityUsesTheClosedInspectionDigest(t *testing.T) {
	if got, want := InspectionIdentity("target-1"), "sha256:75a34976ea1b88daa7ba0c80731fc1dbf0d7a3d4c63e7a255764facd1c7d0f57"; got != want {
		t.Fatalf("InspectionIdentity() = %q, want %q", got, want)
	}
}
