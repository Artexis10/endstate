// Copyright 2026 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package migration

import (
	"context"
	"github.com/Artexis10/endstate/go-engine/internal/modules"
	"github.com/Artexis10/endstate/go-engine/internal/settingscodec"
	"testing"
)

func TestStageRejectsUnsupportedSettingsBeforeRestore(t *testing.T) {
	payload, parent := newStagePayload(t, map[string]string{"config": "--pre=external-command\n"})
	request := directStageRequest(t, payload, parent, []modules.ValidationDef{{Type: "settings-codec", Path: "config", Codec: settingscodec.RipgrepPortableArgumentsV1, Optional: true}})
	if staged, err := NewEngine().Stage(context.Background(), request); err == nil {
		staged.Close()
		t.Fatal("unsupported incoming configuration reached restore staging")
	}
}

func TestStageAllowsAbsentOptionalSettingsAlias(t *testing.T) {
	payload, parent := newStagePayload(t, map[string]string{"config": "--hidden\n"})
	request := directStageRequest(t, payload, parent, []modules.ValidationDef{
		{Type: "settings-codec", Path: "config", Codec: settingscodec.RipgrepPortableArgumentsV1, Optional: true},
		{Type: "settings-codec", Path: "absent", Codec: settingscodec.RipgrepPortableArgumentsV1, Optional: true},
	})
	staged, err := NewEngine().Stage(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	defer staged.Close()
}
