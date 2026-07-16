// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configdoc

import (
	"encoding/json"
	"fmt"
	"strconv"
	"unicode/utf8"
)

type jsonPathToken struct {
	key   string
	index int
	isKey bool
}

func ValidateJSONPath(path string) error {
	_, err := parseJSONPath(path)
	return err
}

func parseJSONPath(value string) ([]jsonPathToken, error) {
	invalid := func() ([]jsonPathToken, error) {
		return nil, documentError(CodeInvalidJSONPath, value, fmt.Errorf("path is outside the supported subset"))
	}
	if value == "" || !utf8.ValidString(value) || value[0] != '$' {
		return invalid()
	}
	tokens := make([]jsonPathToken, 0)
	for offset := 1; offset < len(value); {
		switch value[offset] {
		case '.':
			offset++
			if offset >= len(value) || !isJSONFieldStart(value[offset]) {
				return invalid()
			}
			start := offset
			for offset < len(value) && isJSONFieldContinue(value[offset]) {
				offset++
			}
			tokens = append(tokens, jsonPathToken{key: value[start:offset], isKey: true})
		case '[':
			offset++
			if offset >= len(value) {
				return invalid()
			}
			if value[offset] == '"' {
				start := offset
				offset++
				escaped := false
				for offset < len(value) {
					character := value[offset]
					if escaped {
						escaped = false
						offset++
						continue
					}
					if character == '\\' {
						escaped = true
						offset++
						continue
					}
					if character == '"' {
						break
					}
					offset++
				}
				if offset >= len(value) || value[offset] != '"' {
					return invalid()
				}
				var key string
				if err := json.Unmarshal([]byte(value[start:offset+1]), &key); err != nil {
					return invalid()
				}
				offset++
				if offset >= len(value) || value[offset] != ']' {
					return invalid()
				}
				offset++
				tokens = append(tokens, jsonPathToken{key: key, isKey: true})
				continue
			}
			start := offset
			for offset < len(value) && value[offset] >= '0' && value[offset] <= '9' {
				offset++
			}
			if start == offset || offset >= len(value) || value[offset] != ']' ||
				(offset-start > 1 && value[start] == '0') {
				return invalid()
			}
			index, err := strconv.Atoi(value[start:offset])
			if err != nil {
				return invalid()
			}
			offset++
			tokens = append(tokens, jsonPathToken{index: index})
		default:
			return invalid()
		}
	}
	return tokens, nil
}

func isJSONFieldStart(character byte) bool {
	return character == '_' || (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z')
}

func isJSONFieldContinue(character byte) bool {
	return isJSONFieldStart(character) || (character >= '0' && character <= '9')
}
