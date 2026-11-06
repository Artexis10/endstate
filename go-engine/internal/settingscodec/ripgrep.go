// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package settingscodec

import (
	"bytes"
	"fmt"
	"unicode/utf8"
)

const RipgrepPortableArgumentsV1 = "ripgrep-portable-arguments-v1"

func ripgrepTargetSupported(target string) bool {
	// These aliases are the closed target contract for this versioned codec.
	switch target {
	case "${home}/.ripgreprc", "${xdg.config}/ripgrep/config", "${xdg.config}/ripgrep/ripgreprc":
		return true
	default:
		return false
	}
}

func portableRipgrepArguments(data []byte) ([]byte, error) {
	if !utf8.Valid(data) || bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("ripgrep configuration is not portable text")
	}
	var output bytes.Buffer
	for index, line := range bytes.Split(data, []byte{'\n'}) {
		// ripgrep's reader trims ASCII whitespace and treats each line as one argument.
		argument := bytes.Trim(line, " \t\n\r\v\f")
		if len(argument) == 0 || argument[0] == '#' {
			continue
		}
		// This closed flag subset defines v1's portable protocol, not inferred intent.
		switch string(argument) {
		case "--hidden", "--no-hidden", "--smart-case", "--case-sensitive", "--ignore-case",
			"--line-number", "--no-line-number", "--column", "--no-column",
			"--heading", "--no-heading", "--fixed-strings", "--word-regexp",
			"--line-regexp", "--invert-match", "--trim", "--no-trim":
			output.Write(argument)
			output.WriteByte('\n')
		default:
			return nil, fmt.Errorf("ripgrep configuration has an unsupported portable argument at line %d", index+1)
		}
	}
	return output.Bytes(), nil
}
