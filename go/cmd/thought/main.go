// Command thought is the CLI entrypoint: `thought {tui|run|scenario|doctor|compare|stability}`
// (PORT-PLAN #39, ports thought_harness/__main__.py).
//
// It is the one place in the harness that is NOT headless-pure: it owns stdout. It builds an Engine,
// wires the trace sinks (always a ConsoleTracer; a JsonlSink when --log is given), drives the chosen
// subcommand, and prints a run summary. The engine itself never prints — it emits; this file's sinks
// render the stream.
//
// One events.Bus per engine.Engine (the engine constructs its own bus); _wireSinks subscribes the
// ConsoleTracer (honouring --layer / --quiet) and, on --log, the golden JsonlSink.
//
// The `tui` subcommand builds the engine and runs the Bubble Tea app via internal/tui.Run — the ONLY
// charmbracelet import in the tree is inside internal/tui, NOT here (cmd/thought stays charm-free and
// calls tui.Run, which wraps tea.NewProgram(...).Run()).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/berttrycoding/thought-harness/internal/appraisal"
	"github.com/berttrycoding/thought-harness/internal/backends"
	"github.com/berttrycoding/thought-harness/internal/clock"
	"github.com/berttrycoding/thought-harness/internal/config"
	"github.com/berttrycoding/thought-harness/internal/conformance"
	"github.com/berttrycoding/thought-harness/internal/engine"
	"github.com/berttrycoding/thought-harness/internal/events"
	"github.com/berttrycoding/thought-harness/internal/flywheel"
	"github.com/berttrycoding/thought-harness/internal/host"
	"github.com/berttrycoding/thought-harness/internal/llm"
	"github.com/berttrycoding/thought-harness/internal/persist"
	"github.com/berttrycoding/thought-harness/internal/scenarios"
	"github.com/berttrycoding/thought-harness/internal/signals"
	"github.com/berttrycoding/thought-harness/internal/stability"
	"github.com/berttrycoding/thought-harness/internal/trace"
	"github.com/berttrycoding/thought-harness/internal/tui"
	"github.com/berttrycoding/thought-harness/internal/web"
)

func main() { os.Exit(run(os.Args[1:])) }

