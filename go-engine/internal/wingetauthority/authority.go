// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package wingetauthority binds hosted Winget launches to a trusted executable.
package wingetauthority

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	StrictEnvironment    = "ENDSTATE_INTERNAL_HOSTED_WINGET_STRICT_V1"
	AuthorityEnvironment = "ENDSTATE_INTERNAL_HOSTED_WINGET_AUTHORITY_V1"
	StrictValue          = "v1"
)

var errInvalidAuthority = errors.New("invalid Winget authority")

const maxCapabilityLength = 4096

// Encode creates the private hosted Winget authority capability.
func Encode(path string, digest [32]byte) (string, error) {
	if !validCapabilityPath(path) {
		return "", errInvalidAuthority
	}
	value := StrictValue + ":" + base64.RawURLEncoding.EncodeToString([]byte(path)) + ":" + hex.EncodeToString(digest[:])
	if len(value) > maxCapabilityLength {
		return "", errInvalidAuthority
	}
	return value, nil
}

// Decode parses a private hosted Winget authority capability.
func Decode(value string) (string, [32]byte, error) {
	parts := strings.Split(value, ":")
	if len(value) > maxCapabilityLength || len(parts) != 3 || parts[0] != StrictValue || len(parts[1]) == 0 || len(parts[2]) != 64 || strings.ToLower(parts[2]) != parts[2] {
		return "", [32]byte{}, errInvalidAuthority
	}
	path, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !validCapabilityPath(string(path)) || base64.RawURLEncoding.EncodeToString(path) != parts[1] {
		return "", [32]byte{}, errInvalidAuthority
	}
	decoded, err := hex.DecodeString(parts[2])
	if err != nil || len(decoded) != 32 {
		return "", [32]byte{}, errInvalidAuthority
	}
	var digest [32]byte
	copy(digest[:], decoded)
	return string(path), digest, nil
}

// RequireHosted validates the private hosted environment boundary.
func RequireHosted(environment map[string]string) error {
	marker, markerPresent, markerInvalid := environmentValue(environment, StrictEnvironment)
	capability, capabilityPresent, capabilityInvalid := environmentValue(environment, AuthorityEnvironment)
	if markerInvalid || capabilityInvalid || !markerPresent || !capabilityPresent || marker != StrictValue {
		return errInvalidAuthority
	}
	_, _, err := Decode(capability)
	if err != nil {
		return errInvalidAuthority
	}
	return nil
}

// Command creates a Winget command bound to hosted authority when present.
func Command(args ...string) (*exec.Cmd, func(), error) {
	return CommandWith(exec.Command, args...)
}

// CommandContext creates a context-bound Winget command.
func CommandContext(ctx context.Context, args ...string) (*exec.Cmd, func(), error) {
	if ctx == nil {
		return nil, nil, errInvalidAuthority
	}
	return CommandWith(func(name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, name, args...)
	}, args...)
}

// CommandWith creates a Winget command through builder.
func CommandWith(builder func(string, ...string) *exec.Cmd, args ...string) (*exec.Cmd, func(), error) {
	if builder == nil {
		return nil, nil, errInvalidAuthority
	}
	environment := currentEnvironment()
	marker, markerPresent, markerInvalid := environmentValue(environment, StrictEnvironment)
	capability, capabilityPresent, capabilityInvalid := environmentValue(environment, AuthorityEnvironment)
	if markerInvalid || capabilityInvalid {
		return nil, nil, errInvalidAuthority
	}
	if !markerPresent && !capabilityPresent {
		command := builder("winget", args...)
		if command == nil {
			return nil, nil, errInvalidAuthority
		}
		return command, func() {}, nil
	}
	if !markerPresent || !capabilityPresent || marker != StrictValue {
		return nil, nil, errInvalidAuthority
	}
	path, digest, err := Decode(capability)
	if err != nil {
		return nil, nil, errInvalidAuthority
	}
	release, err := bindStrict(path, digest)
	if err != nil {
		return nil, nil, errInvalidAuthority
	}
	command := builder(path, args...)
	if command == nil {
		release()
		return nil, nil, errInvalidAuthority
	}
	childEnvironment := command.Env
	if childEnvironment == nil {
		childEnvironment = os.Environ()
	}
	command.Env = withoutAuthority(childEnvironment)
	return command, release, nil
}

func currentEnvironment() map[string]string {
	environment := make(map[string]string)
	for _, value := range os.Environ() {
		key, value, found := strings.Cut(value, "=")
		if found {
			environment[key] = value
		}
	}
	return environment
}

func environmentValue(environment map[string]string, name string) (string, bool, bool) {
	var value string
	found := false
	for key, candidate := range environment {
		if strings.EqualFold(key, name) {
			if found {
				return "", false, true
			}
			value, found = candidate, true
		}
	}
	return value, found, false
}

func validCapabilityPath(path string) bool {
	if path == "" || !utf8.ValidString(path) {
		return false
	}
	for _, value := range path {
		if unicode.IsControl(value) {
			return false
		}
	}
	return true
}

func withoutAuthority(environment []string) []string {
	filtered := make([]string, 0, len(environment))
	for _, value := range environment {
		key, _, found := strings.Cut(value, "=")
		if found && (strings.EqualFold(key, StrictEnvironment) || strings.EqualFold(key, AuthorityEnvironment)) {
			continue
		}
		filtered = append(filtered, value)
	}
	return filtered
}
