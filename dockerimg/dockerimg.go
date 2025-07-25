// Package dockerimg
package dockerimg

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/ssh"
	"sketch.dev/browser"
	"sketch.dev/embedded"
	"sketch.dev/loop/server"
	"sketch.dev/skribe"
)

// ContainerConfig holds all configuration for launching a container
type ContainerConfig struct {
	// SessionID is the unique identifier for this session
	SessionID string

	// LocalAddr is the initial address to use (though it may be overwritten later)
	LocalAddr string

	// SkabandAddr is the address of the skaband service if available
	SkabandAddr string

	// Model is the name of the LLM model to use.
	Model string

	// ModelURL is the URL of the LLM service.
	ModelURL string

	// ModelAPIKey is the API key for LLM service.
	ModelAPIKey string

	// Path is the local filesystem path to use
	Path string

	// GitUsername is the username to use for git operations
	GitUsername string

	// GitEmail is the email to use for git operations
	GitEmail string

	// OpenBrowser determines whether to open a browser automatically
	OpenBrowser bool

	// NoCleanup prevents container cleanup when set to true
	NoCleanup bool

	// ForceRebuild forces rebuilding of the Docker image even if it exists
	ForceRebuild bool

	// BaseImage is the base Docker image to use for layering the repo
	BaseImage string

	// Host directory to copy container logs into, if not set to ""
	ContainerLogDest string

	// Path to pre-built linux sketch binary, or build a new one if set to ""
	SketchBinaryLinux string

	// Sketch client public key.
	SketchPubKey string

	// Host port for the container's ssh server
	SSHPort int

	// Outside information to pass to the container
	OutsideHostname   string
	OutsideOS         string
	OutsideWorkingDir string

	// If true, exit after the first turn
	OneShot bool

	// Initial prompt
	Prompt string

	// Verbose enables verbose output
	Verbose bool

	// DockerArgs are additional arguments to pass to the docker create command
	DockerArgs string

	// Mounts specifies volumes to mount in the container in format /path/on/host:/path/in/container
	Mounts []string

	// ExperimentFlag contains the experimental features to enable
	ExperimentFlag string

	// TermUI enables terminal UI
	TermUI bool

	// Budget configuration
	MaxDollars float64

	GitRemoteUrl string

	// Original git origin URL from the host repository
	OriginalGitOrigin string

	// Upstream branch for git work
	Upstream string

	// Commit hash to checkout from GetRemoteUrl
	Commit string

	// Outtie's HTTP server
	OutsideHTTP string

	// Prefix for git branches created by sketch
	BranchPrefix string

	// LinkToGitHub enables GitHub branch linking in UI
	LinkToGitHub bool

	// SubtraceToken enables running sketch under subtrace.dev (development only)
	SubtraceToken string

	// MCPServers contains MCP server configurations
	MCPServers []string

	// PentestMode enables specific pentesting features when true
	PentestMode bool

	// PentestTarget specifies the target for pentesting operations
	PentestTarget string

	// PentestScope defines the scope of the pentesting engagement
	PentestScope string

	// PentestResultsDir specifies where to store pentesting results
	PentestResultsDir string
}

