// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package desktopsettings

import (
	"context"
	"strings"
	"testing"
)

// Malformed or target-bearing payloads must never acquire mutation authority.
func TestDecodeRejectsInvalidDesktopState(t *testing.T) {
	for _, data := range []string{
		`{"type":"boolean","present":false,"value":false}` + "\n",
		`{"type":"boolean","present":true}` + "\n",
		`{"type":"boolean","present":true,"value":null}` + "\n",
		`{"type":"boolean","present":true,"value":"false"}` + "\n",
		`{"type":"string","present":false}` + "\n",
		`{"type":"boolean","present":false,"target":"other"}` + "\n",
		`{"type":"boolean","present":false}` + "\n{}",
		`{"type":"boolean","present":true,"present":false}` + "\n",
		`{"type":"boolean"}` + "\n",
		strings.Repeat(" ", MaxPayloadBytes+1),
	} {
		t.Run(data[:min(len(data), 70)], func(t *testing.T) {
			if _, err := Decode([]byte(data)); err == nil {
				t.Fatal("accepted invalid desktop payload")
			}
		})
	}
}

// False and inheritance must survive the portable encoding independently.
func TestDesktopStateDistinguishesFalseAndAbsence(t *testing.T) {
	absent, err := Decode([]byte("{\"type\":\"boolean\",\"present\":false}\n"))
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := Decode([]byte("{\"type\":\"boolean\",\"present\":true,\"value\":false}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if absent.Equal(explicit) || absent.Value != nil || explicit.Value == nil || *explicit.Value {
		t.Fatal("false collapsed into absence")
	}
}

type observedReader struct {
	observation Observation
	err         error
}

func (r observedReader) Read(context.Context, Target) (Observation, error) {
	return r.observation, r.err
}

// An unreadable or unsupported raw state must not produce an absence payload.
func TestReadRejectsUnavailableAndWrongType(t *testing.T) {
	target := Target{Schema: "org.example.settings", Path: "/org/example/", Key: "enabled", Type: "boolean"}
	for _, reader := range []Reader{nil, observedReader{err: ErrUnavailable}, observedReader{observation: Observation{Scope: "fixture", User: Value{Type: "string"}}}, observedReader{observation: Observation{Scope: "fixture", User: Value{Type: "boolean", Present: true}}}} {
		if _, err := Read(context.Background(), reader, target); err == nil {
			t.Fatal("unavailable or unsupported state became absence")
		}
	}
}
