// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package linuxparity builds the deterministic applicability matrix between
// the existing Windows settings catalog and reviewed Linux authorities.
package linuxparity

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/hmregistry"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/packagecatalog"
)

const SchemaVersion = 1

type Status string

const (
	StatusSupported       Status = "supported"
	StatusNotApplicable   Status = "not-applicable"
	StatusAdapterRequired Status = "adapter-required"
	StatusReviewRequired  Status = "review-required"
)

type Evidence string

const (
	EvidenceLinuxVariant       Evidence = "linux-module-variant"
	EvidenceHomeManagerAdapter Evidence = "home-manager-adapter"
	EvidenceHomeManagerProgram Evidence = "home-manager-program"
	EvidencePackageCatalog     Evidence = "package-catalog"
	EvidenceManualReview       Evidence = "manual-review"
)

type Review struct {
	ModuleID string
	Status   Status
	Reason   string
}

type Row struct {
	ModuleID           string     `json:"moduleId"`
	DisplayName        string     `json:"displayName"`
	Status             Status     `json:"status"`
	Evidence           []Evidence `json:"evidence"`
	HomeManagerProgram string     `json:"homeManagerProgram,omitempty"`
	Reason             string     `json:"reason,omitempty"`
	WindowsCapture     bool       `json:"windowsCapture"`
	WindowsRestore     bool       `json:"windowsRestore"`
	LinuxCapture       bool       `json:"linuxCapture"`
	LinuxRestore       bool       `json:"linuxRestore"`
}

type Counts struct {
	Total           int `json:"total"`
	Supported       int `json:"supported"`
	NotApplicable   int `json:"notApplicable"`
	AdapterRequired int `json:"adapterRequired"`
	ReviewRequired  int `json:"reviewRequired"`
}

type Matrix struct {
	SchemaVersion int    `json:"schemaVersion"`
	Ready         bool   `json:"ready"`
	Counts        Counts `json:"counts"`
	Rows          []Row  `json:"rows"`
}

// Build joins four independent authorities without treating any one source as
// proof of settings parity. Package or Home Manager presence establishes a
// Linux counterpart; only an executable Linux module or reviewed supported
// adapter establishes settings support.
func Build(moduleCatalog map[string]*modules.Module, packages []*packagecatalog.Entry, registry *hmregistry.Registry, reviews ...Review) (Matrix, error) {
	packageModules := make(map[string]bool)
	for _, entry := range packages {
		if entry != nil && entry.ConfigModule != "" {
			packageModules[entry.ConfigModule] = true
		}
	}

	programs := make(map[string]bool)
	if registry != nil {
		for _, program := range registry.Programs {
			programs[program] = true
		}
	}
	adapterByModule := make(map[string]hmregistry.Entry)
	if registry != nil {
		for _, entry := range registry.Entries {
			moduleID := entry.ModuleID
			if moduleID == "" {
				candidate := "apps." + entry.ID
				if moduleCatalog[candidate] != nil {
					moduleID = candidate
				}
			}
			if moduleID == "" || moduleCatalog[moduleID] == nil {
				continue
			}
			if previous, duplicate := adapterByModule[moduleID]; duplicate {
				return Matrix{}, fmt.Errorf("module %q has multiple Home Manager adapters %q and %q", moduleID, previous.ID, entry.ID)
			}
			adapterByModule[moduleID] = entry
		}
	}

	reviewByModule := make(map[string]Review, len(reviews))
	for _, review := range reviews {
		if moduleCatalog[review.ModuleID] == nil {
			return Matrix{}, fmt.Errorf("Linux applicability review names unknown module %q", review.ModuleID)
		}
		if _, duplicate := reviewByModule[review.ModuleID]; duplicate {
			return Matrix{}, fmt.Errorf("duplicate Linux applicability review for %q", review.ModuleID)
		}
		if review.Status != StatusNotApplicable || strings.TrimSpace(review.Reason) == "" {
			return Matrix{}, fmt.Errorf("Linux applicability review for %q must be a reasoned not-applicable disposition", review.ModuleID)
		}
		reviewByModule[review.ModuleID] = review
	}

	moduleIDs := make([]string, 0, len(moduleCatalog))
	for id, module := range moduleCatalog {
		if module == nil {
			continue
		}
		moduleIDs = append(moduleIDs, id)
	}
	sort.Strings(moduleIDs)

	matrix := Matrix{SchemaVersion: SchemaVersion, Ready: true, Rows: make([]Row, 0, len(moduleIDs))}
	for _, moduleID := range moduleIDs {
		module := moduleCatalog[moduleID]
		row := Row{
			ModuleID: moduleID, DisplayName: module.DisplayName,
			WindowsCapture: moduleCaptures(module, "windows"),
			WindowsRestore: moduleRestores(module, "windows"),
		}
		suffix := strings.TrimPrefix(moduleID, "apps.")
		_, hasLinuxVariant := module.Platforms["linux"]
		row.LinuxCapture = hasLinuxVariant && moduleCaptures(module, "linux")
		row.LinuxRestore = hasLinuxVariant && moduleRestores(module, "linux")
		linuxVariantSupportsParity := hasLinuxVariant &&
			(!row.WindowsCapture || row.LinuxCapture) &&
			(!row.WindowsRestore || row.LinuxRestore)
		adapter, hasAdapter := adapterByModule[moduleID]
		hasProgram := programs[suffix]
		hasPackage := packageModules[moduleID]

		if hasLinuxVariant {
			row.Evidence = append(row.Evidence, EvidenceLinuxVariant)
		}
		if hasAdapter {
			row.Evidence = append(row.Evidence, EvidenceHomeManagerAdapter)
			row.HomeManagerProgram = adapter.Program
		}
		if hasProgram {
			row.Evidence = append(row.Evidence, EvidenceHomeManagerProgram)
			if row.HomeManagerProgram == "" {
				row.HomeManagerProgram = suffix
			}
		}
		if hasPackage {
			row.Evidence = append(row.Evidence, EvidencePackageCatalog)
		}

		switch {
		case linuxVariantSupportsParity:
			row.Status = StatusSupported
		case hasAdapter && adapter.Disposition != hmregistry.Excluded:
			row.Status = StatusSupported
		case !row.WindowsCapture && !row.WindowsRestore && hasPackage:
			// A Windows module with no settings surface promises installation
			// only. A reviewed Linux package mapping already matches it.
			row.Status = StatusSupported
		case reviewByModule[moduleID].ModuleID != "":
			review := reviewByModule[moduleID]
			row.Status = review.Status
			row.Reason = review.Reason
			row.Evidence = append(row.Evidence, EvidenceManualReview)
		case hasAdapter:
			row.Status = StatusAdapterRequired
			row.Reason = adapter.ExclusionReason
		case hasLinuxVariant:
			row.Status = StatusAdapterRequired
			row.Reason = "Linux variant does not cover the Windows capture and restore surface."
		case hasProgram || hasPackage:
			row.Status = StatusAdapterRequired
		default:
			row.Status = StatusReviewRequired
		}

		matrix.Rows = append(matrix.Rows, row)
		matrix.Counts.Total++
		switch row.Status {
		case StatusSupported:
			matrix.Counts.Supported++
		case StatusNotApplicable:
			matrix.Counts.NotApplicable++
		case StatusAdapterRequired:
			matrix.Counts.AdapterRequired++
			matrix.Ready = false
		case StatusReviewRequired:
			matrix.Counts.ReviewRequired++
			matrix.Ready = false
		}
	}
	return matrix, nil
}

