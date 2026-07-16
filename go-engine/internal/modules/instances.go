// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/config"
)

// PackageEvidence is runtime evidence supplied by an engine package backend.
// Discovery never queries a backend itself, which keeps module declarations
// declarative and makes the evidence used for a run explicit.
type PackageEvidence struct {
	AppID      string `json:"appId,omitempty"`
	Backend    string `json:"backend"`
	Platform   string `json:"platform,omitempty"`
	Ref        string `json:"ref"`
	Driver     string `json:"driver,omitempty"`
	RawVersion string `json:"rawVersion,omitempty"`
}

// InstanceEvidence records how a concrete config instance was found.
type InstanceEvidence struct {
	Type     string `json:"type"`
	AppID    string `json:"appId,omitempty"`
	Backend  string `json:"backend,omitempty"`
	Platform string `json:"platform,omitempty"`
	Ref      string `json:"ref,omitempty"`
	Driver   string `json:"driver,omitempty"`
	Path     string `json:"-"`
}

// ConfigInstance is one detected application/config root. Side-by-side roots
// remain separate instances even when their versions can be numerically
// compared.
type ConfigInstance struct {
	ID               string           `json:"id"`
	ModuleID         string           `json:"moduleId"`
	DetectorID       string           `json:"detectorId"`
	Root             string           `json:"-"`
	Version          VersionEvidence  `json:"version"`
	Evidence         InstanceEvidence `json:"evidence"`
	CanonicalLocator string           `json:"-"`
}

// DiscoveryOptions contains replaceable operating-system boundaries. A zero
// value uses filepath.Glob.
type DiscoveryOptions struct {
	Glob func(pattern string) ([]string, error)
}

// InstancePathRole distinguishes host paths from portable bundle-relative
// paths. Portable paths are held to the stricter containment contract.
type InstancePathRole string

const (
	HostPath             InstancePathRole = "host"
	PortableRelativePath InstancePathRole = "portable-relative"
)

// StableInstanceID derives an opaque stable identifier from non-secret,
// canonical discovery coordinates.
func StableInstanceID(moduleID, detectorID, canonicalLocator string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(moduleID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(detectorID))
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write([]byte(canonicalLocator))
	return "instance-" + hex.EncodeToString(hash.Sum(nil))
}

type instanceAccumulator struct {
	instance    ConfigInstance
	rawVersions map[string]struct{}
}

// DiscoverInstances executes a schema-v2 module's declarative instance
// detectors. Package detectors consume only supplied evidence; path detectors
// expand and glob through the engine-owned boundary in DiscoveryOptions.
func DiscoverInstances(mod *Module, packages []PackageEvidence, options DiscoveryOptions) ([]ConfigInstance, error) {
	if mod == nil {
		return nil, fmt.Errorf("module is nil")
	}
	if mod.Config == nil || len(mod.Config.InstanceDetectors) == 0 {
		return []ConfigInstance{}, nil
	}

	glob := options.Glob
	if glob == nil {
		glob = filepath.Glob
	}

	accumulators := make(map[string]*instanceAccumulator)
	for _, detector := range mod.Config.InstanceDetectors {
		switch detector.Type {
		case "package":
			for _, evidence := range packages {
				instance, err := packageInstance(mod.ID, detector.ID, evidence)
				if err != nil {
					return nil, err
				}
				accumulateInstance(accumulators, instance)
			}
		case "path":
			instances, err := discoverPathInstances(mod.ID, detector, glob)
			if err != nil {
				return nil, err
			}
			for _, instance := range instances {
				accumulateInstance(accumulators, instance)
			}
		default:
			return nil, fmt.Errorf("module %q detector %q has unsupported type %q", mod.ID, detector.ID, detector.Type)
		}
	}

	instances := make([]ConfigInstance, 0, len(accumulators))
	for _, accumulator := range accumulators {
		instance := accumulator.instance
		if len(accumulator.rawVersions) == 1 {
			for rawVersion := range accumulator.rawVersions {
				instance.Version = NewVersionEvidence(rawVersion)
			}
		} else if len(accumulator.rawVersions) > 1 {
			// Conflicting duplicate evidence is not a license to choose a newer
			// or lexicographically preferred version. Preserve the instance and
			// leave its version unknown.
			instance.Version = NewVersionEvidence("")
		}
		instance.ID = StableInstanceID(instance.ModuleID, instance.DetectorID, instance.CanonicalLocator)
		instances = append(instances, instance)
	}
	sort.Slice(instances, func(left, right int) bool {
		if instances[left].DetectorID != instances[right].DetectorID {
			return instances[left].DetectorID < instances[right].DetectorID
		}
		return instances[left].CanonicalLocator < instances[right].CanonicalLocator
	})
	return instances, nil
}

