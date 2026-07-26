// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

// Package catalogplan resolves one tracked bundle into revision-bound catalog
// module actions without selecting any package or application intent.
package catalogplan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/validationmatrix"
)

var stableSlug = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// Bundle identifies the one tracked bundle resolved by the command. Path is
// always repository-relative so host paths cannot enter the result contract.
type Bundle struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Hash    string `json:"hash"`
	Version int    `json:"version"`
}

// Action is one ordered module-resolution result. It deliberately contains no
// package reference, matcher, executable, or verifier result.
type Action struct {
	BundleID                string `json:"bundleId"`
	BundleHash              string `json:"bundleHash"`
	ModuleID                string `json:"moduleId"`
	ModuleRevision          string `json:"moduleRevision"`
	ModuleSchemaVersion     int    `json:"moduleSchemaVersion"`
	ValidationHash          string `json:"validationHash"`
	ValidationScenarioCount int    `json:"validationScenarioCount"`
	Status                  string `json:"status"`
	Skipped                 bool   `json:"skipped"`
}

// Result is the stable catalog-plan data projection. Proof is intentionally
// limited to catalog: resolving memberships proves neither installation nor
// configuration behavior.
type Result struct {
	Proof           string    `json:"proof"`
	Bundle          Bundle    `json:"bundle"`
	MembershipCount int       `json:"membershipCount"`
	ActionCount     int       `json:"actionCount"`
	Actions         []Action  `json:"actions"`
	Failures        []Failure `json:"failures,omitempty"`
}

// Failure describes one safe, machine-readable failed membership or catalog
// record. It intentionally excludes filesystem paths and host-specific error
// text so callers can surface it in envelopes and evidence.
type Failure struct {
	ModuleID string `json:"moduleId"`
	Reason   string `json:"reason"`
}

// ResolutionError preserves the stable failure identity while allowing callers
// to keep the human-facing envelope message free of host paths.
type ResolutionError struct {
	Failure Failure
}

func (err *ResolutionError) Error() string {
	return err.Failure.Reason
}

type bundleDocument struct {
	Version int
	ID      string
	Name    string
	Modules []string
}

// Resolve reads and validates one tracked bundle using the canonical
// production module and validation-sidecar catalog. It performs no mutation,
// driver construction, network access, or inventory probing.
func Resolve(root, bundlePath string, now time.Time) (*Result, error) {
	path, relativePath, err := resolveBundlePath(root, bundlePath)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tracked bundle")
	}
	document, err := parseBundleJSONC(data)
	if err != nil {
		return nil, err
	}
	if err := validateBundle(document, filepath.Base(path)); err != nil {
		return nil, err
	}

	bundleHash := sha256Hex(normalizeLineEndings(data))
	result := &Result{
		Proof:           "catalog",
		Bundle:          Bundle{ID: document.ID, Name: document.Name, Path: relativePath, Hash: bundleHash, Version: document.Version},
		MembershipCount: len(document.Modules),
		Actions:         make([]Action, 0, len(document.Modules)),
	}
	catalog, err := validationmatrix.LoadCatalog(root, now)
	if err != nil {
		failure := catalogFailure(err, document.Modules)
		result.Failures = append(result.Failures, failure)
		return result, &ResolutionError{Failure: failure}
	}
	for _, slug := range document.Modules {
		moduleID := "apps." + slug
		mod, exists := catalog.Modules[moduleID]
		if !exists {
			failure := Failure{ModuleID: moduleID, Reason: "missing_module"}
			result.Failures = append(result.Failures, failure)
			return result, &ResolutionError{Failure: failure}
		}
		record, exists := catalog.Records[moduleID]
		if !exists {
			failure := Failure{ModuleID: moduleID, Reason: "missing_validation_sidecar"}
			result.Failures = append(result.Failures, failure)
			return result, &ResolutionError{Failure: failure}
		}
		validationHash := sha256Hex(normalizeLineEndings(record.SourceSnapshot()))
		result.Actions = append(result.Actions, Action{
			BundleID:                document.ID,
			BundleHash:              bundleHash,
			ModuleID:                moduleID,
			ModuleRevision:          mod.Revision,
			ModuleSchemaVersion:     mod.EffectiveSchemaVersion(),
			ValidationHash:          validationHash,
			ValidationScenarioCount: len(record.Synthetic.Scenarios),
			Status:                  "resolved",
			Skipped:                 false,
		})
	}
	result.ActionCount = len(result.Actions)
	if result.MembershipCount == 0 || result.ActionCount != result.MembershipCount {
		return nil, fmt.Errorf("catalog plan did not resolve every bundle membership")
	}
	return result, nil
}

func catalogFailure(err error, memberships []string) Failure {
	failure := Failure{Reason: validationmatrix.ErrorCode(err)}
	if failure.Reason == "" {
		failure.Reason = "invalid_catalog"
	}
	var validationError *validationmatrix.ValidationError
	if errors.As(err, &validationError) {
		failure.ModuleID = validationError.ModuleID
	}
	if failure.ModuleID == "" && len(memberships) > 0 {
		failure.ModuleID = "apps." + memberships[0]
	}
	return failure
}

