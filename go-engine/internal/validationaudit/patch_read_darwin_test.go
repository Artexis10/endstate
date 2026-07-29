// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build darwin

package validationaudit

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSameDarwinMount(t *testing.T) {
	base := syscall.Statfs_t{}
	base.Fsid.Val[0] = 1
	base.Mntonname[0] = 'r'
	base.Mntfromname[0] = 'd'
	if !sameDarwinMount(base, base) {
		t.Fatal("sameDarwinMount() rejected identical mount")
	}
	otherFSID := base
	otherFSID.Fsid.Val[1] = 2
	if sameDarwinMount(base, otherFSID) {
		t.Fatal("sameDarwinMount() accepted foreign filesystem")
	}
	otherMount := base
	otherMount.Mntonname[1] = 'x'
	if sameDarwinMount(base, otherMount) {
		t.Fatal("sameDarwinMount() accepted foreign mount identity")
	}
}

func TestLoadCandidatePatchRejectsDarwinIntermediateSymlink(t *testing.T) {
	root, candidate, raw := writeCandidatePatch(t, patchFor("go-engine/internal/planner/plan.go"))
	patches := filepath.Join(root, "validation", "ci-efficacy", "v1", "patches")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "candidate.patch"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(patches); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, patches); err != nil {
		t.Skipf("symbolic links unavailable: %v", err)
	}
	if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrUnsafePatchPath) {
		t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrUnsafePatchPath)
	}
}
