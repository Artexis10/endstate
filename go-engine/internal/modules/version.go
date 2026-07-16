// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

const (
	// GenerationUnknown means no declared generation matched the evidence.
	GenerationUnknown = "unknown_generation"
	// GenerationAmbiguous means more than one generation matched; declaration
	// order is never used as a tie-breaker.
	GenerationAmbiguous = "ambiguous_generation"
)

var (
	numericDottedPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)*$`)
	rangeConstraint      = regexp.MustCompile(`^(>=|<=|==|!=|>|<|=)?([0-9]+(?:\.[0-9]+)*)$`)
)

// VersionEvidence preserves the vendor string exactly and, when it is a
// numeric dotted value, carries a normalized comparison form separately.
type VersionEvidence struct {
	Raw        string `json:"raw"`
	Normalized string `json:"normalized,omitempty"`
	Numeric    bool   `json:"numeric"`
}

// NewVersionEvidence preserves raw verbatim. Normalization trims surrounding
// whitespace, removes component-leading zeroes, and removes insignificant
// trailing zero components. Non-numeric vendor strings remain raw-only.
func NewVersionEvidence(raw string) VersionEvidence {
	normalized, err := NormalizeNumericVersion(raw)
	if err != nil {
		return VersionEvidence{Raw: raw}
	}
	return VersionEvidence{Raw: raw, Normalized: normalized, Numeric: true}
}

// NormalizeNumericVersion canonicalizes a numeric dotted version without
// converting components to fixed-width integers.
func NormalizeNumericVersion(version string) (string, error) {
	trimmed := strings.TrimSpace(version)
	if !numericDottedPattern.MatchString(trimmed) {
		return "", fmt.Errorf("version %q is not numeric dotted", version)
	}
	parts := strings.Split(trimmed, ".")
	for index, part := range parts {
		part = strings.TrimLeft(part, "0")
		if part == "" {
			part = "0"
		}
		parts[index] = part
	}
	for len(parts) > 1 && parts[len(parts)-1] == "0" {
		parts = parts[:len(parts)-1]
	}
	return strings.Join(parts, "."), nil
}

// CompareNumericVersions compares two numeric dotted versions and returns -1,
// 0, or 1. Missing components compare as zero and component size is unbounded.
func CompareNumericVersions(left, right string) (int, error) {
	leftNormalized, err := NormalizeNumericVersion(left)
	if err != nil {
		return 0, err
	}
	rightNormalized, err := NormalizeNumericVersion(right)
	if err != nil {
		return 0, err
	}

	leftParts := strings.Split(leftNormalized, ".")
	rightParts := strings.Split(rightNormalized, ".")
	length := len(leftParts)
	if len(rightParts) > length {
		length = len(rightParts)
	}
	for index := 0; index < length; index++ {
		leftPart := "0"
		if index < len(leftParts) {
			leftPart = leftParts[index]
		}
		rightPart := "0"
		if index < len(rightParts) {
			rightPart = rightParts[index]
		}
		if len(leftPart) < len(rightPart) {
			return -1, nil
		}
		if len(leftPart) > len(rightPart) {
			return 1, nil
		}
		if leftPart < rightPart {
			return -1, nil
		}
		if leftPart > rightPart {
			return 1, nil
		}
	}
	return 0, nil
}

// MatchNumericVersionRange evaluates a whitespace-separated AND expression
// such as ">=25 <28". Supported operators are >, >=, <, <=, =, ==, and !=;
// an omitted operator means equality.
func MatchNumericVersionRange(version, expression string) (bool, error) {
	if _, err := NormalizeNumericVersion(version); err != nil {
		return false, err
	}
	constraints := strings.Fields(strings.TrimSpace(expression))
	if len(constraints) == 0 {
		return false, fmt.Errorf("version range is empty")
	}
	type parsedConstraint struct {
		operator string
		version  string
	}
	parsed := make([]parsedConstraint, 0, len(constraints))
	for _, constraint := range constraints {
		matches := rangeConstraint.FindStringSubmatch(constraint)
		if matches == nil {
			return false, fmt.Errorf("invalid numeric version constraint %q", constraint)
		}
		operator := matches[1]
		if operator == "" {
			operator = "="
		}
		parsed = append(parsed, parsedConstraint{operator: operator, version: matches[2]})
	}
	for _, constraint := range parsed {
		comparison, err := CompareNumericVersions(version, constraint.version)
		if err != nil {
			return false, err
		}
		matched := false
		switch constraint.operator {
		case ">":
			matched = comparison > 0
		case ">=":
			matched = comparison >= 0
		case "<":
			matched = comparison < 0
		case "<=":
			matched = comparison <= 0
		case "=", "==":
			matched = comparison == 0
		case "!=":
			matched = comparison != 0
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}

// MatchVersionSelector evaluates a validated selector against preserved raw
// and optional normalized evidence.
func MatchVersionSelector(selector VersionSelectorDef, evidence VersionEvidence) (bool, error) {
	hasRange := selector.VersionRange != ""
	hasPattern := selector.VersionPattern != ""
	if hasRange == hasPattern {
		return false, fmt.Errorf("version selector must declare exactly one of versionRange or versionPattern")
	}
	if hasRange {
		if !evidence.Numeric {
			return false, nil
		}
		return MatchNumericVersionRange(evidence.Normalized, selector.VersionRange)
	}
	if err := validateAnchoredPattern(selector.VersionPattern); err != nil {
		return false, err
	}
	pattern, err := regexp.Compile(selector.VersionPattern)
	if err != nil {
		return false, err
	}
	return pattern.MatchString(evidence.Raw), nil
}

// GenerationMatchError carries a stable reason code for exactly-one matching.
type GenerationMatchError struct {
	Code        string
	ConfigSetID string
	Matches     []string
}

func (e *GenerationMatchError) Error() string {
	if e.Code == GenerationAmbiguous {
		return fmt.Sprintf("config set %q matches multiple generations: %s", e.ConfigSetID, strings.Join(e.Matches, ", "))
	}
	return fmt.Sprintf("config set %q has no matching generation", e.ConfigSetID)
}

// GenerationMatchCode extracts the stable exactly-one matching reason.
func GenerationMatchCode(err error) string {
	var matchError *GenerationMatchError
	if errors.As(err, &matchError) {
		return matchError.Code
	}
	return ""
}

// SelectGeneration returns a generation only when exactly one declaration
// matches. A selectorless generation is a catch-all, useful for stable layouts.
func SelectGeneration(set *ConfigSetDef, evidence VersionEvidence) (*GenerationDef, error) {
	if set == nil {
		return nil, &GenerationMatchError{Code: GenerationUnknown}
	}
	matching := make([]*GenerationDef, 0, 1)
	matchingIDs := make([]string, 0, 1)
	for index := range set.Generations {
		generation := &set.Generations[index]
		matched := len(generation.Matches) == 0
		for _, selector := range generation.Matches {
			selectorMatched, err := MatchVersionSelector(selector, evidence)
			if err != nil {
				return nil, err
			}
			if selectorMatched {
				matched = true
				break
			}
		}
		if matched {
			matching = append(matching, generation)
			matchingIDs = append(matchingIDs, generation.ID)
		}
	}
	if len(matching) == 0 {
		return nil, &GenerationMatchError{Code: GenerationUnknown, ConfigSetID: set.ID}
	}
	if len(matching) > 1 {
		return nil, &GenerationMatchError{Code: GenerationAmbiguous, ConfigSetID: set.ID, Matches: matchingIDs}
	}
	return matching[0], nil
}

func validateAnchoredPattern(pattern string) error {
	if !strings.HasPrefix(pattern, "^") || !strings.HasSuffix(pattern, "$") {
		return fmt.Errorf("raw version pattern %q must be anchored with ^ and $", pattern)
	}
	if _, err := regexp.Compile(pattern); err != nil {
		return fmt.Errorf("invalid raw version pattern %q: %w", pattern, err)
	}
	return nil
}
