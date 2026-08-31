// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package settingscodec contains the deliberately small semantic transforms
// used by curated Linux settings adapters. Unknown codecs fail closed.
package settingscodec

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	BoundedRegularFileV1 = "bounded-regular-file-v1"
	GitConfigSafeV1      = "git-config-safe-v1"
)

// Supported reports whether this engine can execute a frozen settings codec.
// Registry review still decides which codec may own each target.
func Supported(codec string) bool {
	switch codec {
	case BoundedRegularFileV1, GitConfigSafeV1:
		return true
	default:
		return false
	}
}

// TargetSupported binds semantic codecs to their reviewed live layouts. A
// same-named file elsewhere is not implicitly treated as application config.
func TargetSupported(codec, target string) bool {
	switch codec {
	case BoundedRegularFileV1:
		return true
	case GitConfigSafeV1:
		switch target {
		case "${home}/.gitattributes", "${home}/.gitconfig",
			"${xdg.config}/git/attributes", "${xdg.config}/git/config", "${xdg.config}/git/ignore":
			return true
		default:
			return false
		}
	default:
		return false
	}
}

var (
	gitSectionPattern = regexp.MustCompile(`^\[\s*([A-Za-z0-9][A-Za-z0-9.-]*)(?:\s+"((?:[^"\\]|\\.)*)")?\s*\]$`)
	gitKeyPattern     = regexp.MustCompile(`^\s*([A-Za-z][A-Za-z0-9-]*)\s*(?:=\s*(.*)|\s+(.*))?$`)
	urlUserInfo       = regexp.MustCompile(`(?i)[a-z][a-z0-9+.-]*://[^/\s"']+@`)
)

func Transform(codec, target string, data []byte) ([]byte, error) {
	if !TargetSupported(codec, target) {
		return nil, fmt.Errorf("settings codec %q does not own target %q", codec, target)
	}
	switch codec {
	case BoundedRegularFileV1:
		return append([]byte(nil), data...), nil
	case GitConfigSafeV1:
		switch target {
		case "${home}/.gitconfig", "${xdg.config}/git/config":
			return sanitizeGitConfig(data)
		case "${home}/.gitattributes", "${xdg.config}/git/attributes", "${xdg.config}/git/ignore":
			return append([]byte(nil), data...), nil
		}
	}
	return nil, fmt.Errorf("settings codec %q is not implemented", codec)
}

type gitSection struct {
	name       string
	subsection string
	blocked    bool
}

func sanitizeGitConfig(data []byte) ([]byte, error) {
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return nil, fmt.Errorf("Git config is not valid text")
	}
	normalized := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\r", "\n")
	hadFinalNewline := strings.HasSuffix(normalized, "\n")
	lines := strings.Split(strings.TrimSuffix(normalized, "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return []byte{}, nil
	}

	output := make([]string, 0, len(lines))
	var section *gitSection
	for index := 0; index < len(lines); index++ {
		line := lines[index]
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			header := strings.TrimSpace(stripGitInlineComment(trimmed))
			match := gitSectionPattern.FindStringSubmatch(header)
			if match == nil {
				return nil, fmt.Errorf("Git config has a malformed section at line %d", index+1)
			}
			section = &gitSection{name: strings.ToLower(match[1]), subsection: match[2]}
			section.blocked = blockedGitSection(*section)
			if !section.blocked {
				output = append(output, header)
			}
			continue
		}
		if section == nil {
			return nil, fmt.Errorf("Git config has a key outside a section at line %d", index+1)
		}

		record := []string{line}
		for gitLineContinues(record[len(record)-1]) {
			index++
			if index >= len(lines) {
				return nil, fmt.Errorf("Git config has an unterminated continuation")
			}
			record = append(record, lines[index])
		}
		first := strings.TrimSpace(stripGitInlineComment(record[0]))
		match := gitKeyPattern.FindStringSubmatch(first)
		if match == nil {
			return nil, fmt.Errorf("Git config has a malformed key at line %d", index-len(record)+2)
		}
		key := strings.ToLower(match[1])
		cleaned := make([]string, 0, len(record))
		for _, part := range record {
			part = strings.TrimSpace(stripGitInlineComment(part))
			if part != "" {
				cleaned = append(cleaned, part)
			}
		}
		logical := strings.Join(cleaned, "\n")
		if section.blocked || sensitiveGitSetting(*section, key, logical) {
			continue
		}
		output = append(output, cleaned...)
	}

	result := strings.Join(output, "\n")
	if result != "" && hadFinalNewline {
		result += "\n"
	}
	return []byte(result), nil
}

func blockedGitSection(section gitSection) bool {
	switch section.name {
	case "credential", "diff", "difftool", "filter", "imap", "include", "includeif", "merge", "mergetool", "sendemail":
		return true
	case "url":
		return urlUserInfo.MatchString(section.subsection) || containsSecretMarker(section.subsection)
	default:
		return false
	}
}

func sensitiveGitSetting(section gitSection, key, logical string) bool {
	lower := strings.ToLower(logical)
	for _, fragment := range []string{"password", "passwd", "token", "secret"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	if containsSecretMarker(lower) || urlUserInfo.MatchString(logical) {
		return true
	}
	if section.name == "http" {
		switch key {
		case "extraheader", "cookiefile", "proxy", "sslcert", "sslkey":
			return true
		}
	}
	if section.name == "alias" && strings.HasPrefix(strings.TrimSpace(gitSettingValue(logical)), "!") {
		return true
	}
	if section.name == "core" && key == "sshcommand" {
		return true
	}
	if section.name == "core" {
		switch key {
		case "askpass", "fsmonitor", "gitproxy", "hookspath", "pager":
			return true
		}
	}
	if section.name == "gpg" && key == "program" {
		return true
	}
	if section.name == "interactive" && key == "difffilter" {
		return true
	}
	if section.name == "submodule" && key == "update" && strings.HasPrefix(strings.TrimSpace(gitSettingValue(logical)), "!") {
		return true
	}
	return false
}

func containsSecretMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"authorization:", "bearer ", "basic ", "ghp_", "github_pat_", "glpat-", "oauth",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func gitSettingValue(line string) string {
	if index := strings.IndexByte(line, '='); index >= 0 {
		return line[index+1:]
	}
	fields := strings.Fields(line)
	if len(fields) > 1 {
		return strings.Join(fields[1:], " ")
	}
	return ""
}

func gitLineContinues(line string) bool {
	line = strings.TrimRight(line, " \t")
	backslashes := 0
	for index := len(line) - 1; index >= 0 && line[index] == '\\'; index-- {
		backslashes++
	}
	return backslashes%2 == 1
}

func stripGitInlineComment(line string) string {
	inQuotes := false
	escaped := false
	for index, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuotes = !inQuotes
			continue
		}
		if !inQuotes && (r == '#' || r == ';') && (index == 0 || line[index-1] == ' ' || line[index-1] == '\t') {
			return strings.TrimRight(line[:index], " \t")
		}
	}
	return line
}
