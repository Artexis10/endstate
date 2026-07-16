// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/migration"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/planner"
	"github.com/Artexis10/endstate/go-engine/internal/restore"
)

func TestConfigRestoreExecutionEmitsLegacyWarningBeforeDryRunAction(t *testing.T) {
	manifestDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(manifestDir, "legacy.json"), []byte(`{"theme":"dark"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "settings.json")
	inputs := emptyConfigRestoreInputs()
	inputs.hasConfigPayloads = true
	inputs.legacyLanes = []configRestoreLegacyLane{{
		captureID: bundle.LegacyCaptureID("apps.legacy"), moduleID: "apps.legacy", configSetID: "legacy",
		restoreEntries: []manifest.RestoreEntry{{Type: "copy", Source: "legacy.json", Target: target, FromModule: "apps.legacy"}},
		selected:       true,
	}}
	runtime := newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot())
	buffer := &bytes.Buffer{}
	emitter := events.NewEmitterWithWriter("legacy-events", true, buffer)
	session := &configRestoreExecutionSession{
		runtime: runtime, coordinator: &staticConfigRestoreCoordinator{final: emptyConfigRestorePlan()},
	}

	result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, DryRun: true, RunID: "restore-test", StateDir: t.TempDir(),
		ManifestDir: manifestDir, Emitter: emitter,
	})
	if envErr != nil {
		t.Fatalf("execute: %+v", envErr)
	}
	if len(result.Plan.Sets) != 1 || result.Plan.Sets[0].Resolution.Resolution != planner.ResolutionLegacyUnverified ||
		result.Plan.Sets[0].Resolution.Status != planner.StatusPlanned {
		t.Fatalf("legacy plan = %+v", result.Plan)
	}
	if len(result.RestoreItems) != 1 || result.RestoreItems[0].CaptureID != bundle.LegacyCaptureID("apps.legacy") ||
		result.RestoreItems[0].SourceGeneration != "" || result.RestoreItems[0].TargetGeneration != "" {
		t.Fatalf("legacy items = %+v", result.RestoreItems)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("dry-run changed target: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) < 2 {
		t.Fatalf("events = %q", buffer.String())
	}
	decoded := make([]map[string]any, len(lines))
	for index := range lines {
		if err := json.Unmarshal([]byte(lines[index]), &decoded[index]); err != nil {
			t.Fatal(err)
		}
	}
	if decoded[0]["event"] != "phase" || decoded[0]["phase"] != "restore" ||
		decoded[1]["event"] != "config-resolution" || decoded[1]["resolution"] != "legacy_unverified" ||
		decoded[len(decoded)-2]["event"] != "restore-item" || decoded[len(decoded)-1]["event"] != "summary" {
		t.Fatalf("event order = %#v", decoded)
	}
	restoreEvents := []map[string]any{}
	for _, event := range decoded {
		if event["event"] == "restore-item" {
			restoreEvents = append(restoreEvents, event)
		}
	}
	if len(restoreEvents) != 2 || restoreEvents[0]["id"] != restoreEvents[1]["id"] ||
		restoreEvents[0]["status"] != "restoring" || restoreEvents[1]["status"] == "restoring" {
		t.Fatalf("restore-item lifecycle = %#v", restoreEvents)
	}
}

func TestConfigRestoreExecutionFramesConsentOffResolutionsWithRestorePhaseAndSummary(t *testing.T) {
	inputs := emptyConfigRestoreInputs()
	inputs.hasConfigPayloads = true
	inputs.legacyLanes = []configRestoreLegacyLane{{
		captureID: bundle.LegacyCaptureID("apps.legacy"), moduleID: "apps.legacy", configSetID: "legacy", selected: true,
	}}
	runtime := newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot())
	buffer := &bytes.Buffer{}
	session := &configRestoreExecutionSession{
		runtime: runtime, coordinator: &staticConfigRestoreCoordinator{final: emptyConfigRestorePlan()},
	}

	_, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: false,
		Emitter:        events.NewEmitterWithWriter("consent-off-events", true, buffer),
	})
	if envErr != nil {
		t.Fatalf("execute: %+v", envErr)
	}
	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("event count = %d, want phase/resolution/summary: %s", len(lines), buffer.String())
	}
	decoded := make([]map[string]any, len(lines))
	for index := range lines {
		if err := json.Unmarshal([]byte(lines[index]), &decoded[index]); err != nil {
			t.Fatal(err)
		}
	}
	if decoded[0]["event"] != "phase" || decoded[0]["phase"] != "restore" ||
		decoded[1]["event"] != "config-resolution" || decoded[2]["event"] != "summary" || decoded[2]["phase"] != "restore" {
		t.Fatalf("event framing = %#v", decoded)
	}
}

func TestConfigRestoreExecutionEmitsExactlyOneDeleteGlobLifecycleForZeroOneOrManyMatches(t *testing.T) {
	for _, matchCount := range []int{0, 1, 2} {
		t.Run(fmt.Sprintf("matches-%d", matchCount), func(t *testing.T) {
			target := t.TempDir()
			for index := 0; index < matchCount; index++ {
				if err := os.WriteFile(filepath.Join(target, fmt.Sprintf("old-%d.tmp", index)), []byte("old"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			inputs := emptyConfigRestoreInputs()
			inputs.hasConfigPayloads = true
			inputs.legacyLanes = []configRestoreLegacyLane{{
				captureID: bundle.LegacyCaptureID("apps.legacy"), moduleID: "apps.legacy", configSetID: "legacy",
				restoreEntries: []manifest.RestoreEntry{{Type: "delete-glob", Target: target, Pattern: "*.tmp", FromModule: "apps.legacy"}},
				selected:       true,
			}}
			buffer := &bytes.Buffer{}
			session := &configRestoreExecutionSession{
				runtime:     newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot()),
				coordinator: &staticConfigRestoreCoordinator{final: emptyConfigRestorePlan()},
			}
			_, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
				RestoreEnabled: true, DryRun: true, ManifestDir: t.TempDir(),
				Emitter: events.NewEmitterWithWriter("delete-glob-events", true, buffer),
			})
			if envErr != nil {
				t.Fatalf("execute: %+v", envErr)
			}
			restoreEvents := []map[string]any{}
			var summary map[string]any
			for _, line := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
				var event map[string]any
				if err := json.Unmarshal([]byte(line), &event); err != nil {
					t.Fatal(err)
				}
				if event["event"] == "restore-item" {
					restoreEvents = append(restoreEvents, event)
				}
				if event["event"] == "summary" && event["phase"] == "restore" {
					summary = event
				}
			}
			if len(restoreEvents) != 2 || restoreEvents[0]["id"] != restoreEvents[1]["id"] ||
				restoreEvents[0]["status"] != "restoring" || restoreEvents[1]["status"] == "restoring" ||
				restoreEvents[0]["restorer"] != "delete-glob" || restoreEvents[1]["restorer"] != "delete-glob" ||
				restoreEvents[0]["target"] != restoreEvents[1]["target"] || summary["total"] != float64(1) {
				t.Fatalf("delete-glob lifecycle = %#v", restoreEvents)
			}
		})
	}
}

func TestConfigRestoreExecutionKeepsDuplicateInputActionsAsDistinctLifecycles(t *testing.T) {
	manifestDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(manifestDir, "settings.json"), []byte("settings"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "settings.json")
	entry := manifest.RestoreEntry{Type: "copy", Source: "settings.json", Target: target, FromModule: "apps.legacy"}
	inputs := emptyConfigRestoreInputs()
	inputs.hasConfigPayloads = true
	inputs.legacyLanes = []configRestoreLegacyLane{{
		captureID: bundle.LegacyCaptureID("apps.legacy"), moduleID: "apps.legacy", configSetID: "legacy",
		restoreEntries: []manifest.RestoreEntry{entry, entry}, selected: true,
	}}
	buffer := &bytes.Buffer{}
	session := &configRestoreExecutionSession{
		runtime:     newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot()),
		coordinator: &staticConfigRestoreCoordinator{final: emptyConfigRestorePlan()},
	}
	_, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, DryRun: true, ManifestDir: manifestDir,
		Emitter: events.NewEmitterWithWriter("duplicate-action-events", true, buffer),
	})
	if envErr != nil {
		t.Fatalf("execute: %+v", envErr)
	}
	restoreEvents := []map[string]any{}
	var summary map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == "restore-item" {
			restoreEvents = append(restoreEvents, event)
		}
		if event["event"] == "summary" && event["phase"] == "restore" {
			summary = event
		}
	}
	if len(restoreEvents) != 4 || restoreEvents[0]["id"] != restoreEvents[1]["id"] ||
		restoreEvents[2]["id"] != restoreEvents[3]["id"] || restoreEvents[0]["id"] == restoreEvents[2]["id"] ||
		restoreEvents[0]["status"] != "restoring" || restoreEvents[2]["status"] != "restoring" ||
		summary["total"] != float64(2) {
		t.Fatalf("duplicate action lifecycles = %#v summary=%#v", restoreEvents, summary)
	}
}

func TestWriteLegacyConfigRestoreJournalReturnsExactAbsolutePathWithoutConfiguredLogsDir(t *testing.T) {
	working := t.TempDir()
	originalWorkingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(working); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(originalWorkingDirectory); err != nil {
			t.Errorf("restore working directory: %v", err)
		}
	})
	logs := filepath.Join(working, "logs")
	if err := os.MkdirAll(logs, 0o700); err != nil {
		t.Fatal(err)
	}
	unrelated := filepath.Join(logs, "restore-journal-unrelated.json")
	if err := os.WriteFile(unrelated, []byte(`{"runId":"unrelated"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(24 * time.Hour)
	if err := os.Chtimes(unrelated, future, future); err != nil {
		t.Fatal(err)
	}

	path, err := writeLegacyConfigRestoreJournal(configRestoreExecutionOptions{
		RunID: "restore-exact-path", ManifestDir: working,
	}, []restore.RestoreResult{{ID: "legacy", Status: "restored", RestoreType: "copy"}})
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(logs, "restore-journal-restore-exact-path.json")
	if path != want || !filepath.IsAbs(path) {
		t.Fatalf("path = %q, want exact absolute %q", path, want)
	}
}

func TestWriteLegacyConfigRestoreJournalDoesNotOverwriteSameRunID(t *testing.T) {
	working := t.TempDir()
	logs := filepath.Join(working, "missing", "nested", "logs")
	options := configRestoreExecutionOptions{RunID: "restore-same-second", JournalLogsDir: logs, ManifestDir: working}
	firstPath, err := writeLegacyConfigRestoreJournal(options, []restore.RestoreResult{{
		ID: "first", Target: "first-target", Status: "restored", RestoreType: "copy",
	}})
	if err != nil {
		t.Fatal(err)
	}
	firstBefore, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	secondPath, err := writeLegacyConfigRestoreJournal(options, []restore.RestoreResult{{
		ID: "second", Target: "second-target", Status: "restored", RestoreType: "copy",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if firstPath == secondPath {
		t.Fatalf("same run ID reused immutable journal path %q", firstPath)
	}
	thirdPath, err := writeLegacyConfigRestoreJournal(options, []restore.RestoreResult{{
		ID: "third", Target: "third-target", Status: "restored", RestoreType: "copy",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if thirdPath == firstPath || thirdPath == secondPath {
		t.Fatalf("third same-run publication reused a journal path: %q", thirdPath)
	}
	firstAfter, err := os.ReadFile(firstPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstBefore, firstAfter) {
		t.Fatal("first registered-candidate journal was overwritten")
	}
	for _, path := range []string{firstPath, secondPath, thirdPath} {
		journal, err := restore.ReadJournal(path)
		if err != nil {
			t.Fatal(err)
		}
		if journal.RunID != options.RunID {
			t.Fatalf("journal %q changed public runId to %q", path, journal.RunID)
		}
	}
	latest, err := restore.FindLatestJournal(logs)
	if err != nil {
		t.Fatal(err)
	}
	if latest != thirdPath {
		t.Fatalf("latest same-run publication = %q, want third %q", latest, thirdPath)
	}
}

func TestEnsureDurableConfigRestoreDirectoryCreatesNestedChain(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "one", "two", "logs")
	created, err := ensureDurableConfigRestoreDirectory(target, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("new nested directory chain was not reported as created")
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("target mode = %v, want directory", info.Mode())
	}
	created, err = ensureDurableConfigRestoreDirectory(target, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("existing directory was reported as newly created")
	}
}

type staticConfigRestoreCoordinator struct {
	preview planner.ConfigPlan
	final   planner.ConfigPlan
}

func (coordinator *staticConfigRestoreCoordinator) Preview(context.Context) (planner.ConfigPlan, error) {
	return planner.CloneConfigPlan(coordinator.preview), nil
}

func (coordinator *staticConfigRestoreCoordinator) Final(context.Context, bool) (planner.ConfigPlan, error) {
	return planner.CloneConfigPlan(coordinator.final), nil
}

func (coordinator *staticConfigRestoreCoordinator) ExecutionPlan() (planner.ConfigPlan, bool) {
	return planner.CloneConfigPlan(coordinator.final), true
}

type recordingLiveConfigRestoreGuard struct {
	base       string
	created    []string
	closeCount int
}

func (guard *recordingLiveConfigRestoreGuard) CreateTransactionRoot(captureID string) (string, error) {
	guard.created = append(guard.created, captureID)
	root := filepath.Join(guard.base, captureID)
	return root, os.Mkdir(root, 0o700)
}

func (guard *recordingLiveConfigRestoreGuard) RegisterLegacyJournal(string) (*configrestore.StoreMember, error) {
	return nil, nil
}

func (guard *recordingLiveConfigRestoreGuard) Close() error {
	guard.closeCount++
	return nil
}

func TestConfigRestoreExecutionContinuesAfterRolledBackSet(t *testing.T) {
	runtime, final := configRestoreExecutionFixture(t, "capture-a", "capture-b")
	guard := &recordingLiveConfigRestoreGuard{base: t.TempDir()}
	var executed []string
	restoreExecutionSeams(t,
		func(context.Context, string, string, configrestore.RegistryMutator) (liveConfigRestoreGuard, error) {
			return guard, nil
		},
		func(_ context.Context, request migration.StageRequest) (*migration.StageResult, error) {
			return &migration.StageResult{Root: t.TempDir(), TargetGeneration: request.TargetGeneration.ID}, nil
		},
		func(_ context.Context, request configrestore.Request) (*configrestore.MaterializedSet, error) {
			return &configrestore.MaterializedSet{Actions: []configrestore.Action{{
				Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob",
				Target: filepath.Join(t.TempDir(), request.Plan.Source.CaptureID), SnapshotRequired: true,
			}}}, nil
		},
		func(_ context.Context, request configRestoreLiveSetRequest) configRestoreSetOutcome {
			executed = append(executed, request.Lineage.CaptureID)
			if request.Lineage.CaptureID == "capture-a" {
				reason := planner.ReasonCommitFailed
				return configRestoreSetOutcome{Status: planner.StatusRolledBack, Reason: &reason, Err: errors.New("write failed"), CanContinue: true}
			}
			return configRestoreSetOutcome{Status: planner.StatusRestored, CanContinue: true}
		},
	)

	session := &configRestoreExecutionSession{
		runtime:     runtime,
		coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
	}
	if _, err := session.Preview(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, RunID: "apply-test", StateDir: t.TempDir(),
	})
	if envErr != nil {
		t.Fatalf("execute: %+v", envErr)
	}
	if !reflect.DeepEqual(executed, []string{"capture-a", "capture-b"}) || guard.closeCount != 1 {
		t.Fatalf("executed=%v closeCount=%d", executed, guard.closeCount)
	}
	if result.Plan.Sets[0].Resolution.Status != planner.StatusRolledBack ||
		result.Plan.Sets[1].Resolution.Status != planner.StatusRestored {
		t.Fatalf("statuses = %s, %s", result.Plan.Sets[0].Resolution.Status, result.Plan.Sets[1].Resolution.Status)
	}
}

func TestConfigRestoreExecutionRecoversBeforeLiveMaterialization(t *testing.T) {
	runtime, final := configRestoreExecutionFixture(t, "capture-a")
	guard := &recordingLiveConfigRestoreGuard{base: t.TempDir()}
	order := []string{}
	restoreExecutionSeams(t,
		func(context.Context, string, string, configrestore.RegistryMutator) (liveConfigRestoreGuard, error) {
			order = append(order, "begin-live")
			return guard, nil
		},
		func(_ context.Context, request migration.StageRequest) (*migration.StageResult, error) {
			order = append(order, "stage")
			return &migration.StageResult{Root: t.TempDir(), TargetGeneration: request.TargetGeneration.ID}, nil
		},
		func(_ context.Context, request configrestore.Request) (*configrestore.MaterializedSet, error) {
			order = append(order, "materialize")
			return &configrestore.MaterializedSet{Actions: []configrestore.Action{{
				Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob",
				Target: filepath.Join(t.TempDir(), request.Plan.Source.CaptureID), SnapshotRequired: true,
			}}}, nil
		},
		func(context.Context, configRestoreLiveSetRequest) configRestoreSetOutcome {
			return configRestoreSetOutcome{Status: planner.StatusRestored, CanContinue: true}
		},
	)

	session := &configRestoreExecutionSession{
		runtime: runtime, coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
	}
	_, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, RunID: "apply-test", StateDir: t.TempDir(),
	})
	if envErr != nil {
		t.Fatalf("execute: %+v", envErr)
	}
	if !reflect.DeepEqual(order, []string{"begin-live", "stage", "materialize"}) {
		t.Fatalf("order = %v", order)
	}
}

func TestConfigRestoreExecutionOrdersResolutionMigrationRollbackAndRestoreItemEvents(t *testing.T) {
	runtime, final := configRestoreExecutionFixture(t, "capture-a")
	hostRoot := final.Sets[0].TargetInstances[0].Root
	guard := &recordingLiveConfigRestoreGuard{base: t.TempDir()}
	buffer := &bytes.Buffer{}
	emitter := events.NewEmitterWithWriter("ordered-events", true, buffer)
	restoreExecutionSeams(t,
		func(context.Context, string, string, configrestore.RegistryMutator) (liveConfigRestoreGuard, error) {
			return guard, nil
		},
		func(_ context.Context, request migration.StageRequest) (*migration.StageResult, error) {
			for _, progress := range []migration.StageProgress{
				{CaptureID: request.CaptureID, Stage: migration.ProgressStaging, Status: migration.ProgressStarted, EdgeIndex: -1},
				{CaptureID: request.CaptureID, Stage: migration.ProgressStaging, Status: migration.ProgressCompleted, EdgeIndex: -1},
				{CaptureID: request.CaptureID, Stage: migration.ProgressEdge, Status: migration.ProgressStarted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
				{CaptureID: request.CaptureID, Stage: migration.ProgressEdge, Status: migration.ProgressCompleted, EdgeIndex: 0, FromGeneration: "g1", ToGeneration: "g2"},
				{CaptureID: request.CaptureID, Stage: migration.ProgressValidation, Status: migration.ProgressStarted, EdgeIndex: 0},
				{CaptureID: request.CaptureID, Stage: migration.ProgressValidation, Status: migration.ProgressCompleted, EdgeIndex: 0},
			} {
				request.Observer.ObserveStageProgress(progress)
			}
			return &migration.StageResult{Root: t.TempDir(), TargetGeneration: request.TargetGeneration.ID}, nil
		},
		func(_ context.Context, request configrestore.Request) (*configrestore.MaterializedSet, error) {
			return &configrestore.MaterializedSet{Actions: []configrestore.Action{{
				Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob",
				Target: filepath.Join(t.TempDir(), request.Plan.Source.CaptureID), SnapshotRequired: true,
			}}}, nil
		},
		func(_ context.Context, request configRestoreLiveSetRequest) configRestoreSetOutcome {
			if request.Ready != nil {
				request.Ready(nil)
			}
			for _, observation := range []configrestore.TransactionObservation{
				{Stage: configrestore.TransactionStageCommit, Progress: configrestore.TransactionProgressStarted},
				{Stage: configrestore.TransactionStageCommit, Progress: configrestore.TransactionProgressFailed, Reason: configrestore.ReasonCommitFailed},
				{Stage: configrestore.TransactionStageRollback, Progress: configrestore.TransactionProgressStarted},
				{Stage: configrestore.TransactionStageRollback, Progress: configrestore.TransactionProgressCompleted},
			} {
				request.Observer.Observe(observation)
			}
			reason := planner.ReasonCommitFailed
			return configRestoreSetOutcome{Status: planner.StatusRolledBack, Reason: &reason, Err: errors.New("commit failed"), CanContinue: true}
		},
	)
	session := &configRestoreExecutionSession{
		runtime: runtime, coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
	}
	_, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, RunID: "apply-test", StateDir: t.TempDir(), Emitter: emitter,
	})
	if envErr != nil {
		t.Fatalf("execute: %+v", envErr)
	}
	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	decoded := make([]map[string]any, len(lines))
	for index, line := range lines {
		if err := json.Unmarshal([]byte(line), &decoded[index]); err != nil {
			t.Fatalf("event %d: %v", index, err)
		}
	}
	if len(decoded) != 15 || decoded[0]["event"] != "phase" || decoded[0]["phase"] != "restore" ||
		decoded[7]["event"] != "config-resolution" || decoded[8]["event"] != "restore-item" ||
		decoded[8]["status"] != "restoring" || decoded[13]["event"] != "restore-item" ||
		decoded[13]["status"] == "restoring" || decoded[8]["id"] != decoded[13]["id"] ||
		decoded[14]["event"] != "summary" || decoded[14]["phase"] != "restore" {
		t.Fatalf("ordered events = %#v", decoded)
	}
	resolutionCount := 0
	for _, event := range decoded {
		if event["event"] == "config-resolution" {
			resolutionCount++
		}
	}
	if resolutionCount != 1 {
		t.Fatalf("resolution count = %d", resolutionCount)
	}
	if decoded[9]["stage"] != "commit" || decoded[9]["status"] != "started" ||
		decoded[10]["stage"] != "commit" || decoded[10]["status"] != "failed" ||
		decoded[11]["stage"] != "rollback" || decoded[11]["status"] != "started" ||
		decoded[12]["stage"] != "rollback" || decoded[12]["status"] != "completed" {
		t.Fatalf("commit/rollback events = %#v", decoded[9:13])
	}
	resolutionJSON, _ := json.Marshal(decoded[7])
	if strings.Contains(string(resolutionJSON), hostRoot) {
		t.Fatalf("config-resolution leaked host-local target root %q: %s", hostRoot, resolutionJSON)
	}
}

func TestConfigRestoreExecutionStreamsFinalPreflightCollisionResolutionOnce(t *testing.T) {
	runtime, final := configRestoreExecutionFixture(t, "capture-a", "capture-b")
	sharedTarget := filepath.Join(t.TempDir(), "settings.json")
	restoreExecutionSeams(t,
		beginLiveConfigRestoreFn,
		func(_ context.Context, request migration.StageRequest) (*migration.StageResult, error) {
			return &migration.StageResult{Root: t.TempDir(), TargetGeneration: request.TargetGeneration.ID}, nil
		},
		func(_ context.Context, request configrestore.Request) (*configrestore.MaterializedSet, error) {
			return &configrestore.MaterializedSet{Actions: []configrestore.Action{{
				Kind: configrestore.ActionWriteFile, Strategy: "copy", Target: sharedTarget, SnapshotRequired: true,
			}}}, nil
		},
		executeLiveConfigRestoreSet,
	)
	buffer := &bytes.Buffer{}
	session := &configRestoreExecutionSession{
		runtime: runtime, coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
	}
	result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, DryRun: true,
		Emitter: events.NewEmitterWithWriter("collision-resolution-events", true, buffer),
	})
	if envErr != nil {
		t.Fatalf("execute: %+v", envErr)
	}
	if result.Plan.Summary.Failed != 2 {
		t.Fatalf("plan = %+v", result.Plan)
	}
	resolutionEvents := []map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(buffer.String()), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatal(err)
		}
		if event["event"] == "config-resolution" {
			resolutionEvents = append(resolutionEvents, event)
		}
	}
	if len(resolutionEvents) != 2 {
		t.Fatalf("resolution events = %#v", resolutionEvents)
	}
	for _, event := range resolutionEvents {
		if event["reason"] != "target_collision" {
			t.Fatalf("streamed preflight resolution = %#v", event)
		}
	}
}

