// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package registryfile_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/registryfile"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

func TestRewriteSubtreeOutputSatisfiesRegistryImportScopeContract(t *testing.T) {
	physical := `HKCU\Software\Endstate\Validation\contract-nonce\Software\Vendor`
	semantic := `HKCU\Software\Vendor`
	input := []byte("Windows Registry Editor Version 5.00\r\n\r\n" +
		`[HKEY_CURRENT_USER\Software\Endstate\Validation\contract-nonce\Software\Vendor]` + "\r\n" +
		`"Root"="ok"` + "\r\n\r\n" +
		`[HKEY_CURRENT_USER\Software\Endstate\Validation\contract-nonce\Software\Vendor\Child]` + "\r\n" +
		`"Number"=dword:0000002a` + "\r\n")

	rewritten, err := registryfile.RewriteSubtree(input, physical, semantic)
	if err != nil {
		t.Fatalf("RewriteSubtree: %v", err)
	}
	path := filepath.Join(t.TempDir(), "settings.reg")
	if err := os.WriteFile(path, rewritten, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := restore.ValidateRegistryImportScope(path, semantic); err != nil {
		t.Fatalf("published registry document violates import scope contract: %v", err)
	}
}
