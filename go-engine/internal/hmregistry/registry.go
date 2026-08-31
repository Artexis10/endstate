// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package hmregistry loads the frozen, release-reviewed inversion registry
// derived from the exact Home Manager input used by Endstate.
package hmregistry

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

	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/releaseinputs"
)

const SchemaVersion = 1

type Disposition string

const (
	TypedRoundTrip Disposition = "typed-roundtrip"
	FileRoundTrip  Disposition = "file-roundtrip"
	CuratedCodec   Disposition = "curated-codec"
	Excluded       Disposition = "excluded"
)

var (
	registryIDPattern = regexp.MustCompile(`^[a-z0-9]+(?:[-._][a-z0-9]+)*$`)
	programPattern    = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]*$`)
	optionPattern     = regexp.MustCompile(`^programs\.[a-zA-Z0-9][a-zA-Z0-9_-]*(?:\.[a-zA-Z0-9][a-zA-Z0-9_-]*)+$`)
	sha256Pattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type InputIdentity struct {
	NixpkgsRevision     string `json:"nixpkgsRevision"`
	HomeManagerRevision string `json:"homeManagerRevision"`
}

type Registry struct {
	SchemaVersion  int           `json:"schemaVersion"`
	Inputs         InputIdentity `json:"inputs"`
	DocsJSONSHA256 string        `json:"docsJsonSha256"`
	Entries        []Entry       `json:"entries"`
}

type Entry struct {
	ID                string            `json:"id"`
	Program           string            `json:"program"`
	DisplayName       string            `json:"displayName"`
	PackageID         string            `json:"packageId,omitempty"`
	ModuleID          string            `json:"moduleId,omitempty"`
	Disposition       Disposition       `json:"disposition"`
	Codec             string            `json:"codec,omitempty"`
	ExclusionReason   string            `json:"exclusionReason,omitempty"`
	Probe             *Probe            `json:"probe,omitempty"`
	Options           []Option          `json:"options"`
	Targets           []Target          `json:"targets"`
	SourceHashes      map[string]string `json:"sourceHashes"`
	ReviewFingerprint string            `json:"reviewFingerprint"`
}

// Probe is a minimal, reviewed option assignment used only by release tooling
// to force every managed target owned by a supported adapter into
// Home Manager's evaluated home.file output.
type Probe struct {
	Values map[string]any `json:"values"`
}

type Option struct {
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Declarations []string `json:"declarations"`
}

type Target struct {
	Coordinate string `json:"coordinate"`
	Optional   bool   `json:"optional,omitempty"`
}

func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read Home Manager adapter registry %s: %w", path, err)
	}
	inputs, err := releaseinputs.Load()
	if err != nil {
		return nil, err
	}
	return Parse(data, inputs)
}

func Parse(data []byte, inputs releaseinputs.Inputs) (*Registry, error) {
	registry, err := Decode(data)
	if err != nil {
		return nil, err
	}
	if err := Validate(registry, inputs); err != nil {
		return nil, err
	}
	return registry, nil
}

// Decode strictly parses a registry without accepting its pinned/reviewed
// claims. It exists for the release-time harvester, which must be able to
// refresh stale metadata before Validate can legitimately succeed.
func Decode(data []byte) (*Registry, error) {
	clean := manifest.StripJsoncComments(data)
	var registry Registry
	decoder := json.NewDecoder(bytes.NewReader(clean))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&registry); err != nil {
		return nil, fmt.Errorf("parse Home Manager adapter registry: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, err
	}
	return &registry, nil
}

func Validate(registry *Registry, inputs releaseinputs.Inputs) error {
	if registry == nil {
		return fmt.Errorf("Home Manager adapter registry is nil")
	}
	if registry.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported Home Manager adapter registry schema %d", registry.SchemaVersion)
	}
	if registry.Inputs.NixpkgsRevision != inputs.Nixpkgs.Revision ||
		registry.Inputs.HomeManagerRevision != inputs.HomeManager.Revision {
		return fmt.Errorf("Home Manager adapter registry input revisions do not match this release")
	}
	if !sha256Pattern.MatchString(registry.DocsJSONSHA256) {
		return fmt.Errorf("Home Manager adapter registry docsJsonSha256 is not a lowercase SHA-256")
	}
	ids := make(map[string]bool)
	targets := make(map[string]string)
	previousID := ""
	for index := range registry.Entries {
		entry := &registry.Entries[index]
		if previousID != "" && entry.ID <= previousID {
			return fmt.Errorf("Home Manager adapter registry entries are not in unique ID order at %q", entry.ID)
		}
		previousID = entry.ID
		if ids[entry.ID] {
			return fmt.Errorf("duplicate Home Manager adapter id %q", entry.ID)
		}
		ids[entry.ID] = true
		if err := validateEntry(entry, targets); err != nil {
			return fmt.Errorf("Home Manager adapter %q: %w", entry.ID, err)
		}
		fingerprint, err := ReviewFingerprint(*entry)
		if err != nil {
			return fmt.Errorf("Home Manager adapter %q: compute review fingerprint: %w", entry.ID, err)
		}
		if entry.ReviewFingerprint != fingerprint {
			return fmt.Errorf("Home Manager adapter %q review is stale: metadata or disposition changed", entry.ID)
		}
	}
	return nil
}

func validateEntry(entry *Entry, targets map[string]string) error {
	if !registryIDPattern.MatchString(entry.ID) {
		return fmt.Errorf("id is not a stable portable identifier")
	}
	if !programPattern.MatchString(entry.Program) {
		return fmt.Errorf("program %q is invalid", entry.Program)
	}
	if strings.TrimSpace(entry.DisplayName) == "" {
		return fmt.Errorf("displayName is required")
	}
	if entry.PackageID != "" && !registryIDPattern.MatchString(entry.PackageID) {
		return fmt.Errorf("packageId %q is invalid", entry.PackageID)
	}
	if entry.ModuleID != "" && !strings.HasPrefix(entry.ModuleID, "apps.") {
		return fmt.Errorf("moduleId %q must use the apps.* namespace", entry.ModuleID)
	}
	switch entry.Disposition {
	case TypedRoundTrip, FileRoundTrip, CuratedCodec:
		if strings.TrimSpace(entry.Codec) == "" {
			return fmt.Errorf("%s disposition requires a reviewed codec", entry.Disposition)
		}
		if !supportedCodec(entry.Disposition, entry.Codec) {
			return fmt.Errorf("codec %q is not implemented for %s", entry.Codec, entry.Disposition)
		}
		if entry.ExclusionReason != "" {
			return fmt.Errorf("supported disposition cannot declare exclusionReason")
		}
		if len(entry.Targets) == 0 {
			return fmt.Errorf("%s disposition requires at least one target", entry.Disposition)
		}
		if entry.Probe == nil || len(entry.Probe.Values) == 0 {
			return fmt.Errorf("%s disposition requires a reviewed target probe", entry.Disposition)
		}
	case Excluded:
		if strings.TrimSpace(entry.ExclusionReason) == "" {
			return fmt.Errorf("excluded disposition requires exclusionReason")
		}
		if entry.Codec != "" {
			return fmt.Errorf("excluded disposition cannot declare a codec")
		}
		if entry.Probe != nil {
			return fmt.Errorf("excluded disposition cannot declare a probe")
		}
	default:
		return fmt.Errorf("disposition %q is not supported", entry.Disposition)
	}
	if len(entry.Options) == 0 {
		return fmt.Errorf("at least one pinned Home Manager option is required")
	}
	previousOption := ""
	declarations := make(map[string]bool)
	for _, option := range entry.Options {
		if !optionPattern.MatchString(option.Name) || !strings.HasPrefix(option.Name, "programs."+entry.Program+".") {
			return fmt.Errorf("option %q is outside programs.%s", option.Name, entry.Program)
		}
		if previousOption != "" && option.Name <= previousOption {
			return fmt.Errorf("options are not in unique name order at %q", option.Name)
		}
		previousOption = option.Name
		if strings.TrimSpace(option.Type) == "" || len(option.Declarations) == 0 {
			return fmt.Errorf("option %q is missing type or declarations", option.Name)
		}
		for _, declaration := range option.Declarations {
			if !safeDeclarationPath(declaration) {
				return fmt.Errorf("option %q has unsafe declaration %q", option.Name, declaration)
			}
			declarations[declaration] = true
		}
	}
	if entry.Probe != nil {
		optionNames := make(map[string]bool, len(entry.Options))
		for _, option := range entry.Options {
			optionNames[option.Name] = true
		}
		for name, value := range entry.Probe.Values {
			if !optionNames[name] {
				return fmt.Errorf("probe value %q is not a harvested option", name)
			}
			if err := validateProbeValue(value, 0); err != nil {
				return fmt.Errorf("probe value %q: %w", name, err)
			}
		}
	}
	for declaration := range declarations {
		if !sha256Pattern.MatchString(entry.SourceHashes[declaration]) {
			return fmt.Errorf("declaration %q has no reviewed source hash", declaration)
		}
	}
	if len(entry.SourceHashes) != len(declarations) {
		return fmt.Errorf("sourceHashes contains undeclared or duplicate authority")
	}
	previousTarget := ""
	for _, target := range entry.Targets {
		if err := config.ValidateEnginePath(target.Coordinate, "linux"); err != nil {
			return fmt.Errorf("target %q is unsafe: %w", target.Coordinate, err)
		}
		if previousTarget != "" && target.Coordinate <= previousTarget {
			return fmt.Errorf("targets are not in unique coordinate order at %q", target.Coordinate)
		}
		previousTarget = target.Coordinate
		if owner := targets[target.Coordinate]; owner != "" {
			return fmt.Errorf("target %q is also owned by %q", target.Coordinate, owner)
		}
		targets[target.Coordinate] = entry.ID
	}
	return nil
}

func validateProbeValue(value any, depth int) error {
	if depth > 32 {
		return fmt.Errorf("nesting exceeds 32 levels")
	}
	switch typed := value.(type) {
	case nil, bool, string, json.Number, float64:
		return nil
	case []any:
		for _, item := range typed {
			if err := validateProbeValue(item, depth+1); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		for key, item := range typed {
			if strings.TrimSpace(key) == "" {
				return fmt.Errorf("attribute name is empty")
			}
			if err := validateProbeValue(item, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported JSON value type %T", value)
	}
}

func supportedCodec(disposition Disposition, codec string) bool {
	return disposition == FileRoundTrip && codec == "bounded-regular-file-v1"
}

func safeDeclarationPath(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(value))
	return strings.HasPrefix(clean, "modules/") && clean == value && !strings.Contains(clean, "../")
}

// ReviewFingerprint binds the upstream option/target/source facts to the
// reviewed disposition and codec. A changed Home Manager declaration cannot
// silently retain approval.
func ReviewFingerprint(entry Entry) (string, error) {
	entry.ReviewFingerprint = ""
	canonical, err := json.Marshal(entry)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

func AcceptReviews(registry *Registry) error {
	sort.Slice(registry.Entries, func(i, j int) bool { return registry.Entries[i].ID < registry.Entries[j].ID })
	for index := range registry.Entries {
		fingerprint, err := ReviewFingerprint(registry.Entries[index])
		if err != nil {
			return err
		}
		registry.Entries[index].ReviewFingerprint = fingerprint
	}
	return nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parse Home Manager adapter registry: multiple JSON values")
		}
		return fmt.Errorf("parse Home Manager adapter registry: %w", err)
	}
	return nil
}
