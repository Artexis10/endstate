// Copyright 2026 Substrate Systems OU
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	stateEnvironment     = "ENDSTATE_FAKE_WINGET_STATE"
	logEnvironment       = "ENDSTATE_FAKE_WINGET_LOG"
	strictEnvironment    = "ENDSTATE_INTERNAL_HOSTED_WINGET_STRICT_V1"
	authorityEnvironment = "ENDSTATE_INTERNAL_HOSTED_WINGET_AUTHORITY_V1"
)

type call struct {
	Executable       string   `json:"executable"`
	Operation        string   `json:"operation"`
	Arguments        []string `json:"arguments"`
	StrictPresent    bool     `json:"strictPresent"`
	AuthorityPresent bool     `json:"authorityPresent"`
}

func main() {
	state, log := os.Getenv(stateEnvironment), os.Getenv(logEnvironment)
	if state == "" || log == "" {
		fmt.Fprintln(os.Stderr, "fake Winget requires private test state")
		os.Exit(2)
	}
	executable, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake Winget executable unavailable")
		os.Exit(2)
	}
	args := append([]string(nil), os.Args[1:]...)
	operation := classify(args)
	if err := appendCall(log, call{
		Executable: filepath.Clean(executable), Operation: operation, Arguments: args,
		StrictPresent: os.Getenv(strictEnvironment) != "", AuthorityPresent: os.Getenv(authorityEnvironment) != "",
	}); err != nil {
		fmt.Fprintln(os.Stderr, "fake Winget log unavailable")
		os.Exit(2)
	}

	switch operation {
	case "install":
		must(os.MkdirAll(state, 0o700))
		must(os.WriteFile(filepath.Join(state, "installed"), []byte("1"), 0o600))
	case "uninstall":
		must(os.Remove(filepath.Join(state, "installed")))
	case "export":
		writeExport(state, outputPath(args))
	case "details":
		if installed(state) {
			fmt.Println("Fake App [Fake.App]")
			fmt.Println(`    ARP\Machine\X64\Fake.App`)
		}
	case "list":
		if installed(state) {
			writeList(true)
			return
		}
		if hasArgument(args, "--id") {
			os.Exit(1)
		}
		writeList(false)
	}
}

func classify(args []string) string {
	if len(args) == 0 {
		return "unknown"
	}
	if args[0] == "list" && hasArgument(args, "--details") {
		return "details"
	}
	return args[0]
}

func appendCall(path string, value call) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}

func installed(state string) bool {
	_, err := os.Stat(filepath.Join(state, "installed"))
	return err == nil
}

func outputPath(args []string) string {
	for index, arg := range args {
		if arg == "-o" && index+1 < len(args) {
			return args[index+1]
		}
	}
	return ""
}

func writeExport(state, output string) {
	if output == "" {
		fmt.Fprintln(os.Stderr, "fake Winget export output missing")
		os.Exit(2)
	}
	packages := []map[string]string{}
	if installed(state) {
		packages = append(packages, map[string]string{"PackageIdentifier": "Fake.App", "Version": "1.0.0"})
	}
	data, err := json.Marshal(map[string]any{"Sources": []map[string]any{{"SourceDetails": map[string]string{"Name": "winget"}, "Packages": packages}}})
	must(err)
	must(os.WriteFile(output, data, 0o600))
}

func writeList(hasPackage bool) {
	fmt.Println("Name                 Id                   Version      Source")
	fmt.Println("---------------------------------------------------------------")
	if hasPackage {
		fmt.Println("Fake App             Fake.App             1.0.0        winget")
	}
}

func hasArgument(args []string, wanted string) bool {
	for _, arg := range args {
		if strings.EqualFold(arg, wanted) {
			return true
		}
	}
	return false
}

func must(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "fake Winget state failure")
	os.Exit(2)
}
