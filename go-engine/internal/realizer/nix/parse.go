// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package nix

import (
	"bufio"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ActivityType subset (Nix src/libutil/logging.hh), confirmed against real
// Determinate Nix 3.21.0 internal-json output.
const (
	actFileTransfer = 101
	actRealise      = 102
	actBuilds       = 104
	actBuild        = 105
	actSubstitute   = 108
)

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// nixEvent is one parsed `@nix {...}` internal-json line. The `msg` TEXT is NOT
// stable across Nix releases; only the locked anchor table consults it, and the
// structural signals (activity type, generation advance) carry the stage.
type nixEvent struct {
	Action string `json:"action"`
	Level  int    `json:"level"`
	Msg    string `json:"msg"`
	RawMsg string `json:"raw_msg"`
	Type   int    `json:"type"`
}

// parsedLog is the structured view of a nix invocation's internal-json stderr.
type parsedLog struct {
	errorMsgs   []string    // level<=1 text, ANSI-stripped (raw -> error.detail only)
	startedActs map[int]int // ActivityType -> count
	blob        string      // lowercased concat of level<=1 msgs (for anchor matching)
}

func (p parsedLog) sawBuild() bool {
	return p.startedActs[actBuild] > 0 || p.startedActs[actBuilds] > 0 || p.startedActs[actRealise] > 0
}

func (p parsedLog) sawDownload() bool {
	return p.startedActs[actFileTransfer] > 0 || p.startedActs[actSubstitute] > 0
}

// parseInternalJSON extracts structured signals from `nix ... --log-format
// internal-json` stderr. Lines that are not `@nix {...}` are ignored.
func parseInternalJSON(stderr []byte) parsedLog {
	p := parsedLog{startedActs: map[int]int{}}
	var sb strings.Builder
	sc := bufio.NewScanner(strings.NewReader(string(stderr)))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "@nix ") {
			continue
		}
		var ev nixEvent
		if json.Unmarshal([]byte(line[5:]), &ev) != nil {
			continue
		}
		switch ev.Action {
		case "msg":
			if ev.Level <= 1 {
				txt := ev.Msg
				if txt == "" {
					txt = ev.RawMsg
				}
				txt = stripANSI(strings.TrimSpace(txt))
				if txt != "" {
					p.errorMsgs = append(p.errorMsgs, txt)
					sb.WriteString(strings.ToLower(txt))
					sb.WriteByte('\n')
				}
			}
		case "start":
			p.startedActs[ev.Type]++
		}
	}
	p.blob = sb.String()
	return p
}

// stageOf names the pipeline stage from structural signals (informational; feeds
// realizer.Error.Stage and a future progress UI).
func stageOf(p parsedLog, advanced bool) string {
	switch {
	case advanced:
		return "commit"
	case p.sawBuild():
		return "build"
	case p.sawDownload():
		return "fetch"
	default:
		return "eval"
	}
}

// profileElement is one entry of `nix profile list --json`.
type profileElement struct {
	StorePaths  []string `json:"storePaths"`
	AttrPath    string   `json:"attrPath"`
	OriginalURL string   `json:"originalUrl"`
	URL         string   `json:"url"`
}

// parseProfileList parses `nix profile list --json`. The Nix 3.x shape is
// {version, elements: {<name>: {...}}} (a name-keyed OBJECT); older Nix used an
// array. Both are handled defensively. Empty/blank input yields an empty set.
func parseProfileList(data []byte) (realizer.Set, error) {
	set := realizer.Set{Elements: map[string]realizer.Element{}}
	if len(strings.TrimSpace(string(data))) == 0 {
		return set, nil
	}
	var probe struct {
		Version  int             `json:"version"`
		Elements json.RawMessage `json:"elements"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return set, err
	}
	trimmed := strings.TrimSpace(string(probe.Elements))
	if trimmed == "" || trimmed == "null" {
		return set, nil
	}
	switch trimmed[0] {
	case '{': // v3 name-keyed object
		var obj map[string]profileElement
		if err := json.Unmarshal(probe.Elements, &obj); err != nil {
			return set, err
		}
		for name, e := range obj {
			set.Elements[name] = realizer.Element{Name: name, AttrPath: e.AttrPath, StorePaths: e.StorePaths}
		}
	case '[': // legacy array
		var arr []profileElement
		if err := json.Unmarshal(probe.Elements, &arr); err != nil {
			return set, err
		}
		for _, e := range arr {
			name := attrLeaf(e.AttrPath)
			if name == "" {
				name = attrLeaf(e.OriginalURL)
			}
			if name == "" {
				continue
			}
			set.Elements[name] = realizer.Element{Name: name, AttrPath: e.AttrPath, StorePaths: e.StorePaths}
		}
	}
	return set, nil
}

// attrLeaf returns the trailing attribute of an installable/attrPath: the part
// after the last '#', else after the last '.', else the input unchanged.
func attrLeaf(s string) string {
	if i := strings.LastIndex(s, "#"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[i+1:]
	}
	return s
}
