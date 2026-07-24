// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package registryfile validates and semantically rewrites reg.exe documents
// without executing operating-system registry commands.
package registryfile

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const registryHeader = "Windows Registry Editor Version 5.00"

// RewriteSubtree validates that every section in data is the expected HKCU
// root or one of its descendants, rewrites that root to replacementRoot, and
// returns deterministic UTF-16LE-with-BOM bytes suitable for reg import.
// Value lines and section order are preserved; only section roots and newline
// encoding are changed.
func RewriteSubtree(data []byte, expectedRoot, replacementRoot string) ([]byte, error) {
	expected, err := validationmode.NormalizeHKCU(expectedRoot)
	if err != nil {
		return nil, fmt.Errorf("registry file expected root: %w", err)
	}
	replacement, err := validationmode.NormalizeHKCU(replacementRoot)
	if err != nil {
		return nil, fmt.Errorf("registry file replacement root: %w", err)
	}
	text, err := decode(data)
	if err != nil {
		return nil, err
	}
	if err := validateText(text); err != nil {
		return nil, err
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) == 0 || lines[0] != registryHeader {
		return nil, fmt.Errorf("registry file has an invalid or misplaced header")
	}
	sections := 0
	expectedFold := strings.ToLower(expected)
	for index, line := range lines {
		if index > 0 && line == registryHeader {
			return nil, fmt.Errorf("registry file contains multiple documents")
		}
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "=-") && (strings.HasPrefix(trimmed, `"`) || strings.HasPrefix(trimmed, "@")) {
			return nil, fmt.Errorf("registry file contains a deletion directive on line %d", index+1)
		}
		if !strings.HasPrefix(trimmed, "[") && !strings.HasSuffix(trimmed, "]") {
			continue
		}
		if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") ||
			strings.Count(trimmed, "[") != 1 || strings.Count(trimmed, "]") != 1 || trimmed != line {
			return nil, fmt.Errorf("registry file has a malformed section on line %d", index+1)
		}
		key := trimmed[1 : len(trimmed)-1]
		deletion := strings.HasPrefix(key, "-")
		if deletion {
			return nil, fmt.Errorf("registry file contains a deletion section on line %d", index+1)
		}
		normalized, normalizeErr := validationmode.NormalizeHKCU(key)
		if normalizeErr != nil {
			return nil, fmt.Errorf("registry file section on line %d: %w", index+1, normalizeErr)
		}
		normalizedFold := strings.ToLower(normalized)
		if normalizedFold != expectedFold && !strings.HasPrefix(normalizedFold, expectedFold+`\`) {
			return nil, fmt.Errorf("registry file section on line %d is outside the expected root", index+1)
		}
		suffix := normalized[len(expected):]
		lines[index] = "[" + replacement + suffix + "]"
		sections++
	}
	if sections == 0 {
		return nil, fmt.Errorf("registry file contains no registry key sections")
	}

	// reg.exe exports end in a newline. Always emitting one removes input
	// encoding/newline variance and makes capture hashes reproducible.
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return encodeUTF16LE(strings.Join(lines, "\r\n") + "\r\n"), nil
}

func decode(data []byte) (string, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xfe}):
		payload := data[2:]
		if len(payload)%2 != 0 {
			return "", fmt.Errorf("registry file has malformed UTF-16LE content")
		}
		words := make([]uint16, len(payload)/2)
		for index := range words {
			words[index] = binary.LittleEndian.Uint16(payload[index*2:])
		}
		if !validUTF16(words) {
			return "", fmt.Errorf("registry file has malformed UTF-16LE content")
		}
		return string(utf16.Decode(words)), nil
	case bytes.HasPrefix(data, []byte{0xfe, 0xff}):
		return "", fmt.Errorf("registry file UTF-16BE encoding is unsupported")
	case bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}):
		data = data[3:]
	}
	if !utf8.Valid(data) {
		return "", fmt.Errorf("registry file is not canonical UTF-8 or UTF-16LE")
	}
	return string(data), nil
}

func validUTF16(words []uint16) bool {
	for index := 0; index < len(words); index++ {
		word := words[index]
		switch {
		case 0xd800 <= word && word <= 0xdbff:
			if index+1 >= len(words) || words[index+1] < 0xdc00 || words[index+1] > 0xdfff {
				return false
			}
			index++
		case 0xdc00 <= word && word <= 0xdfff:
			return false
		}
	}
	return true
}

func validateText(value string) error {
	for _, character := range value {
		if character == '\r' || character == '\n' || character == '\t' {
			continue
		}
		if character == 0 || unicode.IsControl(character) {
			return fmt.Errorf("registry file contains ambiguous control content")
		}
	}
	return nil
}

func encodeUTF16LE(value string) []byte {
	words := utf16.Encode([]rune(value))
	result := make([]byte, 2+len(words)*2)
	result[0], result[1] = 0xff, 0xfe
	for index, word := range words {
		binary.LittleEndian.PutUint16(result[2+index*2:], word)
	}
	return result
}
