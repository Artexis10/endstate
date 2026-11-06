// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

package schedule

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsBackendNativeArgumentsAndMissingTask(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isolated POSIX executable boundary; native Windows acceptance is separate")
	}
	dir := t.TempDir()
	record := filepath.Join(dir, "arguments")
	// This executable records the native argv; it never contacts Task Scheduler.
	script := "#!/bin/sh\nprintf '%s\\000' \"$@\" >> \"$ENDSTATE_TEST_TASK_ARGS\"\nif [ \"$1\" = /Query ]; then printf '\"\\\\Endstate\\\\DriftCheck\",\"N/A\",\"Ready\"\\n'; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "schtasks"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("ENDSTATE_TEST_TASK_ARGS", record)
	cfg := &Config{}
	reg := &SchtasksRegistrar{}
	intent := Intent{Executable: `C:\Program Files\Endstate\endstate.exe`, Args: []string{"schedule", "run", "--root", `C:\root with "quotes"\`, "--json"}, Interval: "weekly", Time: "10:30"}
	saves := 0
	if err := reg.Enable(context.Background(), intent, cfg, func() error { saves++; return nil }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(string(data), "\x00")
	joined := strings.Join(args, "|")
	if !cfg.Enabled || saves != 1 || !strings.Contains(joined, "/TN|"+TaskName+"|") || !strings.Contains(joined, "/SC|WEEKLY|") || !strings.Contains(joined, "/IT|") {
		t.Fatalf("native registration=%q", args)
	}
	encoded := strings.Join([]string{windowsArgument(intent.Executable), windowsArgument("schedule"), windowsArgument("run"), windowsArgument("--root"), windowsArgument(intent.Args[3]), windowsArgument("--json")}, " ")
	found := false
	for i, arg := range args {
		if arg == "/TR" && i+1 < len(args) && args[i+1] == encoded {
			found = true
		}
	}
	if !found {
		t.Fatalf("literal native command=%q want %q", args, encoded)
	}
	if err := reg.Disable(context.Background(), cfg, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || !strings.Contains(string(data), "/Delete\x00/F\x00/TN\x00"+TaskName) {
		t.Fatalf("delete argv=%q enabled=%v", data, cfg.Enabled)
	}
	// Empty structured task enumeration proves absence without localized text.
	if err := os.WriteFile(filepath.Join(dir, "schtasks"), []byte("#!/bin/sh\nif [ \"$1\" != /Query ]; then exit 9; fi\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := reg.Disable(context.Background(), cfg, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
}
