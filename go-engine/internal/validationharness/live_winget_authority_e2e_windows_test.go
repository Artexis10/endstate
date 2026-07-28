// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

//go:build windows

package validationharness

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/wingetauthority"
	"golang.org/x/sys/windows"
)

func TestBuiltEngineBindsAllWingetLifecycleCallsToTrustedAuthority(t *testing.T) {
	root := t.TempDir()
	moduleRoot := liveWingetAuthorityModuleRoot(t)
	engine := liveWingetAuthorityBuild(t, moduleRoot, filepath.Join(root, "bin", "endstate.exe"), "./cmd/endstate")
	trusted := liveWingetAuthorityBuild(t, moduleRoot, filepath.Join(root, "trusted", "winget.exe"), "./internal/wingetauthority/testdata/fakewinget")
	hostile := liveWingetAuthorityBuild(t, moduleRoot, filepath.Join(root, "hostile", "winget.exe"), "./internal/wingetauthority/testdata/fakewinget")

	capability := liveWingetAuthorityCapability(t, trusted)
	state := filepath.Join(root, "fake-state")
	log := filepath.Join(root, "fake-winget.jsonl")
	manifestPath := filepath.Join(root, "install.jsonc")
	liveWingetAuthorityWriteFile(t, manifestPath, []byte(`{
  "version": 1,
  "name": "strict-winget-authority",
  "apps": [{
    "id": "fake-app",
    "refs": {"windows": "Fake.App"},
    "driver": "winget",
    "source": "winget"
  }]
}`))

	environment := liveWingetAuthorityEnvironment(root, hostile, state, log, capability)
	liveWingetAuthorityRun(t, engine, moduleRoot, environment, "apply", "--manifest", manifestPath, "--json")
	liveWingetAuthorityRun(t, engine, moduleRoot, environment, "verify", "--manifest", manifestPath, "--json")
	captureManifest := filepath.Join(root, "captured.jsonc")
	if output, err := liveWingetAuthorityRunRaw(engine, moduleRoot, environment, "capture", "--only", "fake-app", "--driver", "winget", "--out", captureManifest, "--json"); err != nil {
		logData, _ := os.ReadFile(log)
		t.Fatalf("endstate capture failed: %v\n%s\nfake log=%s", err, output, logData)
	}
	captureBundle := strings.TrimSuffix(captureManifest, filepath.Ext(captureManifest)) + ".endstate"
	if _, err := os.Stat(captureBundle); err != nil {
		t.Fatalf("capture bundle %q: %v", captureBundle, err)
	}
	if err := os.Remove(filepath.Join(state, "installed")); err != nil && !os.IsNotExist(err) {
		t.Fatalf("clear fake installed state: %v", err)
	}
	liveWingetAuthorityRun(t, engine, moduleRoot, environment, "rebuild", "--from", captureBundle, "--no-restore", "--json")
	liveWingetAuthorityRun(t, engine, moduleRoot, environment, "rollback", "--confirm", "--json")

	calls := liveWingetAuthorityReadCalls(t, log)
	if len(calls) == 0 {
		t.Fatal("trusted fake Winget received no lifecycle calls")
	}
	seen := make(map[string]bool)
	for _, call := range calls {
		if strings.EqualFold(call.Executable, hostile) {
			t.Fatalf("hostile Winget sentinel was invoked: %#v", call)
		}
		if !strings.EqualFold(call.Executable, trusted) {
			t.Fatalf("Winget invoked %q, want trusted executable %q; calls=%#v", call.Executable, trusted, calls)
		}
		if call.StrictPresent || call.AuthorityPresent {
			t.Fatalf("Winget inherited private hosted authority: %#v", call)
		}
		seen[call.Operation] = true
	}
	for _, operation := range []string{"list", "install", "export", "details", "uninstall"} {
		if !seen[operation] {
			t.Fatalf("trusted fake Winget did not receive %q; calls=%#v", operation, calls)
		}
	}
}

func TestBuiltEngineRejectsMalformedOrDriftedWingetAuthorityBeforeLaunchingAnyFake(t *testing.T) {
	root := t.TempDir()
	moduleRoot := liveWingetAuthorityModuleRoot(t)
	engine := liveWingetAuthorityBuild(t, moduleRoot, filepath.Join(root, "bin", "endstate.exe"), "./cmd/endstate")
	trusted := liveWingetAuthorityBuild(t, moduleRoot, filepath.Join(root, "trusted", "winget.exe"), "./internal/wingetauthority/testdata/fakewinget")
	hostile := liveWingetAuthorityBuild(t, moduleRoot, filepath.Join(root, "hostile", "winget.exe"), "./internal/wingetauthority/testdata/fakewinget")
	state := filepath.Join(root, "fake-state")
	log := filepath.Join(root, "fake-winget.jsonl")
	manifestPath := filepath.Join(root, "install.jsonc")
	liveWingetAuthorityWriteFile(t, manifestPath, []byte(`{"version":1,"name":"strict-winget-authority","apps":[{"id":"fake-app","refs":{"windows":"Fake.App"}}]}`))

	for _, tt := range []struct {
		name       string
		capability string
	}{
		{name: "malformed capability", capability: "malformed-private-capability"},
		{name: "digest drift", capability: liveWingetAuthorityDriftedCapability(t, trusted)},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.Remove(log); err != nil && !os.IsNotExist(err) {
				t.Fatalf("reset fake log: %v", err)
			}
			environment := liveWingetAuthorityEnvironment(root, hostile, state, log, tt.capability)
			output, err := liveWingetAuthorityRunRaw(engine, moduleRoot, environment, "apply", "--manifest", manifestPath, "--json")
			if err != nil {
				t.Fatalf("apply with %s failed to return its expected item failure: %v\n%s", tt.name, err, output)
			}
			if strings.Contains(output, tt.capability) {
				t.Fatalf("apply leaked private authority: %s", output)
			}
			if !strings.Contains(output, `"reason":"install_failed"`) {
				t.Fatalf("apply with %s did not report a bounded install failure: %s", tt.name, output)
			}
			if _, err := os.Stat(log); !os.IsNotExist(err) {
				t.Fatalf("invalid authority started a fake Winget: %v", err)
			}
		})
	}
}

