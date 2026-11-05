// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// CanonicalizeModuleJSON parses JSONC and returns compact deterministic JSON.
// Object keys are ordered by encoding/json; comments, property order,
// whitespace, and line endings therefore do not affect the result.
func CanonicalizeModuleJSON(data []byte) ([]byte, error) {
	clean := manifest.StripJsoncComments(data)
	decoder := json.NewDecoder(bytes.NewReader(clean))
	decoder.UseNumber()

	var parsed any
	if err := decoder.Decode(&parsed); err != nil {
		return nil, fmt.Errorf("parse module JSON: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return nil, err
	}
	removeLoaderFields(parsed)

	canonical, err := json.Marshal(parsed)
	if err != nil {
		return nil, fmt.Errorf("canonicalize module JSON: %w", err)
	}
	return canonical, nil
}

// ComputeModuleRevision returns the lowercase SHA-256 of canonical parsed
// module JSON.
func ComputeModuleRevision(data []byte) (string, error) {
	canonical, err := CanonicalizeModuleJSON(data)
	if err != nil {
		return "", err
	}
	return sha256Hex(canonical), nil
}

// ComputeGenerationFingerprint returns the stable identity hash for one
// generation definition. Accepted historical fingerprints are compatibility
// data, not part of the generation's meaning, and are deliberately excluded.
func ComputeGenerationFingerprint(generation GenerationDef) (string, error) {
	generation.AcceptsSourceFingerprints = nil
	generation.Fingerprint = ""
	canonical, err := json.Marshal(generation)
	if err != nil {
		return "", fmt.Errorf("canonicalize generation %q: %w", generation.ID, err)
	}
	return sha256Hex(canonical), nil
}

// ParseModuleJSON parses one JSONC module and computes its revision and
// generation fingerprints. Validation is intentionally separate so callers
// can inspect parsed declarations and receive structured validation errors.
func ParseModuleJSON(data []byte) (*Module, error) {
	clean := manifest.StripJsoncComments(data)
	var mod Module
	if err := decodeModuleJSON(clean, &mod); err != nil {
		return nil, err
	}
	// Schema v1 is the established legacy format and retains its permissive
	// loader for backward compatibility. Schema v2 is a declarative engine
	// contract: reject misspelled or unsupported fields instead of ignoring
	// intent the engine cannot execute.
	if mod.EffectiveSchemaVersion() == 2 || mod.EffectiveSchemaVersion() == 3 {
		if err := decodeStrictModuleJSON(clean, &mod); err != nil {
			return nil, err
		}
	}

	canonical, err := CanonicalizeModuleJSON(data)
	if err != nil {
		return nil, err
	}
	mod.canonicalSnapshot = append([]byte(nil), canonical...)
	mod.Revision = sha256Hex(canonical)
	mod.Unversioned = mod.EffectiveSchemaVersion() == 1

	if mod.Config != nil {
		parsedFingerprints, err := computeParsedGenerationFingerprints(canonical)
		if err != nil {
			return nil, err
		}
		for setIndex := range mod.Config.Sets {
			for generationIndex := range mod.Config.Sets[setIndex].Generations {
				generation := &mod.Config.Sets[setIndex].Generations[generationIndex]
				if setIndex < len(parsedFingerprints) && generationIndex < len(parsedFingerprints[setIndex]) {
					generation.Fingerprint = parsedFingerprints[setIndex][generationIndex]
					continue
				}
				fingerprint, err := ComputeGenerationFingerprint(*generation)
				if err != nil {
					return nil, err
				}
				generation.Fingerprint = fingerprint
			}
		}
	}
	for platform, variant := range mod.Platforms {
		if err := setPlatformGenerationFingerprints(&mod, platform, &variant); err != nil {
			return nil, err
		}
		mod.Platforms[platform] = variant
	}
	return &mod, nil
}

func computeParsedGenerationFingerprints(canonicalModule []byte) ([][]string, error) {
	var module map[string]any
	if err := decodeModuleJSON(canonicalModule, &module); err != nil {
		return nil, fmt.Errorf("read canonical module generations: %w", err)
	}
	config, ok := module["config"].(map[string]any)
	if !ok {
		return nil, nil
	}
	sets, ok := config["sets"].([]any)
	if !ok {
		return nil, nil
	}
	fingerprints := make([][]string, len(sets))
	for setIndex, setValue := range sets {
		set, ok := setValue.(map[string]any)
		if !ok {
			continue
		}
		generations, ok := set["generations"].([]any)
		if !ok {
			continue
		}
		fingerprints[setIndex] = make([]string, len(generations))
		for generationIndex, generationValue := range generations {
			generation, ok := generationValue.(map[string]any)
			if !ok {
				continue
			}
			delete(generation, "acceptsSourceFingerprints")
			delete(generation, "fingerprint")
			definition, err := json.Marshal(generation)
			if err != nil {
				return nil, fmt.Errorf("canonicalize parsed generation %d/%d: %w", setIndex, generationIndex, err)
			}
			fingerprints[setIndex][generationIndex] = sha256Hex(definition)
		}
	}
	return fingerprints, nil
}

func decodeModuleJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("parse module JSON: %w", err)
	}
	return ensureJSONEOF(decoder)
}

