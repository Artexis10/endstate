// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmharvest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

const (
	maxProbeOutput  = 1 << 20
	maxProbeTargets = 1024
)

var (
	probeProgramPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
	probeOptionPattern  = regexp.MustCompile(`^programs\.[a-zA-Z0-9][a-zA-Z0-9_-]*(?:\.[a-zA-Z0-9][a-zA-Z0-9_-]*)+$`)
	probeSystemPattern  = regexp.MustCompile(`^[a-zA-Z0-9_+-]+-linux$`)
)

type commandRunner func(name string, args ...string) (stdout, stderr []byte, err error)

// NixTargetProber performs a pure, fixed-argument evaluation of the exact
// release inputs. It subtracts Home Manager's baseline files in Nix so callers
// receive only targets owned by the enabled program.
type NixTargetProber struct {
	inputs releaseinputs.Inputs
	system string
	run    commandRunner
}

func NewNixTargetProber(inputs releaseinputs.Inputs, system string) *NixTargetProber {
	return newNixTargetProber(inputs, system, runBoundedCommand)
}

func newNixTargetProber(inputs releaseinputs.Inputs, system string, run commandRunner) *NixTargetProber {
	return &NixTargetProber{inputs: inputs, system: system, run: run}
}

func (p *NixTargetProber) ProbeTargets(program string, values map[string]any) ([]string, error) {
	if p == nil || p.run == nil {
		return nil, fmt.Errorf("Nix target prober is unavailable")
	}
	expression, err := renderProbeExpression(p.inputs, p.system, program, values)
	if err != nil {
		return nil, err
	}
	stdout, stderr, runErr := p.run(
		"nix", "eval", "--json", "--option", "pure-eval", "true", "--expr", expression,
	)
	if runErr != nil {
		detail := strings.TrimSpace(string(stderr))
		if detail == "" {
			return nil, fmt.Errorf("pure Nix evaluation failed: %w", runErr)
		}
		return nil, fmt.Errorf("pure Nix evaluation failed: %w: %s", runErr, detail)
	}
	var targets []string
	decoder := json.NewDecoder(bytes.NewReader(stdout))
	if err := decoder.Decode(&targets); err != nil {
		return nil, fmt.Errorf("decode pure Nix target probe: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	if len(targets) > maxProbeTargets {
		return nil, fmt.Errorf("pure Nix target probe returned more than %d targets", maxProbeTargets)
	}
	for _, target := range targets {
		if len(target) > 4096 {
			return nil, fmt.Errorf("pure Nix target probe returned an oversized target")
		}
	}
	return targets, nil
}

func renderProbeExpression(inputs releaseinputs.Inputs, system, program string, values map[string]any) (string, error) {
	if !probeSystemPattern.MatchString(system) {
		return "", fmt.Errorf("probe system %q is not a Linux Nix system", system)
	}
	if !probeProgramPattern.MatchString(program) {
		return "", fmt.Errorf("probe program %q is unsafe", program)
	}
	keys := make([]string, 0, len(values))
	for name := range values {
		if !probeOptionPattern.MatchString(name) || !strings.HasPrefix(name, "programs."+program+".") {
			return "", fmt.Errorf("probe option %q is outside programs.%s", name, program)
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)

	var assignments strings.Builder
	fmt.Fprintf(&assignments, "    programs.%s.enable = true;\n", program)
	for _, name := range keys {
		value, err := renderNixValue(values[name])
		if err != nil {
			return "", fmt.Errorf("render probe option %q: %w", name, err)
		}
		fmt.Fprintf(&assignments, "    %s = %s;\n", name, value)
	}

	nixpkgsRef, err := renderNixString(inputs.Nixpkgs.FlakeRef)
	if err != nil {
		return "", err
	}
	homeManagerRef, err := renderNixString(inputs.HomeManager.FlakeRef)
	if err != nil {
		return "", err
	}
	nixSystem, err := renderNixString(system)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(`let
  nixpkgs = builtins.getFlake %s;
  homeManager = builtins.getFlake %s;
  pkgs = nixpkgs.legacyPackages.%s;
  evaluate = module:
    builtins.attrNames ((homeManager.lib.homeManagerConfiguration {
      inherit pkgs;
      modules = [
        {
          home.username = "endstate-probe";
          home.homeDirectory = "%s";
          home.stateVersion = "24.11";
        }
        module
      ];
    }).config.home.file);
  baseline = evaluate { };
  configured = evaluate {
%s  };
in
  builtins.filter (target: !(builtins.elem target baseline)) configured`, nixpkgsRef, homeManagerRef, nixSystem, probeHome, assignments.String()), nil
}

func renderNixValue(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "null", nil
	case bool:
		return strconv.FormatBool(typed), nil
	case string:
		return renderNixString(typed)
	case json.Number:
		if _, err := typed.Float64(); err != nil {
			return "", fmt.Errorf("invalid number %q", typed)
		}
		return typed.String(), nil
	case float64:
		if math.IsInf(typed, 0) || math.IsNaN(typed) {
			return "", fmt.Errorf("number is not finite")
		}
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	case float32:
		if math.IsInf(float64(typed), 0) || math.IsNaN(float64(typed)) {
			return "", fmt.Errorf("number is not finite")
		}
		return strconv.FormatFloat(float64(typed), 'g', -1, 32), nil
	case int:
		return strconv.Itoa(typed), nil
	case int8:
		return strconv.FormatInt(int64(typed), 10), nil
	case int16:
		return strconv.FormatInt(int64(typed), 10), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case uint:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint64:
		return strconv.FormatUint(typed, 10), nil
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			rendered, err := renderNixValue(item)
			if err != nil {
				return "", err
			}
			items = append(items, rendered)
		}
		return "[ " + strings.Join(items, " ") + " ]", nil
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		attributes := make([]string, 0, len(keys))
		for _, key := range keys {
			renderedKey, err := renderNixString(key)
			if err != nil {
				return "", err
			}
			renderedValue, err := renderNixValue(typed[key])
			if err != nil {
				return "", err
			}
			attributes = append(attributes, renderedKey+" = "+renderedValue+";")
		}
		return "{ " + strings.Join(attributes, " ") + " }", nil
	default:
		return "", fmt.Errorf("unsupported value type %T", value)
	}
}

