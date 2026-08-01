// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

// ProfileInspectResult is the read-only profile inventory returned by the
// profile inspect command wiring added separately from this model.
type ProfileInspectResult struct {
	Profile      ProfileInspectProfile       `json:"profile"`
	Apps         []ProfileInspectApp         `json:"apps"`
	SettingsApps []ProfileInspectSettingsApp `json:"settingsApps"`
	Warnings     []ProfileInspectWarning     `json:"warnings"`
	Summary      ProfileInspectSummary       `json:"summary"`
}

type ProfileInspectProfile struct {
	Name            *string `json:"name"`
	CapturedAt      *string `json:"capturedAt"`
	ManifestVersion int     `json:"manifestVersion"`
	ManifestPath    string  `json:"manifestPath"`
}

type ProfileInspectApp struct {
	ID            string   `json:"id"`
	ManifestAppID string   `json:"manifestAppId"`
	DisplayName   string   `json:"displayName"`
	PackageRefs   []string `json:"packageRefs"`
	HasSettings   bool     `json:"hasSettings"`
}

type ProfileInspectSettingsApp struct {
	ID                 string   `json:"id"`
	DisplayName        string   `json:"displayName"`
	AssociationStatus  string   `json:"associationStatus"`
	OwnerID            *string  `json:"ownerId"`
	AppID              *string  `json:"appId"`
	AppIncluded        bool     `json:"appIncluded"`
	PackageRefs        []string `json:"packageRefs"`
	ModuleIDs          []string `json:"moduleIds"`
	CandidateAppIDs    []string `json:"candidateAppIds"`
	CapturedEntryCount int      `json:"capturedEntryCount"`
}

type ProfileInspectWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Impact  string `json:"impact"`
}

type ProfileInspectSummary struct {
	AppCount                     int `json:"appCount"`
	SettingsRowCount             int `json:"settingsRowCount"`
	VerifiedSettingsAppCount     int `json:"verifiedSettingsAppCount"`
	UnidentifiedSettingsRowCount int `json:"unidentifiedSettingsRowCount"`
}

func runProfileInspect(path string) (interface{}, *envelope.Error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, envelope.NewError(envelope.ErrManifestValidationError, "profile inspect requires an extracted manifest path").
			WithRemediation("Provide one extracted .json, .jsonc, or .json5 manifest path.")
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext != ".json" && ext != ".jsonc" && ext != ".json5" {
		return nil, envelope.NewError(envelope.ErrManifestValidationError, "profile inspect accepts only extracted .json, .jsonc, or .json5 manifests").
			WithDetail(map[string]string{"path": path}).
			WithRemediation("Extract the profile first, then provide its manifest path.")
	}
	result, err := inspectProfile(path, defaultProfileInspectDeps())
	if err == nil {
		return result, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, envelope.NewError(envelope.ErrManifestNotFound, "The specified manifest file does not exist.").
			WithDetail(map[string]string{"path": path, "error": err.Error()})
	}
	if errors.Is(err, manifest.ErrValidation) {
		return nil, envelope.NewError(envelope.ErrManifestValidationError, "The manifest is invalid for profile inspection.").
			WithDetail(map[string]string{"path": path, "error": err.Error()}).
			WithRemediation("Correct the manifest validation error and try again.")
	}
	return nil, envelope.NewError(envelope.ErrManifestParseError, "Failed to parse the manifest file.").
		WithDetail(map[string]string{"path": path, "error": err.Error()}).
		WithRemediation("Ensure the extracted manifest is valid JSONC.")
}

type profileInspectDeps struct {
	loadManifest      func(string) (*manifest.Manifest, error)
	validateManifest  func(string) error
	preflightIncludes func(string) error
	readFile          func(string) ([]byte, error)
	loadCatalog       func(string) (map[string]*modules.Module, []modules.CatalogDiagnostic, error)
	verifySnapshot    func(string, manifest.ConfigCapture) error
}

func defaultProfileInspectDeps() profileInspectDeps {
	return profileInspectDeps{
		loadManifest:      manifest.LoadManifest,
		validateManifest:  validateProfileInspectManifest,
		preflightIncludes: preflightProfileInspectIncludes,
		readFile:          os.ReadFile,
		loadCatalog:       modules.GetCatalogWithDiagnostics,
		verifySnapshot:    bundle.VerifyModuleSnapshot,
	}
}