func TestConfigRestoreExecutionBlocksGenerationAndLegacyCollisionBeforeEitherRuns(t *testing.T) {
	runtime, final := configRestoreExecutionFixture(t, "capture-generation")
	manifestDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(manifestDir, "legacy.json"), []byte("legacy"), 0o600); err != nil {
		t.Fatal(err)
	}
	sharedTarget := filepath.Join(t.TempDir(), "settings.json")
	runtime.inputs.hasConfigPayloads = true
	runtime.inputs.legacyLanes = []configRestoreLegacyLane{{
		captureID: bundle.LegacyCaptureID("apps.legacy"), moduleID: "apps.legacy", configSetID: "legacy", selected: true,
		restoreEntries: []manifest.RestoreEntry{{Type: "copy", Source: "legacy.json", Target: sharedTarget, FromModule: "apps.legacy"}},
	}}
	restoreExecutionSeams(t,
		beginLiveConfigRestoreFn,
		func(_ context.Context, request migration.StageRequest) (*migration.StageResult, error) {
			return &migration.StageResult{Root: t.TempDir(), TargetGeneration: request.TargetGeneration.ID}, nil
		},
		func(_ context.Context, request configrestore.Request) (*configrestore.MaterializedSet, error) {
			return &configrestore.MaterializedSet{Actions: []configrestore.Action{{
				Kind: configrestore.ActionWriteFile, Strategy: "copy", Target: sharedTarget, SnapshotRequired: true,
			}}}, nil
		},
		executeLiveConfigRestoreSet,
	)
	session := &configRestoreExecutionSession{
		runtime: runtime, coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
	}
	result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, DryRun: true, ManifestDir: manifestDir,
	})
	if envErr != nil {
		t.Fatalf("execute: %+v", envErr)
	}
	if len(result.Plan.Sets) != 2 || len(result.RestoreItems) != 0 {
		t.Fatalf("collision result = %+v", result)
	}
	for _, set := range result.Plan.Sets {
		if set.Resolution.Status != planner.StatusFailed || set.Resolution.Reason == nil ||
			*set.Resolution.Reason != planner.ReasonTargetCollision {
			t.Fatalf("collision set = %+v", set.Resolution)
		}
	}
	if _, err := os.Stat(sharedTarget); !os.IsNotExist(err) {
		t.Fatalf("collision preflight changed target: %v", err)
	}
}

