// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package bundle

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

const (
	ConfigCaptureInvalidPlan          = "CONFIG_CAPTURE_INVALID_PLAN"
	ConfigCaptureUnsafePath           = "CONFIG_CAPTURE_UNSAFE_PATH"
	ConfigCaptureMissingRequired      = "CONFIG_CAPTURE_MISSING_REQUIRED"
	ConfigCaptureDestinationCollision = "CONFIG_CAPTURE_DESTINATION_COLLISION"
	ConfigCaptureLinkUnsupported      = "CONFIG_CAPTURE_LINK_UNSUPPORTED"
	ConfigCaptureIO                   = "CONFIG_CAPTURE_IO"
	ConfigCaptureRegistryUnsupported  = "CONFIG_CAPTURE_REGISTRY_UNSUPPORTED"
)

// ConfigCaptureError is a stable typed collection failure suitable for later
// per-set capture diagnostics.
type ConfigCaptureError struct {
	Code   string
	Detail string
}

func (e *ConfigCaptureError) Error() string { return "bundle config capture: " + e.Detail }

// UnsupportedConfigCaptureError distinguishes a declared capture primitive
// that this generation-aware collector cannot safely represent yet.
type UnsupportedConfigCaptureError struct {
	Code   string
	Detail string
}

func (e *UnsupportedConfigCaptureError) Error() string { return "bundle config capture: " + e.Detail }

// ConfigCaptureDiagnosticCode extracts a stable collection diagnostic.
func ConfigCaptureDiagnosticCode(err error) string {
	var captureErr *ConfigCaptureError
	if errors.As(err, &captureErr) {
		return captureErr.Code
	}
	var unsupportedErr *UnsupportedConfigCaptureError
	if errors.As(err, &unsupportedErr) {
		return unsupportedErr.Code
	}
	return ""
}

// ConfigSetCapturePlan pins the exact catalog objects and discovered source
// instance used to collect one independently versioned config set.
type ConfigSetCapturePlan struct {
	Module     *modules.Module
	Set        *modules.ConfigSetDef
	Generation *modules.GenerationDef
	Instance   modules.ConfigInstance
}

// ConfigSetCollection describes one isolated staged payload. PayloadRoot and
// Files are portable bundle-relative paths; callers use the staging root when
// opening them on the host.
type ConfigSetCollection struct {
	CaptureID       string
	PayloadRoot     string
	Files           []string
	FilesCollected  int
	SecretsExcluded int
}

// CaptureID returns an opaque deterministic identity for a module/config-set/
// instance tuple. Length prefixes make tuple framing unambiguous.
func CaptureID(moduleID, setID, instanceID string) string {
	hash := sha256.New()
	var length [8]byte
	for _, component := range []string{moduleID, setID, instanceID} {
		binary.BigEndian.PutUint64(length[:], uint64(len(component)))
		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(component))
	}
	return "capture-" + hex.EncodeToString(hash.Sum(nil))
}

type configCopyItem struct {
	source string
	dest   string
}

type configCopyPreflight struct {
	items           []configCopyItem
	secretsExcluded int
}

// CollectConfigSet preflights and copies one generation capture into only its
// configs/<captureId>/ root. The root is removed on every collection error.
func CollectConfigSet(plan ConfigSetCapturePlan, stagingRoot string) (_ *ConfigSetCollection, returnErr error) {
	if plan.Module == nil || plan.Set == nil || plan.Generation == nil || plan.Module.ID == "" || plan.Set.ID == "" || plan.Generation.ID == "" || plan.Instance.ID == "" {
		return nil, captureError(ConfigCaptureInvalidPlan, "module, set, generation, and instance identities are required")
	}
	if err := validateConfigSetCapturePlan(plan); err != nil {
		return nil, err
	}
	if plan.Generation.Capture == nil {
		return nil, captureError(ConfigCaptureInvalidPlan, "generation %q has no capture declaration", plan.Generation.ID)
	}
	if len(plan.Generation.Capture.RegistryKeys) > 0 || len(plan.Generation.Capture.RegistryValues) > 0 {
		return nil, &UnsupportedConfigCaptureError{
			Code:   ConfigCaptureRegistryUnsupported,
			Detail: fmt.Sprintf("generation registry capture is unsupported for %s/%s/%s", plan.Module.ID, plan.Set.ID, plan.Generation.ID),
		}
	}

	captureID := CaptureID(plan.Module.ID, plan.Set.ID, plan.Instance.ID)
	payloadRoot := path.Join("configs", captureID)
	preflight, err := preflightConfigCopies(plan)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(stagingRoot) == "" {
		return nil, captureError(ConfigCaptureUnsafePath, "staging root is empty")
	}
	if err := ensureNoLinksInExistingPath(stagingRoot); err != nil {
		return nil, capturePathError(err, "staging root %q", stagingRoot)
	}
	payloadHost, err := containedHostPath(stagingRoot, payloadRoot)
	if err != nil {
		return nil, capturePathError(err, "payload root %q", payloadRoot)
	}
	if _, err := os.Lstat(payloadHost); err == nil {
		return nil, captureError(ConfigCaptureDestinationCollision, "destination payload root already exists: %s", payloadRoot)
	} else if !os.IsNotExist(err) {
		return nil, captureError(ConfigCaptureIO, "inspect payload root %q: %v", payloadRoot, err)
	}
	if err := ensureNoLinksInExistingPath(filepath.Dir(payloadHost)); err != nil {
		return nil, capturePathError(err, "payload parent for %q", payloadRoot)
	}
	if err := os.MkdirAll(payloadHost, 0o755); err != nil {
		return nil, captureError(ConfigCaptureIO, "create payload root %q: %v", payloadRoot, err)
	}
	createdPayload := true
	defer func() {
		if returnErr != nil && createdPayload {
			_ = os.RemoveAll(payloadHost)
		}
	}()
	files := make([]string, 0, len(preflight.items))
	for _, item := range preflight.items {
		destination, err := containedHostPath(payloadHost, item.dest)
		if err != nil {
			return nil, capturePathError(err, "destination %q", item.dest)
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return nil, captureError(ConfigCaptureIO, "create destination directory for %q: %v", item.dest, err)
		}
		if err := ensureNoLinksInExistingPath(filepath.Dir(destination)); err != nil {
			return nil, capturePathError(err, "destination %q", item.dest)
		}
		if err := copyRegularFile(item.source, destination); err != nil {
			return nil, captureError(ConfigCaptureIO, "copy %q to %q: %v", item.source, item.dest, err)
		}
		files = append(files, item.dest)
	}
	createdPayload = false
	return &ConfigSetCollection{
		CaptureID:       captureID,
		PayloadRoot:     payloadRoot,
		Files:           files,
		FilesCollected:  len(files),
		SecretsExcluded: preflight.secretsExcluded,
	}, nil
}

