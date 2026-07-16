// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/driver"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/realizer"
)

// ---------------------------------------------------------------------------
// rebuild test helpers
// ---------------------------------------------------------------------------

// envRef returns a host-appropriate environment-variable reference so restore
// targets expand on both Windows (%VAR%) and Unix ($VAR).
func envRef(name string) string {
	if runtime.GOOS == "windows" {
		return "%" + name + "%"
	}
	return "$" + name
}

// ghostDriver reports every install as a fresh success but always detects the
// package as ABSENT — modelling "installer claimed success, app is still
// missing" so verify reports a failure.
type ghostDriver struct{ installCalls int }

func (g *ghostDriver) Name() string { return "ghost" }

func (g *ghostDriver) Detect(ref string) (bool, string, error) { return false, "", nil }

func (g *ghostDriver) DetectBatch(refs []string) (map[string]driver.DetectResult, error) {
	m := make(map[string]driver.DetectResult, len(refs))
	for _, r := range refs {
		m[r] = driver.DetectResult{Installed: false}
	}
	return m, nil
}

func (g *ghostDriver) Install(ref string) (*driver.InstallResult, error) {
	g.installCalls++
	return &driver.InstallResult{Status: driver.StatusInstalled, Message: "installed (ghost)"}, nil
}

// withDriverFn installs an arbitrary driver.Driver on the no-realizer (Windows-
// like) path, mirroring withMockDriver for a non-*mockDriver backend.
func withDriverFn(d driver.Driver, f func()) {
	origDriver := newDriverFn
	origRealizer := newRealizerFn
	origBrew := newBrewDriverFn
	newDriverFn = func() (driver.Driver, error) { return d, nil }
	newRealizerFn = func() (realizer.Realizer, error) { return nil, ErrNoRealizer }
	newBrewDriverFn = func() (driver.Driver, error) { return nil, ErrNoBrewDriver }
	defer func() {
		newDriverFn = origDriver
		newRealizerFn = origRealizer
		newBrewDriverFn = origBrew
	}()
	f()
}

// captureStderr redirects os.Stderr for the duration of f and returns everything
// written to it. The event emitters read os.Stderr at construction time, so a
// RunRebuild invoked inside f streams its NDJSON into the returned string.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	done := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()
	f()
	_ = w.Close()
	os.Stderr = old
	out := <-done
	_ = r.Close()
	return out
}

