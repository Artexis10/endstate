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
	"strconv"
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
  revert          Revert last restore operation
  export-config   Export configuration files from system
  validate-export Validate export completeness
  report          Retrieve run history
  doctor          Run diagnostics
  bootstrap       Bootstrap Endstate installation
  backup          Hosted Backup commands (login, logout, status, ...)
  account         Hosted account management (delete)

Global flags:
  --json               Output result as a single-line JSON envelope to stdout
  --events jsonl       Stream events as NDJSON to stderr
  --debug-cli          Print resolved command and flags to stderr before running
  --help, -h           Show this help message

Per-command flags:
  --manifest <path>    Path to manifest file (apply, verify, plan, capture, restore)
                       Alias: --profile <path> (apply, verify, plan)
  --dry-run            Preview changes without applying them (apply, restore)
  --enable-restore     Enable restore operations during apply (opt-in)
  --out <path>         Output file path (capture)
  --name <name>        Manifest name (capture)
  --profile <name>     Profile name for output (capture)
  --sanitize           Strip machine-specific fields (capture)
  --discover           Discover installed apps (capture)
  --update             Update existing manifest (capture)
  --include-runtimes   Include runtime packages (capture)
  --include-store-apps Include Microsoft Store apps (capture)
  --minimize           Minimize manifest format (capture)
  --export <path>      Export directory path (restore, export-config, validate-export)
  --restore-filter <e> Filter restore entries by module ID (restore, apply)
  --latest             Most recent run (report)
  --last <n>           Last N runs (report)
  --run-id <id>        Specific run ID (report)