// LaunchContainer creates a docker container for a project, installs sketch and opens a connection to it.
// It writes status to stdout.
func LaunchContainer(ctx context.Context, config ContainerConfig) error {
	slog.Debug("Container Config", slog.String("config", fmt.Sprintf("%+v", config)))
	if _, err := exec.LookPath("docker"); err != nil {
		if runtime.GOOS == "darwin" {
			return fmt.Errorf("cannot find `docker` binary; run: brew install docker colima && colima start")
		} else {
			return fmt.Errorf("cannot find `docker` binary; install docker (e.g., apt-get install docker.io)")
		}
	}

	if out, err := combinedOutput(ctx, "docker", "ps"); err != nil {
		// `docker ps` provides a good error message here that can be
		// easily chatgpt'ed by users, so send it to the user as-is:
		//		Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?
		return fmt.Errorf("docker ps: %s (%w)", out, err)
	}

	_, hostPort, err := net.SplitHostPort(config.LocalAddr)
	if err != nil {
		return err
	}
	// Bail early if sketch was started from a path that isn't in a git repo.
	err = requireGitRepo(ctx, config.Path)
	if err != nil {
		return err
	}

	// Best effort attempt to get repo root; fall back to current directory.
	gitRoot := config.Path
	if root, err := gitRepoRoot(ctx, config.Path); err == nil {
		gitRoot = root
	}

	// Capture the original git origin URL before we set up the temporary git server
	config.OriginalGitOrigin = getOriginalGitOrigin(ctx, gitRoot)

	imgName, err := findOrBuildDockerImage(ctx, gitRoot, config.BaseImage, config.ForceRebuild, config.Verbose)
	if err != nil {
		return err
	}

	cntrName := "sketch-" + config.SessionID
	defer func() {
		if config.NoCleanup {
			return
		}
		if out, err := combinedOutput(ctx, "docker", "kill", cntrName); err != nil {
			// TODO: print in verbose mode? fmt.Fprintf(os.Stderr, "docker kill: %s: %v\n", out, err)
			_ = out
		}
		if out, err := combinedOutput(ctx, "docker", "rm", cntrName); err != nil {
			// TODO: print in verbose mode? fmt.Fprintf(os.Stderr, "docker kill: %s: %v\n", out, err)
			_ = out
		}
	}()

	// errCh receives errors from operations that this function calls in separate goroutines.
	errCh := make(chan error)

	// Start the git server
	gitSrv, err := newGitServer(gitRoot)
	if err != nil {
		return fmt.Errorf("failed to start git server: %w", err)
	}
	defer gitSrv.shutdown(ctx)

	go func() {
		errCh <- gitSrv.serve(ctx)
	}()

	// Check if we have any commits, and if not, create an empty initial commit
	cmd := exec.CommandContext(ctx, "git", "rev-list", "--all", "--count")
	countOut, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git rev-list --all --count: %s: %w", countOut, err)
	}
	commitCount := strings.TrimSpace(string(countOut))
	if commitCount == "0" {
		slog.Info("No commits found, creating empty initial commit")
		cmd = exec.CommandContext(ctx, "git", "commit", "--allow-empty", "-m", "Initial empty commit")
		if commitOut, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git commit --allow-empty: %s: %w", commitOut, err)
		}
	}

	// Get the current host git commit
	var commit string
	if out, err := combinedOutput(ctx, "git", "rev-parse", "HEAD"); err != nil {
		return fmt.Errorf("git rev-parse HEAD: %w", err)
	} else {
		commit = strings.TrimSpace(string(out))
	}

	var upstream string
	if out, err := combinedOutput(ctx, "git", "branch", "--show-current"); err != nil {
		slog.DebugContext(ctx, "git branch --show-current failed (continuing)", "error", err)
	} else {
		upstream = strings.TrimSpace(string(out))
	}
	if out, err := combinedOutput(ctx, "git", "config", "http.receivepack", "true"); err != nil {
		return fmt.Errorf("git config http.receivepack true: %s: %w", out, err)
	}

	relPath, err := filepath.Rel(gitRoot, config.Path)
	if err != nil {
		return err
	}

	config.OutsideHTTP = fmt.Sprintf("http://sketch:%s@host.docker.internal:%s", gitSrv.pass, gitSrv.gitPort)
	config.GitRemoteUrl = fmt.Sprintf("http://sketch:%s@host.docker.internal:%s/.git", gitSrv.pass, gitSrv.gitPort)
	config.Upstream = upstream
	config.Commit = commit

	// Create the sketch container, copy over linux sketch
	if err := createDockerContainer(ctx, cntrName, hostPort, relPath, imgName, config); err != nil {
		return fmt.Errorf("failed to create docker container: %w", err)
	}
	if err := copyEmbeddedLinuxBinaryToContainer(ctx, cntrName); err != nil {
		return fmt.Errorf("failed to copy linux binary to container: %w", err)
	}

	if config.PentestMode {
		fmt.Printf("🛡️ running Kali Linux pentesting container %s\n", cntrName)
		fmt.Println("🔒 Pentesting tools available: nmap, metasploit, burpsuite, dirb, nikto, sqlmap, hydra, john, wireshark, etc.")
	} else {
		fmt.Printf("📦 running in Kali Linux container %s\n", cntrName)
	}

	// Setup subtrace if token is provided (development only) - after container creation, before start
	if config.SubtraceToken != "" {
		fmt.Println("🔍 Setting up subtrace (development only)")
		if err := setupSubtraceBeforeStart(ctx, cntrName, config.SubtraceToken); err != nil {
			return fmt.Errorf("failed to setup subtrace: %w", err)
		}
	}

	// Start the sketch container
	if out, err := combinedOutput(ctx, "docker", "start", cntrName); err != nil {
		return fmt.Errorf("docker start: %s, %w", out, err)
	}

	// Copies structured logs from the container to the host.
	copyLogs := func() {
		if config.ContainerLogDest == "" {
			return
		}
		out, err := combinedOutput(ctx, "docker", "logs", cntrName)
		if err != nil {
			fmt.Fprintf(os.Stderr, "docker logs failed: %v\n", err)
			return
		}
		prefix := []byte("structured logs:")
		for line := range bytes.Lines(out) {
			rest, ok := bytes.CutPrefix(line, prefix)
			if !ok {
				continue
			}
			logFile := string(bytes.TrimSpace(rest))
			srcPath := fmt.Sprintf("%s:%s", cntrName, logFile)
			logFileName := filepath.Base(logFile)
			dstPath := filepath.Join(config.ContainerLogDest, logFileName)
			_, err := combinedOutput(ctx, "docker", "cp", srcPath, dstPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "docker cp %s %s failed: %v\n", srcPath, dstPath, err)
			}
			fmt.Fprintf(os.Stderr, "\ncopied container log %s to %s\n", srcPath, dstPath)
		}
	}

	// NOTE: we want to see what the internal sketch binary prints
	// regardless of the setting of the verbosity flag on the external
	// binary, so reading "docker logs", which is the stdout/stderr of
	// the internal binary is not conditional on the verbose flag.
	appendInternalErr := func(err error) error {
		if err == nil {
			return nil
		}
		out, logsErr := combinedOutput(ctx, "docker", "logs", cntrName)
		if logsErr != nil {
			return fmt.Errorf("%w; and docker logs failed: %s, %v", err, out, logsErr)
		}
		out = bytes.TrimSpace(out)
		if len(out) > 0 {
			return fmt.Errorf("docker logs: %s;\n%w", out, err)
		}
		return err
	}

	// Get the sketch server port from the container
	localAddr, err := getContainerPort(ctx, cntrName, "80")
	if err != nil {
		return appendInternalErr(err)
	}

	if config.Verbose {
		fmt.Fprintf(os.Stderr, "Host web server: http://%s/\n", localAddr)
	}

	localSSHAddr, err := getContainerPort(ctx, cntrName, "22")
	if err != nil {
		return appendInternalErr(err)
	}
	sshHost, sshPort, err := net.SplitHostPort(localSSHAddr)
	if err != nil {
		return appendInternalErr(fmt.Errorf("failed to split ssh host and port: %w", err))
	}

	var sshServerIdentity, sshUserIdentity, containerCAPublicKey, hostCertificate []byte

	cst, err := NewLocalSSHimmer(cntrName, sshHost, sshPort)
	if err != nil {
		return appendInternalErr(fmt.Errorf("NewContainerSSHTheather: %w", err))
	}

	sshErr := CheckSSHReachability(cntrName)
	sshAvailable := false
	sshErrMsg := ""
	if sshErr != nil {
		fmt.Println(sshErr.Error())
		sshErrMsg = sshErr.Error()
		// continue - ssh config is not required for the rest of sketch to function locally.
	} else {
		sshAvailable = true
		// Note: The vscode: link uses an undocumented request parameter that I really had to dig to find:
		// https://github.com/microsoft/vscode/blob/2b9486161abaca59b5132ce3c59544f3cc7000f6/src/vs/code/electron-main/app.ts#L878
		fmt.Printf(`Connect to this container via any of these methods:
🖥️  ssh %s
🖥️  code --remote ssh-remote+root@%s /app -n
🔗 vscode://vscode-remote/ssh-remote+root@%s/app?windowId=_blank
`, cntrName, cntrName, cntrName)
		sshUserIdentity = cst.userIdentity
		sshServerIdentity = cst.serverIdentity

		// Get the Container CA public key for mutual auth
		if cst.containerCAPublicKey != nil {
			containerCAPublicKey = ssh.MarshalAuthorizedKey(cst.containerCAPublicKey)
			fmt.Println("🔒 SSH Mutual Authentication enabled (container will verify host)")
		}

		// Get the host certificate for mutual auth
		hostCertificate = cst.hostCertificate

		defer func() {
			if err := cst.Cleanup(); err != nil {
				appendInternalErr(err)
			}
		}()
	}

	// Tell the sketch container to Init(), which starts the SSH server
	// and checks out the right commit.
	// TODO: I'm trying to move as much configuration as possible into the command-line
	// arguments to avoid splitting them up. "localAddr" is the only difficult one:
	// we run (effectively) "docker run -p 0:80 image sketch -flags" and you can't
	// get the port Docker chose until after the process starts. The SSH config is
	// mostly available ahead of time, but whether it works ("sshAvailable"/"sshErrMsg")
	// may also empirically need to be done after the SSH server is up and running.
	go func() {
		// TODO: Why is this called in a goroutine? I have found that when I pull this out
		// of the goroutine and call it inline, then the terminal UI clears itself and all
		// the scrollback (which is not good, but also not fatal).  I can't see why it does this
		// though, since none of the calls in postContainerInitConfig obviously write to stdout
		// or stderr.
		if err := postContainerInitConfig(ctx, localAddr, sshAvailable, sshErrMsg, sshServerIdentity, sshUserIdentity, containerCAPublicKey, hostCertificate); err != nil {
			slog.ErrorContext(ctx, "LaunchContainer.postContainerInitConfig", slog.String("err", err.Error()))
			errCh <- appendInternalErr(err)
		}

		// We open the browser after the init config because the above waits for the web server to be serving.
		ps1URL := "http://" + localAddr
		if config.SkabandAddr != "" {
			ps1URL = fmt.Sprintf("%s/s/%s", config.SkabandAddr, config.SessionID)
		}
		if config.OpenBrowser {
			browser.Open(ps1URL)
		}
		gitSrv.ps1URL.Store(&ps1URL)
	}()

	go func() {
		cmd := exec.CommandContext(ctx, "docker", "attach", cntrName)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		errCh <- run(ctx, "docker attach", cmd)
	}()

	defer copyLogs()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			if err != nil {
				return appendInternalErr(fmt.Errorf("container process: %w", err))
			}
			return nil
		}
	}
}

