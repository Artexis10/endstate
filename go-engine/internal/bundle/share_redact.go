// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"bytes"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// RedactionReport records exactly what redaction did and did not do.
//
// The report is the feature, not a nicety. Redaction that silently misses
// things is worse than none, because it invites trust it has not earned. A
// sharer can read this, inspect the local zip, and decide — so every rule hit is
// counted and every file that could not be scanned is named.
type RedactionReport struct {
	// Rules maps rule name to the number of replacements it made.
	Rules map[string]int `json:"rules,omitempty"`
	// FilesScanned is the number of payload files successfully decoded and
	// examined.
	FilesScanned int `json:"filesScanned"`
	// FilesChanged is the number of payload files actually modified.
	FilesChanged int `json:"filesChanged"`
	// Unscanned names bundle-relative payload files that could not be decoded as
	// text — binaries, databases, unlabelled encodings. Their contents were
	// neither examined nor altered, so identity inside them travels as-is.
	Unscanned []string `json:"unscanned,omitempty"`
}

// Redaction rule names, stable for reporting.
const (
	RedactRuleUserPath = "user-path"
	RedactRuleEmail    = "email"
	RedactRuleHostname = "hostname"
	RedactRuleGitUser  = "git-user"
)

var (
	// userPathPattern matches a Windows user directory, capturing the username
	// segment. Anchoring on the Users component keeps false positives near zero;
	// a bare username replace would mangle unrelated words.
	//
	// Separators allow one or two characters because JSON payloads store paths
	// escaped ("C:\\Users\\hugo"), which is the common case in the config files
	// most worth sharing. Matching only a single separator silently missed them.
	//
	// The drive colon may also be percent-encoded, because editors store file
	// URIs ("file:///c%3A/Users/hugo/..."). Found by scanning a real share bundle:
	// a VSCodium extensions.json leaked the username in exactly this form after
	// every plain path had been redacted.
	userPathPattern = regexp.MustCompile(`(?i)([A-Za-z](?::|%3A)[\\/]{1,2}Users[\\/]{1,2})([^\\/"'<>:|?*\s]+)`)
	// emailPattern is deliberately ordinary. Exotic-but-legal addresses are
	// accepted as a documented miss rather than risking corruption of adjacent
	// text.
	emailPattern = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)
	// gitUserLinePattern matches identity assignments in a git config file.
	gitUserLinePattern = regexp.MustCompile(`(?im)^\s*(name|email|signingkey)\s*=.*$`)
)

const (
	redactedUser  = "REDACTED"
	redactedEmail = "redacted@example.invalid"
	redactedHost  = "REDACTED-HOST"
)

// shareDenyModules are config modules whose payloads are inherently bound to an
// account or device rather than to preferences worth sharing. In share mode they
// are dropped whole rather than scrubbed: their value to a recipient is near
// zero, and partial redaction of a credential-shaped store is a bad trade.
//
// Kept small and explicit. A module absent here is still subject to the pattern
// pass below.
var shareDenyModules = map[string]bool{
	"apps.betterbird":  true,
	"apps.claws-mail":  true,
	"apps.thunderbird": true,
	"apps.anydesk":     true,
	"apps.teamviewer":  true,
	"apps.rustdesk":    true,
}

// ShareModuleDenied reports whether a module is excluded from share bundles.
func ShareModuleDenied(moduleID string) bool {
	return shareDenyModules[strings.ToLower(strings.TrimSpace(moduleID))]
}

// redactShareTree rewrites identity-bearing values in a share bundle's staged
// configs/ tree and reports what it did.
//
// hostname is the capturing machine's name, replaced wherever it appears in
// payload text.
//
// What this deliberately does NOT do, and must be stated wherever share mode is
// surfaced:
//   - usernames outside a path context, because a bare token replace mangles
//     unrelated words
//   - licence keys and product-key shapes, because GUID-like regexes corrupt
//     functional identifiers at an unacceptable rate
//   - anything inside a file that does not decode as text; those are reported as
//     unscanned rather than silently passed
//   - paths on drives that do not use a Users directory
func redactShareTree(stagingRoot, hostname string) (RedactionReport, error) {
	report := RedactionReport{Rules: map[string]int{}}
	configsRoot := filepath.Join(stagingRoot, "configs")
	if _, err := os.Stat(configsRoot); err != nil {
		return report, nil
	}

	var unscanned []string
	walkErr := filepath.Walk(configsRoot, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !info.Mode().IsRegular() {
			return nil
		}
		rel, relErr := filepath.Rel(stagingRoot, p)
		if relErr != nil {
			return relErr
		}
		relSlash := path.Join("configs", filepath.ToSlash(strings.TrimPrefix(rel, "configs"+string(filepath.Separator))))

		raw, readErr := os.ReadFile(p)
		if readErr != nil {
			return readErr
		}
		text, encoding, ok := decodeText(raw)
		if !ok {
			unscanned = append(unscanned, relSlash)
			return nil
		}
		report.FilesScanned++

		redacted, counts := redactText(text, hostname, isGitConfigPath(relSlash))
		if redacted == text {
			return nil
		}
		for rule, n := range counts {
			report.Rules[rule] += n
		}
		report.FilesChanged++
		return os.WriteFile(p, encodeText(redacted, encoding), info.Mode().Perm())
	})
	if walkErr != nil {
		return report, walkErr
	}
	sort.Strings(unscanned)
	report.Unscanned = unscanned
	return report, nil
}

