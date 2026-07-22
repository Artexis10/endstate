// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"fmt"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// HostPathPolicy controls the two deliberate host-path exceptions.
type HostPathPolicy struct {
	AllowRoot   bool
	DynamicRoot string
}

// ResolveHostPath resolves a Windows-authored path using only Context aliases.
func (context *Context) ResolveHostPath(authored string, policy HostPathPolicy) (string, error) {
	fail := func(message string) (string, error) {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, message)
	}
	if authored == "" || authored != strings.TrimSpace(authored) || strings.ContainsRune(authored, '\x00') {
		return fail("path is empty or malformed")
	}
	if strings.HasPrefix(authored, `\\`) || strings.HasPrefix(authored, "//") || hasWindowsDrivePrefix(authored) || strings.HasPrefix(authored, "~") {
		return fail("raw host paths are forbidden")
	}
	var root, suffix string
	if strings.HasPrefix(strings.ToLower(authored), "${instance.root}") {
		if policy.DynamicRoot == "" {
			return fail("dynamic root was not selected")
		}
		var declared bool
		for _, name := range context.descriptor.DynamicRoots {
			if strings.EqualFold(name, policy.DynamicRoot) {
				root, declared = context.VirtualRoot(name)
				break
			}
		}
		if !declared {
			return fail("dynamic root is not declared")
		}
		suffix = authored[len("${instance.root}"):]
	} else if strings.HasPrefix(authored, "%") {
		closing := strings.Index(authored[1:], "%")
		if closing < 0 {
			return fail("unterminated environment alias")
		}
		closing++
		name := authored[1:closing]
		if _, canonical := canonicalAlias(name); !canonical {
			return fail("unknown environment alias")
		}
		root, _ = context.VirtualRoot(name)
		suffix = authored[closing+1:]
	} else {
		return fail("path must begin with a declared alias")
	}
	if suffix == "" {
		if policy.AllowRoot {
			return root, nil
		}
		return fail("virtual root itself is not allowed")
	}
	if suffix[0] != '\\' && suffix[0] != '/' {
		return fail("alias must end at a component boundary")
	}
	suffix = suffix[1:]
	if suffix == "" || strings.ContainsAny(suffix, `%$~:<>`) {
		return fail("suffix contains a placeholder or unsafe character")
	}
	resolved, err := safepath.Resolve(root, suffix)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafePath, err)
	}
	return resolved, nil
}

// ResolvePortablePath resolves a portable payload or bundle path beneath an
// explicit existing root.
func ResolvePortablePath(root, portable string) (string, error) {
	resolved, err := safepath.Resolve(root, portable)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafePath, err)
	}
	return resolved, nil
}

func canonicalAlias(name string) (string, bool) {
	for candidate := range canonicalRootRelative {
		if strings.EqualFold(candidate, name) {
			return candidate, true
		}
	}
	return "", false
}

func hasWindowsDrivePrefix(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':'
}
