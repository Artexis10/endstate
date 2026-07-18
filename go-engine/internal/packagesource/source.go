// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package packagesource owns the stable source identity used by package
// manifests, drivers, capture, and provisioning history.
package packagesource

import (
	"regexp"
	"strings"
)

const (
	Winget  = "winget"
	MSStore = "msstore"
)

var storeIDPatterns = []*regexp.Regexp{
	regexp.MustCompile(`^9[A-Z0-9]{10,}$`),
	regexp.MustCompile(`^XP[A-Z0-9]{10,}$`),
}

func IsStoreID(ref string) bool {
	ref = strings.ToUpper(strings.TrimSpace(ref))
	for _, pattern := range storeIDPatterns {
		if pattern.MatchString(ref) {
			return true
		}
	}
	return false
}

// ResolveWinget returns an explicit valid source unchanged and otherwise
// applies the compatibility Store-ID classifier before defaulting to winget.
func ResolveWinget(ref, explicit string) string {
	if source := strings.ToLower(strings.TrimSpace(explicit)); source != "" {
		return source
	}
	if IsStoreID(ref) {
		return MSStore
	}
	return Winget
}

func ValidWinget(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", Winget, MSStore:
		return true
	default:
		return false
	}
}
