package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"sketch.dev/experiment"
	"sketch.dev/llm"
	"sketch.dev/llm/gem"
	"sketch.dev/llm/oai"

	"github.com/richardlehane/crock32"
	"sketch.dev/browser"
	"sketch.dev/dockerimg"
	"sketch.dev/httprr"
	"sketch.dev/llm/ant"
	"sketch.dev/llm/conversation"
	"sketch.dev/loop"
	"sketch.dev/loop/server"
	"sketch.dev/skabandclient"
	"sketch.dev/skribe"
	"sketch.dev/termui"

	"golang.org/x/term"
)

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v: %v\n", os.Args[0], err)
		os.Exit(1)
	}
}

// run is the main entry point that parses flags and dispatches to the appropriate
// execution path based on whether we're running in a container or not.
func run() error {
	flagArgs := parseCLIFlags()

	if flagArgs.version {
		bi, ok := debug.ReadBuildInfo()
		if ok {
			fmt.Printf("%s@%v\n", bi.Path, bi.Main.Version)
		}
		return nil
	}

	if flagArgs.listModels {
		fmt.Println("Available models:")
		fmt.Println("- claude (default, uses Anthropic service)")
		fmt.Println("- gemini (uses Google Gemini 2.5 Pro service)")
		for _, name := range oai.ListModels() {
			note := ""
			if name != "gpt4.1" {
				note = " (not recommended)"
			}
			fmt.Printf("- %s%s\n", name, note)
		}
		return nil
	}

	// Claude and Gemini are supported in container mode
	// TODO: finish support--thread through API keys, add server support
	isContainerSupported := flagArgs.modelName == "claude" || flagArgs.modelName == "" || flagArgs.modelName == "gemini"
	if !isContainerSupported && (!flagArgs.unsafe || flagArgs.skabandAddr != "") {
		return fmt.Errorf("only -model=claude is supported in safe mode right now, use -unsafe -skaband-addr=''")
	}

	if err := flagArgs.experimentFlag.Process(); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing experimental flags: %v\n", err)
		os.Exit(1)
	}
	if experiment.Enabled("list") {
		experiment.Fprint(os.Stdout)
		os.Exit(0)
	}

	// Add a global "session_id" to all logs using this context.
	// A "session" is a single full run of the agent.
	ctx := skribe.ContextWithAttr(context.Background(), slog.String("session_id", flagArgs.sessionID))

	// Configure logging
	slogHandler, logFile, err := setupLogging(flagArgs.oneShot, flagArgs.verbose, flagArgs.unsafe)
	if err != nil {
		return err
	}
	if logFile != nil {
		defer logFile.Close()
	}
	slog.SetDefault(slog.New(slogHandler))

	// Change to working directory if specified
	if flagArgs.workingDir != "" {
		if err := os.Chdir(flagArgs.workingDir); err != nil {
			return fmt.Errorf("sketch: cannot change directory to %q: %v", flagArgs.workingDir, err)
		}
	}

	// Set default git username and email if not provided
	if flagArgs.gitUsername == "" {
		flagArgs.gitUsername = defaultGitUsername()
	}
	if flagArgs.gitEmail == "" {
		flagArgs.gitEmail = defaultGitEmail()
	}

	// Detect if we're inside the sketch container
	inInsideSketch := flagArgs.outsideHostname != ""

	// Validate initial commit and unsafe flag combination
	if flagArgs.unsafe && flagArgs.initialCommit != "HEAD" {
		return fmt.Errorf("cannot use -initial-commit with -unsafe, they are incompatible")
	}

	// Dispatch to the appropriate execution path
	if inInsideSketch {
		// We're running inside the Docker container
		return runInContainerMode(ctx, flagArgs, logFile)
	} else if flagArgs.unsafe {
		// We're running directly on the host in unsafe mode
		return runInUnsafeMode(ctx, flagArgs, logFile)
	} else {
		// We're running on the host and need to launch a container
		return runInHostMode(ctx, flagArgs)
	}
}

// CLIFlags holds all command-line arguments
type CLIFlags struct {
	addr              string
	skabandAddr       string
	unsafe            bool
	openBrowser       bool
	httprrFile        string
	maxIterations     uint64
	maxWallTime       time.Duration
	maxDollars        float64
	oneShot           bool
	prompt            string
	modelName         string
	listModels        bool
	verbose           bool
	version           bool
	workingDir        string
	sshPort           int
	forceRebuild      bool
	initialCommit     string
	gitUsername       string
	gitEmail          string
	experimentFlag    experiment.Flag
	sessionID         string
	record            bool
	noCleanup         bool
	containerLogDest  string
	outsideHostname   string
	outsideOS         string
	outsideWorkingDir string
	sketchBinaryLinux string
	dockerArgs        string
	termUI            bool
}

