// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"path/filepath"
	"testing"
)

func TestResolveEnginePathUsesXDGDefaultsAndOverrides(t *testing.T) {
	environment := PathEnvironment{GOOS: "linux", Home: "/home/tester", Env: map[string]string{}}
	got, err := ResolveEnginePath(`${xdg.config}/ripgrep/ripgreprc`, environment)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.FromSlash("/home/tester/.config/ripgrep/ripgreprc") {
		t.Fatalf("default XDG config path = %q", got)
	}
	environment.Env["XDG_CONFIG_HOME"] = "/mnt/config"
	got, err = ResolveEnginePath(`${xdg.config}/ripgrep/ripgreprc`, environment)
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.FromSlash("/mnt/config/ripgrep/ripgreprc") {
		t.Fatalf("custom XDG config path = %q", got)
	}
}

func TestResolveEnginePathSupportsPlatformRoots(t *testing.T) {
	windows := PathEnvironment{GOOS: "windows", Home: `C:\Users\Hugo`, Env: map[string]string{"APPDATA": `C:\Users\Hugo\AppData\Roaming`}}
	if got, err := ResolveEnginePath(`${windows.appdata}/ripgrep/config`, windows); err != nil || got != filepath.Clean(`C:\Users\Hugo\AppData\Roaming/ripgrep/config`) {
		t.Fatalf("Windows appdata = %q, %v", got, err)
	}
	darwin := PathEnvironment{GOOS: "darwin", Home: "/Users/hugo", Env: map[string]string{}}
	if got, err := ResolveEnginePath(`${darwin.applicationSupport}/App/config`, darwin); err != nil || got != filepath.FromSlash("/Users/hugo/Library/Application Support/App/config") {
		t.Fatalf("Darwin application support = %q, %v", got, err)
	}
}

func TestValidateEnginePathRejectsShellSyntaxTraversalAndWrongPlatform(t *testing.T) {
	for name, path := range map[string]string{
		"command substitution":     `${home}/$(whoami)`,
		"shell fallback":           `${XDG_CONFIG_HOME:-$HOME/.config}/app`,
		"backticks":                "${home}/`whoami`",
		"traversal":                `${xdg.config}/../.ssh/id_ed25519`,
		"relative":                 `app/config`,
		"unknown coordinate":       `${xdg.confgi}/app`,
		"coordinate concatenation": `${home}.ssh/config`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateEnginePath(path, "linux"); err == nil {
				t.Fatalf("path %q was accepted", path)
			}
		})
	}
	if err := ValidateEnginePath(`${windows.appdata}/app`, "linux"); err == nil {
		t.Fatal("Linux variant accepted a Windows coordinate")
	}
	if err := ValidateEnginePath(`${xdg.config}/app`, "linux"); err != nil {
		t.Fatalf("valid Linux coordinate rejected: %v", err)
	}
}
