// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"fmt"

	"github.com/Artexis10/endstate/go-engine/internal/config"
)

const (
	DiagnosticMixedPlatformDeclarations = "mixed_platform_declarations"
	DiagnosticMissingPlatform           = "missing_platform"
	DiagnosticInvalidRealization        = "invalid_realization"
)

var supportedModulePlatforms = map[string]bool{"windows": true, "linux": true, "darwin": true}

func validateModuleV3(mod *Module, filePath string) error {
	if len(mod.Platforms) == 0 {
		return validationError(mod, filePath, DiagnosticMissingPlatform, "schema-v3 module must declare at least one platform variant")
	}
	if !matchCriteriaEmpty(mod.Matches) || len(mod.Verify) > 0 || len(mod.Restore) > 0 || mod.Capture != nil || mod.Secrets != nil || mod.Config != nil {
		return validationError(mod, filePath, DiagnosticMixedPlatformDeclarations, "schema-v3 module cannot mix platforms with top-level matches, capture, restore, verify, secrets, or config")
	}
	for platform, variant := range mod.Platforms {
		if !supportedModulePlatforms[platform] {
			return validationError(mod, filePath, DiagnosticMissingPlatform, "platform %q is not supported", platform)
		}
		if variant.Realization != "home-manager" && variant.Realization != "endstate-restore" {
			return validationError(mod, filePath, DiagnosticInvalidRealization, "platform %q must declare realization home-manager or endstate-restore", platform)
		}
		if err := validatePlatformHostPaths(platform, variant); err != nil {
			return validationError(mod, filePath, DiagnosticUnsafePath, "platform %q: %v", platform, err)
		}
		projected := projectPlatformVariant(mod, platform, variant)
		if err := validateModule(projected, filePath); err != nil {
			return validationError(mod, filePath, DiagnosticCode(err), "platform %q: %v", platform, err)
		}
	}
	return nil
}

func validatePlatformHostPaths(platform string, variant PlatformVariant) error {
	for index, path := range variant.Matches.PathExists {
		if err := config.ValidateEnginePath(path, platform); err != nil {
			return fmt.Errorf("matches.pathExists[%d]: %w", index, err)
		}
	}
	if variant.Capture != nil {
		for index, file := range variant.Capture.Files {
			if err := config.ValidateEnginePath(file.Source, platform); err != nil {
				return fmt.Errorf("capture.files[%d].source: %w", index, err)
			}
		}
	}
	for index, restore := range variant.Restore {
		if restore.Target == "" {
			continue
		}
		if err := config.ValidateEnginePath(restore.Target, platform); err != nil {
			return fmt.Errorf("restore[%d].target: %w", index, err)
		}
	}
	for index, verify := range variant.Verify {
		if verify.Path == "" {
			continue
		}
		if err := config.ValidateEnginePath(verify.Path, platform); err != nil {
			return fmt.Errorf("verify[%d].path: %w", index, err)
		}
	}
	if variant.Secrets != nil {
		for index, path := range variant.Secrets.Files {
			if err := config.ValidateEnginePath(path, platform); err != nil {
				return fmt.Errorf("secrets.files[%d]: %w", index, err)
			}
		}
	}
	return nil
}

func matchCriteriaEmpty(matches MatchCriteria) bool {
	return len(matches.Winget) == 0 && len(matches.Chocolatey) == 0 && len(matches.Exe) == 0 &&
		len(matches.UninstallDisplayName) == 0 && len(matches.PathExists) == 0
}

func schemaV3GenerationIdentity(moduleID, platform, setID, generationID string) string {
	return fmt.Sprintf("%s/%s/%s/%s", moduleID, platform, setID, generationID)
}
