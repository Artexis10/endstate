// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package configrestore

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/bundle"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/configvalidate"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
)

type transferredMember struct {
	source, target string
	actionIndex    int
}

func mappedTransferMembers(entries []resolvedRestore) ([]transferredMember, error) {
	var members []transferredMember
	for index, entry := range entries {
		if entry.sourceMissing || entry.source == "" || entry.registryClaim || entry.definition.Type == "delete-glob" {
			continue
		}
		if entry.definition.Type != "copy" || !entry.sourceInfo.IsDir() {
			members = append(members, transferredMember{canonicalPortablePath(entry.definition.Source), entry.target, index})
			continue
		}
		err := filepath.WalkDir(entry.source, func(candidate string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			suffix, err := filepath.Rel(entry.source, candidate)
			if err != nil {
				return err
			}
			if suffix != "." && copyPathExcluded(suffix, entry.definition.Exclude) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if err := validateMaterializePath(entry.boundary, candidate); err != nil {
				return err
			}
			info, err := os.Lstat(candidate)
			if err != nil {
				return err
			}
			if isLinkOrReparse(info) || (!info.IsDir() && !info.Mode().IsRegular()) {
				return fmt.Errorf("mapped source member is not a regular file or directory")
			}
			target := entry.target
			source := entry.definition.Source
			if suffix != "." {
				target = filepath.Join(target, suffix)
				source = filepath.Join(source, suffix)
			}
			members = append(members, transferredMember{canonicalPortablePath(source), target, index})
			return nil
		})
		if err != nil {
			return nil, newError(CodeUnsafePath, index, entry.target, err)
		}
	}
	return members, nil
}

func resolveMappedValidations(definitions []modules.ValidationDef, members []transferredMember) ([]configvalidate.ResolvedValidation, error) {
	resolved := make([]configvalidate.ResolvedValidation, 0, len(definitions))
	for index, definition := range definitions {
		target := ""
		count := 0
		for _, member := range members {
			if member.source == canonicalPortablePath(definition.Path) {
				target = member.target
				count++
			}
		}
		if count != 1 {
			return nil, &Error{Code: CodeValidationMapping, ActionIndex: -1, ValidationIndex: index, MappingCount: count, Err: fmt.Errorf("mapped validation %q must identify exactly one transferred member", definition.Path)}
		}
		resolved = append(resolved, configvalidate.ResolvedValidation{Definition: definition, HostPath: target})
	}
	return resolved, nil
}

func checkMappedTargetPolicy(request Request, instance modules.ConfigInstance, members []transferredMember, entries []resolvedRestore) error {
	scope, _ := modules.ResolveRegistrySecretScope(&modules.Module{Secrets: request.Plan.TargetSecrets})
	for index, entry := range entries {
		if entry.registry != nil && scope.ExcludesValue(entry.registry.Key, entry.registry.ValueName) {
			return newError(CodeUnsafePath, index, entry.target, fmt.Errorf("mapped registry value matches target secrets policy"))
		}
	}

	caseSensitive := request.Plan.Resolution.TargetPlatform != "windows"
	var secrets []string
	if request.Plan.TargetSecrets != nil {
		for _, authored := range request.Plan.TargetSecrets.Files {
			kind, _ := modules.ClassifySecretCoordinate(authored)
			if kind != modules.SecretCoordinateFilesystem {
				continue
			}
			resolved := authored
			// Host separators and expansion tokens mark rooted patterns; basename globs stay relative.
			if strings.ContainsAny(authored, `/\$%~`) || filepath.IsAbs(authored) {
				var err error
				resolved, err = resolveMappedSecretPattern(authored, instance, request.Boundary)
				if err != nil {
					return newError(CodeUnsafePath, -1, authored, err)
				}
			}
			secrets = append(secrets, resolved)
		}
	}
	capture := request.Plan.TargetGenerationDef.Capture
	type captureRoot struct {
		path     string
		excluded bool
	}
	var roots []captureRoot
	if capture != nil {
		for _, file := range capture.Files {
			root, err := resolveMappedPolicyPath(file.Source, instance, request.Boundary, false)
			if err != nil {
				return newError(CodeUnsafePath, -1, file.Source, err)
			}
			excluded, err := mappedCaptureExcluded(filepath.Base(root), capture.ExcludeGlobs)
			if err != nil {
				return newError(CodeUnsafePath, -1, file.Source, err)
			}
			roots = append(roots, captureRoot{root, excluded})
		}
	}
	for _, member := range members {
		forbidden, err := bundle.ResolvedPathMatchesSecrets(member.target, secrets, caseSensitive)
		if err != nil {
			return newError(CodeUnsafePath, member.actionIndex, member.target, err)
		}
		if forbidden {
			return newError(CodeUnsafePath, member.actionIndex, member.target, fmt.Errorf("mapped member matches target secrets policy"))
		}
		for _, root := range roots {
			relative, err := filepath.Rel(root.path, member.target)
			if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
				continue
			}
			forbidden := root.excluded
			if !forbidden && relative != "." {
				forbidden, err = mappedCaptureExcluded(filepath.ToSlash(relative), capture.ExcludeGlobs)
				if err != nil {
					return newError(CodeUnsafePath, member.actionIndex, member.target, err)
				}
			}
			if forbidden {
				return newError(CodeUnsafePath, member.actionIndex, member.target, fmt.Errorf("mapped member matches target capture exclusions"))
			}
		}
	}
	return nil
}