// run dispatches argv to a subcommand handler, returning the process exit code. No subcommand (or an
// unrecognised first token) defaults to `tui` with the remaining args, mirroring Python's
// `if not args.cmd: args = parse(["tui"] + argv)`.
func run(argv []string) int {
	if len(argv) == 0 {
		return cmdTUI(nil)
	}
	cmd, rest := argv[0], argv[1:]
	switch cmd {
	case "tui":
		return cmdTUI(rest)
	case "run":
		return cmdRun(rest)
	case "scenario":
		return cmdScenario(rest)
	case "appraisals":
		return cmdAppraisals(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "compare":
		return cmdCompare(rest)
	case "stability":
		return cmdStability(rest)
	case "conformance":
		return cmdConformance(rest)
	case "mcp-serve":
		return cmdMCPServe(rest)
	case "cognition":
		return cmdCognition(rest)
	case "state":
		return cmdState(rest)
	case "ledger":
		return cmdLedger(rest)
	case "campaign":
		return cmdCampaign(rest)
	case "probe":
		return cmdProbe(rest)
	case "plangate":
		return cmdPlanGate(rest)
	case "-h", "--help", "help":
		usage()
		return 0
	default:
		// No recognised subcommand: Python re-parses with `tui` prepended (tui is the default surface).
		return cmdTUI(argv)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "thought — Silent-Injection Cognition harness.")
	fmt.Fprintln(os.Stderr, "subcommands: tui | run | scenario | appraisals | doctor | compare | stability | cognition | state | ledger | plangate")
}

// ---------------------------------------------------------------------------
// shared flag helpers (Python _add_backend_flags / _add_log_flags / _backend)
// ---------------------------------------------------------------------------

// backendFlags holds the dev-override backend selection shared by run/scenario/doctor/appraisals.
type backendFlags struct {
	backend      string // "test" | "llm" | "" (dev override; the TUI configures the model itself)
	llmURL       string
	llmModel     string
	llmMaxTokens int    // --llm-max-tokens: per-call completion budget (0 → env/default 4096)
	workspace    string // only run/tui/appraisals take a workspace (the REAL Action layer)
}

// addBackendFlags registers --backend/--llm-url/--llm-model/--llm-max-tokens (and --workspace when
// withWorkspace) on fs. Mirrors Python _add_backend_flags.
func addBackendFlags(fs *flag.FlagSet, bf *backendFlags, withWorkspace bool) {
	fs.StringVar(&bf.backend, "backend", "",
		"substrate (auto|frontier|local|session|claude|test; aliases llm/cc accepted) — the same menu "+
			"as the TUI Settings; claude = the Claude Code CLI bridge (subscription, no GPU)")
	fs.StringVar(&bf.llmURL, "llm-url", "", "LLM base URL (default http://localhost:1234/v1)")
	fs.StringVar(&bf.llmModel, "llm-model", "",
		"model id (default nvidia/nemotron-3-nano-omni; 'auto' = the loaded model)")
	fs.IntVar(&bf.llmMaxTokens, "llm-max-tokens", 0,
		"per-call completion budget (0 = THOUGHT_LLM_MAX_TOKENS, default 4096 — reasoning headroom)")
	if withWorkspace {
		fs.StringVar(&bf.workspace, "workspace", "",
			"enable the REAL Action layer sandboxed to DIR (e.g. --workspace /path/to/repo)")
	}
}

// logFlags holds --log / --layer shared by run/scenario.
type logFlags struct {
	logPath string
	layer   string
}

func addLogFlags(fs *flag.FlagSet, lf *logFlags) {
	fs.StringVar(&lf.logPath, "log", "",
		"persist every event as JSONL (full data incl. LLM prompts/responses)")
	fs.StringVar(&lf.layer, "layer", "", "console: show only these event layers (e.g. seam,critic,llm)")
}

// parseLayers splits a comma list into a layer slice, dropping blanks (Python _layers). nil ⇒ all.
func parseLayers(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// featureFlags holds the system-wide config selection (the representation-space rebuild, M1):
// --config PATH, and --enable/--disable as comma-lists of dotted toggle paths. Shared by run/tui.
type featureFlags struct {
	profile    string // --profile NAME — a one-pick knob bundle (config.Profiles()); the base config + its Mode
	configPath string // --config PATH ("" ⇒ no file; the all-on baseline merged with THOUGHT_CFG_* env)
	enable     string // --enable a,b,c — flip these toggles ON  (the highest non-TUI tier)
	disable    string // --disable a,b,c — flip these toggles OFF (the highest non-TUI tier)
	stateDir   string // --state DIR — cross-session persistence of learned artifacts (M4); "" ⇒ in-memory only
}

// addFeatureFlags registers --config/--enable/--disable on fs (M1). Off by default — a missing
// --config + empty env + empty enable/disable ⇒ config.AllOn(), byte-identical to pre-config.
func addFeatureFlags(fs *flag.FlagSet, ff *featureFlags) {
	fs.StringVar(&ff.profile, "profile", "",
		"cognition profile — a one-pick knob bundle ("+strings.Join(config.ProfileNames(), " | ")+
			"); sets the base config + its loop mode (mutually exclusive with --config)")
	fs.StringVar(&ff.configPath, "config", "",
		"system-wide config JSON (toggles per component + the representation matrix); merged over all-on")
	fs.StringVar(&ff.enable, "enable", "",
		"comma list of dotted toggle paths to force ON  (e.g. --enable seam.hidden_gate)")
	fs.StringVar(&ff.disable, "disable", "",
		"comma list of dotted toggle paths to force OFF (e.g. --disable memory.reflect,seam.hidden_transform)")
	fs.StringVar(&ff.stateDir, "state", "",
		"persist learned artifacts (skills/operators/specialists/priors/memory/knowledge) to DIR across sessions (M4)")
}

// resolveStore builds the cross-session persistence store from --state DIR (M4). An empty dir ⇒ nil (the
// in-memory-only default — learned state evaporates on exit, byte-identical to pre-M4). A JSONL store is
// created (the dir is made if absent). Returns the error so the caller can fail with a clear message.
func resolveStore(ff *featureFlags) (persist.Store, error) {
	if ff == nil || strings.TrimSpace(ff.stateDir) == "" {
		return nil, nil
	}
	st, err := persist.NewJSONLStore(ff.stateDir)
	if err != nil {
		return nil, err
	}
	return st, nil
}

// wireStoreLog wires the store's emit closure to a JSONL sink when --log is given, so a one-shot
// `state`/`ledger` command's registry.* decisions (snapshot/reset/diff/ledger) land in the event log —
// the observability contract reached from the CLI path, not only the live engine. Returns a close
// func (nil when --log is unset, leaving the store silent — the byte-identical default). Best-effort:
// a sink-open failure logs to stderr and leaves the store silent rather than aborting the command.
func wireStoreLog(store persist.Store, lf logFlags) func() {
	if strings.TrimSpace(lf.logPath) == "" {
		return nil
	}
	sink, err := trace.NewJsonlSink(lf.logPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: --log:", err)
		return nil
	}
	bus := events.New(256)
	bus.Subscribe(sink.On)
	store.SetEmit(bus.Emit)
	return func() { _ = sink.Close() }
}

// resolveFeatures builds the system-wide HarnessConfig from the precedence chain (M1):
//
//	defaults (AllOn)  <  --config file  <  THOUGHT_CFG_* env  <  --enable/--disable flags
//
// config.Load handles file<env; this then applies the --disable then --enable flags (the highest
// non-TUI tier) and re-Validates. A nil ff (no feature flags wired) ⇒ AllOn() (the all-on default).
// Returns nil on error so the caller can fail with a clear message. emit may be nil.
func resolveFeatures(ff *featureFlags) (*config.HarnessConfig, error) {
	if ff == nil {
		return config.New(), nil
	}
	var c *config.HarnessConfig
	if strings.TrimSpace(ff.profile) != "" {
		// A profile is the BASE config (instead of AllOn()). --config (a file base) and --profile both
		// define the base, so they are mutually exclusive; --enable/--disable still layer on top.
		if strings.TrimSpace(ff.configPath) != "" {
			return nil, fmt.Errorf("--profile and --config are mutually exclusive (both set the base config)")
		}
		p, ok := config.ProfileByName(ff.profile)
		if !ok {
			return nil, fmt.Errorf("--profile: unknown profile %q (use one of: %s)",
				ff.profile, strings.Join(config.ProfileNames(), ", "))
		}
		c = p.Build()
	} else {
		explicit := strings.TrimSpace(ff.configPath) != ""
		var err error
		c, _, err = config.Load(ff.configPath, explicit, nil)
		if err != nil {
			return nil, err
		}
	}
	for _, path := range parseLayers(ff.disable) { // reuse the comma-splitter
		if !config.ApplyToggle(c, path, false) {
			return nil, fmt.Errorf("--disable: unknown or non-bool toggle %q", path)
		}
	}
	for _, path := range parseLayers(ff.enable) {
		if !config.ApplyToggle(c, path, true) {
			return nil, fmt.Errorf("--enable: unknown or non-bool toggle %q", path)
		}
	}
	c.Validate()
	return c, nil
}

// makeBackend builds the dev-override backend for run/scenario/doctor/appraisals. Mirrors Python
// _backend: an empty selection defaults to the offline test double (the dev/demo CLI default; the TUI
// is the product surface and defaults to a model). Returns the error from MakeBackend (unknown name).
func makeBackend(bf backendFlags) (backends.Backend, error) {
	name := bf.backend
	if name == "" {
		name = "test"
	}
	return llm.MakeBackend(name, bf.llmURL, bf.llmModel, bf.llmMaxTokens)
}

// newEngineWith builds an engine on an explicit dev-override backend (the CLI path), wiring the
// workspace if one was given. Returns the construction error (the backend is explicit, so this is
// effectively never a substrate-resolution error — but it is surfaced faithfully).
func newEngineWith(cfg engine.EngineConfig, bf backendFlags) (*engine.Engine, error) {
	b, err := makeBackend(bf)
	if err != nil {
		return nil, err
	}
	// RESUME IS AUTOMATIC for the interactive CLI/TUI session — turned on here at the edge (like the
	// sense seams below), NOT in config.AllOn(): the measurement harnesses (campaign/bench/probe) build
	// engines via AllOn() directly and need FRESH per-episode determinism, so a global default-on would
	// contaminate them. A --state session continues its prior cognitive cursor; with no --state there is
	// no Store, so resume is a harmless no-op. Must be set BEFORE NewEngine (loadResume runs there).
	if cfg.Features == nil {
		f := config.AllOn()
		cfg.Features = &f
	}
	cfg.Features.Persist.Resume = true
	// WEB-LANE BUNDLE (capability-enhancement T1.1/T1.4): opting into web_search auto-includes the two
	// validated web-lane improvements (query_formulation + fetch_url) — the --web config the +5pp
	// HotpotQA-fullwiki lift was measured on. Edge-only (like Resume), no-op when web_search is off.
	cfg.Features.EnableWebLaneBundle()
	eng, err := engine.NewEngine(&cfg, b)
	if err != nil {
		return nil, err
	}
	// Grounded sensing is default-ON; wire the real-world sense SEAMS at the edge (the engine stays
	// headless-pure — Wall reads enter only here). The go-test path builds engines WITHOUT this wiring,
	// so the suite stays time-blind/deterministic; on a live run each real read is recorded to the
	// percept-log for deterministic replay. A nil seam (the suite) ⇒ the sensor is a no-op.
	eng.SetClock(clock.Wall{}, 0)
	eng.SetHost(host.Wall{})
	// fetch_web (follow-up #15 — the OUTWARD distal sense): wire the real Web seam at the edge so flipping
	// sense.web ON just works. UNLIKE clock/host the sense.web knob DEFAULTS OFF, so this wired seam stays
	// DORMANT (no fetch) until the knob is flipped — wiring the seam is not the same as turning the sense
	// on. A live fetch is recorded to the percept-log for deterministic replay; the go-test path builds
	// engines without this wiring (nil web ⇒ web-blind), so the suite stays deterministic.
	eng.SetWeb(web.NewWall())
	// fetch_url (T1.4 — the OUTWARD page-FETCH seam, sibling of web): wire the real page fetcher at the edge
	// so flipping subconscious.fetch_url ON just works. Like the web seam it stays DORMANT (the tool is not
	// even registered) until the knob is flipped — wiring the seam is not turning the capability on. The
	// go-test path builds engines without this wiring (nil ⇒ page-blind), so the suite stays deterministic.
	eng.SetPageFetcher(web.NewPager())
	return eng, nil
}

// wireSinks subscribes the trace sinks to the engine's bus: ALWAYS a ConsoleTracer (honouring
// quiet + the --layer filter), and a golden JsonlSink when --log is set. One Bus per Engine; this is
// the only place the CLI attaches output. Mirrors Python _wire_sinks. The returned closer flushes the
// JSONL file (nil when no --log). Color autodetects from the stream (nil tri-state).
func wireSinks(eng *engine.Engine, lf logFlags, quiet bool) (func(), error) {
	tracer := trace.NewConsoleTracer(os.Stdout, quiet, nil, parseLayers(lf.layer))
	eng.Bus().Subscribe(tracer.On)
	if lf.logPath != "" {
		sink, err := trace.NewJsonlSink(lf.logPath)
		if err != nil {
			return nil, err
		}
		eng.Bus().Subscribe(sink.On)
		fmt.Printf("(logging all events -> %s)\n", lf.logPath)
		// SignalFrame sidecar (Track G, G0): when tui.signal_frames is ON, derive the per-tick vitals
		// vector off the SAME bus and persist it to a SIDECAR (*.signals.jsonl) next to the event log.
		// It is a pure Pattern-A subscriber that emits NOTHING back onto the bus, so the event golden
		// stays byte-identical; default-OFF wires nothing. Best-effort: a sidecar-open failure leaves the
		// event log intact and only skips the frames.
		closeSignals := wireSignalSidecar(eng, lf.logPath)
		// OFFLINE-RL flywheel sidecar (Track C, RL roadmap §6 P0): when flywheel.capture is ON, persist the
		// per-decision (state, action, grounded-outcome) training tuples to a SIDECAR (*.flywheel.jsonl) next
		// to the event log. The engine owns the Recorder + the backfill; the edge only owns the file (the
		// engine stays headless-pure). Default-OFF wires nothing. Best-effort: a sidecar-open failure leaves
		// the event log intact and only skips the dataset.
		closeFlywheel := wireFlywheelSidecar(eng, lf.logPath)
		return func() { closeFlywheel(); closeSignals(); _ = sink.Close() }, nil
	}
	return func() {}, nil
}

// wireFlywheelSidecar injects a flywheel.JSONLSink into the engine, writing the per-decision training
// tuples to the flywheel sidecar derived from logPath, GATED on the flywheel.capture knob. Returns a
// closer that flushes the sidecar. A no-op (and a no-op closer) when the knob is OFF or the sidecar cannot
// be opened — the default path adds nothing.
func wireFlywheelSidecar(eng *engine.Engine, logPath string) func() {
	if feat := eng.Features(); feat == nil || !feat.Flywheel.Capture {
		return func() {}
	}
	side := flywheelSidecarPath(logPath)
	f, err := os.Create(side)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: --log flywheel sidecar:", err)
		return func() {}
	}
	sink := flywheel.NewJSONLSink(f)
	eng.SetFlywheelSink(sink)
	fmt.Printf("(capturing offline-RL flywheel tuples -> %s)\n", side)
	return func() { _ = sink.Flush(); _ = f.Close() }
}

// flywheelSidecarPath turns an event-log path into its flywheel sidecar path: strip a trailing ".jsonl"
// (the conventional event-log extension) and append ".flywheel.jsonl"; otherwise append the suffix
// wholesale so any --log path gets a deterministic, adjacent sidecar.
func flywheelSidecarPath(logPath string) string {
	base := logPath
	if strings.HasSuffix(base, ".jsonl") {
		base = base[:len(base)-len(".jsonl")]
	}
	return base + ".flywheel.jsonl"
}

// wireSignalSidecar attaches the signals.Recorder to the engine bus and writes its frames to the
// SignalFrame sidecar derived from logPath, GATED on the tui.signal_frames knob. Returns a closer
// that flushes the final in-progress tick + the sidecar file. A no-op (and a no-op closer) when the
// knob is OFF or the sidecar cannot be opened — the default path adds nothing.
func wireSignalSidecar(eng *engine.Engine, logPath string) func() {
	if feat := eng.Features(); feat == nil || !feat.Tui.SignalFrames {
		return func() {}
	}
	side := signalSidecarPath(logPath)
	f, err := os.Create(side)
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: --log signal sidecar:", err)
		return func() {}
	}
	rec := signals.NewRecorder(f)
	eng.Bus().Subscribe(rec.On)
	fmt.Printf("(deriving SignalFrame sidecar -> %s)\n", side)
	return func() { rec.Close(); _ = f.Close() }
}

