// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package manifest

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// BundleManifestEntry is the archive-root path every capture bundle stores its
// manifest at. See the capture-bundle-zip contract.
const BundleManifestEntry = "manifest.jsonc"

// maxBundleManifestBytes caps what will be read out of an archive entry. A
// capture manifest is a list of apps and config lanes — tens of KB in practice.
// The cap keeps a hostile or corrupt archive from being decompressed into
// memory without bound (a zip entry can claim any uncompressed size).
const maxBundleManifestBytes = 64 << 20 // 64 MiB

// IsBundlePath reports whether path names a capture bundle rather than a
// manifest file. Extension-based on purpose: the caller is choosing how to read
// a path the user supplied, before anything has been opened.
func IsBundlePath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".zip")
}

// LoadManifestOrBundle loads a manifest from either a manifest file or a
// capture bundle (.zip).
//
// A bundle carries manifest.jsonc at its archive root, which makes it a
// complete source for anything that only reads the manifest — verify, and the
// scheduled drift check that runs verify. Before this, a bundle could never be
// a schedule baseline: the loader parses raw JSONC only, so the GUI had to
// side-write a `<bundle>.zip.manifest.jsonc` next to every saved bundle purely
// to give the scheduler something it could read. That sidecar is a second file
// to keep paired with the first, and renaming or moving the bundle silently
// orphans it.
//
// Payloads are deliberately NOT extracted. Commands that need bundle contents
// (restore) must keep unpacking the bundle themselves; this is for readers of
// the manifest alone.
func LoadManifestOrBundle(path string) (*Manifest, error) {
	if !IsBundlePath(path) {
		return LoadManifest(path)
	}

	data, err := ReadBundleManifest(path)
	if err != nil {
		return nil, err
	}

	// Reuse LoadManifest rather than re-implementing version dispatch and
	// validation: write the entry to a temp file and load that. Includes inside
	// a bundle manifest would resolve against the temp directory, but capture
	// bundles are self-contained and never declare includes.
	temp, err := os.CreateTemp("", "endstate-bundle-manifest-*.jsonc")
	if err != nil {
		return nil, fmt.Errorf("manifest: cannot stage bundle manifest from %q: %w", path, err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return nil, fmt.Errorf("manifest: cannot stage bundle manifest from %q: %w", path, err)
	}
	if err := temp.Close(); err != nil {
		return nil, fmt.Errorf("manifest: cannot stage bundle manifest from %q: %w", path, err)
	}

	return LoadManifest(tempPath)
}

// ReadBundleManifest returns the raw bytes of manifest.jsonc from a capture
// bundle, without unpacking anything else.
func ReadBundleManifest(bundlePath string) ([]byte, error) {
	archive, err := zip.OpenReader(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("manifest: cannot read bundle %q: %w", bundlePath, err)
	}
	defer func() { _ = archive.Close() }()

	for _, file := range archive.File {
		// Compare on the normalized archive path so a bundle written with
		// backslashes still resolves, and so only the ROOT entry matches — a
		// nested configs/.../manifest.jsonc must never be mistaken for it.
		name := path.Clean(strings.ReplaceAll(file.Name, `\`, "/"))
		if name != BundleManifestEntry {
			continue
		}
		entry, err := file.Open()
		if err != nil {
			return nil, fmt.Errorf("manifest: cannot read %s in bundle %q: %w", BundleManifestEntry, bundlePath, err)
		}
		defer func() { _ = entry.Close() }()

		data, err := io.ReadAll(io.LimitReader(entry, maxBundleManifestBytes+1))
		if err != nil {
			return nil, fmt.Errorf("manifest: cannot read %s in bundle %q: %w", BundleManifestEntry, bundlePath, err)
		}
		if len(data) > maxBundleManifestBytes {
			return nil, fmt.Errorf("manifest: %s in bundle %q exceeds %d bytes", BundleManifestEntry, bundlePath, maxBundleManifestBytes)
		}
		return data, nil
	}

	return nil, fmt.Errorf("manifest: bundle %q has no %s at its archive root", bundlePath, BundleManifestEntry)
}