// parseCLIFlags parses all command-line flags and returns a CLIFlags struct
func parseCLIFlags() CLIFlags {
	var flags CLIFlags

	openBrowserDefault := os.Getenv("SSH_CONNECTION") == ""
	flag.StringVar(&flags.addr, "addr", "localhost:0", "local debug HTTP server address")
	flag.StringVar(&flags.skabandAddr, "skaband-addr", "https://sketch.dev", "URL of the skaband server")
	flag.BoolVar(&flags.unsafe, "unsafe", false, "run directly without a docker container")
	flag.BoolVar(&flags.openBrowser, "open", openBrowserDefault, "open sketch URL in system browser; on by default except if -one-shot is used or a ssh connection is detected")
	flag.StringVar(&flags.httprrFile, "httprr", "", "if set, record HTTP interactions to file")
	flag.Uint64Var(&flags.maxIterations, "max-iterations", 0, "maximum number of iterations the agent should perform per turn, 0 to disable limit")
	flag.DurationVar(&flags.maxWallTime, "max-wall-time", 0, "maximum time the agent should run per turn, 0 to disable limit")
	flag.Float64Var(&flags.maxDollars, "max-dollars", 5.0, "maximum dollars the agent should spend per turn, 0 to disable limit")
	flag.BoolVar(&flags.oneShot, "one-shot", false, "exit after the first turn without termui")
	flag.StringVar(&flags.prompt, "prompt", "", "prompt to send to sketch")
	flag.StringVar(&flags.modelName, "model", "claude", "model to use (e.g. claude, gpt4.1)")
	flag.BoolVar(&flags.listModels, "list-models", false, "list all available models and exit")
	flag.BoolVar(&flags.verbose, "verbose", false, "enable verbose output")
	flag.BoolVar(&flags.version, "version", false, "print the version and exit")
	flag.StringVar(&flags.workingDir, "C", "", "when set, change to this directory before running")
	flag.IntVar(&flags.sshPort, "ssh_port", 0, "the host port number that the container's ssh server will listen on, or a randomly chosen port if this value is 0")
	flag.BoolVar(&flags.forceRebuild, "force-rebuild-container", false, "rebuild Docker container")
	flag.StringVar(&flags.initialCommit, "initial-commit", "HEAD", "the git commit reference to use as starting point (incompatible with -unsafe)")
	flag.StringVar(&flags.dockerArgs, "docker-args", "", "additional arguments to pass to the docker create command (e.g., --memory=2g --cpus=2)")
	flag.BoolVar(&flags.termUI, "termui", true, "enable terminal UI")

	// Flags geared towards sketch developers or sketch internals:
	flag.StringVar(&flags.gitUsername, "git-username", "", "(internal) username for git commits")
	flag.StringVar(&flags.gitEmail, "git-email", "", "(internal) email for git commits")
	flag.StringVar(&flags.sessionID, "session-id", newSessionID(), "(internal) unique session-id for a sketch process")
	flag.BoolVar(&flags.record, "httprecord", true, "(debugging) Record trace (if httprr is set)")
	flag.BoolVar(&flags.noCleanup, "nocleanup", false, "(debugging) do not clean up docker containers on exit")
	flag.StringVar(&flags.containerLogDest, "save-container-logs", "", "(debugging) host path to save container logs to on exit")
	flag.StringVar(&flags.outsideHostname, "outside-hostname", "", "(internal) hostname on the outside system")
	flag.StringVar(&flags.outsideOS, "outside-os", "", "(internal) OS on the outside system")
	flag.StringVar(&flags.outsideWorkingDir, "outside-working-dir", "", "(internal) working dir on the outside system")
	flag.StringVar(&flags.sketchBinaryLinux, "sketch-binary-linux", "", "(development) path to a pre-built sketch binary for linux")
	flag.Var(&flags.experimentFlag, "x", "enable experimental features (comma-separated list or repeat flag; use 'list' to show all)")

	flag.Parse()

	// -open's default value should be true normally but false in one-shot mode.
	// Distinguish between -open default value vs explicitly set.
	openExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "open" {
			openExplicit = true
		}
	})
	if !openExplicit {
		flags.openBrowser = !flags.oneShot
	}

	return flags
}

