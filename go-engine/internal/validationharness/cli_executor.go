// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

const maxCommandOutputBytes = 8 << 20

type guardTarget struct {
	Path    string
	Content string
}

func (runtime *scenarioRuntime) prepareGuardsAndTools() error {
	runtime.OriginalEnvironment = map[string]string{}
	seenGuards := map[string]struct{}{}
	for _, target := range runtime.Plan.Targets {
		alias, suffix, ok := authoredAliasSuffix(target.Authored)
		if !ok {
			return fmt.Errorf("fixture target has no declared alias")
		}
		aliasRoot := filepath.Join(runtime.GuardRoot, strings.ToLower(strings.ReplaceAll(alias, "(", "-")))
		aliasRoot = strings.ReplaceAll(aliasRoot, ")", "")
		runtime.OriginalEnvironment[alias] = aliasRoot
		if err := os.MkdirAll(aliasRoot, 0o700); err != nil {
			return err
		}
		guardPath := filepath.Join(aliasRoot, filepath.FromSlash(strings.ReplaceAll(suffix, `\`, "/")))
		if target.Directory {
			guardPath = filepath.Join(guardPath, fixturePayloadName)
		}
		if _, exists := seenGuards[strings.ToLower(guardPath)]; exists {
			continue
		}
		seenGuards[strings.ToLower(guardPath)] = struct{}{}
		content := fixtureSentinel(runtime.Module.ID, runtime.Scenario.ID, target.Coordinate, "original")
		if err := os.MkdirAll(filepath.Dir(guardPath), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(guardPath, []byte(content), 0o600); err != nil {
			return err
		}
		runtime.Guards = append(runtime.Guards, guardTarget{Path: guardPath, Content: content})
	}

	runtime.ToolRoot = filepath.Join(runtime.Root, "state", "validation-tools")
	if err := os.MkdirAll(runtime.ToolRoot, 0o700); err != nil {
		return err
	}
	if runtime.ChildWorkingDir == "" || filepath.Dir(runtime.ChildWorkingDir) != runtime.AuthorityRoot || !fixtureContained(runtime.AuthorityRoot, runtime.ChildWorkingDir) {
		return fmt.Errorf("validation child working directory is outside task authority")
	}
	if runtime.Plan.context.ValidateSandboxPath(runtime.ChildWorkingDir) == nil {
		return fmt.Errorf("validation child working directory overlaps engine mutation authority")
	}
	if err := safepath.ValidateRoot(runtime.ChildWorkingDir); err != nil {
		return err
	}
	if info, err := os.Lstat(runtime.ChildWorkingDir); err != nil || safepath.IsLinkOrReparse(info) || !info.IsDir() {
		return fmt.Errorf("validation child working directory is not a regular directory")
	}
	for index, verifier := range runtime.Module.Verify {
		switch verifier.Type {
		case "command-exists":
			name := filepath.Base(verifier.Command)
			if name == "" || name != verifier.Command || strings.ContainsAny(name, `\/:`) {
				return fmt.Errorf("verify[%d] command is not a contained executable name", index)
			}
			if filepath.Ext(name) == "" {
				name += ".exe"
			}
			if err := os.WriteFile(filepath.Join(runtime.ToolRoot, name), []byte("endstate validation command sentinel"), 0o700); err != nil {
				return err
			}
		case "file-exists":
			path, err := runtime.Plan.context.ResolveHostPath(verifier.Path, validationmode.HostPathPolicy{})
			if err != nil {
				return fmt.Errorf("verify[%d] path: %w", index, err)
			}
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			if _, err := os.Stat(path); os.IsNotExist(err) {
				if err := os.WriteFile(path, []byte("endstate validation verifier sentinel"), 0o600); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		default:
			return fmt.Errorf("verify[%d] type %q is unsupported by Task 7A", index, verifier.Type)
		}
	}
	return nil
}

func authoredAliasSuffix(authored string) (string, string, bool) {
	if !strings.HasPrefix(authored, "%") {
		return "", "", false
	}
	closing := strings.Index(authored[1:], "%")
	if closing < 0 {
		return "", "", false
	}
	closing++
	alias := authored[1:closing]
	suffix := strings.TrimLeft(authored[closing+1:], `\/`)
	if alias == "" || suffix == "" {
		return "", "", false
	}
	return alias, suffix, true
}

func (runtime *scenarioRuntime) assertGuards() *Failure {
	if err := safepath.ValidateRoot(runtime.GuardRoot); err != nil {
		return fail(CodeIsolationFailure, "guard", "originalHost", "original-host guard root changed")
	}
	for _, guard := range runtime.Guards {
		if !fixtureContained(runtime.GuardRoot, guard.Path) {
			return fail(CodeIsolationFailure, "guard", "originalHost", "original-host guard escaped its root")
		}
		info, err := os.Lstat(guard.Path)
		if err != nil || safepath.IsLinkOrReparse(info) || !info.Mode().IsRegular() {
			return fail(CodeIsolationFailure, "guard", "originalHost", "original-host fixture changed type")
		}
		data, _, err := safepath.ReadRegularFile(guard.Path)
		if err != nil || string(data) != guard.Content {
			return fail(CodeIsolationFailure, "guard", "originalHost", "original-host fixture changed")
		}
	}
	return nil
}

type cliJourneyExecutor struct {
	selected         *selection
	runtime          *scenarioRuntime
	environment      []string
	workingDir       string
	rebuildIteration int
	firstRebuild     rebuildEvidenceBinding
}

func newCLIJourneyExecutor(selected *selection, runtime *scenarioRuntime) *cliJourneyExecutor {
	return &cliJourneyExecutor{
		selected: selected, runtime: runtime, workingDir: runtime.ChildWorkingDir,
		environment: childEnvironment(runtime),
	}
}

func childEnvironment(runtime *scenarioRuntime) []string {
	values := map[string]string{
		validationmode.TestModeEnvironment: "1",
		validationmode.RootEnvironment:     runtime.Root,
		"GOTELEMETRY":                      "off",
	}
	for _, name := range []string{"SystemRoot", "WINDIR", "COMSPEC", "PATHEXT", "PROCESSOR_ARCHITECTURE", "NUMBER_OF_PROCESSORS", "TEMP", "TMP"} {
		if value := os.Getenv(name); value != "" {
			values[name] = value
		}
	}
	for name, value := range runtime.OriginalEnvironment {
		values[name] = value
	}
	pathParts := []string{runtime.ToolRoot}
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		pathParts = append(pathParts, filepath.Join(systemRoot, "System32"), systemRoot)
	}
	values["PATH"] = strings.Join(pathParts, string(os.PathListSeparator))
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sortStringsFold(names)
	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name+"="+values[name])
	}
	return result
}

func sortStringsFold(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && strings.ToLower(values[j]) < strings.ToLower(values[j-1]); j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

type commandOutput struct {
	Envelope decodedEnvelope
	Events   []map[string]any
}

func (executor *cliJourneyExecutor) run(ctx context.Context, command string, args ...string) (commandOutput, *Failure) {
	bounded, cancel := context.WithTimeout(ctx, time.Duration(executor.runtime.Scenario.TimeoutSeconds)*time.Second)
	defer cancel()
	allArgs := append([]string{command}, args...)
	allArgs = append(allArgs, "--json", "--events", "jsonl")
	process := exec.CommandContext(bounded, executor.selected.request.EnginePath, allArgs...)
	process.Dir = executor.workingDir
	process.Env = executor.environment
	stdout, stderr := &boundedBuffer{limit: maxCommandOutputBytes}, &boundedBuffer{limit: maxCommandOutputBytes}
	process.Stdout, process.Stderr = stdout, stderr
	processErr := process.Run()
	if boundaryFailure := executor.runtime.assertIndependentBoundaries(); boundaryFailure != nil {
		return commandOutput{}, boundaryFailure
	}
	if stdout.exceeded || stderr.exceeded {
		return commandOutput{}, fail(CodeExecutionFailure, command, "output", "engine output exceeded the validation limit")
	}
	forbidden := executor.runtime.forbiddenOutputValues()
	envelopeValue, failure := decodeEnvelope(stdout.Bytes(), command, executor.runtime.Module.ID, executor.runtime.Scenario.ID, forbidden...)
	if failure != nil {
		return commandOutput{}, failure
	}
	events, failure := decodeEvents(stderr.Bytes(), command, envelopeValue.RunID, forbidden...)
	if failure != nil {
		return commandOutput{}, failure
	}
	if processErr != nil {
		if bounded.Err() != nil {
			return commandOutput{}, fail(CodeExecutionFailure, command, "timeout", "engine command exceeded the sidecar timeout")
		}
		return commandOutput{}, fail(CodeExecutionFailure, command, "exit", "engine process exited unsuccessfully despite its envelope")
	}
	return commandOutput{Envelope: envelopeValue, Events: events}, nil
}

func (executor *cliJourneyExecutor) Capture(ctx context.Context, runtime *scenarioRuntime) (captureEvidence, *Failure) {
	manifestPath := filepath.Join(runtime.Root, "manifests", "captured.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		return captureEvidence{}, fail(CodeIsolationFailure, "capture", "manifest", "create capture output parent")
	}
	_, failure := executor.run(ctx, "capture", "--out", manifestPath, "--only", runtime.Inventory.AppID+","+runtime.Module.ID)
	if failure != nil {
		return captureEvidence{}, failure
	}
	zipPath := strings.TrimSuffix(manifestPath, filepath.Ext(manifestPath)) + ".zip"
	return inspectCaptureArtifact(runtime, zipPath)
}

func (executor *cliJourneyExecutor) CaptureOptionalAbsent(ctx context.Context, runtime *scenarioRuntime) *Failure {
	manifestPath := filepath.Join(runtime.Root, "manifests", "optional-absent.jsonc")
	if _, failure := executor.run(ctx, "capture", "--out", manifestPath, "--only", runtime.Inventory.AppID+","+runtime.Module.ID); failure != nil {
		return failure
	}
	zipPath := strings.TrimSuffix(manifestPath, filepath.Ext(manifestPath)) + ".zip"
	info, err := os.Lstat(zipPath)
	if os.IsNotExist(err) {
		for _, target := range runtime.Plan.Targets {
			if !target.Optional {
				return fail(CodeArtifactContract, "capture", target.Coordinate, "required config disappeared from optional-absence capture")
			}
		}
		if manifestInfo, manifestErr := os.Lstat(manifestPath); manifestErr != nil || !manifestInfo.Mode().IsRegular() || safepath.IsLinkOrReparse(manifestInfo) {
			return fail(CodeArtifactContract, "capture", "optional", "optional-absence capture emitted no regular artifact")
		}
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || safepath.IsLinkOrReparse(info) {
		return fail(CodeArtifactContract, "capture", "optional", "optional-absence bundle is linked or malformed")
	}
	entries, failure := readCaptureArtifactEntries(zipPath)
	if failure != nil {
		return failure
	}
	return validateOptionalAbsentArtifactEntries(runtime, entries)
}

func (executor *cliJourneyExecutor) Rebuild(ctx context.Context, runtime *scenarioRuntime, evidence captureEvidence) *Failure {
	iteration := executor.rebuildIteration
	storageBefore, failure := snapshotRebuildStorage(runtime)
	if failure != nil {
		return failure
	}
	output, failure := executor.run(ctx, "rebuild", "--from", evidence.ArtifactPath, "--only", runtime.Inventory.AppID, "--confirm")
	if failure != nil {
		return failure
	}
	if failure := validateRebuildEvidence(output.Envelope.Data, runtime, iteration); failure != nil {
		return failure
	}
	binding, _, failure := validateRebuildStorageEvidence(runtime, output.Envelope.Data, iteration, storageBefore)
	if failure != nil {
		return failure
	}
	if iteration == 0 {
		executor.firstRebuild = binding
	}
	executor.rebuildIteration++
	return nil
}

func (executor *cliJourneyExecutor) Revert(ctx context.Context, runtime *scenarioRuntime) *Failure {
	output, failure := executor.run(ctx, "revert")
	if failure != nil {
		return failure
	}
	return validateRevertEvidence(output.Envelope.Data, runtime, executor.firstRebuild)
}

func (executor *cliJourneyExecutor) Verify(ctx context.Context, runtime *scenarioRuntime, evidence captureEvidence) (int, *Failure) {
	output, failure := executor.run(ctx, "verify", "--manifest", evidence.VerifyManifest)
	if failure != nil {
		return 0, failure
	}
	if failure := validateVerifyEvidence(output.Envelope.Data, runtime, "verify"); failure != nil {
		return 0, failure
	}
	return 1 + len(runtime.Module.Verify), nil
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	original := len(data)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = true
		return original, nil
	}
	if len(data) > remaining {
		buffer.exceeded = true
		data = data[:remaining]
	}
	_, _ = buffer.buffer.Write(data)
	return original, nil
}

func (buffer *boundedBuffer) Bytes() []byte { return buffer.buffer.Bytes() }

var _ journeyExecutor = (*cliJourneyExecutor)(nil)
