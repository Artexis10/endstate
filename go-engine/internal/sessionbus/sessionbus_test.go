// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package sessionbus

import (
	"errors"
	"runtime"
	"testing"
)

// A relative or missing runtime directory must never turn into a guessed bus path,
// because a wrong guess would make callers talk to another session or none.
func TestAddressUsesOnlyExplicitCoordinates(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("session bus paths are POSIX")
	}
	for _, test := range []struct {
		name string
		env  []string
		want string
		err  error
	}{
		{"explicit address wins", []string{"DBUS_SESSION_BUS_ADDRESS=unix:path=/x/bus", "XDG_RUNTIME_DIR=/run/user/1000"}, "unix:path=/x/bus", nil},
		{"runtime directory fallback", []string{"XDG_RUNTIME_DIR=/run/user/1000"}, "unix:path=/run/user/1000/bus", nil},
		{"relative runtime directory", []string{"XDG_RUNTIME_DIR=run/user"}, "", ErrNoSessionBus},
		{"nothing", nil, "", ErrNoSessionBus},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Address(test.env)
			if got != test.want || !errors.Is(err, test.err) {
				t.Fatalf("Address=%q err=%v want %q %v", got, err, test.want, test.err)
			}
		})
	}
}
