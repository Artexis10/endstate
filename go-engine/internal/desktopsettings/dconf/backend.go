// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package dconf reads, writes, and resets one boolean GSettings user value
// through the maintained dconf command. Go owns every probe and authorization:
// the commands only move the value, and `gsettings` only describes schemas.
// The engine stays CGO-free; both commands are runtime dependencies.
//
// Reads are exact only under dconf's fallback profile, where the user database
// is the sole source. The backend therefore refuses any profile override, any
// database the dconf writer would rename, and any session whose writer resolves
// a different configuration directory than this process.
package dconf

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Artexis10/endstate/go-engine/internal/desktopsettings"
)

// Command argument vectors are fixed. Only the target's own schema, key, path,
// and the literal boolean text vary.
const (
	dconfCommand     = "dconf"
	gsettingsCommand = "gsettings"
	writerName       = "ca.desrt.dconf"
)

// Backend implements desktopsettings.Mutator for one boolean GSettings key.
// It never calls `dconf` Init, which can rename a database.
type Backend struct{ host Host }

var _ desktopsettings.Mutator = (*Backend)(nil)

// New returns a backend over injected host ports.
func New(host Host) *Backend { return &Backend{host: host} }

// NewSystem returns a backend over the real commands, session bus, and /proc.
func NewSystem() *Backend { return New(systemHost()) }

// Read returns the user override for target, or absence when the user database
// has no value. It proves the writer's namespace and the profile first, so an
// empty answer always means inheritance in the session's own database.
func (b *Backend) Read(ctx context.Context, target desktopsettings.Target) (desktopsettings.Observation, error) {
	s, err := b.open(ctx, target)
	if err != nil {
		return desktopsettings.Observation{}, err
	}
	defer s.close()
	if err := s.prove(ctx); err != nil {
		return desktopsettings.Observation{}, err
	}
	value, err := s.readValue(ctx)
	if err != nil {
		return desktopsettings.Observation{}, err
	}
	return desktopsettings.Observation{User: value, Scope: s.scope}, nil
}

// Write sets the override, or resets it for absence, after proving the same
// namespace as Read. A writer change during the command fails the write; the
// accepted transaction then rolls back through a fresh classification.
func (b *Backend) Write(ctx context.Context, target desktopsettings.Target, scope string, value desktopsettings.Value) error {
	if err := value.Validate(); err != nil {
		return err
	}
	s, err := b.open(ctx, target)
	if err != nil {
		return err
	}
	defer s.close()
	if s.scope != scope {
		return fmt.Errorf("%w: runtime scope differs from the snapshot", desktopsettings.ErrConflict)
	}
	if err := s.prove(ctx); err != nil {
		return err
	}
	argv := []string{s.dconf, "reset", s.key()}
	if value.Present {
		argv = []string{s.dconf, "write", s.key(), strconv.FormatBool(*value.Value)}
	}
	out, err := s.run(ctx, argv)
	if err != nil {
		return fmt.Errorf("dconf %s: %w", argv[1], err)
	}
	if out.ExitCode != 0 {
		return fmt.Errorf("dconf %s failed with exit code %d: %s", argv[1], out.ExitCode, out.Stderr)
	}
	// The command reports the writer it reached; a different owner than the
	// proven one may have received the change outside the proven namespace.
	owner, err := s.conn.NameOwner(ctx, writerName)
	if err != nil {
		return fmt.Errorf("dconf writer owner after %s: %w", argv[1], err)
	}
	if owner != s.owner {
		return fmt.Errorf("dconf writer changed during %s", argv[1])
	}
	return nil
}
