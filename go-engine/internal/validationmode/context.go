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
	"sync"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

const (
	TestModeEnvironment = "ENDSTATE_TESTMODE"
	RootEnvironment     = "ENDSTATE_ROOT"
)

var (
	setEnvironment   = os.Setenv
	unsetEnvironment = os.Unsetenv
)

type originalEnvironmentValue struct {
	value string
	set   bool
}

// Context contains validation authority. Its host paths are intentionally
// available only through methods and have no JSON or text representation.
type Context struct {
	root       string
	descriptor Descriptor
	virtual    map[string]string
	original   map[string]originalEnvironmentValue
}

var canonicalRootRelative = map[string]string{
	"APPDATA":           "sandbox/appdata",
	"LOCALAPPDATA":      "sandbox/localappdata",
	"USERPROFILE":       "sandbox/userprofile",
	"ProgramFiles":      "sandbox/program-files",
	"ProgramW6432":      "sandbox/program-files",
	"ProgramFiles(x86)": "sandbox/program-files-x86",
	"ProgramData":       "sandbox/program-data",
	"PUBLIC":            "sandbox/public",
	"SystemRoot":        "sandbox/windows",
	"WINDIR":            "sandbox/windows",
	"TEMP":              "sandbox/temp",
	"TMP":               "sandbox/temp",
}

var reservedEnvironmentNames = func() map[string]struct{} {
	result := map[string]struct{}{
		"path": {}, "pathext": {}, "comspec": {}, "psmodulepath": {},
		"homedrive": {}, "homepath": {}, "username": {}, "userdomain": {},
		"computername": {}, "processor_architecture": {}, "number_of_processors": {},
		"endstate_root": {}, "endstate_testmode": {},
	}
	for name := range canonicalRootRelative {
		result[strings.ToLower(name)] = struct{}{}
	}
	return result
}()

// LoadFromEnvironment validates validation-mode authority without mutating the
// filesystem, environment, package state, or host. A nil context means inactive.
func LoadFromEnvironment() (*Context, error) {
	activation := os.Getenv(TestModeEnvironment)
	if activation == "" {
		return nil, nil
	}
	if activation != "1" {
		return nil, fmt.Errorf("%w: %s must be exactly 1", ErrInvalidActivation, TestModeEnvironment)
	}
	root, err := validateValidationRoot(os.Getenv(RootEnvironment), os.TempDir())
	if err != nil {
		return nil, err
	}
	descriptor, err := loadDescriptor(root)
	if err != nil {
		return nil, err
	}
	return newContext(root, descriptor), nil
}

// ActivateFromEnvironment loads validation mode, creates its derived roots,
// installs their environment aliases, and returns an idempotent restoration.
func ActivateFromEnvironment() (*Context, func() error, error) {
	context, err := LoadFromEnvironment()
	if err != nil {
		return nil, nil, err
	}
	if context == nil {
		return nil, func() error { return nil }, nil
	}
	if err := context.createVirtualRoots(); err != nil {
		return nil, nil, err
	}
	names := context.environmentNames()
	context.original = make(map[string]originalEnvironmentValue, len(names))
	for _, name := range names {
		value, set := os.LookupEnv(name)
		context.original[name] = originalEnvironmentValue{value: value, set: set}
	}
	installed := make([]string, 0, len(names))
	for _, name := range names {
		value, _ := context.VirtualRoot(name)
		if err := setEnvironment(name, value); err != nil {
			_ = restoreEnvironment(installed, context.original)
			return nil, nil, fmt.Errorf("activate validation environment %s: %w", name, err)
		}
		installed = append(installed, name)
	}
	var once sync.Once
	var restoreErr error
	restore := func() error {
		once.Do(func() { restoreErr = restoreEnvironment(names, context.original) })
		return restoreErr
	}
	return context, restore, nil
}

func newContext(root string, descriptor Descriptor) *Context {
	virtual := make(map[string]string, len(canonicalRootRelative)+len(descriptor.DynamicRoots))
	for name, relative := range canonicalRootRelative {
		virtual[name] = filepath.Join(root, filepath.FromSlash(relative))
	}
	for _, name := range descriptor.DynamicRoots {
		virtual[name] = filepath.Join(root, "sandbox", "dynamic", name)
	}
	return &Context{root: root, descriptor: descriptor, virtual: virtual}
}