func TestConfigRestoreExecutionUnifiedCollisionPreflightCoversLegacyOrdinaryAndRegistryScopes(t *testing.T) {
	t.Run("legacy versus legacy", func(t *testing.T) {
		manifestDir := t.TempDir()
		for _, name := range []string{"alpha.json", "beta.json"} {
			if err := os.WriteFile(filepath.Join(manifestDir, name), []byte(name), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		sharedTarget := filepath.Join(t.TempDir(), "settings.json")
		inputs := emptyConfigRestoreInputs()
		inputs.hasConfigPayloads = true
		for index, moduleID := range []string{"apps.alpha", "apps.beta"} {
			inputs.legacyLanes = append(inputs.legacyLanes, configRestoreLegacyLane{
				captureID: bundle.LegacyCaptureID(moduleID), moduleID: moduleID, configSetID: "legacy", selected: true,
				restoreEntries: []manifest.RestoreEntry{{
					Type: "copy", Source: []string{"alpha.json", "beta.json"}[index], Target: sharedTarget, FromModule: moduleID,
				}},
			})
		}
		session := &configRestoreExecutionSession{
			runtime:     newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot()),
			coordinator: &staticConfigRestoreCoordinator{final: emptyConfigRestorePlan()},
		}
		result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
			RestoreEnabled: true, DryRun: true, ManifestDir: manifestDir,
		})
		if envErr != nil || len(result.Plan.Sets) != 2 || len(result.RestoreItems) != 0 {
			t.Fatalf("result=%+v error=%+v", result, envErr)
		}
		for _, set := range result.Plan.Sets {
			if set.Resolution.Status != planner.StatusFailed || set.Resolution.Reason == nil ||
				*set.Resolution.Reason != planner.ReasonTargetCollision {
				t.Fatalf("legacy collision = %+v", set.Resolution)
			}
		}
	})

	t.Run("generation versus ordinary", func(t *testing.T) {
		runtime, final := configRestoreExecutionFixture(t, "capture-generation")
		manifestDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(manifestDir, "ordinary.json"), []byte("ordinary"), 0o600); err != nil {
			t.Fatal(err)
		}
		sharedTarget := filepath.Join(t.TempDir(), "settings.json")
		runtime.inputs.ordinaryRestores = []manifest.RestoreEntry{{Type: "copy", Source: "ordinary.json", Target: sharedTarget}}
		restoreExecutionSeams(t, beginLiveConfigRestoreFn,
			func(_ context.Context, request migration.StageRequest) (*migration.StageResult, error) {
				return &migration.StageResult{Root: t.TempDir(), TargetGeneration: request.TargetGeneration.ID}, nil
			},
			func(context.Context, configrestore.Request) (*configrestore.MaterializedSet, error) {
				return &configrestore.MaterializedSet{Actions: []configrestore.Action{{
					Kind: configrestore.ActionWriteFile, Strategy: "copy", Target: sharedTarget, SnapshotRequired: true,
				}}}, nil
			}, executeLiveConfigRestoreSet,
		)
		session := &configRestoreExecutionSession{
			runtime: runtime, coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
		}
		result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
			RestoreEnabled: true, DryRun: true, ManifestDir: manifestDir,
		})
		if envErr != nil || len(result.Plan.Sets) != 1 || len(result.RestoreItems) != 1 ||
			result.Plan.Sets[0].Resolution.Reason == nil || *result.Plan.Sets[0].Resolution.Reason != planner.ReasonTargetCollision ||
			result.RestoreItems[0].Status != "failed" || result.RestoreItems[0].Error != planner.ReasonTargetCollision.String() {
			t.Fatalf("result=%+v error=%+v", result, envErr)
		}
		if _, err := os.Stat(sharedTarget); !os.IsNotExist(err) {
			t.Fatalf("ordinary collision changed target: %v", err)
		}
	})

	t.Run("registry import subtree versus generation value", func(t *testing.T) {
		runtime, final := configRestoreExecutionFixture(t, "capture-generation")
		manifestDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(manifestDir, "settings.reg"), []byte("Windows Registry Editor Version 5.00\n\n[HKEY_CURRENT_USER\\Software\\Vendor]\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		moduleID := "apps.legacy"
		runtime.inputs.hasConfigPayloads = true
		runtime.inputs.legacyLanes = []configRestoreLegacyLane{{
			captureID: bundle.LegacyCaptureID(moduleID), moduleID: moduleID, configSetID: "legacy", selected: true,
			restoreEntries: []manifest.RestoreEntry{{
				Type: "registry-import", Source: "settings.reg", Target: `HKCU\Software\Vendor`, FromModule: moduleID,
			}},
		}}
		restoreExecutionSeams(t, beginLiveConfigRestoreFn,
			func(_ context.Context, request migration.StageRequest) (*migration.StageResult, error) {
				return &migration.StageResult{Root: t.TempDir(), TargetGeneration: request.TargetGeneration.ID}, nil
			},
			func(context.Context, configrestore.Request) (*configrestore.MaterializedSet, error) {
				return &configrestore.MaterializedSet{Actions: []configrestore.Action{{
					Kind: configrestore.ActionRegistrySet, Strategy: "registry-set", SnapshotRequired: true,
					RegistryValue: &configrestore.RegistryValue{Key: `HKEY_CURRENT_USER\Software\Vendor\App`, ValueName: "Theme"},
				}}}, nil
			}, executeLiveConfigRestoreSet,
		)
		session := &configRestoreExecutionSession{
			runtime: runtime, coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
		}
		result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
			RestoreEnabled: true, DryRun: true, ManifestDir: manifestDir,
		})
		if envErr != nil || len(result.Plan.Sets) != 2 {
			t.Fatalf("result=%+v error=%+v", result, envErr)
		}
		for _, set := range result.Plan.Sets {
			if set.Resolution.Reason == nil || *set.Resolution.Reason != planner.ReasonTargetCollision {
				t.Fatalf("registry collision = %+v", set.Resolution)
			}
		}
	})
}

