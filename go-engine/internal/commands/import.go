// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/importer"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// defaultImportOut is the default output path for a UniGetUI import, relative to
// the repo root. manifests/local/ is gitignored per repo conventions.
const defaultImportOutRel = "manifests/local/imported-unigetui.jsonc"

// ImportFlags holds the parsed CLI flags for the import command.
type ImportFlags struct {
	// From is the source format keyword. Only "unigetui" is supported in v0.
	From string
	// Path is the input file to import (a UniGetUI .ubundle).
	Path string
	// Out is the output manifest path. Empty defaults to
	// manifests/local/imported-unigetui.jsonc (gitignored).
	Out string
	// Pin records versions on imported apps (InstallationOptions.Version pin
	// beats the observed Version). Off by default.
	Pin bool
}

// ImportCounts summarises the outcome by category.
type ImportCounts struct {
	Imported     int `json:"imported"`
	Skipped      int `json:"skipped"`
	Incompatible int `json:"incompatible"`
}

// ImportData is the data payload for the import command JSON envelope. imported,
// skipped and incompatible together account for every package in the bundle —
// skip transparency is a first-class output.
type ImportData struct {
	Source        string                       `json:"source"`
	Input         string                       `json:"input"`
	Output        string                       `json:"output"`
	ExportVersion float64                      `json:"exportVersion"`
	Pinned        bool                         `json:"pinned"`
	Counts        ImportCounts                 `json:"counts"`
	Imported      []importer.ImportedApp       `json:"imported"`
	Skipped       []importer.SkippedPackage    `json:"skipped"`
	Incompatible  []importer.IncompatibleEntry `json:"incompatible"`
	Warnings      []string                     `json:"warnings,omitempty"`
}

// RunImport reads a UniGetUI bundle, maps its winget-source packages onto a
// manifest, gates the emitted JSONC through the manifest loader, then writes it.
// Import is pure: no network access, no package operations, deterministic output.
func RunImport(flags ImportFlags) (interface{}, *envelope.Error) {
	// --- 1. Validate --from (source format) ---
	from := strings.ToLower(strings.TrimSpace(flags.From))
	if from == "" {
		return nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			"--from is required").
			WithRemediation("Specify the source format, e.g. --from unigetui.")
	}
	if from != "unigetui" {
		return nil, envelope.NewError(
			envelope.ErrNotSupported,
			fmt.Sprintf("import source %q is not supported", flags.From)).
			WithDetail(map[string]string{"from": flags.From}).
			WithRemediation("The only supported source is 'unigetui'. Re-run with --from unigetui.")
	}

	// --- 2. Validate --path (input file) ---
	path := strings.TrimSpace(flags.Path)
	if path == "" {
		return nil, envelope.NewError(
			envelope.ErrManifestValidationError,
			"--path is required").
			WithRemediation("Provide the UniGetUI bundle to import, e.g. --path backup.ubundle.")
	}
	if _, statErr := os.Stat(path); errors.Is(statErr, os.ErrNotExist) {
		return nil, envelope.NewError(
			envelope.ErrManifestNotFound,
			"The specified bundle file does not exist.").
			WithDetail(map[string]string{"path": path}).
			WithRemediation("Check the file path and ensure the UniGetUI bundle exists.")
	}

	// Resolve the input to an absolute path so both the envelope's input and the
	// out-of-repo default output are reported as absolute, resolved paths.
	absInputPath, inAbsErr := filepath.Abs(path)
	if inAbsErr != nil {
		absInputPath = path
	}

	// --- 3. Parse the bundle (pure; no network) ---
	f, openErr := os.Open(path)
	if openErr != nil {
		return nil, envelope.NewError(
			envelope.ErrManifestParseError,
			"Failed to open the UniGetUI bundle.").
			WithDetail(map[string]string{"path": path, "error": openErr.Error()})
	}
	defer f.Close()

	bundle, warnings, parseErr := importer.ParseUniGetUI(f)
	if parseErr != nil {
		// A well-formed JSON file that simply is not a UniGetUI bundle is a
		// validation problem (wrong --path), not a JSON parse failure.
		if errors.Is(parseErr, importer.ErrNotUniGetUIBundle) {
			return nil, envelope.NewError(
				envelope.ErrManifestValidationError,
				"The file does not look like a UniGetUI bundle.").
				WithDetail(map[string]string{"path": path, "error": parseErr.Error()}).
				WithRemediation("Point --path at a UniGetUI .ubundle export (JSON with export_version and packages).")
		}
		return nil, envelope.NewError(
			envelope.ErrManifestParseError,
			"Failed to parse the UniGetUI bundle.").
			WithDetail(map[string]string{"path": path, "error": parseErr.Error()}).
			WithRemediation("Ensure --path points to a valid UniGetUI .ubundle (JSON).")
	}

	// --- 4. Map winget-source packages onto app entries ---
	mapRes := importer.MapBundle(bundle, importer.MapOptions{Pin: flags.Pin})

	// --- 5. Resolve output path and build the JSONC manifest ---
	out := strings.TrimSpace(flags.Out)
	if out == "" {
		out = defaultImportOut(absInputPath)
	}
	outAbs, absErr := filepath.Abs(out)
	if absErr != nil {
		outAbs = out
	}

	cliVersion := config.ReadVersion(resolveRepoRootFn())
	jsoncBytes := buildImportManifest(path, bundle.ExportVersion, cliVersion, flags.Pin, mapRes)

	// --- 6. Round-trip validity gate: the emitted bytes MUST load through the
	// manifest loader BEFORE anything is written to the output path. ---
	if gateErr := importRoundTripGateFn(jsoncBytes); gateErr != nil {
		return nil, gateErr
	}

	// --- 7. Write the manifest ---
	if writeErr := writeImportManifest(outAbs, jsoncBytes); writeErr != nil {
		return nil, envelope.NewError(
			envelope.ErrManifestWriteFailed,
			"Failed to write the imported manifest.").
			WithDetail(map[string]string{"path": outAbs, "error": writeErr.Error()}).
			WithRemediation("Ensure the output directory is writable.")
	}

	// --- 8. Assemble the result. Collisions surface as warnings alongside the
	// parser's version warning — every non-imported package is already in
	// skipped/incompatible. ---
	allWarnings := append([]string{}, warnings...)
	allWarnings = append(allWarnings, mapRes.Collisions...)

	return &ImportData{
		Source:        "unigetui",
		Input:         absInputPath,
		Output:        outAbs,
		ExportVersion: bundle.ExportVersion,
		Pinned:        flags.Pin,
		Counts: ImportCounts{
			Imported:     len(mapRes.Imported),
			Skipped:      len(mapRes.Skipped),
			Incompatible: len(mapRes.Incompatible),
		},
		Imported:     mapRes.Imported,
		Skipped:      mapRes.Skipped,
		Incompatible: mapRes.Incompatible,
		Warnings:     allWarnings,
	}, nil
}

