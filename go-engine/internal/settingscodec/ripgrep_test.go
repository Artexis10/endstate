// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package settingscodec

import (
	"strings"
	"testing"
)

func TestRipgrepPortableArgumentsPreservesOrderedPreferences(t *testing.T) {
	for _, target := range []string{"${home}/.ripgreprc", "${xdg.config}/ripgrep/config", "${xdg.config}/ripgrep/ripgreprc"} {
		t.Run(target, func(t *testing.T) {
			got, err := Transform("ripgrep-portable-arguments-v1", target, []byte("# Local comment\r\n  --hidden\r\n\t--smart-case\n--line-number\n--case-sensitive\n--ignore-case\n"))
			if err != nil {
				t.Fatal(err)
			}
			want := "--hidden\n--smart-case\n--line-number\n--case-sensitive\n--ignore-case\n"
			if string(got) != want {
				t.Fatalf("portable arguments = %q, want %q", got, want)
			}
			again, err := Transform("ripgrep-portable-arguments-v1", target, got)
			if err != nil || string(again) != want {
				t.Fatalf("canonical arguments are not stable: %q, %v", again, err)
			}
		})
	}
}

func TestRipgrepPortableArgumentsWithholdsWholeUnsupportedFile(t *testing.T) {
	for name, content := range map[string]string{
		"preprocessor":           "--hidden\n--pre=/private/tool\n",
		"separate command value": "--pre\n/private/tool\n",
		"path argument":          "--hidden\n/private/work\n",
		"pattern file":           "--file=/private/patterns\n",
		"ignore file":            "--ignore-file=/private/ignore\n",
		"unknown flag":           "--future-option\n",
		"shell quoting":          "'--hidden'\n",
		"two flags on one line":  "--hidden --smart-case\n",
		"inline comment":         "--hidden # not a comment\n",
		"non-ASCII whitespace":   "\u00a0--hidden\n",
		"invalid text":           "--hidden\n\xff\n",
		"NUL":                    "--hidden\x00\n",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := Transform("ripgrep-portable-arguments-v1", "${home}/.ripgreprc", []byte(content))
			if err == nil || got != nil {
				t.Fatalf("unsupported content became a partial usable file: %q, %v", got, err)
			}
			if strings.Contains(err.Error(), "/private/") {
				t.Fatalf("error disclosed an argument value: %v", err)
			}
		})
	}
}

func TestRipgrepPortableArgumentsRejectsUnreviewedTarget(t *testing.T) {
	if got, err := Transform("ripgrep-portable-arguments-v1", "${home}/other/config", []byte("--hidden\n")); err == nil || got != nil {
		t.Fatalf("unreviewed target became executable: %q, %v", got, err)
	}
}