Subcommands:
  profile list         List discovered profiles
  profile path <name>  Resolve profile path
  profile validate <p> Validate a profile manifest
  backup signup        Create Hosted Backup account (passphrase via stdin)
  backup login         Sign in to Hosted Backup (passphrase via stdin)
  backup logout        Clear cached Hosted Backup session
  backup status        Report Hosted Backup session state
  backup push          Encrypt and upload a profile (--profile required)
  backup pull          Download and restore a profile (--backup-id, --to required)
  backup list          List backups
  backup versions      List versions of a backup (--backup-id required)
  backup delete        Permanently delete a backup (--backup-id, --confirm)
  backup delete-version Soft-delete a backup version (--backup-id, --version-id, --confirm)
  backup recover       Reset passphrase using BIP39 recovery key (stdin)
  account delete       Delete the Hosted Backup account (requires --confirm)

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
	export        string // --export <path>
	restoreFilter string // --restore-filter <expr>

	// Capture flags
	out              string
	name             string
	profile          string
	sanitize         bool
	discover         bool
	update           bool
	includeRuntimes  bool
	includeStoreApps bool
	minimize         bool

	// Report flags
	latest bool
	last   int
	runID  string

	// Backup / account flags
	email          string
	backupID       string
	versionID      string
	to             string
	confirm        bool
	saveRecoveryTo string
	overwrite      bool

	// Positional args after command (used by profile / backup / account subcommands)
	positionalArgs []string
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
		case "--sanitize", "-Sanitize":
			p.sanitize = true
		case "--discover":
			p.discover = true
		case "--update":
			p.update = true
		case "--include-runtimes":
			p.includeRuntimes = true
		case "--include-store-apps":
			p.includeStoreApps = true
		case "--minimize":
			p.minimize = true
		case "--latest":
			p.latest = true
		case "--confirm":
			p.confirm = true
		case "--overwrite":
			p.overwrite = true
		case "--WithConfig":
			// GUI sends --WithConfig for capture; the Go engine includes
			// config modules by default, so this is a no-op. Accept it
			// silently to avoid "unknown flag" confusion in debug logs.
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
		case "--out", "-Out":
			if i+1 < len(args) {
				p.out = args[i+1]
				i++
			}
		case "--name", "-Name":
			if i+1 < len(args) {
				p.name = args[i+1]
				i++
			}
		case "--profile", "-Profile":
			if i+1 < len(args) {
				p.profile = args[i+1]
				i++
			}
		case "--last":
			if i+1 < len(args) {
				n, err := strconv.Atoi(args[i+1])
				if err == nil {
					p.last = n
				}
				i++
			}
		case "--run-id":
			if i+1 < len(args) {
				p.runID = args[i+1]
				i++
			}
		case "--export":
			if i+1 < len(args) {
				p.export = args[i+1]
				i++
			}
		case "--restore-filter":
			if i+1 < len(args) {
				p.restoreFilter = args[i+1]
				i++
			}
		case "--email":
			if i+1 < len(args) {
				p.email = args[i+1]
				i++
			}
		case "--backup-id":
			if i+1 < len(args) {
				p.backupID = args[i+1]
				i++
			}
		case "--version-id":
			if i+1 < len(args) {
				p.versionID = args[i+1]
				i++
			}
		case "--to":
			if i+1 < len(args) {
				p.to = args[i+1]
				i++
			}
		case "--save-recovery-to":
			if i+1 < len(args) {
				p.saveRecoveryTo = args[i+1]
				i++
			}
		default:
			// Collect positional args (e.g., profile subcommands: "list", "path", "validate").
			if !strings.HasPrefix(arg, "-") {
				p.positionalArgs = append(p.positionalArgs, arg)
			}
			// Unknown flags are silently ignored (graceful degradation).
		}
		i++
	}

	// GUI compatibility: the GUI sends --profile <path> for apply/verify/plan,
	// but the Go engine expects --manifest <path>. When --profile is set and
	// --manifest is not, treat --profile as --manifest for these commands.
	if p.manifest == "" && p.profile != "" {
		switch p.command {
		case "apply", "verify", "plan":
			p.manifest = p.profile
			p.profile = "" // clear so it doesn't confuse capture logic
		}
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
		return "Usage: endstate capture [--discover] [--sanitize] [--name <name>] [--out <path>] [--profile <name>] [--manifest <path>] [--update] [--include-runtimes] [--include-store-apps] [--minimize] [--json] [--events jsonl]\n\nCapture current machine state.\n"
	case "plan":
		return "Usage: endstate plan --manifest <path> [--json] [--events jsonl]\n\nGenerate execution plan.\n"
	case "restore":
		return "Usage: endstate restore [--manifest <path>] [--enable-restore] [--dry-run] [--export <path>] [--restore-filter <expr>] [--json] [--events jsonl]\n\nRestore configuration files.\n"
	case "revert":
		return "Usage: endstate revert [--json] [--events jsonl]\n\nRevert last restore operation using journal.\n"
	case "export-config":
		return "Usage: endstate export-config [--manifest <path>] [--export <path>] [--dry-run] [--json] [--events jsonl]\n\nExport configuration files from system to portable directory.\n"
	case "validate-export":
		return "Usage: endstate validate-export [--manifest <path>] [--export <path>] [--json] [--events jsonl]\n\nValidate export directory completeness.\n"
	case "report":
		return "Usage: endstate report [--latest] [--last <n>] [--run-id <id>] [--json]\n\nRetrieve run history.\n"
	case "doctor":
		return "Usage: endstate doctor [--json]\n\nRun system diagnostics.\n"
	case "profile":
		return "Usage: endstate profile <subcommand> [args] [--json]\n\nSubcommands:\n  list              List discovered profiles\n  path <name>       Resolve profile path from name\n  validate <path>   Validate a profile manifest\n"
	case "backup":
		return "Usage: endstate backup <subcommand> [flags] [--json] [--events jsonl]\n\nSubcommands:\n  signup --email <addr> --save-recovery-to <path>\n                              Create account (passphrase + optional 24-word phrase via stdin)\n  login --email <addr>          Sign in (passphrase via stdin)\n  logout                        Clear local session\n  status                        Report current session state\n  push --profile <path> [--backup-id <id>] [--name <label>]\n                              Encrypt and upload a profile\n  pull --backup-id <id> --to <path> [--version-id <id>] [--overwrite]\n                              Download and restore a profile\n  list                          List backups\n  versions --backup-id <id>     List versions of a backup\n  delete --backup-id <id> --confirm\n                              Permanently delete a backup\n  delete-version --backup-id <id> --version-id <id> --confirm\n                              Soft-delete a backup version\n  recover --email <addr>        Reset passphrase using recovery phrase (stdin: phrase, then new passphrase)\n\nEnv vars:\n  ENDSTATE_OIDC_ISSUER_URL    Backend issuer URL (default: https://substratesystems.io)\n  ENDSTATE_OIDC_AUDIENCE      JWT audience (default: endstate-backup)\n  ENDSTATE_BACKUP_CONCURRENCY Worker pool size for chunk transfer (default 4, clamp 1..16)\n"
	case "account":
		return "Usage: endstate account <subcommand> [flags] [--json]\n\nSubcommands:\n  delete --confirm  Delete the Hosted Backup account permanently\n"
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
			Export:        p.export,
			RestoreFilter: p.restoreFilter,
		})

	case "verify":
		return commands.RunVerify(commands.VerifyFlags{
			Manifest: p.manifest,
			Events:   p.events,
		})

	case "capture":
		return commands.RunCapture(commands.CaptureFlags{
			Manifest:         p.manifest,
			Out:              p.out,
			Profile:          p.profile,
			Name:             p.name,
			Sanitize:         p.sanitize,
			Discover:         p.discover,
			Update:           p.update,
			IncludeRuntimes:  p.includeRuntimes,
			IncludeStoreApps: p.includeStoreApps,
			Minimize:         p.minimize,
			Events:           p.events,
		})

	case "plan":
		return commands.RunPlan(commands.PlanFlags{
			Manifest: p.manifest,
			Events:   p.events,
		})

	case "report":
		return commands.RunReport(commands.ReportFlags{
			Latest: p.latest,
			Last:   p.last,
			RunID:  p.runID,
			Events: p.events,
		})

	case "doctor":
		return commands.RunDoctor(commands.DoctorFlags{
			Events: p.events,
		})

	case "profile":
		subcommand := ""
		var subArgs []string
		if len(p.positionalArgs) > 0 {
			subcommand = p.positionalArgs[0]
			subArgs = p.positionalArgs[1:]
		}
		return commands.RunProfile(commands.ProfileFlags{
			Subcommand: subcommand,
			Args:       subArgs,
			Events:     p.events,
		})

	case "restore":
		return commands.RunRestore(commands.RestoreFlags{
			Manifest:      p.manifest,
			EnableRestore: p.enableRestore,
			DryRun:        p.dryRun,
			Export:        p.export,
			Events:        p.events,
			RestoreFilter: p.restoreFilter,
		})

	case "revert":
		return commands.RunRevert(commands.RevertFlags{
			Events: p.events,
		})

	case "export-config":
		return commands.RunExport(commands.ExportFlags{
			Manifest: p.manifest,
			Export:   p.export,
			DryRun:   p.dryRun,
			Events:   p.events,
		})

	case "validate-export":
		return commands.RunValidateExport(commands.ValidateExportFlags{
			Manifest: p.manifest,
			Export:   p.export,
			Events:   p.events,
		})

	case "bootstrap":
		return commands.RunBootstrap(commands.BootstrapFlags{
			Events: p.events,
		})

	case "backup":
		subcommand := ""
		var subArgs []string
		if len(p.positionalArgs) > 0 {
			subcommand = p.positionalArgs[0]
			subArgs = p.positionalArgs[1:]
		}
		return commands.RunBackup(commands.BackupFlags{
			Subcommand:     subcommand,
			Args:           subArgs,
			Email:          p.email,
			BackupID:       p.backupID,
			VersionID:      p.versionID,
			Profile:        p.profile,
			Name:           p.name,
			To:             p.to,
			Confirm:        p.confirm,
			SaveRecoveryTo: p.saveRecoveryTo,
			Overwrite:      p.overwrite,
			Events:         p.events,
		})

	case "account":
		subcommand := ""
		var subArgs []string
		if len(p.positionalArgs) > 0 {
			subcommand = p.positionalArgs[0]
			subArgs = p.positionalArgs[1:]
		}
		return commands.RunAccount(commands.AccountFlags{
			Subcommand: subcommand,
			Args:       subArgs,
			Confirm:    p.confirm,
			Events:     p.events,
		})

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n\n", p.command)
		fmt.Print(usageText)
		os.Exit(1)
		return nil, nil // unreachable
	}
}