func moduleCaptures(module *modules.Module, platform string) bool {
	if module == nil {
		return false
	}
	if variant, ok := module.Platforms[platform]; ok {
		return captureDoesWork(variant.Capture) || configDoesCapture(variant.Config)
	}
	return captureDoesWork(module.Capture) || configDoesCapture(module.Config)
}

func moduleRestores(module *modules.Module, platform string) bool {
	if module == nil {
		return false
	}
	if variant, ok := module.Platforms[platform]; ok {
		return len(variant.Restore) > 0 || configDoesRestore(variant.Config)
	}
	return len(module.Restore) > 0 || configDoesRestore(module.Config)
}

func captureDoesWork(capture *modules.CaptureDef) bool {
	return capture != nil && (len(capture.Files) > 0 || len(capture.RegistryKeys) > 0 || len(capture.RegistryValues) > 0)
}

func configDoesCapture(config *modules.ConfigDef) bool {
	if config == nil {
		return false
	}
	for _, set := range config.Sets {
		for _, generation := range set.Generations {
			if captureDoesWork(generation.Capture) {
				return true
			}
		}
	}
	return false
}

func configDoesRestore(config *modules.ConfigDef) bool {
	if config == nil {
		return false
	}
	for _, set := range config.Sets {
		for _, generation := range set.Generations {
			if len(generation.Restore) > 0 {
				return true
			}
		}
	}
	return false
}

// RequireReady is the release gate. Its diagnostics are bounded so a stale
// catalog remains useful in CI without dumping hundreds of rows.
func (matrix Matrix) RequireReady() error {
	if matrix.Ready {
		return nil
	}
	const diagnosticLimit = 20
	blocked := make([]string, 0, diagnosticLimit)
	total := 0
	for _, row := range matrix.Rows {
		if row.Status != StatusAdapterRequired && row.Status != StatusReviewRequired {
			continue
		}
		total++
		if len(blocked) < diagnosticLimit {
			blocked = append(blocked, fmt.Sprintf("%s (%s)", row.ModuleID, row.Status))
		}
	}
	detail := strings.Join(blocked, ", ")
	if total > len(blocked) {
		detail += fmt.Sprintf(", and %d more", total-len(blocked))
	}
	return fmt.Errorf("Linux settings parity is not ready: %s", detail)
}
