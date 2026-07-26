// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationharness

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

func TestInspectLiveCaptureArtifactAcceptsProductionSchemaV1NotepadBundle(t *testing.T) {
	definition, path, snapshots, claims := productionLiveArtifactFixture(t)
	evidence, failure := inspectLiveCaptureArtifact(definition, snapshots, *claims, path)
	if failure != nil {
		t.Fatalf("inspectLiveCaptureArtifact() failure = %+v", failure)
	}
	if evidence.SHA256 == "" || evidence.Size == 0 || evidence.Mode == 0 {
		t.Fatalf("path-free artifact evidence = %+v", evidence)
	}
}

func TestInspectLiveCaptureArtifactRejectsHostileSchemaV1Bundles(t *testing.T) {
	definition, path, snapshots, claims := productionLiveArtifactFixture(t)
	entries := readLiveArtifactEntriesForTest(t, path)
	tests := []struct {
		name   string
		mutate func(map[string][]byte, *liveCaptureArtifactClaims)
	}{
		{"extra member", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) { entries["foreign.json"] = []byte("{}") }},
		{"missing payload", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			delete(entries, "configs/notepad-plus-plus/config.xml")
		}},
		{"case alias", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			entries["Configs/notepad-plus-plus/config.xml"] = entries["configs/notepad-plus-plus/config.xml"]
		}},
		{"schema v2", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["manifest.jsonc"], &value)
			value["version"] = 2
			entries["manifest.jsonc"] = mustJSON(t, value)
		}},
		{"module slug app ID", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["manifest.jsonc"], &value)
			value["apps"].([]any)[0].(map[string]any)["id"] = "notepad-plus-plus"
			entries["manifest.jsonc"] = mustJSON(t, value)
		}},
		{"wrong module", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["manifest.jsonc"], &value)
			value["configModules"] = []string{"apps.other"}
			entries["manifest.jsonc"] = mustJSON(t, value)
		}},
		{"wrong revision claim", func(_ map[string][]byte, claims *liveCaptureArtifactClaims) {
			claims.ModuleRevision = strings.Repeat("0", 64)
		}},
		{"wrong source", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["manifest.jsonc"], &value)
			value["apps"].([]any)[0].(map[string]any)["source"] = "msstore"
			entries["manifest.jsonc"] = mustJSON(t, value)
		}},
		{"wrong ref", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["manifest.jsonc"], &value)
			value["apps"].([]any)[0].(map[string]any)["refs"].(map[string]any)["windows"] = "Other.App"
			entries["manifest.jsonc"] = mustJSON(t, value)
		}},
		{"includes", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["manifest.jsonc"], &value)
			value["includes"] = []string{"foreign.jsonc"}
			entries["manifest.jsonc"] = mustJSON(t, value)
		}},
		{"verify", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["manifest.jsonc"], &value)
			value["verify"].([]any)[0].(map[string]any)["command"] = "foreign"
			entries["manifest.jsonc"] = mustJSON(t, value)
		}},
		{"manual app", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["manifest.jsonc"], &value)
			value["apps"].([]any)[0].(map[string]any)["manual"] = map[string]string{"verifyPath": "foreign"}
			entries["manifest.jsonc"] = mustJSON(t, value)
		}},
		{"app version", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["manifest.jsonc"], &value)
			value["apps"].([]any)[0].(map[string]any)["version"] = "1.0"
			entries["manifest.jsonc"] = mustJSON(t, value)
		}},
		{"wrong capture identity", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value bundle.BundleMetadata
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value.MachineName = "foreign"
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"wrong timestamp", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value bundle.BundleMetadata
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value.CapturedAt = "2020-01-01T00:00:00Z"
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"wrong version", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value bundle.BundleMetadata
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value.EndstateVersion = "foreign"
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"metadata os", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value["os"] = "linux"
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"metadata share", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value["share"] = true
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"metadata name", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value["name"] = "foreign"
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"metadata redaction", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value["redaction"] = map[string]any{}
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"metadata manifest version", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value["manifestVersion"] = 2
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"metadata skipped null", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value["configModulesSkipped"] = nil
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"metadata warnings null", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			var value map[string]any
			mustJSONUnmarshal(t, entries["metadata.json"], &value)
			value["captureWarnings"] = nil
			entries["metadata.json"] = mustJSON(t, value)
		}},
		{"unrelated payload", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			entries["configs/notepad-plus-plus/foreign.xml"] = []byte("foreign")
		}},
		{"absent target payload", func(entries map[string][]byte, _ *liveCaptureArtifactClaims) {
			entries["configs/notepad-plus-plus/config.xml"] = []byte("unexpected")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := cloneLiveArtifactEntries(entries)
			candidateClaims := *claims
			test.mutate(candidate, &candidateClaims)
			candidatePath := writeLiveArtifactZip(t, filepath.Dir(path), test.name+".zip", candidate, nil)
			if _, failure := inspectLiveCaptureArtifact(definition, snapshots, candidateClaims, candidatePath); failure == nil {
				t.Fatal("hostile artifact was accepted")
			}
		})
	}
}