// signalSidecarPath turns an event-log path into its SignalFrame sidecar path: strip a trailing
// ".jsonl" (the conventional event-log extension) and append ".signals.jsonl"; otherwise append the
// sidecar suffix wholesale so any --log path gets a deterministic, adjacent sidecar.
func signalSidecarPath(logPath string) string {
	base := logPath
	if strings.HasSuffix(base, ".jsonl") {
		base = base[:len(base)-len(".jsonl")]
	}
	return base + ".signals.jsonl"
}

// ---------------------------------------------------------------------------
// run / continuous
// ---------------------------------------------------------------------------

func cmdRun(argv []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	mode := fs.String("mode", "reactive", "reactive | continuous (= awake)")
	prompt := fs.String("prompt", "", "the goal/percept to submit")
	ticks := fs.Int("ticks", 40, "step budget")
	seed := fs.Int("seed", 7, "seeded RNG")
	quiet := fs.Bool("quiet", false, "show only the loop spine")
	cognition := fs.String("cognition", "control",
		"decision mode: control(rules) | llm(AI) | hybrid (llm/hybrid need --backend llm)")
	var bf backendFlags
	var lf logFlags
	var ff featureFlags
	addBackendFlags(fs, &bf, true)
	addLogFlags(fs, &lf)
	addFeatureFlags(fs, &ff)
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	cfg := engine.DefaultConfig()
	cfg.Mode = *mode
	cfg.Seed = *seed
	cfg.MaxTicks = *ticks
	cfg.Cognition = *cognition
	cfg.Workspace = bf.workspace
	cfg.LLMMaxTokens = bf.llmMaxTokens // 0 → env/default 4096 (reasoning headroom)
	feat, err := resolveFeatures(&ff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg.Features = feat
	if p, ok := config.ProfileByName(ff.profile); ok { // a profile carries its loop regime
		cfg.Mode = p.Mode
		cfg.Profile = p.Name
		if p.Persist && strings.TrimSpace(ff.stateDir) == "" { // self-contained: auto-persist learned state
			sub := cfg.Substrate
			if bf.backend != "" { // a --backend dev override decides the real class (test ⇒ no persist)
				sub = bf.backend
			}
			ff.stateDir = engine.DefaultStateDir(sub)
		}
	}
	store, err := resolveStore(&ff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg.Store = store
	eng, err := newEngineWith(cfg, bf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	closeLog, err := wireSinks(eng, lf, *quiet)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer closeLog()

	p := *prompt
	if p == "" && *mode == "reactive" {
		p = "What's 7×8?" // a demo question; continuous mode self-seeds its wander instead
	}
	if p != "" {
		eng.SubmitDefault(p)
	}
	runWithSignalStop(eng, *ticks)
	eng.FlushState() // persist any learned state on a clean exit (no-op when --state is unset)
	printSummary(eng)
	return 0
}

// runWithSignalStop runs the engine to its tick budget, but on SIGINT/SIGTERM asks the engine to stop at
// the next tick boundary (RequestStop) so the caller's FlushState() persists the learned state + resume
// cursor before exit — a graceful power-down. The flush runs AFTER Run returns (we wait on done), so it
// never races the loop. Proposal 2026-06-20 §2.3 / red-team amendment 6 (the signal handler lives at the
// edge, never inside the headless-pure engine).
func runWithSignalStop(eng *engine.Engine, ticks int) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	done := make(chan struct{})
	go func() { eng.Run(ticks); close(done) }()
	select {
	case <-done:
	case <-sig:
		eng.RequestStop()
		<-done // wait for the loop to actually stop before the caller flushes
	}
}

// ---------------------------------------------------------------------------
// scenario
// ---------------------------------------------------------------------------

func cmdScenario(argv []string) int {
	fs := flag.NewFlagSet("scenario", flag.ContinueOnError)
	list := fs.Bool("list", false, "list the worked scenarios")
	verbose := fs.Bool("verbose", false, "show every event, not just the spine")
	var bf backendFlags
	var lf logFlags
	var ff featureFlags
	addBackendFlags(fs, &bf, false)
	addLogFlags(fs, &lf)
	addFeatureFlags(fs, &ff)

	// The positional scenario id (argparse `name nargs="?"`) may appear ANYWHERE relative to the
	// flags (argparse interleaves; Go's flag stops at the first non-flag token). Pull the first
	// bare positional out before flag parsing so `scenario S1 --backend test` works like Python.
	name, rest := extractPositional(argv)
	if err := fs.Parse(rest); err != nil {
		return 2
	}

	if *list || name == "" {
		fmt.Println("Worked scenarios (spec §8):")
		fmt.Println()
		for _, sc := range scenarios.All() {
			fmt.Printf("  %-4s [%-10s] %s\n", sc.ID, sc.Mode, sc.Title)
			fmt.Printf("       exercises: %s\n", sc.Exercises)
		}
		return 0
	}

	sc, ok := scenarios.Get(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown scenario %s. Use --list.\n", pyRepr(name))
		return 2
	}
	backendName := bf.backend
	if backendName == "" {
		backendName = "None" // Python prints the raw args.backend, which defaults to None
	}
	fmt.Printf("=== %s: %s ===\n", sc.ID, sc.Title)
	fmt.Printf("exercises: %s\n", sc.Exercises)
	// A self-seeded scenario (e.g. S16, awake arousal) carries no prompt — RunScenario handles the empty
	// case, but this banner must not index an empty slice (the headless run never goes through here).
	prompt := "(self-seeded)"
	if len(sc.Prompts) > 0 {
		prompt = pyRepr(sc.Prompts[0])
	}
	fmt.Printf("mode: %s | prompt: %s | backend: %s\n\n", sc.Mode, prompt, backendName)

	cfg := engine.DefaultConfig()
	cfg.Mode = sc.Mode
	feat, err := resolveFeatures(&ff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg.Features = feat
	store, err := resolveStore(&ff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg.Store = store
	eng, err := newEngineWith(cfg, bf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	closeLog, err := wireSinks(eng, lf, !*verbose)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer closeLog()

	if _, err := scenarios.RunScenario(name, eng); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	eng.FlushState() // persist any learned state (no-op when --state is unset)
	printSummary(eng)
	return 0
}

// ---------------------------------------------------------------------------
// cognition — render the unified cross-layer CognitionGraph after a run (X.5)
// ---------------------------------------------------------------------------

// truncate shortens s to at most n runes, appending an ellipsis when cut.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// cmdCognition runs a scenario (default S5 — it acts on reality, so the graph spans every layer) and
// renders the unified cross-layer CognitionGraph the engine assembled from the event bus: the per-layer
// entity counts, the entities themselves by layer, and the provenance lineage of the final action.
func cmdCognition(argv []string) int {
	fs := flag.NewFlagSet("cognition", flag.ContinueOnError)
	var bf backendFlags
	addBackendFlags(fs, &bf, false)
	name, rest := extractPositional(argv)
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if name == "" {
		name = "S5" // exercises the Watched Seam / Action → the richest cross-layer graph
	}
	sc, ok := scenarios.Get(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown scenario %s. Use `scenario --list`.\n", pyRepr(name))
		return 2
	}

	cfg := engine.DefaultConfig()
	cfg.Mode = sc.Mode
	eng, err := newEngineWith(cfg, bf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := scenarios.RunScenario(name, eng); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	cg := eng.CognitionGraph()
	fmt.Printf("=== Cognition graph — %s: %s ===\n\n", sc.ID, sc.Title)
	fmt.Println(cg.Summary())
	fmt.Println()
	for _, layer := range []string{"process", "subconscious", "seam", "conscious", "critic", "action"} {
		nodes := cg.ByLayer(layer)
		if len(nodes) == 0 {
			continue
		}
		fmt.Printf("%-13s (%d):\n", layer, len(nodes))
		for i, n := range nodes {
			if i >= 8 {
				fmt.Printf("    … and %d more\n", len(nodes)-8)
				break
			}
			fmt.Printf("    %-10s %s\n", n.Type, truncate(n.Label, 70))
		}
	}
	// provenance: trace the causes of the last ACTION entity (the watched-seam observation).
	if acts := cg.ByLayer("action"); len(acts) > 0 {
		last := acts[len(acts)-1]
		fmt.Printf("\nprovenance of action %q:\n%s\n", truncate(last.Label, 50), cg.Lineage(last.ID))
	}
	return 0
}

// ---------------------------------------------------------------------------
// appraisals (P6 reasoning dataset)
// ---------------------------------------------------------------------------

func cmdAppraisals(argv []string) int {
	fs := flag.NewFlagSet("appraisals", flag.ContinueOnError)
	prompt := fs.String("prompt", "", "the goal to run")
	ticks := fs.Int("ticks", 20, "step budget")
	seed := fs.Int("seed", 7, "seeded RNG")
	cognition := fs.String("cognition", "control", "Controller decision mode")
	var bf backendFlags
	addBackendFlags(fs, &bf, true)
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	cfg := engine.DefaultConfig()
	cfg.Mode = "reactive"
	cfg.Seed = *seed
	cfg.Cognition = *cognition
	cfg.Workspace = bf.workspace
	eng, err := newEngineWith(cfg, bf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	p := *prompt
	if p == "" {
		p = "Is this change safe to ship?"
	}
	eng.SubmitDefault(p)
	eng.Run(*ticks)

	// Read the captured reasoning dataset back off the event log (Python collect_appraisals(bus.log)).
	// The bus replay ring (default 4000) holds the run's events; Recent(big, nil) returns them all.
	aps := appraisal.Collect(eng.Bus().Recent(1<<30, nil))
	fmt.Printf("=== %d appraisals captured (the reasoning dataset) ===\n\n", len(aps))
	for _, a := range aps {
		verdict := ""
		if a.Verdict != nil {
			verdict = *a.Verdict
		}
		fmt.Printf("[%-13s] %-9s v=%.2f %-10s %s\n",
			a.Site, a.Source, a.Value, verdict, runeHead(a.Reason, 52))
		if len(a.Signals) > 0 {
			var sb strings.Builder
			sb.WriteString("                signals: ")
			for i, k := range sortedKeys(a.Signals) {
				if i > 0 {
					sb.WriteByte(' ')
				}
				sb.WriteString(fmt.Sprintf("%s=%+.2f", k, signalFloat(a.Signals[k])))
			}
			fmt.Println(sb.String())
		}
	}
	return 0
}

// ---------------------------------------------------------------------------
// doctor (per-subsystem LLM probe)
// ---------------------------------------------------------------------------

func cmdDoctor(argv []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	var bf backendFlags
	addBackendFlags(fs, &bf, false)
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	backend, err := makeBackend(bf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	llmBe, isLLM := backend.(*llm.OpenAICompatBackend)
	if tb, ok := backend.(*llm.TieredBackend); ok {
		// The claude bridge (and any tiered build) reports through its PRIMARY tier.
		llmBe, isLLM = tb.Primary, true
	}
	displayName := "test"
	if dn, ok := backend.(interface{ DisplayName() string }); ok {
		displayName = dn.DisplayName()
	}
	fmt.Printf("Backend: %s\n", displayName)
	if isLLM {
		fmt.Printf("  endpoint: %s   |   requested model: %s\n", llmBe.BaseURL, llmBe.Model)
		h := llmBe.Health()
		if h.Up {
			loaded := strings.Join(h.Models, ", ")
			if loaded == "" {
				loaded = "(NONE loaded — run: lms load <model>)"
			}
			fmt.Printf("  server: UP    loaded: %s\n", loaded)
			if !containsStr(h.Models, llmBe.Model) {
				fmt.Printf("  WARNING: %s is not loaded -> CONTENT roles surface the gap; "+
					"the control floor still decides\n", pyRepr(llmBe.Model))
			}
		} else {
			fmt.Printf("  server: DOWN (%s) -> CONTENT roles surface the gap; "+
				"the control floor still decides\n", h.Error)
		}
	} else {
		fmt.Println("  test double (offline, deterministic) — no model calls; every subsystem works")
	}

	fmt.Println("\nProbing each subsystem (1 sample call each):")
	rows := llm.ProbeBackend(backend)
	ok := 0
	for _, r := range rows {
		var status string
		switch {
		case r.Exception != "":
			status = "ERROR"
		case !r.FellBack:
			status, ok = "OK", ok+1
		case r.ModelAnswered:
			status = "PARSE-FAIL"
		default:
			status = "FALLBACK"
		}
		ms := "-"
		if r.HasCallLog {
			ms = fmt.Sprintf("%dms", r.MS)
		}
		out := runeHead(strings.ReplaceAll(r.Output, "\n", " "), 62)
		fmt.Printf("  %-20s %-11s %-7s -> %s\n", r.Subsystem, status, ms, out)
		if status == "PARSE-FAIL" && r.Raw != "" {
			fmt.Printf("       model raw (didn't parse): %s\n", pyRepr(runeHead(r.Raw, 96)))
		}
		if status == "FALLBACK" && r.CallError != "" {
			fmt.Printf("       why: %s\n", r.CallError)
		}
		if r.Exception != "" {
			fmt.Printf("       exception: %s\n", r.Exception)
		}
	}

	total := len(rows)
	if isLLM {
		fmt.Printf("\n%d/%d subsystems returned usable model output.\n", ok, total)
		if ok < total {
			fmt.Println("Hints:")
			fmt.Println("  PARSE-FAIL = model replied but a JSON role (the Filter escalation) didn't " +
				"parse (raw shown above) — try a stronger model.")
			fmt.Println("  FALLBACK 'finish_reason=length' = a reasoning model spent its WHOLE budget " +
				"thinking (incl. the retry-on-truncation grow) and salvage found no answer in the " +
				"reasoning — raise --llm-max-tokens / THOUGHT_LLM_RETRY_CAP, or use a non-reasoning model.")
			fmt.Println("  FALLBACK (connection/no model) = endpoint or model unavailable; the CONTENT role " +
				"surfaces the gap and the control floor still decides — lms load ...")
			fmt.Println("Inspect full prompts + raw responses with:  thought run --backend llm --log t.jsonl")
		}
	} else {
		fmt.Printf("\n%d/%d subsystems OK (test double — offline, deterministic).\n", total, total)
	}
	return 0
}

// ---------------------------------------------------------------------------
// compare (control vs llm vs hybrid Controller)
// ---------------------------------------------------------------------------

func cmdCompare(argv []string) int {
	fs := flag.NewFlagSet("compare", flag.ContinueOnError)
	llmURL := fs.String("llm-url", "", "LLM base URL")
	llmModel := fs.String("llm-model", "", "model id (default nano-omni; 'auto'=loaded)")
	out := fs.String("out", "compare_results.json", "where to save the JSON results")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	scenarioIDs := fs.Args()
	if len(scenarioIDs) == 0 {
		scenarioIDs = []string{"S1", "S3", "S5", "S8"} // Python: args.scenarios or [...]
	}

	// The decision-backend factory (Python's `factory()` returning an OpenAICompatBackend). It returns
	// a backends.Backend (the compare BackendFactory type) — an OpenAICompatBackend satisfies it.
	factory := func() backends.Backend {
		return llm.NewOpenAICompat(llm.Options{BaseURL: *llmURL, Model: *llmModel})
	}
	// Read the model id for the header from one freshly-built backend (Python `factory().model`).
	modelID := llm.NewOpenAICompat(llm.Options{BaseURL: *llmURL, Model: *llmModel}).Model
	fmt.Printf("Comparing cognition modes on %s  (model: %s)\n"+
		"(llm/hybrid make real model calls — this takes a bit)\n\n",
		bracketList(scenarioIDs), modelID)

	res := RunComparison(scenarioIDs, factory, nil, 0) // nil modes -> control/llm/hybrid; maxTicks 0 -> 24
	s := res.Summary
	fmt.Printf("%-10s%-10s%-11s%-13s%-13s%s\n",
		"mode", "correct", "decisions", "model-calls", "escalations", "agree-w-control")
	fmt.Println(strings.Repeat("-", 70))
	for _, mode := range []string{"control", "llm", "hybrid"} {
		m, ok := s[mode]
		if !ok {
			continue
		}
		ag := "-"
		if m.Agreement != nil {
			ag = fmt.Sprintf("%.0f%% of %d", *m.Agreement*100, m.Compared)
		}
		correct := fmt.Sprintf("%d/%d", m.Correct, m.Total)
		fmt.Printf("%-10s%-10s%-11d%-13d%-13d%s\n",
			mode, correct, m.Decisions, m.LLMCalls, m.Escalations, ag)
	}
	fmt.Println("\nDisagreements (control floor vs model — exactly what the hybrid escalates):")
	if len(res.Disagreements) == 0 {
		fmt.Println("  (none — the control floor and model agreed on every compared decision)")
	}
	for _, d := range res.Disagreements {
		fmt.Printf("  %s [%s] ambiguity=%s: control=%s vs llm=%s  | %s\n",
			d.Scenario, d.Mode, pyFloat(d.Ambiguity), d.HeuristicDecision, d.LLMDecision, d.Reason)
	}

	if err := writeCompareJSON(*out, res); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("\nsaved -> %s\n", *out)
	return 0
}

// JSON encoder structs — ordered field declarations reproduce Python's dict INSERTION order (a
// map[string]any would be alphabetised by json.Marshal). Nullable Python-None values use pointers
// (nil → JSON null); ambiguity/agreement are json.Number so a whole float renders "0.0" not "0".

// jsDecision is one captured decision (Python's `{k: m.get(k)}` dict). LLMDecision/Agree are pointers
// so a non-escalated decision renders them as null (Python's None), matching m.get(k) on an absent key.
type jsDecision struct {
	Decision          string      `json:"decision"`
	HeuristicDecision string      `json:"heuristic_decision"`
	LLMDecision       *string     `json:"llm_decision"`
	Escalated         bool        `json:"escalated"`
	Agree             *bool       `json:"agree"`
	Ambiguity         json.Number `json:"ambiguity"`
	Reason            string      `json:"reason"`
}

// jsDisagreement flattens a disagreement with its scenario+mode first (Python `{"scenario":..,
// "mode":.., **d}`), then the decision fields in order.
type jsDisagreement struct {
	Scenario          string      `json:"scenario"`
	Mode              string      `json:"mode"`
	Decision          string      `json:"decision"`
	HeuristicDecision string      `json:"heuristic_decision"`
	LLMDecision       *string     `json:"llm_decision"`
	Escalated         bool        `json:"escalated"`
	Agree             *bool       `json:"agree"`
	Ambiguity         json.Number `json:"ambiguity"`
	Reason            string      `json:"reason"`
}

// jsRun is one run row (Python's per-run dict in summarize's output).
type jsRun struct {
	Scenario     string       `json:"scenario"`
	Mode         string       `json:"mode"`
	Decisions    []jsDecision `json:"decisions"`
	FinalState   string       `json:"final_state"`
	MadeExpected bool         `json:"made_expected"`
}

// jsModeSummary is one mode's aggregate (Python summary[mode] dict, key order preserved). Agreement is
// a pointer so a mode with no compared decisions renders null (Python None).
type jsModeSummary struct {
	Correct     int          `json:"correct"`
	Total       int          `json:"total"`
	Decisions   int          `json:"decisions"`
	Escalations int          `json:"escalations"`
	LLMCalls    int          `json:"llm_calls"`
	Fallbacks   int          `json:"fallbacks"`
	Agreement   *json.Number `json:"agreement"`
	Compared    int          `json:"compared"`
}

// writeCompareJSON serialises the comparison result to out, matching the Python json.dump shape +
// insertion order: {"summary": {...}, "disagreements": [...], "runs": [...]}, indent=2. The compare
// types (ComparisonResult/Run/decisionRecord/ModeSummary/Disagreement) live in cmd/thought/compare.go.
func writeCompareJSON(out string, res ComparisonResult) error {
	runs := make([]jsRun, 0, len(res.Runs))
	for i := range res.Runs {
		r := &res.Runs[i]
		decs := make([]jsDecision, 0, len(r.Decisions))
		for _, d := range r.Decisions {
			decs = append(decs, decisionJSON(d))
		}
		runs = append(runs, jsRun{Scenario: r.Scenario, Mode: r.Mode, Decisions: decs,
			FinalState: r.FinalState, MadeExpected: r.MadeExpected()})
	}
	dis := make([]jsDisagreement, 0, len(res.Disagreements))
	for _, d := range res.Disagreements {
		// A Disagreement is by construction escalated AND a non-agreement (it is the result of the
		// `escalated and agree is False` filter), so the model WAS consulted — llm_decision is the
		// model pick (non-null) and agree is false. Reconstruct those for the flattened dict, matching
		// Python's `{"scenario":.., "mode":.., **d}` where d carried the full decision record.
		ll := d.LLMDecision
		ag := false
		dis = append(dis, jsDisagreement{
			Scenario: d.Scenario, Mode: d.Mode, Decision: d.Decision,
			HeuristicDecision: d.HeuristicDecision, LLMDecision: &ll,
			Escalated: true, Agree: &ag,
			Ambiguity: json.Number(pyFloat(d.Ambiguity)), Reason: d.Reason})
	}
	// A struct (not a map) so the top-level keys keep Python's insertion order summary→disagreements→
	// runs; json.Marshal sorts MAP keys alphabetically, which would reorder them.
	payload := struct {
		Summary       map[string]jsModeSummary `json:"summary"`
		Disagreements []jsDisagreement         `json:"disagreements"`
		Runs          []jsRun                  `json:"runs"`
	}{
		Summary:       summaryJSON(res.Summary),
		Disagreements: dis,
		Runs:          runs,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(out, b, 0o644)
}

// decisionJSON renders one captured decisionRecord (llm_decision/agree are null unless the model was
// computed for that decision — Python's m.get(k) on absent keys; LLMSet is the Go "key present" flag).
func decisionJSON(d decisionRecord) jsDecision {
	jd := jsDecision{
		Decision:          d.Decision,
		HeuristicDecision: d.HeuristicDecision,
		Escalated:         d.Escalated,
		Ambiguity:         json.Number(pyFloat(d.Ambiguity)),
		Reason:            d.Reason,
	}
	if d.LLMSet {
		v := d.LLMDecision
		a := d.Agree
		jd.LLMDecision = &v
		jd.Agree = &a
	}
	return jd
}

// summaryJSON renders the per-mode summary (agreement null when no compared decisions). The summary is
// keyed by mode like Python; json.Marshal alphabetises the mode keys, but the Python dict is keyed by
// mode-encounter order (heuristic, llm, hybrid) — the keys still round-trip, and the CLI table prints
// in the fixed heuristic/llm/hybrid order regardless, so only the saved file's mode-key order differs.
func summaryJSON(s map[string]ModeSummary) map[string]jsModeSummary {
	out := make(map[string]jsModeSummary, len(s))
	for mode, m := range s {
		ms := jsModeSummary{
			Correct: m.Correct, Total: m.Total, Decisions: m.Decisions,
			Escalations: m.Escalations, LLMCalls: m.LLMCalls, Fallbacks: m.Fallbacks,
			Compared: m.Compared,
		}
		if m.Agreement != nil {
			n := json.Number(pyFloat(*m.Agreement))
			ms.Agreement = &n
		}
		out[mode] = ms
	}
	return out
}

// ---------------------------------------------------------------------------
// stability (durability under dynamic synthesis)
// ---------------------------------------------------------------------------

func cmdStability(argv []string) int {
	fs := flag.NewFlagSet("stability", flag.ContinueOnError)
	axis := fs.Bool("axis", false, "print the stability AXIS: durability vs self-modification level (deterministic offline)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}
	if *axis {
		return stability.MainAxis()
	}
	return stability.Main()
}

// ---------------------------------------------------------------------------
// conformance — the L0 conformance ROLLUP (Track H, benchmark-taxonomy §1 L0 + §5 build-order #1):
// run S1..S16 + the requirement checklist + the wiring scan ⇒ one PASS/FAIL. Deterministic, offline,
// no model. Exit 0 ⇒ PASS, 1 ⇒ FAIL, 2 ⇒ bad flags.
// ---------------------------------------------------------------------------

func cmdConformance(argv []string) int {
	fs := flag.NewFlagSet("conformance", flag.ContinueOnError)
	var lf logFlags
	addLogFlags(fs, &lf)
	verbose := fs.Bool("verbose", false, "print every per-scenario check (default: only the rollup + failures)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	// A dedicated bus for the rollup verdict + the per-scenario wiring witnesses, so --log captures the
	// conformance.* stream as JSONL. (Each scenario engine has its OWN bus inside the rollup; this bus
	// carries only the rollup-level verdict.)
	bus := events.NewDefault()
	var closeLog func()
	if lf.logPath != "" {
		sink, err := trace.NewJsonlSink(lf.logPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, "conformance:", err)
			return 2
		}
		bus.Subscribe(sink.On)
		closeLog = func() { _ = sink.Close() }
		fmt.Printf("(logging conformance verdict -> %s)\n", lf.logPath)
	}

	res := conformance.Run(bus)
	if closeLog != nil {
		closeLog()
	}

	verdict := "PASS"
	if !res.Pass {
		verdict = "FAIL"
	}
	fmt.Printf("L0 conformance: %s  (%d/%d checks over %d scenarios; wiring %s)\n",
		verdict, res.ChecksPassed, res.ChecksTotal, res.Scenarios,
		map[bool]string{true: "OK", false: "INCOMPLETE"}[res.WiringOK])

	if *verbose {
		for _, rr := range res.Runs {
			rv := "PASS"
			if !rr.Pass {
				rv = "FAIL"
			}
			fmt.Printf("  %-4s %s  (%d events, %d layers)\n", rr.ID, rv, rr.Events, len(rr.Covered))
			for _, c := range rr.Checks {
				if !c.Pass {
					fmt.Printf("        - FAIL %s: %s\n", c.Name, c.Why)
				}
			}
		}
	}
	if len(res.SuiteMissing) > 0 {
		fmt.Printf("  suite-wide wiring missing: %s\n", strings.Join(res.SuiteMissing, ", "))
	}
	if !res.Pass {
		fmt.Println("failures:")
		for _, f := range res.Failures {
			fmt.Printf("  - %s\n", f)
		}
		return 1
	}
	return 0
}

// ---------------------------------------------------------------------------
// state — registry ledger (W1): snapshot / reset / diff / list
// ---------------------------------------------------------------------------

func cmdState(argv []string) int {
	fs := flag.NewFlagSet("state", flag.ContinueOnError)
	var ff featureFlags
	var lf logFlags
	addFeatureFlags(fs, &ff)
	addLogFlags(fs, &lf)
	action := fs.String("action", "list", "snapshot | reset | diff | list")
	name := fs.String("name", "", "snapshot name (for snapshot/reset)")
	from := fs.String("from", "", "source snapshot name (for diff)")
	to := fs.String("to", "", "target snapshot name (for diff)")
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	store, err := resolveStore(&ff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if store == nil {
		fmt.Fprintln(os.Stderr, "state: --state DIR is required (persistence must be enabled)")
		return 2
	}

	js, ok := store.(persist.Store)
	if !ok {
		fmt.Fprintln(os.Stderr, "state: store does not implement the registry ledger interface")
		return 2
	}

	// Wire the bus → JSONL sink so the ledger's registry.* decisions are observable from the CLI path
	// too (the observability contract). nil --log ⇒ the store stays silent (emit unset). Best-effort.
	if closeLog := wireStoreLog(js, lf); closeLog != nil {
		defer closeLog()
	}

	// Load the store to populate the in-memory snapshot from disk
	if _, err := js.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "state: failed to load store:", err)
		return 1
	}

	switch *action {
	case "list":
		return cmdStateList(js)
	case "snapshot":
		return cmdStateSnapshot(js, *name)
	case "reset":
		return cmdStateReset(js, *name)
	case "diff":
		return cmdStateDiff(js, *from, *to)
	default:
		fmt.Fprintf(os.Stderr, "state: unknown action %q (snapshot|reset|diff|list)\n", *action)
		return 2
	}
}

func cmdStateList(store persist.Store) int {
	metas, err := store.ListSnapshots()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(metas) == 0 {
		fmt.Println("(no snapshots)")
		return 0
	}
	fmt.Printf("%-20s %-12s %-10s %-8s %-8s %-8s %-8s %-8s %-8s %s\n",
		"NAME", "SUBSTRATE", "CREATED", "SKILLS", "OPS", "SPECS", "EP", "BELIEFS", "KNOW", "PREFS")
	fmt.Println(strings.Repeat("-", 120))
	for _, m := range metas {
		created := time.Unix(0, m.CreatedAt).Format("2006-01-02 15:04")
		fmt.Printf("%-20s %-12s %-10s %-8d %-8d %-8d %-8d %-8d %-8d %d\n",
			m.Name, m.Substrate, created, m.SkillCount, m.OperatorCount,
			m.PrimitiveSubAgentCount, m.EpisodeCount, m.BeliefCount,
			m.KnowledgeCount, m.PreferenceCount)
	}
	return 0
}

func cmdStateSnapshot(store persist.Store, name string) int {
	if strings.TrimSpace(name) == "" {
		fmt.Fprintln(os.Stderr, "state snapshot: --name is required")
		return 2
	}
	// Load current state to snapshot
	snap := store.Snapshot()
	if snap == nil {
		fmt.Fprintln(os.Stderr, "state snapshot: no live state to snapshot")
		return 1
	}
	rec := persist.SnapshotRecord{
		Meta: persist.SnapshotMeta{
			Name:      name,
			CreatedAt: time.Now().UnixNano(),
			Substrate: "cli",
		},
		Data: *snap,
	}
	if err := store.SaveSnapshot(rec); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("snapshot saved: %s\n", name)
	return 0
}

func cmdStateReset(store persist.Store, name string) int {
	if strings.TrimSpace(name) == "" {
		fmt.Fprintln(os.Stderr, "state reset: --name is required")
		return 2
	}
	if err := store.ResetToSnapshot(name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	// ResetToSnapshot replaces the IN-MEMORY live set; the CLI is a one-shot process, so without
	// this Flush the reverted state never reaches disk and the revert is a silent no-op (W1 audit bug).
	if err := store.Flush(); err != nil {
		fmt.Fprintln(os.Stderr, "state reset: reverted in memory but failed to persist:", err)
		return 1
	}
	fmt.Printf("state reset to snapshot: %s\n", name)
	return 0
}

func cmdStateDiff(store persist.Store, from, to string) int {
	if strings.TrimSpace(from) == "" || strings.TrimSpace(to) == "" {
		fmt.Fprintln(os.Stderr, "state diff: --from and --to are required")
		return 2
	}
	diff, err := store.DiffSnapshots(from, to)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("diff: %s -> %s\n\n", from, to)
	printDiff("Added", diff.Added)
	printDiff("Removed", diff.Removed)
	printDiff("Changed", diff.Changed)
	return 0
}

func printDiff(label string, m map[string]int) {
	if len(m) == 0 {
		return
	}
	fmt.Printf("%s:\n", label)
	for artifact, count := range m {
		fmt.Printf("  %s: %d\n", artifact, count)
	}
	fmt.Println()
}

// ---------------------------------------------------------------------------
// ledger — self-change ledger (W1): list / config / safety-mode
// ---------------------------------------------------------------------------

func cmdLedger(argv []string) int {
	fs := flag.NewFlagSet("ledger", flag.ContinueOnError)
	var ff featureFlags
	var lf logFlags
	addFeatureFlags(fs, &ff)
	addLogFlags(fs, &lf)
	action := fs.String("action", "list", "list | config | safety-mode")
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	store, err := resolveStore(&ff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if store == nil {
		fmt.Fprintln(os.Stderr, "ledger: --state DIR is required (persistence must be enabled)")
		return 2
	}

	js, ok := store.(persist.Store)
	if !ok {
		fmt.Fprintln(os.Stderr, "ledger: store does not implement the ledger interface")
		return 2
	}

	if closeLog := wireStoreLog(js, lf); closeLog != nil {
		defer closeLog()
	}

	// Load the store to populate the in-memory snapshot from disk
	if _, err := js.Load(); err != nil {
		fmt.Fprintln(os.Stderr, "ledger: failed to load store:", err)
		return 1
	}

	switch *action {
	case "list":
		return cmdLedgerList(js)
	case "config":
		return cmdLedgerConfig(js, fs.Args())
	case "safety-mode":
		return cmdLedgerSafetyMode(js, fs.Args())
	default:
		fmt.Fprintf(os.Stderr, "ledger: unknown action %q (list|config|safety-mode)\n", *action)
		return 2
	}
}

func cmdLedgerList(store persist.Store) int {
	entries, err := store.LoadLedger()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if len(entries) == 0 {
		fmt.Println("(no ledger entries)")
		return 0
	}
	fmt.Printf("%-20s %-6s %-12s %-8s %s\n", "TIMESTAMP", "TICK", "SCOPE", "MODE", "DESCRIPTION")
	fmt.Println(strings.Repeat("-", 120))
	for _, e := range entries {
		ts := time.Unix(0, e.Timestamp).Format("2006-01-02 15:04")
		fmt.Printf("%-20s %-6d %-12s %-8s %s\n", ts, e.Tick, e.Scope, e.SafetyMode, e.Description)
		if e.Evidence != "" {
			fmt.Printf("  evidence: %s\n", e.Evidence)
		}
		if e.GatePassed != "" {
			fmt.Printf("  gate: %s\n", e.GatePassed)
		}
		if e.RevertHandle != "" {
			fmt.Printf("  revert: %s\n", e.RevertHandle)
		}
		fmt.Println()
	}
	return 0
}

func cmdLedgerConfig(store persist.Store, args []string) int {
	if len(args) == 0 {
		// Show current config
		cfg, err := store.LoadLedgerConfig()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Println("Current ledger config:")
		fmt.Printf("  enabled:      %v\n", cfg.Enabled)
		fmt.Printf("  safety_mode:  %s\n", cfg.SafetyMode)
		fmt.Printf("  max_entries:  %d\n", cfg.MaxEntries)
		fmt.Printf("  require_gate: %v\n", cfg.RequireGate)
		fmt.Printf("  auto_snapshot: %v\n", cfg.AutoSnapshot)
		return 0
	}
	// Set config key=value
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "ledger config: expected one key=value argument")
		return 2
	}
	parts := strings.SplitN(args[0], "=", 2)
	if len(parts) != 2 {
		fmt.Fprintln(os.Stderr, "ledger config: expected key=value")
		return 2
	}
	key, value := parts[0], parts[1]
	cfg, err := store.LoadLedgerConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	switch key {
	case "enabled":
		cfg.Enabled = value == "true" || value == "on" || value == "1"
	case "safety_mode":
		if value != "SAFE" && value != "EXPAND" && value != "REWRITE" {
			fmt.Fprintln(os.Stderr, "ledger config: safety_mode must be SAFE, EXPAND, or REWRITE")
			return 2
		}
		cfg.SafetyMode = persist.SafetyMode(value)
	case "max_entries":
		var n int
		fmt.Sscanf(value, "%d", &n)
		if n < 0 {
			fmt.Fprintln(os.Stderr, "ledger config: max_entries must be >= 0")
			return 2
		}
		cfg.MaxEntries = n
	case "require_gate":
		cfg.RequireGate = value == "true" || value == "on" || value == "1"
	case "auto_snapshot":
		cfg.AutoSnapshot = value == "true" || value == "on" || value == "1"
	default:
		fmt.Fprintf(os.Stderr, "ledger config: unknown key %q\n", key)
		return 2
	}
	if err := store.SaveLedgerConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("ledger config updated: %s=%s\n", key, value)
	return 0
}

func cmdLedgerSafetyMode(store persist.Store, args []string) int {
	if len(args) == 0 {
		// Show current safety mode
		cfg, err := store.LoadLedgerConfig()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		fmt.Printf("Current safety mode: %s\n", cfg.SafetyMode)
		fmt.Println("Allowed scopes:")
		fmt.Println("  SAFE (default): S0 (parameters), S1 (registry content)")
		fmt.Println("  EXPAND: S0, S1, S2 (structure) — EXPERIMENTAL, LOCKED")
		fmt.Println("  REWRITE: S0, S1, S2, S3 (code) — EXPERIMENTAL, LOCKED")
		return 0
	}
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "ledger safety-mode: expected one mode (SAFE|EXPAND|REWRITE)")
		return 2
	}
	mode := args[0]
	if mode != "SAFE" && mode != "EXPAND" && mode != "REWRITE" {
		fmt.Fprintln(os.Stderr, "ledger safety-mode: mode must be SAFE, EXPAND, or REWRITE")
		return 2
	}
	cfg, err := store.LoadLedgerConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if mode == "EXPAND" || mode == "REWRITE" {
		fmt.Printf("WARNING: %s is EXPERIMENTAL and LOCKED (changes the plant). ", mode)
		fmt.Println("Requires explicit confirmation via config.")
	}
	cfg.SafetyMode = persist.SafetyMode(mode)
	if err := store.SaveLedgerConfig(cfg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Safety mode set to: %s\n", mode)
	return 0
}

// ---------------------------------------------------------------------------
// tui — the product surface: build the engine, construct the App, run the Bubble Tea program.
// ---------------------------------------------------------------------------

// cmdTUI builds an engine and runs the Bubble Tea app (internal/tui.Run wraps the tea.Program — this
// file never imports charmbracelet). The TUI is the PRODUCT surface: it resolves a REAL model substrate
// by default (no offline fallback — a BackendUnavailable error aborts with a notice, DESIGN §8). Its
// product defaults are the awake CONTINUOUS loop on the CLAUDE bridge (claude-first dev/validation
// substrate) — so a bare `thought tui` opens straight onto the live continuous mind. The dev/CI override
// flags (--backend test, --mode reactive, --llm-url/--llm-model, --workspace, --seed/--cognition) and
// THOUGHT_SUBSTRATE still win, so the binary stays runnable offline for development. The CLI does NOT
// wire the console trace sinks here: the TUI subscribes its own panel drains through the bridge, and a
// console tracer would print over the alt-screen.
func cmdTUI(argv []string) int {
	fs := flag.NewFlagSet("tui", flag.ContinueOnError)
	mode := fs.String("mode", "continuous", "reactive | continuous (= awake)")
	seed := fs.Int("seed", 7, "seeded RNG")
	prompt := fs.String("prompt", "", "an initial prompt submitted on mount (Python tui --prompt)")
	cognition := fs.String("cognition", "control",
		"decision mode: control(rules) | llm(AI) | hybrid (llm/hybrid need a model)")
	var bf backendFlags
	var ff featureFlags
	addBackendFlags(fs, &bf, true)
	addFeatureFlags(fs, &ff)
	if err := fs.Parse(argv); err != nil {
		return 2
	}

	cfg := engine.DefaultConfig()
	cfg.Mode = *mode
	cfg.Seed = *seed
	cfg.Cognition = *cognition
	cfg.Workspace = bf.workspace
	// TUI product defaults (DECIDED: claude-first dev/validation substrate, awake-by-default surface):
	// launching `thought tui` drops straight into the live continuous mind on the claude bridge
	// (frontier, no GPU). An explicit THOUGHT_SUBSTRATE env or a `--backend` dev override still wins
	// (the override path below ignores cfg.Substrate entirely; the env-pinned case is respected here).
	if os.Getenv("THOUGHT_SUBSTRATE") == "" {
		cfg.Substrate = "claude"
	}
	cfg.LLMMaxTokens = bf.llmMaxTokens // 0 → env/default 4096 (reasoning headroom)
	if bf.llmURL != "" {
		cfg.LLMBaseURL = bf.llmURL
	}
	if bf.llmModel != "" {
		cfg.LLMModel = bf.llmModel
	}
	// CONFIG (M1): the system-wide HarnessConfig (toggles + the representation matrix). The shared
	// pointer carries through the TUI App/Bridge, so a Rebuild keeps it (and any TUI live-flip), fixing
	// the live-session config-loss bug. nil-free: resolveFeatures returns AllOn() when nothing is set.
	feat, err := resolveFeatures(&ff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg.Features = feat
	cfg.FeaturesPath = ff.configPath // display-only: the TUI Config tab surfaces the loaded config path
	// A profile carries its own loop regime (awake profiles are continuous); it wins over the --mode
	// default so `thought tui --profile awake` drops straight into the living awake mind.
	if p, ok := config.ProfileByName(ff.profile); ok {
		cfg.Mode = p.Mode
		cfg.Profile = p.Name
		if p.Persist && strings.TrimSpace(ff.stateDir) == "" { // self-contained: auto-persist learned state
			sub := cfg.Substrate
			if bf.backend != "" { // a --backend dev override decides the real class (test ⇒ no persist)
				sub = bf.backend
			}
			ff.stateDir = engine.DefaultStateDir(sub)
		}
	}
	// PERSIST (M4): --state DIR enables cross-session persistence of learned artifacts. The Store carries
	// through the App/Bridge across a Rebuild (a Settings change reloads the persisted state, not drops it).
	store, err := resolveStore(&ff)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	cfg.Store = store

	var eng *engine.Engine
	if bf.backend != "" {
		// dev override: an explicit backend (e.g. --backend test for offline dev/CI) — build it now
		// (instant; no model probe), so the TUI opens straight onto a live engine.
		e, err := newEngineWith(cfg, bf)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			fmt.Fprintln(os.Stderr,
				"(no reachable model — start a local server, or run offline with `thought tui --backend test`)")
			return 1
		}
		eng = e
	}
	// product path (no --backend): leave eng nil. The TUI resolves a real model substrate — and
	// auto-loads a local LM Studio model if none is loaded — ASYNCHRONOUSLY on the welcome screen, so
	// the UI appears instantly instead of blocking the launch on a model probe/load (DESIGN §8). A
	// BackendUnavailable surfaces as a notice on the welcome card, not a failed launch.
	if err := tui.Run(eng, cfg, *prompt); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if eng != nil {
		eng.FlushState() // persist learned state + resume cursor on TUI exit (edge flush; the bridge already flushes on engine swap)
	}
	return 0
}

// ---------------------------------------------------------------------------
// run summary (Python _summary)
// ---------------------------------------------------------------------------

// printSummary prints the post-run snapshot Python's _summary builds: backend (+call tallies),
// lifecycle/arousal/ticks, the durability metrics + stability count, and the thought-graph shape
// (+minted specialists). It reaches engine state through the read-only accessors (the engine itself
// never prints). This is intentionally the CLI's job — the engine stays headless-pure.
func printSummary(eng *engine.Engine) {
	fmt.Println()
	fmt.Println(strings.Repeat("-", 64))

	backendNote := eng.BackendLabel()
	if llmBe := asLLM(eng.Backend()); llmBe != nil {
		backendNote += fmt.Sprintf(" (calls=%d, fallbacks=%d)", llmBe.Calls, llmBe.Fallbacks)
	}
	fmt.Printf("backend: %s\n", backendNote)
	fmt.Printf("lifecycle: %s | arousal: %s | ticks: %d\n",
		eng.LifecycleState(), eng.Arousal().String(), eng.Bus().Tick)

	r := eng.Regulator()
	fmt.Printf("durability: θ=%.2f λ̂=%.2f λ̄=%s n=%.2f μ=%.2f U=%.2f\n",
		r.Theta(), r.LamHat(), fmtLamBar(r.LamBar()), r.N(), r.Mu(), r.Util())

	// emit=true so the end-of-run stability check lands on the bus → the golden JSONL sink (still
	// subscribed; closeLog is deferred). Mirrors Python _summary's r.stability(eng.mode) default
	// emit=True: every reactive scenario closes with one regulator.stability line carrying the
	// per-check booleans + the reactive mu>0 N/A note. (Python: thought_harness/__main__.py:238.)
	checks := r.Stability(eng.Mode(), true)
	hold := 0
	for _, c := range checks {
		if c.Pass && !c.NA {
			hold++
		}
	}
	fmt.Printf("stability: %d/%d hard checks hold\n", hold, len(checks))

	if g := eng.Graph(); g != nil {
		thoughts := g.ActiveContext()
		fmt.Printf("thought graph: %d nodes, %d branches; active b%d (%d thoughts)\n",
			len(g.Nodes), len(g.Branches), g.ActiveBranch, len(thoughts))
		if minted := eng.Convert().Minted; len(minted) > 0 {
			fmt.Printf("minted specialists: %s\n", strings.Join(minted, ", "))
		}
	}
}

// ---------------------------------------------------------------------------
// small helpers (Python str()/format analogues; cmd-local, no core import)
// ---------------------------------------------------------------------------

// asLLM returns the underlying OpenAICompatBackend (the call-tally carrier) for a bare or tiered LLM
// backend, or nil for the heuristic — the cmd-side analogue of Python's `hasattr(backend, "calls")`.
func asLLM(b backends.Backend) *llm.OpenAICompatBackend {
	switch x := b.(type) {
	case *llm.OpenAICompatBackend:
		return x
	case *llm.TieredBackend:
		return x.Primary
	default:
		return nil
	}
}

// fmtLamBar formats λ̄ for the summary line: "∞" for +∞, else two decimals (Python Regulator._fmt).
func fmtLamBar(x float64) string {
	if math.IsInf(x, 1) {
		return "∞"
	}
	return fmt.Sprintf("%.2f", x)
}

// pyRepr renders a string the way Python's repr() chooses quotes for the simple CLI cases here
// (the f-string `{x!r}` interpolations in __main__). Python prefers single quotes, but switches to
// double quotes when the string contains a single quote and no double quote (e.g. "What's 7×8?"),
// and escapes the chosen quote otherwise. That quote-selection rule is reproduced so the scenario
// header / error text match Python byte-for-byte; control-char escaping is not reproduced (the values
// here — scenario ids, prompts, model names, short snippets — do not contain control chars).
func pyRepr(s string) string {
	hasSingle := strings.ContainsRune(s, '\'')
	hasDouble := strings.ContainsRune(s, '"')
	quote := byte('\'')
	if hasSingle && !hasDouble {
		quote = '"'
	}
	var sb strings.Builder
	sb.WriteByte(quote)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == quote || c == '\\' {
			sb.WriteByte('\\')
		}
		sb.WriteByte(c)
	}
	sb.WriteByte(quote)
	return sb.String()
}

// pyFloat renders a float the way Python's str()/f-string default would for the ambiguity value in
// the disagreement line (Python interpolates the raw float, e.g. 0.0 → "0.0", 0.5 → "0.5"). Go's %v
// drops the trailing ".0" on a whole float; this re-adds it so a whole-valued ambiguity matches
// Python (a finite float with no '.'/'e' gets a ".0" suffix).
func pyFloat(x float64) string {
	s := strconv.FormatFloat(x, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEnN") { // no decimal point, exponent, or Inf/NaN word
		s += ".0"
	}
	return s
}

// bracketList renders a string slice as Python's list repr (`['S1', 'S3']`) for the compare header,
// matching `print(f"... on {scenarios} ...")` where scenarios is a Python list of strs.
func bracketList(xs []string) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = pyRepr(x)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// scenarioValueFlags are the value-taking flags of the `scenario` subcommand — needed by
// extractPositional to know that the token after one of these is a flag VALUE (in the `--flag value`
// form), not the positional scenario id. The bool flags (--list/--verbose) take no value.
var scenarioValueFlags = map[string]bool{
	"-backend": true, "--backend": true,
	"-llm-url": true, "--llm-url": true,
	"-llm-model": true, "--llm-model": true,
	"-log": true, "--log": true,
	"-layer": true, "--layer": true,
}

// extractPositional pulls the first bare positional (the scenario id) out of argv, returning it and
// the remaining args (the flags) for flag.Parse. argparse interleaves positionals and optionals; Go's
// flag package stops at the first non-flag token, so without this `scenario S1 --backend x` would lose
// the flag. A token is the positional iff it does not start with "-" and is not the VALUE of a
// preceding value-taking flag given in `--flag value` form (a `--flag=value` token consumes its own
// value). Only the FIRST such positional is extracted (the scenario takes one).
func extractPositional(argv []string) (string, []string) {
	name := ""
	rest := make([]string, 0, len(argv))
	expectValue := false
	for _, tok := range argv {
		if expectValue {
			rest = append(rest, tok)
			expectValue = false
			continue
		}
		if strings.HasPrefix(tok, "-") {
			rest = append(rest, tok)
			if scenarioValueFlags[tok] && !strings.Contains(tok, "=") {
				expectValue = true
			}
			continue
		}
		if name == "" {
			name = tok // the scenario id
			continue
		}
		rest = append(rest, tok) // extra positionals (none expected) flow to flag.Parse
	}
	return name, rest
}

// containsStr reports whether xs contains s.
func containsStr(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// runeHead returns the first n code points of s (Python str slicing s[:n], by rune not byte).
func runeHead(s string, n int) string {
	r := []rune(s)
	if n < 0 {
		n = 0
	}
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// sortedKeys returns a map's keys sorted (deterministic signal-line ordering). Python iterates the
// signals dict in insertion order; Go map order is random, so the keys are sorted for a stable line.
func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// signalFloat coerces a signal value to float64 for the `%+.2f` line (the signals dict carries numeric
// breakdowns; a non-numeric value renders as 0.0, matching how the Python f-string would only ever see
// the numeric signal values it prints).
func signalFloat(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	default:
		return 0.0
	}
}
