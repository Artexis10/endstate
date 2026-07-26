// Copyright 2025 Substrate Systems OÜ
// SPDX-License-Identifier: Apache-2.0

// Command endstate is the Go implementation of the Endstate CLI engine.
// It parses os.Args directly (no external CLI framework) and dispatches to
// the appropriate command handler in internal/commands.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Artexis10/endstate/go-engine/internal/commands"
	"github.com/Artexis10/endstate/go-engine/internal/config"
	"github.com/Artexis10/endstate/go-engine/internal/envelope"
	"github.com/Artexis10/endstate/go-engine/internal/events"
	"github.com/Artexis10/endstate/go-engine/internal/validationmode"
)

// usageText is the top-level help text printed for --help or when no command is
// provided.
const usageText = `Usage: endstate <command> [flags]

Commands:
  capabilities    Report CLI capabilities (GUI handshake)
  apply           Execute provisioning plan
  rebuild         Rebuild a machine from a bundle or manifest (install + restore + verify)
  import          Import an external package list into a manifest (--from unigetui)
  verify          Verify machine state against manifest
  capture         Capture current machine state
  plan            Generate execution plan
  catalog-plan    Resolve one tracked catalog bundle
  restore         Restore configuration files
  revert          Revert last restore operation
  export-config   Export configuration files from system
  validate-export Validate export completeness
  report          Retrieve run history
  generations     List provisioning generations
  rollback        Roll back packages to a prior generation (native-rollback backends)
  doctor          Run diagnostics
  bootstrap       Bootstrap Endstate installation
  backup          Hosted Backup commands (login, logout, status, ...)
  account         Hosted account management (delete)
  schedule        Scheduled drift-check commands (enable, disable, status, run)

Global flags:
  --json               Output result as a single-line JSON envelope to stdout
  --events jsonl       Stream events as NDJSON to stderr
  --debug-cli          Print resolved command and flags to stderr before running
  --help, -h           Show this help message

Per-command flags:
  --manifest <path>    Path to manifest file (apply, verify, plan, capture, restore)
                       Alias: --profile <path> (apply, verify, plan)
  --bundle <path>      Tracked bundle path (catalog-plan)
  --dry-run            Preview changes without applying them (apply, restore)
  --enable-restore     Enable restore operations during apply, and home-manager config rollback (opt-in)
  --out <path>         Output file path (capture)
  --name <name>        Manifest name (capture)
  --profile <name>     Profile name for output (capture)
  --sanitize           Strip machine-specific fields (capture)
  --discover           Discover installed apps (capture)
  --update             Update existing manifest (capture)
  --include-runtimes   Include runtime packages (capture)
  --include-store-apps Deprecated no-op; Store apps are included by default (capture)
  --exclude-store-apps Exclude Microsoft Store apps without accessing that source (capture)
  --minimize           Minimize manifest format (capture)
  --pin                Record installed versions into the manifest (capture)
  --share              Produce a bundle to hand to someone else (capture; needs --only)
  --driver <name>      Limit capture to a package driver; repeatable (capture)
  --bootstrap-backends Authorize setup of selected absent backends (apply, rebuild)
  --no-bootstrap       Skip selected absent backend lanes (apply, rebuild)
  --export <path>      Export directory path (restore, export-config, validate-export)
  --restore-filter <e> Filter restore entries by module ID (restore, apply, rebuild)
  --restore-target <m> Map capture ID to target instance; repeatable (restore, apply, rebuild)
  --from <path>        Bundle (.zip) or manifest (.jsonc) to rebuild from (rebuild)
  --from <source>      External package-list source to import (import; e.g. unigetui)
  --path <file>        Source bundle file to import (import)
  --no-restore         Install without restoring configuration (rebuild)
  --latest             Most recent run (report)
  --last <n>           Last N runs (report)
  --run-id <id>        Specific run ID (report)
  --to <generation>    Target provisioning generation number (rollback; default: previous)
  --confirm            Acknowledge a state-changing operation (apply --prune, rollback, backup/account delete)

Subcommands:
  schedule enable      Register drift-check task (--manifest, --interval, --time, --auto-push)
  schedule disable     Remove drift-check task
  schedule status      Report schedule config and last run outcome
  schedule run         Execute drift-check now (--root, --json)
  profile list         List discovered profiles
  profile path <name>  Resolve profile path
  profile validate <p> Validate a profile manifest
  backup signup        Create Hosted Backup account (passphrase via stdin)
  backup login         Sign in to Hosted Backup (passphrase via stdin)
  backup logout        Clear cached Hosted Backup session
  backup status        Report Hosted Backup session state
  backup subscribe     Start a Hosted Backup subscription checkout (returns checkoutUrl)
  backup browser-session Mint a short-lived /account portal handoff token (returns sessionToken + accountUrl)
  backup push          Encrypt and upload a profile (--profile required)
  backup estimate      Report the upload size a push of a profile would use (--profile required)
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
	envelopeRunID string
	jsonMode      bool
	debugCLI      bool
	helpRequested bool
	events        string // "jsonl" or ""

	// Per-command flags
	manifest           string
	bundle             string
	bundleMissingValue bool
	dryRun             bool
	enableRestore      bool
	export             string   // --export <path>
	restoreFilter      string   // --restore-filter <expr>
	restoreTargets     []string // repeatable --restore-target <captureId>=<targetInstanceId>

	// Rebuild flags
	from      string // rebuild --from <bundle.zip|manifest.jsonc>; import --from <source>
	noRestore bool   // rebuild --no-restore: install without restoring configuration

	// Import flags
	path string // import --path <bundle.ubundle>: source file to import

	// Capture flags
	out                string
	name               string
	profile            string
	sanitize           bool
	discover           bool
	update             bool
	includeRuntimes    bool
	includeStoreApps   bool
	excludeStoreApps   bool
	minimize           bool
	pin                bool
	share              bool
	drivers            []string
	driverMissingValue bool // --driver was present without a following value

	// Report flags
	latest bool
	last   int
	runID  string

	// Backup / account flags
	email             string
	token             string
	backupID          string
	versionID         string
	to                string
	confirm           bool
	prune             bool   // apply --prune: converge to the exact declared set
	repin             bool   // apply --repin: reinstall a drifted declared App.Version
	bootstrapBackends bool   // apply --bootstrap-backends: install an absent backend (consented)
	noBootstrap       bool   // apply --no-bootstrap: skip an absent backend rather than install
	only              string // apply --only: comma-separated app id subset to process
	onlyMissingValue  bool   // --only was present but had no value (next token was a flag or EOL)
	saveRecoveryTo    string
	overwrite         bool
	ifChanged         bool

	// Schedule flags
	scheduleInterval string // --interval daily|weekly
	scheduleTime     string // --time HH:MM
	autoPush         bool   // --auto-push
	root             string // --root <path> (ENDSTATE_ROOT override for schedule run)

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
		case "--exclude-store-apps":
			p.excludeStoreApps = true
		case "--minimize":
			p.minimize = true
		case "--pin":
			p.pin = true
		case "--share":
			p.share = true
		case "--driver":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				p.drivers = append(p.drivers, args[i+1])
				i++
			} else {
				p.driverMissingValue = true
			}
		case "--latest":
			p.latest = true
		case "--confirm":
			p.confirm = true
		case "--prune":
			p.prune = true
		case "--repin":
			p.repin = true
		case "--bootstrap-backends":
			p.bootstrapBackends = true
		case "--no-bootstrap":
			p.noBootstrap = true
		case "--no-restore":
			p.noRestore = true
		case "--only":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				p.only = args[i+1]
				i++
			} else {
				// --only with no value (last arg or next token is another flag).
				// Leave p.only empty and set the missing-value flag so dispatch
				// can return a proper validation error instead of silently no-oping.
				p.onlyMissingValue = true
			}
		case "--overwrite":
			p.overwrite = true
		case "--if-changed":
			p.ifChanged = true
		case "--auto-push":
			p.autoPush = true
		case "--interval":
			if i+1 < len(args) {
				p.scheduleInterval = args[i+1]
				i++
			}
		case "--time":
			if i+1 < len(args) {
				p.scheduleTime = args[i+1]
				i++
			}
		case "--root":
			if i+1 < len(args) {
				p.root = args[i+1]
				i++
			}
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
		case "--bundle":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				p.bundle = args[i+1]
				i++
			} else {
				p.bundleMissingValue = true
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
		case "--restore-target":
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				p.restoreTargets = append(p.restoreTargets, args[i+1])
				i++
			} else {
				// Preserve the occurrence so command-level validation can return
				// INVALID_RESTORE_TARGET after loading the known capture IDs.
				p.restoreTargets = append(p.restoreTargets, "")
			}
		case "--from":
			if i+1 < len(args) {
				p.from = args[i+1]
				i++
			}
		case "--path":
			if i+1 < len(args) {
				p.path = args[i+1]
				i++
			}
		case "--email":
			if i+1 < len(args) {
				p.email = args[i+1]
				i++
			}
		case "--token":
			if i+1 < len(args) {
				p.token = args[i+1]
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
		return "Usage: endstate apply [--manifest <path>] [--dry-run] [--enable-restore] [--restore-filter <expr>] [--restore-target <captureId>=<targetInstanceId>] [--only <id[,id...]>] [--prune] [--repin] [--confirm] [--bootstrap-backends] [--no-bootstrap] [--json] [--events jsonl]\n\nExecute provisioning plan. --restore-target is repeatable and selects a detected target instance for one generation-aware capture; --restore-filter remains the module-level filter and takes precedence. With --only, limit the run to the comma-separated list of manifest app ids (filtering happens before planning so only the selected apps are installed, restored, and verified). With --prune, converge the engine-managed set to exactly the manifest by removing installed-but-undeclared packages (realizer backends only, e.g. Nix on Linux/macOS). With --repin, reinstall a declared app version when the installed version has drifted from it (supported versioned drivers). --prune and --repin both require --confirm to execute; use --dry-run to preview what would change. --only and --prune cannot be combined. When a selected optional package backend is absent, --bootstrap-backends authorizes the engine to install it via its official installer; --no-bootstrap forces skipping it. Without either flag the engine skips the lane and requests consent.\n"
	case "rebuild":
		return "Usage: endstate rebuild --from <bundle.zip|manifest.jsonc> [--only <id[,id...]>] [--dry-run] [--confirm] [--no-restore] [--restore-filter <expr>] [--restore-target <captureId>=<targetInstanceId>] [--bootstrap-backends] [--no-bootstrap] [--json] [--events jsonl]\n\nRebuild a machine from a capture bundle (.zip) or a bare manifest (.jsonc): install the declared apps, restore configuration, then verify. With --only, limit the rebuild to the listed app ids so a recipient can take part of a shared setup; the selection scopes installs, config restore, and verification alike. --restore-target is repeatable and selects a detected target instance for one generation-aware capture; --restore-filter remains the module-level filter and takes precedence. Restore is ON by default, so a live run (not --dry-run, not --no-restore) requires --confirm. Use --dry-run to preview the plan without changing anything, or --no-restore to install and verify without touching configuration. Backend-bootstrap flags are propagated to apply. Overwritten files are backed up first and can be undone with 'endstate revert'. Local file input only — URL input is not supported.\n"
	case "import":
		return "Usage: endstate import --from unigetui --path <file> [--out <path>] [--pin] [--json]\n\nImport an external package list into an Endstate manifest. The only supported source is 'unigetui' (a UniGetUI .ubundle backup/bundle, JSON). Winget-source packages become manifest apps; every non-winget package (chocolatey, scoop, pip, ...) and every incompatible entry is reported, never silently dropped. With --pin, the app version is recorded (a package's InstallationOptions.Version pin wins over the observed version). Output defaults to manifests/local/imported-unigetui.jsonc (gitignored). This is a pure transform: no installs, no network. Scope: the package list only — UniGetUI's own settings are not imported; Endstate's module catalog restores app config on plan/apply.\n"
	case "verify":
		return "Usage: endstate verify [--manifest <path>] [--json] [--events jsonl]\n\nVerify machine state against manifest.\n"
	case "capture":
		return "Usage: endstate capture [--only <id[,id...]>] [--share] [--discover] [--sanitize] [--name <name>] [--out <path>] [--profile <name>] [--manifest <path>] [--update] [--include-runtimes] [--include-store-apps] [--exclude-store-apps] [--minimize] [--pin] [--driver <name>]... [--json] [--events jsonl]\n\nCapture current machine state. Microsoft Store apps are included by default; --include-store-apps is a deprecated compatibility no-op and --exclude-store-apps wins when both are supplied. Repeat --driver to select more than one package driver. With --only, capture just the listed items: a bare id selects a detected app, an 'apps.'-prefixed id selects a config module (e.g. --only git-git,apps.vscode). Under --only, a config module attaches only when a selected app matches it by package reference or the module is named outright, so unselected apps' settings are never bundled. Combining --only with --update adds the selection to an existing manifest rather than truncating it. With --share, the bundle is produced for someone else: config restore prefers merging onto the recipient's existing settings rather than replacing them, and the capturing machine name is omitted. --share requires --only and cannot be combined with --sanitize.\n"
	case "plan":
		return "Usage: endstate plan --manifest <path> [--json] [--events jsonl]\n\nGenerate execution plan.\n"
	case "catalog-plan":
		return "Usage: endstate catalog-plan --bundle <tracked-bundle-path> [--json] [--events jsonl]\n\nResolve one tracked catalog bundle into ordered module actions without package installation or mutation.\n"
	case "restore":
		return "Usage: endstate restore [--manifest <path>] [--enable-restore] [--dry-run] [--export <path>] [--restore-filter <expr>] [--restore-target <captureId>=<targetInstanceId>] [--json] [--events jsonl]\n\nRestore configuration files. --restore-target is repeatable and selects a detected target instance for one generation-aware capture; --restore-filter remains the module-level filter and takes precedence.\n"
	case "revert":
		return "Usage: endstate revert [--json] [--events jsonl]\n\nRevert last restore operation using journal.\n"
	case "export-config":
		return "Usage: endstate export-config [--manifest <path>] [--export <path>] [--dry-run] [--json] [--events jsonl]\n\nExport configuration files from system to portable directory.\n"
	case "validate-export":
		return "Usage: endstate validate-export [--manifest <path>] [--export <path>] [--json] [--events jsonl]\n\nValidate export directory completeness.\n"
	case "report":
		return "Usage: endstate report [--latest] [--last <n>] [--run-id <id>] [--json]\n\nRetrieve run history.\n"
	case "rollback":
		return "Usage: endstate rollback [--to <generation>] [--enable-restore] [--confirm] [--dry-run] [--json] [--events jsonl]\n\nRoll back selected provisioning generations using each recorded backend: native generation rollback for Nix and best-effort uninstall for Winget, Chocolatey, and Homebrew. With --to, targets that engine generation number (see 'endstate generations'); without it, rolls back the newest non-rollback run group. With --enable-restore (and --to), ALSO reverts the home-manager configuration recorded in that generation (symmetric with 'apply --enable-restore'). Requires --confirm; use --dry-run to preview without changing anything.\n"
	case "doctor":
		return "Usage: endstate doctor [--json]\n\nRun system diagnostics.\n"
	case "profile":
		return "Usage: endstate profile <subcommand> [args] [--json]\n\nSubcommands:\n  list              List discovered profiles\n  path <name>       Resolve profile path from name\n  validate <path>   Validate a profile manifest\n"
	case "backup":
		return "Usage: endstate backup <subcommand> [flags] [--json] [--events jsonl]\n\nSubcommands:\n  signup --email <addr> --save-recovery-to <path>\n                              Create account (passphrase + optional 24-word phrase via stdin)\n  claim --token <token> --save-recovery-to <path>\n                              Attach credentials to a pre-account using the bearer claim token\n                              from the buyer's purchase email (passphrase via stdin).\n                              Replaces any existing local session on success.\n  login --email <addr>          Sign in (passphrase via stdin)\n  logout                        Clear local session\n  status                        Report current session state\n  subscribe                     Start a Hosted Backup subscription checkout (returns checkoutUrl for the GUI to open)\n  browser-session               Mint a 60s /account portal handoff token (returns sessionToken + accountUrl for the GUI to open)\n  push --profile <path> [--backup-id <id>] [--name <label>]\n                              Encrypt and upload a profile\n  pull --backup-id <id> --to <path> [--version-id <id>] [--overwrite]\n                              Download and restore a profile\n  list                          List backups\n  versions --backup-id <id>     List versions of a backup\n  delete --backup-id <id> --confirm\n                              Permanently delete a backup\n  delete-version --backup-id <id> --version-id <id> --confirm\n                              Soft-delete a backup version\n  recover --email <addr>        Reset passphrase using recovery phrase (stdin: phrase, then new passphrase)\n\nEnv vars:\n  ENDSTATE_OIDC_ISSUER_URL    Backend issuer URL (default: https://substratesystems.io)\n  ENDSTATE_OIDC_AUDIENCE      JWT audience (default: endstate-backup)\n  ENDSTATE_BACKUP_CONCURRENCY Worker pool size for chunk transfer (default 4, clamp 1..16)\n"
	case "account":
		return "Usage: endstate account <subcommand> [flags] [--json]\n\nSubcommands:\n  delete --confirm  Delete the Hosted Backup account permanently\n"
	case "bootstrap":
		return "Usage: endstate bootstrap\n\nBootstrap Endstate installation.\n"
	case "schedule":
		return "Usage: endstate schedule <subcommand> [flags] [--json]\n\nSubcommands:\n  enable --manifest <path> [--interval daily|weekly] [--time HH:MM] [--auto-push]\n                              Register the drift-check scheduled task (Windows only)\n  disable                     Remove the drift-check scheduled task\n  status                      Report schedule config and last-run outcome\n  run [--root <path>]         Execute drift-check now; write last-run.json; exit 0 on drift\n"
	default:
		return usageText
	}
}

func main() {
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

var dispatchFn = dispatch
var activateCommandValidationModeFn = commands.ActivateValidationMode

var validationModeCommands = map[string]struct{}{
	"capture": {}, "plan": {}, "apply": {}, "rebuild": {},
	"restore": {}, "verify": {}, "revert": {},
}

var knownCommands = map[string]struct{}{
	"capabilities": {}, "apply": {}, "rebuild": {}, "import": {}, "verify": {},
	"capture": {}, "plan": {}, "report": {}, "generations": {}, "rollback": {},
	"catalog-plan": {},
	"doctor":       {}, "profile": {}, "restore": {}, "revert": {}, "export-config": {},
	"validate-export": {}, "bootstrap": {}, "backup": {}, "account": {}, "schedule": {},
}

// runCLI is the process entrypoint without os.Exit. Keeping cleanup visible
// makes validation-mode environment and command-seam restoration testable.
func runCLI(args []string, stdout, stderr io.Writer) int {
	p := parseArgs(args)
	validationContext, activationErr := validationmode.LoadFromEnvironment()
	var validationSession *commands.ValidationModeSession
	if activationErr != nil {
		return renderCLIResult(p, nil, envelope.NewError(
			envelope.ErrTestModeInvalid,
			"Validation mode activation is invalid.",
		), nil, stdout, stderr)
	}

	runStderr := stderr
	if validationContext != nil {
		runStderr = validationRedactingWriter{
			target: stderr,
			roots:  validationOutputRoots(validationContext),
		}
	}

	// Debug output is emitted only after validation-mode activation has been
	// parsed and, when active, through the same root-redacting boundary as
	// command events.
	if p.debugCLI {
		fmt.Fprintf(runStderr, "[debug-cli] command=%q json=%v dryRun=%v enableRestore=%v manifest=%q events=%q\n",
			p.command, p.jsonMode, p.dryRun, p.enableRestore, p.manifest, p.events)
	}

	// Handle --help / no command before doing any further work.
	if p.helpRequested || p.command == "" {
		fmt.Fprint(stdout, commandUsage(p.command))
		return 0
	}

	if validationContext != nil {
		if _, allowed := validationModeCommands[p.command]; !allowed {
			return renderCLIResult(p, nil, envelope.NewError(
				envelope.ErrTestModeCommandForbidden,
				fmt.Sprintf("Command %q is forbidden in validation mode.", p.command),
			), validationContext, stdout, runStderr)
		}
		if p.command == "capture" && len(p.drivers) == 0 {
			p.drivers = []string{validationContext.Descriptor().Inventory.Driver}
		}
		// Validation mode never authorizes backend installation. The command-layer
		// package seams still report the disposable backend as available.
		p.bootstrapBackends = false
		p.noBootstrap = true
	}

	if _, known := knownCommands[p.command]; !known {
		fmt.Fprintf(runStderr, "Unknown command: %q\n\n", p.command)
		fmt.Fprint(stdout, usageText)
		return 1
	}

	var restoreEnvironment func() error
	if validationContext != nil {
		restoreEnvironment, activationErr = validationContext.Activate()
		if activationErr == nil {
			validationSession, activationErr = activateCommandValidationModeFn(validationContext)
		}
		if activationErr != nil {
			if restoreEnvironment != nil {
				_ = restoreEnvironment()
			}
			return renderCLIResult(p, nil, envelope.NewError(
				envelope.ErrTestModeInvalid,
				"Validation mode activation is invalid.",
			), nil, stdout, runStderr)
		}
		defer func() {
			_ = validationSession.Restore()
			_ = restoreEnvironment()
		}()
		restoreDefaultEvents := events.ActivateDefaultWriter(runStderr)
		defer restoreDefaultEvents()
	}

	p.envelopeRunID = envelope.BuildRunID(p.command, time.Now().UTC())
	data, cmdErr := dispatchFn(p)
	if validationSession != nil && validationSession.IsolationError() != nil {
		data = nil
		cmdErr = envelope.NewError(
			envelope.ErrTestModeIsolationViolation,
			"Validation mode rejected an operation outside its disposable authority.",
		)
	}
	return renderCLIResult(p, data, cmdErr, validationContext, stdout, runStderr)
}

func renderCLIResult(p parsedArgs, data interface{}, cmdErr *envelope.Error, validationContext *validationmode.Context, stdout, stderr io.Writer) int {
	if validationContext != nil {
		roots := []string{validationContext.Root(), os.Getenv(validationmode.RootEnvironment)}
		data = redactCLIValue(data, roots)
		cmdErr = redactCLIError(cmdErr, roots)
	}
	repoRoot := ""
	if validationContext != nil || os.Getenv(validationmode.TestModeEnvironment) == "" {
		repoRoot = config.ResolveRepoRoot()
	}

	// Resolve versions from repo root (best-effort; falls back gracefully).
	schemaVersion := config.ReadSchemaVersion(repoRoot)
	cliVersion := config.ReadVersion(repoRoot)

	now := time.Now().UTC()
	runID := p.envelopeRunID
	if runID == "" {
		runID = envelope.BuildRunID(p.command, now)
	}

	if p.jsonMode {
		var env *envelope.Envelope
		if cmdErr != nil {
			env = envelope.NewFailureWithData(p.command, runID, schemaVersion, cliVersion, data, cmdErr)
		} else {
			env = envelope.NewSuccess(p.command, runID, schemaVersion, cliVersion, data)
		}
		if validationContext != nil {
			descriptor := validationContext.Descriptor()
			env.TestMode = &envelope.TestModeIdentity{
				Active: true, ScenarioID: descriptor.ScenarioID, ModuleID: descriptor.ModuleID,
			}
		}

		b, marshalErr := envelope.Marshal(env)
		if marshalErr != nil {
			// Last-resort: write a minimal error envelope manually.
			fmt.Fprintf(stdout, `{"schemaVersion":%q,"cliVersion":%q,"command":%q,"runId":%q,"timestampUtc":%q,"success":false,"data":{},"error":{"code":"INTERNAL_ERROR","message":"failed to marshal response"}}`,
				schemaVersion, cliVersion, p.command, runID, now.Format(time.RFC3339))
			fmt.Fprintln(stdout)
			return 1
		}
		// The JSON envelope is the LAST line of stdout.
		fmt.Fprintln(stdout, string(b))
	} else {
		// Human-readable output.
		if cmdErr != nil {
			fmt.Fprintf(stderr, "Error [%s]: %s\n", cmdErr.Code, cmdErr.Message)
			if cmdErr.Remediation != "" {
				fmt.Fprintf(stderr, "Remediation: %s\n", cmdErr.Remediation)
			}
			return 1
		}
		// For commands with non-JSON output, pretty-print data as indented JSON as a
		// readable fallback until each command has a bespoke human formatter.
		if data != nil {
			b, _ := json.MarshalIndent(data, "", "  ")
			fmt.Fprintln(stdout, string(b))
		}
	}

	if cmdErr != nil {
		return 1
	}
	return 0
}

func redactCLIError(value *envelope.Error, roots []string) *envelope.Error {
	if value == nil {
		return nil
	}
	result := *value
	result.Message = redactCLIString(result.Message, roots)
	result.Remediation = redactCLIString(result.Remediation, roots)
	result.Detail = redactCLIValue(result.Detail, roots)
	return &result
}

func redactCLIValue(value interface{}, roots []string) interface{} {
	if value == nil {
		return nil
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var decoded interface{}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.UseNumber()
	if err := decoder.Decode(&decoded); err != nil {
		return value
	}
	return redactDecodedCLIValue(decoded, roots)
}

func redactDecodedCLIValue(value interface{}, roots []string) interface{} {
	switch typed := value.(type) {
	case string:
		return redactCLIString(typed, roots)
	case []interface{}:
		for index := range typed {
			typed[index] = redactDecodedCLIValue(typed[index], roots)
		}
	case map[string]interface{}:
		for key, item := range typed {
			typed[key] = redactDecodedCLIValue(item, roots)
		}
	}
	return value
}

func redactCLIString(value string, roots []string) string {
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		plainForms := []string{root, strings.ReplaceAll(root, `\`, "/"), strings.ReplaceAll(root, "/", `\`)}
		forms := make([]string, 0, len(plainForms)*2)
		for _, form := range plainForms {
			forms = append(forms, strings.ReplaceAll(form, `\`, `\\`))
		}
		forms = append(forms, plainForms...)
		for _, form := range forms {
			value = replaceFold(value, form, "$ENDSTATE_ROOT")
		}
	}
	return value
}

func validationOutputRoots(context *validationmode.Context) []string {
	if context == nil {
		return nil
	}
	return []string{context.Root(), os.Getenv(validationmode.RootEnvironment)}
}

type validationRedactingWriter struct {
	target io.Writer
	roots  []string
}

func (w validationRedactingWriter) Write(p []byte) (int, error) {
	redacted := redactCLIString(string(p), w.roots)
	written, err := io.WriteString(w.target, redacted)
	if err != nil {
		return 0, err
	}
	if written != len(redacted) {
		return 0, io.ErrShortWrite
	}
	return len(p), nil
}

func replaceFold(value, old, replacement string) string {
	if old == "" {
		return value
	}
	for {
		index := strings.Index(strings.ToLower(value), strings.ToLower(old))
		if index < 0 {
			return value
		}
		value = value[:index] + replacement + value[index+len(old):]
	}
}

// dispatch routes the parsed command to its handler and returns the data payload
// or an envelope error.
var (
	runCaptureFn = commands.RunCapture
	runRebuildFn = commands.RunRebuild
)

func dispatch(p parsedArgs) (interface{}, *envelope.Error) {
	switch p.command {
	case "capabilities":
		return commands.RunCapabilities()

	case "apply":
		if p.onlyMissingValue {
			return nil, envelope.NewError(
				envelope.ErrManifestValidationError,
				"--only requires a value: no app id was provided").
				WithRemediation("Provide one or more comma-separated app ids, e.g. --only git,vscode.")
		}
		return commands.RunApply(commands.ApplyFlags{
			Manifest:          p.manifest,
			DryRun:            p.dryRun,
			EnableRestore:     p.enableRestore,
			Events:            p.events,
			Export:            p.export,
			RestoreFilter:     p.restoreFilter,
			RestoreTargets:    append([]string(nil), p.restoreTargets...),
			Prune:             p.prune,
			Confirm:           p.confirm,
			Repin:             p.repin,
			BootstrapBackends: p.bootstrapBackends,
			NoBootstrap:       p.noBootstrap,
			Only:              p.only,
		})

	case "rebuild":
		if p.onlyMissingValue {
			return nil, envelope.NewError(
				envelope.ErrManifestValidationError,
				"--only requires a value: no app id was provided").
				WithRemediation("Provide one or more comma-separated app ids, e.g. --only git-git.")
		}
		return runRebuildFn(commands.RebuildFlags{
			From:              p.from,
			DryRun:            p.dryRun,
			Confirm:           p.confirm,
			NoRestore:         p.noRestore,
			Events:            p.events,
			RestoreFilter:     p.restoreFilter,
			RestoreTargets:    append([]string(nil), p.restoreTargets...),
			BootstrapBackends: p.bootstrapBackends,
			NoBootstrap:       p.noBootstrap,
			Only:              p.only,
		})

	case "import":
		return commands.RunImport(commands.ImportFlags{
			From: p.from,
			Path: p.path,
			Out:  p.out,
			Pin:  p.pin,
		})

	case "verify":
		return commands.RunVerify(commands.VerifyFlags{
			Manifest: p.manifest,
			Events:   p.events,
		})

	case "capture":
		if p.driverMissingValue {
			return nil, envelope.NewError(
				envelope.ErrManifestValidationError,
				"--driver requires a package driver name").
				WithRemediation("Provide a driver name, e.g. --driver winget or --driver chocolatey.")
		}
		if p.onlyMissingValue {
			return nil, envelope.NewError(
				envelope.ErrManifestValidationError,
				"--only requires a value: no id was provided").
				WithRemediation("Provide one or more comma-separated ids, e.g. --only git-git,apps.vscode.")
		}
		return runCaptureFn(commands.CaptureFlags{
			Manifest:         p.manifest,
			Out:              p.out,
			Profile:          p.profile,
			Name:             p.name,
			Sanitize:         p.sanitize,
			Discover:         p.discover,
			Update:           p.update,
			IncludeRuntimes:  p.includeRuntimes,
			IncludeStoreApps: p.includeStoreApps,
			ExcludeStoreApps: p.excludeStoreApps,
			Minimize:         p.minimize,
			Pin:              p.pin,
			Drivers:          p.drivers,
			Events:           p.events,
			Only:             p.only,
			Share:            p.share,
		})

	case "plan":
		return commands.RunPlan(commands.PlanFlags{
			Manifest: p.manifest,
			Events:   p.events,
		})

	case "catalog-plan":
		if p.bundleMissingValue || p.bundle == "" {
			return nil, envelope.NewError(
				envelope.ErrCatalogPlanInvalid,
				"--bundle requires a tracked bundle path.").
				WithRemediation("Provide a path such as bundles/dev-tools.jsonc.")
		}
		return commands.RunCatalogPlan(commands.CatalogPlanFlags{
			Bundle: p.bundle,
			Events: p.events,
			RunID:  p.envelopeRunID,
		})

	case "report":
		return commands.RunReport(commands.ReportFlags{
			Latest: p.latest,
			Last:   p.last,
			RunID:  p.runID,
			Events: p.events,
		})

	case "generations":
		return commands.RunGenerations(commands.GenerationsFlags{
			Events: p.events,
		})

	case "rollback":
		return commands.RunRollback(commands.RollbackFlags{
			To:            p.to,
			Confirm:       p.confirm,
			DryRun:        p.dryRun,
			EnableRestore: p.enableRestore,
			Events:        p.events,
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
			Manifest:       p.manifest,
			EnableRestore:  p.enableRestore,
			DryRun:         p.dryRun,
			Export:         p.export,
			Events:         p.events,
			RestoreFilter:  p.restoreFilter,
			RestoreTargets: append([]string(nil), p.restoreTargets...),
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
			Token:          p.token,
			BackupID:       p.backupID,
			VersionID:      p.versionID,
			Profile:        p.profile,
			Name:           p.name,
			IfChanged:      p.ifChanged,
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

	case "schedule":
		subcommand := ""
		if len(p.positionalArgs) > 0 {
			subcommand = p.positionalArgs[0]
		}
		return commands.RunSchedule(commands.ScheduleFlags{
			Subcommand: subcommand,
			Manifest:   p.manifest,
			Interval:   p.scheduleInterval,
			Time:       p.scheduleTime,
			AutoPush:   p.autoPush,
			Root:       p.root,
			JSON:       p.jsonMode,
		})

	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %q\n\n", p.command)
		fmt.Print(usageText)
		os.Exit(1)
		return nil, nil // unreachable
	}
}