func decodeStrictModuleJSON(data []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return fmt.Errorf("parse module JSON: %w", err)
	}
	return ensureJSONEOF(decoder)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("parse module JSON: multiple JSON values")
		}
		return fmt.Errorf("parse module JSON: %w", err)
	}
	return nil
}

func removeLoaderFields(value any) {
	module, ok := value.(map[string]any)
	if !ok {
		return
	}
	for _, key := range []string{"filePath", "moduleDir", "revision", "unversioned", "canonicalSnapshot"} {
		delete(module, key)
	}
	config, ok := module["config"].(map[string]any)
	if !ok {
		return
	}
	sets, _ := config["sets"].([]any)
	for _, setValue := range sets {
		set, _ := setValue.(map[string]any)
		generations, _ := set["generations"].([]any)
		for _, generationValue := range generations {
			if generation, ok := generationValue.(map[string]any); ok {
				delete(generation, "fingerprint")
			}
		}
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// Platform fingerprints cover the selected authored variant, not its executable
// projection. Historical acceptance changes compatibility, not configuration.
func platformGenerationFingerprint(mod *Module, platform, setID, generationID string, variant PlatformVariant) (string, error) {
	var definition any
	if len(mod.canonicalSnapshot) > 0 {
		var authored struct {
			Platforms map[string]json.RawMessage `json:"platforms"`
		}
		if err := decodeModuleJSON(mod.canonicalSnapshot, &authored); err != nil {
			return "", err
		}
		if raw := authored.Platforms[platform]; len(raw) > 0 {
			if err := decodeModuleJSON(raw, &definition); err != nil {
				return "", err
			}
		}
	}
	if definition == nil {
		raw, err := json.Marshal(variant)
		if err != nil {
			return "", err
		}
		if err := decodeModuleJSON(raw, &definition); err != nil {
			return "", err
		}
	}
	removePlatformFingerprintFields(definition)
	envelope := struct {
		ModuleID     string `json:"moduleId"`
		Platform     string `json:"platform"`
		ConfigSetID  string `json:"configSetId"`
		GenerationID string `json:"generationId"`
		Variant      any    `json:"variant"`
	}{mod.ID, platform, setID, generationID, definition}
	canonical, err := json.Marshal(envelope)
	if err != nil {
		return "", fmt.Errorf("canonicalize platform generation: %w", err)
	}
	return sha256Hex(canonical), nil
}

func removePlatformFingerprintFields(value any) {
	variant, _ := value.(map[string]any)
	config, _ := variant["config"].(map[string]any)
	sets, _ := config["sets"].([]any)
	for _, item := range sets {
		set, _ := item.(map[string]any)
		generations, _ := set["generations"].([]any)
		for _, item := range generations {
			generation, _ := item.(map[string]any)
			delete(generation, "acceptsSourceFingerprints")
			delete(generation, "fingerprint")
		}
	}
}

func setPlatformGenerationFingerprints(mod *Module, platform string, variant *PlatformVariant) error {
	if variant.Config == nil {
		return nil
	}
	for setIndex := range variant.Config.Sets {
		set := &variant.Config.Sets[setIndex]
		for generationIndex := range set.Generations {
			generation := &set.Generations[generationIndex]
			fingerprint, err := platformGenerationFingerprint(mod, platform, set.ID, generation.ID, *variant)
			if err != nil {
				return err
			}
			generation.Fingerprint = fingerprint
		}
	}
	return nil
}
