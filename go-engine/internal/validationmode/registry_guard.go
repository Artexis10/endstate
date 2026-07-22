// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package validationmode

type ProtectedRegistry struct {
	Key       string
	ValueName string
	Label     string
	WholeKey  bool
}

type RegistryChange struct {
	Label string
	Kind  ChangeKind
}

type registryGuardLimits struct {
	keys, values int
	bytes        int64
}

var defaultRegistryGuardLimits = registryGuardLimits{keys: 4096, values: 16384, bytes: 16 << 20}
