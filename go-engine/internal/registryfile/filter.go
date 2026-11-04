// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package registryfile

import (
	"fmt"
	"strings"
)

// SecretRules decides which keys and values a filtered export drops. A key
// arrives as the export writes it, for example
// HKEY_CURRENT_USER\Software\Vendor\App; an empty value name is the key's
// default value.
type SecretRules interface {
	ExcludesKey(key string) bool
	ExcludesValue(key, valueName string) bool
}

// FilterReport counts what FilterSecrets removed. A value inside a removed key
// counts only as part of that key.
type FilterReport struct {
	KeysRemoved   int
	ValuesRemoved int
}

// Removed is the number of secret registry items that left the export.
func (report FilterReport) Removed() int {
	return report.KeysRemoved + report.ValuesRemoved
}

// MatchKey folds a registry key into the form that secret rules match: its
// CanonicalKey, lower-cased. Lower-casing merges every pair of names that the
// registry merges and a few that it keeps apart, such as İ and i, so a secret
// rule can match too much but never too little. SameKey compares keys exactly
// as the registry does. It does not validate the key.
func MatchKey(key string) string {
	return strings.ToLower(CanonicalKey(key))
}

// CanonicalKey returns the one spelling of key that Endstate opens and
// compares. "/" becomes "\", because module coordinates accept "/" as a
// separator, and the hive is spelled HKCU or HKLM without a PowerShell drive
// colon. The subkey keeps its case. It does not validate the key.
//
// A secret check and a registry read must both use this spelling: a check on
// the authored text would miss a secret that the read then opens. The "/"
// rule means that a key whose name contains "/", such as a MIME type key,
// cannot be addressed.
func CanonicalKey(key string) string {
	key = strings.ReplaceAll(key, "/", `\`)
	// The hive names are fixed by Windows.
	for _, hive := range []struct{ name, short string }{
		{"HKEY_CURRENT_USER", "HKCU"}, {"HKEY_LOCAL_MACHINE", "HKLM"}, {"HKCU", "HKCU"}, {"HKLM", "HKLM"},
	} {
		if len(key) < len(hive.name) || !strings.EqualFold(key[:len(hive.name)], hive.name) {
			continue
		}
		if rest := strings.TrimPrefix(key[len(hive.name):], ":"); rest == "" || rest[0] == '\\' {
			return hive.short + rest
		}
	}
	return key
}

// SplitKey splits the CanonicalKey of key at its first separator into the hive
// and the subkey. The registry value reader and restore open keys through it,
// so they open the key that the secret check compared.
func SplitKey(key string) (hive, subkey string, found bool) {
	return strings.Cut(CanonicalKey(key), `\`)
}

// FilterSecrets removes every key and value that rules exclude from a reg.exe
// export of root and returns UTF-16LE bytes for reg import. Kept sections and
// values are re-emitted byte-for-byte, so ordinary settings survive exactly.
//
// The tokenizer accepts what reg.exe really writes: any key text inside a
// section line, quoted value names with \" and \\ escapes, string data that
// spans physical lines, and hex data with continuation lines. It rejects
// deletion entries, sections outside root, and anything it cannot classify.
// The output is parsed again and must contain nothing rules exclude, so a
// tokenizer defect fails the export instead of leaking it.
func FilterSecrets(data []byte, root string, rules SecretRules) ([]byte, FilterReport, error) {
	if rules == nil {
		return nil, FilterReport{}, fmt.Errorf("registry filter has no secret rules")
	}
	text, err := decode(data)
	if err != nil {
		return nil, FilterReport{}, err
	}
	if err := validateText(text); err != nil {
		return nil, FilterReport{}, err
	}
	document, err := tokenizeRegistryExport(text, root)
	if err != nil {
		return nil, FilterReport{}, err
	}
	if document.sections == 0 {
		return nil, FilterReport{}, fmt.Errorf("registry file contains no registry key sections")
	}

	var report FilterReport
	var output strings.Builder
	output.WriteString(document.header)
	dropSection := false
	for _, entry := range document.entries {
		if entry.section {
			dropSection = rules.ExcludesKey(entry.key)
			if dropSection {
				report.KeysRemoved++
				continue
			}
		} else if dropSection {
			continue
		} else if rules.ExcludesValue(entry.key, entry.name) {
			report.ValuesRemoved++
			continue
		}
		output.WriteString(entry.lead)
		output.WriteString(entry.raw)
	}
	output.WriteString(document.trailer)

	filtered := output.String()
	recheck, err := tokenizeRegistryExport(filtered, root)
	if err != nil {
		return nil, FilterReport{}, fmt.Errorf("filtered registry file does not parse: %w", err)
	}
	for _, entry := range recheck.entries {
		if entry.section && rules.ExcludesKey(entry.key) || !entry.section && rules.ExcludesValue(entry.key, entry.name) {
			return nil, FilterReport{}, fmt.Errorf("filtered registry file still contains secret material")
		}
	}
	return encodeUTF16LE(filtered), report, nil
}

type registryExportDocument struct {
	header   string
	entries  []registryExportEntry
	trailer  string
	sections int
}

// registryExportEntry is one section line or one value entry. raw holds the
// exact text including its line terminators; lead holds blank lines before it.
type registryExportEntry struct {
	section bool
	key     string
	name    string
	lead    string
	raw     string
}

func tokenizeRegistryExport(text, root string) (registryExportDocument, error) {
	var document registryExportDocument
	if !strings.HasPrefix(text, registryHeader) {
		return document, fmt.Errorf("registry file has an invalid or misplaced header")
	}
	position := len(registryHeader)
	terminator, ok := lineTerminatorAt(text, position)
	if !ok && position != len(text) {
		return document, fmt.Errorf("registry file has an invalid or misplaced header")
	}
	position += terminator
	document.header = text[:position]

	rootKey := MatchKey(root)
	currentKey := ""
	lead := strings.Builder{}
	for position < len(text) {
		if terminator, ok := lineTerminatorAt(text, position); ok {
			lead.WriteString(text[position : position+terminator])
			position += terminator
			continue
		}
		start := position
		switch text[position] {
		case '[':
			lineEnd, next := lineEndAt(text, position)
			line := text[position:lineEnd]
			if len(line) < 3 || line[len(line)-1] != ']' {
				return document, fmt.Errorf("registry file has a malformed section at offset %d", start)
			}
			key := line[1 : len(line)-1]
			if strings.HasPrefix(key, "-") {
				return document, fmt.Errorf("registry file contains a deletion section at offset %d", start)
			}
			if match := MatchKey(key); match != rootKey && !strings.HasPrefix(match, rootKey+`\`) {
				return document, fmt.Errorf("registry file section at offset %d is outside the expected root", start)
			}
			currentKey = key
			document.sections++
			document.entries = append(document.entries, registryExportEntry{section: true, key: key, lead: lead.String(), raw: text[start:next]})
			position = next
		case '@', '"':
			if currentKey == "" {
				return document, fmt.Errorf("registry file has content before the first section at offset %d", start)
			}
			name, next, err := scanRegistryValueEntry(text, position)
			if err != nil {
				return document, fmt.Errorf("registry file value at offset %d: %w", start, err)
			}
			document.entries = append(document.entries, registryExportEntry{key: currentKey, name: name, lead: lead.String(), raw: text[start:next]})
			position = next
		default:
			return document, fmt.Errorf("registry file has an unsupported line at offset %d", start)
		}
		lead.Reset()
	}
	document.trailer = lead.String()
	return document, nil
}

// scanRegistryValueEntry reads one value entry starting at position and
// returns its unescaped name and the offset just past its final terminator.
func scanRegistryValueEntry(text string, position int) (string, int, error) {
	name := ""
	if text[position] == '@' {
		position++
	} else {
		var err error
		name, position, err = scanQuoted(text, position, false)
		if err != nil {
			return "", 0, fmt.Errorf("value name: %w", err)
		}
	}
	if position >= len(text) || text[position] != '=' {
		return "", 0, fmt.Errorf("value name is not followed by '='")
	}
	position++
	if position < len(text) && text[position] == '"' {
		_, end, err := scanQuoted(text, position, true)
		if err != nil {
			return "", 0, fmt.Errorf("string data: %w", err)
		}
		terminator, ok := lineTerminatorAt(text, end)
		if !ok && end != len(text) {
			return "", 0, fmt.Errorf("string data is followed by more text")
		}
		return name, end + terminator, nil
	}

	lineEnd, next := lineEndAt(text, position)
	continued, err := validateRegistryValueLine(`@=` + text[position:lineEnd])
	if err != nil {
		return "", 0, err
	}
	for continued {
		if next >= len(text) {
			return "", 0, fmt.Errorf("hex data has an unterminated continuation")
		}
		lineStart := next
		lineEnd, next = lineEndAt(text, lineStart)
		continued, err = validateRegistryContinuation(text[lineStart:lineEnd])
		if err != nil {
			return "", 0, err
		}
	}
	return name, next, nil
}

// scanQuoted reads a quoted .reg token at position and returns its unescaped
// text and the offset just past the closing quote. Only string data may span
// physical lines; a value name may not.
func scanQuoted(text string, position int, multiline bool) (string, int, error) {
	var value strings.Builder
	for index := position + 1; index < len(text); index++ {
		switch character := text[index]; character {
		case '\\':
			if index+1 >= len(text) {
				return "", 0, fmt.Errorf("unterminated escape")
			}
			escaped := text[index+1]
			if escaped != '\\' && escaped != '"' && !multiline {
				return "", 0, fmt.Errorf("unsupported escape")
			}
			value.WriteByte(escaped)
			index++
		case '"':
			return value.String(), index + 1, nil
		case '\r', '\n':
			if !multiline {
				return "", 0, fmt.Errorf("quoted name spans lines")
			}
			value.WriteByte(character)
		default:
			value.WriteByte(character)
		}
	}
	return "", 0, fmt.Errorf("unterminated quoted text")
}

// lineTerminatorAt reports the length of a CRLF or LF terminator at position.
func lineTerminatorAt(text string, position int) (int, bool) {
	if strings.HasPrefix(text[position:], "\r\n") {
		return 2, true
	}
	if strings.HasPrefix(text[position:], "\n") {
		return 1, true
	}
	return 0, false
}

// lineEndAt returns the end of the physical line at position, excluding its
// terminator, and the offset of the next line.
func lineEndAt(text string, position int) (int, int) {
	newline := strings.IndexByte(text[position:], '\n')
	if newline < 0 {
		return len(text), len(text)
	}
	end := position + newline
	next := end + 1
	if end > position && text[end-1] == '\r' {
		end--
	}
	return end, next
}
