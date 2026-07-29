// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

func TestV1CommandsRejectMissingRequiredFlags(t *testing.T) {
	for _, args := range [][]string{{"validate-v1"}, {"run-v1-lane"}, {"aggregate-v1"}} {
		if code := run(args); code != 2 {
			t.Errorf("run(%q) = %d, want 2", args, code)
		}
	}
}