func TestConfigRestoreExecutionRejectsLegacyScopeEscapesDuringUnifiedPreflight(t *testing.T) {
	t.Run("registry import outside declared subtree", func(t *testing.T) {
		manifestDir := t.TempDir()
		if err := os.WriteFile(filepath.Join(manifestDir, "settings.reg"), []byte(
			"Windows Registry Editor Version 5.00\n\n[HKEY_CURRENT_USER\\Software\\Sibling]\n",
		), 0o600); err != nil {
			t.Fatal(err)
		}
		moduleID := "apps.legacy"
		inputs := configRestoreInputs{hasConfigPayloads: true, legacyLanes: []configRestoreLegacyLane{{
			captureID: bundle.LegacyCaptureID(moduleID), moduleID: moduleID, configSetID: "legacy", selected: true,
			restoreEntries: []manifest.RestoreEntry{{
				Type: "registry-import", Source: "settings.reg", Target: `HKCU\Software\Vendor`, FromModule: moduleID,
			}},
		}}}
		session := &configRestoreExecutionSession{
			runtime:     newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot()),
			coordinator: &staticConfigRestoreCoordinator{final: emptyConfigRestorePlan()},
		}
		result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
			RestoreEnabled: true, DryRun: true, ManifestDir: manifestDir,
		})
		if envErr != nil || len(result.Plan.Sets) != 1 || len(result.RestoreItems) != 0 ||
			result.Plan.Sets[0].Resolution.Status != planner.StatusFailed || result.Plan.Sets[0].Resolution.Reason == nil ||
			*result.Plan.Sets[0].Resolution.Reason != planner.ReasonStagingValidationFailed {
			t.Fatalf("result=%+v error=%+v", result, envErr)
		}
	})

	t.Run("delete glob traversal", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "settings")
		outside := filepath.Join(root, "outside")
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(outside, 0o755); err != nil {
			t.Fatal(err)
		}
		victim := filepath.Join(outside, "victim.tmp")
		if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		moduleID := "apps.legacy"
		inputs := configRestoreInputs{hasConfigPayloads: true, legacyLanes: []configRestoreLegacyLane{{
			captureID: bundle.LegacyCaptureID(moduleID), moduleID: moduleID, configSetID: "legacy", selected: true,
			restoreEntries: []manifest.RestoreEntry{{
				Type: "delete-glob", Target: target, Pattern: `../outside/*.tmp`, FromModule: moduleID,
			}},
		}}}
		session := &configRestoreExecutionSession{
			runtime:     newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot()),
			coordinator: &staticConfigRestoreCoordinator{final: emptyConfigRestorePlan()},
		}
		result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{RestoreEnabled: true, DryRun: true})
		if envErr != nil || len(result.Plan.Sets) != 1 || result.Plan.Sets[0].Resolution.Reason == nil ||
			*result.Plan.Sets[0].Resolution.Reason != planner.ReasonStagingValidationFailed {
			t.Fatalf("result=%+v error=%+v", result, envErr)
		}
		if _, err := os.Stat(victim); err != nil {
			t.Fatalf("out-of-scope file changed: %v", err)
		}
	})
}