// defaultImportOut resolves the output path when --out is omitted. Inside a repo
// checkout it lands at manifests/local/imported-unigetui.jsonc (gitignored).
// Outside a repo (no root marker resolved) there is no manifests/local to target,
// so it lands next to the input bundle instead of polluting the working directory.
func defaultImportOut(absInputPath string) string {
	root := resolveRepoRootFn()
	if root == "" {
		return filepath.Join(filepath.Dir(absInputPath), "imported-unigetui.jsonc")
	}
	return filepath.Join(root, filepath.FromSlash(defaultImportOutRel))
}

// buildImportManifest renders the JSONC manifest bytes deterministically: an
// ordered manifest struct marshalled with indentation, prefixed by a generated
// header comment naming the source file and tool version. No timestamp is
// emitted so identical input yields byte-identical output.
func buildImportManifest(sourcePath string, exportVersion float64, cliVersion string, pinned bool, res *importer.MapResult) []byte {
	type outApp struct {
		ID          string            `json:"id"`
		Refs        map[string]string `json:"refs"`
		DisplayName string            `json:"displayName,omitempty"`
		Version     string            `json:"version,omitempty"`
	}
	type outManifest struct {
		Version int      `json:"version"`
		Name    string   `json:"name"`
		Apps    []outApp `json:"apps"`
	}

	apps := make([]outApp, 0, len(res.Imported))
	for _, a := range res.Imported {
		apps = append(apps, outApp{
			ID:          a.ID,
			Refs:        map[string]string{"windows": a.Ref},
			DisplayName: a.DisplayName,
			Version:     a.Version,
		})
	}

	body, _ := json.MarshalIndent(outManifest{
		Version: 1,
		Name:    "imported-unigetui",
		Apps:    apps,
	}, "", "  ")

	pinNote := ""
	if pinned {
		pinNote = " (--pin: versions recorded)"
	}

	var b strings.Builder
	b.WriteString("// Generated by \"endstate import --from unigetui\".\n")
	b.WriteString(fmt.Sprintf("// Source: %s (UniGetUI export_version %s)\n", sourcePath, importer.FormatExportVersion(exportVersion)))
	b.WriteString(fmt.Sprintf("// Tool: endstate %s%s\n", cliVersion, pinNote))
	b.WriteString("// Scope: package list only. UniGetUI's own settings are NOT imported;\n")
	b.WriteString("//   Endstate's module catalog restores app config on plan/apply.\n")
	b.Write(body)
	b.WriteString("\n")
	return []byte(b.String())
}

// importRoundTripGateFn gates the emitted bytes through the manifest loader
// before anything is written. It is a package-level var so tests can force the
// abort branch and assert that no output file is produced.
var importRoundTripGateFn = importRoundTripGate

// importRoundTripGate writes the emitted bytes to a temp file and loads them
// through the manifest loader. A load failure aborts the import (nothing is
// written to the output path) with a validation error.
func importRoundTripGate(jsoncBytes []byte) *envelope.Error {
	tmp, err := os.CreateTemp("", "endstate-import-*.jsonc")
	if err != nil {
		return envelope.NewError(
			envelope.ErrInternalError,
			"Failed to create a temporary file for the manifest validity check.").
			WithDetail(map[string]string{"error": err.Error()})
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if _, err := tmp.Write(jsoncBytes); err != nil {
		tmp.Close()
		return envelope.NewError(
			envelope.ErrInternalError,
			"Failed to stage the manifest for the validity check.").
			WithDetail(map[string]string{"error": err.Error()})
	}
	if err := tmp.Close(); err != nil {
		return envelope.NewError(
			envelope.ErrInternalError,
			"Failed to stage the manifest for the validity check.").
			WithDetail(map[string]string{"error": err.Error()})
	}

	if _, err := manifest.LoadManifest(tmpPath); err != nil {
		return envelope.NewError(
			envelope.ErrManifestValidationError,
			"The generated manifest failed to load and was not written.").
			WithDetail(map[string]string{"error": err.Error()}).
			WithRemediation("This is unexpected — please report the source bundle so the mapping can be fixed.")
	}
	return nil
}

// writeImportManifest creates the output directory and writes the manifest bytes
// atomically: the bytes go to a sibling temp file first, then os.Rename swaps it
// into place (mirroring the state package's temp+rename pattern). On Windows
// os.Rename replaces an existing target, so an existing file at the output path
// is overwritten in a single step rather than truncated in place.
func writeImportManifest(path string, jsoncBytes []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, jsoncBytes, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath) // best-effort cleanup of the staged temp file
		return err
	}
	return nil
}
