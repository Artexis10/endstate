// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package configrestore

import "testing"

func TestValidateInspectionPathNoLinksAcceptsDarwinVarAlias(t *testing.T) {
	if err := validateInspectionPathNoLinks(storeInspectionFilesystem, "/var"); err != nil {
		t.Fatalf("validateInspectionPathNoLinks(\"/var\") error = %v", err)
	}
}
