// THROWAWAY SPIKE — not shippable, not wired into the engine.
//
// Purpose: answer the one open question that "resolves via spike, not
// reasoning" (KB: endstate-cross-os-expansion-is-an-engine-substrate-decision):
//
//	Is Nix's steady-state error surface BOUNDED and TRANSLATABLE — can each
//	failure class be mapped to one stable engine error.code?
//
// It measures, per failure class, whether classification is possible from
// STRUCTURAL signals alone (exit code + internal-json action/level/activity-type
// + did-the-generation-advance) or whether it needs the UNSTABLE human `msg`
// text. Each case self-scores BOUNDED / PARTIAL / UNBOUNDED:
//
//	BOUNDED   = structural-only classifier already returns the expected code
//	PARTIAL   = only the msg-anchor classifier returns it (works today, fragile)
//	UNBOUNDED = neither returns it
//
// Stdlib only, on purpose: this proves a *Go* program can consume Nix's streams.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Engine error codes — mirror the LOCKED taxonomy in
// docs/contracts/cli-json-contract.md §"Standard Error Codes".
// REALIZER_UNAVAILABLE is the proposed additive code (Nix analogue of
// WINGET_NOT_AVAILABLE).
type EngineCode string

const (
	CodeOK                  EngineCode = "OK"
	CodeInstallFailed       EngineCode = "INSTALL_FAILED"
	CodePermissionDenied    EngineCode = "PERMISSION_DENIED"
	CodeRealizerUnavailable EngineCode = "REALIZER_UNAVAILABLE" // proposed (additive)
	CodeInternalError       EngineCode = "INTERNAL_ERROR"
	CodeUnknown             EngineCode = "UNKNOWN" // spike-only: classifier gave up
)

// nixEvent is one parsed `@nix {...}` internal-json line. The `msg` TEXT is
// explicitly NOT stable across Nix releases — recorded, but the structural
// classifier must not use it.
type nixEvent struct {
	Action string            `json:"action"`
	Level  int               `json:"level"` // 0=Error 1=Warn 2=Notice 3=Info 4=Talkative 6=Debug
	Msg    string            `json:"msg"`
	RawMsg string            `json:"raw_msg"`
	ID     uint64            `json:"id"`
	Type   int               `json:"type"` // ActivityType
	Text   string            `json:"text"`
	Parent uint64            `json:"parent"`
	File   *string           `json:"file"` // populated for eval errors (position info)
	Line   *int              `json:"line"`
	Fields []json.RawMessage `json:"fields"`
}

// ActivityType subset (Nix src/libutil/logging.hh), confirmed against real
// Determinate Nix 3.21.0 output during the smoke run.
const (
	actFileTransfer = 101
	actRealise      = 102
	actCopyPaths    = 103
	actBuilds       = 104
	actBuild        = 105
	actSubstitute   = 108
)

func activityName(t int) string {
	switch t {
	case actFileTransfer:
		return "FileTransfer"
	case actRealise:
		return "Realise"
	case actCopyPaths, 100:
		return "CopyPath(s)"
	case actBuilds, actBuild:
		return "Build"
	case actSubstitute:
		return "Substitute"
	case 109:
		return "QueryPathInfo"
	default:
		return "type" + strconv.Itoa(t)
	}
}

type msgRecord struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type structuralSignals struct {
	ExitCode           int            `json:"exitCode"`
	ErrorMsgCount      int            `json:"errorMsgCount"` // action=msg, level=0
	StartedActivities  map[string]int `json:"startedActivities"`
	SawBuild           bool           `json:"sawBuild"`
	SawDownload        bool           `json:"sawDownload"`        // FileTransfer/Substitute
	HasEvalPosition    bool           `json:"hasEvalPosition"`    // an error msg carried file/line
	GenerationAdvanced bool           `json:"generationAdvanced"`
}

type runResult struct {
	Case     string            `json:"case"`
	Expected EngineCode        `json:"expected"`
	Argv     []string          `json:"argv"`
	ExtraEnv []string          `json:"extraEnv,omitempty"`
	ExitCode int               `json:"exitCode"`
	SpawnErr string            `json:"spawnErr,omitempty"`
	GenBefore int              `json:"genBefore"`
	GenAfter  int              `json:"genAfter"`
	Signals  structuralSignals `json:"signals"`

	// The two classifiers and the verdict.
	StructuralClass  EngineCode `json:"structuralClass"`  // exit+activities+gen only
	StructuralReason string     `json:"structuralReason"`
	AnchoredClass    EngineCode `json:"anchoredClass"` // msg-text anchors
	AnchoredReason   string     `json:"anchoredReason"`
	Score            string     `json:"score"` // BOUNDED|PARTIAL|UNBOUNDED

	ErrorMsgs []msgRecord `json:"errorMsgs"` // level<=1, ANSI-stripped, for analysis
	RawStderr string      `json:"-"`
	Events    []nixEvent  `json:"-"`
}

