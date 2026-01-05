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
	"sketch.dev/update"
	"sketch.dev/webui"
)

// Version information set by ldflags at build time
var (
	release = "dev" // release version
	builtBy = ""    // how this binary got built (makefile, goreleaser)
)

// VersionResponse represents the response from sketch.dev/version
type VersionResponse struct {
	Stdout string `json:"stdout"`
}

// doVersionCheck asks the server for version information. Best effort.
func doVersionCheck(ch chan *VersionResponse, pubKey string) {
	req, err := http.NewRequest("GET", "https://sketch.dev/version", nil)
	if err != nil {
		return
	}

	req.Header.Set("X-Client-ID", pubKey)
	req.Header.Set("X-Client-Release", release)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var versionResp VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil {
		return
	}
	ch <- &versionResp
}

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

	// If not built with make, embedded assets will be missing.
	if builtBy == "" {
		// If your sketch binary isn't working and you are seeing this warning,
		// it's probably because your build method has stale or missing embedded assets.
		// See the makefile/GoReleaser configs for how to build.
		fmt.Fprintln(os.Stderr, "⚠️  not using a recommended build method")
	}

	if flagArgs.skabandAddr != "" {
		fmt.Fprintln(os.Stderr, "⚠️  skaband service is being turned down; -skaband-addr flag is deprecated")
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
		fmt.Println("- claude (default, Claude 4.5 Sonnet)")
		fmt.Println("- opus (Claude 4.5 Opus)")
		fmt.Println("- sonnet (Claude 4.5 Sonnet)")
		fmt.Println("- gemini (Google Gemini 3 Pro)")
		fmt.Println("- qwen (Qwen3-Coder)")
		fmt.Println("- glm (Zai GLM4.5)")
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

	// Not all models have skaband support.
	hasSkabandSupport := ant.IsClaudeModel(flagArgs.modelName)
	switch flagArgs.modelName {
	case "gemini", "qwen", "glm", "gpt5", "gpt5mini":
		hasSkabandSupport = true
	}
	if !hasSkabandSupport && flagArgs.skabandAddr != "" {
		return fmt.Errorf("only claude, gemini, qwen, and glm are supported by skaband, use -skaband-addr='' for other models")
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

	// Start zombie reaper if we're running as PID 1
	go zombieReaper(ctx)

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

	// Add skaband MCP server configuration if skaband address is provided and
	// it's not otherwise specified.
	if flagArgs.skabandAddr != "" {
		seenSketchDev := false
		for _, serverConfig := range flagArgs.mcpServers {
			c, err := parseSingleMcpConfiguration(serverConfig)
			// We can ignore errors; they'll get checked when the MCP configuration is used by the agent.
			if err == nil && c.Name == "sketchdev" {
				seenSketchDev = true
				break
			}
		}
		if !seenSketchDev {
			flagArgs.mcpServers = append(flagArgs.mcpServers, skabandMcpConfiguration(flagArgs))
		}
	}

	// Dispatch to the appropriate execution path
	if inInsideSketch {
		// We're running inside the Docker container that was launched by outtie
		slog.Debug("running in innie/container mode")
		return runInInnieMode(ctx, flagArgs, logFile)
	} else if flagArgs.unsafe {
		// We're running directly on the host in unsafe mode
		slog.Debug("running in unsafe mode")
		return runInUnsafeMode(ctx, flagArgs, logFile)
	} else {
		// We're running on the host and need to launch a container
		slog.Debug("running in host mode (outtie)")
		return runAsOuttie(ctx, flagArgs)
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
	addr          string
	skabandAddr   string
	unsafe        bool
	openBrowser   bool
	httprrFile    string
	maxDollars    float64
	oneShot       bool
	prompt        string
	modelName     string
	llmAPIKey     string
	listModels    bool
	verbose       bool
	version       bool
	workingDir    string
	dumpDist      string
	sshPort       int
	forceRebuild  bool
	baseImage     string
	linkToGitHub  bool
	ignoreSig     bool
	doUpdate      bool
	checkVersion  bool
	fetchOnLaunch bool

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
	passthroughUpstream   bool
	// LLM debugging
	dumpLLM bool
}

// parseCLIFlags parses all command-line flags and returns a CLIFlags struct
func parseCLIFlags() CLIFlags {
	var flags CLIFlags

	// Create separate flagsets for user-visible and internal flags
	userFlags := flag.NewFlagSet("sketch", flag.ExitOnError)
	internalFlags := flag.NewFlagSet("sketch-internal", flag.ContinueOnError)

	// User-visible flags
	userFlags.StringVar(&flags.addr, "addr", "localhost:0", "local HTTP server")
	userFlags.StringVar(&flags.skabandAddr, "skaband-addr", "", "URL of the skaband server (deprecated)")
	userFlags.StringVar(&flags.skabandAddr, "ska-band-addr", "", "URL of the skaband server (deprecated, alias for -skaband-addr)")
	userFlags.BoolVar(&flags.unsafe, "unsafe", false, "run without a docker container")
	userFlags.BoolVar(&flags.openBrowser, "open", true, "open sketch URL in system browser; on by default except if -one-shot is used or a ssh connection is detected")
	userFlags.Float64Var(&flags.maxDollars, "max-dollars", 10.0, "maximum dollars the agent should spend per turn, 0 to disable limit")
	userFlags.BoolVar(&flags.oneShot, "one-shot", false, "exit after the first turn without termui")
	userFlags.StringVar(&flags.prompt, "prompt", "", "prompt to send to sketch")
	userFlags.StringVar(&flags.prompt, "p", "", "prompt to send to sketch (alias for -prompt)")
	userFlags.StringVar(&flags.modelName, "model", "claude", "model to use (e.g. claude, opus, gemini, gpt4.1)")
	userFlags.StringVar(&flags.llmAPIKey, "llm-api-key", "", "API key for the LLM provider; if not set, will be read from an env var")
	userFlags.BoolVar(&flags.listModels, "list-models", false, "list all available models and exit")
	userFlags.BoolVar(&flags.verbose, "verbose", false, "enable verbose output")
	userFlags.BoolVar(&flags.version, "version", false, "print the version and exit")
	userFlags.BoolVar(&flags.doUpdate, "update", false, "update to the latest version of sketch")
	userFlags.BoolVar(&flags.checkVersion, "version-check", true, "do version upgrade check (please leave this on)")
	userFlags.BoolVar(&flags.fetchOnLaunch, "fetch-on-launch", true, "do a git fetch when sketch starts")
	userFlags.IntVar(&flags.sshPort, "ssh-port", 0, "the host port number that the container's ssh server will listen on, or a randomly chosen port if this value is 0")
	userFlags.BoolVar(&flags.forceRebuild, "force-rebuild-container", false, "rebuild Docker container")
	userFlags.BoolVar(&flags.forceRebuild, "rebuild", false, "rebuild Docker container (alias for -force-rebuild-container)")
	// Get the default image info for help text
	defaultImageName, _, defaultTag := dockerimg.DefaultImage()
	defaultHelpText := fmt.Sprintf("base Docker image to use (defaults to %s:%s); see https://sketch.dev/docs/docker for instructions", defaultImageName, defaultTag)
	userFlags.StringVar(&flags.baseImage, "base-image", "", defaultHelpText)

	userFlags.StringVar(&flags.dockerArgs, "docker-args", "", "additional arguments to pass to the docker create command (e.g., --memory=2g --cpus=2)")
	userFlags.Var(&flags.mounts, "mount", "volume to mount in the container in format /path/on/host:/path/in/container (can be repeated)")
	userFlags.BoolVar(&flags.termUI, "termui", true, "enable terminal UI")
	userFlags.StringVar(&flags.branchPrefix, "branch-prefix", "sketch/", "prefix for git branches created by sketch")
	userFlags.BoolVar(&flags.ignoreSig, "ignoresig", false, "ignore typical termination signals (SIGINT, SIGTERM)")
	userFlags.Var(&flags.mcpServers, "mcp", "MCP server configuration as JSON (can be repeated). Schema: {\"name\": \"server-name\", \"type\": \"stdio|http|sse\", \"url\": \"...\", \"command\": \"...\", \"args\": [...], \"env\": {...}, \"headers\": {...}}")
	userFlags.StringVar(&flags.bashFastTimeout, "bash-fast-timeout", "30s", "timeout for fast bash commands")
	userFlags.StringVar(&flags.bashSlowTimeout, "bash-slow-timeout", "10m", "timeout for slow bash commands (downloads, builds, tests)")
	userFlags.StringVar(&flags.bashBackgroundTimeout, "bash-background-timeout", "24h", "timeout for background bash commands")

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
	internalFlags.BoolVar(&flags.passthroughUpstream, "passthrough-upstream", false, "(internal) configure upstream remote for passthrough to innie")

	// Developer flags
	internalFlags.StringVar(&flags.httprrFile, "httprr", "", "if set, record HTTP interactions to file")
	internalFlags.Var(&flags.experimentFlag, "x", "enable experimental features (comma-separated list or repeat flag; use 'list' to show all)")
	// This is really only useful for someone running with "go run"
	userFlags.StringVar(&flags.workingDir, "C", "", "when set, change to this directory before running")

	// Internal flags for development/debugging
	internalFlags.StringVar(&flags.dumpDist, "dump-dist", "", "(internal) dump embedded /dist/ filesystem to specified directory and exit")
	internalFlags.StringVar(&flags.subtraceToken, "subtrace-token", "", "(development) run sketch under subtrace.dev with the provided token")
	internalFlags.BoolVar(&flags.dumpLLM, "dump-llm", false, "(debugging) dump raw communications with LLM services to files in ~/.cache/sketch/")

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

	flags.skabandAddr = strings.TrimSuffix(flags.skabandAddr, "/")

	return flags
}

func parseSingleMcpConfiguration(mcpConfig string) (mcp.ServerConfig, error) {
	var serverConfig mcp.ServerConfig
	if err := json.Unmarshal([]byte(mcpConfig), &serverConfig); err != nil {
		return mcp.ServerConfig{}, fmt.Errorf("failed to parse MCP config: %w", err)
	}
	return serverConfig, nil
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

// runAsOuttie handles execution on the host machine, which typically involves
// checking host requirements and launching a Docker container.
func runAsOuttie(ctx context.Context, flags CLIFlags) error {
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

	spec, pubKey, err := resolveModel(flags)
	if err != nil {
		return err
	}

	// Get current working directory
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("sketch: cannot determine current working directory: %v", err)
	}

	// Resolve any symlinks in the working directory path
	cwd, err = filepath.EvalSymlinks(cwd)
	if err != nil {
		return fmt.Errorf("sketch: cannot resolve working directory symlinks: %v", err)
	}

	// Configure and launch the container
	config := dockerimg.ContainerConfig{
		SessionID:         flags.sessionID,
		LocalAddr:         flags.addr,
		SkabandAddr:       flags.skabandAddr,
		Model:             flags.modelName,
		ModelURL:          spec.modelURL,
		OAIModelName:      spec.oaiModelName,
		ModelAPIKey:       spec.apiKey,
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

		Verbose:             flags.verbose,
		DockerArgs:          flags.dockerArgs,
		Mounts:              flags.mounts,
		ExperimentFlag:      flags.experimentFlag.String(),
		TermUI:              flags.termUI,
		MaxDollars:          flags.maxDollars,
		BranchPrefix:        flags.branchPrefix,
		LinkToGitHub:        flags.linkToGitHub,
		SubtraceToken:       flags.subtraceToken,
		MCPServers:          flags.mcpServers,
		PassthroughUpstream: flags.passthroughUpstream,
		DumpLLM:             flags.dumpLLM,
		FetchOnLaunch:       flags.fetchOnLaunch,
	}

	if err := dockerimg.LaunchContainer(ctx, config); err != nil {
		if flags.verbose {
			fmt.Fprintf(os.Stderr, "dockerimg launch container failed: %v\n", err)
		}
		return err
	}

	return nil
}

// runInInnieMode handles execution inside the Docker container.
// The inInsideSketch parameter indicates whether we're inside the sketch container
// with access to outside environment variables.
func runInInnieMode(ctx context.Context, flags CLIFlags, logFile *os.File) error {
	// Get credentials from environment
	apiKey := cmp.Or(os.Getenv("SKETCH_MODEL_API_KEY"), flags.llmAPIKey)
	pubKey := os.Getenv("SKETCH_PUB_KEY")
	modelURL, err := skabandclient.LocalhostToDockerInternal(os.Getenv("SKETCH_MODEL_URL"))
	if err != nil && os.Getenv("SKETCH_MODEL_URL") != "" {
		return err
	}
	spec := modelSpec{
		modelURL:     modelURL,
		oaiModelName: os.Getenv("SKETCH_OAI_MODEL_NAME"),
		apiKey:       apiKey,
	}
	return setupAndRunAgent(ctx, flags, spec, pubKey, true, logFile)
}

// runInUnsafeMode handles execution on the host machine without Docker.
// This mode is used when the -unsafe flag is provided.
func runInUnsafeMode(ctx context.Context, flags CLIFlags, logFile *os.File) error {
	spec, pubKey, err := resolveModel(flags)
	if err != nil {
		return err
	}
	return setupAndRunAgent(ctx, flags, spec, pubKey, false, logFile)
}

type modelSpec struct {
	modelURL     string
	oaiModelName string // the OpenAI model name, if applicable; this varies even for the same model by provider
	apiKey       string
}

// resolveModel logs in to skaband (as appropriate) and resolves the flags to a model URL and API key.
func resolveModel(flags CLIFlags) (spec modelSpec, pubKey string, err error) {
	privKey, err := skabandclient.LoadOrCreatePrivateKey(skabandclient.DefaultKeyPath(flags.skabandAddr))
	if err != nil {
		return modelSpec{}, "", err
	}
	pubKey, modelURL, oaiModelName, apiKey, err := skabandclient.Login(os.Stdout, privKey, flags.skabandAddr, flags.sessionID, flags.modelName)
	if err != nil {
		return modelSpec{}, "", err
	}

	if flags.skabandAddr == "" {
		// When not using skaband, get API key from environment or flag
		envName := envNameForModel(flags.modelName)
		if envName == "" {
			return modelSpec{}, "", fmt.Errorf("unknown model '%s', use -list-models to see available models", flags.modelName)
		}
		apiKey = cmp.Or(os.Getenv(envName), flags.llmAPIKey)
		if apiKey == "" && envName != "NONE" {
			return modelSpec{}, "", fmt.Errorf("%s environment variable is not set, -llm-api-key flag not provided", envName)
		}
	}

	return modelSpec{modelURL: modelURL, oaiModelName: oaiModelName, apiKey: apiKey}, pubKey, nil
}

// setupAndRunAgent handles the common logic for setting up and running the agent
// in both container and unsafe modes.
func setupAndRunAgent(ctx context.Context, flags CLIFlags, spec modelSpec, pubKey string, inInsideSketch bool, logFile *os.File) error {
	// Kick off a version/upgrade check early.
	// If the results come back quickly enough,
	// we can show them as part of the startup UI.
	var versionC chan *VersionResponse
	if flags.checkVersion {
		versionC = make(chan *VersionResponse, 1)
		go doVersionCheck(versionC, pubKey)
	}

	// Set the public key environment variable if provided
	// This is needed for MCP server authentication placeholder replacement
	if pubKey != "" {
		os.Setenv("SKETCH_PUB_KEY", pubKey)
		os.Setenv("SKETCH_MODEL_API_KEY", spec.apiKey)
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

	llmService, err := selectLLMService(nil, flags, spec)
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
		Model:             flags.modelName,
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
		PassthroughUpstream: flags.passthroughUpstream,
		FetchOnLaunch:       flags.fetchOnLaunch,
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

	// Last chance to print stuff to stdout before starting termui.
	// Check for the version upgrade response.
	select {
	case resp := <-versionC:
		if resp != nil && resp.Stdout != "" {
			// Mild server paranoia: Limit message to 120 characters.
			message := resp.Stdout[:min(len(resp.Stdout), 120)]
			fmt.Printf("🦋 %s\n", message)
		}
	default:
		// Version check hasn't responded yet, or never ran, or hit an error. Continue without it.
	}

	var s *termui.TermUI
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
		if agentConfig.SkabandClient != nil {
			sessionSecret := spec.apiKey
			go agentConfig.SkabandClient.DialAndServeLoop(ctx, flags.sessionID, sessionSecret, srv, connectFn)
		}
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
// If modelName corresponds to a Claude model, it uses the Anthropic service.
// If modelName is "gemini", it uses the Gemini service.
// Otherwise, it tries to use the OpenAI service with the specified model.
// Returns an error if the model name is not recognized or if required configuration is missing.
func selectLLMService(client *http.Client, flags CLIFlags, spec modelSpec) (llm.Service, error) {
	if ant.IsClaudeModel(flags.modelName) {
		if spec.apiKey == "" {
			return nil, fmt.Errorf("no anthropic api key provided, set %s", ant.APIKeyEnv)
		}
		return &ant.Service{
			HTTPC:   client,
			URL:     spec.modelURL,
			APIKey:  spec.apiKey,
			DumpLLM: flags.dumpLLM,
			Model:   ant.ClaudeModelName(flags.modelName),
		}, nil
	}

	if flags.modelName == "gemini" {
		if spec.apiKey == "" {
			return nil, fmt.Errorf("no gemini api key provided, set %s", gem.GeminiAPIKeyEnv)
		}
		return &gem.Service{
			HTTPC:   client,
			URL:     spec.modelURL,
			Model:   gem.DefaultModel,
			APIKey:  spec.apiKey,
			DumpLLM: flags.dumpLLM,
		}, nil
	}

	model := oai.ModelByUserName(flags.modelName)
	if model.IsZero() {
		return nil, fmt.Errorf("unknown model '%s', use -list-models to see available models", flags.modelName)
	}

	// Verify we have an API key, if necessary.
	apiKey := cmp.Or(spec.apiKey, os.Getenv(model.APIKeyEnv), flags.llmAPIKey)
	if apiKey == "" && model.APIKeyEnv != "NONE" {
		return nil, fmt.Errorf("missing API key for %s model, set %s environment variable", model.UserName, model.APIKeyEnv)
	}

	// Respect skaband-provided model name, if present.
	if spec.oaiModelName != "" {
		model.ModelName = spec.oaiModelName
	}

	return &oai.Service{
		HTTPC:    client,
		Model:    model,
		ModelURL: spec.modelURL,
		APIKey:   apiKey,
		DumpLLM:  flags.dumpLLM,
	}, nil
}

func envNameForModel(modelName string) string {
	switch {
	case ant.IsClaudeModel(modelName):
		return ant.APIKeyEnv
	case modelName == "gemini":
		return gem.GeminiAPIKeyEnv
	default:
		model := oai.ModelByUserName(modelName)
		if model.IsZero() {
			return ""
		}
		return model.APIKeyEnv
	}
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

// zombieReaper monitors /proc for zombie processes and reaps them after 5 minutes.
// This goroutine should only run when we are PID 1 (init process).
func zombieReaper(ctx context.Context) {
	if runtime.GOOS != "linux" {
		return // only needed on Linux
	}
	if os.Getpid() != 1 {
		return // not running as init, exit immediately
	}

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	// Track when we first saw each zombie process
	zombieStartTime := make(map[int]time.Time)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Find zombie processes
			currentZombies := findZombieProcesses()
			now := time.Now()

			// Check existing zombies
			for pid, startTime := range zombieStartTime {
				if _, exists := currentZombies[pid]; !exists {
					// Process is no longer a zombie, remove from tracking
					delete(zombieStartTime, pid)
					continue
				}

				// If zombie has been around for 5+ minutes, try to reap it
				if now.Sub(startTime) >= 5*time.Minute {
					var wstatus syscall.WaitStatus
					reapedPid, err := syscall.Wait4(pid, &wstatus, syscall.WNOHANG, nil)
					if err == nil && reapedPid == pid {
						slog.Info("reaped long-lived zombie process", "pid", pid, "zombie_duration", now.Sub(startTime), "status", wstatus)
						delete(zombieStartTime, pid)
					} else if err != nil && err != syscall.ECHILD {
						slog.Debug("failed to reap zombie process", "pid", pid, "error", err)
					}
				}
			}

			// Track new zombies
			for pid := range currentZombies {
				if _, exists := zombieStartTime[pid]; !exists {
					// New zombie process discovered
					zombieStartTime[pid] = now
					slog.Debug("discovered zombie process", "pid", pid)
				}
			}
		}
	}
}

// findZombieProcesses scans /proc to find zombie processes.
// Returns a map of PID -> true for all zombie processes.
func findZombieProcesses() map[int]bool {
	zombies := make(map[int]bool)

	if runtime.GOOS != "linux" {
		return zombies // empty map on non-Linux
	}

	// Read /proc directory
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return zombies
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Check if directory name is a PID (numeric)
		var pid int
		if _, err := fmt.Sscanf(entry.Name(), "%d", &pid); err != nil {
			continue
		}

		// Read /proc/PID/stat to check process state
		statPath := fmt.Sprintf("/proc/%d/stat", pid)
		statData, err := os.ReadFile(statPath)
		if err != nil {
			continue // Process may have disappeared
		}

		// Parse the stat file to get the process state
		// Format: pid (comm) state ...
		statStr := string(statData)
		fields := strings.Fields(statStr)
		if len(fields) >= 3 {
			state := fields[2]
			if state == "Z" {
				zombies[pid] = true
			}
		}
	}

	return zombies
}