// runInHostMode handles execution on the host machine, which typically involves
// checking host requirements and launching a Docker container.
func runInHostMode(ctx context.Context, flags CLIFlags) error {
	// Check host requirements
	msgs, err := hostReqsCheck(flags.unsafe)
	if flags.verbose {
		fmt.Println("Host requirement checks:")
		for _, m := range msgs {
			fmt.Println(m)
		}
	}
	if err != nil {
		return err
	}

	// Get credentials and connect to skaband if needed
	privKey, err := skabandclient.LoadOrCreatePrivateKey(skabandclient.DefaultKeyPath())
	if err != nil {
		return err
	}
	pubKey, modelURL, apiKey, err := skabandclient.Login(os.Stdout, privKey, flags.skabandAddr, flags.sessionID, flags.modelName)
	if err != nil {
		return err
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("sketch: cannot determine current working directory: %v", err)
	}

	// Configure and launch the container
	config := dockerimg.ContainerConfig{
		SessionID:         flags.sessionID,
		LocalAddr:         flags.addr,
		SkabandAddr:       flags.skabandAddr,
		Model:             flags.modelName,
		ModelURL:          modelURL,
		ModelAPIKey:       apiKey,
		Path:              cwd,
		GitUsername:       flags.gitUsername,
		GitEmail:          flags.gitEmail,
		OpenBrowser:       flags.openBrowser,
		NoCleanup:         flags.noCleanup,
		ContainerLogDest:  flags.containerLogDest,
		SketchBinaryLinux: flags.sketchBinaryLinux,
		SketchPubKey:      pubKey,
		SSHPort:           flags.sshPort,
		ForceRebuild:      flags.forceRebuild,
		OutsideHostname:   getHostname(),
		OutsideOS:         runtime.GOOS,
		OutsideWorkingDir: cwd,
		OneShot:           flags.oneShot,
		Prompt:            flags.prompt,
		InitialCommit:     flags.initialCommit,
		Verbose:           flags.verbose,
		DockerArgs:        flags.dockerArgs,
		ExperimentFlag:    flags.experimentFlag.String(),
	}

	if err := dockerimg.LaunchContainer(ctx, config); err != nil {
		if flags.verbose {
			fmt.Fprintf(os.Stderr, "dockerimg launch container failed: %v\n", err)
		}
		return err
	}

	return nil
}

// runInContainerMode handles execution inside the Docker container.
// The inInsideSketch parameter indicates whether we're inside the sketch container
// with access to outside environment variables.
func runInContainerMode(ctx context.Context, flags CLIFlags, logFile *os.File) error {
	// Get credentials from environment
	apiKey := os.Getenv("SKETCH_MODEL_API_KEY")
	pubKey := os.Getenv("SKETCH_PUB_KEY")
	modelURL, err := skabandclient.LocalhostToDockerInternal(os.Getenv("SKETCH_MODEL_URL"))
	if err != nil && os.Getenv("SKETCH_MODEL_URL") != "" {
		return err
	}

	return setupAndRunAgent(ctx, flags, modelURL, apiKey, pubKey, true, logFile)
}

// runInUnsafeMode handles execution on the host machine without Docker.
// This mode is used when the -unsafe flag is provided.
func runInUnsafeMode(ctx context.Context, flags CLIFlags, logFile *os.File) error {
	// Check if we need to get the API key from environment
	var apiKey, antURL, pubKey string

	if flags.skabandAddr == "" {
		envName := "ANTHROPIC_API_KEY"
		if flags.modelName == "gemini" {
			envName = gem.GeminiAPIKeyEnv
		}
		apiKey = os.Getenv(envName)
		if apiKey == "" {
			return fmt.Errorf("%s environment variable is not set", envName)
		}
	} else {
		// Connect to skaband
		privKey, err := skabandclient.LoadOrCreatePrivateKey(skabandclient.DefaultKeyPath())
		if err != nil {
			return err
		}
		pubKey, antURL, apiKey, err = skabandclient.Login(os.Stdout, privKey, flags.skabandAddr, flags.sessionID, flags.modelName)
		if err != nil {
			return err
		}
	}

	return setupAndRunAgent(ctx, flags, antURL, apiKey, pubKey, false, logFile)
}