func validateConfigSetCapturePlan(plan ConfigSetCapturePlan) error {
	if plan.Module.EffectiveSchemaVersion() != 2 || plan.Module.Config == nil {
		return captureError(ConfigCaptureInvalidPlan, "module %q is not a schema-v2 generation module", plan.Module.ID)
	}
	if plan.Instance.ModuleID != plan.Module.ID {
		return captureError(ConfigCaptureInvalidPlan, "instance %q belongs to module %q, not %q", plan.Instance.ID, plan.Instance.ModuleID, plan.Module.ID)
	}

	var ownedSet *modules.ConfigSetDef
	for index := range plan.Module.Config.Sets {
		candidate := &plan.Module.Config.Sets[index]
		if candidate.ID != plan.Set.ID {
			continue
		}
		if ownedSet != nil {
			return captureError(ConfigCaptureInvalidPlan, "module %q contains duplicate config set %q", plan.Module.ID, plan.Set.ID)
		}
		ownedSet = candidate
	}
	if ownedSet == nil || ownedSet != plan.Set {
		return captureError(ConfigCaptureInvalidPlan, "config set %q is not the pinned set owned by module %q", plan.Set.ID, plan.Module.ID)
	}

	var ownedGeneration *modules.GenerationDef
	for index := range ownedSet.Generations {
		candidate := &ownedSet.Generations[index]
		if candidate.ID != plan.Generation.ID {
			continue
		}
		if ownedGeneration != nil {
			return captureError(ConfigCaptureInvalidPlan, "config set %q contains duplicate generation %q", ownedSet.ID, plan.Generation.ID)
		}
		ownedGeneration = candidate
	}
	if ownedGeneration == nil || ownedGeneration != plan.Generation {
		return captureError(ConfigCaptureInvalidPlan, "generation %q is not the pinned generation owned by config set %q", plan.Generation.ID, ownedSet.ID)
	}
	computedFingerprint, err := modules.ComputeGenerationFingerprint(*ownedGeneration)
	if err != nil {
		return captureError(ConfigCaptureInvalidPlan, "fingerprint generation %q: %v", ownedGeneration.ID, err)
	}
	if !payloadHashPattern.MatchString(ownedGeneration.Fingerprint) || computedFingerprint != ownedGeneration.Fingerprint {
		return captureError(ConfigCaptureInvalidPlan, "generation %q fingerprint does not match its pinned definition", ownedGeneration.ID)
	}
	return nil
}