func resolveBundlePath(root, input string) (string, string, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(input) == "" || hasTraversal(input) {
		return "", "", fmt.Errorf("bundle path must be a tracked immediate child of bundles")
	}
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("resolve catalog root")
	}
	bundlesRoot := filepath.Join(cleanRoot, "bundles")
	path := input
	if !filepath.IsAbs(path) {
		path = filepath.Join(cleanRoot, path)
	}
	path = filepath.Clean(path)
	if filepath.Dir(path) != filepath.Clean(bundlesRoot) || filepath.Ext(path) != ".jsonc" {
		return "", "", fmt.Errorf("bundle path must be a tracked immediate child of bundles")
	}
	if err := rejectLinkOrNonRegular(bundlesRoot); err != nil {
		return "", "", err
	}
	if err := rejectLinkOrNonRegular(path); err != nil {
		return "", "", err
	}
	return path, filepath.ToSlash(filepath.Join("bundles", filepath.Base(path))), nil
}

func hasTraversal(path string) bool {
	for _, part := range strings.FieldsFunc(filepath.ToSlash(path), func(r rune) bool { return r == '/' }) {
		if part == ".." {
			return true
		}
	}
	return false
}

func rejectLinkOrNonRegular(path string) error {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || isReparsePoint(path) || !info.Mode().IsRegular() && !info.IsDir() {
		return fmt.Errorf("tracked bundle path is not a regular file")
	}
	return nil
}

func parseBundleJSONC(data []byte) (bundleDocument, error) {
	decoder := json.NewDecoder(bytes.NewReader(manifest.StripJsoncComments(data)))
	token, err := decoder.Token()
	if err != nil {
		return bundleDocument{}, fmt.Errorf("parse bundle JSON")
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return bundleDocument{}, fmt.Errorf("bundle must be a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return bundleDocument{}, fmt.Errorf("parse bundle JSON")
		}
		name, ok := token.(string)
		if !ok {
			return bundleDocument{}, fmt.Errorf("bundle object field is invalid")
		}
		if _, exists := fields[name]; exists {
			return bundleDocument{}, fmt.Errorf("bundle field %q is duplicated", name)
		}
		if name != "version" && name != "id" && name != "name" && name != "modules" {
			return bundleDocument{}, fmt.Errorf("bundle field %q is not allowed", name)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return bundleDocument{}, fmt.Errorf("parse bundle field %q", name)
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return bundleDocument{}, fmt.Errorf("parse bundle JSON")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return bundleDocument{}, fmt.Errorf("bundle has trailing JSON values")
	}
	for _, field := range []string{"version", "id", "name", "modules"} {
		if _, exists := fields[field]; !exists {
			return bundleDocument{}, fmt.Errorf("bundle field %q is required", field)
		}
	}
	var document bundleDocument
	if err := json.Unmarshal(fields["version"], &document.Version); err != nil {
		return bundleDocument{}, fmt.Errorf("bundle version is invalid")
	}
	if err := json.Unmarshal(fields["id"], &document.ID); err != nil {
		return bundleDocument{}, fmt.Errorf("bundle id is invalid")
	}
	if err := json.Unmarshal(fields["name"], &document.Name); err != nil {
		return bundleDocument{}, fmt.Errorf("bundle name is invalid")
	}
	if err := json.Unmarshal(fields["modules"], &document.Modules); err != nil {
		return bundleDocument{}, fmt.Errorf("bundle modules are invalid")
	}
	return document, nil
}

func validateBundle(document bundleDocument, filename string) error {
	if document.Version != 1 {
		return fmt.Errorf("bundle version must be 1")
	}
	if !stableSlug.MatchString(document.ID) {
		return fmt.Errorf("bundle id must be a canonical slug")
	}
	if strings.TrimSpace(document.Name) == "" || document.Name != strings.TrimSpace(document.Name) {
		return fmt.Errorf("bundle name must be nonblank and canonical")
	}
	if document.ID != strings.TrimSuffix(filename, filepath.Ext(filename)) {
		return fmt.Errorf("bundle id must match its filename")
	}
	if len(document.Modules) == 0 {
		return fmt.Errorf("bundle modules must not be empty")
	}
	seen := make(map[string]struct{}, len(document.Modules))
	for _, slug := range document.Modules {
		if !stableSlug.MatchString(slug) {
			return fmt.Errorf("bundle module reference must be a bare canonical slug")
		}
		if _, exists := seen[slug]; exists {
			return fmt.Errorf("bundle module reference %q is duplicated", slug)
		}
		seen[slug] = struct{}{}
	}
	return nil
}

func normalizeLineEndings(data []byte) []byte {
	return bytes.ReplaceAll(data, []byte("\r\n"), []byte("\n"))
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
