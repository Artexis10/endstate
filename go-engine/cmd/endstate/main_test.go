// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

func cliValidationRoot(t *testing.T) string {
	t.Helper()
	return cliValidationRootWithInventory(t, validationmode.Inventory{
		AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
		DisplayName: "Notepad++", Source: "winget", InitialState: "present",
	})
}

func cliValidationRootWithInventory(t *testing.T, inventory validationmode.Inventory) string {
	t.Helper()
	root, err := os.MkdirTemp(os.TempDir(), "endstate-validation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	nonce := strings.TrimPrefix(filepath.Base(root), "endstate-validation-")
	descriptor := validationmode.Descriptor{
		SchemaVersion: 1,
		ScenarioID:    "cli-validation",
		Nonce:         nonce,
		ModuleID:      "apps.notepad-plus-plus",
		Inventory:     inventory,
	}
	data, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".endstate"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".endstate", "validation-mode.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}

func decodeCLIEnvelope(t *testing.T, output string) map[string]interface{} {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	var decoded map[string]interface{}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &decoded); err != nil {
		t.Fatalf("decode envelope %q: %v", output, err)
	}
	return decoded
}

func TestRunCLIInactiveOmitsTestModeAndPreservesDispatch(t *testing.T) {
	t.Setenv(validationmode.TestModeEnvironment, "")
	originalDispatch := dispatchFn
	t.Cleanup(func() { dispatchFn = originalDispatch })
	called := false
	dispatchFn = func(p parsedArgs) (interface{}, *envelope.Error) {
		called = true
		return map[string]bool{"ok": true}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"plan", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	if !called {
		t.Fatal("inactive run did not dispatch")
	}
	decoded := decodeCLIEnvelope(t, stdout.String())
	if _, exists := decoded["testMode"]; exists {
		t.Fatalf("inactive envelope included testMode: %s", stdout.String())
	}
}

func TestRunCLIActiveSuccessAndFailureIncludeSafeIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failure *envelope.Error
		want    float64
	}{
		{name: "success", want: 0},
		{name: "failure", failure: envelope.NewError(envelope.ErrManifestParseError, "bad manifest"), want: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := cliValidationRoot(t)
			t.Setenv(validationmode.TestModeEnvironment, "1")
			t.Setenv(validationmode.RootEnvironment, root)
			originalDispatch := dispatchFn
			t.Cleanup(func() { dispatchFn = originalDispatch })
			dispatchFn = func(parsedArgs) (interface{}, *envelope.Error) {
				return map[string]string{"result": "ordinary-dispatch"}, tc.failure
			}
			var stdout, stderr bytes.Buffer
			code := runCLI([]string{"plan", "--json"}, &stdout, &stderr)
			if float64(code) != tc.want {
				t.Fatalf("exit = %d stderr=%s stdout=%s", code, stderr.String(), stdout.String())
			}
			decoded := decodeCLIEnvelope(t, stdout.String())
			identity, ok := decoded["testMode"].(map[string]interface{})
			if !ok || identity["active"] != true || identity["scenarioId"] != "cli-validation" || identity["moduleId"] != "apps.notepad-plus-plus" {
				t.Fatalf("testMode = %#v", decoded["testMode"])
			}
			for _, forbidden := range []string{"root", "nonce", "source"} {
				if _, exists := identity[forbidden]; exists {
					t.Fatalf("testMode leaked %s: %#v", forbidden, identity)
				}
			}
		})
	}
}

