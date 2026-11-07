// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package settingscodec

import (
	"errors"
	"fmt"

	"github.com/pelletier/go-toml/v2"
)

// parseTOMLDocument returns the raw table of a TOML document. The library's
// error text quotes document lines, so only the line number leaves this file.
func parseTOMLDocument(data []byte) (map[string]any, error) {
	var raw map[string]any
	if err := toml.Unmarshal(data, &raw); err != nil {
		var decodeErr *toml.DecodeError
		if errors.As(err, &decodeErr) {
			line, _ := decodeErr.Position()
			return nil, fmt.Errorf("settings are not a valid TOML document (line %d)", line)
		}
		return nil, fmt.Errorf("settings are not a valid TOML document")
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return raw, nil
}

func marshalTOMLDocument(value map[string]any) ([]byte, error) {
	if len(value) == 0 {
		return []byte{}, nil
	}
	data, err := toml.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("settings could not be encoded as TOML")
	}
	return data, nil
}
