// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package safepath

import (
	"path/filepath"
	"testing"
)

func TestCanonicalizePlatformRootAliasResolvesDarwinVar(t *testing.T) {
	got, err := CanonicalizePlatformRootAlias("/var/folders/endstate")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("/private/var", "folders", "endstate")
	if got != want {
		t.Fatalf("canonical path = %q, want %q", got, want)
	}
}
