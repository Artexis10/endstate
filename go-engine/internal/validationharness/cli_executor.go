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
	type guardFixtureTarget struct {
		authored, coordinate string
		directory            bool
	}
	var guardTargets []guardFixtureTarget
	if runtime.Plan != nil {
		for _, target := range runtime.Plan.Targets {
			guardTargets = append(guardTargets, guardFixtureTarget{target.Authored, target.Coordinate, target.Directory})
		}
	}
	if runtime.V2Plan != nil {
		for _, target := range append(append([]V2FixtureTarget(nil), runtime.V2Plan.CaptureTargets...), runtime.V2Plan.Targets...) {
			guardTargets = append(guardTargets, guardFixtureTarget{target.Authored, target.Coordinate, target.Directory})
		}
	}
	if runtime.CapturePlan != nil {
		for _, target := range runtime.CapturePlan.Targets {
			guardTargets = append(guardTargets, guardFixtureTarget{target.AuthoredSource, target.Coordinate, false})
		}
	}
	for _, target := range guardTargets {
		if strings.HasPrefix(strings.ToLower(target.authored), "${instance.root}") {
			if runtime.V2Plan == nil || !strings.EqualFold(target.authored, "${instance.root}") {
				return fmt.Errorf("instance-root guard lacks exact detector authority")
			}
			alias, detectorSuffix, ok := authoredAliasSuffix(runtime.V2Plan.Compiled.Detector.Glob)
			if !ok {
				return fmt.Errorf("instance-root detector has no declared alias")
			}
			wildcard := strings.IndexAny(detectorSuffix, "*?[")
			if wildcard < 0 {
				return fmt.Errorf("instance-root detector is not a production glob")
			}
			prefix := strings.TrimRight(detectorSuffix[:wildcard], `\/`)
			parent := filepath.Dir(filepath.FromSlash(strings.ReplaceAll(prefix, `\`, "/")))
			if parent == "." || strings.ContainsAny(parent, "*?[") {
				return fmt.Errorf("instance-root detector anchor is ambiguous")
			}
			aliasRoot := filepath.Join(runtime.GuardRoot, strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(alias, "(", "-"), ")", "")))
			runtime.OriginalEnvironment[alias] = aliasRoot
			guardPath := filepath.Join(aliasRoot, parent, filepath.Base(runtime.V2Plan.Instance.Root))
			if target.directory {
				guardPath = filepath.Join(guardPath, fixturePayloadName)
			}
			if _, exists := seenGuards[strings.ToLower(guardPath)]; exists {
				continue
			}
			seenGuards[strings.ToLower(guardPath)] = struct{}{}
			content := fixtureSentinel(runtime.Module.ID, runtime.Scenario.ID, target.coordinate, "original")
			if err := os.MkdirAll(filepath.Dir(guardPath), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(guardPath, []byte(content), 0o600); err != nil {
				return err
			}
			runtime.Guards = append(runtime.Guards, guardTarget{Path: guardPath, Content: content})
			continue
		}
		alias, suffix, ok := authoredAliasSuffix(target.authored)
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
		if target.directory {
			guardPath = filepath.Join(guardPath, fixturePayloadName)
		}
		if _, exists := seenGuards[strings.ToLower(guardPath)]; exists {
			continue
		}
		seenGuards[strings.ToLower(guardPath)] = struct{}{}
		content := fixtureSentinel(runtime.Module.ID, runtime.Scenario.ID, target.coordinate, "original")
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
	if runtime.validationContext().ValidateSandboxPath(runtime.ChildWorkingDir) == nil {
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
			if runtime.InstallPlan == nil {
				if err := os.WriteFile(filepath.Join(runtime.ToolRoot, name), []byte("endstate validation command sentinel"), 0o700); err != nil {
					return err
				}
			} else if name != runtime.InstallPlan.CommandExecutable {
				return fmt.Errorf("verify[%d] command differs from compiled install authority", index)
			}
		case "file-exists":
			path, err := runtime.validationContext().ResolveHostPath(verifier.Path, validationmode.HostPathPolicy{})
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
	v2FirstRebuild   v2TransactionBinding
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
	values["PATH"] = strings.Join(installPATHParts(runtime), string(os.PathListSeparator))
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

func installPATHParts(runtime *scenarioRuntime) []string {
	pathParts := []string{runtime.ToolRoot}
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		pathParts = append(pathParts, filepath.Join(systemRoot, "System32"), systemRoot)
	}
	return pathParts
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
	return executor.runWithEventPolicy(ctx, false, command, args...)
}

func (executor *cliJourneyExecutor) runExpectingVerifyFailure(ctx context.Context, args ...string) (commandOutput, *Failure) {
	return executor.runWithEventPolicy(ctx, true, "verify", args...)
}

func (executor *cliJourneyExecutor) runWithEventPolicy(ctx context.Context, allowVerifyFailure bool, command string, args ...string) (commandOutput, *Failure) {
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
	var events []map[string]any
	if allowVerifyFailure {
		events, failure = decodeExpectedVerifyFailureEvents(stderr.Bytes(), command, envelopeValue.RunID, forbidden...)
	} else {
		events, failure = decodeEvents(stderr.Bytes(), command, envelopeValue.RunID, forbidden...)
	}
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

func (executor *cliJourneyExecutor) ApplyDryRun(ctx context.Context, runtime *scenarioRuntime) *Failure {
	if failure := executor.assertInstallPATH(); failure != nil {
		return failure
	}
	output, failure := executor.run(ctx, "apply", "--manifest", runtime.InstallPlan.ManifestPath, "--dry-run", "--only", runtime.Inventory.AppID)
	if failure != nil {
		return failure
	}
	return validateInstallApplyEvidence(output.Envelope.Data, output.Events, runtime)
}

func (executor *cliJourneyExecutor) VerifyAbsent(ctx context.Context, runtime *scenarioRuntime) *Failure {
	if failure := executor.assertInstallPATH(); failure != nil {
		return failure
	}
	output, failure := executor.runExpectingVerifyFailure(ctx, "--manifest", runtime.InstallPlan.ManifestPath)
	if failure != nil {
		return failure
	}
	return validateInstallVerifyEvidence(output.Envelope.Data, output.Events, runtime, false)
}

func (executor *cliJourneyExecutor) VerifyPresent(ctx context.Context, runtime *scenarioRuntime) *Failure {
	if failure := executor.assertInstallPATH(); failure != nil {
		return failure
	}
	output, failure := executor.run(ctx, "verify", "--manifest", runtime.InstallPlan.ManifestPath)
	if failure != nil {
		return failure
	}
	return validateInstallVerifyEvidence(output.Envelope.Data, output.Events, runtime, true)
}

func (executor *cliJourneyExecutor) assertInstallPATH() *Failure {
	if executor == nil || executor.runtime == nil || executor.runtime.InstallPlan == nil {
		return fail(CodeIsolationFailure, "install", "PATH", "install executor authority is absent")
	}
	want := "PATH=" + strings.Join(installPATHParts(executor.runtime), string(os.PathListSeparator))
	count := 0
	for _, value := range executor.environment {
		if strings.HasPrefix(strings.ToUpper(value), "PATH=") {
			count++
			if value != want {
				return fail(CodeIsolationFailure, "install", "PATH", "install child PATH contains authority outside ToolRoot and system roots")
			}
		}
	}
	if count != 1 {
		return fail(CodeIsolationFailure, "install", "PATH", "install child PATH is absent or duplicated")
	}
	return nil
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

func (executor *cliJourneyExecutor) CaptureContract(ctx context.Context, runtime *scenarioRuntime) (captureContractEvidence, *Failure) {
	manifestPath := filepath.Join(runtime.Root, "manifests", "captured.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		return captureContractEvidence{}, fail(CodeIsolationFailure, "capture", "manifest", "create capture contract output parent")
	}
	output, failure := executor.run(ctx, "capture", "--out", manifestPath, "--only", runtime.Inventory.AppID+","+runtime.Module.ID)
	if failure != nil {
		return captureContractEvidence{}, failure
	}
	zipPath := strings.TrimSuffix(manifestPath, filepath.Ext(manifestPath)) + ".zip"
	if failure := validateCaptureContractCommandEvidence(output.Envelope.Data, output.Events, runtime, filepath.Base(zipPath)); failure != nil {
		return captureContractEvidence{}, failure
	}
	return inspectCaptureContractArtifact(runtime, zipPath)
}

func (executor *cliJourneyExecutor) CaptureContractOptionalAbsent(ctx context.Context, runtime *scenarioRuntime) *Failure {
	manifestPath := filepath.Join(runtime.Root, "manifests", "optional-absent.jsonc")
	output, failure := executor.run(ctx, "capture", "--out", manifestPath, "--only", runtime.Inventory.AppID+","+runtime.Module.ID)
	if failure != nil {
		return failure
	}
	zipPath := strings.TrimSuffix(manifestPath, filepath.Ext(manifestPath)) + ".zip"
	return validateCaptureContractOptionalAbsentOutcome(output.Envelope.Data, output.Events, runtime, zipPath)
}

func (executor *cliJourneyExecutor) CaptureV2(ctx context.Context, runtime *scenarioRuntime) (captureEvidence, *Failure) {
	manifestPath := filepath.Join(runtime.Root, "manifests", "captured.jsonc")
	if err := os.MkdirAll(filepath.Dir(manifestPath), 0o700); err != nil {
		return captureEvidence{}, fail(CodeIsolationFailure, "capture", "manifest", "create schema-v2 capture output parent")
	}
	output, failure := executor.run(ctx, "capture", "--out", manifestPath, "--only", runtime.Inventory.AppID+","+runtime.Module.ID)
	if failure != nil {
		return captureEvidence{}, failure
	}
	zipPath := strings.TrimSuffix(manifestPath, filepath.Ext(manifestPath)) + ".zip"
	return inspectV2CaptureArtifact(runtime, zipPath, output.Envelope.Data)
}

func (executor *cliJourneyExecutor) TransitionV2(_ context.Context, runtime *scenarioRuntime, evidence captureEvidence) *Failure {
	if runtime == nil || runtime.V2Transition == nil || evidence.ArtifactPath == "" {
		return fail(CodeMigrationContract, "transition", "state", "migration transition authority or captured bundle is absent")
	}
	return runtime.V2Transition.Apply(evidence.ArtifactPath)
}

func (executor *cliJourneyExecutor) RebuildV2(ctx context.Context, runtime *scenarioRuntime, evidence captureEvidence) *Failure {
	if runtime.V2Transition != nil {
		if failure := runtime.V2Transition.ValidateBundle(evidence.ArtifactPath); failure != nil {
			return failure
		}
	}
	storageBefore, failure := snapshotV2Storage(runtime)
	if failure != nil {
		return failure
	}
	output, failure := executor.run(ctx, "rebuild", "--from", evidence.ArtifactPath, "--only", runtime.Inventory.AppID, "--confirm")
	if failure != nil {
		return failure
	}
	if runtime.V2Transition != nil {
		if failure := runtime.V2Transition.ValidateReinitialized(evidence.ArtifactPath); failure != nil {
			return failure
		}
	}
	validated, failure := validateV2RebuildEvidence(output.Envelope.Data, output.Events, runtime, executor.rebuildIteration)
	if failure != nil {
		return failure
	}
	binding, failure := validateV2RebuildStorage(ctx, runtime, executor.rebuildIteration, storageBefore, validated)
	if failure != nil {
		return failure
	}
	if executor.rebuildIteration == 0 {
		executor.v2FirstRebuild = binding
	}
	executor.rebuildIteration++
	return nil
}

func (executor *cliJourneyExecutor) RevertV2(ctx context.Context, runtime *scenarioRuntime) *Failure {
	if runtime.V2Transition != nil {
		if failure := runtime.V2Transition.ValidateBundle(runtime.V2Transition.bundlePath); failure != nil {
			return failure
		}
	}
	storageBefore, failure := snapshotV2Storage(runtime)
	if failure != nil {
		return failure
	}
	if runtime.V2Transition != nil {
		if failure := runtime.V2Transition.ValidateReinitialized(runtime.V2Transition.bundlePath); failure != nil {
			return failure
		}
	}
	output, failure := executor.run(ctx, "revert")
	if failure != nil {
		return failure
	}
	if failure := validateV2RevertEvidence(output.Envelope.Data, output.Events, runtime); failure != nil {
		return failure
	}
	return validateV2RevertStorage(runtime, storageBefore, executor.v2FirstRebuild)
}

func (executor *cliJourneyExecutor) VerifyV2(ctx context.Context, runtime *scenarioRuntime, evidence captureEvidence) *Failure {
	if runtime.V2Transition != nil {
		if failure := runtime.V2Transition.ValidateReinitialized(evidence.ArtifactPath); failure != nil {
			return failure
		}
	}
	output, failure := executor.run(ctx, "verify", "--manifest", evidence.VerifyManifest)
	if failure != nil {
		return failure
	}
	if runtime.V2Transition != nil {
		if failure := runtime.V2Transition.ValidateReinitialized(evidence.ArtifactPath); failure != nil {
			return failure
		}
	}
	if failure := validateVerifyEvidence(output.Envelope.Data, runtime, "verify"); failure != nil {
		return failure
	}
	return validateV2VerifyEventSegments(output.Events, runtime, false)
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
var _ installJourneyExecutor = (*cliJourneyExecutor)(nil)
