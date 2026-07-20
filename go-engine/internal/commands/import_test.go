// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

const sampleBundlePath = "testdata/unigetui-sample.ubundle"

// runImportOK runs RunImport against the sample fixture, fails the test on an
// envelope error, and returns the typed data payload.
func runImportOK(t *testing.T, flags ImportFlags) *ImportData {
	t.Helper()
	res, err := RunImport(flags)
	if err != nil {
		t.Fatalf("RunImport returned error: %+v", err)
	}
	data, ok := res.(*ImportData)
	if !ok {
		t.Fatalf("expected *ImportData, got %T", res)
	}
	return data
}

// End-to-end: the sample fixture imports to a manifest that loads through the
// engine's manifest loader, and its apps match the winget-source packages.
func TestRunImport_FixtureRoundTrips(t *testing.T) {
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	data := runImportOK(t, ImportFlags{From: "unigetui", Path: sampleBundlePath, Out: out})

	// The fixture has 3 winget packages (VS Code, Git, Firefox), 3 non-winget
	// (choco, scoop, pip), and 1 incompatible.
	if data.Counts.Imported != 3 {
		t.Errorf("imported = %d, want 3", data.Counts.Imported)
	}
	if data.Counts.Skipped != 3 {
		t.Errorf("skipped = %d, want 3", data.Counts.Skipped)
	}
	if data.Counts.Incompatible != 1 {
		t.Errorf("incompatible = %d, want 1", data.Counts.Incompatible)
	}
	if data.ExportVersion != 3 {
		t.Errorf("exportVersion = %v, want 3", data.ExportVersion)
	}

	// The written file loads through the manifest loader (the same gate the
	// command applies before writing).
	mf, loadErr := manifest.LoadManifest(out)
	if loadErr != nil {
		t.Fatalf("emitted manifest failed to load: %v", loadErr)
	}
	if len(mf.Apps) != 3 {
		t.Fatalf("loaded manifest apps = %d, want 3", len(mf.Apps))
	}

	byRef := map[string]manifest.App{}
	for _, a := range mf.Apps {
		byRef[a.Refs["windows"]] = a
	}
	vscode, ok := byRef["Microsoft.VisualStudioCode"]
	if !ok {
		t.Fatalf("expected an app with refs.windows=Microsoft.VisualStudioCode, apps=%+v", mf.Apps)
	}
	if vscode.ID != "visualstudiocode" || vscode.DisplayName != "Microsoft Visual Studio Code" {
		t.Errorf("vscode app = %+v", vscode)
	}
	// No --pin: no app carries a version.
	for _, a := range mf.Apps {
		if a.Version != "" {
			t.Errorf("app %q has version %q without --pin", a.ID, a.Version)
		}
	}
}

// --pin records versions; the InstallationOptions.Version pin wins over the
// observed Version (VS Code: observed 1.85.1, pinned 1.85.0).
func TestRunImport_PinRecordsVersions(t *testing.T) {
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	runImportOK(t, ImportFlags{From: "unigetui", Path: sampleBundlePath, Out: out, Pin: true})

	mf, loadErr := manifest.LoadManifest(out)
	if loadErr != nil {
		t.Fatalf("emitted manifest failed to load: %v", loadErr)
	}
	versions := map[string]string{}
	for _, a := range mf.Apps {
		versions[a.Refs["windows"]] = a.Version
	}
	if versions["Microsoft.VisualStudioCode"] != "1.85.0" {
		t.Errorf("VS Code version = %q, want 1.85.0 (pin beats observed)", versions["Microsoft.VisualStudioCode"])
	}
	if versions["Git.Git"] != "2.43.0" {
		t.Errorf("Git version = %q, want 2.43.0 (observed)", versions["Git.Git"])
	}
	// Firefox is version-less in the fixture: even with --pin it has no version.
	if versions["Mozilla.Firefox"] != "" {
		t.Errorf("Firefox version = %q, want empty (version-less package)", versions["Mozilla.Firefox"])
	}
}

