package main

import (
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	"golang.org/x/term"
	"sketch.dev/browser"
	"sketch.dev/claudetool"
	"sketch.dev/dockerimg"
	"sketch.dev/experiment"
	"sketch.dev/llm"
	"sketch.dev/llm/ant"
	"sketch.dev/llm/conversation"
	"sketch.dev/llm/gem"
	"sketch.dev/llm/oai"
	"sketch.dev/loop"
	"sketch.dev/loop/server"
	"sketch.dev/mcp"
	"sketch.dev/skabandclient"
	"sketch.dev/skribe"
	"sketch.dev/termui"
	"sketch.dev/termui/bubbletea"
	"sketch.dev/update"
	"sketch.dev/webui"
)

// Version information set by ldflags at build time
var (
	release = "dev" // release version
	builtBy = ""    // how this binary got built (makefile, goreleaser)
)

func main() {
	_ = godotenv.Load()
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

	// If not built with make, embedded assets will be missing.
	if builtBy == "" {
		// If your sketch binary isn't working and you are seeing this warning,
		// it's probably because your build method has stale or missing embedded assets.
		// See the makefile/GoReleaser configs for how to build.
		fmt.Fprintln(os.Stderr, "⚠️  not using a recommended build method")
	}

	// Set up signal handling if -ignoresig flag is set
	if flagArgs.ignoreSig {
		setupSignalIgnoring()
	}

	if flagArgs.version {
		fmt.Printf("sketch %s\n", release)
		fmt.Printf("\tbuild.system: %s\n", builtBy)
		bi, ok := debug.ReadBuildInfo()
		if ok {
			for _, s := range bi.Settings {
				if strings.HasPrefix(s.Key, "vcs.") {
					fmt.Printf("\t%s: %v\n", s.Key, s.Value)
				}
			}
		}
		return nil
	}

	if flagArgs.doUpdate {
		return doSelfUpdate()
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

	if flagArgs.dumpDist != "" {
		return dumpDistFilesystem(flagArgs.dumpDist)
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
	slogHandler, logFile, err := setupLogging(flagArgs.termUI, flagArgs.verbose, flagArgs.unsafe)
	if err != nil {
		return err
	}
	if logFile != nil {
		defer logFile.Close()
	}
	slog.SetDefault(slog.New(slogHandler))

	// Detect whether we're inside the sketch container
	inInsideSketch := flagArgs.outsideHostname != ""

	// Change to working directory if specified
	// Delay chdir when running in container mode, so that container setup can happen first,
	// which might be necessary for the requested working dir to exist.
	if flagArgs.workingDir != "" && !inInsideSketch {
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

// expandTilde expands ~ in the given path to the user's home directory
func expandTilde(path string) (string, error) {
	if path == "~" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path, err
		}
		return homeDir, nil
	}
	if strings.HasPrefix(path, "~/") {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return path, err
		}
		return strings.Replace(path, "~", homeDir, 1), nil
	}
	return path, nil
}

// CLIFlags holds all command-line arguments
// StringSliceFlag is a custom flag type that allows for repeated flag values.
// It collects all values into a slice.
type StringSliceFlag []string

// String returns the string representation of the flag value.
func (f *StringSliceFlag) String() string {
	return strings.Join(*f, ",")
}

// Set adds a value to the flag.
func (f *StringSliceFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// Get returns the flag values.
func (f *StringSliceFlag) Get() any {
	return []string(*f)
}

type CLIFlags struct {
	addr         string
	skabandAddr  string
	unsafe       bool
	openBrowser  bool
	httprrFile   string
	maxDollars   float64
	oneShot      bool
	prompt       string
	modelName    string
	llmAPIKey    string
	listModels   bool
	verbose      bool
	version      bool
	workingDir   string
	dumpDist     string
	sshPort      int
	forceRebuild bool
	baseImage    string
	linkToGitHub bool
	ignoreSig    bool
	doUpdate     bool
	useBubbleTea bool

	gitUsername         string
	gitEmail            string
	experimentFlag      experiment.Flag
	sessionID           string
	record              bool
	noCleanup           bool
	containerLogDest    string
	outsideHostname     string
	outsideOS           string
	outsideWorkingDir   string
	sketchBinaryLinux   string
	dockerArgs          string
	mounts              StringSliceFlag
	termUI              bool
	gitRemoteURL        string
	originalGitOrigin   string
	upstream            string
	commit              string
	outsideHTTP         string
	branchPrefix        string
	sshConnectionString string
	subtraceToken       string
	mcpServers          StringSliceFlag
	// Timeout configuration for bash tool
	bashFastTimeout       string
	bashSlowTimeout       string
	bashBackgroundTimeout string

	// Pentesting configuration
	pentestMode       bool
	pentestTarget     string
	pentestScope      string
	pentestResultsDir string
}

// parseCLIFlags parses all command-line flags and returns a CLIFlags struct
func parseCLIFlags() CLIFlags {
	var flags CLIFlags

	// Create separate flagsets for user-visible and internal flags
	userFlags := flag.NewFlagSet("sketch", flag.ExitOnError)
	internalFlags := flag.NewFlagSet("sketch-internal", flag.ContinueOnError)

	// User-visible flags
	userFlags.StringVar(&flags.addr, "addr", "0.0.0.0:0", "HTTP server address (0.0.0.0:0 allows network access)")
	userFlags.StringVar(&flags.skabandAddr, "skaband-addr", "", "URL of the skaband server; set to empty to disable sketch.dev integration (default: disabled)")
	userFlags.StringVar(&flags.skabandAddr, "ska-band-addr", "", "URL of the skaband server; set to empty to disable sketch.dev integration (alias for -skaband-addr)")
	userFlags.BoolVar(&flags.unsafe, "unsafe", false, "run without a docker container")
	userFlags.BoolVar(&flags.openBrowser, "open", true, "open sketch URL in system browser; on by default except if -one-shot is used or a ssh connection is detected")
	userFlags.Float64Var(&flags.maxDollars, "max-dollars", 10.0, "maximum dollars the agent should spend per turn, 0 to disable limit")
	userFlags.BoolVar(&flags.oneShot, "one-shot", false, "exit after the first turn without termui")
	userFlags.StringVar(&flags.prompt, "prompt", "", "prompt to send to sketch")
	userFlags.StringVar(&flags.prompt, "p", "", "prompt to send to sketch (alias for -prompt)")
	userFlags.StringVar(&flags.modelName, "model", "claude", "model to use (e.g. claude, gpt4.1)")
	userFlags.StringVar(&flags.llmAPIKey, "llm-api-key", "", "API key for the LLM provider; if not set, will be read from an env var")
	userFlags.BoolVar(&flags.listModels, "list-models", false, "list all available models and exit")
	userFlags.BoolVar(&flags.verbose, "verbose", false, "enable verbose output")
	userFlags.BoolVar(&flags.version, "version", false, "print the version and exit")
	userFlags.BoolVar(&flags.doUpdate, "update", false, "update to the latest version of sketch")
	userFlags.IntVar(&flags.sshPort, "ssh-port", 0, "the host port number that the container's ssh server will listen on, or a randomly chosen port if this value is 0")
	userFlags.BoolVar(&flags.forceRebuild, "force-rebuild-container", false, "rebuild Docker container")
	// Get the default image info for help text
	defaultImageName, _, defaultTag := dockerimg.DefaultImage()
	defaultHelpText := fmt.Sprintf("base Docker image to use (defaults to %s:%s); see https://sketch.dev/docs/docker for instructions", defaultImageName, defaultTag)
	userFlags.StringVar(&flags.baseImage, "base-image", "", defaultHelpText)

	userFlags.StringVar(&flags.dockerArgs, "docker-args", "", "additional arguments to pass to the docker create command (e.g., --memory=2g --cpus=2)")
	userFlags.Var(&flags.mounts, "mount", "volume to mount in the container in format /path/on/host:/path/in/container (can be repeated)")
	userFlags.BoolVar(&flags.termUI, "termui", true, "enable terminal UI")
	userFlags.BoolVar(&flags.useBubbleTea, "bubbletea", false, "use the new Bubble Tea terminal UI")
	userFlags.StringVar(&flags.branchPrefix, "branch-prefix", "sketch/", "prefix for git branches created by sketch")
	userFlags.BoolVar(&flags.ignoreSig, "ignoresig", false, "ignore typical termination signals (SIGINT, SIGTERM)")
	userFlags.Var(&flags.mcpServers, "mcp", "MCP server configuration as JSON (can be repeated). Schema: {\"name\": \"server-name\", \"type\": \"stdio|http|sse\", \"url\": \"...\", \"command\": \"...\", \"args\": [...], \"env\": {...}, \"headers\": {...}}")
	userFlags.StringVar(&flags.bashFastTimeout, "bash-fast-timeout", "30s", "timeout for fast bash commands")
	userFlags.StringVar(&flags.bashSlowTimeout, "bash-slow-timeout", "10m", "timeout for slow bash commands (downloads, builds, tests)")
	userFlags.StringVar(&flags.bashBackgroundTimeout, "bash-background-timeout", "24h", "timeout for background bash commands")

	// Pentesting flags
	userFlags.BoolVar(&flags.pentestMode, "pentest-mode", false, "enable pentesting features and use Kali Linux tools")
	userFlags.StringVar(&flags.pentestTarget, "pentest-target", "", "specify the target for pentesting operations")
	userFlags.StringVar(&flags.pentestScope, "pentest-scope", "", "define the scope of the pentesting engagement")
	userFlags.StringVar(&flags.pentestResultsDir, "pentest-results-dir", "", "specify where to store pentesting results")

	// Internal flags (for sketch developers or internal use)
	// Args to sketch innie:
	internalFlags.StringVar(&flags.gitUsername, "git-username", "", "(internal) username for git commits")
	internalFlags.StringVar(&flags.gitEmail, "git-email", "", "(internal) email for git commits")
	internalFlags.StringVar(&flags.sessionID, "session-id", skabandclient.NewSessionID(), "(internal) unique session-id for a sketch process")
	internalFlags.BoolVar(&flags.record, "httprecord", true, "(debugging) Record trace (if httprr is set)")
	internalFlags.BoolVar(&flags.noCleanup, "nocleanup", false, "(debugging) do not clean up docker containers on exit")
	internalFlags.StringVar(&flags.containerLogDest, "save-container-logs", "", "(debugging) host path to save container logs to on exit")
	internalFlags.StringVar(&flags.outsideHostname, "outside-hostname", "", "(internal) hostname on the outside system")
	internalFlags.StringVar(&flags.outsideOS, "outside-os", "", "(internal) OS on the outside system")
	internalFlags.StringVar(&flags.outsideWorkingDir, "outside-working-dir", "", "(internal) working dir on the outside system")
	internalFlags.StringVar(&flags.sketchBinaryLinux, "sketch-binary-linux", "", "(development) path to a pre-built sketch binary for linux")
	internalFlags.StringVar(&flags.gitRemoteURL, "git-remote-url", "", "(internal) git remote for outside sketch")
	internalFlags.StringVar(&flags.originalGitOrigin, "original-git-origin", "", "(internal) original git origin URL from host repository")
	internalFlags.StringVar(&flags.upstream, "upstream", "", "(internal) upstream branch for git work")
	internalFlags.StringVar(&flags.commit, "commit", "", "(internal) the git commit reference to check out from git remote url")
	internalFlags.StringVar(&flags.outsideHTTP, "outside-http", "", "(internal) host for outside sketch")
	internalFlags.BoolVar(&flags.linkToGitHub, "link-to-github", false, "(internal) enable GitHub branch linking in UI")
	internalFlags.StringVar(&flags.sshConnectionString, "ssh-connection-string", "", "(internal) SSH connection string for connecting to the container")

	// Developer flags
	internalFlags.StringVar(&flags.httprrFile, "httprr", "", "if set, record HTTP interactions to file")
	internalFlags.Var(&flags.experimentFlag, "x", "enable experimental features (comma-separated list or repeat flag; use 'list' to show all)")
	// This is really only useful for someone running with "go run"
	userFlags.StringVar(&flags.workingDir, "C", "", "when set, change to this directory before running")

	// Internal flags for development/debugging
	internalFlags.StringVar(&flags.dumpDist, "dump-dist", "", "(internal) dump embedded /dist/ filesystem to specified directory and exit")
	internalFlags.StringVar(&flags.subtraceToken, "subtrace-token", "", "(development) run sketch under subtrace.dev with the provided token")

	// Custom usage function that shows only user-visible flags by default
	userFlags.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		userFlags.PrintDefaults()
		fmt.Fprintf(os.Stderr, "\nFor additional internal/debugging flags, use -help-internal\n")
	}

	// Check if user requested internal help
	if len(os.Args) > 1 && os.Args[1] == "-help-internal" {
		fmt.Fprintf(os.Stderr, "Internal/debugging flags for %s:\n", os.Args[0])
		internalFlags.PrintDefaults()
		os.Exit(0)
	}

	// Create a combined flagset for actual parsing by merging the two flagsets
	allFlags := flag.NewFlagSet("sketch-all", flag.ExitOnError)
	allFlags.Usage = userFlags.Usage

	// Copy all flags from userFlags to allFlags
	userFlags.VisitAll(func(f *flag.Flag) {
		allFlags.Var(f.Value, f.Name, f.Usage)
	})

	// Copy all flags from internalFlags to allFlags
	internalFlags.VisitAll(func(f *flag.Flag) {
		allFlags.Var(f.Value, f.Name, f.Usage)
	})

	// Parse all arguments with the combined flagset
	allFlags.Parse(os.Args[1:])

	// -open's default value is not a simple true/false; it depends on other flags and conditions.
	// Distinguish between -open default value vs explicitly set.
	openExplicit := false
	allFlags.Visit(func(f *flag.Flag) {
		if f.Name == "open" {
			openExplicit = true
		}
	})
	if !openExplicit {
		// Not explicitly set.
		// Calculate the right default value: true except with one-shot mode or if we're running in a ssh session.
		flags.openBrowser = !flags.oneShot && os.Getenv("SSH_CONNECTION") == ""
	}

	// expand ~ in mounts
	for i, mount := range flags.mounts {
		host, container, ok := strings.Cut(mount, ":")
		if !ok {
			continue
		}
		expanded, err := expandTilde(host)
		if err != nil {
			slog.Warn("failed to expand tilde in mount path", "path", host, "error", err)
			continue
		}
		flags.mounts[i] = expanded + ":" + container
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
	var pubKey, modelURL, apiKey string
	if flags.skabandAddr != "" {
		privKey, err := skabandclient.LoadOrCreatePrivateKey(skabandclient.DefaultKeyPath())
		if err != nil {
			return err
		}
		pubKey, modelURL, apiKey, err = skabandclient.Login(os.Stdout, privKey, flags.skabandAddr, flags.sessionID, flags.modelName)
		if err != nil {
			return err
		}
		flags.mcpServers = append(flags.mcpServers, skabandMcpConfiguration(flags))
	} else {
		// When not using skaband, get API key from environment or flag
		envName := "ANTHROPIC_API_KEY"
		if flags.modelName == "gemini" {
			envName = gem.GeminiAPIKeyEnv
		}
		apiKey = cmp.Or(os.Getenv(envName), flags.llmAPIKey)
		if apiKey == "" {
			return fmt.Errorf("%s environment variable is not set, -llm-api-key flag not provided", envName)
		}
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
		BaseImage:         flags.baseImage,
		OutsideHostname:   getHostname(),
		OutsideOS:         runtime.GOOS,
		OutsideWorkingDir: cwd,
		OneShot:           flags.oneShot,
		Prompt:            flags.prompt,

		Verbose:        flags.verbose,
		DockerArgs:     flags.dockerArgs,
		Mounts:         flags.mounts,
		ExperimentFlag: flags.experimentFlag.String(),
		TermUI:         flags.termUI,
		MaxDollars:     flags.maxDollars,
		BranchPrefix:   flags.branchPrefix,
		LinkToGitHub:   flags.linkToGitHub,
		SubtraceToken:  flags.subtraceToken,
		MCPServers:     flags.mcpServers,

		// Pentesting configuration
		PentestMode:       flags.pentestMode,
		PentestTarget:     flags.pentestTarget,
		PentestScope:      flags.pentestScope,
		PentestResultsDir: flags.pentestResultsDir,
	}

	if err := dockerimg.LaunchContainer(ctx, config); err != nil {
		if flags.verbose {
			fmt.Fprintf(os.Stderr, "dockerimg launch container failed: %v\n", err)
		}
		return err
	}

	return nil
}

func skabandMcpConfiguration(flags CLIFlags) string {
	skabandaddr, err := skabandclient.LocalhostToDockerInternal(flags.skabandAddr)
	if err != nil {
		skabandaddr = flags.skabandAddr
	}
	config := mcp.ServerConfig{
		Name: "sketchdev",
		Type: "http",
		URL:  skabandaddr + "/api/mcp",
		Headers: map[string]string{
			"Session-Id":     flags.sessionID,
			"Public-Key":     "env:SKETCH_PUB_KEY",
			"Session-Secret": "env:SKETCH_MODEL_API_KEY",
		},
	}
	out, err := json.Marshal(&config)
	if err != nil {
		panic("programming error" + err.Error())
	}
	return string(out)
}

// runInContainerMode handles execution inside the Docker container.
// The inInsideSketch parameter indicates whether we're inside the sketch container
// with access to outside environment variables.
func runInContainerMode(ctx context.Context, flags CLIFlags, logFile *os.File) error {
	// Get credentials from environment
	apiKey := cmp.Or(os.Getenv("SKETCH_MODEL_API_KEY"), flags.llmAPIKey)
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
	var apiKey, modelURL, pubKey string
	if flags.skabandAddr == "" {
		// Handle different model types when not using skaband
		if flags.modelName == "gemini" {
			// Handle Gemini model specifically
			apiKey = cmp.Or(os.Getenv(gem.GeminiAPIKeyEnv), flags.llmAPIKey)
			if apiKey == "" {
				return fmt.Errorf("%s environment variable is not set, and -llm-api-key flag not provided", gem.GeminiAPIKeyEnv)
			}
		} else {
			// Find the model in the registry to determine which API key to use.
			model := oai.ModelByUserName(flags.modelName)

			if model != nil {
				// Model was found in the oai registry.
				apiKey = cmp.Or(os.Getenv(model.APIKeyEnv), flags.llmAPIKey)
				if apiKey == "" {
					return fmt.Errorf("%s environment variable is not set, and -llm-api-key flag not provided", model.APIKeyEnv)
				}
			} else {
				// If the model is not in the oai registry, it might be a bifrost model (like Azure).
				// The bifrost adapter handles API keys automatically, so we just need to avoid an error here.
				// We set a dummy value to pass the checks.
				apiKey = "not-empty"
			}
		}
	} else {
		// Connect to skaband
		privKey, err := skabandclient.LoadOrCreatePrivateKey(skabandclient.DefaultKeyPath())
		if err != nil {
			return err
		}
		pubKey, modelURL, apiKey, err = skabandclient.Login(os.Stdout, privKey, flags.skabandAddr, flags.sessionID, flags.modelName)
		if err != nil {
			return err
		}

		// Add MCP Server for skaband
		flags.mcpServers = append(flags.mcpServers, skabandMcpConfiguration(flags))
	}

	return setupAndRunAgent(ctx, flags, modelURL, apiKey, pubKey, false, logFile)
}

func setupAndRunAgent(ctx context.Context, flags CLIFlags, modelURL, apiKey, pubKey string, inInsideSketch bool, logFile *os.File) error {
	var client *http.Client

	// Set the public key environment variable if provided
	// This is needed for MCP server authentication placeholder replacement
	if pubKey != "" {
		os.Setenv("SKETCH_PUB_KEY", pubKey)
	}

	wd, err := os.Getwd()
	if err != nil {
		return err
	}

	// In container mode, do the (delayed) chdir.
	if flags.workingDir != "" && inInsideSketch {
		if filepath.IsAbs(flags.workingDir) {
			wd = flags.workingDir
		} else {
			wd = filepath.Join(wd, flags.workingDir)
		}
	}

	llmService, err := selectLLMService(client, flags.modelName, modelURL, apiKey)
	if err != nil {
		return fmt.Errorf("failed to initialize LLM service: %w", err)
	}
	budget := conversation.Budget{
		MaxDollars: flags.maxDollars,
	}

	// Get the original git origin URL
	originalGitOrigin := flags.originalGitOrigin
	if originalGitOrigin == "" && flags.outsideHostname == "" {
		// Not in container mode, get the git origin directly
		originalGitOrigin = getGitOrigin(ctx, wd)
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
		OutsideHostname:   flags.outsideHostname,
		OutsideOS:         flags.outsideOS,
		OutsideWorkingDir: flags.outsideWorkingDir,
		WorkingDir:        wd,
		// Ultimately this is a subtle flag because it's trying to distinguish
		// between unsafe-on-host and inside sketch, and should probably be renamed/simplified.
		InDocker:            flags.outsideHostname != "",
		OneShot:             flags.oneShot,
		GitRemoteAddr:       flags.gitRemoteURL,
		OriginalGitOrigin:   originalGitOrigin,
		Upstream:            flags.upstream,
		OutsideHTTP:         flags.outsideHTTP,
		Commit:              flags.commit,
		BranchPrefix:        flags.branchPrefix,
		LinkToGitHub:        flags.linkToGitHub,
		SSHConnectionString: flags.sshConnectionString,
		MCPServers:          flags.mcpServers,
	}

	// Parse timeout configuration
	var bashTimeouts claudetool.Timeouts
	if dur, err := time.ParseDuration(flags.bashFastTimeout); err == nil {
		bashTimeouts.Fast = dur
	} else {
		bashTimeouts.Fast = claudetool.DefaultFastTimeout
	}
	if dur, err := time.ParseDuration(flags.bashSlowTimeout); err == nil {
		bashTimeouts.Slow = dur
	} else {
		bashTimeouts.Slow = claudetool.DefaultSlowTimeout
	}
	if dur, err := time.ParseDuration(flags.bashBackgroundTimeout); err == nil {
		bashTimeouts.Background = dur
	} else {
		bashTimeouts.Background = claudetool.DefaultBackgroundTimeout
	}
	agentConfig.BashTimeouts = &bashTimeouts

	// Create SkabandClient if skaband address is provided
	if flags.skabandAddr != "" && pubKey != "" {
		agentConfig.SkabandClient = skabandclient.NewSkabandClient(flags.skabandAddr, pubKey)
	}
	agent := loop.NewAgent(agentConfig)

	// Create the server
	srv, err := server.New(agent, logFile)
	if err != nil {
		return err
	}

	// Initialize the agent (only needed when not inside sketch with outside hostname)
	// In the innie case, outtie sends a POST /init
	if !inInsideSketch {
		if err = agent.Init(loop.AgentInit{}); err != nil {
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
	var bt *bubbletea.BubbleTeaUI

	// Create the terminal UI instance based on flags
	if flags.termUI {
		if flags.useBubbleTea {
			// Use the new Bubble Tea terminal UI
			bt = bubbletea.New(agent, ps1URL)
		} else {
			// Use the classic terminal UI
			s = termui.New(agent, ps1URL)
		}
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
		if agentConfig.SkabandClient != nil {
			sessionSecret := apiKey
			go agentConfig.SkabandClient.DialAndServeLoop(ctx, flags.sessionID, sessionSecret, srv, connectFn)
		}
	}

	// Handle one-shot mode or mode without terminal UI
	if flags.oneShot || (s == nil && bt == nil) {
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

	// Run the appropriate terminal UI
	if flags.useBubbleTea && bt != nil {
		// Run the Bubble Tea terminal UI
		defer func() {
			r := recover()
			if err := bt.RestoreOldState(); err != nil {
				fmt.Fprintf(os.Stderr, "couldn't restore old terminal state: %s\n", err)
			}
			if r != nil {
				panic(r)
			}
		}()
		if err := bt.Run(ctx); err != nil {
			return err
		}
	} else if s != nil {
		// Run the classic terminal UI
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
	} else {
		panic("Should have exited above.")
	}

	return nil
}

// setupLogging configures the logging system based on command-line flags.
// Returns the slog handler and optionally a log file (which should be closed by the caller).
func setupLogging(termui, verbose, unsafe bool) (slog.Handler, *os.File, error) {
	var slogHandler slog.Handler
	var logFile *os.File
	var err error

	if verbose && !termui {
		// Log to stderr
		slogHandler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
		return slogHandler, nil, nil
	}

	// Log to a file
	logFile, err = os.CreateTemp("", "sketch-cli-log-*")
	if err != nil {
		return nil, nil, fmt.Errorf("cannot create log file: %v", err)
	}
	if unsafe {
		fmt.Printf("structured logs: %v\n", logFile.Name())
	}

	slogHandler = slog.NewJSONHandler(logFile, &slog.HandlerOptions{Level: slog.LevelDebug})
	slogHandler = skribe.AttrsWrap(slogHandler)

	return slogHandler, logFile, nil
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

// dumpDistFilesystem dumps the embedded /dist/ filesystem to the specified directory
func dumpDistFilesystem(outputDir string) error {
	// Build the embedded filesystem
	distFS, err := webui.Build()
	if err != nil {
		return fmt.Errorf("failed to build embedded filesystem: %w", err)
	}

	// Create the output directory
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("failed to create output directory %q: %w", outputDir, err)
	}

	// Walk through the filesystem and copy all files
	err = fs.WalkDir(distFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		outputPath := filepath.Join(outputDir, path)

		if d.IsDir() {
			// Create directory
			if err := os.MkdirAll(outputPath, 0o755); err != nil {
				return fmt.Errorf("failed to create directory %q: %w", outputPath, err)
			}
			return nil
		}

		// Copy file
		src, err := distFS.Open(path)
		if err != nil {
			return fmt.Errorf("failed to open source file %q: %w", path, err)
		}
		defer src.Close()

		dst, err := os.Create(outputPath)
		if err != nil {
			return fmt.Errorf("failed to create destination file %q: %w", outputPath, err)
		}
		defer dst.Close()

		if _, err := io.Copy(dst, src); err != nil {
			return fmt.Errorf("failed to copy file %q: %w", path, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to dump filesystem: %w", err)
	}

	fmt.Printf("Successfully dumped embedded /dist/ filesystem to %q\n", outputDir)
	return nil
}

// setupSignalIgnoring sets up signal handling to ignore SIGINT and SIGTERM
// when the -ignoresig flag is used. This prevents the typical Ctrl+C or
// termination signals from killing the process.
func setupSignalIgnoring() {
	// Create a channel to receive signals
	sigChan := make(chan os.Signal, 1)

	// Register the channel to receive specific signals
	// We ignore SIGINT (Ctrl+C) and SIGTERM (termination request)
	// but allow SIGQUIT to still work for debugging/stack dumps
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start a goroutine to handle the signals
	go func() {
		for sig := range sigChan {
			// Simply ignore the signal by doing nothing
			// This prevents the default behavior of terminating the process
			_ = sig // Suppress unused variable warning
		}
	}()
}

// getGitOrigin returns the URL of the git remote 'origin' if it exists
func getGitOrigin(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "remote.origin.url")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func doSelfUpdate() error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %w", err)
	}
	return update.Do(context.Background(), release, executable)
}
