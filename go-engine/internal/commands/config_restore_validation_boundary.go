// Copyright 2025 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package commands

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Artexis10/endstate/go-engine/internal/configrestore"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

type configRestoreHostBoundary struct{ context *validationmode.Context }

func newConfigRestoreHostBoundary(context *validationmode.Context) configrestore.HostBoundary {
	if context == nil {
		return nil
	}
	return configRestoreHostBoundary{context: context}
}

func (boundary configRestoreHostBoundary) ResolveHostPath(authored string, instance modules.ConfigInstance) (string, error) {
	return boundary.context.ResolveHostPath(authored, validationmode.HostPathPolicy{
		InstanceRoot: instance.Root, AllowRoot: strings.EqualFold(authored, "${instance.root}"),
	})
}

func (boundary configRestoreHostBoundary) ResolveFilesystemIdentity(identity string) (string, error) {
	const rootToken = "$ENDSTATE_ROOT/"
	if strings.HasPrefix(identity, rootToken) {
		return validationmode.ResolvePortablePath(boundary.context.Root(), strings.TrimPrefix(identity, rootToken))
	}
	resolved, err := boundary.context.ResolveHostPath(identity, validationmode.HostPathPolicy{AllowRoot: true})
	if err != nil {
		return "", fmt.Errorf("resolve configuration identity %q: %w", identity, err)
	}
	return resolved, nil
}

func (boundary configRestoreHostBoundary) ProjectFilesystemIdentity(absolute string) (string, error) {
	return boundary.context.DisplayPath(filepath.Clean(absolute))
}

func (boundary configRestoreHostBoundary) ValidateFilesystemTarget(absolute string) error {
	return boundary.context.ValidateSandboxPath(filepath.Clean(absolute))
}