func renderNixString(value string) (string, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("string is not valid UTF-8")
	}
	var rendered strings.Builder
	rendered.WriteByte('"')
	for index := 0; index < len(value); {
		r, size := utf8.DecodeRuneInString(value[index:])
		switch r {
		case '\\':
			rendered.WriteString(`\\`)
		case '"':
			rendered.WriteString(`\"`)
		case '\n':
			rendered.WriteString(`\n`)
		case '\r':
			rendered.WriteString(`\r`)
		case '\t':
			rendered.WriteString(`\t`)
		case '$':
			if index+size < len(value) && value[index+size] == '{' {
				rendered.WriteString(`\$`)
			} else {
				rendered.WriteRune(r)
			}
		default:
			if r < 0x20 || r == 0x7f {
				return "", fmt.Errorf("string contains unsupported control character")
			}
			rendered.WriteRune(r)
		}
		index += size
	}
	rendered.WriteByte('"')
	return rendered.String(), nil
}

type limitedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (w *limitedBuffer) Write(data []byte) (int, error) {
	originalLength := len(data)
	remaining := w.limit - w.buffer.Len()
	if remaining <= 0 {
		w.exceeded = true
		return originalLength, nil
	}
	if len(data) > remaining {
		w.exceeded = true
		data = data[:remaining]
	}
	_, _ = w.buffer.Write(data)
	return originalLength, nil
}

func runBoundedCommand(name string, args ...string) ([]byte, []byte, error) {
	stdout := &limitedBuffer{limit: maxProbeOutput}
	stderr := &limitedBuffer{limit: maxProbeOutput}
	command := exec.Command(name, args...)
	command.Stdout = stdout
	command.Stderr = stderr
	err := command.Run()
	if stdout.exceeded || stderr.exceeded {
		return stdout.buffer.Bytes(), stderr.buffer.Bytes(), fmt.Errorf("command output exceeded %d bytes", maxProbeOutput)
	}
	return stdout.buffer.Bytes(), stderr.buffer.Bytes(), err
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("decode pure Nix target probe: multiple JSON values")
		}
		return fmt.Errorf("decode pure Nix target probe: %w", err)
	}
	return nil
}
