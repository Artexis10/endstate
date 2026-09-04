// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package hmharvest

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

func TestNixTargetProberUsesPinnedPureFixedArgEvaluation(t *testing.T) {
	inputs, err := releaseinputs.Load()
	if err != nil {
		t.Fatal(err)
	}
	var gotName string
	var gotArgs []string
	prober := newNixTargetProber(inputs, "x86_64-linux", func(name string, args ...string) ([]byte, []byte, error) {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return []byte(`[
  "/home/endstate-probe/.config/ripgrep/ripgreprc"
]`), nil, nil
	})

	targets, err := prober.ProbeTargets("ripgrep", map[string]any{
		"programs.ripgrep.arguments": []any{"--hidden", "--glob=${HOME}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "nix" {
		t.Fatalf("command = %q", gotName)
	}
	wantPrefix := []string{"eval", "--json", "--option", "pure-eval", "true", "--expr"}
	if len(gotArgs) != len(wantPrefix)+1 || !reflect.DeepEqual(gotArgs[:len(wantPrefix)], wantPrefix) {
		t.Fatalf("args = %#v", gotArgs)
	}
	expression := gotArgs[len(gotArgs)-1]
	for _, fragment := range []string{
		inputs.Nixpkgs.FlakeRef,
		inputs.HomeManager.FlakeRef,
		`programs.ripgrep.enable = true;`,
		`programs.ripgrep.arguments = [ "--hidden" "--glob=\${HOME}" ];`,
		`baseline = evaluate { };`,
		`builtins.filter (target: !(builtins.elem target baseline)) configured`,
	} {
		if !strings.Contains(expression, fragment) {
			t.Fatalf("expression omitted %q:\n%s", fragment, expression)
		}
	}
	if !reflect.DeepEqual(targets, []string{"/home/endstate-probe/.config/ripgrep/ripgreprc"}) {
		t.Fatalf("targets = %#v", targets)
	}
}

func TestNixTargetProberRejectsUnsafeInputAndBadOutput(t *testing.T) {
	inputs, err := releaseinputs.Load()
	if err != nil {
		t.Fatal(err)
	}
	prober := newNixTargetProber(inputs, "x86_64-linux", func(string, ...string) ([]byte, []byte, error) {
		return []byte(`not-json`), nil, nil
	})
	if _, err := prober.ProbeTargets("ripgrep; builtins.abort", map[string]any{}); err == nil {
		t.Fatal("unsafe program was accepted")
	}
	if _, err := prober.ProbeTargets("ripgrep", map[string]any{"programs.other.settings": true}); err == nil {
		t.Fatal("foreign option was accepted")
	}
	if _, err := prober.ProbeTargets("ripgrep", map[string]any{}); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("bad output error = %v", err)
	}

	prober = newNixTargetProber(inputs, "x86_64-linux", func(string, ...string) ([]byte, []byte, error) {
		return nil, []byte("evaluation failed"), errors.New("exit status 1")
	})
	if _, err := prober.ProbeTargets("ripgrep", map[string]any{}); err == nil || !strings.Contains(err.Error(), "evaluation failed") {
		t.Fatalf("command error = %v", err)
	}
}

func TestRenderNixValueIsDeterministic(t *testing.T) {
	first, err := renderNixValue(map[string]any{"z": true, "a": []any{float64(2), "x"}})
	if err != nil {
		t.Fatal(err)
	}
	second, err := renderNixValue(map[string]any{"a": []any{float64(2), "x"}, "z": true})
	if err != nil {
		t.Fatal(err)
	}
	if first != second || first != `{ "a" = [ 2 "x" ]; "z" = true; }` {
		t.Fatalf("rendered values = %q and %q", first, second)
	}
}