// redactText applies the rules to one decoded payload, returning the result and
// per-rule replacement counts.
func redactText(text, hostname string, gitConfig bool) (string, map[string]int) {
	counts := map[string]int{}

	if gitConfig {
		text = gitUserLinePattern.ReplaceAllStringFunc(text, func(line string) string {
			counts[RedactRuleGitUser]++
			key := strings.TrimSpace(strings.SplitN(strings.TrimSpace(line), "=", 2)[0])
			return "\t" + key + " = " + redactedUser
		})
	}

	text = userPathPattern.ReplaceAllStringFunc(text, func(match string) string {
		groups := userPathPattern.FindStringSubmatch(match)
		if len(groups) != 3 || strings.EqualFold(groups[2], redactedUser) {
			return match
		}
		counts[RedactRuleUserPath]++
		return groups[1] + redactedUser
	})

	text = emailPattern.ReplaceAllStringFunc(text, func(match string) string {
		if match == redactedEmail {
			return match
		}
		counts[RedactRuleEmail]++
		return redactedEmail
	})

	if host := strings.TrimSpace(hostname); host != "" && host != redactedHost {
		if n := strings.Count(text, host); n > 0 {
			counts[RedactRuleHostname] += n
			text = strings.ReplaceAll(text, host, redactedHost)
		}
	}

	return text, counts
}

// isGitConfigPath reports whether a bundle-relative payload path is a git config
// file, which gets the field-level identity strip in addition to the patterns.
func isGitConfigPath(relSlash string) bool {
	base := strings.ToLower(path.Base(relSlash))
	return base == ".gitconfig" || base == "gitconfig" || base == "config" && strings.Contains(strings.ToLower(relSlash), "/git/")
}

// textEncoding identifies how a payload was decoded, so it can be written back
// in the same form.
type textEncoding int

const (
	encodingUTF8 textEncoding = iota
	encodingUTF8BOM
	encodingUTF16LE
	encodingUTF16BE
)

var (
	bomUTF8    = []byte{0xEF, 0xBB, 0xBF}
	bomUTF16LE = []byte{0xFF, 0xFE}
	bomUTF16BE = []byte{0xFE, 0xFF}
)

// decodeText decodes a payload to a string, reporting its encoding.
//
// UTF-16 is handled because Windows registry exports are UTF-16LE with a BOM and
// carry user paths. A file that does not decode cleanly is refused rather than
// guessed at: a bad guess writes corruption into someone else's config.
func decodeText(raw []byte) (string, textEncoding, bool) {
	switch {
	case bytes.HasPrefix(raw, bomUTF16LE):
		s, ok := decodeUTF16(raw[2:], true)
		return s, encodingUTF16LE, ok
	case bytes.HasPrefix(raw, bomUTF16BE):
		s, ok := decodeUTF16(raw[2:], false)
		return s, encodingUTF16BE, ok
	case bytes.HasPrefix(raw, bomUTF8):
		body := raw[3:]
		if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
			return "", encodingUTF8BOM, false
		}
		return string(body), encodingUTF8BOM, true
	}
	// A NUL byte means binary in practice; scanning it as text risks corruption.
	if !utf8.Valid(raw) || bytes.IndexByte(raw, 0) >= 0 {
		return "", encodingUTF8, false
	}
	return string(raw), encodingUTF8, true
}

func decodeUTF16(body []byte, little bool) (string, bool) {
	if len(body)%2 != 0 {
		return "", false
	}
	units := make([]uint16, 0, len(body)/2)
	for i := 0; i < len(body); i += 2 {
		if little {
			units = append(units, uint16(body[i])|uint16(body[i+1])<<8)
		} else {
			units = append(units, uint16(body[i])<<8|uint16(body[i+1]))
		}
	}
	return string(utf16.Decode(units)), true
}

// encodeText writes a decoded string back in its original encoding, preserving
// the BOM so the consuming application still reads the file.
func encodeText(s string, encoding textEncoding) []byte {
	switch encoding {
	case encodingUTF8BOM:
		return append(append([]byte{}, bomUTF8...), []byte(s)...)
	case encodingUTF16LE, encodingUTF16BE:
		units := utf16.Encode([]rune(s))
		out := make([]byte, 0, 2+len(units)*2)
		if encoding == encodingUTF16LE {
			out = append(out, bomUTF16LE...)
			for _, u := range units {
				out = append(out, byte(u), byte(u>>8))
			}
			return out
		}
		out = append(out, bomUTF16BE...)
		for _, u := range units {
			out = append(out, byte(u>>8), byte(u))
		}
		return out
	default:
		return []byte(s)
	}
}