type liveWingetAuthorityCall struct {
	Executable       string `json:"executable"`
	Operation        string `json:"operation"`
	StrictPresent    bool   `json:"strictPresent"`
	AuthorityPresent bool   `json:"authorityPresent"`
}

func liveWingetAuthorityModuleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve Go module root: %v", err)
	}
	return root
}

func liveWingetAuthorityBuild(t *testing.T, moduleRoot, output, target string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(output), 0o700); err != nil {
		t.Fatalf("create build directory: %v", err)
	}
	command := exec.Command("go", "build", "-buildvcs=false", "-o", output, target)
	command.Dir = moduleRoot
	command.Env = append(liveWingetAuthorityWithoutEnvironment(os.Environ(), "GOCACHE", "GOTELEMETRY"),
		"GOCACHE="+filepath.Join(filepath.Dir(output), "gocache"), "GOTELEMETRY=off")
	if outputBytes, err := command.CombinedOutput(); err != nil {
		t.Fatalf("fresh build %q failed: %v\n%s", target, err, outputBytes)
	}
	return liveWingetAuthorityLongPath(t, output)
}

func liveWingetAuthorityLongPath(t *testing.T, path string) string {
	t.Helper()
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		t.Fatalf("convert %q to UTF-16: %v", path, err)
	}
	buffer := make([]uint16, 32768)
	count, err := windows.GetLongPathName(wide, &buffer[0], uint32(len(buffer)))
	if err != nil || count == 0 || count >= uint32(len(buffer)) {
		t.Fatalf("resolve long path for %q: count=%d err=%v", path, count, err)
	}
	return windows.UTF16ToString(buffer[:count])
}

func liveWingetAuthorityCapability(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read trusted fake Winget: %v", err)
	}
	capability, err := wingetauthority.Encode(path, sha256.Sum256(data))
	if err != nil {
		t.Fatalf("encode trusted fake Winget authority: %v", err)
	}
	return capability
}

func liveWingetAuthorityDriftedCapability(t *testing.T, path string) string {
	t.Helper()
	digest := sha256.Sum256([]byte("digest-drift"))
	capability, err := wingetauthority.Encode(path, digest)
	if err != nil {
		t.Fatalf("encode drifted fake Winget authority: %v", err)
	}
	return capability
}

func liveWingetAuthorityEnvironment(root, hostile, state, log, capability string) []string {
	environment := liveWingetAuthorityWithoutEnvironment(os.Environ(), "ENDSTATE_ROOT", "PATH", wingetauthority.StrictEnvironment, wingetauthority.AuthorityEnvironment, "ENDSTATE_FAKE_WINGET_STATE", "ENDSTATE_FAKE_WINGET_LOG")
	path := filepath.Dir(hostile)
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		path += string(os.PathListSeparator) + filepath.Join(systemRoot, "System32")
	}
	return append(environment,
		"ENDSTATE_ROOT="+root,
		"PATH="+path,
		"ENDSTATE_FAKE_WINGET_STATE="+state,
		"ENDSTATE_FAKE_WINGET_LOG="+log,
		wingetauthority.StrictEnvironment+"="+wingetauthority.StrictValue,
		wingetauthority.AuthorityEnvironment+"="+capability,
	)
}

func liveWingetAuthorityRun(t *testing.T, engine, directory string, environment []string, args ...string) {
	t.Helper()
	output, err := liveWingetAuthorityRunRaw(engine, directory, environment, args...)
	if err != nil {
		t.Fatalf("endstate %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	var envelope struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &envelope); err != nil || !envelope.Success {
		t.Fatalf("endstate %s envelope = %q, decode error = %v", strings.Join(args, " "), output, err)
	}
}

func liveWingetAuthorityRunRaw(engine, directory string, environment []string, args ...string) (string, error) {
	command := exec.Command(engine, args...)
	command.Dir = directory
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		return stdout.String() + "\nstderr=" + stderr.String(), err
	}
	return stdout.String(), nil
}

func liveWingetAuthorityReadCalls(t *testing.T, path string) []liveWingetAuthorityCall {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fake Winget log: %v", err)
	}
	var calls []liveWingetAuthorityCall
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var call liveWingetAuthorityCall
		if err := json.Unmarshal([]byte(line), &call); err != nil {
			t.Fatalf("decode fake Winget log entry %q: %v", line, err)
		}
		calls = append(calls, call)
	}
	return calls
}

func liveWingetAuthorityWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create %q parent: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %q: %v", path, err)
	}
}

func liveWingetAuthorityWithoutEnvironment(values []string, names ...string) []string {
	blocked := make(map[string]struct{}, len(names))
	for _, name := range names {
		blocked[strings.ToLower(name)] = struct{}{}
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		name := strings.ToLower(strings.SplitN(value, "=", 2)[0])
		if _, found := blocked[name]; !found {
			result = append(result, value)
		}
	}
	return result
}