// setupAndRunAgent handles the common logic for setting up and running the agent
// in both container and unsafe modes.
func setupAndRunAgent(ctx context.Context, flags CLIFlags, modelURL, apiKey, pubKey string, inInsideSketch bool, logFile *os.File) error {
	// Configure HTTP client with optional recording
	var client *http.Client
	if flags.httprrFile != "" {
		var err error
		var rr *httprr.RecordReplay
		if flags.record {
			rr, err = httprr.OpenForRecording(flags.httprrFile, http.DefaultTransport)
		} else {
			rr, err = httprr.Open(flags.httprrFile, http.DefaultTransport)
		}
		if err != nil {
			return fmt.Errorf("httprr: %v", err)
		}
		// Scrub API keys from requests for security
		rr.ScrubReq(func(req *http.Request) error {
			req.Header.Del("x-api-key")
			req.Header.Del("anthropic-api-key")
			return nil
		})
		client = rr.Client()
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	llmService, err := selectLLMService(client, flags.modelName, modelURL, apiKey)
	if err != nil {
		return fmt.Errorf("failed to initialize LLM service: %w", err)
	}
	budget := conversation.Budget{
		MaxResponses: flags.maxIterations,
		MaxWallTime:  flags.maxWallTime,
		MaxDollars:   flags.maxDollars,
	}

	agentConfig := loop.AgentConfig{
		Context:           ctx,
		Service:           llmService,
		Budget:            budget,
		GitUsername:       flags.gitUsername,
		GitEmail:          flags.gitEmail,
		SessionID:         flags.sessionID,
		ClientGOOS:        runtime.GOOS,
		ClientGOARCH:      runtime.GOARCH,
		UseAnthropicEdit:  os.Getenv("SKETCH_ANTHROPIC_EDIT") == "1",
		OutsideHostname:   flags.outsideHostname,
		OutsideOS:         flags.outsideOS,
		OutsideWorkingDir: flags.outsideWorkingDir,
		InDocker:          true, // This is true when we're in container mode or simulating it in unsafe mode
		OneShot:           flags.oneShot,
	}
	agent := loop.NewAgent(agentConfig)

	// Create the server
	srv, err := server.New(agent, logFile)
	if err != nil {
		return err
	}

	// Initialize the agent (only needed when not inside sketch with outside hostname)
	if !inInsideSketch {
		ini := loop.AgentInit{
			WorkingDir: wd,
		}
		if err = agent.Init(ini); err != nil {
			return fmt.Errorf("failed to initialize agent: %v", err)
		}
	}

	// Start the agent
	go agent.Loop(ctx)

	// Start the local HTTP server
	ln, err := net.Listen("tcp", flags.addr)
	if err != nil {
		return fmt.Errorf("cannot create debug server listener: %v", err)
	}
	go (&http.Server{Handler: srv}).Serve(ln)

	// Determine the URL to display
	var ps1URL string
	if flags.skabandAddr != "" {
		ps1URL = fmt.Sprintf("%s/s/%s", flags.skabandAddr, flags.sessionID)
	} else if !agentConfig.InDocker {
		// Do not tell users about the port inside the container, let the
		// process running on the host report this.
		ps1URL = fmt.Sprintf("http://%s", ln.Addr())
	}

	if inInsideSketch {
		<-agent.Ready()
		if ps1URL == "" {
			ps1URL = agent.URL()
		}
	}

	// Use prompt if provided
	if flags.prompt != "" {
		agent.UserMessage(ctx, flags.prompt)
	}

	// Open the web UI URL in the system browser if requested
	if flags.openBrowser {
		browser.Open(ps1URL)
	}

	// Check if terminal UI should be enabled
	// Disable termui if the flag is explicitly set to false or if we detect no PTY is available
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		flags.termUI = false
	}

	// Create a variable for terminal UI
	var s *termui.TermUI

	// Create the termui instance only if needed
	if flags.termUI {
		s = termui.New(agent, ps1URL)
	}

	// Start skaband connection loop if needed
	if flags.skabandAddr != "" {
		connectFn := func(connected bool) {
			if flags.verbose {
				if connected {
					if s != nil {
						s.AppendSystemMessage("skaband connected")
					}
				} else {
					if s != nil {
						s.AppendSystemMessage("skaband disconnected")
					}
				}
			}
		}
		go skabandclient.DialAndServeLoop(ctx, flags.skabandAddr, flags.sessionID, pubKey, srv, connectFn)
	}

	// Handle one-shot mode or mode without terminal UI
	if flags.oneShot || s == nil {
		it := agent.NewIterator(ctx, 0)
		for {
			m := it.Next()
			if m == nil {
				return nil
			}
			if m.Content != "" {
				fmt.Printf("[%d] 💬 %s %s: %s\n", m.Idx, m.Timestamp.Format("15:04:05"), m.Type, m.Content)
			}
			if m.EndOfTurn && m.ParentConversationID == nil {
				fmt.Printf("Total cost: $%0.2f\n", agent.TotalUsage().TotalCostUSD)
				if flags.oneShot {
					return nil
				}
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}
		}
	}
	if s == nil {
		panic("Should have exited above.")
	}

	// Run the terminal UI
	defer func() {
		r := recover()
		if err := s.RestoreOldState(); err != nil {
			fmt.Fprintf(os.Stderr, "couldn't restore old terminal state: %s\n", err)
		}
		if r != nil {
			panic(r)
		}
	}()
	if err := s.Run(ctx); err != nil {
		return err
	}

	return nil
}