type inspectedModule struct {
	key           string
	label         string
	rawIDs        []string
	refs          []string
	snapshotRefs  []string
	catalogRefs   []string
	count         int
	captureIDs    map[string]struct{}
	snapshotLabel string
}

func inspectProfile(path string, deps profileInspectDeps) (*ProfileInspectResult, error) {
	if deps.loadManifest == nil {
		deps = defaultProfileInspectDeps()
	}
	if deps.preflightIncludes == nil {
		deps.preflightIncludes = preflightProfileInspectIncludes
	}
	if deps.readFile == nil {
		deps.readFile = os.ReadFile
	}
	if deps.verifySnapshot == nil {
		deps.verifySnapshot = bundle.VerifyModuleSnapshot
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if profileInspectPathContainsLink(absPath) {
		return nil, fmt.Errorf("%w: root manifest path contains a link or reparse hop", manifest.ErrValidation)
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%w: profile inspect requires a manifest file, not a directory", manifest.ErrValidation)
	}
	if err := deps.preflightIncludes(path); err != nil {
		var includeErr profileInspectIncludeError
		if errors.As(err, &includeErr) {
			return nil, fmt.Errorf("%w: %w", manifest.ErrValidation, err)
		}
		return nil, err
	}
	if deps.validateManifest != nil {
		if err := deps.validateManifest(path); err != nil {
			return nil, err
		}
	}
	mf, err := deps.loadManifest(path)
	if err != nil {
		return nil, err
	}
	metadata, warnings := readProfileInspectMetadata(filepath.Dir(path), deps.readFile)
	owned := collectProfileInspectOwnership(mf, metadata)
	applyProfileInspectExclusions(owned, mf.ExcludeConfigs)

	apps := profileInspectApps(profileInspectExcludedApps(mf.Apps, mf.Exclude))
	if len(owned) > 0 && deps.loadCatalog != nil {
		catalog, diagnostics, catalogErr := deps.loadCatalog(config.ResolveRepoRoot())
		if catalogErr != nil {
			warnings = append(warnings, profileInspectWarning("PROFILE_INSPECT_CATALOG", "Could not read the trusted module catalog.", "inventory_incomplete"))
		} else {
			for _, diagnostic := range diagnostics {
				warnings = append(warnings, profileInspectWarning("PROFILE_INSPECT_CATALOG", "A trusted module catalog entry could not be loaded.", "inventory_incomplete"))
				_ = diagnostic
			}
			enrichProfileInspectCatalog(owned, catalog)
		}
	}
	root := filepath.Dir(path)
	for _, capture := range mf.ConfigCaptures {
		module := owned[canonicalProfileInspectModule(capture.ModuleID)]
		if module == nil || strings.TrimSpace(capture.CaptureModule.SnapshotPath) == "" {
			continue
		}
		if err := deps.verifySnapshot(root, capture); err != nil {
			warnings = append(warnings, profileInspectWarning("PROFILE_INSPECT_SNAPSHOT", "A saved module snapshot could not be verified.", "inventory_incomplete"))
			continue
		}
		data, err := deps.readFile(filepath.Join(root, filepath.FromSlash(capture.CaptureModule.SnapshotPath)))
		if err != nil {
			warnings = append(warnings, profileInspectWarning("PROFILE_INSPECT_SNAPSHOT", "A verified module snapshot could not be read.", "inventory_incomplete"))
			continue
		}
		if fmt.Sprintf("%x", sha256.Sum256(data)) != capture.CaptureModule.ContentHash {
			warnings = append(warnings, profileInspectWarning("PROFILE_INSPECT_SNAPSHOT", "A saved module snapshot changed after verification.", "inventory_incomplete"))
			continue
		}
		snapshot, err := modules.ParseModuleJSON(data)
		if err != nil || canonicalProfileInspectModule(snapshot.ID) != module.key || snapshot.EffectiveSchemaVersion() != capture.CaptureModule.SchemaVersion {
			warnings = append(warnings, profileInspectWarning("PROFILE_INSPECT_SNAPSHOT", "A saved module snapshot does not match its captured provenance.", "inventory_incomplete"))
			continue
		}
		module.snapshotLabel = strings.TrimSpace(snapshot.DisplayName)
		module.snapshotRefs = sortedProfileInspectStrings(append(module.snapshotRefs, append(snapshot.Matches.Winget, snapshot.Matches.Chocolatey...)...))
	}

	settings := profileInspectSettings(owned, apps)
	for index := range settings {
		if settings[index].AssociationStatus == "included" {
			for appIndex := range apps {
				if apps[appIndex].ID == *settings[index].AppID {
					apps[appIndex].HasSettings = true
				}
			}
		}
	}
	name := optionalProfileInspectString(mf.Name)
	capturedAt := optionalProfileInspectString(mf.Captured)
	if capturedAt == nil {
		capturedAt = optionalProfileInspectString(metadata.CapturedAt)
	}
	if mf.Captured != "" && metadata.CapturedAt != "" && mf.Captured != metadata.CapturedAt {
		warnings = append(warnings, profileInspectWarning("PROFILE_INSPECT_TIMESTAMP", "Manifest and metadata capture timestamps differ; the manifest timestamp was used.", "diagnostic"))
	}
	sortProfileInspectApps(apps)
	sort.Slice(settings, func(i, j int) bool {
		return profileInspectLess(settings[i].DisplayName, settings[i].ID, settings[j].DisplayName, settings[j].ID)
	})
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].Code == warnings[j].Code {
			return warnings[i].Message < warnings[j].Message
		}
		return warnings[i].Code < warnings[j].Code
	})
	result := &ProfileInspectResult{Profile: ProfileInspectProfile{Name: name, CapturedAt: capturedAt, ManifestVersion: profileInspectManifestVersion(mf), ManifestPath: path}, Apps: apps, SettingsApps: settings, Warnings: warnings}
	result.Summary.AppCount = len(result.Apps)
	result.Summary.SettingsRowCount = len(result.SettingsApps)
	for _, row := range result.SettingsApps {
		if row.AssociationStatus == "included" || row.AssociationStatus == "not_in_profile" {
			result.Summary.VerifiedSettingsAppCount++
		} else {
			result.Summary.UnidentifiedSettingsRowCount++
		}
	}
	return result, nil
}