func TestInspectLiveCaptureArtifactRejectsUnsafeZipAndPathClaims(t *testing.T) {
	definition, path, snapshots, claims := productionLiveArtifactFixture(t)
	entries := readLiveArtifactEntriesForTest(t, path)
	tests := []struct {
		name   string
		writer func(string)
		claims func(*liveCaptureArtifactClaims)
	}{
		{"duplicate", func(path string) {
			writeLiveArtifactZip(t, filepath.Dir(path), filepath.Base(path), entries, func(writer *zip.Writer) { _, _ = writer.Create("manifest.jsonc") })
		}, nil},
		{"noncanonical case", func(path string) {
			candidate := cloneLiveArtifactEntries(entries)
			candidate["CONFIGS/notepad-plus-plus/config.xml"] = candidate["configs/notepad-plus-plus/config.xml"]
			delete(candidate, "configs/notepad-plus-plus/config.xml")
			writeLiveArtifactZip(t, filepath.Dir(path), filepath.Base(path), candidate, nil)
		}, nil},
		{"backslash member", func(path string) {
			candidate := cloneLiveArtifactEntries(entries)
			candidate[`configs\notepad-plus-plus\config.xml`] = candidate["configs/notepad-plus-plus/config.xml"]
			delete(candidate, "configs/notepad-plus-plus/config.xml")
			writeLiveArtifactZip(t, filepath.Dir(path), filepath.Base(path), candidate, nil)
		}, nil},
		{"directory mode", func(path string) {
			writeLiveArtifactZip(t, filepath.Dir(path), filepath.Base(path), entries, func(writer *zip.Writer) {
				for _, header := range []*zip.FileHeader{{Name: "configs/"}, {Name: "configs/notepad-plus-plus/"}} {
					header.SetMode(os.ModeDir | 0o700)
					if _, err := writer.CreateHeader(header); err != nil {
						t.Fatal(err)
					}
				}
			})
		}, nil},
		{"directory without trailing slash", func(path string) {
			writeLiveArtifactZip(t, filepath.Dir(path), filepath.Base(path), entries, func(writer *zip.Writer) {
				for _, header := range []*zip.FileHeader{{Name: "configs"}, {Name: "configs/notepad-plus-plus"}} {
					header.SetMode(os.ModeDir | 0o755)
					if _, err := writer.CreateHeader(header); err != nil {
						t.Fatal(err)
					}
				}
			})
		}, nil},
		{"directory", func(path string) {
			writeLiveArtifactZip(t, filepath.Dir(path), filepath.Base(path), entries, func(writer *zip.Writer) { _, _ = writer.Create("foreign/") })
		}, nil},
		{"link", func(path string) {
			writeLiveArtifactZip(t, filepath.Dir(path), filepath.Base(path), entries, func(writer *zip.Writer) {
				header := &zip.FileHeader{Name: "link"}
				header.SetMode(os.ModeSymlink | 0o777)
				entry, _ := writer.CreateHeader(header)
				_, _ = io.WriteString(entry, "target")
			})
		}, nil},
		{"ratio", func(path string) {
			writeLiveArtifactZip(t, filepath.Dir(path), filepath.Base(path), map[string][]byte{"manifest.jsonc": entries["manifest.jsonc"], "metadata.json": entries["metadata.json"], "configs/notepad-plus-plus/config.xml": bytes.Repeat([]byte("0"), 1024*1024)}, nil)
		}, nil},
		{"output mismatch", nil, func(claims *liveCaptureArtifactClaims) {
			claims.OutputPath = filepath.Join(filepath.Dir(path), "other.zip")
		}},
		{"event mismatch", nil, func(claims *liveCaptureArtifactClaims) {
			claims.EventPath = filepath.Join(filepath.Dir(path), "other.zip")
		}},
		{"receipt mismatch", nil, func(claims *liveCaptureArtifactClaims) {
			claims.Receipt.Path = filepath.Join(filepath.Dir(path), "other.zip")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidatePath := path
			if test.writer != nil {
				candidatePath = filepath.Join(filepath.Dir(path), test.name+".zip")
				test.writer(candidatePath)
			}
			candidateClaims := *claims
			candidateClaims.OutputPath, candidateClaims.EventPath, candidateClaims.Receipt.Path = candidatePath, candidatePath, candidatePath
			if test.claims != nil {
				test.claims(&candidateClaims)
			}
			if _, failure := inspectLiveCaptureArtifact(definition, snapshots, candidateClaims, candidatePath); failure == nil {
				t.Fatal("unsafe artifact or mismatched path claim was accepted")
			}
		})
	}
}

