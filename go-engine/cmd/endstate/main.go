// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Command endstate is the Go implementation of the Endstate CLI engine.
// It parses os.Args directly (no external CLI framework) and dispatches to
// the appropriate command handler in internal/commands.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
)

// usageText is the top-level help text printed for --help or when no command is
// provided.
const usageText = `Usage: endstate <command> [flags]

Commands:
  capabilities    Report CLI capabilities (GUI handshake)
  apply           Execute provisioning plan
  verify          Verify machine state against manifest
  capture         Capture current machine state
  plan            Generate execution plan
  restore         Restore configuration files
  report          Retrieve run history
  doctor          Run diagnostics
  bootstrap       Bootstrap Endstate installation

Global flags:
  --json               Output result as a single-line JSON envelope to stdout
  --events jsonl       Stream events as NDJSON to stderr
  --debug-cli          Print resolved command and flags to stderr before running
  --help, -h           Show this help message

Per-command flags:
  --manifest <path>    Path to manifest file (apply, verify, plan, restore)
  --dry-run            Preview changes without applying them (apply, restore)
  --enable-restore     Enable restore operations during apply (opt-in)

Run 'endstate <command> --help' for command-specific help.
`

// parsedArgs holds the result of parsing os.Args.
type parsedArgs struct {
	command       string
	jsonMode      bool
	debugCLI      bool
	helpRequested bool
	events        string // "jsonl" or ""

	// Per-command flags
	manifest      string
	dryRun        bool
	enableRestore bool
}

func parseArgs(args []string) parsedArgs {
	var p parsedArgs

	if len(args) == 0 {
		p.helpRequested = true
		return p
	}

	// First non-flag argument is the command.
	i := 0
	if len(args) > 0 && !strings.HasPrefix(args[0], "--") && args[0] != "-h" {
		p.command = strings.ToLower(args[0])
		i = 1
	}

	for i < len(args) {
		arg := args[i]
		switch arg {
		case "--json":
			p.jsonMode = true
		case "--debug-cli":
			p.debugCLI = true
		case "--help", "-h":
			p.helpRequested = true
		case "--dry-run":
			p.dryRun = true
		case "--enable-restore":
			p.enableRestore = true
		case "--events":
			if i+1 < len(args) {
				p.events = args[i+1]
				i++
			}
		case "--manifest":
			if i+1 < len(args) {
				p.manifest = args[i+1]
				i++
			}
		default:
			// Unknown flag: silently ignore (graceful degradation for future flags).
		}
		i++
	}

	return p
}

// commandUsage returns a short usage blurb for a specific command.
func commandUsage(cmd string) string {
	switch cmd {
	case "capabilities":
		return "Usage: endstate capabilities [--json]\n\nReport CLI capabilities for GUI handshake.\n"
	case "apply":
		return "Usage: endstate apply [--manifest <path>] [--dry-run] [--enable-restore] [--json] [--events jsonl]\n\nExecute provisioning plan.\n"
	case "verify":
		return "Usage: endstate verify [--manifest <path>] [--json] [--events jsonl]\n\nVerify machine state against manifest.\n"
	case "capture":
		return "Usage: endstate capture [--manifest <path>] [--output <path>] [--json] [--events jsonl]\n\nCapture current machine state.\n"
	case "plan":
		return "Usage: endstate plan [--manifest <path>] [--json]\n\nGenerate execution plan.\n"
	case "restore":
		return "Usage: endstate restore [--manifest <path>] [--filter <expr>] [--json] [--events jsonl]\n\nRestore configuration files.\n"
	case "report":
		return "Usage: endstate report [--json]\n\nRetrieve run history.\n"
	case "doctor":
		return "Usage: endstate doctor [--json]\n\nRun diagnostics.\n"
	case "bootstrap":
		return "Usage: endstate bootstrap\n\nBootstrap Endstate installation.\n"
	default:
		return usageText
	}
}

func main() {
	// Skip the program name (args[0]).
	p := parseArgs(os.Args[1:])

	// --debug-cli: print resolved command and flags to stderr.
	if p.debugCLI {
		fmt.Fprintf(os.Stderr, "[debug-cli] command=%q json=%v dryRun=%v enableRestore=%v manifest=%q events=%q\n",
			p.command, p.jsonMode, p.dryRun, p.enableRestore, p.manifest, p.events)
	}

	// Handle --help / no command before doing any further work.
	if p.helpRequested || p.command == "" {
		fmt.Print(commandUsage(p.command))
		os.Exit(0)
	}

	// Resolve versions from repo root (best-effort; falls back gracefully).
	repoRoot := config.ResolveRepoRoot()
	schemaVersion := config.ReadSchemaVersion(repoRoot)
	cliVersion := config.ReadVersion(repoRoot)

	now := time.Now().UTC()
	runID := envelope.BuildRunID(p.command, now)

	// Dispatch to command handler.
	data, cmdErr := dispatch(p)

	if p.jsonMode {
		var env *envelope.Envelope
		if cmdErr != nil {
			env = envelope.NewFailure(p.command, runID, schemaVersion, cliVersion, cmdErr)
		} else {
			env = envelope.NewSuccess(p.command, runID, schemaVersion, cliVersion, data)
		}

		b, marshalErr := envelope.Marshal(env)
		if marshalErr != nil {
			// Last-resort: write a minimal error envelope manually.
			fmt.Fprintf(os.Stdout, `{"schemaVersion":%q,"cliVersion":%q,"command":%q,"runId":%q,"timestampUtc":%q,"success":false,"data":{},"error":{"code":"INTERNAL_ERROR","message":"failed to marshal response"}}`,
				schemaVersion, cliVersion, p.command, runID, now.Format(time.RFC3339))
			fmt.Fprintln(os.Stdout)
			os.Exit(1)
		}
		// The JSON envelope is the LAST line of stdout.
		fmt.Println(string(b))
	} else {
		// Human-readable output.
		if cmdErr != nil {
			fmt.Fprintf(os.Stderr, "Error [%s]: %s\n", cmdErr.Code, cmdErr.Message)
			if cmdErr.Remediation != "" {
				fmt.Fprintf(os.Stderr, "Remediation: %s\n", cmdErr.Remediation)
			}
			os.Exit(1)
		}
		// For commands with non-JSON output, pretty-print data as indented JSON as a
		// readable fallback until each command has a bespoke human formatter.
		if data != nil {
			b, _ := json.MarshalIndent(data, "", "  ")
			fmt.Println(string(b))
		}
	}

	if cmdErr != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// dispatch routes the parsed command to its handler and returns the data payload
// or an envelope error.
func dispatch(p parsedArgs) (interface{}, *envelope.Error) {
	switch p.command {
	case "capabilities":
		return commands.RunCapabilities()

	case "apply":
		return commands.RunApply(commands.ApplyFlags{
			Manifest:      p.manifest,
			DryRun:        p.dryRun,
			EnableRestore: p.enableRestore,
			Events:        p.events,
		})

	case "verify":
		return commands.RunVerify(commands.VerifyFlags{
			Manifest: p.manifest,
			Events:   p.events,
		})

	case "capture", "plan", "restore", "report", "doctor", "bootstrap":
		// Stubs: Teammate 2 / Teammate 3 will fill these in.
		return nil, envelope.NewError(
			envelope.ErrInternalError,
			"command not yet implemented",
		)

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n\n", p.command)
		fmt.Print(usageText)
		os.Exit(1)
		return nil, nil // unreachable
	}
}