func validateProfileInspectManifest(path string) error {
	validation := manifest.ValidateProfile(path)
	if validation.Valid {
		return nil
	}
	if len(validation.Errors) == 0 {
		return errors.New("manifest validation failed")
	}
	first := validation.Errors[0]
	switch first.Code {
	case "FILE_NOT_FOUND":
		return os.ErrNotExist
	case "PARSE_ERROR", "MANIFEST_PARSE_ERROR":
		return errors.New(first.Message)
	default:
		return fmt.Errorf("%w: %s", manifest.ErrValidation, first.Message)
	}
}

func profileInspectPathContainsLink(path string) bool {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true
		}
		parent := filepath.Dir(current)
		if parent == current {
			return false
		}
	}
}

func collectProfileInspectOwnership(mf *manifest.Manifest, metadata bundle.BundleMetadata) map[string]*inspectedModule {
	owned := map[string]*inspectedModule{}
	add := func(raw string, count int) *inspectedModule {
		key := canonicalProfileInspectModule(raw)
		if key == "" {
			return nil
		}
		item := owned[key]
		if item == nil {
			item = &inspectedModule{key: key, captureIDs: map[string]struct{}{}}
			owned[key] = item
		}
		item.rawIDs = sortedProfileInspectStrings(append(item.rawIDs, raw))
		item.count += count
		return item
	}
	version := profileInspectManifestVersion(mf)
	if version == 2 {
		for _, capture := range mf.ConfigCaptures {
			item := add(capture.ModuleID, 0)
			if item == nil {
				continue
			}
			if _, seen := item.captureIDs[capture.CaptureID]; !seen {
				item.captureIDs[capture.CaptureID] = struct{}{}
				item.count += len(capture.PayloadManifest)
			}
			if capture.SourceInstance.Evidence != nil {
				item.refs = sortedProfileInspectStrings(append(item.refs, capture.SourceInstance.Evidence.Ref))
			}
		}
		for _, lane := range mf.LegacyConfigLanes {
			add(lane.ModuleID, 0)
		}
		for _, restore := range mf.Restore {
			if restore.LegacyCaptureID == "" {
				continue
			}
			for _, lane := range mf.LegacyConfigLanes {
				if lane.CaptureID == restore.LegacyCaptureID {
					add(lane.ModuleID, 1)
					break
				}
			}
		}
		return owned
	}
	for _, restore := range mf.Restore {
		if item := add(restore.FromModule, 0); item != nil && strings.TrimSpace(restore.FromModule) != "" {
			item.count++
			continue
		}
		if module := profileInspectModuleFromSource(restore.Source); module != "" {
			add(module, 1)
		}
	}
	for _, module := range mf.ConfigModules {
		add(module, 0)
	}
	for _, module := range metadata.ConfigModulesIncluded {
		add(module, 0)
	}
	return owned
}

