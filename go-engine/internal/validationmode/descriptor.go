// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/Artexis10/endstate/go-engine/internal/safepath"
)

const descriptorFilename = "validation-mode.json"

var (
	stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	kebabPattern    = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	modulePattern   = regexp.MustCompile(`^apps\.[a-z0-9]+(?:-[a-z0-9]+)*$`)
	tokenPattern    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._+/-]*$`)
)

// Descriptor is the data-only authority for one validation scenario.
type Descriptor struct {
	SchemaVersion int       `json:"schemaVersion"`
	ScenarioID    string    `json:"scenarioId"`
	Nonce         string    `json:"nonce"`
	ModuleID      string    `json:"moduleId"`
	Inventory     Inventory `json:"inventory"`
	DynamicRoots  []string  `json:"dynamicRoots,omitempty"`
}

// Inventory describes the single disposable package visible to the scenario.
type Inventory struct {
	AppID        string `json:"appId"`
	Driver       string `json:"driver"`
	Ref          string `json:"ref"`
	DisplayName  string `json:"displayName"`
	Version      string `json:"version,omitempty"`
	Source       string `json:"source,omitempty"`
	InitialState string `json:"initialState"`
}

func loadDescriptor(root string) (Descriptor, error) {
	path, err := safepath.Resolve(root, ".endstate/"+descriptorFilename)
	if err != nil {
		return Descriptor{}, fmt.Errorf("%w: descriptor path: %v", ErrInvalidDescriptor, err)
	}
	data, _, err := safepath.ReadRegularFile(path)
	if err != nil {
		return Descriptor{}, fmt.Errorf("%w: read descriptor: %v", ErrInvalidDescriptor, err)
	}
	if err := validateDescriptorJSONShape(data); err != nil {
		return Descriptor{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var descriptor Descriptor
	if err := decoder.Decode(&descriptor); err != nil {
		return Descriptor{}, fmt.Errorf("%w: decode descriptor: %v", ErrInvalidDescriptor, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return Descriptor{}, fmt.Errorf("%w: %v", ErrInvalidDescriptor, err)
	}
	if err := validateDescriptor(descriptor, filepath.Base(root)); err != nil {
		return Descriptor{}, err
	}
	return descriptor, nil
}

var descriptorJSONFields = map[string]map[string]struct{}{
	"": {
		"schemaVersion": {}, "scenarioId": {}, "nonce": {}, "moduleId": {},
		"inventory": {}, "dynamicRoots": {},
	},
	"inventory": {
		"appId": {}, "driver": {}, "ref": {}, "displayName": {},
		"version": {}, "source": {}, "initialState": {},
	},
}

func validateDescriptorJSONShape(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := inspectDescriptorJSONValue(decoder, ""); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidDescriptor, err)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return fmt.Errorf("%w: %v", ErrInvalidDescriptor, err)
	}
	return nil
}

func inspectDescriptorJSONValue(decoder *json.Decoder, objectPath string) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		allowed := descriptorJSONFields[objectPath]
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate field %q", key)
			}
			seen[key] = struct{}{}
			if allowed != nil {
				if _, known := allowed[key]; !known {
					return fmt.Errorf("unknown or incorrectly cased field %q", key)
				}
			}
			childPath := objectPath + "." + key
			if objectPath == "" {
				childPath = key
			}
			if err := inspectDescriptorJSONValue(decoder, childPath); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return errors.New("object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := inspectDescriptorJSONValue(decoder, objectPath+"[]"); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return errors.New("array is not closed")
		}
	default:
		return fmt.Errorf("unexpected delimiter %q", delimiter)
	}
	return nil
}

func validateDescriptor(descriptor Descriptor, rootBase string) error {
	fail := func(message string) error {
		return fmt.Errorf("%w: %s", ErrInvalidDescriptor, message)
	}
	if descriptor.SchemaVersion != 1 {
		return fail("schemaVersion must be 1")
	}
	if !stableIDPattern.MatchString(descriptor.ScenarioID) {
		return fail("scenarioId is not stable")
	}
	if !kebabPattern.MatchString(descriptor.Nonce) {
		return fail("nonce is not lowercase kebab-case")
	}
	if rootBase != "endstate-validation-"+descriptor.Nonce {
		return fail("nonce does not match validation root")
	}
	if !modulePattern.MatchString(descriptor.ModuleID) {
		return fail("moduleId is malformed")
	}
	if err := validateInventory(descriptor.Inventory); err != nil {
		return err
	}
	seen := make(map[string]struct{}, len(descriptor.DynamicRoots))
	for _, name := range descriptor.DynamicRoots {
		folded := strings.ToLower(name)
		if !kebabPattern.MatchString(name) {
			return fail("dynamic root name is malformed")
		}
		if _, exists := seen[folded]; exists {
			return fail("dynamic root names are duplicated")
		}
		if _, collision := reservedEnvironmentNames[folded]; collision {
			return fail("dynamic root collides with a reserved environment name")
		}
		seen[folded] = struct{}{}
	}
	return nil
}

func validateInventory(inventory Inventory) error {
	fail := func(message string) error {
		return fmt.Errorf("%w: inventory %s", ErrInvalidDescriptor, message)
	}
	if !stableIDPattern.MatchString(inventory.AppID) {
		return fail("appId is malformed")
	}
	if !kebabPattern.MatchString(inventory.Driver) {
		return fail("driver is malformed")
	}
	if !safeToken(inventory.Ref) {
		return fail("ref is malformed")
	}
	if inventory.Source != "" && !safeToken(inventory.Source) {
		return fail("source is malformed")
	}
	if inventory.Version != "" && !safeToken(inventory.Version) {
		return fail("version is malformed")
	}
	if strings.TrimSpace(inventory.DisplayName) == "" || containsControl(inventory.DisplayName) {
		return fail("displayName is blank or contains control characters")
	}
	if inventory.InitialState != "present" && inventory.InitialState != "absent" {
		return fail("initialState must be present or absent")
	}
	return nil
}

func safeToken(value string) bool {
	return tokenPattern.MatchString(value) && !strings.Contains(value, "//") &&
		!strings.Contains(strings.ToLower(value), "://")
}

func containsControl(value string) bool {
	return strings.IndexFunc(value, unicode.IsControl) >= 0
}
