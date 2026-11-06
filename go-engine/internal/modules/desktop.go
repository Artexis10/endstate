// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package modules

import "fmt"

func validateDesktopGeneration(mod *Module, generation *GenerationDef) error {
	hasDetector := false
	if mod.Config != nil {
		for _, detector := range mod.Config.InstanceDetectors {
			hasDetector = hasDetector || detector.Type == "desktop"
		}
	}
	desktopAllowed := mod.SourceSchemaVersion == 3 && mod.Platform == "linux" && mod.Realization == "endstate-restore" && hasDetector
	if generation.Capture != nil {
		targets := map[string]bool{}
		for _, decl := range generation.Capture.DesktopValues {
			if !desktopAllowed {
				return fmt.Errorf("desktop capture requires schema-v3 Linux endstate-restore")
			}
			if err := decl.Target.Validate(); err != nil {
				return err
			}
			if err := validatePortablePath(decl.Dest); err != nil {
				return err
			}
			if targets[decl.Target.Identity()] {
				return fmt.Errorf("duplicate desktop backing key")
			}
			targets[decl.Target.Identity()] = true
		}
	}
	for _, decl := range generation.Restore {
		if decl.Type != "desktop-value" {
			if decl.Desktop != nil {
				return fmt.Errorf("non-desktop restore has desktop fields")
			}
			continue
		}
		if !desktopAllowed || decl.Desktop == nil {
			return fmt.Errorf("desktop restore requires schema-v3 Linux endstate-restore target")
		}
		if err := decl.Desktop.Validate(); err != nil {
			return err
		}
		if decl.Source == "" || decl.Target != "" || decl.Key != "" || decl.ValueName != "" || decl.ValueType != "" || decl.Data != "" || decl.Pattern != "" || len(decl.Exclude) != 0 {
			return fmt.Errorf("desktop restore requires a payload source and no foreign target fields")
		}
	}
	return nil
}
