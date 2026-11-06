// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package sessionbus resolves and dials the existing user session bus. It never
// discovers or launches a bus, so availability checks cannot start a session.
package sessionbus

import (
	"context"
	"errors"
	"path/filepath"
	"strings"

	"github.com/godbus/dbus/v5"
)

// ErrNoSessionBus reports that the environment names no existing session bus.
var ErrNoSessionBus = errors.New("no existing user session bus address")

// Address returns DBUS_SESSION_BUS_ADDRESS from env (KEY=VALUE entries), else
// the conventional unix:path=$XDG_RUNTIME_DIR/bus when that directory is absolute.
func Address(env []string) (string, error) {
	values := map[string]string{}
	for _, entry := range env {
		key, value, ok := strings.Cut(entry, "=")
		if ok {
			values[key] = value
		}
	}
	if address := values["DBUS_SESSION_BUS_ADDRESS"]; address != "" {
		return address, nil
	}
	if filepath.IsAbs(values["XDG_RUNTIME_DIR"]) {
		return "unix:path=" + dbus.EscapeBusAddressValue(filepath.Join(values["XDG_RUNTIME_DIR"], "bus")), nil
	}
	return "", ErrNoSessionBus
}

// Connect dials an explicit address; an explicit address prevents godbus's
// session-bus autolaunch path.
func Connect(ctx context.Context, address string) (*dbus.Conn, error) {
	return dbus.Connect(address, dbus.WithContext(ctx))
}