func packageInstance(moduleID, detectorID string, evidence PackageEvidence) (ConfigInstance, error) {
	backend := strings.ToLower(strings.TrimSpace(evidence.Backend))
	ref := strings.TrimSpace(evidence.Ref)
	if backend == "" {
		return ConfigInstance{}, fmt.Errorf("module %q detector %q received package evidence without a backend", moduleID, detectorID)
	}
	if ref == "" {
		return ConfigInstance{}, fmt.Errorf("module %q detector %q received package evidence without a ref", moduleID, detectorID)
	}
	locator := "package:" + backend + ":" + canonicalPackageRef(backend, ref)
	return ConfigInstance{
		ModuleID:         moduleID,
		DetectorID:       detectorID,
		Version:          NewVersionEvidence(evidence.RawVersion),
		CanonicalLocator: locator,
		Evidence: InstanceEvidence{
			Type:     "package",
			AppID:    evidence.AppID,
			Backend:  backend,
			Platform: evidence.Platform,
			Ref:      ref,
			Driver:   evidence.Driver,
		},
	}, nil
}

func canonicalPackageRef(backend, ref string) string {
	switch backend {
	case "winget":
		return strings.ToLower(ref)
	default:
		return ref
	}
}

func discoverPathInstances(moduleID string, detector InstanceDetectorDef, glob func(string) ([]string, error)) ([]ConfigInstance, error) {
	expandedGlob := config.ExpandEnvVars(detector.Glob)
	if err := validateDiscoveryGlob(expandedGlob); err != nil {
		return nil, fmt.Errorf("module %q detector %q: %w", moduleID, detector.ID, err)
	}

	var versionPattern *regexp.Regexp
	if detector.VersionPattern != "" {
		if err := validateVersionExtractionPattern(detector.VersionPattern); err != nil {
			return nil, fmt.Errorf("module %q detector %q: %w", moduleID, detector.ID, err)
		}
		versionPattern = regexp.MustCompile(detector.VersionPattern)
	}

	matches, err := glob(expandedGlob)
	if err != nil {
		return nil, fmt.Errorf("module %q detector %q glob %q: %w", moduleID, detector.ID, expandedGlob, err)
	}
	instances := make([]ConfigInstance, 0, len(matches))
	for _, match := range matches {
		root, err := filepath.Abs(filepath.Clean(match))
		if err != nil {
			return nil, fmt.Errorf("module %q detector %q canonicalize %q: %w", moduleID, detector.ID, match, err)
		}
		rawVersion := ""
		if versionPattern != nil {
			parts := versionPattern.FindStringSubmatch(filepath.Base(root))
			if parts == nil {
				continue
			}
			rawVersion = parts[versionPattern.SubexpIndex("version")]
		}
		locator := "path:" + canonicalPath(root)
		instances = append(instances, ConfigInstance{
			ModuleID:         moduleID,
			DetectorID:       detector.ID,
			Root:             root,
			Version:          NewVersionEvidence(rawVersion),
			Evidence:         InstanceEvidence{Type: "path", Path: root},
			CanonicalLocator: locator,
		})
	}
	return instances, nil
}

func validateDiscoveryGlob(pattern string) error {
	if strings.TrimSpace(pattern) == "" {
		return fmt.Errorf("path glob is empty")
	}
	if strings.Contains(pattern, "**") {
		return fmt.Errorf("recursive glob ** is not supported")
	}
	if strings.ContainsAny(pattern, "{}") {
		return fmt.Errorf("brace glob expansion is not supported")
	}
	if _, err := filepath.Match(pattern, pattern); err != nil {
		return fmt.Errorf("malformed path glob %q: %w", pattern, err)
	}
	return nil
}

func canonicalPath(path string) string {
	canonical := filepath.ToSlash(filepath.Clean(path))
	if runtime.GOOS == "windows" {
		canonical = strings.ToLower(canonical)
	}
	return canonical
}

func accumulateInstance(accumulators map[string]*instanceAccumulator, candidate ConfigInstance) {
	key := candidate.DetectorID + "\x00" + candidate.CanonicalLocator
	accumulator, exists := accumulators[key]
	if !exists {
		accumulator = &instanceAccumulator{
			instance:    candidate,
			rawVersions: make(map[string]struct{}),
		}
		accumulators[key] = accumulator
	} else {
		accumulator.instance.Evidence = mergeInstanceEvidence(accumulator.instance.Evidence, candidate.Evidence)
		accumulator.instance.Root = deterministicNonEmpty(accumulator.instance.Root, candidate.Root)
	}
	if candidate.Version.Raw != "" {
		accumulator.rawVersions[candidate.Version.Raw] = struct{}{}
	}
}