func combinedOutput(ctx context.Context, cmdName string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, cmdName, args...)
	start := time.Now()

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, cmdName, slog.Duration("elapsed", time.Since(start)), slog.String("err", err.Error()), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	} else {
		slog.DebugContext(ctx, cmdName, slog.Duration("elapsed", time.Since(start)), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	}
	return out, err
}

func run(ctx context.Context, cmdName string, cmd *exec.Cmd) error {
	start := time.Now()
	err := cmd.Run()
	if err != nil {
		slog.ErrorContext(ctx, cmdName, slog.Duration("elapsed", time.Since(start)), slog.String("err", err.Error()), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	} else {
		slog.DebugContext(ctx, cmdName, slog.Duration("elapsed", time.Since(start)), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	}
	return err
}

type gitServer struct {
	gitLn   net.Listener
	gitPort string
	srv     *http.Server
	pass    string
	ps1URL  atomic.Pointer[string]
}

func (gs *gitServer) shutdown(ctx context.Context) {
	gs.srv.Shutdown(ctx)
	gs.gitLn.Close()
}

// Serve a git remote from the host for the container to fetch from and push to.
func (gs *gitServer) serve(ctx context.Context) error {
	slog.DebugContext(ctx, "starting git server", slog.String("git_remote_addr", "http://host.docker.internal:"+gs.gitPort+"/.git"))
	return gs.srv.Serve(gs.gitLn)
}

func newGitServer(gitRoot string) (*gitServer, error) {
	ret := &gitServer{
		pass: rand.Text(),
	}

	gitLn, err := net.Listen("tcp4", ":0")
	if err != nil {
		return nil, fmt.Errorf("git listen: %w", err)
	}
	ret.gitLn = gitLn

	browserC := make(chan bool, 1) // channel of browser open requests

	go func() {
		for range browserC {
			browser.Open(*ret.ps1URL.Load())
		}
	}()

	srv := http.Server{Handler: &gitHTTP{gitRepoRoot: gitRoot, pass: []byte(ret.pass), browserC: browserC}}
	ret.srv = &srv

	_, gitPort, err := net.SplitHostPort(gitLn.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("git port: %w", err)
	}
	ret.gitPort = gitPort
	return ret, nil
}

func createDockerContainer(ctx context.Context, cntrName, hostPort, relPath, imgName string, config ContainerConfig) error {
	cmdArgs := []string{
		"create",
		"-i",
		"--name", cntrName,
		"-p", hostPort + ":80", // forward container port 80 to a host port
		"-e", "SKETCH_MODEL_API_KEY=" + config.ModelAPIKey,
	}
	if !(config.OneShot || !config.TermUI) {
		cmdArgs = append(cmdArgs, "-t")
	}

	for _, envVar := range getEnvForwardingFromGitConfig(ctx) {
		cmdArgs = append(cmdArgs, "-e", envVar)
	}
	if config.ModelURL != "" {
		cmdArgs = append(cmdArgs, "-e", "SKETCH_MODEL_URL="+config.ModelURL)
	}
	if config.SketchPubKey != "" {
		cmdArgs = append(cmdArgs, "-e", "SKETCH_PUB_KEY="+config.SketchPubKey)
	}
	if config.SSHPort > 0 {
		cmdArgs = append(cmdArgs, "-p", fmt.Sprintf("%d:22", config.SSHPort)) // forward container ssh port to host ssh port
	} else {
		cmdArgs = append(cmdArgs, "-p", "0:22") // use an ephemeral host port for ssh.
	}
	// colima does this by default, but Linux docker seems to need this set explicitly
	cmdArgs = append(cmdArgs, "--add-host", "host.docker.internal:host-gateway")

	// Add network capabilities for pentesting if PentestMode is enabled
	if config.PentestMode {
		// Use host network mode for better network access during pentesting
		cmdArgs = append(cmdArgs, "--network=host")

		// Add capabilities needed for network scanning and packet manipulation
		cmdArgs = append(cmdArgs, "--cap-add=NET_ADMIN", "--cap-add=NET_RAW")

		// Allow ptrace for debugging tools
		cmdArgs = append(cmdArgs, "--security-opt", "seccomp=unconfined")

		// Set environment variables for pentesting tools
		cmdArgs = append(cmdArgs, "-e", "PENTESTING_TOOLS_PATH=/opt")
		cmdArgs = append(cmdArgs, "-e", "WORDLISTS_PATH=/usr/share/wordlists")
	}

	// Add seccomp profile to prevent killing PID 1 (the sketch process itself)
	// Write the seccomp profile to cache directory if it doesn't exist
	seccompPath, err := ensureSeccompProfile(ctx)
	if err != nil {
		return fmt.Errorf("failed to create seccomp profile: %w", err)
	}
	cmdArgs = append(cmdArgs, "--security-opt", "seccomp="+seccompPath)

	// Add subtrace environment variable if token is provided
	if config.SubtraceToken != "" {
		cmdArgs = append(cmdArgs, "-e", "SUBTRACE_TOKEN="+config.SubtraceToken)
		cmdArgs = append(cmdArgs, "-e", "SUBTRACE_HTTP2=1")
	}

	// Add volume mounts if specified
	for _, mount := range config.Mounts {
		if mount != "" {
			cmdArgs = append(cmdArgs, "-v", mount)
		}
	}
	cmdArgs = append(cmdArgs, imgName)

	// Add command: either [sketch] or [subtrace run -- sketch]
	if config.SubtraceToken != "" {
		cmdArgs = append(cmdArgs, "/usr/local/bin/subtrace", "run", "--", "/bin/sketch")
	} else {
		cmdArgs = append(cmdArgs, "/bin/sketch")
	}

	// Add all sketch arguments
	cmdArgs = append(cmdArgs,
		"-unsafe",
		"-addr=:80",
		"-session-id="+config.SessionID,
		"-git-username="+config.GitUsername,
		"-git-email="+config.GitEmail,
		"-outside-hostname="+config.OutsideHostname,
		"-outside-os="+config.OutsideOS,
		"-outside-working-dir="+config.OutsideWorkingDir,
		fmt.Sprintf("-max-dollars=%f", config.MaxDollars),
		"-open=false",
		"-termui="+fmt.Sprintf("%t", config.TermUI),
		"-verbose="+fmt.Sprintf("%t", config.Verbose),
		"-x="+config.ExperimentFlag,
		"-branch-prefix="+config.BranchPrefix,
		"-link-to-github="+fmt.Sprintf("%t", config.LinkToGitHub),
	)
	// Set SSH connection string based on session ID for SSH Theater
	cmdArgs = append(cmdArgs, "-ssh-connection-string=sketch-"+config.SessionID)
	if relPath != "." {
		cmdArgs = append(cmdArgs, "-C", relPath)
	}
	if config.Model != "" {
		cmdArgs = append(cmdArgs, "-model="+config.Model)
	}
	if config.GitRemoteUrl != "" {
		cmdArgs = append(cmdArgs, "-git-remote-url="+config.GitRemoteUrl)
		if config.Commit == "" {
			panic("Commit should have been set when GitRemoteUrl was set")
		}
		cmdArgs = append(cmdArgs, "-commit="+config.Commit)
		cmdArgs = append(cmdArgs, "-upstream="+config.Upstream)
	}
	if config.OriginalGitOrigin != "" {
		cmdArgs = append(cmdArgs, "-original-git-origin="+config.OriginalGitOrigin)
	}
	if config.OutsideHTTP != "" {
		cmdArgs = append(cmdArgs, "-outside-http="+config.OutsideHTTP)
	}
	cmdArgs = append(cmdArgs, "-skaband-addr="+config.SkabandAddr)
	if config.Prompt != "" {
		cmdArgs = append(cmdArgs, "-prompt", config.Prompt)
	}
	if config.OneShot {
		cmdArgs = append(cmdArgs, "-one-shot")
	}
	if config.ModelURL == "" {
		// Forward ANTHROPIC_API_KEY for direct use.
		// TODO: have outtie run an http proxy?
		// TODO: select and forward the relevant API key based on the model
		cmdArgs = append(cmdArgs, "-llm-api-key="+os.Getenv("ANTHROPIC_API_KEY"))
	}
	// Add MCP server configurations
	for _, mcpServer := range config.MCPServers {
		cmdArgs = append(cmdArgs, "-mcp", mcpServer)
	}

	// Add pentesting-specific configurations if PentestMode is enabled
	if config.PentestMode {
		cmdArgs = append(cmdArgs, "-pentest-mode=true")
		if config.PentestTarget != "" {
			cmdArgs = append(cmdArgs, "-pentest-target="+config.PentestTarget)
		}
		if config.PentestScope != "" {
			cmdArgs = append(cmdArgs, "-pentest-scope="+config.PentestScope)
		}
		if config.PentestResultsDir != "" {
			// Create a volume mount for pentest results if not already specified
			resultsMount := config.PentestResultsDir + ":/opt/pentest-results"
			mountExists := false
			for _, mount := range config.Mounts {
				if strings.Contains(mount, "/opt/pentest-results") {
					mountExists = true
					break
				}
			}
			if !mountExists {
				cmdArgs = append(cmdArgs, "-v", resultsMount)
			}
			cmdArgs = append(cmdArgs, "-pentest-results-dir=/opt/pentest-results")
		}
	}

	// Add additional docker arguments if provided
	if config.DockerArgs != "" {
		// Parse space-separated docker arguments with support for quotes and escaping
		args := parseDockerArgs(config.DockerArgs)
		// Insert arguments after "create" but before other arguments
		for i := len(args) - 1; i >= 0; i-- {
			cmdArgs = append(cmdArgs[:1], append([]string{args[i]}, cmdArgs[1:]...)...)
		}
	}

	if out, err := combinedOutput(ctx, "docker", cmdArgs...); err != nil {
		return fmt.Errorf("docker create: %s, %w", out, err)
	}
	return nil
}

func getContainerPort(ctx context.Context, cntrName, cntrPort string) (string, error) {
	localAddr := ""
	if out, err := combinedOutput(ctx, "docker", "port", cntrName, cntrPort); err != nil {
		return "", fmt.Errorf("failed to find container port: %s: %v", out, err)
	} else {
		v4, _, found := strings.Cut(string(out), "\n")
		if !found {
			return "", fmt.Errorf("failed to find container port: %s: %v", out, err)
		}
		localAddr = v4
		if strings.HasPrefix(localAddr, "0.0.0.0") {
			localAddr = "127.0.0.1" + strings.TrimPrefix(localAddr, "0.0.0.0")
		}
	}
	return localAddr, nil
}

// Contact the container and configure it.
func postContainerInitConfig(ctx context.Context, localAddr string, sshAvailable bool, sshError string, sshServerIdentity, sshAuthorizedKeys, sshContainerCAKey, sshHostCertificate []byte) error {
	localURL := "http://" + localAddr

	initMsg, err := json.Marshal(
		server.InitRequest{
			HostAddr:           localAddr,
			SSHAuthorizedKeys:  sshAuthorizedKeys,
			SSHServerIdentity:  sshServerIdentity,
			SSHContainerCAKey:  sshContainerCAKey,
			SSHHostCertificate: sshHostCertificate,
			SSHAvailable:       sshAvailable,
			SSHError:           sshError,
		})
	if err != nil {
		return fmt.Errorf("init msg: %w", err)
	}

	// Note: this /init POST is handled in loop/server/loophttp.go:
	initMsgByteReader := bytes.NewReader(initMsg)
	req, err := http.NewRequest("POST", localURL+"/init", initMsgByteReader)
	if err != nil {
		return err
	}

	var res *http.Response
	for i := 0; ; i++ {
		time.Sleep(100 * time.Millisecond)
		// If you DON'T reset this byteReader, then subsequent retries may end up sending 0 bytes.
		initMsgByteReader.Reset(initMsg)
		res, err = http.DefaultClient.Do(req)
		if err != nil {
			if i < 100 {
				if i%10 == 0 {
					slog.DebugContext(ctx, "postContainerInitConfig retrying", slog.Int("retry", i), slog.String("err", err.Error()))
				}
				continue
			}
			return fmt.Errorf("failed to %s/init sketch in container, NOT retrying: err: %v", localURL, err)
		}
		break
	}
	resBytes, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to initialize sketch in container, response status code %d: %s", res.StatusCode, resBytes)
	}
	return nil
}

func findOrBuildDockerImage(ctx context.Context, gitRoot, baseImage string, forceRebuild, verbose bool) (imgName string, err error) {
	// Default to the published sketch image if no base image is specified
	if baseImage == "" {
		// Use Kali Linux directly as the base image
		baseImage = "kalilinux/kali-rolling:latest"
		fmt.Printf("🐋 Using Kali Linux base image directly\n")
	}

	// Ensure the base image exists locally, pull if necessary
	if err := ensureBaseImageExists(ctx, baseImage); err != nil {
		return "", fmt.Errorf("failed to ensure base image %s exists: %w", baseImage, err)
	}

	// Get the base image container ID for caching
	baseImageID, err := getDockerImageID(ctx, baseImage)
	if err != nil {
		return "", fmt.Errorf("failed to get base image ID for %s: %w", baseImage, err)
	}

	// Create a cache key based on base image ID and working directory
	// Docker naming conventions restrict you to 20 characters per path component
	// and only allow lowercase letters, digits, underscores, and dashes, so encoding
	// the hash and the repo directory is sadly a bit of a non-starter.
	cacheKey := createCacheKey(baseImageID, gitRoot)
	imgName = "sketch-" + cacheKey

	// Check if the cached image exists and is up to date
	if !forceRebuild {
		if exists, err := dockerImageExists(ctx, imgName); err != nil {
			return "", fmt.Errorf("failed to check if image exists: %w", err)
		} else if exists {
			if verbose {
				fmt.Printf("using cached image %s\n", imgName)
			}
			return imgName, nil
		}
	}

	// Build the layered image
	if err := buildLayeredImage(ctx, imgName, baseImage, gitRoot, verbose); err != nil {
		return "", fmt.Errorf("failed to build layered image: %w", err)
	}

	return imgName, nil
}

// ensureBaseImageExists checks if the base image exists locally and pulls it if not
func ensureBaseImageExists(ctx context.Context, imageName string) error {
	exists, err := dockerImageExists(ctx, imageName)
	if err != nil {
		return fmt.Errorf("failed to check if image exists: %w", err)
	}

	if !exists {
		fmt.Printf("🐋 pulling base image %s...\n", imageName)
		if out, err := combinedOutput(ctx, "docker", "pull", imageName); err != nil {
			// If the specific hash-tagged image doesn't exist, try falling back to "latest"
			if strings.Contains(string(out), "manifest unknown") {
				fmt.Printf("⚠️ Image %s not found, trying latest tag instead...\n", imageName)
				fallbackImage := dockerImgName + ":latest"
				if fallbackOut, fallbackErr := combinedOutput(ctx, "docker", "pull", fallbackImage); fallbackErr != nil {
					return fmt.Errorf("docker pull fallback to %s failed: %s: %w", fallbackImage, fallbackOut, fallbackErr)
				}
				// Tag the pulled latest image with the expected hash tag
				if tagOut, tagErr := combinedOutput(ctx, "docker", "tag", fallbackImage, imageName); tagErr != nil {
					return fmt.Errorf("docker tag %s %s failed: %s: %w", fallbackImage, imageName, tagOut, tagErr)
				}
				fmt.Printf("✅ successfully pulled %s and tagged as %s\n", fallbackImage, imageName)
				return nil
			}
			return fmt.Errorf("docker pull %s failed: %s: %w", imageName, out, err)
		}
		fmt.Printf("✅ successfully pulled %s\n", imageName)
	}

	return nil
}

// getDockerImageID gets the container ID for a Docker image
func getDockerImageID(ctx context.Context, imageName string) (string, error) {
	out, err := combinedOutput(ctx, "docker", "inspect", "--format", "{{.Id}}", imageName)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// createCacheKey creates a cache key from base image ID and working directory
func createCacheKey(baseImageID, gitRoot string) string {
	h := sha256.New()
	h.Write([]byte(baseImageID))
	h.Write([]byte(gitRoot))
	// one-time cache-busting for the transition from copying git repos to only copying git objects
	h.Write([]byte("git-objects"))
	return hex.EncodeToString(h.Sum(nil))[:12] // Use first 12 chars for shorter name
}

// dockerImageExists checks if a Docker image exists locally
func dockerImageExists(ctx context.Context, imageName string) (bool, error) {
	out, err := combinedOutput(ctx, "docker", "inspect", imageName)
	if err != nil {
		if strings.Contains(strings.ToLower(string(out)), "no such object") ||
			strings.Contains(strings.ToLower(string(out)), "no such image") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// buildLayeredImage builds a new Docker image by layering the repo on top of the base image.
//
// TODO: git config stuff could be environment variables at runtime for email and username.
// The git docs seem to say that http.postBuffer is a bug in our git proxy more than a thing
// that's needed, but we haven't found the bug yet!
//
// TODO: There is a caching tension. A base image is great for tools (like, some version
// of Go). Then you want a git repo, which is much faster to incrementally fetch rather
// than cloning every time. Then you want some build artifacts, like perhaps the
// "go mod download" cache, or the "go build" cache or the "npm install" cache.
// The implementation here copies the git objects into the base image.
// That enables fast clones into the container, because most of the git objects are already there.
// It also avoids copying uncommitted changes, configs/hooks, etc.
// We also set up fake temporary Go module(s) so we can run "go mod download".
// TODO: maybe 'go list ./...' and then do a build as well to populate the build cache.
// TODO: 'npm install', etc? We have the rails for it.
// If /app/.git already exists, we fetch from the existing repo instead of cloning.
// This lets advanced users arrange their git repo exactly as they desire.
// Note that buildx has some support for conditional COPY, but without buildx, which
// we can't reliably depend on, we have to run the base image to inspect its file system,
// and then we can decide what to do.
//
// We may in the future want to enable people to bring along uncommitted changes to tracked files.
// To do that, we would run `git stash create` in outie at launch time, treat HEAD as the base commit,
// and add in the stash commit as a new commit atop it.
// That would accurately model the base commit as well as the uncommitted changes.
// (This wouldn't happen here, but at agent/container initialization time.)
//
// repoPath is the current working directory where sketch is being run from.
func buildLayeredImage(ctx context.Context, imgName, baseImage, gitRoot string, verbose bool) error {
	goModules, err := collectGoModules(ctx, gitRoot)
	if err != nil {
		return fmt.Errorf("failed to collect go modules: %w", err)
	}

	buf := new(strings.Builder)
	line := func(msg string, args ...any) {
		fmt.Fprintf(buf, msg+"\n", args...)
	}

	line("FROM %s", baseImage)
	// TODO: this is a total hack. The base image should have git, but it seems
	// that in some cases we are using a base image that does not have it.
	line("RUN apt-get update && apt-get install -y git")
	line("COPY . /git-ref")

	for _, module := range goModules {
		line("RUN mkdir -p /go-module")
		line("RUN git --git-dir=/git-ref --work-tree=/go-module cat-file blob %s > /go-module/go.mod", module.modSHA)
		if module.sumSHA != "" {
			line("RUN git --git-dir=/git-ref --work-tree=/go-module cat-file blob %s > /go-module/go.sum", module.sumSHA)
		}
		// drop any replaced modules
		line("RUN cd /go-module && go mod edit -json | jq -r '.Replace? // [] | .[] | .Old.Path' | xargs -r -I{} go mod edit -dropreplace={} -droprequire={}")
		// grab what’s left, best effort only to avoid breaking on (say) private modules
		line("RUN cd /go-module && go mod download || true")
		line("RUN rm -rf /go-module")
	}

	line("WORKDIR /app")
	line(`CMD ["/bin/sketch"]`)
	dockerfileContent := buf.String()

	// Create a temporary directory for the Dockerfile
	tmpDir, err := os.MkdirTemp("", "sketch-docker-*")
	if err != nil {
		return fmt.Errorf("failed to create temporary directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	dockerfilePath := filepath.Join(tmpDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileContent), 0o666); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Get git user info
	var gitUserEmail, gitUserName string
	if out, err := combinedOutput(ctx, "git", "config", "--get", "user.email"); err != nil {
		return fmt.Errorf("git user.email is not set. Please run 'git config --global user.email \"your.email@example.com\"' to set your email address")
	} else {
		gitUserEmail = strings.TrimSpace(string(out))
	}
	if out, err := combinedOutput(ctx, "git", "config", "--get", "user.name"); err != nil {
		return fmt.Errorf("git user.name is not set. Please run 'git config --global user.name \"Your Name\"' to set your name")
	} else {
		gitUserName = strings.TrimSpace(string(out))
	}

	start := time.Now()
	cmdArgs := []string{
		"build",
		"-t", imgName,
		"-f", dockerfilePath,
		"--build-arg", "GIT_USER_EMAIL=" + gitUserEmail,
		"--build-arg", "GIT_USER_NAME=" + gitUserName,
		".",
	}

	commonDir, err := gitCommonDir(ctx, gitRoot)
	if err != nil {
		return fmt.Errorf("failed to get git common dir: %w", err)
	}

	cmd := exec.CommandContext(ctx, "docker", cmdArgs...)
	cmd.Dir = commonDir
	// We print the docker build output whether or not the user
	// has selected --verbose. Building an image takes a while
	// and this gives good context.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("🏗️  building docker image %s from base %s...\n", imgName, baseImage)

	err = run(ctx, "docker build", cmd)
	if err != nil {
		return fmt.Errorf("docker build failed: %v", err)
	}
	fmt.Printf("built docker image %s in %s\n", imgName, time.Since(start).Round(time.Millisecond))
	return nil
}

// requireGitRepo confirms that path is within a git repository.
func requireGitRepo(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "not a git repository") {
			return fmt.Errorf(`sketch needs to run from within a git repo, but %s is not part of a git repo.
Consider one of the following options:
	- cd to a different dir that is already part of a git repo first, or
	- to create a new git repo from this directory (%s), run this command:

		git init . && git commit --allow-empty -m "initial commit"

and try running sketch again.
`, path, path)
		}
		return fmt.Errorf("git rev-parse --git-dir: %s: %w", out, err)
	}
	return nil
}

// gitRepoRoot attempts to find the git repository root directory.
// Returns an error if not in a git repository or if it's a bare repository.
// This is used to calculate relative paths for preserving user's working directory context.
func gitRepoRoot(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel")
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %s: %w", out, err)
	}
	// The returned path is absolute.
	return strings.TrimSpace(string(out)), nil
}

// gitCommonDir finds the git common directory for path.
func gitCommonDir(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-common-dir")
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir: %s: %w", out, err)
	}
	gitCommonDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitCommonDir) {
		gitCommonDir = filepath.Join(path, gitCommonDir)
	}
	return gitCommonDir, nil
}

// goModuleInfo represents a Go module with its file paths and blob SHAs
type goModuleInfo struct {
	// modPath is the path to the go.mod file, for debugging
	modPath string
	// modSHA is the git blob SHA of the go.mod file
	modSHA string
	// sumSHA is the git blob SHA of the go.sum file, empty if no go.sum exists
	sumSHA string
}

// collectGoModules returns all go.mod files in the git repository with their blob SHAs.
func collectGoModules(ctx context.Context, gitRoot string) ([]goModuleInfo, error) {
	cmd := exec.CommandContext(ctx, "git", "ls-files", "-z", "*.mod")
	cmd.Dir = gitRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git ls-files -z *.mod: %s: %w", out, err)
	}

	modFiles := strings.Split(string(out), "\x00")
	var modules []goModuleInfo
	for _, file := range modFiles {
		if filepath.Base(file) != "go.mod" {
			continue
		}

		modSHA, err := getGitBlobSHA(ctx, gitRoot, file)
		if err != nil {
			return nil, fmt.Errorf("failed to get blob SHA for %s: %w", file, err)
		}

		// If corresponding go.sum exists, get its SHA
		sumFile := filepath.Join(filepath.Dir(file), "go.sum")
		sumSHA, _ := getGitBlobSHA(ctx, gitRoot, sumFile) // best effort

		modules = append(modules, goModuleInfo{
			modPath: file,
			modSHA:  modSHA,
			sumSHA:  sumSHA,
		})
	}

	return modules, nil
}

