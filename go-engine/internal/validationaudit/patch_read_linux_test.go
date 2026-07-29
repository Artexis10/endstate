// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package validationaudit

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestLoadCandidatePatchRejectsLinuxBindMount(t *testing.T) {
	root, candidate, raw := writeCandidatePatch(t, patchFor("go-engine/internal/planner/plan.go"))
	mounted := filepath.Join(root, "validation", "ci-efficacy", "v1", "patches")
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "candidate.patch"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mount(outside, mounted, "", syscall.MS_BIND, ""); err != nil {
		t.Skipf("bind mounts unavailable: %v", err)
	}
	defer syscall.Unmount(mounted, 0)
	if _, err := LoadCandidatePatch(root, "v1", candidate); !errors.Is(err, ErrUnsafePatchPath) {
		t.Fatalf("LoadCandidatePatch() error = %v, want %v", err, ErrUnsafePatchPath)
	}
}
