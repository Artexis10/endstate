//go:build !windows

// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package arp

// Read is a no-op outside Windows, where ARP does not exist.
func Read() []Entry { return nil }
