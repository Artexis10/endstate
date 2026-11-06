// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/provision"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

func TestHomeManagerVerifyDetectsChangedBytesWithUnchangedGeneration(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Home Manager records POSIX targets")
	}
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	target := filepath.Join(t.TempDir(), "settings")
	expected := sha256.Sum256([]byte("declared A"))
	data, err := json.Marshal(map[string]any{
		"generation":       5,
		"fileExpectations": map[string]any{"files": []any{map[string]any{"target": target, "sha256": fmt.Sprintf("%x", expected)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var home provision.HomeGenRef
	if err := json.Unmarshal(data, &home); err != nil {
		t.Fatal(err)
	}
	if err := provision.Write(&provision.Generation{Backend: "nix", HomeManager: &home}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("changed B"), 0600); err != nil {
		t.Fatal(err)
	}
	item := checkHomeManagerGeneration(&fakeRealizer{homeObservation: realizer.HomeObservation{Present: true, Generation: 5, ActivationOutput: "/nix/store/fake-home-output"}}, noopEmitter())
	if item.Status != "fail" || item.Reason != "config_drift" {
		t.Fatalf("unchanged generation hid changed declared file: %+v", item)
	}
}

type expectationHomeBackend struct {
	fakeRealizer
	output       string
	activeOutput string
	prepare      func(string)
	activate     func(string)
	targets      []string
	// recursive reports that an enabled declaration expands to leaves that targets omits.
	recursive bool
	built     bool
}

func (b *expectationHomeBackend) PrepareHome(flake string, build bool) (*realizer.HomePreparation, error) {
	b.built = build
	if build && b.prepare != nil {
		b.prepare(flake)
	}
	return &realizer.HomePreparation{Resolution: &realizer.InputResolution{Outputs: []string{"out"}, OutputPaths: map[string]string{"out": b.output}}, FlatTargets: b.targets, RecursiveDeclarations: b.recursive}, nil
}

func (b *expectationHomeBackend) ActivatePreparedHome(resolution *realizer.InputResolution) (int, error) {
	output := resolution.OutputPaths["out"]
	if b.activate != nil {
		b.activate(output)
	}
	b.homeObservation.Generation++
	b.activeOutput = output
	return b.homeObservation.Generation, nil
}

func (b *expectationHomeBackend) ObserveHome() (realizer.HomeObservation, error) {
	return realizer.HomeObservation{Present: b.homeObservation.Generation > 0, Generation: b.homeObservation.Generation, ActivationOutput: b.activeOutput}, nil
}

func TestHomeManagerFrozenFilesApplyVerifyAndRollback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Home Manager records POSIX targets")
	}
	root := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", root)
	target := filepath.Join(root, "managed")
	source := filepath.Join(root, "source")
	if err := os.WriteFile(source, []byte("A"), 0600); err != nil {
		t.Fatal(err)
	}
	outputs := make(map[string]string)
	for _, value := range []string{"A", "B"} {
		output := filepath.Join(root, "activation-"+value)
		if err := os.MkdirAll(output, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(output, "source"), []byte(value), 0444); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256([]byte(value))
		data, _ := json.Marshal(provision.HomeFileExpectations{Files: []provision.HomeFileExpectation{{Target: filepath.ToSlash(target), SHA256: fmt.Sprintf("%x", sum)}}})
		if err := os.WriteFile(filepath.Join(output, "endstate-home-files.json"), data, 0444); err != nil {
			t.Fatal(err)
		}
		outputs[value] = output
	}
	backend := &expectationHomeBackend{output: outputs["A"]}
	backend.activate = func(output string) {
		_ = os.Remove(target)
		if err := os.Symlink(filepath.Join(output, "source"), target); err != nil {
			t.Fatal(err)
		}
	}
	backend.prepare = func(flake string) {
		// Evaluation already froze A. Both mutable input locations now contain B.
		if err := os.WriteFile(source, []byte("B"), 0600); err != nil {
			t.Fatal(err)
		}
		directory, _, _ := strings.Cut(strings.TrimPrefix(flake, "path:"), "#")
		generated := filepath.Join(directory, "home.nix")
		if err := os.WriteFile(generated, []byte(`throw "changed generated module"`), 0600); err != nil {
			t.Fatal(err)
		}
		files, _ := filepath.Glob(filepath.Join(filepath.Dir(generated), "files", "*"))
		for _, file := range files {
			if err := os.WriteFile(file, []byte("B"), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	mf := &manifest.Manifest{Version: 1, HomeManager: &manifest.HomeManagerConfig{Settings: &manifest.HomeManagerSettings{Files: map[string]string{"managed": source}}}}
	flags := ApplyFlags{Manifest: filepath.Join(root, "manifest.json"), EnableRestore: true}
	if _, err := runApplyRealizer(flags, mf, backend, noopEmitter(), "apply-A", nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	history, err := provision.List()
	if err != nil || len(history) != 1 || history[0].HomeManager.FileExpectations == nil {
		t.Fatalf("apply lost frozen expectations: %+v %v", history, err)
	}
	prior := history[0]
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "different-xdg"))
	if item := checkHomeManagerGeneration(backend, noopEmitter()); item.Status != "pass" {
		t.Fatalf("changed inputs redefined recorded A: %+v", item)
	}
	backend.output, backend.prepare = outputs["B"], nil
	if _, err := runApplyRealizer(flags, mf, backend, noopEmitter(), "apply-B", nil, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	// Rollback must reactivate A without reading the now-changed declaration.
	backend.prepare = func(string) { t.Fatal("rollback evaluated current inputs") }
	raw, rollbackErr := runRealizerRollback(RollbackFlags{To: fmt.Sprint(prior.Number), Confirm: true, EnableRestore: true}, backend)
	if rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
	result := raw.(*RollbackResult)
	if result.HomeManager == nil || result.HomeManager.Verification != "verified" {
		t.Fatalf("rollback did not verify prior expectations: %+v", result)
	}
	history, err = provision.List()
	if err != nil || len(history) != 3 || history[0].HomeManager.FileExpectations.Files[0].SHA256 != prior.HomeManager.FileExpectations.Files[0].SHA256 {
		t.Fatalf("rollback did not carry A into forward history: %+v %v", history, err)
	}
	if item := checkHomeManagerGeneration(backend, noopEmitter()); item.Status != "pass" {
		t.Fatalf("rollback forward history did not verify: %+v", item)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if item := checkHomeManagerGeneration(backend, noopEmitter()); item.Reason != "missing" || item.Status != "fail" {
		t.Fatalf("missing target hid behind active generation: %+v", item)
	}
	if err := os.Symlink(filepath.Join(outputs["B"], "source"), target); err != nil {
		t.Fatal(err)
	}
	if item := checkHomeManagerGeneration(backend, noopEmitter()); item.Reason != "config_drift" || item.Status != "fail" {
		t.Fatalf("changed target hid behind active generation: %+v", item)
	}
	backend.activate = func(string) {} // Native commitment succeeds without fixing drift.
	if _, err := runRealizerRollback(RollbackFlags{To: fmt.Sprint(prior.Number), Confirm: true, EnableRestore: true}, backend); err == nil || string(err.Code) != "ROLLBACK_FAILED" {
		t.Fatalf("rollback mismatch reported success: %+v", err)
	}
	history, err = provision.List()
	if err != nil || len(history) != 4 || !history[0].Partial || !history[0].Rollback || history[0].HomeManager.Generation != backend.homeObservation.Generation || !history[0].HomeManager.ActivationFailed {
		t.Fatalf("verification failure lost committed forward history: %+v %v", history, err)
	}
}

func TestHomeManagerFileEvidenceUnavailableAndEmptyAreDistinct(t *testing.T) {
	for _, evidence := range []struct {
		name   string
		files  *provision.HomeFileExpectations
		status string
	}{
		{name: "legacy", status: "skip"},
		{name: "empty declaration", files: &provision.HomeFileExpectations{Files: []provision.HomeFileExpectation{}}, status: "pass"},
	} {
		t.Run(evidence.name, func(t *testing.T) {
			t.Setenv("ENDSTATE_ROOT", t.TempDir())
			if err := provision.Write(&provision.Generation{Backend: "nix", HomeManager: &provision.HomeGenRef{Generation: 5, FileExpectations: evidence.files}}); err != nil {
				t.Fatal(err)
			}
			item := checkHomeManagerGeneration(&fakeRealizer{homeObservation: realizer.HomeObservation{Present: true, Generation: 5, ActivationOutput: "/nix/store/fake-home-output"}}, noopEmitter())
			if item.Status != evidence.status || strings.Contains(item.Message, "typed settings match") {
				t.Fatalf("file evidence scope was misrepresented: %+v", item)
			}
		})
	}
}

func TestHomeManagerNonregularLeafDoesNotPass(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Home Manager records POSIX targets")
	}
	target := t.TempDir() // A directory cannot be hashed as declared file bytes.
	reason, _ := checkHomeFileExpectations(&provision.HomeFileExpectations{Files: []provision.HomeFileExpectation{{Target: filepath.ToSlash(target), SHA256: strings.Repeat("a", 64)}}})
	if reason != "unreadable" {
		t.Fatalf("nonregular leaf reason = %q, want unreadable", reason)
	}
}

func TestHomeManagerUnreadableFileDoesNotPass(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("requires POSIX permissions and an unprivileged reader")
	}
	target := filepath.Join(t.TempDir(), "unreadable")
	if err := os.WriteFile(target, []byte("A"), 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(target, 0600) })
	reason, _ := checkHomeFileExpectations(&provision.HomeFileExpectations{Files: []provision.HomeFileExpectation{{Target: target, SHA256: strings.Repeat("a", 64)}}})
	if reason != "unreadable" {
		t.Fatalf("unreadable file reason = %q, want unreadable", reason)
	}
}

func TestHomeManagerMalformedFrozenArtifactStopsApplyBeforeMutation(t *testing.T) {
	for name, artifact := range map[string]string{
		"missing":       "missing",
		"null files":    `{"files":null}`,
		"unknown field": `{"files":[],"extra":true}`,
		"oversized":     strings.Repeat(" ", (1<<20)+1),
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("ENDSTATE_ROOT", root)
			backend := &expectationHomeBackend{output: root}
			if artifact != "missing" {
				if err := os.WriteFile(filepath.Join(root, "endstate-home-files.json"), []byte(artifact), 0600); err != nil {
					t.Fatal(err)
				}
			}
			mf := &manifest.Manifest{HomeManager: &manifest.HomeManagerConfig{Settings: &manifest.HomeManagerSettings{}}}
			_, err := runApplyRealizer(ApplyFlags{EnableRestore: true}, mf, backend, noopEmitter(), "invalid-metadata", nil, nil, nil, nil, nil)
			if err == nil || backend.realizeCalls != 0 || backend.homeObservation.Generation != 0 {
				t.Fatalf("invalid activation metadata reached mutation: %+v %+v", backend, err)
			}
		})
	}
}

func TestHomeManagerDoesNotInventEvidenceOutsideFrozenSettings(t *testing.T) {
	for _, input := range []string{"dry-run", "config", "flake"} {
		t.Run(input, func(t *testing.T) {
			root := t.TempDir()
			t.Setenv("ENDSTATE_ROOT", root)
			mf := &manifest.Manifest{HomeManager: &manifest.HomeManagerConfig{}}
			flags := ApplyFlags{EnableRestore: true}
			switch input {
			case "dry-run":
				flags.DryRun = true
				mf.HomeManager.Settings = &manifest.HomeManagerSettings{}
			case "config":
				module := filepath.Join(root, "user.nix")
				if err := os.WriteFile(module, []byte("{ ... }: {}"), 0600); err != nil {
					t.Fatal(err)
				}
				mf.HomeManager.Config = module
			case "flake":
				mf.HomeManager.Flake = "path:/example#profile"
			}
			home, err := prepareHomeManager(flags, mf, &expectationHomeBackend{output: root})
			if err != nil || home == nil || home.fileExpectations != nil {
				t.Fatalf("input without frozen settings gained evidence: %+v %v", home, err)
			}
		})
	}
}

func TestHomeManagerLegacyRollbackRemainsExplicitlyUnverified(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	target := &provision.Generation{Backend: "nix", HomeManager: &provision.HomeGenRef{Generation: 2}}
	if err := provision.Write(target); err != nil {
		t.Fatal(err)
	}
	backend := &fakeRealizer{homeRollbackGen: 3}
	raw, err := runRealizerRollback(RollbackFlags{To: fmt.Sprint(target.Number), Confirm: true, EnableRestore: true}, backend)
	if err != nil {
		t.Fatal(err)
	}
	result := raw.(*RollbackResult)
	if result.HomeManager.Verification != "unverified" || result.HomeManager.VerificationReason != "file_expectations_unavailable" || !result.HomeManager.Reactivated {
		t.Fatalf("legacy rollback invented content verification: %+v", result.HomeManager)
	}
}

func TestHomeManagerInvalidRecordedEvidenceReportsUnreadableHistory(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	if err := os.MkdirAll(provision.Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(provision.Dir(), "000001.json"), []byte(`{"homeManager":{"generation":5,"fileExpectations":{"files":null}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	item := checkHomeManagerGeneration(&fakeRealizer{homeObservation: realizer.HomeObservation{Present: true, Generation: 5, ActivationOutput: "/nix/store/fake-home-output"}}, noopEmitter())
	if item.Status != "fail" || item.Reason != "unreadable" {
		t.Fatalf("invalid history fabricated generation drift: %+v", item)
	}
}