func TestConfigRestoreExecutionCollidesCaseAliasesOnInsensitiveVolume(t *testing.T) {
	root := t.TempDir()
	actualParent := filepath.Join(root, "Preferences")
	if err := os.MkdirAll(actualParent, 0o755); err != nil {
		t.Fatal(err)
	}
	aliasParent := filepath.Join(root, "pREFERENCES")
	actualInfo, err := os.Lstat(actualParent)
	if err != nil {
		t.Fatal(err)
	}
	aliasInfo, err := os.Lstat(aliasParent)
	if err != nil || !os.SameFile(actualInfo, aliasInfo) {
		t.Skip("test volume is case-sensitive")
	}
	manifestDir := t.TempDir()
	for _, source := range []string{"one.json", "two.json"} {
		if err := os.WriteFile(filepath.Join(manifestDir, source), []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	inputs := configRestoreInputs{hasConfigPayloads: true, legacyLanes: []configRestoreLegacyLane{
		{captureID: bundle.LegacyCaptureID("apps.one"), moduleID: "apps.one", configSetID: "legacy", selected: true,
			restoreEntries: []manifest.RestoreEntry{{Type: "copy", Source: "one.json", Target: filepath.Join(actualParent, "Theme.JSON")}}},
		{captureID: bundle.LegacyCaptureID("apps.two"), moduleID: "apps.two", configSetID: "legacy", selected: true,
			restoreEntries: []manifest.RestoreEntry{{Type: "copy", Source: "two.json", Target: filepath.Join(aliasParent, "tHEME.json")}}},
	}}
	session := &configRestoreExecutionSession{
		runtime:     newConfigRestoreRuntimeFromInputs(inputs, emptyConfigCatalogSnapshot()),
		coordinator: &staticConfigRestoreCoordinator{final: emptyConfigRestorePlan()},
	}
	result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, DryRun: true, ManifestDir: manifestDir,
	})
	if envErr != nil || len(result.Plan.Sets) != 2 {
		t.Fatalf("result=%+v error=%+v", result, envErr)
	}
	for _, set := range result.Plan.Sets {
		if set.Resolution.Reason == nil || *set.Resolution.Reason != planner.ReasonTargetCollision {
			t.Fatalf("case alias collision = %+v", set.Resolution)
		}
	}
}