func TestInspectLiveCaptureArtifactRequiresProductionDirectoryMembers(t *testing.T) {
	definition, path, snapshots, claims := productionLiveArtifactFixture(t)
	entries := readLiveArtifactEntriesForTest(t, path)
	repacked := writeLiveArtifactZip(t, filepath.Dir(path), "repacked.zip", entries, nil)
	copy := *claims
	copy.OutputPath, copy.EventPath, copy.Receipt.Path = repacked, repacked, repacked
	if _, failure := inspectLiveCaptureArtifact(definition, snapshots, copy, repacked); failure == nil {
		t.Fatal("repacked artifact without production directory members was accepted")
	}
}

func productionLiveArtifactFixture(t *testing.T) (LiveDefinition, string, []liveTargetSnapshot, *liveCaptureArtifactClaims) {
	t.Helper()
	definition, err := CompileLiveDefinition(productionLiveRepoRoot(t), "apps.notepad-plus-plus")
	if err != nil {
		t.Fatal(err)
	}
	module, err := modules.ParseModuleJSON(mustReadFile(t, filepath.Join(productionLiveRepoRoot(t), "modules", "apps", "notepad-plus-plus", "module.jsonc")))
	if err != nil {
		t.Fatal(err)
	}
	appData := t.TempDir()
	t.Setenv("APPDATA", appData)
	snapshots := make([]liveTargetSnapshot, 0, len(definition.Comparator.Mappings))
	for _, mapping := range definition.Comparator.Mappings {
		relative := strings.TrimPrefix(strings.ReplaceAll(mapping.CaptureTemplate, "%APPDATA%\\", ""), `\`)
		target := filepath.Join(appData, filepath.FromSlash(strings.ReplaceAll(relative, `\`, "/")))
		payload := []byte("seed-derived " + mapping.Identity)
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, payload, 0o600); err != nil {
			t.Fatal(err)
		}
		snapshots = append(snapshots, liveTargetSnapshot{Identity: mapping.Identity, Mode: 0o600, Size: int64(len(payload)), SHA256: liveSHA256(payload), Bytes: payload})
	}
	manifestPath := filepath.Join(t.TempDir(), "captured.jsonc")
	input := manifest.Manifest{Version: 1, Apps: []manifest.App{{ID: liveManifestAppID(definition.WingetRef), Refs: map[string]string{"windows": definition.WingetRef}, Driver: "winget", Source: "winget"}}}
	if err := os.WriteFile(manifestPath, mustJSON(t, input), 0o600); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "captured.zip")
	result, err := bundle.CreateCaptureBundle(bundle.CaptureBundleRequest{ManifestPath: manifestPath, OutputPath: output, EndstateVersion: "test-version", Modules: []*modules.Module{module}})
	if err != nil {
		t.Fatal(err)
	}
	if result.ManifestVersion != 1 || result.BundleSchemaVersion != "1.0" || len(result.LegacyModules) != 1 || result.LegacyModules[0].FilesCaptured != len(definition.Comparator.Mappings) {
		t.Fatalf("production v1 capture result = %+v", result)
	}
	entries := readLiveArtifactEntriesForTest(t, output)
	var metadata bundle.BundleMetadata
	mustJSONUnmarshal(t, entries["metadata.json"], &metadata)
	claims := &liveCaptureArtifactClaims{OutputPath: output, EventPath: output, Receipt: liveReceiptArtifactPathClaim{Path: output}, ModuleRevision: definition.ModuleRevision, MachineName: metadata.MachineName, CapturedAt: metadata.CapturedAt, EndstateVersion: metadata.EndstateVersion, OS: runtime.GOOS, RestoreProjection: append([]modules.RestoreDef(nil), module.Restore...), VerifyProjection: append([]modules.VerifyDef(nil), module.Verify...)}
	return definition, output, snapshots, claims
}

func readLiveArtifactEntriesForTest(t *testing.T, path string) map[string][]byte {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	values := map[string][]byte{}
	for _, file := range reader.File {
		if strings.HasSuffix(file.Name, "/") {
			continue
		}
		stream, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(stream)
		_ = stream.Close()
		if err != nil {
			t.Fatal(err)
		}
		values[file.Name] = data
	}
	return values
}
func writeLiveArtifactZip(t *testing.T, dir, name string, entries map[string][]byte, extra func(*zip.Writer)) string {
	t.Helper()
	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, data := range entries {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if extra != nil {
		extra(writer)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}
func cloneLiveArtifactEntries(entries map[string][]byte) map[string][]byte {
	cloned := make(map[string][]byte, len(entries))
	for name, data := range entries {
		cloned[name] = append([]byte(nil), data...)
	}
	return cloned
}
func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func mustJSONUnmarshal(t *testing.T, data []byte, value any) {
	t.Helper()
	if err := json.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}
