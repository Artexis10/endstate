// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

import (
	"fmt"
	"strings"
	"unicode"
)

// MapHKCU maps a semantic current-user key into the scenario namespace.
func (context *Context) MapHKCU(key string) (string, error) {
	fail := func(message string) (string, error) {
		return "", fmt.Errorf("%w: %s", ErrUnsafeRegistry, message)
	}
	if key == "" || key != strings.TrimSpace(key) || strings.ContainsRune(key, '\x00') || strings.Contains(key, "/") {
		return fail("key is empty or malformed")
	}
	separator := strings.IndexByte(key, '\\')
	if separator <= 0 || separator == len(key)-1 {
		return fail("bare hives are forbidden")
	}
	hive := key[:separator]
	if !strings.EqualFold(hive, "HKCU") && !strings.EqualFold(hive, "HKEY_CURRENT_USER") {
		return fail("only HKCU is supported")
	}
	remainder := key[separator+1:]
	components := strings.Split(remainder, `\`)
	for _, component := range components {
		if component == "" || component == "." || component == ".." || component != strings.TrimSpace(component) ||
			strings.ContainsAny(component, `:%$~`) || strings.IndexFunc(component, unicode.IsControl) >= 0 {
			return fail("key contains an unsafe component")
		}
	}
	prefix := `Software\Endstate\Validation\`
	if len(components) >= 3 && strings.EqualFold(components[0], "Software") && strings.EqualFold(components[1], "Endstate") && strings.EqualFold(components[2], "Validation") {
		if len(components) < 5 || !strings.EqualFold(components[3], context.descriptor.Nonce) {
			return fail("key belongs to a different validation namespace")
		}
		return context.RegistryNamespace() + `\` + strings.Join(components[4:], `\`), nil
	}
	return `HKCU\` + prefix + context.descriptor.Nonce + `\` + strings.Join(components, `\`), nil
}