func validateValidationRoot(value, originalTemp string) (string, error) {
	if value == "" || !filepath.IsAbs(value) || value != filepath.Clean(value) {
		return "", fmt.Errorf("%w: root must be canonical absolute", ErrUnsafeRoot)
	}
	root, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafeRoot, err)
	}
	root, err = safepath.CanonicalizePlatformRootAlias(filepath.Clean(root))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafeRoot, err)
	}
	if err := safepath.ValidateRoot(root); err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnsafeRoot, err)
	}
	temp, err := filepath.Abs(originalTemp)
	if err != nil {
		return "", fmt.Errorf("%w: temp: %v", ErrUnsafeRoot, err)
	}
	temp, err = safepath.CanonicalizePlatformRootAlias(filepath.Clean(temp))
	if err != nil {
		return "", fmt.Errorf("%w: temp: %v", ErrUnsafeRoot, err)
	}
	comparisonRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize root: %v", ErrUnsafeRoot, err)
	}
	comparisonTemp, err := filepath.EvalSymlinks(temp)
	if err != nil {
		return "", fmt.Errorf("%w: canonicalize temp: %v", ErrUnsafeRoot, err)
	}
	relative, err := filepath.Rel(comparisonTemp, comparisonRoot)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: root must be a strict temp descendant", ErrUnsafeRoot)
	}
	return root, nil
}

func (context *Context) createVirtualRoots() error {
	unique := make(map[string]struct{}, len(context.virtual))
	for _, absolute := range context.virtual {
		relative, err := filepath.Rel(context.root, absolute)
		if err != nil {
			return fmt.Errorf("derive validation root: %w", err)
		}
		portable := filepath.ToSlash(relative)
		if _, seen := unique[portable]; seen {
			continue
		}
		if err := safepath.MkdirParent(context.root, portable+"/.validation-leaf", 0o700); err != nil {
			return fmt.Errorf("create validation root %s: %w", portable, err)
		}
		resolved, err := safepath.Resolve(context.root, portable)
		if err != nil {
			return fmt.Errorf("validate derived root %s: %w", portable, err)
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("derived root %s is not a directory: %w", portable, errors.Join(ErrUnsafeRoot, err))
		}
		unique[portable] = struct{}{}
	}
	return nil
}

func restoreEnvironment(names []string, original map[string]originalEnvironmentValue) error {
	var result error
	for index := len(names) - 1; index >= 0; index-- {
		name := names[index]
		value := original[name]
		var err error
		if value.set {
			err = setEnvironment(name, value.value)
		} else {
			err = unsetEnvironment(name)
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("restore %s: %w", name, err))
		}
	}
	return result
}

func (context *Context) environmentNames() []string {
	return managedEnvironmentNames(context.descriptor.DynamicRoots)
}

func managedEnvironmentNames(dynamic []string) []string {
	names := make([]string, 0, len(canonicalRootRelative)+len(dynamic))
	for name := range canonicalRootRelative {
		names = append(names, name)
	}
	sort.Slice(names, func(left, right int) bool {
		return strings.ToLower(names[left]) < strings.ToLower(names[right])
	})
	names = append(names, dynamic...)
	return names
}

// Root returns the disposable validation root for boundary checks.
func (context *Context) Root() string { return context.root }

// Descriptor returns a defensive copy of the trusted descriptor.
func (context *Context) Descriptor() Descriptor {
	result := context.descriptor
	result.DynamicRoots = append([]string(nil), context.descriptor.DynamicRoots...)
	return result
}

// VirtualRoot resolves an environment alias case-insensitively.
func (context *Context) VirtualRoot(name string) (string, bool) {
	for candidate, value := range context.virtual {
		if strings.EqualFold(candidate, name) {
			return value, true
		}
	}
	return "", false
}

// OriginalEnvironment returns the pre-activation value without making Context
// itself serializable or printable with host paths.
func (context *Context) OriginalEnvironment(name string) (value string, set bool, managed bool) {
	for candidate, original := range context.original {
		if strings.EqualFold(candidate, name) {
			return original.value, original.set, true
		}
	}
	return "", false, false
}

// RegistryNamespace returns the disposable HKCU namespace identity.
func (context *Context) RegistryNamespace() string {
	return `HKCU\Software\Endstate\Validation\` + context.descriptor.Nonce
}

// String intentionally omits host roots and original environment values.
func (context *Context) String() string {
	if context == nil {
		return "validationmode.Context{inactive}"
	}
	return fmt.Sprintf("validationmode.Context{active, scenarioId:%q, moduleId:%q}", context.descriptor.ScenarioID, context.descriptor.ModuleID)
}

// GoString applies the same redaction to %#v formatting.
func (context *Context) GoString() string { return context.String() }