// ---------------------------------------------------------------------------

var nixBin = "nix"
var expFeatures = []string{"--extra-experimental-features", "nix-command flakes"}
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func runNix(extraEnv []string, args ...string) (exitCode int, events []nixEvent, rawStderr, stdout string, spawnErr error) {
	full := append([]string{}, args...)
	full = append(full, expFeatures...)
	full = append(full, "--log-format", "internal-json")

	cmd := exec.Command(nixBin, full...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	runErr := cmd.Run()
	rawStderr, stdout = errBuf.String(), outBuf.String()
	events = parseInternalJSON(rawStderr)

	switch e := runErr.(type) {
	case nil:
		exitCode = 0
	case *exec.ExitError:
		exitCode = e.ExitCode()
	default:
		exitCode = -1
		spawnErr = runErr
	}
	return
}

func parseInternalJSON(stderr string) []nixEvent {
	var evs []nixEvent
	sc := bufio.NewScanner(strings.NewReader(stderr))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "@nix ") {
			continue
		}
		var ev nixEvent
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "@nix ")), &ev); err != nil {
			continue
		}
		evs = append(evs, ev)
	}
	return evs
}

// currentGeneration reads the profile symlink: <p> -> <p>-<N>-link.
func currentGeneration(profile string) int {
	target, err := os.Readlink(profile)
	if err != nil {
		return 0
	}
	base := strings.TrimSuffix(filepath.Base(target), "-link")
	if idx := strings.LastIndex(base, "-"); idx >= 0 {
		if n, err := strconv.Atoi(base[idx+1:]); err == nil {
			return n
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// Two classifiers.
// ---------------------------------------------------------------------------

// classifyStructural uses ONLY (exit code, started activities, generation
// advanced, eval-position-present). No msg text. This is the BOUNDED test.
func classifyStructural(r *runResult) (EngineCode, string) {
	s := r.Signals
	if r.SpawnErr != "" {
		return CodeRealizerUnavailable, "spawn error: nix binary missing/unrunnable"
	}
	if s.ExitCode == 0 {
		return CodeOK, "exit 0"
	}
	switch {
	case len(s.StartedActivities) == 0 && s.ErrorMsgCount > 0:
		// eval(cached) / daemon-down / early-permission all look identical here.
		return CodeUnknown, "no activity started + error: class is structurally ambiguous"
	case s.SawBuild && !s.GenerationAdvanced:
		// Build/realise ran but no new generation. CANNOT distinguish a true
		// build/store failure from a post-build commit/permission failure.
		return CodeInstallFailed, "build/realise started, generation did not advance (NOTE: cannot separate build-fail vs commit/permission-fail)"
	case s.SawDownload && !s.SawBuild:
		// eval-after-fetch and genuine network failure are indistinguishable.
		return CodeInstallFailed, "fetch/substitute phase, no build (eval-after-fetch OR network — indistinguishable)"
	default:
		return CodeUnknown, "non-zero exit, no decisive structural signal"
	}
}

// anchorPatterns: stable-ish Nix message phrases observed in real output. Order
// matters (most specific first). These are what a YELLOW design would maintain
// and re-validate per Nix version.
var anchorPatterns = []struct {
	needle string
	code   EngineCode
	note   string
}{
	{"permission denied", CodePermissionDenied, "permission"},
	{"read-only file system", CodePermissionDenied, "permission"},
	{"opening a connection to remote store", CodeRealizerUnavailable, "daemon/store-connection"},
	{"cannot connect to socket", CodeRealizerUnavailable, "daemon/store-connection"},
	{"connection refused", CodeRealizerUnavailable, "daemon/store-connection"},
	{"does not provide attribute", CodeInstallFailed, "eval"},
	{"undefined variable", CodeInstallFailed, "eval"},
	{"is not a valid", CodeInstallFailed, "eval"},
	{"collision between", CodeInstallFailed, "collision"},
	{"files in conflict", CodeInstallFailed, "collision"},
	{"conflict between", CodeInstallFailed, "collision"},
	{"no space left", CodeInstallFailed, "store/disk"},
	{"unable to download", CodeInstallFailed, "network"},
	{"http error", CodeInstallFailed, "network"},
	{"couldn't resolve host", CodeInstallFailed, "network"},
	{"while fetching", CodeInstallFailed, "network"},
}

// classifyAnchored matches stable message phrases against level<=1 msgs.
func classifyAnchored(r *runResult) (EngineCode, string) {
	if r.ExitCode == 0 {
		return CodeOK, "exit 0"
	}
	var sb strings.Builder
	for _, m := range r.ErrorMsgs {
		sb.WriteString(strings.ToLower(m.Text))
		sb.WriteByte('\n')
	}
	blob := sb.String()
	for _, p := range anchorPatterns {
		if strings.Contains(blob, p.needle) {
			return p.code, "anchor '" + p.needle + "' -> " + p.note
		}
	}
	return CodeUnknown, "no known anchor matched"
}

func computeSignals(r *runResult) {
	s := structuralSignals{ExitCode: r.ExitCode, StartedActivities: map[string]int{}}
	for _, ev := range r.Events {
		switch ev.Action {
		case "msg":
			if ev.Level <= 1 && (ev.Msg != "" || ev.RawMsg != "") {
				txt := ev.Msg
				if txt == "" {
					txt = ev.RawMsg
				}
				r.ErrorMsgs = append(r.ErrorMsgs, msgRecord{Level: ev.Level, Text: stripANSI(txt)})
			}
			if ev.Level == 0 {
				s.ErrorMsgCount++
				if ev.File != nil || ev.Line != nil {
					s.HasEvalPosition = true
				}
			}
		case "start":
			s.StartedActivities[activityName(ev.Type)]++
			if ev.Type == actBuild || ev.Type == actBuilds || ev.Type == actRealise {
				s.SawBuild = true
			}
			if ev.Type == actFileTransfer || ev.Type == actSubstitute {
				s.SawDownload = true
			}
		}
	}
	s.GenerationAdvanced = r.GenAfter > r.GenBefore
	r.Signals = s
}

// ---------------------------------------------------------------------------
// Cases.
// ---------------------------------------------------------------------------

type spikeCase struct {
	name     string
	expected EngineCode
	invasive bool
	build    func(profile, flake string) (extraEnv, args []string)
	note     string
}

func install(profile string, installables ...string) []string {
	return append([]string{"profile", "install", "--profile", profile}, installables...)
}

func buildCases() []spikeCase {
	return []spikeCase{
		{
			name: "0-happy-hello", expected: CodeOK,
			build: func(p, f string) ([]string, []string) { return nil, install(p, f+"#hello") },
		},
		{
			name: "1-eval-nonexistent-attr", expected: CodeInstallFailed,
			build: func(p, f string) ([]string, []string) {
				return nil, install(p, f+"#thispackagedoesnotexist12345zz")
			},
		},
		{
			name: "2-network-bad-flake-host", expected: CodeInstallFailed,
			build: func(p, f string) ([]string, []string) {
				return nil, install(p, "github:endstate-spike-nonexistent-org/nonexistent-repo-qpz#pkg")
			},
		},
		{
			name: "3-daemon-unavailable", expected: CodeRealizerUnavailable,
			build: func(p, f string) ([]string, []string) {
				return []string{"NIX_REMOTE=unix:///nonexistent/spike-store-socket"}, install(p, f+"#hello")
			},
		},
		{
			name: "4-store-disk-full", expected: CodeInstallFailed, invasive: true,
			note: "Provoke manually (tiny full tmpfs store / concurrent gc). Run with -invasive.",
			build: func(p, f string) ([]string, []string) {
				return []string{"TMPDIR=/dev/full"}, install(p, f+"#hello")
			},
		},
		{
			name: "5-collision", expected: CodeInstallFailed,
			// uutils-coreutils-noprefix is built to provide the SAME unprefixed
			// binaries as coreutils -> guaranteed path collision in one profile.
			build: func(p, f string) ([]string, []string) {
				return nil, install(p, f+"#coreutils", f+"#uutils-coreutils-noprefix")
			},
		},
		{
			name: "6-permission-readonly-profile", expected: CodePermissionDenied,
			// the profile dir is chmod 0555 before this runs (see runCase).
			build: func(p, f string) ([]string, []string) { return nil, install(p, f+"#hello") },
		},
		{
			name: "7-atomic-partial-mix", expected: CodeInstallFailed,
			// valid + invalid in ONE install -> must fail AND leave gen at 0 (atomic).
			build: func(p, f string) ([]string, []string) {
				return nil, install(p, f+"#hello", f+"#thispackagedoesnotexist98765")
			},
		},
	}
}

func main() {
	var (
		outDir      = flag.String("out", "./artifacts", "artifacts dir (ok on /mnt/c)")
		profileBase = flag.String("profile-base", "/tmp/endstate-nix-spike", "base dir for per-case isolated profiles (keep on Linux fs)")
		flakeRef    = flag.String("flake", "nixpkgs", "base flakeref (pin to a rev for reproducibility)")
		invasive    = flag.Bool("invasive", false, "also run invasive/manual cases")
		repeat      = flag.Int("repeat", 1, "runs per case (rubric wants >=3)")
		nixPath     = flag.String("nix", "nix", "path to the nix binary")
	)
	flag.Parse()
	nixBin = *nixPath

	must(os.MkdirAll(*outDir, 0o755))
	preflight(*outDir)

	var results []*runResult
	for _, c := range buildCases() {
		if c.invasive && !*invasive {
			fmt.Printf("SKIP  %-32s (invasive; %s)\n", c.name, c.note)
			continue
		}
		for i := 0; i < *repeat; i++ {
			r := runCase(c, *profileBase, *flakeRef, *outDir, i)
			results = append(results, r)
			fmt.Printf("RUN   %-32s exit=%-3d gen %d->%d  struct=%-20s anchor=%-20s expect=%-20s %s\n",
				c.name, r.ExitCode, r.GenBefore, r.GenAfter, r.StructuralClass, r.AnchoredClass, r.Expected, r.Score)
		}
	}

	writeJSON(filepath.Join(*outDir, "results.json"), results)
	printSummary(results)
	fmt.Printf("\nWrote %d results to %s\n", len(results), filepath.Join(*outDir, "results.json"))
}

func runCase(c spikeCase, profileBase, flake, outDir string, iter int) *runResult {
	// Fresh isolated profile per case (no cross-case contamination).
	caseDir := filepath.Join(profileBase, c.name)
	_ = os.RemoveAll(caseDir)
	must(os.MkdirAll(caseDir, 0o755))
	profile := filepath.Join(caseDir, "profile")

	extraEnv, args := c.build(profile, flake)

	if c.name == "6-permission-readonly-profile" {
		_ = os.Chmod(caseDir, 0o555)
		defer os.Chmod(caseDir, 0o755) // restore so RemoveAll works next run
	}

	r := &runResult{Case: c.name, Expected: c.expected, ExtraEnv: extraEnv}
	r.GenBefore = currentGeneration(profile)
	r.Argv = append([]string{nixBin}, args...)

	exitCode, events, rawStderr, _, spawnErr := runNix(extraEnv, args...)
	r.ExitCode, r.Events, r.RawStderr = exitCode, events, rawStderr
	if spawnErr != nil {
		r.SpawnErr = spawnErr.Error()
	}
	r.GenAfter = currentGeneration(profile)

	computeSignals(r)
	r.StructuralClass, r.StructuralReason = classifyStructural(r)
	r.AnchoredClass, r.AnchoredReason = classifyAnchored(r)
	r.Score = score(r)

	stem := filepath.Join(outDir, fmt.Sprintf("%s.run%d", c.name, iter))
	_ = os.WriteFile(stem+".stderr.txt", []byte(rawStderr), 0o644)
	writeJSON(stem+".events.json", events)
	return r
}

func score(r *runResult) string {
	switch {
	case r.StructuralClass == r.Expected:
		return "BOUNDED"
	case r.AnchoredClass == r.Expected:
		return "PARTIAL"
	default:
		return "UNBOUNDED"
	}
}

func printSummary(results []*runResult) {
	counts := map[string]int{}
	for _, r := range results {
		counts[r.Score]++
	}
	fmt.Printf("\n=== SCORE SUMMARY ===\nBOUNDED=%d PARTIAL=%d UNBOUNDED=%d (n=%d)\n",
		counts["BOUNDED"], counts["PARTIAL"], counts["UNBOUNDED"], len(results))
	fmt.Println("GREEN needs >=5/6 BOUNDED + #3(daemon) & #6(permission) BOUNDED + none UNBOUNDED.")
}

func preflight(outDir string) {
	verOut, _ := exec.Command(nixBin, "--version").CombinedOutput()
	fmt.Printf("nix --version: %s", verOut)
	_ = os.WriteFile(filepath.Join(outDir, "nix-version.txt"), verOut, 0o644)

	listArgs := append([]string{"profile", "list", "--json"}, expFeatures...)
	listOut, _ := exec.Command(nixBin, listArgs...).CombinedOutput()
	_ = os.WriteFile(filepath.Join(outDir, "profile-list-shape.json"), listOut, 0o644)
	var probe struct {
		Version  int             `json:"version"`
		Elements json.RawMessage `json:"elements"`
	}
	if json.Unmarshal(listOut, &probe) == nil {
		shape := "object(name-keyed)"
		if len(probe.Elements) > 0 && probe.Elements[0] == '[' {
			shape = "array(index)"
		}
		fmt.Printf("profile list --json: version=%d elements=%s\n", probe.Version, shape)
	}
}

func writeJSON(path string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "marshal %s: %v\n", path, err)
		return
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write %s: %v\n", path, err)
	}
}

func must(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "spike: %v\n", err)
		os.Exit(1)
	}
}
