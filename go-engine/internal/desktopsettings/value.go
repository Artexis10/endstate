// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package desktopsettings defines the typed, injected desktop boundary. Native
// availability and target authority belong to its callers.
package desktopsettings

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// MaxPayloadBytes is the portable desktop payload limit fixed by the contract.
const MaxPayloadBytes = 1024

var ErrUnavailable = errors.New("desktop settings boundary is unavailable")
var ErrConflict = errors.New("desktop settings ownership conflict")

// ErrUnsupported reports stored state outside the contract's value types. It is
// never absence, so apply and revert refuse it instead of overwriting it.
var ErrUnsupported = errors.New("desktop settings value is unsupported")

// UserDatabaseCoordinate names the dconf user database that backs every GSettings
// target. The backend reads it, and collision checks treat it as claimed by any
// GSettings target.
const UserDatabaseCoordinate = "${xdg.config}/dconf/user"

// Target is trusted generation metadata. Path is the backing directory; Schema
// authorizes the typed access but does not distinguish owners of the same key.
type Target struct {
	Schema string `json:"schema"`
	Path   string `json:"path"`
	Key    string `json:"key"`
	Type   string `json:"type"`
}

func (t Target) Validate() error {
	// Boolean GSettings values are the only desktop primitive in this contract.
	if t.Type != "boolean" || t.Schema == "" || len(t.Schema) > 255 || t.Path == "/" || !strings.HasPrefix(t.Path, "/") || !strings.HasSuffix(t.Path, "/") || path.Clean(t.Path)+"/" != t.Path || len(t.Path) > 512 || t.Key == "" || len(t.Key) > 255 || strings.ContainsAny(t.Key, "/\\") || path.Clean(t.Key) != t.Key || (t.Key == ".." || t.Key == ".") {
		return fmt.Errorf("invalid boolean GSettings target")
	}
	for _, part := range []string{t.Schema, t.Path, t.Key} {
		for _, r := range part {
			if r <= 0x20 || r == 0x7f {
				return fmt.Errorf("desktop target contains whitespace or control characters")
			}
		}
	}
	return nil
}
func (t Target) Identity() string { return "gsettings:" + t.Path + t.Key }

// Value preserves the explicit user override, including absence and false.
type Value struct {
	Type    string `json:"type"`
	Present bool   `json:"present"`
	Value   *bool  `json:"value,omitempty"`
}

func (v Value) Validate() error {
	if v.Type != "boolean" || v.Present != (v.Value != nil) {
		return fmt.Errorf("desktop state requires boolean type and a value exactly when present")
	}
	return nil
}
func (v Value) Clone() Value {
	if v.Value != nil {
		b := *v.Value
		v.Value = &b
	}
	return v
}
func (v Value) Equal(other Value) bool {
	return v.Type == other.Type && v.Present == other.Present && (v.Value == nil && other.Value == nil || v.Value != nil && other.Value != nil && *v.Value == *other.Value)
}
func Encode(v Value) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(v)
	return append(data, '\n'), err
}
func Decode(data []byte) (Value, error) {
	if len(data) > MaxPayloadBytes {
		return Value{}, fmt.Errorf("desktop payload exceeds %d bytes", MaxPayloadBytes)
	}
	// Requiring canonical JSON also rejects duplicate fields and explicit nulls.
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var v Value
	if err := decoder.Decode(&v); err != nil {
		return Value{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return Value{}, fmt.Errorf("desktop payload has trailing data")
	}
	canonical, err := Encode(v)
	if err != nil {
		return Value{}, err
	}
	if !bytes.Equal(data, canonical) {
		return Value{}, fmt.Errorf("desktop payload is not canonical")
	}
	return v, nil
}

// Observation separates effective defaults from exact user state. Scope names
// the stable resolved user backend/profile namespace; it is never portable.
type Observation struct {
	User      Value
	Effective *bool
	Scope     string
}

func (o Observation) Validate() error {
	if err := o.User.Validate(); err != nil {
		return err
	}
	if o.Scope == "" || len(o.Scope) > 4096 || strings.TrimSpace(o.Scope) != o.Scope {
		return fmt.Errorf("desktop runtime scope is missing or invalid")
	}
	for _, r := range o.Scope {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("desktop scope contains a control character")
		}
	}
	return nil
}

type Reader interface {
	Read(context.Context, Target) (Observation, error)
}

// Write completes and synchronizes one override mutation; an absent value resets
// only that override. The mutator must prove the expected scope before writing.
// The transaction verifies every write with a fresh read.
type Mutator interface {
	Reader
	Write(context.Context, Target, string, Value) error
}

func Read(ctx context.Context, reader Reader, target Target) (Observation, error) {
	if err := target.Validate(); err != nil {
		return Observation{}, err
	}
	if reader == nil {
		return Observation{}, ErrUnavailable
	}
	if ctx == nil {
		return Observation{}, fmt.Errorf("desktop read context is nil")
	}
	if err := ctx.Err(); err != nil {
		return Observation{}, err
	}
	observed, err := reader.Read(ctx, target)
	if err != nil {
		return Observation{}, err
	}
	if err := observed.Validate(); err != nil {
		return Observation{}, err
	}
	observed.User = observed.User.Clone()
	return observed, nil
}

type backendKey struct{}

func WithBackend(ctx context.Context, backend Mutator) context.Context {
	if backend == nil {
		return ctx
	}
	return context.WithValue(ctx, backendKey{}, backend)
}
func Backend(ctx context.Context) Mutator {
	if ctx == nil {
		return nil
	}
	backend, _ := ctx.Value(backendKey{}).(Mutator)
	return backend
}