func preflightConfigCopies(plan ConfigSetCapturePlan) (*configCopyPreflight, error) {
	capture := plan.Generation.Capture
	excludeGlobs := capture.ExcludeGlobs
	var secretFiles []string
	if plan.Module.Secrets != nil {
		secretFiles = plan.Module.Secrets.Files
	}
	preflight := &configCopyPreflight{}
	for index, declaration := range capture.Files {
		source, err := modules.ExpandInstancePath(declaration.Source, plan.Instance, modules.HostPath)
		if err != nil {
			return nil, captureError(ConfigCaptureUnsafePath, "capture.files[%d].source: %v", index, err)
		}
		destinationExpanded, err := modules.ExpandInstancePath(declaration.Dest, plan.Instance, modules.PortableRelativePath)
		if err != nil {
			return nil, captureError(ConfigCaptureUnsafePath, "capture.files[%d].dest: %v", index, err)
		}
		destination, err := normalizePortablePath(destinationExpanded)
		if err != nil {
			return nil, captureError(ConfigCaptureUnsafePath, "capture.files[%d].dest: %v", index, err)
		}

		if matchesSecrets(source, secretFiles) {
			preflight.secretsExcluded++
			continue
		}
		if matchesExcludeGlobs(source, excludeGlobs) {
			continue
		}
		if err := ensureNoLinksInExistingPath(source); err != nil {
			if os.IsNotExist(err) && declaration.Optional {
				continue
			}
			return nil, capturePathError(err, "source %q", source)
		}
		info, err := os.Lstat(source)
		if err != nil {
			if os.IsNotExist(err) && declaration.Optional {
				continue
			}
			if os.IsNotExist(err) {
				return nil, captureError(ConfigCaptureMissingRequired, "missing required source: %s (module: %s)", source, plan.Module.ID)
			}
			return nil, captureError(ConfigCaptureIO, "inspect source %q: %v", source, err)
		}
		if isLinkOrReparse(info) {
			return nil, captureError(ConfigCaptureLinkUnsupported, "source %q is a link or reparse point", source)
		}
		if info.Mode().IsRegular() {
			if isOversizedInstaller(source, info.Size()) {
				continue
			}
			if err := addConfigCopyItem(preflight, source, destination); err != nil {
				return nil, err
			}
			continue
		}
		if !info.IsDir() {
			return nil, captureError(ConfigCaptureLinkUnsupported, "source %q is not a regular file or directory", source)
		}
		if err := preflightConfigDirectory(preflight, source, destination, excludeGlobs, secretFiles); err != nil {
			return nil, err
		}
	}
	sort.Slice(preflight.items, func(left, right int) bool { return preflight.items[left].dest < preflight.items[right].dest })
	return preflight, nil
}

func preflightConfigDirectory(preflight *configCopyPreflight, sourceRoot, destinationRoot string, excludeGlobs, secretFiles []string) error {
	return filepath.WalkDir(sourceRoot, func(source string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return captureError(ConfigCaptureIO, "walk source %q: %v", source, walkErr)
		}
		if source == sourceRoot {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return captureError(ConfigCaptureIO, "inspect source %q: %v", source, err)
		}
		if isLinkOrReparse(info) {
			return captureError(ConfigCaptureLinkUnsupported, "source %q is a link or reparse point", source)
		}
		if matchesSecrets(source, secretFiles) {
			preflight.secretsExcluded++
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if matchesExcludeGlobs(source, excludeGlobs) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(sourceRoot, source)
		if err != nil {
			return captureError(ConfigCaptureUnsafePath, "relative source path for %q: %v", source, err)
		}
		if isBloatDirSegment(relative) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return captureError(ConfigCaptureLinkUnsupported, "source %q is not a regular file", source)
		}
		if isOversizedInstaller(source, info.Size()) {
			return nil
		}
		destination, err := normalizePortablePath(path.Join(destinationRoot, filepath.ToSlash(relative)))
		if err != nil {
			return captureError(ConfigCaptureUnsafePath, "destination for %q: %v", source, err)
		}
		return addConfigCopyItem(preflight, source, destination)
	})
}

func addConfigCopyItem(preflight *configCopyPreflight, source, destination string) error {
	key := strings.ToLower(destination)
	for _, existingItem := range preflight.items {
		existing := existingItem.dest
		existingKey := strings.ToLower(existing)
		if key == existingKey || strings.HasPrefix(key, existingKey+"/") || strings.HasPrefix(existingKey, key+"/") {
			return captureError(ConfigCaptureDestinationCollision, "destination collision between %q and %q", existing, destination)
		}
	}
	preflight.items = append(preflight.items, configCopyItem{source: source, dest: destination})
	return nil
}

func copyRegularFile(source, destination string) error {
	if err := ensureNoLinksInExistingPath(source); err != nil {
		return err
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("source is not a regular file")
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = output.Close()
		}
	}()
	if _, err := io.Copy(output, input); err != nil {
		return err
	}
	if err := output.Sync(); err != nil {
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	closed = true
	return nil
}

func captureError(code, format string, args ...any) error {
	return &ConfigCaptureError{Code: code, Detail: fmt.Sprintf(format, args...)}
}

func capturePathError(err error, format string, args ...any) error {
	detail := fmt.Sprintf(format, args...)
	if errors.Is(err, errBundleLink) {
		return captureError(ConfigCaptureLinkUnsupported, "%s: %v", detail, err)
	}
	if errors.Is(err, errUnsafeBundlePath) {
		return captureError(ConfigCaptureUnsafePath, "%s: %v", detail, err)
	}
	return captureError(ConfigCaptureIO, "%s: %v", detail, err)
}