func applyProfileInspectExclusions(owned map[string]*inspectedModule, excluded []string) {
	for _, raw := range excluded {
		delete(owned, canonicalProfileInspectModule(raw))
	}
}

func enrichProfileInspectCatalog(owned map[string]*inspectedModule, catalog map[string]*modules.Module) {
	for _, module := range catalog {
		item := owned[canonicalProfileInspectModule(module.ID)]
		if item == nil {
			continue
		}
		item.label = strings.TrimSpace(module.DisplayName)
		item.catalogRefs = sortedProfileInspectStrings(append(module.Matches.Winget, module.Matches.Chocolatey...))
	}
}

func profileInspectApps(entries []manifest.App) []ProfileInspectApp {
	counts := map[string]int{}
	apps := make([]ProfileInspectApp, 0, len(entries))
	for _, entry := range entries {
		key := strings.ToLower(strings.TrimSpace(entry.ID))
		if key == "" {
			key = "unnamed"
		}
		counts[key]++
		refs := make([]string, 0, len(entry.Refs))
		for _, ref := range entry.Refs {
			refs = append(refs, ref)
		}
		refs = sortedProfileInspectStrings(refs)
		label := strings.TrimSpace(entry.DisplayName)
		if label == "" && len(refs) > 0 {
			label = refs[0]
		}
		if label == "" {
			label = humanizeProfileInspect(entry.ID)
		}
		apps = append(apps, ProfileInspectApp{ID: fmt.Sprintf("app:%s:%d", key, counts[key]), ManifestAppID: entry.ID, DisplayName: label, PackageRefs: refs})
	}
	return apps
}

func profileInspectExcludedApps(entries []manifest.App, exclusions []string) []manifest.App {
	excluded := make(map[string]struct{}, len(exclusions))
	for _, ref := range exclusions {
		excluded[ref] = struct{}{}
	}
	filtered := make([]manifest.App, 0, len(entries))
	for _, entry := range entries {
		if _, found := excluded[entry.Refs["windows"]]; !found {
			filtered = append(filtered, entry)
		}
	}
	return filtered
}

