// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/arp"
	"github.com/Artexis10/endstate/go-engine/internal/manifest"
)

// readARPInventoryFn is the command-level seam for hermetic ARP fixtures.
var readARPInventoryFn = arp.Read

// appPresence is the shared package-manager/ARP observation. The package
// manager ledger wins whenever it reports presence; ARP is only a fallback.
type appPresence struct {
	Present bool
	Version string
}

func observeAppPresence(ledgerPresent bool, ledgerVersion string, app manifest.App, inventory []arp.Entry) appPresence {
	if ledgerPresent {
		return appPresence{Present: true, Version: strings.TrimSpace(ledgerVersion)}
	}
	if app.Fingerprint == nil {
		return appPresence{}
	}
	if strings.TrimSpace(app.Fingerprint.Key) == "" || strings.TrimSpace(app.Fingerprint.Publisher) == "" {
		return appPresence{}
	}
	versions := map[string]bool{}
	for _, entry := range inventory {
		if sameFingerprintValue(app.Fingerprint.Key, entry.Key) && sameFingerprintValue(app.Fingerprint.Publisher, entry.Publisher) {
			versions[strings.TrimSpace(entry.DisplayVersion)] = true
		}
	}
	if len(versions) == 0 {
		return appPresence{}
	}
	if want := strings.TrimSpace(app.Version); want != "" && versions[want] {
		return appPresence{Present: true, Version: want}
	}
	if len(versions) == 1 {
		for version := range versions {
			return appPresence{Present: true, Version: version}
		}
	}
	return appPresence{Present: true}
}

func appPresent(ledgerPresent bool, app manifest.App, inventory []arp.Entry) bool {
	return observeAppPresence(ledgerPresent, "", app, inventory).Present
}

func sameFingerprintValue(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

// captureFingerprint returns an ARP fingerprint only for the backend-provided
// local ARP identifiers. Display names are never a capture binding key.
func captureFingerprint(localIdentifiers []string, inventory []arp.Entry) *manifest.InstallFingerprint {
	match := captureFingerprintMatch(localIdentifiers, inventory)
	return match
}

func captureInventoryMatches(localIdentifiers []string, inventory []arp.Entry) []int {
	matched := map[int]bool{}
	for _, localIdentifier := range localIdentifiers {
		if strings.TrimSpace(localIdentifier) == "" {
			continue
		}
		for i := range inventory {
			if sameFingerprintValue(localIdentifier, inventory[i].LocalIdentifier) {
				matched[i] = true
			}
		}
	}
	matches := make([]int, 0, len(matched))
	for i := range inventory {
		if matched[i] {
			matches = append(matches, i)
		}
	}
	return matches
}

func captureFingerprintMatch(localIdentifiers []string, inventory []arp.Entry) *manifest.InstallFingerprint {
	var fingerprint *manifest.InstallFingerprint
	versions := map[string]bool{}
	for _, index := range captureInventoryMatches(localIdentifiers, inventory) {
		entry := inventory[index]
		if strings.TrimSpace(entry.Key) == "" || strings.TrimSpace(entry.Publisher) == "" {
			return nil
		}
		if fingerprint == nil {
			fingerprint = &manifest.InstallFingerprint{Key: entry.Key, Publisher: entry.Publisher}
		} else if !sameFingerprintValue(fingerprint.Key, entry.Key) || !sameFingerprintValue(fingerprint.Publisher, entry.Publisher) {
			return nil
		}
		versions[strings.TrimSpace(entry.DisplayVersion)] = true
	}
	if fingerprint == nil {
		return nil
	}
	if len(versions) == 1 {
		for version := range versions {
			fingerprint.Version = version
		}
	}
	return fingerprint
}
