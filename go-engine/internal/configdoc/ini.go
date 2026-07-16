// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package configdoc

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type INI struct {
	sections map[string]map[string]string
}

func ParseINI(data []byte) (*INI, error) {
	if !utf8.Valid(data) || strings.ContainsRune(string(data), '\x00') {
		return nil, documentError(CodeMalformedINI, "", fmt.Errorf("document is not valid UTF-8 text"))
	}
	document := &INI{sections: map[string]map[string]string{"": {}}}
	seenSections := map[string]bool{"": true}
	currentSection := ""
	for lineNumber, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.ContainsRune(line, '\r') {
			return nil, malformedINILine(lineNumber, "bare carriage return")
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			if !strings.HasSuffix(trimmed, "]") || strings.Count(trimmed, "[") != 1 || strings.Count(trimmed, "]") != 1 {
				return nil, malformedINILine(lineNumber, "malformed section header")
			}
			section := trimmed[1 : len(trimmed)-1]
			if section == "" || section != strings.TrimSpace(section) || strings.ContainsAny(section, "[]\r\n") {
				return nil, malformedINILine(lineNumber, "invalid section name")
			}
			if seenSections[section] {
				return nil, malformedINILine(lineNumber, "duplicate section")
			}
			seenSections[section] = true
			document.sections[section] = make(map[string]string)
			currentSection = section
			continue
		}
		equals := strings.Index(line, "=")
		if equals <= 0 {
			return nil, malformedINILine(lineNumber, "expected key=value assignment")
		}
		key := line[:equals]
		if key == "" || key != strings.TrimSpace(key) || strings.ContainsAny(key, "[]\r\n") {
			return nil, malformedINILine(lineNumber, "invalid key")
		}
		if _, exists := document.sections[currentSection][key]; exists {
			return nil, malformedINILine(lineNumber, "duplicate key")
		}
		document.sections[currentSection][key] = line[equals+1:]
	}
	return document, nil
}

func EncodeINI(document *INI) []byte {
	if document == nil {
		return nil
	}
	groups := make([]string, 0, len(document.sections))
	if global := encodeINIKeys(document.sections[""]); global != "" {
		groups = append(groups, global)
	}
	sections := make([]string, 0, len(document.sections))
	for section := range document.sections {
		if section != "" {
			sections = append(sections, section)
		}
	}
	sort.Strings(sections)
	for _, section := range sections {
		body := encodeINIKeys(document.sections[section])
		group := "[" + section + "]"
		if body != "" {
			group += "\n" + body
		}
		groups = append(groups, group)
	}
	if len(groups) == 0 {
		return nil
	}
	return []byte(strings.Join(groups, "\n\n") + "\n")
}

func INISet(document *INI, section, key, value string) (*INI, error) {
	if err := validateINIAddress(section, key); err != nil {
		return nil, err
	}
	if err := ValidateINIValue(value); err != nil {
		return nil, err
	}
	copy := cloneINI(document)
	if _, exists := copy.sections[section]; !exists {
		copy.sections[section] = make(map[string]string)
	}
	copy.sections[section][key] = value
	return copy, nil
}

func ValidateINIValue(value string) error {
	if !utf8.ValidString(value) || strings.ContainsAny(value, "\r\n\x00") {
		return documentError(CodeInvalidINIValue, "", fmt.Errorf("INI value must be one valid UTF-8 line"))
	}
	return nil
}

// ValidateINIAddress accepts only exact, non-empty section and key names that
// can be addressed without normalization.
func ValidateINIAddress(section, key string) error {
	return validateINIAddress(section, key)
}

// INIKeyExists reports whether an exact-case section/key pair exists.
func INIKeyExists(document *INI, section, key string) (bool, error) {
	if err := validateINIAddress(section, key); err != nil {
		return false, err
	}
	if document == nil {
		return false, nil
	}
	values, exists := document.sections[section]
	if !exists {
		return false, nil
	}
	_, exists = values[key]
	return exists, nil
}

func INIDelete(document *INI, section, key string) (*INI, error) {
	if err := validateINIAddress(section, key); err != nil {
		return nil, err
	}
	values, exists := document.sections[section]
	if !exists {
		return nil, documentError(CodeINISourceMissing, section+"/"+key, fmt.Errorf("source section does not exist"))
	}
	if _, exists := values[key]; !exists {
		return nil, documentError(CodeINISourceMissing, section+"/"+key, fmt.Errorf("source key does not exist"))
	}
	copy := cloneINI(document)
	delete(copy.sections[section], key)
	return copy, nil
}

func INIMove(document *INI, fromSection, fromKey, toSection, toKey string) (*INI, error) {
	if err := validateINIAddress(fromSection, fromKey); err != nil {
		return nil, err
	}
	if err := validateINIAddress(toSection, toKey); err != nil {
		return nil, err
	}
	sourceSection, exists := document.sections[fromSection]
	if !exists {
		return nil, documentError(CodeINISourceMissing, fromSection+"/"+fromKey, fmt.Errorf("source section does not exist"))
	}
	value, exists := sourceSection[fromKey]
	if !exists {
		return nil, documentError(CodeINISourceMissing, fromSection+"/"+fromKey, fmt.Errorf("source key does not exist"))
	}
	if destinationSection, exists := document.sections[toSection]; exists {
		if _, exists := destinationSection[toKey]; exists {
			return nil, documentError(CodeINIDestinationExists, toSection+"/"+toKey, fmt.Errorf("destination key already exists"))
		}
	}
	copy := cloneINI(document)
	delete(copy.sections[fromSection], fromKey)
	if _, exists := copy.sections[toSection]; !exists {
		copy.sections[toSection] = make(map[string]string)
	}
	copy.sections[toSection][toKey] = value
	return copy, nil
}

func encodeINIKeys(values map[string]string) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+values[key])
	}
	return strings.Join(lines, "\n")
}

func cloneINI(document *INI) *INI {
	copy := &INI{sections: make(map[string]map[string]string, len(document.sections))}
	for section, values := range document.sections {
		valueCopy := make(map[string]string, len(values))
		for key, value := range values {
			valueCopy[key] = value
		}
		copy.sections[section] = valueCopy
	}
	return copy
}

func validateINIAddress(section, key string) error {
	if strings.TrimSpace(section) == "" || strings.TrimSpace(key) == "" ||
		section != strings.TrimSpace(section) || key != strings.TrimSpace(key) ||
		!utf8.ValidString(section) || !utf8.ValidString(key) ||
		strings.ContainsAny(section, "[]\r\n\x00") || strings.ContainsAny(key, "[]=\r\n\x00") ||
		strings.HasPrefix(key, ";") || strings.HasPrefix(key, "#") {
		return documentError(CodeMalformedINI, section+"/"+key, fmt.Errorf("invalid INI section or key"))
	}
	return nil
}

func malformedINILine(index int, reason string) error {
	return documentError(CodeMalformedINI, fmt.Sprintf("line %d", index+1), fmt.Errorf("%s", reason))
}
