// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
)

// writeVerifyManifest writes a one-app manifest declaring a windows ref and an
// optional version, and returns its path. Keyed by "windows" so the driver
// (winget) verify path resolves it under withMockDriver on any host.
func writeVerifyManifest(t *testing.T, version string) string {
	t.Helper()
	ver := ""
	if version != "" {
		ver = `"version": "` + version + `", `
	}
	content := `{
		"name": "drift-test",
		"apps": [ { "id": "a", ` + ver + `"refs": { "windows": "Vendor.A" } } ]
	}`
	p := filepath.Join(t.TempDir(), "m.jsonc")
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func verifyItemForID(t *testing.T, vr *VerifyResult, id string) VerifyItem {
	t.Helper()
	for _, it := range vr.Results {
		if it.ID == id {
			return it
		}
	}
	t.Fatalf("no verify item for id %q in %+v", id, vr.Results)
	return VerifyItem{}
}

// Declared version differs from installed → fail/version_drift with both
// versions surfaced.
func TestRunVerify_VersionDrift_Fails(t *testing.T) {
	md := &mockDriver{
		installed: map[string]bool{"Vendor.A": true},
		versions:  map[string]string{"Vendor.A": "1.1.0"},
	}
	mPath := writeVerifyManifest(t, "1.2.0")

	var vr *VerifyResult
	withMockDriver(md, func() {
		r, _ := RunVerify(VerifyFlags{Manifest: mPath})
		vr = r.(*VerifyResult)
	})

	it := verifyItemForID(t, vr, "a")
	if it.Status != "fail" || it.Reason != driver.ReasonVersionDrift {
		t.Fatalf("status/reason = %q/%q, want fail/version_drift", it.Status, it.Reason)
	}
	if it.Version != "1.1.0" || it.Expected != "1.2.0" {
		t.Fatalf("version/expected = %q/%q, want 1.1.0/1.2.0", it.Version, it.Expected)
	}
	if vr.Summary.Fail != 1 {
		t.Fatalf("Summary.Fail = %d, want 1", vr.Summary.Fail)
	}
}

// Declared version matches installed → pass, version surfaced.
func TestRunVerify_VersionMatch_Passes(t *testing.T) {
	md := &mockDriver{
		installed: map[string]bool{"Vendor.A": true},
		versions:  map[string]string{"Vendor.A": "1.2.0"},
	}
	mPath := writeVerifyManifest(t, "1.2.0")

	var vr *VerifyResult
	withMockDriver(md, func() {
		r, _ := RunVerify(VerifyFlags{Manifest: mPath})
		vr = r.(*VerifyResult)
	})

	it := verifyItemForID(t, vr, "a")
	if it.Status != "pass" {
		t.Fatalf("status = %q, want pass (versions equal)", it.Status)
	}
	if it.Version != "1.2.0" {
		t.Fatalf("version = %q, want 1.2.0 surfaced on pass", it.Version)
	}
}

// No declared version → no drift check (installed = pass regardless of version).
func TestRunVerify_NoDeclaredVersion_NoDriftCheck(t *testing.T) {
	md := &mockDriver{
		installed: map[string]bool{"Vendor.A": true},
		versions:  map[string]string{"Vendor.A": "9.9.9"},
	}
	mPath := writeVerifyManifest(t, "") // no version declared

	var vr *VerifyResult
	withMockDriver(md, func() {
		r, _ := RunVerify(VerifyFlags{Manifest: mPath})
		vr = r.(*VerifyResult)
	})

	if it := verifyItemForID(t, vr, "a"); it.Status != "pass" || it.Reason != "" {
		t.Fatalf("status/reason = %q/%q, want pass/<empty> (no declared version)", it.Status, it.Reason)
	}
}

// Declared version but the backend exposes no installed version → no false
// drift (best-effort capture).
func TestRunVerify_UnknownInstalledVersion_NoFalseDrift(t *testing.T) {
	md := &mockDriver{
		installed: map[string]bool{"Vendor.A": true},
		// no versions entry → DetectBatch reports Version ""
	}
	mPath := writeVerifyManifest(t, "1.2.0")

	var vr *VerifyResult
	withMockDriver(md, func() {
		r, _ := RunVerify(VerifyFlags{Manifest: mPath})
		vr = r.(*VerifyResult)
	})

	if it := verifyItemForID(t, vr, "a"); it.Status != "pass" {
		t.Fatalf("status = %q, want pass (unknown installed version must not flag drift)", it.Status)
	}
}