func mergeInstanceEvidence(left, right InstanceEvidence) InstanceEvidence {
	return InstanceEvidence{
		Type:     deterministicNonEmpty(left.Type, right.Type),
		AppID:    deterministicNonEmpty(left.AppID, right.AppID),
		Backend:  deterministicNonEmpty(left.Backend, right.Backend),
		Platform: deterministicNonEmpty(left.Platform, right.Platform),
		Ref:      deterministicNonEmpty(left.Ref, right.Ref),
		Driver:   deterministicNonEmpty(left.Driver, right.Driver),
		Path:     deterministicNonEmpty(left.Path, right.Path),
	}
}

func deterministicNonEmpty(left, right string) string {
	if left == "" {
		return right
	}
	if right == "" || left <= right {
		return left
	}
	return right
}

// IsGenerationCaptureEligible reports whether a module has schema-v2
// generation capture declarations. It intentionally does not depend on an app
// match so path-only discovery can run with no package evidence.
func IsGenerationCaptureEligible(mod *Module) bool {
	if mod == nil || mod.EffectiveSchemaVersion() != 2 || mod.Config == nil || len(mod.Config.InstanceDetectors) == 0 {
		return false
	}
	for _, set := range mod.Config.Sets {
		for _, generation := range set.Generations {
			if generation.Capture != nil {
				return true
			}
		}
	}
	return false
}

// ExpandInstancePath expands the allowlisted instance placeholders and
// enforces the containment contract for the requested path role.
func ExpandInstancePath(template string, instance ConfigInstance, role InstancePathRole) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", fmt.Errorf("instance path is empty")
	}
	expanded, err := expandInstancePlaceholders(template, instance)
	if err != nil {
		return "", err
	}

	switch role {
	case PortableRelativePath:
		if containsPortableHostExpansion(expanded) {
			return "", fmt.Errorf("portable path %q contains a host expansion", template)
		}
		if isPortableAbsoluteOrVolume(expanded) {
			return "", fmt.Errorf("portable path %q is absolute or volume-qualified", template)
		}
		if hasTraversal(expanded) {
			return "", fmt.Errorf("portable path %q contains parent traversal", template)
		}
		return filepath.Clean(expanded), nil
	case HostPath:
		if hasTraversal(template) {
			return "", fmt.Errorf("host path %q contains parent traversal", template)
		}
		expanded = config.ExpandEnvVars(expanded)
		if hasTraversal(expanded) {
			return "", fmt.Errorf("expanded host path %q contains parent traversal", expanded)
		}
		expanded = filepath.Clean(expanded)
		if strings.Contains(template, "${instance.root}") {
			if err := requireContainedPath(instance.Root, expanded); err != nil {
				return "", err
			}
		}
		return expanded, nil
	default:
		return "", fmt.Errorf("unsupported instance path role %q", role)
	}
}

func containsPortableHostExpansion(path string) bool {
	trimmed := strings.TrimSpace(path)
	return strings.HasPrefix(trimmed, "~") || strings.Contains(trimmed, "%") || strings.Contains(trimmed, "$")
}

func isPortableAbsoluteOrVolume(path string) bool {
	trimmed := strings.TrimSpace(path)
	normalized := strings.ReplaceAll(trimmed, `\`, "/")
	if strings.HasPrefix(normalized, "/") || filepath.IsAbs(trimmed) || filepath.VolumeName(trimmed) != "" {
		return true
	}
	return len(normalized) >= 2 && ((normalized[0] >= 'A' && normalized[0] <= 'Z') || (normalized[0] >= 'a' && normalized[0] <= 'z')) && normalized[1] == ':'
}

func requireContainedPath(root, candidate string) error {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return fmt.Errorf("canonicalize instance root %q: %w", root, err)
	}
	candidateAbs, err := filepath.Abs(filepath.Clean(candidate))
	if err != nil {
		return fmt.Errorf("canonicalize expanded instance path %q: %w", candidate, err)
	}
	relative, err := filepath.Rel(rootAbs, candidateAbs)
	if err != nil {
		return fmt.Errorf("expanded instance path %q is not within root %q: %w", candidate, root, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return fmt.Errorf("expanded instance path %q escapes root %q", candidate, root)
	}
	return nil
}