// getGitBlobSHA returns the git blob SHA for a file at HEAD
func getGitBlobSHA(ctx context.Context, gitRoot, filePath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD:"+filePath)
	cmd.Dir = gitRoot
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD:%s: %s: %w", filePath, out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// getEnvForwardingFromGitConfig retrieves environment variables to pass through to Docker
// from git config using the sketch.envfwd multi-valued key.
func getEnvForwardingFromGitConfig(ctx context.Context) []string {
	outb, err := exec.CommandContext(ctx, "git", "config", "--get-all", "sketch.envfwd").CombinedOutput()
	out := string(outb)
	if err != nil {
		if strings.Contains(out, "key does not exist") {
			return nil
		}
		slog.ErrorContext(ctx, "failed to get sketch.envfwd from git config", "err", err, "output", out)
		return nil
	}

	var envVars []string
	for envVar := range strings.Lines(out) {
		envVar = strings.TrimSpace(envVar)
		if envVar == "" {
			continue
		}
		envVars = append(envVars, envVar+"="+os.Getenv(envVar))
	}
	return envVars
}

// getOriginalGitOrigin returns the URL of the git remote 'origin' if it exists in the given directory
func getOriginalGitOrigin(ctx context.Context, dir string) string {
	cmd := exec.CommandContext(ctx, "git", "config", "--get", "remote.origin.url")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// parseDockerArgs parses a string containing space-separated Docker arguments into an array of strings.
// It handles quoted arguments and escaped characters.
//
// Examples:
//
//	--memory=2g --cpus=2                 -> ["--memory=2g", "--cpus=2"]
//	--label="my label" --env=FOO=bar     -> ["--label=my label", "--env=FOO=bar"]
//	--env="KEY=\"quoted value\""         -> ["--env=KEY=\"quoted value\""]
func parseDockerArgs(args string) []string {
	if args = strings.TrimSpace(args); args == "" {
		return []string{}
	}

	var result []string
	var current strings.Builder
	inQuotes := false
	escapeNext := false
	quoteChar := rune(0)

	for _, char := range args {
		if escapeNext {
			current.WriteRune(char)
			escapeNext = false
			continue
		}

		if char == '\\' {
			escapeNext = true
			continue
		}

		if char == '"' || char == '\'' {
			if !inQuotes {
				inQuotes = true
				quoteChar = char
				continue
			} else if char == quoteChar {
				inQuotes = false
				quoteChar = rune(0)
				continue
			}
			// Non-matching quote character inside quotes
			current.WriteRune(char)
			continue
		}

		// Space outside of quotes is an argument separator
		if char == ' ' && !inQuotes {
			if current.Len() > 0 {
				result = append(result, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteRune(char)
	}

	// Add the last argument if there is one
	if current.Len() > 0 {
		result = append(result, current.String())
	}

	return result
}

// copyEmbeddedLinuxBinaryToContainer copies the embedded linux binary to the container
func copyEmbeddedLinuxBinaryToContainer(ctx context.Context, containerName string) error {
	out, err := combinedOutput(ctx, "docker", "version", "--format", "{{.Server.Arch}}")
	if err != nil {
		return fmt.Errorf("failed to detect Docker server architecture: %s: %w", out, err)
	}
	arch := strings.TrimSpace(string(out))

	bin := embedded.LinuxBinary(arch)
	if bin == nil {
		return fmt.Errorf("no embedded linux binary for architecture %q", arch)
	}

	// Stream a tarball to docker cp.
	pr, pw := io.Pipe()

	errCh := make(chan error, 1)
	go func() {
		defer pw.Close()
		tw := tar.NewWriter(pw)

		hdr := &tar.Header{
			Name: "bin/sketch", // final path inside the container
			Mode: 0o700,
			Size: int64(len(bin)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			errCh <- fmt.Errorf("failed to write tar header: %w", err)
			return
		}
		if _, err := tw.Write(bin); err != nil {
			errCh <- fmt.Errorf("failed to write binary to tar: %w", err)
			return
		}
		if err := tw.Close(); err != nil {
			errCh <- fmt.Errorf("failed to close tar writer: %w", err)
			return
		}
		errCh <- nil
	}()

	cmd := exec.CommandContext(ctx, "docker", "cp", "-", containerName+":/")
	cmd.Stdin = pr

	out, cmdErr := cmd.CombinedOutput()

	if tarErr := <-errCh; tarErr != nil {
		return tarErr
	}
	if cmdErr != nil {
		return fmt.Errorf("docker cp failed: %s: %w", out, cmdErr)
	}
	return nil
}

const seccompProfile = `{
  "defaultAction": "SCMP_ACT_ALLOW",
  "syscalls": [
    {
      "names": ["kill", "tkill", "tgkill", "pidfd_send_signal"],
      "action": "SCMP_ACT_ERRNO",
      "args": [
        {
          "index": 0,
          "value": 1,
          "op": "SCMP_CMP_EQ"
        }
      ]
    }
  ]
}`

// ensureSeccompProfile creates the seccomp profile file in the sketch cache directory if it doesn't exist.
func ensureSeccompProfile(ctx context.Context) (seccompPath string, err error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}
	cacheDir := filepath.Join(homeDir, ".cache", "sketch")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return "", fmt.Errorf("failed to create cache directory: %w", err)
	}
	seccompPath = filepath.Join(cacheDir, "seccomp-no-kill-1.json")

	curBytes, err := os.ReadFile(seccompPath)
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("failed to read seccomp profile file %s: %w", seccompPath, err)
	}
	if string(curBytes) == seccompProfile {
		return seccompPath, nil // File already exists and matches the expected profile
	}

	if err := os.WriteFile(seccompPath, []byte(seccompProfile), 0o644); err != nil {
		return "", fmt.Errorf("failed to write seccomp profile to %s: %w", seccompPath, err)
	}
	slog.DebugContext(ctx, "created seccomp profile", "path", seccompPath)
	return seccompPath, nil
}