// Skip transparency: every non-winget manager is named and the incompatible
// entry passes through.
func TestRunImport_SkipTransparency(t *testing.T) {
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	data := runImportOK(t, ImportFlags{From: "unigetui", Path: sampleBundlePath, Out: out})

	managers := map[string]bool{}
	for _, s := range data.Skipped {
		managers[s.Manager] = true
		if s.Reason == "" {
			t.Errorf("skipped %q missing a reason", s.ID)
		}
	}
	for _, want := range []string{"Chocolatey", "Scoop", "Pip"} {
		if !managers[want] {
			t.Errorf("expected manager %q in skipped list, got %v", want, managers)
		}
	}
	if len(data.Incompatible) != 1 || data.Incompatible[0].ID != "Contoso.LocalOnlyApp" {
		t.Errorf("incompatible passthrough failed: %+v", data.Incompatible)
	}
}

// Import is deterministic: the emitted file bytes are identical across two runs.
func TestRunImport_DeterministicOutput(t *testing.T) {
	out1 := filepath.Join(t.TempDir(), "a.jsonc")
	out2 := filepath.Join(t.TempDir(), "b.jsonc")
	runImportOK(t, ImportFlags{From: "unigetui", Path: sampleBundlePath, Out: out1, Pin: true})
	runImportOK(t, ImportFlags{From: "unigetui", Path: sampleBundlePath, Out: out2, Pin: true})

	b1, _ := os.ReadFile(out1)
	b2, _ := os.ReadFile(out2)
	if string(b1) != string(b2) {
		t.Errorf("import output is not byte-identical across runs:\n--- run1 ---\n%s\n--- run2 ---\n%s", b1, b2)
	}
}

// The default output path resolves under the repo root (ENDSTATE_ROOT here) at
// manifests/local/imported-unigetui.jsonc.
func TestRunImport_DefaultOutputPath(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ENDSTATE_ROOT", root)

	data := runImportOK(t, ImportFlags{From: "unigetui", Path: sampleBundlePath})

	want := filepath.Join(root, "manifests", "local", "imported-unigetui.jsonc")
	if data.Output != want {
		t.Errorf("output = %q, want %q", data.Output, want)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected the manifest at the default path, stat err = %v", err)
	}
}