func profileInspectSettings(owned map[string]*inspectedModule, apps []ProfileInspectApp) []ProfileInspectSettingsApp {
	keys := make([]string, 0, len(owned))
	for key := range owned {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	rows := make([]ProfileInspectSettingsApp, 0, len(keys))
	for _, key := range keys {
		item := owned[key]
		refs := selectedProfileInspectRefs(item)
		candidates := profileInspectCandidates(refs, apps)
		status := "unresolved"
		var owner, appID *string
		rowID := "settings:module:" + key
		if len(refs) > 0 && len(candidates) == 0 {
			status = "not_in_profile"
			absent := "package:" + strings.Join(profileInspectFolded(refs), "|")
			owner = &absent
			rowID = "settings:" + absent
		} else if len(candidates) == 1 {
			status = "included"
			owner = &candidates[0]
			appID = &candidates[0]
			rowID = "settings:" + candidates[0]
		} else if len(candidates) > 1 {
			status = "ambiguous"
		}
		label := item.snapshotLabel
		if label == "" {
			label = item.label
		}
		if label == "" && appID != nil {
			for _, app := range apps {
				if app.ID == *appID {
					label = app.DisplayName
					break
				}
			}
		}
		if label == "" && len(refs) > 0 {
			label = refs[0]
		}
		if label == "" {
			label = humanizeProfileInspect(key)
		}
		rows = append(rows, ProfileInspectSettingsApp{ID: rowID, DisplayName: label, AssociationStatus: status, OwnerID: owner, AppID: appID, AppIncluded: status == "included", PackageRefs: refs, ModuleIDs: item.rawIDs, CandidateAppIDs: candidates, CapturedEntryCount: item.count})
	}
	return groupProfileInspectSettings(rows)
}

func groupProfileInspectSettings(rows []ProfileInspectSettingsApp) []ProfileInspectSettingsApp {
	groups := map[string][]ProfileInspectSettingsApp{}
	order := []string{}
	for _, row := range rows {
		key := row.ID
		if row.AssociationStatus == "ambiguous" || row.AssociationStatus == "unresolved" {
			key += ":" + row.ID
		}
		if _, found := groups[key]; !found {
			order = append(order, key)
		}
		groups[key] = append(groups[key], row)
	}
	grouped := make([]ProfileInspectSettingsApp, 0, len(order))
	for _, key := range order {
		items := groups[key]
		row := items[0]
		for _, item := range items[1:] {
			row.ModuleIDs = append(row.ModuleIDs, item.ModuleIDs...)
			row.PackageRefs = append(row.PackageRefs, item.PackageRefs...)
			row.CapturedEntryCount += item.CapturedEntryCount
		}
		row.ModuleIDs = sortedProfileInspectStrings(row.ModuleIDs)
		row.PackageRefs = sortedProfileInspectStrings(row.PackageRefs)
		grouped = append(grouped, row)
	}
	return grouped
}

func profileInspectCandidates(refs []string, apps []ProfileInspectApp) []string {
	candidates := []string{}
	wanted := map[string]struct{}{}
	for _, ref := range refs {
		wanted[strings.ToLower(ref)] = struct{}{}
	}
	for _, app := range apps {
		for _, ref := range app.PackageRefs {
			if _, ok := wanted[strings.ToLower(ref)]; ok {
				candidates = append(candidates, app.ID)
				break
			}
		}
	}
	return sortedProfileInspectStrings(candidates)
}

func selectedProfileInspectRefs(item *inspectedModule) []string {
	if len(item.refs) > 0 {
		return item.refs
	}
	if len(item.snapshotRefs) > 0 {
		return item.snapshotRefs
	}
	return item.catalogRefs
}

type profileInspectIncludeError struct{ err error }

func (e profileInspectIncludeError) Error() string { return e.err.Error() }
func (e profileInspectIncludeError) Unwrap() error { return e.err }

func preflightProfileInspectIncludes(rootPath string) error {
	root, err := filepath.EvalSymlinks(rootPath)
	if err != nil {
		return err
	}
	rootDir, err := filepath.EvalSymlinks(filepath.Dir(root))
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(path string) error {
		if seen[path] {
			return nil
		}
		seen[path] = true
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var source struct {
			Includes []string `json:"includes"`
		}
		if err := json.Unmarshal(manifest.StripJsoncComments(data), &source); err != nil {
			return err
		}
		for _, include := range source.Includes {
			ext := strings.ToLower(filepath.Ext(include))
			if isAbsoluteProfileInspectInclude(include) || (ext != ".json" && ext != ".jsonc" && ext != ".json5") {
				return profileInspectIncludeError{fmt.Errorf("unsupported include %q", include)}
			}
			candidate := filepath.Clean(filepath.Join(filepath.Dir(path), include))
			lexicalRelative, err := filepath.Rel(rootDir, candidate)
			if err != nil || lexicalRelative == ".." || strings.HasPrefix(lexicalRelative, ".."+string(filepath.Separator)) {
				return profileInspectIncludeError{fmt.Errorf("include escapes root: %q", include)}
			}
			resolved, err := filepath.EvalSymlinks(candidate)
			if err != nil {
				return profileInspectIncludeError{err}
			}
			relative, err := filepath.Rel(rootDir, resolved)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
				return profileInspectIncludeError{fmt.Errorf("include escapes root: %q", include)}
			}
			if filepath.Clean(resolved) != filepath.Clean(filepath.Join(rootDir, lexicalRelative)) {
				return profileInspectIncludeError{fmt.Errorf("include contains a link or reparse hop: %q", include)}
			}
			info, err := os.Stat(resolved)
			if err != nil {
				return profileInspectIncludeError{err}
			}
			if info.IsDir() {
				return profileInspectIncludeError{fmt.Errorf("include is a directory: %q", include)}
			}
			if err := visit(resolved); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root)
}

func isAbsoluteProfileInspectInclude(value string) bool {
	if filepath.IsAbs(value) || strings.HasPrefix(value, `\\`) || strings.HasPrefix(value, "//") {
		return true
	}
	return len(value) >= 3 && ((value[0] >= 'A' && value[0] <= 'Z') || (value[0] >= 'a' && value[0] <= 'z')) && value[1] == ':' && (value[2] == '\\' || value[2] == '/')
}

func readProfileInspectMetadata(root string, readFile func(string) ([]byte, error)) (bundle.BundleMetadata, []ProfileInspectWarning) {
	data, err := readFile(filepath.Join(root, "metadata.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return bundle.BundleMetadata{}, []ProfileInspectWarning{profileInspectWarning("PROFILE_INSPECT_METADATA", "Profile metadata is missing.", "inventory_incomplete")}
		}
		return bundle.BundleMetadata{}, []ProfileInspectWarning{profileInspectWarning("PROFILE_INSPECT_METADATA", "Profile metadata could not be read.", "inventory_incomplete")}
	}
	var metadata bundle.BundleMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return bundle.BundleMetadata{}, []ProfileInspectWarning{profileInspectWarning("PROFILE_INSPECT_METADATA", "Profile metadata could not be parsed.", "inventory_incomplete")}
	}
	return metadata, []ProfileInspectWarning{}
}
func canonicalProfileInspectModule(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.TrimPrefix(value, "apps.")
}
func profileInspectModuleFromSource(source string) string {
	source = strings.TrimPrefix(strings.ReplaceAll(strings.TrimSpace(source), `\`, "/"), "./")
	parts := strings.Split(source, "/")
	if len(parts) > 2 && parts[0] == "configs" {
		return parts[1]
	}
	return ""
}
func profileInspectManifestVersion(mf *manifest.Manifest) int {
	if version, ok := mf.Version.(int); ok {
		return version
	}
	if version, ok := mf.Version.(float64); ok {
		return int(version)
	}
	return 1
}
func optionalProfileInspectString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
func profileInspectWarning(code, message, impact string) ProfileInspectWarning {
	return ProfileInspectWarning{Code: code, Message: message, Impact: impact}
}
func humanizeProfileInspect(value string) string {
	value = strings.ReplaceAll(strings.ReplaceAll(strings.TrimPrefix(value, "apps."), "-", " "), ".", " ")
	return strings.Title(value)
}
func sortedProfileInspectStrings(values []string) []string {
	seen := map[string]string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		folded := strings.ToLower(value)
		if existing, ok := seen[folded]; !ok || value < existing {
			seen[folded] = value
		}
	}
	result := make([]string, 0, len(seen))
	for _, value := range seen {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := strings.ToLower(result[i]), strings.ToLower(result[j])
		return left < right || left == right && result[i] < result[j]
	})
	return result
}
func profileInspectFolded(values []string) []string {
	folded := make([]string, len(values))
	for index, value := range values {
		folded[index] = strings.ToLower(value)
	}
	return folded
}
func profileInspectLess(leftLabel, leftID, rightLabel, rightID string) bool {
	left, right := strings.ToLower(leftLabel), strings.ToLower(rightLabel)
	return left < right || left == right && leftID < rightID
}
func sortProfileInspectApps(apps []ProfileInspectApp) {
	sort.Slice(apps, func(i, j int) bool {
		return profileInspectLess(apps[i].DisplayName, apps[i].ID, apps[j].DisplayName, apps[j].ID)
	})
}
