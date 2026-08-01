// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

// HostPathPolicy controls the two deliberate host-path exceptions.
type HostPathPolicy struct {
	AllowRoot     bool
	DynamicRoot   string
	InstanceRoot  string
	InstanceAlias string
}

// ResolveHostPath resolves a Windows-authored path using only Context aliases.
func (context *Context) ResolveHostPath(authored string, policy HostPathPolicy) (string, error) {
	fail := func(message string) (string, error) {
		return "", fmt.Errorf("%w: %s", ErrUnsafePath, message)
	}
	if authored == "" || authored != strings.TrimSpace(authored) || strings.ContainsRune(authored, '\x00') {
		return fail("path is empty or malformed")
	}
	authored = NormalizeProductionAuthoredPath(authored)
	if strings.HasPrefix(authored, `\\`) || strings.HasPrefix(authored, "//") || hasWindowsDrivePrefix(authored) || strings.HasPrefix(authored, "~") {
		return fail("raw host paths are forbidden")
	}
	var root, suffix string
	if strings.HasPrefix(strings.ToLower(authored), "${instance.root}") {
		if policy.InstanceRoot != "" {
			var err error
			root, err = context.validateInstanceRoot(policy.InstanceRoot)
			if err != nil {
				return "", err
			}
			if policy.InstanceAlias != "" {
				aliasRoot, ok := context.VirtualRoot(policy.InstanceAlias)
				if !ok || !isAncestorOrSame(aliasRoot, root) {
					return fail("instance alias provenance does not match root")
				}
			}
		} else {
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
		}
		suffix = authored[len("${instance.root}"):]
	} else if strings.HasPrefix(authored, "%") {
		closing := strings.Index(authored[1:], "%")
		if closing < 0 {
			return fail("unterminated environment alias")
		}
		closing++
		name := authored[1:closing]
		if !context.declaredAlias(name) {
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
		return "", fmt.Errorf("%w: resolved path is linked or outside its virtual root", ErrUnsafePath)
	}
	return resolved, nil
}

func (context *Context) declaredAlias(name string) bool {
	if _, ok := canonicalAlias(name); ok {
		return true
	}
	for _, dynamic := range context.descriptor.DynamicRoots {
		if strings.EqualFold(dynamic, name) {
			return true
		}
	}
	return false
}

// NormalizeProductionAuthoredPath maps supported production home paths to the
// canonical declared alias form.
func NormalizeProductionAuthoredPath(value string) string {
	if strings.HasPrefix(value, `~\`) || strings.HasPrefix(value, "~/") {
		return `%USERPROFILE%\` + value[2:]
	}
	return value
}

func (context *Context) validateInstanceRoot(value string) (string, error) {
	fail := func(message string) (string, error) { return "", fmt.Errorf("%w: %s", ErrUnsafePath, message) }
	if value == "" || !filepath.IsAbs(value) || value != filepath.Clean(value) {
		return fail("instance root must be canonical absolute")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return fail("instance root is invalid")
	}
	absolute, err = safepath.CanonicalizePlatformRootAlias(filepath.Clean(absolute))
	if err != nil {
		return fail("instance root is invalid")
	}
	unique := map[string]string{}
	for _, virtual := range context.virtual {
		key := pathComparisonKey(virtual)
		unique[key] = virtual
	}
	contained := 0
	for _, virtual := range unique {
		if !isAncestorOrSame(virtual, absolute) {
			continue
		}
		relative, relErr := filepath.Rel(virtual, absolute)
		if relErr != nil {
			continue
		}
		if relative != "." {
			if _, resolveErr := safepath.Resolve(virtual, filepath.ToSlash(relative)); resolveErr != nil {
				return fail("instance root contains a linked or unsafe component")
			}
		}
		contained++
	}
	if contained != 1 {
		return fail("instance root is not contained in exactly one virtual root")
	}
	return absolute, nil
}

// ValidateSandboxPath accepts only link-free materialized paths under one
// validation virtual root or a narrowly owned engine runtime location.
func (context *Context) ValidateSandboxPath(absolute string) error {
	if context == nil || absolute == "" || !filepath.IsAbs(absolute) || absolute != filepath.Clean(absolute) {
		return fmt.Errorf("%w: sandbox path must be canonical absolute", ErrUnsafePath)
	}
	absolute, err := safepath.CanonicalizePlatformRootAlias(filepath.Clean(absolute))
	if err != nil {
		return fmt.Errorf("%w: sandbox path is invalid", ErrUnsafePath)
	}
	for _, virtual := range context.uniqueVirtualRoots() {
		if !isAncestorOrSame(virtual, absolute) {
			continue
		}
		relative, relErr := filepath.Rel(virtual, absolute)
		if relErr != nil {
			continue
		}
		if relative == "." {
			if err := safepath.ValidateRoot(virtual); err != nil {
				return fmt.Errorf("%w: virtual root is linked or unavailable", ErrUnsafePath)
			}
			return nil
		}
		if _, err := safepath.Resolve(virtual, filepath.ToSlash(relative)); err != nil {
			return fmt.Errorf("%w: sandbox path contains a linked component", ErrUnsafePath)
		}
		return nil
	}
	if absolute == context.root || !isAncestorOrSame(context.root, absolute) {
		return fmt.Errorf("%w: path is outside validation-owned locations", ErrUnsafePath)
	}
	relative, err := filepath.Rel(context.root, absolute)
	if err != nil {
		return fmt.Errorf("%w: path is outside validation-owned locations", ErrUnsafePath)
	}
	portable := filepath.ToSlash(relative)
	owned := portable == ".endstate/"+packageStateFilename
	if !owned {
		for _, directory := range []string{"logs", "manifests", "state"} {
			if portable == directory || strings.HasPrefix(portable, directory+"/") {
				owned = true
				break
			}
		}
	}
	if !owned {
		return fmt.Errorf("%w: path is not an owned validation runtime target", ErrUnsafePath)
	}
	if _, err := safepath.Resolve(context.root, portable); err != nil {
		return fmt.Errorf("%w: owned runtime path contains a linked component", ErrUnsafePath)
	}
	return nil
}

func (context *Context) uniqueVirtualRoots() []string {
	seen := map[string]struct{}{}
	roots := make([]string, 0, len(context.virtual))
	for _, root := range context.virtual {
		key := pathComparisonKey(root)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		roots = append(roots, root)
	}
	sort.Slice(roots, func(i, j int) bool { return pathComparisonKey(roots[i]) < pathComparisonKey(roots[j]) })
	return roots
}

type displayRoot struct {
	name, path string
}

func (context *Context) displayRoots() []displayRoot {
	preferred := []string{"APPDATA", "LOCALAPPDATA", "USERPROFILE", "ProgramFiles", "ProgramFiles(x86)", "ProgramData", "PUBLIC", "SystemRoot", "TEMP"}
	result := make([]displayRoot, 0, len(context.virtual))
	seen := map[string]struct{}{}
	for _, name := range preferred {
		if path, ok := context.VirtualRoot(name); ok {
			key := pathComparisonKey(path)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			result = append(result, displayRoot{name: name, path: path})
		}
	}
	dynamic := append([]string(nil), context.descriptor.DynamicRoots...)
	sort.Strings(dynamic)
	for _, name := range dynamic {
		path, _ := context.VirtualRoot(name)
		key := pathComparisonKey(path)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, displayRoot{name: name, path: path})
	}
	sort.SliceStable(result, func(i, j int) bool { return len(result[i].path) > len(result[j].path) })
	return result
}

// DisplayPath projects an absolute validation path to a non-sensitive token.
func (context *Context) DisplayPath(absolute string) (string, error) {
	if err := context.ValidateSandboxPath(absolute); err != nil {
		return "", err
	}
	for _, root := range context.displayRoots() {
		if !isAncestorOrSame(root.path, absolute) {
			continue
		}
		relative, _ := filepath.Rel(root.path, absolute)
		prefix := "%" + root.name + "%"
		if relative == "." {
			return prefix, nil
		}
		return prefix + `\` + strings.ReplaceAll(filepath.ToSlash(relative), "/", `\`), nil
	}
	relative, _ := filepath.Rel(context.root, absolute)
	return "$ENDSTATE_ROOT/" + filepath.ToSlash(relative), nil
}

// DisplayHostPath uses explicit instance provenance when available; ordinary
// dynamic aliases retain their authored alias identity.
func (context *Context) DisplayHostPath(absolute string, policy HostPathPolicy) (string, error) {
	if err := context.ValidateSandboxPath(absolute); err != nil {
		return "", err
	}
	if policy.InstanceRoot != "" && policy.InstanceAlias != "" {
		root, err := context.validateInstanceRoot(policy.InstanceRoot)
		if err != nil {
			return "", err
		}
		aliasRoot, ok := context.VirtualRoot(policy.InstanceAlias)
		if !ok || !isAncestorOrSame(aliasRoot, root) {
			return "", fmt.Errorf("%w: instance provenance is invalid", ErrUnsafePath)
		}
		if isAncestorOrSame(root, absolute) {
			relative, err := filepath.Rel(root, absolute)
			if err != nil {
				return "", fmt.Errorf("%w: instance display is invalid", ErrUnsafePath)
			}
			if relative == "." {
				return "${instance.root}", nil
			}
			return `${instance.root}\` + strings.ReplaceAll(filepath.ToSlash(relative), "/", `\`), nil
		}
	}
	return context.DisplayPath(absolute)
}

// OriginalHostPath transfers a validated authored suffix to the corresponding
// environment value captured before activation. Wildcards protect their
// non-wildcard parent prefix. Callers must never serialize the returned path.
func (context *Context) OriginalHostPath(authored string, policy HostPathPolicy) (string, error) {
	authored = NormalizeProductionAuthoredPath(authored)
	if strings.HasPrefix(strings.ToLower(authored), "${instance.root}") {
		if policy.InstanceRoot == "" || policy.InstanceAlias == "" {
			return "", fmt.Errorf("%w: instance provenance is incomplete", ErrUnsafePath)
		}
		instanceRoot, err := context.validateInstanceRoot(policy.InstanceRoot)
		if err != nil {
			return "", err
		}
		virtual, ok := context.VirtualRoot(policy.InstanceAlias)
		if !ok || !isAncestorOrSame(virtual, instanceRoot) {
			return "", fmt.Errorf("%w: instance alias provenance does not match root", ErrUnsafePath)
		}
		original, set, managed := context.OriginalEnvironment(policy.InstanceAlias)
		if !managed || !set || strings.TrimSpace(original) == "" {
			return "", nil
		}
		original, err = canonicalGuardPath(filepath.Clean(original), false)
		if err != nil {
			return "", fmt.Errorf("%w: original instance alias is unsafe", ErrUnsafePath)
		}
		instanceSuffix, err := filepath.Rel(virtual, instanceRoot)
		if err != nil || filepath.IsAbs(instanceSuffix) || strings.HasPrefix(instanceSuffix, "..") {
			return "", fmt.Errorf("%w: instance suffix is unsafe", ErrUnsafePath)
		}
		base := original
		if instanceSuffix != "." {
			base, err = safepath.Resolve(original, filepath.ToSlash(instanceSuffix))
			if err != nil {
				return "", fmt.Errorf("%w: original instance root is unsafe", ErrUnsafePath)
			}
		}
		return resolveOriginalSuffix(base, authored[len("${instance.root}"):])
	}
	if !strings.HasPrefix(authored, "%") {
		return "", fmt.Errorf("%w: authored path has no alias", ErrUnsafePath)
	}
	closing := strings.Index(authored[1:], "%")
	if closing < 0 {
		return "", fmt.Errorf("%w: unterminated alias", ErrUnsafePath)
	}
	closing++
	name := authored[1:closing]
	if !context.declaredAlias(name) {
		return "", fmt.Errorf("%w: unknown alias", ErrUnsafePath)
	}
	if _, err := context.ResolveHostPath(strings.ReplaceAll(strings.ReplaceAll(authored, "*", "x"), "?", "x"), HostPathPolicy{AllowRoot: policy.AllowRoot}); err != nil {
		return "", err
	}
	original, set, managed := context.OriginalEnvironment(name)
	if !managed || !set || strings.TrimSpace(original) == "" {
		return "", nil
	}
	original, err := canonicalGuardPath(filepath.Clean(original), false)
	if err != nil {
		return "", fmt.Errorf("%w: original alias root is unsafe", ErrUnsafePath)
	}
	return resolveOriginalSuffix(original, authored[closing+1:])
}

func resolveOriginalSuffix(original, rawSuffix string) (string, error) {
	suffix := strings.TrimLeft(rawSuffix, `\/`)
	if wildcard := strings.IndexAny(suffix, "*?["); wildcard >= 0 {
		prefix := suffix[:wildcard]
		if separator := strings.LastIndexAny(prefix, `\/`); separator >= 0 {
			prefix = prefix[:separator]
		} else {
			prefix = ""
		}
		suffix = prefix
	}
	if suffix == "" {
		return original, nil
	}
	resolved, err := safepath.Resolve(original, filepath.ToSlash(suffix))
	if err != nil {
		return "", fmt.Errorf("%w: original target is linked or outside its alias root", ErrUnsafePath)
	}
	return resolved, nil
}

// ResolvePortablePath resolves a portable payload or bundle path beneath an
// explicit existing root.
func ResolvePortablePath(root, portable string) (string, error) {
	resolved, err := safepath.Resolve(root, portable)
	if err != nil {
		return "", fmt.Errorf("%w: portable path is linked or outside its root", ErrUnsafePath)
	}
	return resolved, nil
}

// ResolveHostPattern resolves a capture-side host pattern through the same
// alias and instance authority as ResolveHostPath while retaining wildcard
// syntax for matching. The non-wildcard probe is resolved first, so traversal,
// unknown aliases, hardcoded paths, and linked parents fail closed.
func (context *Context) ResolveHostPattern(authored string, policy HostPathPolicy) (string, error) {
	probe := authored
	for {
		wildcard := strings.IndexAny(probe, "*?[")
		if wildcard < 0 {
			break
		}
		end := wildcard + 1
		if probe[wildcard] == '[' {
			closing := strings.IndexByte(probe[wildcard+1:], ']')
			if closing < 0 {
				return "", fmt.Errorf("%w: glob syntax is invalid", ErrUnsafePath)
			}
			end = wildcard + closing + 2
		}
		probe = probe[:wildcard] + "x" + probe[end:]
	}
	if _, err := context.ResolveHostPath(probe, policy); err != nil {
		return "", err
	}
	authored = NormalizeProductionAuthoredPath(authored)
	var root, suffix string
	if strings.HasPrefix(strings.ToLower(authored), "${instance.root}") {
		var err error
		root, err = context.validateInstanceRoot(policy.InstanceRoot)
		if err != nil {
			return "", err
		}
		suffix = authored[len("${instance.root}"):]
	} else {
		closing := strings.Index(authored[1:], "%")
		if closing < 0 {
			return "", fmt.Errorf("%w: host pattern has no declared alias", ErrUnsafePath)
		}
		closing++
		root, _ = context.VirtualRoot(authored[1:closing])
		suffix = authored[closing+1:]
	}
	return filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(strings.TrimLeft(suffix, `\/`), `\`, "/"))), nil
}

// GlobSandboxPattern evaluates an already-expanded detector pattern only
// inside validation-owned virtual roots. Every returned match is canonical,
// link-free, and revalidated for containment before it can become production
// instance evidence.
func (context *Context) GlobSandboxPattern(pattern string) ([]string, error) {
	fail := func(message string) ([]string, error) {
		return nil, fmt.Errorf("%w: %s", ErrUnsafePath, message)
	}
	if context == nil || pattern == "" || pattern != strings.TrimSpace(pattern) || strings.ContainsRune(pattern, '\x00') || strings.Contains(pattern, "**") {
		return fail("glob is empty or malformed")
	}
	for _, component := range strings.FieldsFunc(filepath.ToSlash(pattern), func(character rune) bool { return character == '/' }) {
		if component == ".." {
			return fail("glob contains parent traversal")
		}
	}
	absolute, err := filepath.Abs(pattern)
	if err != nil || absolute != filepath.Clean(absolute) {
		return fail("glob is not canonical absolute")
	}
	firstWildcard := strings.IndexAny(absolute, "*?[")
	anchor := absolute
	if firstWildcard >= 0 {
		prefix := absolute[:firstWildcard]
		separator := strings.LastIndexAny(prefix, `\/`)
		if separator < 0 {
			return fail("glob has no contained anchor")
		}
		anchor = strings.TrimRight(prefix[:separator], `\/`)
	}
	if anchor == "" || context.ValidateSandboxPath(filepath.Clean(anchor)) != nil {
		return fail("glob anchor is outside validation roots")
	}
	matches, err := filepath.Glob(absolute)
	if err != nil {
		return fail("glob syntax is invalid")
	}
	result := make([]string, 0, len(matches))
	for _, match := range matches {
		canonical, canonicalErr := filepath.Abs(filepath.Clean(match))
		if canonicalErr != nil || context.ValidateSandboxPath(canonical) != nil {
			return fail("glob returned an unsafe match")
		}
		if _, statErr := os.Lstat(canonical); statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
			return fail("glob match cannot be inspected")
		}
		result = append(result, canonical)
	}
	sort.Slice(result, func(left, right int) bool { return pathComparisonKey(result[left]) < pathComparisonKey(result[right]) })
	return result, nil
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
