// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package modules

import (
	"fmt"
	"strings"
)

// ExpandInstanceTemplate expands only the allowlisted instance placeholders
// without applying filesystem cleaning. It is intended for non-filesystem
// declarations such as registry keys and value names.
func ExpandInstanceTemplate(template string, instance ConfigInstance) (string, error) {
	if strings.TrimSpace(template) == "" {
		return "", fmt.Errorf("instance template is empty")
	}
	return expandInstancePlaceholders(template, instance)
}

func expandInstancePlaceholders(template string, instance ConfigInstance) (string, error) {
	if err := validateTemplatePlaceholders(template); err != nil {
		return "", err
	}
	values := []struct {
		token string
		value string
	}{
		{token: "${instance.root}", value: instance.Root},
		{token: "${instance.version}", value: instance.Version.Raw},
		{token: "${instance.id}", value: instance.ID},
	}
	replacements := make([]string, 0, len(values)*2)
	for _, replacement := range values {
		if !strings.Contains(template, replacement.token) {
			continue
		}
		if replacement.value == "" {
			return "", fmt.Errorf("placeholder %s has no value", replacement.token)
		}
		replacements = append(replacements, replacement.token, replacement.value)
	}
	if len(replacements) == 0 {
		return template, nil
	}
	return strings.NewReplacer(replacements...).Replace(template), nil
}
