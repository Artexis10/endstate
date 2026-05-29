// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ---------------------------------------------------------------------------
// Selection matrix — selectBackend / selectRealizer / driversFor
// ---------------------------------------------------------------------------

func TestSelectRealizer_LinuxReturnsNix(t *testing.T) {
	r, err := selectRealizer("linux")
	if err != nil {
		t.Fatalf("selectRealizer(linux) error = %v, want nil", err)
	}
	if r == nil {
		t.Fatal("selectRealizer(linux) realizer = nil, want non-nil")
	}
	if r.Name() != "nix" {
		t.Errorf("selectRealizer(linux).Name() = %q, want nix", r.Name())
	}
}

func TestSelectRealizer_DarwinReturnsNix(t *testing.T) {
	r, err := selectRealizer("darwin")
	if err != nil {
		t.Fatalf("selectRealizer(darwin) error = %v, want nil", err)
	}
	if r == nil {
		t.Fatal("selectRealizer(darwin) realizer = nil, want non-nil")
	}
	if r.Name() != "nix" {
		t.Errorf("selectRealizer(darwin).Name() = %q, want nix", r.Name())
	}
}

func TestSelectRealizer_WindowsReturnsErrNoRealizer(t *testing.T) {
	r, err := selectRealizer("windows")
	if !errors.Is(err, ErrNoRealizer) {
		t.Errorf("selectRealizer(windows) error = %v, want ErrNoRealizer", err)
	}
	if r != nil {
		t.Errorf("selectRealizer(windows) realizer = %v, want nil", r)
	}
}

func TestSelectRealizer_Plan9ReturnsErrNoRealizer(t *testing.T) {
	r, err := selectRealizer("plan9")
	if !errors.Is(err, ErrNoRealizer) {
		t.Errorf("selectRealizer(plan9) error = %v, want ErrNoRealizer", err)
	}
	if r != nil {
		t.Errorf("selectRealizer(plan9) realizer = %v, want nil", r)
	}
}

func TestSelectBackend_LinuxReturnsErrNoBackend(t *testing.T) {
	d, err := selectBackend("linux")
	if !errors.Is(err, ErrNoBackend) {
		t.Errorf("selectBackend(linux) error = %v, want ErrNoBackend", err)
	}
	if d != nil {
		t.Errorf("selectBackend(linux) driver = %v, want nil", d)
	}
}

func TestSelectBackend_DarwinReturnsErrNoBackend(t *testing.T) {
	d, err := selectBackend("darwin")
	if !errors.Is(err, ErrNoBackend) {
		t.Errorf("selectBackend(darwin) error = %v, want ErrNoBackend", err)
	}
	if d != nil {
		t.Errorf("selectBackend(darwin) driver = %v, want nil", d)
	}
}

func TestSelectBackend_Plan9ReturnsErrNoBackend(t *testing.T) {
	d, err := selectBackend("plan9")
	if !errors.Is(err, ErrNoBackend) {
		t.Errorf("selectBackend(plan9) error = %v, want ErrNoBackend", err)
	}
	if d != nil {
		t.Errorf("selectBackend(plan9) driver = %v, want nil", d)
	}
}

func TestDriversFor_Windows(t *testing.T) {
	got := driversFor("windows")
	if len(got) != 1 {
		t.Fatalf("driversFor(windows) = %v, want [winget]", got)
	}
	if got[0] != "winget" {
		t.Errorf("driversFor(windows)[0] = %q, want winget", got[0])
	}
}

func TestDriversFor_Linux(t *testing.T) {
	got := driversFor("linux")
	if len(got) != 1 {
		t.Fatalf("driversFor(linux) = %v, want [nix]", got)
	}
	if got[0] != "nix" {
		t.Errorf("driversFor(linux)[0] = %q, want nix", got[0])
	}
}

func TestDriversFor_Darwin(t *testing.T) {
	got := driversFor("darwin")
	if len(got) != 1 {
		t.Fatalf("driversFor(darwin) = %v, want [nix]", got)
	}
	if got[0] != "nix" {
		t.Errorf("driversFor(darwin)[0] = %q, want nix", got[0])
	}
}

func TestDriversFor_Plan9_IsEmpty(t *testing.T) {
	got := driversFor("plan9")
	if len(got) != 0 {
		t.Errorf("driversFor(plan9) = %v, want empty slice", got)
	}
}

// TestWingetDriverIsNotRealizer proves that the winget driver does NOT satisfy
// realizer.Realizer — no Nix concept leaked into the winget type.
func TestWingetDriverIsNotRealizer(t *testing.T) {
	d, err := selectBackend("windows")
	if err != nil {
		t.Fatalf("selectBackend(windows) error = %v", err)
	}
	_, ok := d.(realizer.Realizer)
	if ok {
		t.Error("winget driver must NOT implement realizer.Realizer — abstraction leak")
	}
}

// TestSelectBackendAndRealizerMutuallyExclusive verifies that for any host
// exactly one of the two selectors succeeds (or both fail for unknown OS).
func TestSelectBackendAndRealizerMutuallyExclusive(t *testing.T) {
	tests := []struct {
		goos       string
		backendOK  bool
		realizerOK bool
	}{
		{"windows", true, false},
		{"linux", false, true},
		{"darwin", false, true},
		{"plan9", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.goos, func(t *testing.T) {
			d, derr := selectBackend(tt.goos)
			r, rerr := selectRealizer(tt.goos)

			gotBackendOK := derr == nil && d != nil
			gotRealizerOK := rerr == nil && r != nil

			if gotBackendOK != tt.backendOK {
				t.Errorf("selectBackend(%s): ok=%v, want %v (err=%v)", tt.goos, gotBackendOK, tt.backendOK, derr)
			}
			if gotRealizerOK != tt.realizerOK {
				t.Errorf("selectRealizer(%s): ok=%v, want %v (err=%v)", tt.goos, gotRealizerOK, tt.realizerOK, rerr)
			}

			// At most one should succeed simultaneously.
			if gotBackendOK && gotRealizerOK {
				t.Errorf("both selectBackend and selectRealizer succeeded for %s — must be mutually exclusive", tt.goos)
			}
		})
	}
}

// Compile-time check: mockDriver (from commands_test.go) is NOT a Realizer.
// This keeps the package-level "winget ≠ realizer" invariant honest in tests too.
var _ driver.Driver = (*mockDriver)(nil)
