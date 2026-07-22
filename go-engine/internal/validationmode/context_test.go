// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestLoadFromEnvironmentInactiveHasNoSideEffects(t *testing.T) {
	t.Setenv(TestModeEnvironment, "")
	root := filepath.Join(t.TempDir(), "endstate-validation-inactive")
	t.Setenv(RootEnvironment, root)
	before := environmentSnapshot(t, managedEnvironmentNames(nil))

	context, err := LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if context != nil {
		t.Fatalf("context = %#v, want nil", context)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("inactive load created state at %q: %v", root, err)
	}
	after := environmentSnapshot(t, managedEnvironmentNames(nil))
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("inactive load changed environment: before=%v after=%v", before, after)
	}
}

func TestLoadFromEnvironmentRejectsInvalidActivationValues(t *testing.T) {
	for _, value := range []string{"0", "true", "TRUE", " 1", "1 "} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(TestModeEnvironment, value)
			if _, err := LoadFromEnvironment(); !errors.Is(err, ErrInvalidActivation) {
				t.Fatalf("LoadFromEnvironment error = %v, want ErrInvalidActivation", err)
			}
		})
	}
}

func TestLoadFromEnvironmentValidatesRootAndDescriptor(t *testing.T) {
	t.Setenv(TestModeEnvironment, "1")
	temp := canonicalTempDir(t)
	outside, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	outside, err = filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		root string
		want error
	}{
		{name: "missing variable", root: "", want: ErrUnsafeRoot},
		{name: "relative", root: "relative", want: ErrUnsafeRoot},
		{name: "temp itself", root: temp, want: ErrUnsafeRoot},
		{name: "outside temp", root: outside, want: ErrUnsafeRoot},
		{name: "missing root", root: filepath.Join(temp, "endstate-validation-missing"), want: ErrUnsafeRoot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(RootEnvironment, tt.root)
			if _, err := LoadFromEnvironment(); !errors.Is(err, tt.want) {
				t.Fatalf("LoadFromEnvironment error = %v, want %v", err, tt.want)
			}
		})
	}

	t.Run("missing descriptor", func(t *testing.T) {
		root := makeValidationRoot(t, "missing-descriptor")
		t.Setenv(RootEnvironment, root)
		if _, err := LoadFromEnvironment(); !errors.Is(err, ErrInvalidDescriptor) {
			t.Fatalf("LoadFromEnvironment error = %v, want ErrInvalidDescriptor", err)
		}
	})

	invalid := []struct {
		name string
		body string
	}{
		{name: "empty", body: ""},
		{name: "malformed", body: "{"},
		{name: "unknown field", body: strings.Replace(validDescriptor("unknown-field"), `"schemaVersion":1`, `"schemaVersion":1,"extra":true`, 1)},
		{name: "wrong field casing", body: strings.Replace(validDescriptor("wrong-field-casing"), `"scenarioId":"scenario-one"`, `"ScenarioId":"scenario-one"`, 1)},
		{name: "duplicate field", body: strings.Replace(validDescriptor("duplicate-field"), `"schemaVersion":1`, `"schemaVersion":1,"schemaVersion":1`, 1)},
		{name: "duplicate inventory field", body: strings.Replace(validDescriptor("duplicate-inventory-field"), `"appId":"notepad-plus-plus"`, `"appId":"notepad-plus-plus","appId":"notepad-plus-plus"`, 1)},
		{name: "multiple values", body: validDescriptor("multiple-values") + `{}`},
		{name: "wrong schema", body: strings.Replace(validDescriptor("wrong-schema"), `"schemaVersion":1`, `"schemaVersion":2`, 1)},
		{name: "blank scenario", body: strings.Replace(validDescriptor("blank-scenario"), `"scenarioId":"scenario-one"`, `"scenarioId":" "`, 1)},
		{name: "bad module", body: strings.Replace(validDescriptor("bad-module"), `"moduleId":"apps.notepad-plus-plus"`, `"moduleId":"notepad"`, 1)},
		{name: "zero inventory", body: strings.Replace(validDescriptor("zero-inventory"), `"appId":"notepad-plus-plus"`, `"appId":""`, 1)},
		{name: "bad state", body: strings.Replace(validDescriptor("bad-state"), `"initialState":"absent"`, `"initialState":"missing"`, 1)},
		{name: "url ref", body: strings.Replace(validDescriptor("url-ref"), `"ref":"Notepad++.Notepad++"`, `"ref":"https://example.invalid/setup.exe"`, 1)},
		{name: "duplicate dynamic", body: strings.Replace(validDescriptor("duplicate-dynamic"), `"dynamicRoots":["instance-home"]`, `"dynamicRoots":["instance-home","INSTANCE-HOME"]`, 1)},
		{name: "canonical collision", body: strings.Replace(validDescriptor("canonical-collision"), `"dynamicRoots":["instance-home"]`, `"dynamicRoots":["AppData"]`, 1)},
		{name: "security collision", body: strings.Replace(validDescriptor("security-collision"), `"dynamicRoots":["instance-home"]`, `"dynamicRoots":["Path"]`, 1)},
		{name: "malformed dynamic", body: strings.Replace(validDescriptor("malformed-dynamic"), `"dynamicRoots":["instance-home"]`, `"dynamicRoots":["instance_home"]`, 1)},
	}
	for _, tt := range invalid {
		t.Run(tt.name, func(t *testing.T) {
			root := makeValidationRoot(t, strings.ReplaceAll(tt.name, " ", "-"))
			writeDescriptor(t, root, tt.body)
			t.Setenv(RootEnvironment, root)
			if _, err := LoadFromEnvironment(); !errors.Is(err, ErrInvalidDescriptor) {
				t.Fatalf("LoadFromEnvironment error = %v, want ErrInvalidDescriptor", err)
			}
		})
	}
}