func mappedCaptureExcluded(value string, patterns []string) (bool, error) {
	for _, pattern := range patterns {
		matched, err := bundle.ConfigPathMatchesExcludeGlob(value, pattern)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func resolveMappedPolicyPath(authored string, instance modules.ConfigInstance, boundary HostBoundary, pattern bool) (string, error) {
	if boundary != nil {
		if pattern {
			if resolver, ok := boundary.(interface {
				ResolveHostPattern(string, modules.ConfigInstance) (string, error)
			}); ok {
				return resolver.ResolveHostPattern(authored, instance)
			}
		}
		return boundary.ResolveHostPath(authored, instance)
	}
	return modules.ExpandInstancePath(authored, instance, modules.HostPath)
}

func validateMappedJournalTransfers(lineage JournalLineage, actions []JournalAction, validations []JournalValidation) error {
	if lineage.MappingID == "" {
		return nil
	}
	for _, action := range actions {
		if action.Kind != ActionCopy && action.Kind != ActionWriteFile {
			continue
		}
		if len(action.TransferredPaths) == 0 {
			return fmt.Errorf("mapping action must record transferred members")
		}
		seen := map[string]bool{}
		for _, member := range action.TransferredPaths {
			if member != "." {
				if err := validateJournalValidationPath(member); err != nil {
					return err
				}
			}
			if seen[member] {
				return fmt.Errorf("duplicate transferred member")
			}
			seen[member] = true
		}
	}
	for _, validation := range validations {
		count := 0
		for _, action := range actions {
			if !journalValidationMapsToAction(validation, action) {
				continue
			}
			relative, err := filepath.Rel(action.Target, validation.HostPath)
			if err != nil {
				continue
			}
			for _, member := range action.TransferredPaths {
				if filepath.ToSlash(relative) == member {
					count++
					break
				}
			}
		}
		if count != 1 {
			return fmt.Errorf("mapped journal validation must identify exactly one transferred member")
		}
	}
	return nil
}

func resolveMappedSecretPattern(authored string, instance modules.ConfigInstance, boundary HostBoundary) (string, error) {
	coordinate, suffix, ok := config.SplitEnginePath(authored)
	if !ok && strings.HasPrefix(authored, "${instance.root}") {
		coordinate = "${instance.root}"
		suffix = strings.TrimPrefix(authored, coordinate)
		ok = true
	}
	if !ok {
		return resolveMappedPolicyPath(authored, instance, boundary, true)
	}
	root, err := resolveMappedPolicyPath(coordinate, instance, boundary, true)
	if err != nil {
		return "", err
	}
	quoted := filepath.ToSlash(config.QuoteGlobLiteral(root))
	// Doublestar also reserves braces; resolved coordinate roots are literal host data.
	quoted = strings.NewReplacer("{", "[{]", "}", "[}]").Replace(quoted)
	suffix = strings.TrimLeft(strings.ReplaceAll(suffix, `\`, "/"), "/")
	if suffix == "" {
		return quoted, nil
	}
	return strings.TrimSuffix(quoted, "/") + "/" + suffix, nil
}