func TestConfigRestoreExecutionReturnsStableRecoveryRequiredReason(t *testing.T) {
	runtime, final := configRestoreExecutionFixture(t, "capture-a")
	staged := false
	restoreExecutionSeams(t,
		func(context.Context, string, string, configrestore.RegistryMutator) (liveConfigRestoreGuard, error) {
			return nil, fmt.Errorf("pending restore: %w", configrestore.ErrRecoveryRequired)
		},
		func(context.Context, migration.StageRequest) (*migration.StageResult, error) {
			staged = true
			return nil, errors.New("must not stage")
		},
		materializeConfigRestoreSetFn,
		executeLiveConfigRestoreSet,
	)
	session := &configRestoreExecutionSession{
		runtime: runtime, coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
	}
	_, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, RunID: "apply-test", StateDir: t.TempDir(),
	})
	if envErr == nil || staged {
		t.Fatalf("error=%+v staged=%v", envErr, staged)
	}
	detail, ok := envErr.Detail.(map[string]string)
	if !ok || detail["reason"] != "recovery_required" {
		t.Fatalf("detail = %#v", envErr.Detail)
	}
}

func TestConfigRestoreExecutionTreatsJournalIntentFailureAsCommandFatal(t *testing.T) {
	runtime, final := configRestoreExecutionFixture(t, "capture-a", "capture-b")
	guard := &recordingLiveConfigRestoreGuard{base: t.TempDir()}
	targets := map[string]string{
		"capture-a": filepath.Join(t.TempDir(), "settings-a.json"),
		"capture-b": filepath.Join(t.TempDir(), "settings-b.json"),
	}
	for _, target := range targets {
		if err := os.WriteFile(target, []byte("prior"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	secondMaterialized := false
	restoreExecutionSeams(t,
		func(context.Context, string, string, configrestore.RegistryMutator) (liveConfigRestoreGuard, error) {
			return guard, nil
		},
		func(_ context.Context, request migration.StageRequest) (*migration.StageResult, error) {
			return &migration.StageResult{Root: t.TempDir(), TargetGeneration: request.TargetGeneration.ID}, nil
		},
		func(_ context.Context, request configrestore.Request) (*configrestore.MaterializedSet, error) {
			if request.Plan.Source.CaptureID == "capture-b" {
				secondMaterialized = true
			}
			return &configrestore.MaterializedSet{Actions: []configrestore.Action{{
				Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob",
				Target: targets[request.Plan.Source.CaptureID], SnapshotRequired: true,
			}}}, nil
		},
		executeLiveConfigRestoreSet,
	)
	persistCalls := 0
	originalPersist := persistConfigRestoreJournalIntentFn
	persistConfigRestoreJournalIntentFn = func(context.Context, configrestore.JournalIntentRequest) (*configrestore.JournalIntent, error) {
		persistCalls++
		return nil, errors.New("disk full")
	}
	t.Cleanup(func() { persistConfigRestoreJournalIntentFn = originalPersist })

	session := &configRestoreExecutionSession{
		runtime: runtime, coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
	}
	result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, RunID: "apply-test", StateDir: t.TempDir(),
	})
	if envErr == nil || !secondMaterialized || persistCalls != 1 || guard.closeCount != 1 {
		t.Fatalf("error=%+v secondMaterialized=%v persistCalls=%d closeCount=%d", envErr, secondMaterialized, persistCalls, guard.closeCount)
	}
	detail, ok := envErr.Detail.(map[string]string)
	if !ok || detail["reason"] != "journal_intent_failed" {
		t.Fatalf("detail = %#v", envErr.Detail)
	}
	if result.Plan.Sets[0].Resolution.Status != planner.StatusFailed ||
		result.Plan.Sets[0].Resolution.Reason == nil || *result.Plan.Sets[0].Resolution.Reason != planner.ReasonJournalIntentFailed ||
		result.Plan.Sets[1].Resolution.Status != planner.StatusSkipped || result.Plan.Sets[1].Resolution.Reason == nil ||
		*result.Plan.Sets[1].Resolution.Reason != planner.ReasonRecoveryRequired {
		t.Fatalf("partial failure plan = %+v", result.Plan)
	}
	for _, target := range targets {
		if _, err := os.Stat(target); err != nil {
			t.Fatalf("journal intent failure changed target: %v", err)
		}
	}
}

func TestConfigRestoreExecutionStopsAfterRollbackFailure(t *testing.T) {
	runtime, final := configRestoreExecutionFixture(t, "capture-a", "capture-b")
	guard := &recordingLiveConfigRestoreGuard{base: t.TempDir()}
	var executed []string
	restoreExecutionSeams(t,
		func(context.Context, string, string, configrestore.RegistryMutator) (liveConfigRestoreGuard, error) {
			return guard, nil
		},
		func(_ context.Context, request migration.StageRequest) (*migration.StageResult, error) {
			return &migration.StageResult{Root: t.TempDir(), TargetGeneration: request.TargetGeneration.ID}, nil
		},
		func(_ context.Context, request configrestore.Request) (*configrestore.MaterializedSet, error) {
			return &configrestore.MaterializedSet{Actions: []configrestore.Action{{
				Kind: configrestore.ActionDeleteFile, Strategy: "delete-glob",
				Target: filepath.Join(t.TempDir(), request.Plan.Source.CaptureID), SnapshotRequired: true,
			}}}, nil
		},
		func(_ context.Context, request configRestoreLiveSetRequest) configRestoreSetOutcome {
			executed = append(executed, request.Lineage.CaptureID)
			reason := planner.ReasonCommitFailed
			return configRestoreSetOutcome{Status: planner.StatusRollbackFailed, Reason: &reason, Err: errors.New("rollback failed"), CanContinue: false}
		},
	)

	session := &configRestoreExecutionSession{
		runtime:     runtime,
		coordinator: &staticConfigRestoreCoordinator{preview: final, final: final},
	}
	result, envErr := session.Execute(context.Background(), configRestoreExecutionOptions{
		RestoreEnabled: true, RunID: "apply-test", StateDir: t.TempDir(),
	})
	if envErr != nil {
		t.Fatalf("execute: %+v", envErr)
	}
	if !reflect.DeepEqual(executed, []string{"capture-a"}) || guard.closeCount != 1 {
		t.Fatalf("executed=%v closeCount=%d", executed, guard.closeCount)
	}
	second := result.Plan.Sets[1].Resolution
	if second.Status != planner.StatusSkipped || second.Reason == nil || *second.Reason != planner.ReasonRecoveryRequired {
		t.Fatalf("later set = %+v", second)
	}
}

func configRestoreExecutionFixture(t *testing.T, captureIDs ...string) (*configRestoreRuntime, planner.ConfigPlan) {
	t.Helper()
	digest := strings.Repeat("a", 64)
	runtime := newConfigRestoreRuntimeFromInputs(emptyConfigRestoreInputs(), emptyConfigCatalogSnapshot())
	sets := make([]planner.PlanSet, 0, len(captureIDs))
	for _, captureID := range captureIDs {
		source := planner.SourceCapture{
			CaptureID: captureID, ModuleID: "apps.example", ConfigSetID: "preferences",
			Instance: planner.SourceInstance{ID: "source-" + captureID}, Generation: "g1",
			GenerationFingerprint: digest, ModuleRevision: digest,
		}
		runtime.inputs.generationSources = append(runtime.inputs.generationSources, configRestoreSource{
			source: source, payloadRoot: t.TempDir(), payloadManifest: []manifest.PayloadManifestEntry{}, selected: true,
		})
		target := planner.TargetInstance{
			ID: "target-" + captureID, ModuleID: "apps.example", Generation: "g1", ModuleRevision: digest,
			Root: t.TempDir(),
		}
		generation := &modules.GenerationDef{ID: "g1", Order: 1, Fingerprint: digest}
		set := planner.PlanSet{
			Source: source, TargetInstances: []planner.TargetInstance{target}, TargetGenerationDef: generation,
			Resolution: planner.ConfigResolution{
				CaptureID: captureID, ModuleID: source.ModuleID, ConfigSetID: source.ConfigSetID,
				TargetInstanceID: target.ID, SourceGeneration: "g1", TargetGeneration: "g1",
				SourceGenerationFingerprint: digest, CaptureModuleRevision: digest, RestoreModuleRevision: digest,
				Resolution: planner.ResolutionDirect, MigrationPath: []string{}, ResolvedTargets: []string{}, Status: planner.StatusPlanned,
			},
		}
		sets = append(sets, set)
	}
	return runtime, planner.ConfigPlan{Sets: sets}
}

func restoreExecutionSeams(
	t *testing.T,
	begin func(context.Context, string, string, configrestore.RegistryMutator) (liveConfigRestoreGuard, error),
	stage func(context.Context, migration.StageRequest) (*migration.StageResult, error),
	materialize func(context.Context, configrestore.Request) (*configrestore.MaterializedSet, error),
	execute func(context.Context, configRestoreLiveSetRequest) configRestoreSetOutcome,
) {
	t.Helper()
	originalBegin := beginLiveConfigRestoreFn
	originalStage := stageConfigRestoreSetFn
	originalMaterialize := materializeConfigRestoreSetFn
	originalExecute := executeLiveConfigRestoreSetFn
	beginLiveConfigRestoreFn = begin
	stageConfigRestoreSetFn = stage
	materializeConfigRestoreSetFn = materialize
	executeLiveConfigRestoreSetFn = execute
	t.Cleanup(func() {
		beginLiveConfigRestoreFn = originalBegin
		stageConfigRestoreSetFn = originalStage
		materializeConfigRestoreSetFn = originalMaterialize
		executeLiveConfigRestoreSetFn = originalExecute
	})
}
