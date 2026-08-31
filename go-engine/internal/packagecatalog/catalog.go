// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package packagecatalog loads reviewed, source-qualified mappings from native
// Linux inventory to portable Nix realization intent.
package packagecatalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

var (
	portableIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[-._][a-z0-9]+)*$`)
	nixAttribute      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9+_-]*(?:\.[A-Za-z0-9][A-Za-z0-9+_-]*)*$`)
)

var allowedSources = map[string]struct{}{
	"nix": {}, "endstate-profile": {}, "debian": {}, "rpm": {}, "arch": {},
	"flatpak": {}, "snap": {}, "desktop": {}, "executable": {}, "config": {},
}

// NixTarget is the portable attribute under the release-owned Nixpkgs input.
type NixTarget struct {
	Attribute string `json:"attribute"`
}

// Entry is one reviewed portable application mapping.
type Entry struct {
	SchemaVersion int                 `json:"schemaVersion"`
	ID            string              `json:"id"`
	DisplayName   string              `json:"displayName"`
	Nix           NixTarget           `json:"nix"`
	Aliases       map[string][]string `json:"aliases"`
	ConfigModule  string              `json:"configModule,omitempty"`

	Revision string `json:"-"`
	FilePath string `json:"-"`
}

// Catalog holds entries indexed both by portable ID and exact
// source-qualified alias.
type Catalog struct {
	entries map[string]*Entry
	aliases map[string]*Entry
}

// ParseEntry parses one strict JSONC mapping and computes its canonical
// revision. Unknown fields are rejected because a misspelled alias or Nix
// target would otherwise silently change portability behavior.
func ParseEntry(data []byte, filePath string) (*Entry, error) {
	clean := manifest.StripJsoncComments(data)
	var entry Entry
	decoder := json.NewDecoder(bytes.NewReader(clean))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&entry); err != nil {
		return nil, fmt.Errorf("parse package mapping %s: %w", filePath, err)
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, fmt.Errorf("parse package mapping %s: %w", filePath, err)
	}
	if err := validateEntry(&entry); err != nil {
		return nil, fmt.Errorf("invalid package mapping %s: %w", filePath, err)
	}

	var canonicalValue any
	canonicalDecoder := json.NewDecoder(bytes.NewReader(clean))
	canonicalDecoder.UseNumber()
	if err := canonicalDecoder.Decode(&canonicalValue); err != nil {
		return nil, fmt.Errorf("canonicalize package mapping %s: %w", filePath, err)
	}
	canonical, err := json.Marshal(canonicalValue)
	if err != nil {
		return nil, fmt.Errorf("canonicalize package mapping %s: %w", filePath, err)
	}
	sum := sha256.Sum256(canonical)
	entry.Revision = hex.EncodeToString(sum[:])
	entry.FilePath = filePath
	return &entry, nil
}

func validateEntry(entry *Entry) error {
	if entry.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schemaVersion %d", entry.SchemaVersion)
	}
	if !portableIDPattern.MatchString(entry.ID) {
		return fmt.Errorf("id %q is not a stable portable ID", entry.ID)
	}
	if strings.TrimSpace(entry.DisplayName) == "" {
		return fmt.Errorf("displayName is required")
	}
	if !nixAttribute.MatchString(entry.Nix.Attribute) {
		return fmt.Errorf("nix attribute %q is not a portable attribute path", entry.Nix.Attribute)
	}
	if len(entry.Aliases) == 0 {
		return fmt.Errorf("at least one source-qualified alias is required")
	}
	for source, refs := range entry.Aliases {
		if _, ok := allowedSources[source]; !ok {
			return fmt.Errorf("alias source %q is not supported", source)
		}
		if len(refs) == 0 {
			return fmt.Errorf("alias source %q has no refs", source)
		}
		seen := make(map[string]struct{}, len(refs))
		for _, ref := range refs {
			if strings.TrimSpace(ref) == "" || ref != strings.TrimSpace(ref) {
				return fmt.Errorf("alias source %q contains an empty or padded ref", source)
			}
			if _, duplicate := seen[ref]; duplicate {
				return fmt.Errorf("alias source %q repeats ref %q", source, ref)
			}
			seen[ref] = struct{}{}
		}
	}
	if entry.ConfigModule != "" && !strings.HasPrefix(entry.ConfigModule, "apps.") {
		return fmt.Errorf("configModule %q must use the apps.* namespace", entry.ConfigModule)
	}
	return nil
}

// Load reads every JSON/JSONC entry in a directory and rejects ambiguous IDs or
// aliases before runtime resolution.
func Load(dir string) (*Catalog, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read package catalog %s: %w", dir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	catalog := &Catalog{entries: make(map[string]*Entry), aliases: make(map[string]*Entry)}
	for _, directoryEntry := range entries {
		if directoryEntry.IsDir() {
			continue
		}
		ext := strings.ToLower(filepath.Ext(directoryEntry.Name()))
		if ext != ".json" && ext != ".jsonc" {
			continue
		}
		path := filepath.Join(dir, directoryEntry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil, fmt.Errorf("read package mapping %s: %w", path, readErr)
		}
		entry, parseErr := ParseEntry(data, path)
		if parseErr != nil {
			return nil, parseErr
		}
		if previous := catalog.entries[entry.ID]; previous != nil {
			return nil, fmt.Errorf("duplicate portable package id %q in %s and %s", entry.ID, previous.FilePath, entry.FilePath)
		}
		for source, refs := range entry.Aliases {
			for _, ref := range refs {
				key := aliasKey(source, ref)
				if previous := catalog.aliases[key]; previous != nil {
					return nil, fmt.Errorf("duplicate package alias %s:%s in %s and %s", source, ref, previous.FilePath, entry.FilePath)
				}
				catalog.aliases[key] = entry
			}
		}
		catalog.entries[entry.ID] = entry
	}
	return catalog, nil
}

// Resolve returns the mapping for one exact source namespace/ref pair.
func (catalog *Catalog) Resolve(source, ref string) (*Entry, bool) {
	if catalog == nil {
		return nil, false
	}
	entry, ok := catalog.aliases[aliasKey(source, ref)]
	return entry, ok
}

// Entry returns one portable entry by ID.
func (catalog *Catalog) Entry(id string) (*Entry, bool) {
	if catalog == nil {
		return nil, false
	}
	entry, ok := catalog.entries[id]
	return entry, ok
}

// Entries returns portable entries in deterministic ID order.
func (catalog *Catalog) Entries() []*Entry {
	if catalog == nil {
		return []*Entry{}
	}
	ids := make([]string, 0, len(catalog.entries))
	for id := range catalog.entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]*Entry, 0, len(ids))
	for _, id := range ids {
		result = append(result, catalog.entries[id])
	}
	return result
}

func aliasKey(source, ref string) string { return source + "\x00" + ref }

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values")
		}
		return err
	}
	return nil
}