func TestRunCLIActiveCaptureDefaultsToDescriptorDriver(t *testing.T) {
	root := cliValidationRootWithInventory(t, validationmode.Inventory{
		AppID: "tool", Driver: "chocolatey", Ref: "vendor-tool",
		DisplayName: "Tool", InitialState: "present",
	})
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	originalDispatch := dispatchFn
	t.Cleanup(func() { dispatchFn = originalDispatch })
	dispatchFn = func(p parsedArgs) (interface{}, *envelope.Error) {
		if !reflect.DeepEqual(p.drivers, []string{"chocolatey"}) {
			t.Fatalf("capture drivers = %v, want descriptor driver", p.drivers)
		}
		return struct{}{}, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"capture", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestRunCLIInvalidActivationFailsBeforeDispatch(t *testing.T) {
	t.Setenv(validationmode.TestModeEnvironment, "yes")
	t.Setenv(validationmode.RootEnvironment, "")
	sensitiveManifest := filepath.Join(t.TempDir(), "must-not-leak.jsonc")
	originalDispatch := dispatchFn
	t.Cleanup(func() { dispatchFn = originalDispatch })
	dispatchFn = func(parsedArgs) (interface{}, *envelope.Error) {
		t.Fatal("dispatch ran after invalid activation")
		return nil, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"plan", "--manifest", sensitiveManifest, "--debug-cli", "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	for _, form := range []string{sensitiveManifest, strings.ReplaceAll(sensitiveManifest, `\`, `\\`), filepath.ToSlash(sensitiveManifest)} {
		if strings.Contains(stdout.String(), form) || strings.Contains(stderr.String(), form) {
			t.Fatalf("invalid activation leaked pre-validation debug path %q: stdout=%s stderr=%s", form, stdout.String(), stderr.String())
		}
	}
	decoded := decodeCLIEnvelope(t, stdout.String())
	if _, exists := decoded["testMode"]; exists {
		t.Fatalf("untrusted activation claimed testMode: %s", stdout.String())
	}
	errorObject := decoded["error"].(map[string]interface{})
	if errorObject["code"] != string(envelope.ErrTestModeInvalid) {
		t.Fatalf("error code = %v", errorObject["code"])
	}
}

func TestRunCLIActiveDebugAndEventsRedactValidationAuthority(t *testing.T) {
	root := cliValidationRoot(t)
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	loadedContext, err := validationmode.LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}

	originalDispatch := dispatchFn
	t.Cleanup(func() { dispatchFn = originalDispatch })
	dispatchFn = func(parsedArgs) (interface{}, *envelope.Error) {
		emitter := events.NewEmitter("redaction-run", true)
		emitter.EmitArtifact("capture", "manifest", filepath.Join(root, "artifacts", "captured.jsonc"))
		reason := filepath.ToSlash(filepath.Join(root, "reasons", "configured"))
		remediation := filepath.Join(root, "remediation", "retry.txt")
		emitter.EmitConfigMigration(events.ConfigMigrationProgress{
			CaptureID:   "capture-settings",
			ConfigSetID: "settings",
			Stage:       events.ConfigMigrationStaging,
			Status:      events.ConfigProgressFailed,
			Reason:      &reason,
			Message:     `failed under ` + filepath.Join(root, "staging"),
			Remediation: &remediation,
		})
		backupPath := filepath.Join(root, "backups", "settings.json")
		emitter.EmitRestoreItem(events.RestoreItemProgress{
			ID:         "restore-settings",
			Module:     "apps.notepad-plus-plus",
			Restorer:   "copy",
			Source:     filepath.Join(root, "exports", "settings.json"),
			Target:     filepath.ToSlash(filepath.Join(root, "sandbox", "settings.json")),
			Status:     events.RestoreItemRestored,
			Reason:     &reason,
			BackupPath: &backupPath,
			Message:    `restored from ` + filepath.Join(root, "exports"),
		})
		return map[string]string{"artifact": filepath.Join(root, "result.json")}, nil
	}

	manifestPath := filepath.Join(root, "manifests", "active.jsonc")
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"plan", "--manifest", manifestPath, "--debug-cli", "--events", "jsonl", "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), `"event":"artifact"`) ||
		!strings.Contains(stderr.String(), `"event":"config-migration"`) ||
		!strings.Contains(stderr.String(), `"event":"restore-item"`) {
		t.Fatalf("active default events were not routed to CLI stderr: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "$ENDSTATE_ROOT") || !strings.Contains(stdout.String(), "$ENDSTATE_ROOT") {
		t.Fatalf("active output did not use the stable root placeholder: stdout=%s stderr=%s", stdout.String(), stderr.String())
	}

	for _, output := range []string{stdout.String(), stderr.String()} {
		for _, rootValue := range []string{root, loadedContext.Root()} {
			forms := []string{
				rootValue,
				filepath.ToSlash(rootValue),
				strings.ReplaceAll(rootValue, `/`, `\`),
				strings.ReplaceAll(rootValue, `\`, `\\`),
			}
			for _, form := range forms {
				if form != "" && strings.Contains(strings.ToLower(output), strings.ToLower(form)) {
					t.Fatalf("active output leaked disposable root form %q: %s", form, output)
				}
			}
		}
	}
}

func TestRunCLIActiveForbiddenCommandFailsBeforeDispatch(t *testing.T) {
	root := cliValidationRoot(t)
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	originalDispatch := dispatchFn
	t.Cleanup(func() { dispatchFn = originalDispatch })
	dispatchFn = func(parsedArgs) (interface{}, *envelope.Error) {
		t.Fatal("forbidden command reached dispatch")
		return nil, nil
	}
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"doctor", "--json"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit = %d stderr=%s", code, stderr.String())
	}
	decoded := decodeCLIEnvelope(t, stdout.String())
	errorObject := decoded["error"].(map[string]interface{})
	if errorObject["code"] != string(envelope.ErrTestModeCommandForbidden) {
		t.Fatalf("error code = %v", errorObject["code"])
	}
}

func TestRunCLIHelpAndForbiddenCommandsDoNotActivateValidationMode(t *testing.T) {
	for _, tc := range []struct {
		name     string
		args     []string
		wantCode int
	}{
		{name: "help", args: []string{"--help"}, wantCode: 0},
		{name: "forbidden", args: []string{"doctor", "--json"}, wantCode: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := cliValidationRoot(t)
			t.Setenv(validationmode.TestModeEnvironment, "1")
			t.Setenv(validationmode.RootEnvironment, root)
			originalActivate := activateCommandValidationModeFn
			t.Cleanup(func() { activateCommandValidationModeFn = originalActivate })
			activateCommandValidationModeFn = func(*validationmode.Context) (*commands.ValidationModeSession, error) {
				t.Fatal("help/forbidden command activated command validation mode")
				return nil, nil
			}
			var stdout, stderr bytes.Buffer
			if code := runCLI(tc.args, &stdout, &stderr); code != tc.wantCode {
				t.Fatalf("exit = %d, want %d stdout=%s stderr=%s", code, tc.wantCode, stdout.String(), stderr.String())
			}
			for _, forbiddenPath := range []string{
				filepath.Join(root, "sandbox"),
				filepath.Join(root, ".endstate", "validation-package-state.json"),
			} {
				if _, err := os.Lstat(forbiddenPath); !os.IsNotExist(err) {
					t.Fatalf("read-only pre-dispatch path created %s (err=%v)", forbiddenPath, err)
				}
			}
		})
	}
}

func TestRunCLIRestoresValidationEnvironmentAfterSuccessAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "failure"}[fail], func(t *testing.T) {
			root := cliValidationRoot(t)
			t.Setenv(validationmode.TestModeEnvironment, "1")
			t.Setenv(validationmode.RootEnvironment, root)
			t.Setenv("APPDATA", "original-appdata")
			originalDispatch := dispatchFn
			t.Cleanup(func() { dispatchFn = originalDispatch })
			dispatchFn = func(parsedArgs) (interface{}, *envelope.Error) {
				if os.Getenv("APPDATA") == "original-appdata" {
					t.Fatal("validation environment was not active during dispatch")
				}
				if fail {
					return nil, envelope.NewError(envelope.ErrInternalError, "boom")
				}
				return struct{}{}, nil
			}
			var stdout, stderr bytes.Buffer
			_ = runCLI([]string{"verify", "--json"}, &stdout, &stderr)
			if got := os.Getenv("APPDATA"); got != "original-appdata" {
				t.Fatalf("APPDATA after run = %q", got)
			}
		})
	}
}

func TestRunCLIChecksIsolationAfterSuccessfulAndFailedDispatch(t *testing.T) {
	for _, dispatchFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "successful dispatch", true: "failed dispatch"}[dispatchFails], func(t *testing.T) {
			root := cliValidationRoot(t)
			t.Setenv(validationmode.TestModeEnvironment, "1")
			t.Setenv(validationmode.RootEnvironment, root)
			authority := t.TempDir()
			t.Setenv("GITHUB_WORKSPACE", authority)
			marker := filepath.Join(authority, "guarded.txt")
			if err := os.WriteFile(marker, []byte("before"), 0o600); err != nil {
				t.Fatal(err)
			}
			originalDispatch := dispatchFn
			t.Cleanup(func() { dispatchFn = originalDispatch })
			dispatchFn = func(parsedArgs) (interface{}, *envelope.Error) {
				if err := os.WriteFile(marker, []byte("after"), 0o600); err != nil {
					t.Fatal(err)
				}
				if dispatchFails {
					return nil, envelope.NewError(envelope.ErrInternalError, "ordinary dispatch failure")
				}
				return struct{}{}, nil
			}
			var stdout, stderr bytes.Buffer
			if code := runCLI([]string{"verify", "--json"}, &stdout, &stderr); code != 1 {
				t.Fatalf("exit = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			decoded := decodeCLIEnvelope(t, stdout.String())
			errorObject := decoded["error"].(map[string]interface{})
			if errorObject["code"] != string(envelope.ErrTestModeIsolationViolation) {
				t.Fatalf("error code = %v envelope=%s", errorObject["code"], stdout.String())
			}
			for _, output := range []string{stdout.String(), stderr.String()} {
				if strings.Contains(strings.ToLower(output), strings.ToLower(authority)) {
					t.Fatalf("CLI output leaked authority path: %s", output)
				}
			}
		})
	}
}

func TestRunCLIMapsPackageIdentityViolationsAndDoesNotMutateState(t *testing.T) {
	for _, tc := range []struct {
		name   string
		driver string
		ref    string
		source string
	}{
		{name: "wrong driver", driver: "chocolatey", ref: "Notepad++.Notepad++", source: ""},
		{name: "wrong ref", driver: "winget", ref: "Other.Package", source: "winget"},
		{name: "wrong source", driver: "winget", ref: "Notepad++.Notepad++", source: "msstore"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := cliValidationRootWithInventory(t, validationmode.Inventory{
				AppID: "notepad-plus-plus", Driver: "winget", Ref: "Notepad++.Notepad++",
				DisplayName: "Notepad++", Source: "winget", InitialState: "absent",
			})
			t.Setenv(validationmode.TestModeEnvironment, "1")
			t.Setenv(validationmode.RootEnvironment, root)
			manifestPath := filepath.Join(root, "identity-violation.jsonc")
			manifest := `{"version":1,"name":"identity-violation","apps":[{"id":"candidate","displayName":"Candidate","driver":"` + tc.driver + `","source":"` + tc.source + `","refs":{"windows":"` + tc.ref + `"}}]}`
			if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
				t.Fatal(err)
			}
			var stdout, stderr bytes.Buffer
			if code := runCLI([]string{"plan", "--manifest", manifestPath, "--json"}, &stdout, &stderr); code != 1 {
				t.Fatalf("exit = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
			decoded := decodeCLIEnvelope(t, stdout.String())
			errorObject := decoded["error"].(map[string]interface{})
			if errorObject["code"] != string(envelope.ErrTestModeIsolationViolation) {
				t.Fatalf("error code = %v, envelope=%s", errorObject["code"], stdout.String())
			}
			stateData, err := os.ReadFile(filepath.Join(root, ".endstate", "validation-package-state.json"))
			if err != nil {
				t.Fatal(err)
			}
			var state struct {
				Present bool `json:"present"`
			}
			if err := json.Unmarshal(stateData, &state); err != nil {
				t.Fatal(err)
			}
			if state.Present {
				t.Fatalf("identity violation mutated package state: %s", stateData)
			}
		})
	}
}

func TestRunCLIActiveEnvelopeDoesNotSerializeDisposableRoot(t *testing.T) {
	root := cliValidationRoot(t)
	t.Setenv(validationmode.TestModeEnvironment, "1")
	t.Setenv(validationmode.RootEnvironment, root)
	manifestPath := filepath.Join(root, "valid-plan.jsonc")
	manifest := `{"version":1,"name":"valid-plan","apps":[{"id":"notepad-plus-plus","displayName":"Notepad++","driver":"winget","source":"winget","refs":{"windows":"Notepad++.Notepad++"}}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	loadedContext, err := validationmode.LoadFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := runCLI([]string{"plan", "--manifest", manifestPath, "--json"}, &stdout, &stderr); code != 0 {
		t.Fatalf("exit = %d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	rootForms := []string{root, loadedContext.Root(), strings.ReplaceAll(root, `\`, `\\`), strings.ReplaceAll(loadedContext.Root(), `\`, `\\`)}
	for _, rootForm := range rootForms {
		if !strings.Contains(strings.ToLower(stdout.String()), strings.ToLower(rootForm)) {
			continue
		}
		t.Fatalf("active envelope serialized disposable root: %s", stdout.String())
	}
}

func TestBuiltExecutableUsesDisposableEngineAcrossPlanApplyVerify(t *testing.T) {
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	buildRoot := t.TempDir()
	binaryPath := filepath.Join(buildRoot, "endstate-validation-test.exe")
	if !filepath.IsAbs(binaryPath) {
		t.Fatalf("binary path is not absolute: %s", binaryPath)
	}
	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/endstate")
	build.Dir = moduleRoot
	build.Env = make([]string, 0, len(os.Environ())+1)
	for _, item := range os.Environ() {
		name := strings.SplitN(item, "=", 2)[0]
		if !strings.EqualFold(name, "GOCACHE") {
			build.Env = append(build.Env, item)
		}
	}
	build.Env = append(build.Env, "GOCACHE="+filepath.Join(buildRoot, "gocache"))
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("fresh build failed: %v\n%s", err, output)
	}

	root := cliValidationRootWithInventory(t, validationmode.Inventory{
		AppID: "vendor-notepadplusplus", Driver: "winget", Ref: "Vendor.NotepadPlusPlus",
		DisplayName: "Notepad++", Version: "8.8.2", Source: "winget", InitialState: "absent",
	})
	moduleDir := filepath.Join(root, "modules", "apps", "notepad-plus-plus")
	if err := os.MkdirAll(moduleDir, 0o700); err != nil {
		t.Fatal(err)
	}
	moduleJSON := `{"id":"apps.notepad-plus-plus","displayName":"Notepad++","sensitivity":"low","matches":{"winget":["Vendor.NotepadPlusPlus"]},"verify":[],"restore":[],"capture":{"files":[{"source":"%APPDATA%\\Notepad++\\config.xml","dest":"apps/notepad-plus-plus/config.xml","optional":true}],"excludeGlobs":[]}}`
	if err := os.WriteFile(filepath.Join(moduleDir, "module.jsonc"), []byte(moduleJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(root, "manifests", "engine-flow.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		t.Fatal(err)
	}
	manifest := `{"version":1,"name":"engine-flow","apps":[{"id":"vendor-notepadplusplus","displayName":"Notepad++","driver":"winget","source":"winget","version":"8.8.2","refs":{"windows":"Vendor.NotepadPlusPlus"}}]}`
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}

	childEnvironment := make([]string, 0, len(os.Environ())+2)
	for _, item := range os.Environ() {
		name := strings.SplitN(item, "=", 2)[0]
		if strings.EqualFold(name, validationmode.TestModeEnvironment) || strings.EqualFold(name, validationmode.RootEnvironment) {
			continue
		}
		childEnvironment = append(childEnvironment, item)
	}
	childEnvironment = append(childEnvironment,
		validationmode.TestModeEnvironment+"=1",
		validationmode.RootEnvironment+"="+root,
	)

	run := func(command string) map[string]interface{} {
		t.Helper()
		cmd := exec.Command(binaryPath, command, "--manifest", manifestPath, "--json")
		cmd.Dir = moduleRoot
		cmd.Env = childEnvironment
		var stdout, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("%s failed: %v\nstdout=%s\nstderr=%s", command, err, stdout.String(), stderr.String())
		}
		for _, output := range []string{stdout.String(), stderr.String()} {
			for _, rootForm := range []string{root, filepath.ToSlash(root), strings.ReplaceAll(root, `\`, `\\`)} {
				if rootForm != "" && strings.Contains(strings.ToLower(output), strings.ToLower(rootForm)) {
					t.Fatalf("%s output leaked disposable root form %q: %s", command, rootForm, output)
				}
			}
		}
		decoded := decodeCLIEnvelope(t, stdout.String())
		if decoded["success"] != true {
			t.Fatalf("%s envelope = %s", command, stdout.String())
		}
		identity, ok := decoded["testMode"].(map[string]interface{})
		if !ok || identity["scenarioId"] != "cli-validation" || identity["moduleId"] != "apps.notepad-plus-plus" {
			t.Fatalf("%s testMode = %#v", command, decoded["testMode"])
		}
		return decoded
	}

	planEnvelope := run("plan")
	planData := planEnvelope["data"].(map[string]interface{})
	planSummary := planData["plan"].(map[string]interface{})
	if planSummary["toInstall"] != float64(1) {
		t.Fatalf("initial plan = %#v", planSummary)
	}
	applyEnvelope := run("apply")
	applyData := applyEnvelope["data"].(map[string]interface{})
	applySummary := applyData["summary"].(map[string]interface{})
	if applySummary["success"] != float64(1) {
		t.Fatalf("apply summary = %#v", applySummary)
	}
	verifyEnvelope := run("verify")
	verifyData := verifyEnvelope["data"].(map[string]interface{})
	verifySummary := verifyData["summary"].(map[string]interface{})
	if verifySummary["pass"] != float64(1) || verifySummary["fail"] != float64(0) {
		t.Fatalf("verify summary = %#v", verifySummary)
	}

	packageStatePath := filepath.Join(root, ".endstate", "validation-package-state.json")
	packageStateBefore, err := os.ReadFile(packageStatePath)
	if err != nil {
		t.Fatal(err)
	}
	unsafeModuleJSON := `{"id":"apps.notepad-plus-plus","displayName":"Notepad++","sensitivity":"low","matches":{"winget":["Vendor.NotepadPlusPlus"]},"verify":[],"restore":[],"capture":{"files":[{"source":"C:\\host\\escape\\config.xml","dest":"apps/notepad-plus-plus/config.xml","optional":true}],"excludeGlobs":[]}}`
	if err := os.WriteFile(filepath.Join(moduleDir, "module.jsonc"), []byte(unsafeModuleJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binaryPath, "verify", "--manifest", manifestPath, "--json")
	cmd.Dir = moduleRoot
	cmd.Env = childEnvironment
	var failureStdout, failureStderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &failureStdout, &failureStderr
	if err := cmd.Run(); err == nil {
		t.Fatalf("unsafe production module unexpectedly succeeded: stdout=%s stderr=%s", failureStdout.String(), failureStderr.String())
	}
	failureEnvelope := decodeCLIEnvelope(t, failureStdout.String())
	errorObject, ok := failureEnvelope["error"].(map[string]interface{})
	if !ok || errorObject["code"] != string(envelope.ErrTestModeIsolationViolation) {
		t.Fatalf("unsafe module error = %#v envelope=%s", failureEnvelope["error"], failureStdout.String())
	}
	for _, output := range []string{failureStdout.String(), failureStderr.String()} {
		for _, rootForm := range []string{root, filepath.ToSlash(root), strings.ReplaceAll(root, `\`, `\\`)} {
			if rootForm != "" && strings.Contains(strings.ToLower(output), strings.ToLower(rootForm)) {
				t.Fatalf("unsafe module output leaked disposable root form %q: %s", rootForm, output)
			}
		}
	}
	packageStateAfter, err := os.ReadFile(packageStatePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(packageStateAfter, packageStateBefore) {
		t.Fatalf("unsafe production-module preflight mutated package state: before=%s after=%s", packageStateBefore, packageStateAfter)
	}
}

func TestParseArgsPreservesRepeatableRestoreTargets(t *testing.T) {
	parsed := parseArgs([]string{
		"rebuild",
		"--from", "profile.zip",
		"--restore-target", "capture-b=instance-2",
		"--restore-target", "capture-a=instance-1",
	})

	want := []string{"capture-b=instance-2", "capture-a=instance-1"}
	if !reflect.DeepEqual(parsed.restoreTargets, want) {
		t.Fatalf("restoreTargets = %#v, want %#v", parsed.restoreTargets, want)
	}
}

func TestParseArgsPreservesMissingRestoreTargetForValidation(t *testing.T) {
	parsed := parseArgs([]string{"restore", "--restore-target", "--dry-run"})
	if !reflect.DeepEqual(parsed.restoreTargets, []string{""}) {
		t.Fatalf("restoreTargets = %#v, want one empty value", parsed.restoreTargets)
	}
	if !parsed.dryRun {
		t.Fatal("following flag was consumed as a restore target")
	}
}

func TestRestoreCapableCommandUsageAdvertisesRestoreTarget(t *testing.T) {
	for _, command := range []string{"apply", "restore", "rebuild"} {
		usage := commandUsage(command)
		if !strings.Contains(usage, "--restore-target <captureId>=<targetInstanceId>") {
			t.Fatalf("%s usage does not advertise --restore-target: %s", command, usage)
		}
	}
	if !strings.Contains(usageText, "--restore-target <m>") {
		t.Fatalf("top-level usage does not advertise repeatable --restore-target: %s", usageText)
	}
}

func TestParseArgs_CaptureRepeatableDriver(t *testing.T) {
	got := parseArgs([]string{"capture", "--driver", "winget", "--driver", "chocolatey", "--json"})
	if want := []string{"winget", "chocolatey"}; !reflect.DeepEqual(got.drivers, want) {
		t.Fatalf("drivers = %v, want %v", got.drivers, want)
	}
}

func TestParseArgs_CaptureDriverRequiresValue(t *testing.T) {
	for _, args := range [][]string{
		{"capture", "--driver"},
		{"capture", "--driver", "--json"},
		{"capture", "--driver", "-h"},
	} {
		parsed := parseArgs(args)
		if !parsed.driverMissingValue {
			t.Fatalf("parseArgs(%v) did not record missing --driver value", args)
		}
		_, err := dispatch(parsed)
		if err == nil || err.Code != envelope.ErrManifestValidationError {
			t.Fatalf("dispatch(%v) error = %+v, want %s", args, err, envelope.ErrManifestValidationError)
		}
	}
}

func TestParseArgs_RebuildBootstrapFlags(t *testing.T) {
	got := parseArgs([]string{"rebuild", "--from", "machine.zip", "--bootstrap-backends", "--no-bootstrap"})
	if !got.bootstrapBackends || !got.noBootstrap {
		t.Fatalf("bootstrap flags = (%v, %v), want both parsed", got.bootstrapBackends, got.noBootstrap)
	}
}

func TestCommandUsage_MultiDriverFlags(t *testing.T) {
	for _, tc := range []struct {
		command string
		flags   []string
	}{
		{command: "capture", flags: []string{"--driver <name>"}},
		{command: "rebuild", flags: []string{"--bootstrap-backends", "--no-bootstrap"}},
	} {
		usage := commandUsage(tc.command)
		for _, flag := range tc.flags {
			if !strings.Contains(usage, flag) {
				t.Errorf("%s usage missing %q: %s", tc.command, flag, usage)
			}
		}
	}
}

func TestDispatch_ForwardsCaptureDrivers(t *testing.T) {
	orig := runCaptureFn
	defer func() { runCaptureFn = orig }()
	var captured commands.CaptureFlags
	runCaptureFn = func(flags commands.CaptureFlags) (interface{}, *envelope.Error) {
		captured = flags
		return struct{}{}, nil
	}

	parsed := parseArgs([]string{"capture", "--out", "capture.jsonc", "--driver", "winget", "--driver", "chocolatey", "--pin", "--events", "jsonl"})
	if _, eerr := dispatch(parsed); eerr != nil {
		t.Fatalf("dispatch error: %v", eerr)
	}
	if !reflect.DeepEqual(captured.Drivers, []string{"winget", "chocolatey"}) || !captured.Pin || captured.Events != "jsonl" || captured.Out != "capture.jsonc" {
		t.Fatalf("forwarded capture flags = %+v", captured)
	}
}

func TestCaptureStoreFlags_AreCompatibleAndExcludeWins(t *testing.T) {
	orig := runCaptureFn
	defer func() { runCaptureFn = orig }()
	var captured commands.CaptureFlags
	runCaptureFn = func(flags commands.CaptureFlags) (interface{}, *envelope.Error) {
		captured = flags
		return struct{}{}, nil
	}

	parsed := parseArgs([]string{"capture", "--include-store-apps", "--exclude-store-apps"})
	if _, eerr := dispatch(parsed); eerr != nil {
		t.Fatal(eerr)
	}
	if !captured.IncludeStoreApps || !captured.ExcludeStoreApps {
		t.Fatalf("flags = %+v, want deprecated include accepted and explicit exclude forwarded", captured)
	}
	usage := commandUsage("capture")
	if !strings.Contains(usage, "--exclude-store-apps") || !strings.Contains(usage, "deprecated") {
		t.Fatalf("capture usage missing store compatibility details: %s", usage)
	}
}

func TestDispatch_ForwardsRebuildBootstrapFlags(t *testing.T) {
	orig := runRebuildFn
	defer func() { runRebuildFn = orig }()
	var captured commands.RebuildFlags
	runRebuildFn = func(flags commands.RebuildFlags) (interface{}, *envelope.Error) {
		captured = flags
		return struct{}{}, nil
	}

	parsed := parseArgs([]string{"rebuild", "--from", "machine.zip", "--bootstrap-backends", "--no-bootstrap", "--restore-filter", "apps.git", "--restore-target", "capture-a=instance-1", "--dry-run"})
	if _, eerr := dispatch(parsed); eerr != nil {
		t.Fatalf("dispatch error: %v", eerr)
	}
	if captured.From != "machine.zip" || !captured.BootstrapBackends || !captured.NoBootstrap || !captured.DryRun || captured.RestoreFilter != "apps.git" || !reflect.DeepEqual(captured.RestoreTargets, []string{"capture-a=instance-1"}) {
		t.Fatalf("forwarded rebuild flags = %+v", captured)
	}
}