// bareManifest writes a minimal one-app manifest and returns its path.
func bareManifest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "machine.jsonc")
	content := `{
  "version": 1,
  "name": "bare-machine",
  "apps": [ { "id": "bare-app", "refs": { "windows": "TestVendor.Bare" } } ]
}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// seedRoundTripBundle builds a real capture bundle whose single module captures
// a seeded config file and restores it to <ENDSTATE_TEST_ROOT>/cfg/settings.json.
// It sets ENDSTATE_TEST_ROOT (the restore-target root) and returns the zip path,
// that root, and the captured file content.
func seedRoundTripBundle(t *testing.T) (zipPath, restoreRoot, wantContent string) {
	t.Helper()
	work := t.TempDir()

	srcConfig := filepath.Join(work, "src", "settings.json")
	if err := os.MkdirAll(filepath.Dir(srcConfig), 0o755); err != nil {
		t.Fatal(err)
	}
	wantContent = `{"editor.fontSize":15,"roundtrip":true}`
	if err := os.WriteFile(srcConfig, []byte(wantContent), 0o644); err != nil {
		t.Fatal(err)
	}

	manifestPath := filepath.Join(work, "manifest.jsonc")
	manifestContent := `{
  "version": 1,
  "name": "roundtrip",
  "apps": [ { "id": "roundtrip-app", "refs": { "windows": "TestVendor.RoundTrip" } } ]
}`
	if err := os.WriteFile(manifestPath, []byte(manifestContent), 0o644); err != nil {
		t.Fatal(err)
	}

	restoreRoot = t.TempDir()
	t.Setenv("ENDSTATE_TEST_ROOT", restoreRoot)

	mod := &modules.Module{
		ID:          "apps.roundtrip-app",
		DisplayName: "RoundTrip App",
		Matches:     modules.MatchCriteria{Winget: []string{"TestVendor.RoundTrip"}},
		Restore: []modules.RestoreDef{{
			Type:     "copy",
			Source:   "./payload/apps/roundtrip-app/settings.json",
			Target:   filepath.Join(envRef("ENDSTATE_TEST_ROOT"), "cfg", "settings.json"),
			Backup:   true,
			Optional: true,
		}},
		Capture: &modules.CaptureDef{
			Files: []modules.CaptureFile{{Source: srcConfig, Dest: "apps/roundtrip-app/settings.json"}},
		},
	}

	zipPath = filepath.Join(work, "MyProfile.zip")
	if err := bundle.CreateBundle(manifestPath, []*modules.Module{mod}, zipPath, "test-1.0"); err != nil {
		t.Fatalf("CreateBundle: %v", err)
	}
	return zipPath, restoreRoot, wantContent
}

// ---------------------------------------------------------------------------
// 1. Capture → rebuild round-trip (the key regression lock)
// ---------------------------------------------------------------------------

func TestRunRebuild_CaptureRoundTrip_RestoresCapturedContent(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir()) // keep restore backups/journal + generations hermetic
	zipPath, restoreRoot, wantContent := seedRoundTripBundle(t)

	md := &mockDriver{installed: map[string]bool{}}
	var res *RebuildResult
	withMockDriver(md, func() {
		r, e := RunRebuild(RebuildFlags{From: zipPath, Confirm: true})
		if e != nil {
			t.Fatalf("RunRebuild returned error: %v", e)
		}
		var ok bool
		res, ok = r.(*RebuildResult)
		if !ok {
			t.Fatalf("expected *RebuildResult, got %T", r)
		}
	})

	// The restored file content equals the captured content — resolved from
	// configs/<module>/ inside the extracted bundle with zero apply-side rewriting.
	restored := filepath.Join(restoreRoot, "cfg", "settings.json")
	got, err := os.ReadFile(restored)
	if err != nil {
		t.Fatalf("restored file missing at %s: %v", restored, err)
	}
	if string(got) != wantContent {
		t.Errorf("restored content = %q, want %q", string(got), wantContent)
	}

	// Bundle info + restore state.
	if res.Bundle == nil || !res.Bundle.Extracted {
		t.Errorf("expected bundle info with Extracted=true, got %+v", res.Bundle)
	}
	if res.Restore != "enabled" {
		t.Errorf("restore = %q, want enabled", res.Restore)
	}

	// apply + verify summaries present.
	ar, ok := res.Apply.(*ApplyResult)
	if !ok || ar == nil {
		t.Fatalf("expected *ApplyResult in Apply, got %T", res.Apply)
	}
	if ar.Summary.Total == 0 {
		t.Errorf("apply summary total = 0, want > 0")
	}
	vr, ok := res.Verify.(*VerifyResult)
	if !ok || vr == nil {
		t.Fatalf("expected *VerifyResult in Verify, got %T", res.Verify)
	}
	if vr.Summary.Total == 0 {
		t.Errorf("verify summary total = 0, want > 0")
	}
}

// ---------------------------------------------------------------------------
// 2. Bare .jsonc → installs, no bundle extraction
// ---------------------------------------------------------------------------

func TestRunRebuild_BareManifest_InstallsWithoutExtraction(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	manifestPath := bareManifest(t)

	md := &mockDriver{installed: map[string]bool{}}
	var res *RebuildResult
	withMockDriver(md, func() {
		r, e := RunRebuild(RebuildFlags{From: manifestPath, Confirm: true})
		if e != nil {
			t.Fatalf("RunRebuild returned error: %v", e)
		}
		res = r.(*RebuildResult)
	})

	if res.Bundle != nil {
		t.Errorf("expected Bundle=nil for a bare manifest, got %+v", res.Bundle)
	}
	if md.installCalls == 0 {
		t.Errorf("expected the app to be installed via the driver, got 0 install calls")
	}
	ar := res.Apply.(*ApplyResult)
	if ar.Summary.Success != 1 {
		t.Errorf("apply success = %d, want 1", ar.Summary.Success)
	}
}

// ---------------------------------------------------------------------------
// 3. No --confirm (live run) → CONFIRMATION_REQUIRED, zero installs
// ---------------------------------------------------------------------------

func TestRunRebuild_NoConfirm_RefusesBeforeMutation(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	zipPath, _, _ := seedRoundTripBundle(t)

	// Steer ExtractBundle's temp parent into a controlled dir so we can prove
	// the refusal happened before extraction (spec: "no bundle SHALL be
	// extracted").
	controlled := t.TempDir()
	t.Setenv("TMPDIR", controlled)
	t.Setenv("TEMP", controlled)
	t.Setenv("TMP", controlled)

	md := &mockDriver{installed: map[string]bool{}}
	withMockDriver(md, func() {
		_, e := RunRebuild(RebuildFlags{From: zipPath})
		if e == nil {
			t.Fatal("expected CONFIRMATION_REQUIRED, got nil error")
		}
		if e.Code != envelope.ErrConfirmationRequired {
			t.Errorf("error code = %q, want CONFIRMATION_REQUIRED", e.Code)
		}
	})
	if md.installCalls != 0 {
		t.Errorf("expected zero installs on refusal, got %d", md.installCalls)
	}
	if extracted, _ := filepath.Glob(filepath.Join(controlled, "endstate-apply-*")); len(extracted) != 0 {
		t.Errorf("refusal must precede extraction, found extraction dir(s): %v", extracted)
	}
}

// ---------------------------------------------------------------------------
// 4. --dry-run without confirm → succeeds, Verify nil, no installs
// ---------------------------------------------------------------------------

func TestRunRebuild_DryRun_PreviewsWithoutVerifyOrInstall(t *testing.T) {
	manifestPath := bareManifest(t)

	md := &mockDriver{installed: map[string]bool{}}
	var res *RebuildResult
	withMockDriver(md, func() {
		r, e := RunRebuild(RebuildFlags{From: manifestPath, DryRun: true})
		if e != nil {
			t.Fatalf("RunRebuild dry-run returned error: %v", e)
		}
		res = r.(*RebuildResult)
	})

	if !res.DryRun {
		t.Error("expected dryRun=true in result")
	}
	if res.Verify != nil {
		t.Errorf("expected Verify=nil on dry-run, got %v", res.Verify)
	}
	if md.installCalls != 0 {
		t.Errorf("expected zero installs on dry-run, got %d", md.installCalls)
	}
}

func TestRebuildApplyFlags_PropagatesBackendBootstrapConsent(t *testing.T) {
	got := rebuildApplyFlags(RebuildFlags{
		DryRun:            true,
		NoRestore:         true,
		Events:            "jsonl",
		BootstrapBackends: true,
		NoBootstrap:       true,
	}, "resolved-manifest.jsonc")

	if !got.BootstrapBackends || !got.NoBootstrap {
		t.Fatalf("bootstrap flags = (%v, %v), want both propagated", got.BootstrapBackends, got.NoBootstrap)
	}
	if got.Manifest != "resolved-manifest.jsonc" || !got.DryRun || got.EnableRestore || got.Events != "jsonl" {
		t.Fatalf("other rebuild apply flags changed: %+v", got)
	}
}

// ---------------------------------------------------------------------------
// 5. --no-restore without confirm → succeeds, targets untouched, restore disabled
// ---------------------------------------------------------------------------

func TestRunRebuild_NoRestore_SkipsRestore(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	zipPath, restoreRoot, _ := seedRoundTripBundle(t)

	md := &mockDriver{installed: map[string]bool{}}
	var res *RebuildResult
	withMockDriver(md, func() {
		r, e := RunRebuild(RebuildFlags{From: zipPath, NoRestore: true})
		if e != nil {
			t.Fatalf("RunRebuild --no-restore returned error: %v", e)
		}
		res = r.(*RebuildResult)
	})

	if res.Restore != "disabled" {
		t.Errorf("restore = %q, want disabled", res.Restore)
	}
	// The restore target must NOT have been written.
	restored := filepath.Join(restoreRoot, "cfg", "settings.json")
	if _, err := os.Stat(restored); !os.IsNotExist(err) {
		t.Errorf("expected restore target absent under --no-restore, stat err = %v", err)
	}
	// Apps are still installed and verified.
	if md.installCalls == 0 {
		t.Error("expected the app to be installed under --no-restore")
	}
	if res.Verify == nil {
		t.Error("expected verify to run under --no-restore")
	}
}

// ---------------------------------------------------------------------------
// 6. Input validation
// ---------------------------------------------------------------------------

func TestRunRebuild_InputValidation(t *testing.T) {
	t.Run("URL is rejected as NOT_SUPPORTED", func(t *testing.T) {
		_, e := RunRebuild(RebuildFlags{From: "https://example.com/MyProfile.zip"})
		if e == nil || e.Code != envelope.ErrNotSupported {
			t.Fatalf("expected NOT_SUPPORTED, got %v", e)
		}
	})

	t.Run("empty --from is a validation error", func(t *testing.T) {
		_, e := RunRebuild(RebuildFlags{From: ""})
		if e == nil || e.Code != envelope.ErrManifestValidationError {
			t.Fatalf("expected MANIFEST_VALIDATION_ERROR, got %v", e)
		}
	})

	t.Run("missing path is MANIFEST_NOT_FOUND", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "does-not-exist.zip")
		_, e := RunRebuild(RebuildFlags{From: missing})
		if e == nil || e.Code != envelope.ErrManifestNotFound {
			t.Fatalf("expected MANIFEST_NOT_FOUND, got %v", e)
		}
	})

	t.Run("zip without manifest.jsonc is MANIFEST_PARSE_ERROR", func(t *testing.T) {
		zipPath := filepath.Join(t.TempDir(), "no-manifest.zip")
		zf, err := os.Create(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		w := zip.NewWriter(zf)
		f, _ := w.Create("metadata.json")
		_, _ = f.Write([]byte("{}"))
		_ = w.Close()
		_ = zf.Close()

		// Confirm=true so validation reaches the extraction step.
		_, e := RunRebuild(RebuildFlags{From: zipPath, Confirm: true})
		if e == nil || e.Code != envelope.ErrManifestParseError {
			t.Fatalf("expected MANIFEST_PARSE_ERROR, got %v", e)
		}
	})
}

// ---------------------------------------------------------------------------
// 7. Temp extraction dir removed after success AND after a mid-pipeline error
// ---------------------------------------------------------------------------

func TestRunRebuild_TempDirCleanedUp(t *testing.T) {
	run := func(t *testing.T, md *mockDriver) {
		t.Setenv("ENDSTATE_ROOT", t.TempDir())
		zipPath, _, _ := seedRoundTripBundle(t)

		// Steer ExtractBundle's temp parent into a controlled dir we can inspect.
		controlled := t.TempDir()
		t.Setenv("TMPDIR", controlled)
		t.Setenv("TEMP", controlled)
		t.Setenv("TMP", controlled)

		withMockDriver(md, func() {
			_, _ = RunRebuild(RebuildFlags{From: zipPath, Confirm: true})
		})

		leftovers, _ := filepath.Glob(filepath.Join(controlled, "endstate-apply-*"))
		if len(leftovers) != 0 {
			t.Errorf("temp extraction dir(s) not cleaned up: %v", leftovers)
		}
	}

	t.Run("after success", func(t *testing.T) {
		run(t, &mockDriver{installed: map[string]bool{}})
	})

	t.Run("after mid-pipeline install error", func(t *testing.T) {
		run(t, &mockDriver{installed: map[string]bool{}, installErr: errInstallBoom})
	})

	// The install-error lane above returns through apply's normal path (an
	// install failure is a failed action, not an envelope error). This lane
	// forces the envelope-error return (rebuild's `return nil, applyErr`):
	// extraction succeeds, then the extracted manifest fails to parse.
	t.Run("after post-extraction manifest parse error", func(t *testing.T) {
		controlled := t.TempDir()
		t.Setenv("TMPDIR", controlled)
		t.Setenv("TEMP", controlled)
		t.Setenv("TMP", controlled)

		zipPath := filepath.Join(t.TempDir(), "bad-manifest.zip")
		zf, err := os.Create(zipPath)
		if err != nil {
			t.Fatal(err)
		}
		w := zip.NewWriter(zf)
		f, _ := w.Create("manifest.jsonc")
		_, _ = f.Write([]byte("{not valid json"))
		_ = w.Close()
		_ = zf.Close()

		_, e := RunRebuild(RebuildFlags{From: zipPath, Confirm: true})
		if e == nil || e.Code != envelope.ErrManifestParseError {
			t.Fatalf("expected MANIFEST_PARSE_ERROR from the extracted manifest, got %v", e)
		}
		if leftovers, _ := filepath.Glob(filepath.Join(controlled, "endstate-apply-*")); len(leftovers) != 0 {
			t.Errorf("temp extraction dir(s) not cleaned up on error return: %v", leftovers)
		}
	})
}

// errInstallBoom is a scripted install failure for the cleanup-on-error lane.
var errInstallBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "winget exploded" }

// ---------------------------------------------------------------------------
// 8. Events: first event is phase, last is summary
// ---------------------------------------------------------------------------

func TestRunRebuild_Events_PhaseFirst_SummaryLast(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	manifestPath := bareManifest(t)

	md := &mockDriver{installed: map[string]bool{}}
	stderr := captureStderr(t, func() {
		withMockDriver(md, func() {
			_, e := RunRebuild(RebuildFlags{From: manifestPath, Confirm: true, Events: "jsonl"})
			if e != nil {
				t.Fatalf("RunRebuild returned error: %v", e)
			}
		})
	})

	var evts []map[string]interface{}
	dec := json.NewDecoder(bytes.NewReader([]byte(stderr)))
	for dec.More() {
		var obj map[string]interface{}
		if err := dec.Decode(&obj); err != nil {
			break
		}
		evts = append(evts, obj)
	}

	if len(evts) == 0 {
		t.Fatal("expected streamed events, got none")
	}
	if evts[0]["event"] != "phase" {
		t.Errorf("first event = %v, want phase", evts[0]["event"])
	}
	if last := evts[len(evts)-1]; last["event"] != "summary" {
		t.Errorf("last event = %v, want summary", last["event"])
	}
}

// ---------------------------------------------------------------------------
// 9. Capabilities advertises rebuild with --from
// ---------------------------------------------------------------------------

func TestRunCapabilities_RebuildFlags_IncludesFrom(t *testing.T) {
	result, err := RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities returned error: %v", err)
	}
	data := result.(CapabilitiesData)
	rebuildCmd, ok := data.Commands["rebuild"]
	if !ok {
		t.Fatal("rebuild command not found in capabilities")
	}
	if !rebuildCmd.Supported {
		t.Error("expected commands.rebuild.supported = true")
	}
	found := false
	for _, f := range rebuildCmd.Flags {
		if f == "--from" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("commands.rebuild.flags does not contain --from; got %v", rebuildCmd.Flags)
	}
}

// ---------------------------------------------------------------------------
// 10. Verify failures are data: success envelope, verify fail > 0
// ---------------------------------------------------------------------------

func TestRunRebuild_VerifyFailure_IsData(t *testing.T) {
	t.Setenv("ENDSTATE_ROOT", t.TempDir())
	manifestPath := bareManifest(t)

	// ghostDriver installs "successfully" but the app stays undetected.
	g := &ghostDriver{}
	var res *RebuildResult
	withDriverFn(g, func() {
		r, e := RunRebuild(RebuildFlags{From: manifestPath, Confirm: true})
		if e != nil {
			t.Fatalf("expected a success envelope even with verify drift, got error: %v", e)
		}
		res = r.(*RebuildResult)
	})

	if g.installCalls == 0 {
		t.Error("expected the driver install to be attempted")
	}
	vr, ok := res.Verify.(*VerifyResult)
	if !ok || vr == nil {
		t.Fatalf("expected *VerifyResult, got %T", res.Verify)
	}
	if vr.Summary.Fail == 0 {
		t.Errorf("expected verify summary fail > 0 (drifted app), got %d", vr.Summary.Fail)
	}
}
