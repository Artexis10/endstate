// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/sessionbus"
	"github.com/godbus/dbus/v5"
)

type sessionManager struct {
	env                []string
	address, systemctl string
	initErr            error
}

func newSessionManager(env []string) *sessionManager {
	m := &sessionManager{env: append([]string(nil), env...)}
	address, err := sessionbus.Address(env)
	if err != nil {
		m.initErr = err
		return m
	}
	m.address = address
	// systemctl otherwise prefers XDG_RUNTIME_DIR/systemd/private, which can
	// differ from the explicit bus used for typed reads. Force that same bus.
	m.env = nil
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if key != "DBUS_SESSION_BUS_ADDRESS" && key != "SYSTEMCTL_FORCE_BUS" {
			m.env = append(m.env, entry)
		}
	}
	m.env = append(m.env, "DBUS_SESSION_BUS_ADDRESS="+m.address, "SYSTEMCTL_FORCE_BUS=1")
	// Resolve once; systemctl and typed reads share this captured environment.
	m.systemctl, m.initErr = exec.LookPath("systemctl")
	return m
}
func (m *sessionManager) connect(ctx context.Context) (*dbus.Conn, error) {
	if m.initErr != nil {
		return nil, m.initErr
	}
	conn, err := sessionbus.Connect(ctx, m.address)
	if err != nil {
		return nil, err
	}
	var owned bool
	err = conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.NameHasOwner", 0, "org.freedesktop.systemd1").Store(&owned)
	if err != nil || !owned {
		conn.Close()
		if err == nil {
			err = fmt.Errorf("systemd user manager does not own its bus name")
		}
		return nil, err
	}
	return conn, nil
}
func (m *sessionManager) UnitPaths(ctx context.Context) ([]string, error) {
	conn, err := m.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var value dbus.Variant
	err = conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").CallWithContext(ctx, "org.freedesktop.DBus.Properties.Get", dbus.FlagNoAutoStart, "org.freedesktop.systemd1.Manager", "UnitPath").Store(&value)
	if err != nil {
		return nil, err
	}
	var paths []string
	if err := dbus.Store([]interface{}{value.Value()}, &paths); err != nil {
		return nil, err
	}
	return paths, nil
}
func (m *sessionManager) Properties(ctx context.Context, name, kind string) (map[string]dbus.Variant, error) {
	conn, err := m.connect(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	var path dbus.ObjectPath
	err = conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1").CallWithContext(ctx, "org.freedesktop.systemd1.Manager.LoadUnit", dbus.FlagNoAutoStart, name).Store(&path)
	if err != nil {
		var native dbus.Error
		// NoSuchUnit is a systemd D-Bus error identifier, independent of locale.
		if kind == "Unit" && errors.As(err, &native) && native.Name == "org.freedesktop.systemd1.NoSuchUnit" {
			return map[string]dbus.Variant{"LoadState": dbus.MakeVariant("not-found"), "FragmentPath": dbus.MakeVariant(""), "DropInPaths": dbus.MakeVariant([]string{})}, nil
		}
		return nil, err
	}
	var values map[string]dbus.Variant
	err = conn.Object("org.freedesktop.systemd1", path).CallWithContext(ctx, "org.freedesktop.DBus.Properties.GetAll", dbus.FlagNoAutoStart, "org.freedesktop.systemd1."+kind).Store(&values)
	return values, err
}
func (m *sessionManager) Command(ctx context.Context, args ...string) error {
	if m.initErr != nil {
		return m.initErr
	}
	cmd := exec.CommandContext(ctx, m.systemctl, append([]string{"--user"}, args...)...)
	cmd.Env = m.env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl --user %v: %w: %s", args, err, out)
	}
	return nil
}
