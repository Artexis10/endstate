// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"errors"
	"testing"
)

func TestSelectBackend_WindowsReturnsWinget(t *testing.T) {
	d, err := selectBackend("windows")
	if err != nil {
		t.Fatalf("selectBackend(windows) error = %v, want nil", err)
	}
	if d == nil {
		t.Fatal("selectBackend(windows) driver = nil, want non-nil")
	}
	if d.Name() != "winget" {
		t.Errorf("selectBackend(windows).Name() = %q, want winget", d.Name())
	}
}

func TestSelectBackend_UnsupportedReturnsErrNoBackend(t *testing.T) {
	d, err := selectBackend("linux")
	if !errors.Is(err, ErrNoBackend) {
		t.Errorf("selectBackend(linux) error = %v, want ErrNoBackend", err)
	}
	if d != nil {
		t.Errorf("selectBackend(linux) driver = %v, want nil", d)
	}
}

func TestPlatformInfoFor_Windows(t *testing.T) {
	pi := platformInfoFor("windows")
	if pi.OS != "windows" {
		t.Errorf("platformInfoFor(windows).OS = %q, want windows", pi.OS)
	}
	found := false
	for _, drv := range pi.Drivers {
		if drv == "winget" {
			found = true
		}
	}
	if !found {
		t.Errorf("platformInfoFor(windows).Drivers = %v, want to include winget", pi.Drivers)
	}
}

func TestPlatformInfoFor_NonWindowsHasNoWinget(t *testing.T) {
	pi := platformInfoFor("linux")
	if pi.OS != "linux" {
		t.Errorf("platformInfoFor(linux).OS = %q, want linux", pi.OS)
	}
	for _, drv := range pi.Drivers {
		if drv == "winget" {
			t.Errorf("platformInfoFor(linux).Drivers = %v, must not include winget", pi.Drivers)
		}
	}
}
