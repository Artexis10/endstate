// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PathEnvironment is the bounded host state used to resolve engine-owned path
// coordinates. Tests supply it directly; production builds it from the current
// user and XDG/platform environment.
type PathEnvironment struct {
	GOOS string
	Home string
	Env  map[string]string
}

var engineCoordinates = map[string]map[string]bool{
	"home":                      {"windows": true, "linux": true, "darwin": true},
	"xdg.config":                {"linux": true, "darwin": true},
	"xdg.data":                  {"linux": true, "darwin": true},
	"xdg.state":                 {"linux": true, "darwin": true},
	"xdg.cache":                 {"linux": true, "darwin": true},
	"windows.appdata":           {"windows": true},
	"windows.localAppData":      {"windows": true},
	"darwin.applicationSupport": {"darwin": true},
}

// ValidateEnginePath verifies portable authored host-path syntax without
// evaluating a shell or consulting the filesystem.
func ValidateEnginePath(authored, platform string) error {
	if strings.Contains(authored, "$(") || strings.ContainsRune(authored, '`') ||
		strings.Contains(authored, ":-") || strings.Contains(authored, ":+") || strings.Contains(authored, ":?") {
		return fmt.Errorf("shell evaluation and fallback syntax are not supported")
	}
	coordinate, suffix, ok := splitEngineCoordinate(authored)
	if !ok {
		return fmt.Errorf("host path must begin with an engine-owned coordinate")
	}
	platforms := engineCoordinates[coordinate]
	if platforms == nil {
		return fmt.Errorf("unknown engine path coordinate %q", coordinate)
	}
	if !platforms[platform] {
		return fmt.Errorf("coordinate %q is not valid on %s", coordinate, platform)
	}
	if suffix != "" && !strings.HasPrefix(suffix, "/") && !strings.HasPrefix(suffix, `\`) {
		return fmt.Errorf("coordinate suffix must begin with a path separator")
	}
	if strings.Contains(suffix, "${") {
		return fmt.Errorf("nested expansion is not supported")
	}
	for _, segment := range strings.FieldsFunc(suffix, func(r rune) bool { return r == '/' || r == '\\' }) {
		if segment == ".." {
			return fmt.Errorf("host path traverses a parent directory")
		}
	}
	return nil
}

// ResolveEnginePath expands one validated coordinate to the current host root
// and verifies the cleaned result remains inside that root.
func ResolveEnginePath(authored string, environment PathEnvironment) (string, error) {
	if err := ValidateEnginePath(authored, environment.GOOS); err != nil {
		return "", err
	}
	coordinate, suffix, _ := splitEngineCoordinate(authored)
	root, err := coordinateRoot(coordinate, environment)
	if err != nil {
		return "", err
	}
	cleanRoot := filepath.Clean(root)
	suffix = strings.TrimLeft(suffix, "/\\")
	resolved := filepath.Clean(filepath.Join(cleanRoot, filepath.FromSlash(strings.ReplaceAll(suffix, "\\", "/"))))
	if environment.GOOS != "windows" {
		relative, relErr := filepath.Rel(cleanRoot, resolved)
		if relErr != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("resolved path escapes coordinate root")
		}
	}
	return resolved, nil
}

// ResolveHostEnginePath resolves a coordinate against the current user.
func ResolveHostEnginePath(authored string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	environment := PathEnvironment{GOOS: runtime.GOOS, Home: home, Env: map[string]string{}}
	for _, name := range []string{"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "APPDATA", "LOCALAPPDATA"} {
		if value, ok := os.LookupEnv(name); ok {
			environment.Env[name] = value
		}
	}
	return ResolveEnginePath(authored, environment)
}

func isEngineCoordinatePath(value string) bool {
	coordinate, _, ok := splitEngineCoordinate(value)
	return ok && engineCoordinates[coordinate] != nil
}

func splitEngineCoordinate(value string) (coordinate, suffix string, ok bool) {
	if !strings.HasPrefix(value, "${") {
		return "", "", false
	}
	end := strings.IndexByte(value, '}')
	if end < 3 {
		return "", "", false
	}
	return value[2:end], value[end+1:], true
}

func coordinateRoot(coordinate string, environment PathEnvironment) (string, error) {
	home := strings.TrimSpace(environment.Home)
	if home == "" {
		return "", fmt.Errorf("user home is unavailable")
	}
	env := func(name string) string { return strings.TrimSpace(environment.Env[name]) }
	root := ""
	switch coordinate {
	case "home":
		root = home
	case "xdg.config":
		root = env("XDG_CONFIG_HOME")
		if root == "" {
			root = filepath.Join(home, ".config")
		}
	case "xdg.data":
		root = env("XDG_DATA_HOME")
		if root == "" {
			root = filepath.Join(home, ".local", "share")
		}
	case "xdg.state":
		root = env("XDG_STATE_HOME")
		if root == "" {
			root = filepath.Join(home, ".local", "state")
		}
	case "xdg.cache":
		root = env("XDG_CACHE_HOME")
		if root == "" {
			root = filepath.Join(home, ".cache")
		}
	case "windows.appdata":
		root = env("APPDATA")
		if root == "" {
			root = filepath.Join(home, "AppData", "Roaming")
		}
	case "windows.localAppData":
		root = env("LOCALAPPDATA")
		if root == "" {
			root = filepath.Join(home, "AppData", "Local")
		}
	case "darwin.applicationSupport":
		root = filepath.Join(home, "Library", "Application Support")
	default:
		return "", fmt.Errorf("unknown engine path coordinate %q", coordinate)
	}
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("coordinate %q has no usable root", coordinate)
	}
	return root, nil
}