// setupLogging configures the logging system based on command-line flags.
// Returns the slog handler and optionally a log file (which should be closed by the caller).
func setupLogging(oneShot, verbose, unsafe bool) (slog.Handler, *os.File, error) {
	var slogHandler slog.Handler
	var logFile *os.File
	var err error

	if !oneShot && !verbose {
		// Log to a file
		logFile, err = os.CreateTemp("", "sketch-cli-log-*")
		if err != nil {
			return nil, nil, fmt.Errorf("cannot create log file: %v", err)
		}
		if unsafe {
			fmt.Printf("structured logs: %v\n", logFile.Name())
		}
	}

	// Always send slogs to the logFile.
	slogHandler = slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug})
	slogHandler = skribe.AttrsWrap(slogHandler)

	return slogHandler, logFile, nil
}

// newSessionID generates a new 10-byte random Session ID.
func newSessionID() string {
	u1, u2 := rand.Uint64(), rand.Uint64N(1<<16)
	s := crock32.Encode(u1) + crock32.Encode(uint64(u2))
	if len(s) < 16 {
		s += strings.Repeat("0", 16-len(s))
	}
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
}

func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func defaultGitUsername() string {
	out, err := exec.Command("git", "config", "user.name").CombinedOutput()
	if err != nil {
		return "Sketch🕴️" // TODO: what should this be?
	}
	return strings.TrimSpace(string(out))
}

func defaultGitEmail() string {
	out, err := exec.Command("git", "config", "user.email").CombinedOutput()
	if err != nil {
		return "skallywag@sketch.dev" // TODO: what should this be?
	}
	return strings.TrimSpace(string(out))
}

// selectLLMService creates an LLM service based on the specified model name.
// If modelName is empty or "claude", it uses the Anthropic service.
// If modelName is "gemini", it uses the Gemini service.
// Otherwise, it tries to use the OpenAI service with the specified model.
// Returns an error if the model name is not recognized or if required configuration is missing.
func selectLLMService(client *http.Client, modelName string, modelURL, apiKey string) (llm.Service, error) {
	if modelName == "" || modelName == "claude" {
		if apiKey == "" {
			return nil, fmt.Errorf("missing ANTHROPIC_API_KEY")
		}
		return &ant.Service{
			HTTPC:  client,
			URL:    modelURL,
			APIKey: apiKey,
		}, nil
	}

	if modelName == "gemini" {
		if apiKey == "" {
			return nil, fmt.Errorf("missing %s", gem.GeminiAPIKeyEnv)
		}
		return &gem.Service{
			HTTPC:  client,
			URL:    modelURL,
			Model:  gem.DefaultModel,
			APIKey: apiKey,
		}, nil
	}

	model := oai.ModelByUserName(modelName)
	if model == nil {
		return nil, fmt.Errorf("unknown model '%s', use -list-models to see available models", modelName)
	}

	// Verify we have an API key, if necessary.
	apiKey = os.Getenv(model.APIKeyEnv)
	if model.APIKeyEnv != "" && apiKey == "" {
		return nil, fmt.Errorf("missing API key for %s model, set %s environment variable", model.UserName, model.APIKeyEnv)
	}

	return &oai.Service{
		HTTPC:  client,
		Model:  *model,
		APIKey: apiKey,
	}, nil
}
