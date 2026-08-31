// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// endstate-hm-registry refreshes/checks the frozen runtime adapter registry.
// It is release tooling and is never invoked by capture.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/hmharvest"
	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

func main() {
	registryPath := flag.String("registry", "", "path to catalog/home-manager/registry.jsonc")
	docsPath := flag.String("docs-json", "", "path to the pinned Home Manager options.json")
	sourceRoot := flag.String("home-manager-source", "", "path to the exact pinned Home Manager source checkout")
	system := flag.String("system", defaultNixSystem(), "Linux Nix system used for pure managed-target probes")
	write := flag.Bool("write", false, "atomically write refreshed canonical registry JSON")
	acceptReviews := flag.Bool("accept-reviews", false, "explicitly accept refreshed metadata/dispositions (required with --write)")
	flag.Parse()
	if err := run(*registryPath, *docsPath, *sourceRoot, *system, *write, *acceptReviews); err != nil {
		fmt.Fprintln(os.Stderr, "endstate-hm-registry:", err)
		os.Exit(1)
	}
}

func run(registryPath, docsPath, sourceRoot, system string, write, acceptReviews bool) error {
	if registryPath == "" || docsPath == "" || sourceRoot == "" {
		return fmt.Errorf("--registry, --docs-json, and --home-manager-source are required")
	}
	if write && !acceptReviews {
		return fmt.Errorf("--write requires --accept-reviews so upstream drift cannot be blessed accidentally")
	}
	inputs, err := releaseinputs.Load()
	if err != nil {
		return err
	}
	revision, err := gitRevision(sourceRoot)
	if err != nil {
		return err
	}
	if revision != inputs.HomeManager.Revision {
		return fmt.Errorf("Home Manager source revision %s does not match release pin %s", revision, inputs.HomeManager.Revision)
	}
	registryData, err := os.ReadFile(registryPath)
	if err != nil {
		return err
	}
	registry, err := hmregistry.Decode(registryData)
	if err != nil {
		return err
	}
	original, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	docsJSON, err := os.ReadFile(docsPath)
	if err != nil {
		return err
	}
	prober := hmharvest.NewNixTargetProber(inputs, system)
	if err := hmharvest.Refresh(registry, docsJSON, sourceRoot, inputs, prober); err != nil {
		return err
	}
	refreshed, err := json.MarshalIndent(registry, "", "  ")
	if err != nil {
		return err
	}
	refreshed = append(refreshed, '\n')
	if !write {
		if !bytes.Equal(append(original, '\n'), refreshed) {
			return fmt.Errorf("registry drift detected; review the pinned diff, then rerun with --write --accept-reviews")
		}
		fmt.Println("Home Manager adapter registry matches the pinned docs and source")
		return nil
	}
	if err := writeAtomic(registryPath, refreshed); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", registryPath)
	return nil
}

func defaultNixSystem() string {
	switch runtime.GOARCH {
	case "arm64":
		return "aarch64-linux"
	default:
		return "x86_64-linux"
	}
}

func gitRevision(sourceRoot string) (string, error) {
	command := exec.Command("git", "-C", sourceRoot, "rev-parse", "HEAD")
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("read Home Manager source revision: %w", err)
	}
	revision := strings.TrimSpace(string(output))
	if len(revision) != 40 {
		return "", fmt.Errorf("Home Manager source revision is not a full Git hash")
	}
	return revision, nil
}

func writeAtomic(path string, data []byte) (returnErr error) {
	directory := filepath.Dir(path)
	temp, err := os.CreateTemp(directory, ".registry-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer func() {
		_ = temp.Close()
		if returnErr != nil {
			_ = os.Remove(tempPath)
		}
	}()
	if err := temp.Chmod(0o644); err != nil {
		return err
	}
	if _, err := temp.Write(data); err != nil {
		return err
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