func TestLoadFromEnvironmentBindsNonceToRootBasename(t *testing.T) {
	root := makeValidationRoot(t, "root-nonce")
	writeDescriptor(t, root, validDescriptor("descriptor-nonce"))
	t.Setenv(TestModeEnvironment, "1")
	t.Setenv(RootEnvironment, root)
	if _, err := LoadFromEnvironment(); !errors.Is(err, ErrInvalidDescriptor) {
		t.Fatalf("LoadFromEnvironment error = %v, want ErrInvalidDescriptor", err)
	}
}

func TestLoadFromEnvironmentRejectsLinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		// The test still runs where Developer Mode or symlink privilege is enabled.
	}
	t.Run("root component", func(t *testing.T) {
		realParent := t.TempDir()
		realRoot := filepath.Join(realParent, "endstate-validation-linked-root")
		if err := os.MkdirAll(realRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		writeDescriptor(t, realRoot, validDescriptor("linked-root"))
		linkedParent := filepath.Join(canonicalTempDir(t), "validation-link-parent-"+strings.ToLower(t.Name()[len("TestLoadFromEnvironmentRejectsLinks/"):]))
		if err := os.Symlink(realParent, linkedParent); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Cleanup(func() { _ = os.Remove(linkedParent) })
		t.Setenv(TestModeEnvironment, "1")
		t.Setenv(RootEnvironment, filepath.Join(linkedParent, filepath.Base(realRoot)))
		if _, err := LoadFromEnvironment(); !errors.Is(err, ErrUnsafeRoot) {
			t.Fatalf("LoadFromEnvironment error = %v, want ErrUnsafeRoot", err)
		}
	})

	t.Run("descriptor", func(t *testing.T) {
		root := makeValidationRoot(t, "linked-descriptor")
		descriptorDir := filepath.Join(root, ".endstate")
		if err := os.MkdirAll(descriptorDir, 0o700); err != nil {
			t.Fatal(err)
		}
		target := filepath.Join(root, "descriptor-target.json")
		if err := os.WriteFile(target, []byte(validDescriptor("linked-descriptor")), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, filepath.Join(descriptorDir, descriptorFilename)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		t.Setenv(TestModeEnvironment, "1")
		t.Setenv(RootEnvironment, root)
		if _, err := LoadFromEnvironment(); !errors.Is(err, ErrInvalidDescriptor) {
			t.Fatalf("LoadFromEnvironment error = %v, want ErrInvalidDescriptor", err)
		}
	})
}

func TestActivateFromEnvironmentCreatesAliasesAndRestoresEnvironment(t *testing.T) {
	root := makeValidationRoot(t, "activate")
	writeDescriptor(t, root, validDescriptor("activate"))
	t.Setenv(TestModeEnvironment, "1")
	t.Setenv(RootEnvironment, root)
	names := managedEnvironmentNames([]string{"instance-home"})
	hostTemp := canonicalTempDir(t)
	for index, name := range names {
		if strings.EqualFold(name, "TEMP") {
			t.Setenv(name, hostTemp)
		} else if strings.EqualFold(name, "TMP") {
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		} else if index%2 == 0 {
			t.Setenv(name, "original-"+name)
		} else {
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
	before := environmentSnapshot(t, names)

	context, restore, err := ActivateFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	if context == nil {
		t.Fatal("active context is nil")
	}
	for _, name := range names {
		got, ok := context.VirtualRoot(name)
		if !ok {
			t.Fatalf("missing virtual root %q", name)
		}
		if env := os.Getenv(name); env != got {
			t.Fatalf("%s = %q, want %q", name, env, got)
		}
		info, statErr := os.Stat(got)
		if statErr != nil || !info.IsDir() {
			t.Fatalf("virtual root %s is not a directory: %v", name, statErr)
		}
	}
	programFiles, _ := context.VirtualRoot("ProgramFiles")
	programW6432, _ := context.VirtualRoot("ProgramW6432")
	if programFiles != programW6432 {
		t.Fatalf("ProgramFiles = %q, ProgramW6432 = %q", programFiles, programW6432)
	}
	systemRoot, _ := context.VirtualRoot("SystemRoot")
	windir, _ := context.VirtualRoot("WINDIR")
	if systemRoot != windir {
		t.Fatalf("SystemRoot = %q, WINDIR = %q", systemRoot, windir)
	}
	temp, _ := context.VirtualRoot("TEMP")
	tmp, _ := context.VirtualRoot("TMP")
	if temp != tmp {
		t.Fatalf("TEMP = %q, TMP = %q", temp, tmp)
	}
	if got := context.RegistryNamespace(); got != `HKCU\Software\Endstate\Validation\activate` {
		t.Fatalf("registry namespace = %q", got)
	}
	if got := context.Descriptor().ModuleID; got != "apps.notepad-plus-plus" {
		t.Fatalf("descriptor module = %q", got)
	}

	if err := restore(); err != nil {
		t.Fatal(err)
	}
	if err := restore(); err != nil {
		t.Fatalf("second restore: %v", err)
	}
	after := environmentSnapshot(t, names)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("restore mismatch: before=%v after=%v", before, after)
	}
}

func TestLoadedContextActivatesWithoutRereadingEnvironment(t *testing.T) {
	root := makeValidationRoot(t, "loaded-context")
	writeDescriptor(t, root, validDescriptor("loaded-context"))
	t.Setenv(TestModeEnvironment, "1")
	t.Setenv(RootEnvironment, root)
	context, err := LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}

	// The loaded context is the authority. A later environment change must not
	// redirect activation to a different descriptor/root.
	t.Setenv(RootEnvironment, filepath.Join(canonicalTempDir(t), "not-a-validation-root"))
	restore, err := context.Activate()
	if err != nil {
		t.Fatalf("Context.Activate: %v", err)
	}
	defer func() { _ = restore() }()
	appData, _ := context.VirtualRoot("APPDATA")
	if got := os.Getenv("APPDATA"); got != appData {
		t.Fatalf("APPDATA = %q, want loaded-context root %q", got, appData)
	}
}

func TestContextDoesNotSerializeOrFormatHostPaths(t *testing.T) {
	context := activeTestContext(t, "redaction")
	encoded, err := json.Marshal(context)
	if err != nil {
		t.Fatal(err)
	}
	formatted := fmt.Sprintf("%v %#v", context, context)
	for _, output := range []string{string(encoded), formatted} {
		if strings.Contains(strings.ToLower(output), strings.ToLower(context.Root())) {
			t.Fatalf("context leaked validation root in %q", output)
		}
		for _, name := range managedEnvironmentNames(context.Descriptor().DynamicRoots) {
			root, _ := context.VirtualRoot(name)
			if strings.Contains(strings.ToLower(output), strings.ToLower(root)) {
				t.Fatalf("context leaked virtual root %q in %q", name, output)
			}
		}
	}
}

func TestActivateFromEnvironmentRestoresAfterPartialSetFailure(t *testing.T) {
	root := makeValidationRoot(t, "partial")
	writeDescriptor(t, root, validDescriptor("partial"))
	t.Setenv(TestModeEnvironment, "1")
	t.Setenv(RootEnvironment, root)
	names := managedEnvironmentNames([]string{"instance-home"})
	before := environmentSnapshot(t, names)
	originalSetenv := setEnvironment
	calls := 0
	setEnvironment = func(name, value string) error {
		calls++
		if calls == 4 {
			return errors.New("injected environment failure")
		}
		return os.Setenv(name, value)
	}
	t.Cleanup(func() { setEnvironment = originalSetenv })

	if _, _, err := ActivateFromEnvironment(); err == nil {
		t.Fatal("ActivateFromEnvironment succeeded, want failure")
	}
	after := environmentSnapshot(t, names)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("partial activation leaked environment: before=%v after=%v", before, after)
	}
}

func validDescriptor(nonce string) string {
	return `{"schemaVersion":1,"scenarioId":"scenario-one","nonce":"` + nonce + `","moduleId":"apps.notepad-plus-plus","inventory":{"appId":"notepad-plus-plus","driver":"winget","ref":"Notepad++.Notepad++","displayName":"Notepad++","version":"8.8.1","source":"winget","initialState":"absent"},"dynamicRoots":["instance-home"]}`
}

func canonicalTempDir(t *testing.T) string {
	t.Helper()
	value, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	value, err = filepath.Abs(value)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(value)
}

func makeValidationRoot(t *testing.T, nonce string) string {
	t.Helper()
	root := filepath.Join(canonicalTempDir(t), "endstate-validation-"+nonce)
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return root
}

func writeDescriptor(t *testing.T, root, body string) {
	t.Helper()
	dir := filepath.Join(root, ".endstate")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, descriptorFilename), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

type envValue struct {
	Value string
	Set   bool
}

func environmentSnapshot(t *testing.T, names []string) map[string]envValue {
	t.Helper()
	result := make(map[string]envValue, len(names))
	for _, name := range names {
		value, set := os.LookupEnv(name)
		result[name] = envValue{Value: value, Set: set}
	}
	return result
}
