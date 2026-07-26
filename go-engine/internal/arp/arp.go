// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Package arp reads Windows Add/Remove Programs inventory.
package arp

// Entry is one visible Add/Remove Programs entry.
type Entry struct {
	LocalIdentifier string
	Key             string
	DisplayName     string
	DisplayVersion  string
	Publisher       string
	InstallLocation string
}