// Outside a repo checkout (repo-root resolution empty), the default output lands
// next to the input bundle rather than in a cwd-relative manifests/local path.
func TestRunImport_DefaultOutputOutsideRepo(t *testing.T) {
	orig := resolveRepoRootFn
	resolveRepoRootFn = func() string { return "" }
	defer func() { resolveRepoRootFn = orig }()

	// Copy the sample bundle into a temp dir so the default output lands beside it
	// (and never pollutes the testdata directory).
	dir := t.TempDir()
	in := filepath.Join(dir, "backup.ubundle")
	srcBytes, err := os.ReadFile(sampleBundlePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(in, srcBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	data := runImportOK(t, ImportFlags{From: "unigetui", Path: in})

	want := filepath.Join(dir, "imported-unigetui.jsonc")
	if data.Output != want {
		t.Errorf("output = %q, want %q (beside the input bundle when outside a repo)", data.Output, want)
	}
	if _, statErr := os.Stat(want); statErr != nil {
		t.Errorf("expected the manifest beside the input bundle, stat err = %v", statErr)
	}
}

// The reported input path is absolute even when --path is relative, matching the
// absolute output path.
func TestRunImport_InputAbsolutized(t *testing.T) {
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	data := runImportOK(t, ImportFlags{From: "unigetui", Path: sampleBundlePath, Out: out})
	if !filepath.IsAbs(data.Input) {
		t.Errorf("input = %q, want an absolute path", data.Input)
	}
	wantAbs, _ := filepath.Abs(sampleBundlePath)
	if data.Input != wantAbs {
		t.Errorf("input = %q, want %q", data.Input, wantAbs)
	}
}

// A well-formed JSON file that is not a UniGetUI bundle (e.g. a package.json) is
// a validation error pointing back at --path; nothing is written.
func TestRunImport_WrongFileShapeValidation(t *testing.T) {
	notBundle := filepath.Join(t.TempDir(), "package.json")
	if err := os.WriteFile(notBundle, []byte(`{"name": "x", "version": "1.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	_, err := RunImport(ImportFlags{From: "unigetui", Path: notBundle, Out: out})
	if err == nil || err.Code != envelope.ErrManifestValidationError {
		t.Fatalf("expected MANIFEST_VALIDATION_ERROR for a non-bundle JSON file, got %+v", err)
	}
	if err.Remediation == "" || !strings.Contains(err.Remediation, "--path") {
		t.Errorf("remediation should mention --path, got %q", err.Remediation)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("nothing should be written for a non-bundle file, stat err = %v", statErr)
	}
}

// An empty but versioned bundle imports as empty (zero apps, no error).
func TestRunImport_EmptyVersionedBundle(t *testing.T) {
	in := filepath.Join(t.TempDir(), "empty.ubundle")
	if err := os.WriteFile(in, []byte(`{"export_version": 3, "packages": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	data := runImportOK(t, ImportFlags{From: "unigetui", Path: in, Out: out})
	if data.Counts.Imported != 0 || data.Counts.Skipped != 0 || data.Counts.Incompatible != 0 {
		t.Errorf("empty bundle counts = %+v, want all zero", data.Counts)
	}
}

// The round-trip gate rejects bytes that fail to load as a manifest (white-box).
func TestImportRoundTripGate_RejectsInvalid(t *testing.T) {
	gateErr := importRoundTripGate([]byte("{ this is not a manifest"))
	if gateErr == nil {
		t.Fatal("expected the round-trip gate to reject invalid manifest bytes")
	}
	if gateErr.Code != envelope.ErrManifestValidationError {
		t.Errorf("gate error code = %q, want MANIFEST_VALIDATION_ERROR", gateErr.Code)
	}
}

// When the gate aborts, RunImport returns the gate error and writes no output.
func TestRunImport_GateFailureWritesNothing(t *testing.T) {
	orig := importRoundTripGateFn
	importRoundTripGateFn = func(_ []byte) *envelope.Error {
		return envelope.NewError(envelope.ErrManifestValidationError, "forced gate failure")
	}
	defer func() { importRoundTripGateFn = orig }()

	out := filepath.Join(t.TempDir(), "imported.jsonc")
	_, err := RunImport(ImportFlags{From: "unigetui", Path: sampleBundlePath, Out: out})
	if err == nil || err.Code != envelope.ErrManifestValidationError {
		t.Fatalf("expected the gate's MANIFEST_VALIDATION_ERROR, got %+v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no manifest should be written when the gate fails, stat err = %v", statErr)
	}
}

// An existing file at the output path is replaced (atomic overwrite).
func TestRunImport_OverwritesExistingOutput(t *testing.T) {
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	if err := os.WriteFile(out, []byte("STALE CONTENT"), 0o644); err != nil {
		t.Fatal(err)
	}
	runImportOK(t, ImportFlags{From: "unigetui", Path: sampleBundlePath, Out: out})

	b, _ := os.ReadFile(out)
	if string(b) == "STALE CONTENT" {
		t.Error("expected the existing output file to be replaced, but it was unchanged")
	}
	if _, err := manifest.LoadManifest(out); err != nil {
		t.Errorf("the replaced file must load as a manifest, got: %v", err)
	}
	// No temp file lingers.
	if _, statErr := os.Stat(out + ".tmp"); !os.IsNotExist(statErr) {
		t.Errorf("expected the .tmp staging file to be removed, stat err = %v", statErr)
	}
}

// The emitted manifest carries a JSONC header comment yet still loads (the
// header must round-trip through StripJsoncComments).
func TestRunImport_EmitsJsoncHeader(t *testing.T) {
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	runImportOK(t, ImportFlags{From: "unigetui", Path: sampleBundlePath, Out: out})

	b, _ := os.ReadFile(out)
	if len(b) == 0 || b[0] != '/' {
		t.Fatalf("expected the file to start with a // header comment, got: %.40q", string(b))
	}
	if _, err := manifest.LoadManifest(out); err != nil {
		t.Errorf("a header-commented manifest must load via the JSONC loader, got: %v", err)
	}
}

// An unknown --from source is NOT_SUPPORTED with remediation.
func TestRunImport_UnknownSourceNotSupported(t *testing.T) {
	_, err := RunImport(ImportFlags{From: "dsc", Path: sampleBundlePath})
	if err == nil || err.Code != envelope.ErrNotSupported {
		t.Fatalf("expected NOT_SUPPORTED, got %+v", err)
	}
	if err.Remediation == "" {
		t.Error("NOT_SUPPORTED should carry remediation")
	}
}

// An empty --from is a validation error.
func TestRunImport_EmptyFromValidation(t *testing.T) {
	_, err := RunImport(ImportFlags{From: "", Path: sampleBundlePath})
	if err == nil || err.Code != envelope.ErrManifestValidationError {
		t.Fatalf("expected MANIFEST_VALIDATION_ERROR, got %+v", err)
	}
}

// An empty --path is a validation error.
func TestRunImport_EmptyPathValidation(t *testing.T) {
	_, err := RunImport(ImportFlags{From: "unigetui", Path: ""})
	if err == nil || err.Code != envelope.ErrManifestValidationError {
		t.Fatalf("expected MANIFEST_VALIDATION_ERROR, got %+v", err)
	}
}

// A missing bundle path is MANIFEST_NOT_FOUND.
func TestRunImport_MissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope.ubundle")
	_, err := RunImport(ImportFlags{From: "unigetui", Path: missing})
	if err == nil || err.Code != envelope.ErrManifestNotFound {
		t.Fatalf("expected MANIFEST_NOT_FOUND, got %+v", err)
	}
}

// Malformed bundle JSON is MANIFEST_PARSE_ERROR; nothing is written.
func TestRunImport_MalformedBundle(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.ubundle")
	if err := os.WriteFile(bad, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	_, err := RunImport(ImportFlags{From: "unigetui", Path: bad, Out: out})
	if err == nil || err.Code != envelope.ErrManifestParseError {
		t.Fatalf("expected MANIFEST_PARSE_ERROR, got %+v", err)
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Errorf("no manifest should be written on a parse error, stat err = %v", statErr)
	}
}

// A future export_version imports with a warning surfaced in the result.
func TestRunImport_FutureVersionWarns(t *testing.T) {
	src := `{"export_version": 4, "packages": [{"Id": "Git.Git", "Name": "Git", "Source": "winget"}]}`
	in := filepath.Join(t.TempDir(), "v4.ubundle")
	if err := os.WriteFile(in, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "imported.jsonc")
	data := runImportOK(t, ImportFlags{From: "unigetui", Path: in, Out: out})
	if len(data.Warnings) == 0 {
		t.Error("expected a version warning in the result")
	}
	if data.Counts.Imported != 1 {
		t.Errorf("imported = %d, want 1 under a future version", data.Counts.Imported)
	}
}

// Capabilities advertises the import command with --from and --path.
func TestRunCapabilities_ImportFlags(t *testing.T) {
	result, err := RunCapabilities()
	if err != nil {
		t.Fatalf("RunCapabilities returned error: %v", err)
	}
	data := result.(CapabilitiesData)
	importCmd, ok := data.Commands["import"]
	if !ok {
		t.Fatal("import command not found in capabilities")
	}
	if !importCmd.Supported {
		t.Error("expected commands.import.supported = true")
	}
	for _, want := range []string{"--from", "--path", "--out", "--pin", "--json"} {
		found := false
		for _, f := range importCmd.Flags {
			if f == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("commands.import.flags missing %q; got %v", want, importCmd.Flags)
		}
	}
}
