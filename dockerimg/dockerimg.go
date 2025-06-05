// Package dockerimg
package dockerimg

import (
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
	"sketch.dev/llm/ant"
	"sketch.dev/loop/server"
	"sketch.dev/skribe"
	"sketch.dev/webui"
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

	// Commit hash to checkout from GetRemoteUrl
	Commit string

	// Outtie's HTTP server
	OutsideHTTP string

	// Prefix for git branches created by sketch
	BranchPrefix string
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
	gitRoot, err := findGitRoot(ctx, config.Path)
	if err != nil {
		return err
	}
	err = checkForEmptyGitRepo(ctx, config.Path)
	if err != nil {
		return err
	}

	imgName, err := findOrBuildDockerImage(ctx, config.Path, gitRoot, config.Model, config.ModelURL, config.ModelAPIKey, config.ForceRebuild, config.Verbose)
	if err != nil {
		return err
	}

	linuxSketchBin := config.SketchBinaryLinux
	if linuxSketchBin == "" {
		linuxSketchBin, err = buildLinuxSketchBin(ctx)
		if err != nil {
			return err
		}
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

	// Get the current host git commit
	var commit string
	if out, err := combinedOutput(ctx, "git", "rev-parse", "HEAD"); err != nil {
		return fmt.Errorf("git rev-parse HEAD: %w", err)
	} else {
		commit = strings.TrimSpace(string(out))
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
	config.Commit = commit

	// Create the sketch container
	if err := createDockerContainer(ctx, cntrName, hostPort, relPath, imgName, config); err != nil {
		return fmt.Errorf("failed to create docker container: %w", err)
	}

	// Copy the sketch linux binary into the container
	if out, err := combinedOutput(ctx, "docker", "cp", linuxSketchBin, cntrName+":/bin/sketch"); err != nil {
		return fmt.Errorf("docker cp: %s, %w", out, err)
	}

	// Make sure that the webui is built so we can copy the results to the container.
	_, err = webui.Build()
	if err != nil {
		return fmt.Errorf("failed to build webui: %w", err)
	}

	webuiZipPath, err := webui.ZipPath()
	if err != nil {
		return err
	}
	if out, err := combinedOutput(ctx, "docker", "cp", webuiZipPath, cntrName+":/root/.cache/sketch/webui/"+filepath.Base(webuiZipPath)); err != nil {
		return fmt.Errorf("docker cp: %s, %w", out, err)
	}

	fmt.Printf("📦 running in container %s\n", cntrName)

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

	cst, err := NewSSHTheater(cntrName, sshHost, sshPort)
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

	// Start automatic port tunneling if SSH is available
	if sshAvailable {
		go func() {
			containerURL := "http://" + localAddr
			tunnelManager := NewTunnelManager(containerURL, cntrName, 10) // Allow up to 10 concurrent tunnels
			tunnelManager.Start(ctx)
			slog.InfoContext(ctx, "Started automatic port tunnel manager", "container", cntrName)
		}()
	}

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
	if relPath != "." {
		cmdArgs = append(cmdArgs, "-w", "/app/"+relPath)
	}
	// colima does this by default, but Linux docker seems to need this set explicitly
	cmdArgs = append(cmdArgs, "--add-host", "host.docker.internal:host-gateway")

	// Add volume mounts if specified
	for _, mount := range config.Mounts {
		if mount != "" {
			cmdArgs = append(cmdArgs, "-v", mount)
		}
	}
	cmdArgs = append(
		cmdArgs,
		imgName,
		"/bin/sketch",
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
	)
	if config.Model != "" {
		cmdArgs = append(cmdArgs, "-model="+config.Model)
	}
	if config.GitRemoteUrl != "" {
		cmdArgs = append(cmdArgs, "-git-remote-url="+config.GitRemoteUrl)
		if config.Commit == "" {
			panic("Commit should have been set when GitRemoteUrl was set")
		}
		cmdArgs = append(cmdArgs, "-commit="+config.Commit)
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

func buildLinuxSketchBin(ctx context.Context) (string, error) {
	// Detect if race detector is enabled and use a different cache path
	raceEnabled := RaceEnabled()
	cacheSuffix := ""
	if raceEnabled {
		cacheSuffix = "-race"
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	linuxGopath := filepath.Join(homeDir, ".cache", "sketch", "linuxgo"+cacheSuffix)
	if err := os.MkdirAll(linuxGopath, 0o777); err != nil {
		return "", err
	}

	// When race detector is enabled, use Docker to build the Linux binary
	if raceEnabled {
		return buildLinuxSketchBinWithDocker(ctx, linuxGopath)
	}

	// Standard non-race build using cross-compilation
	// Change to directory containing dockerimg.go for module detection
	_, codeFile, _, _ := runtime.Caller(0)
	codeDir := filepath.Dir(codeFile)
	if currentDir, err := os.Getwd(); err != nil {
		slog.WarnContext(ctx, "could not get current directory", "err", err)
	} else {
		if err := os.Chdir(codeDir); err != nil {
			slog.WarnContext(ctx, "could not change to code directory for module check", "err", err)
		} else {
			defer func() {
				_ = os.Chdir(currentDir)
			}()
		}
	}

	verToInstall := "@latest"
	if out, err := exec.Command("go", "list", "-m").CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to run go list -m: %s: %v", out, err)
	} else {
		if strings.TrimSpace(string(out)) == "sketch.dev" {
			slog.DebugContext(ctx, "built linux agent from currently checked out module")
			verToInstall = ""
		}
	}

	start := time.Now()
	args := []string{"install"}
	args = append(args, "sketch.dev/cmd/sketch"+verToInstall)

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Env = append(
		os.Environ(),
		"GOOS=linux",
		"CGO_ENABLED=0",
		"GOTOOLCHAIN=auto",
		"GOPATH="+linuxGopath,
		"GOBIN=",
	)

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "go", slog.Duration("elapsed", time.Since(start)), slog.String("err", err.Error()), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
		return "", fmt.Errorf("failed to build linux sketch binary: %s: %w", out, err)
	} else {
		slog.DebugContext(ctx, "go", slog.Duration("elapsed", time.Since(start)), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	}

	if runtime.GOOS != "linux" {
		return filepath.Join(linuxGopath, "bin", "linux_"+runtime.GOARCH, "sketch"), nil
	}
	// If we are already on Linux, there's no extra platform name in the path
	return filepath.Join(linuxGopath, "bin", "sketch"), nil
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

func findOrBuildDockerImage(ctx context.Context, cwd, gitRoot, model, modelURL, modelAPIKey string, forceRebuild, verbose bool) (imgName string, err error) {
	h := sha256.Sum256([]byte(gitRoot))
	imgName = "sketch-" + hex.EncodeToString(h[:6])

	var curImgInitFilesHash string
	if out, err := combinedOutput(ctx, "docker", "inspect", "--format", "{{json .Config.Labels}}", imgName); err != nil {
		if strings.Contains(string(out), "No such object") {
			// Image does not exist, continue and build it.
			curImgInitFilesHash = ""
		} else {
			return "", fmt.Errorf("docker inspect failed: %s, %v", out, err)
		}
	} else {
		m := map[string]string{}
		if err := json.Unmarshal(bytes.TrimSpace(out), &m); err != nil {
			return "", fmt.Errorf("docker inspect output unparsable: %s, %v", out, err)
		}
		curImgInitFilesHash = m["sketch_context"]
	}

	candidates, err := findRepoDockerfiles(cwd, gitRoot)
	if err != nil {
		return "", fmt.Errorf("find dockerfile: %w", err)
	}

	var initFiles map[string]string
	var dockerfilePath string
	var generatedDockerfile string

	// Prioritize Dockerfile.sketch over Dockerfile, then fall back to generated dockerfile
	if len(candidates) > 0 {
		dockerfilePath = prioritizeDockerfiles(candidates)
		contents, err := os.ReadFile(dockerfilePath)
		if err != nil {
			return "", err
		}
		fmt.Printf("using %s as dev env\n", dockerfilePath)
		if hashInitFiles(map[string]string{dockerfilePath: string(contents)}) == curImgInitFilesHash && !forceRebuild {
			return imgName, nil
		}
	} else {
		initFiles, err = readInitFiles(os.DirFS(gitRoot))
		if err != nil {
			return "", err
		}
		subPathWorkingDir, err := filepath.Rel(gitRoot, cwd)
		if err != nil {
			return "", err
		}
		initFileHash := hashInitFiles(initFiles)
		if curImgInitFilesHash == initFileHash && !forceRebuild {
			return imgName, nil
		}

		if model == "gemini" {
			if strings.HasSuffix(modelURL, "/gemmsgs") {
				// Horrible hack! Switch back to anthropic for container building.
				// We can do this because we are talking to skaband and know the address.
				modelURL = strings.Replace(modelURL, "/gemmsgs", "/antmsgs", 1)
			} else {
				return "", fmt.Errorf("building docker image with gemini model is not supported yet; start with -model=anthropic first then use gemini")
			}
		}

		start := time.Now()
		srv := &ant.Service{
			URL:    modelURL,
			APIKey: modelAPIKey,
			HTTPC:  http.DefaultClient,
		}
		generatedDockerfile, err = createDockerfile(ctx, srv, initFiles, subPathWorkingDir, verbose)
		if err != nil {
			return "", fmt.Errorf("create dockerfile: %w", err)
		}
		// Create a unique temporary directory for the Dockerfile
		tmpDir, err := os.MkdirTemp("", "sketch-docker-*")
		if err != nil {
			return "", fmt.Errorf("failed to create temporary directory: %w", err)
		}
		dockerfilePath = filepath.Join(tmpDir, tmpSketchDockerfile)
		if err := os.WriteFile(dockerfilePath, []byte(generatedDockerfile), 0o666); err != nil {
			return "", err
		}
		// Remove the temporary directory and all contents when done
		defer os.RemoveAll(tmpDir)

		if verbose {
			fmt.Fprintf(os.Stderr, "generated Dockerfile in %s:\n\t%s\n\n", time.Since(start).Round(time.Millisecond), strings.Replace(generatedDockerfile, "\n", "\n\t", -1))
		}
	}

	var gitUserEmail, gitUserName string
	if out, err := combinedOutput(ctx, "git", "config", "--get", "user.email"); err != nil {
		return "", fmt.Errorf("git config: %s: %v", out, err)
	} else {
		gitUserEmail = strings.TrimSpace(string(out))
	}
	if out, err := combinedOutput(ctx, "git", "config", "--get", "user.name"); err != nil {
		return "", fmt.Errorf("git config: %s: %v", out, err)
	} else {
		gitUserName = strings.TrimSpace(string(out))
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx,
		"docker", "build",
		"-t", imgName,
		"-f", dockerfilePath,
		"--build-arg", "GIT_USER_EMAIL="+gitUserEmail,
		"--build-arg", "GIT_USER_NAME="+gitUserName,
		".",
	)
	cmd.Dir = gitRoot
	// We print the docker build output whether or not the user
	// has selected --verbose. Building an image takes a while
	// and this gives good context.
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("🏗️  building docker image %s... (use -verbose to see build output)\n", imgName)

	err = run(ctx, "docker build", cmd)
	if err != nil {
		var msg string
		if generatedDockerfile != "" {
			if !verbose {
				fmt.Fprintf(os.Stderr, "Generated Dockerfile:\n\t%s\n\n", strings.Replace(generatedDockerfile, "\n", "\n\t", -1))
			}
			msg = fmt.Sprintf("\n\nThe generated Dockerfile failed to build.\nYou can override it by committing a Dockerfile to your project.")
		}
		return "", fmt.Errorf("docker build failed: %v%s", err, msg)
	}
	fmt.Printf("built docker image %s in %s\n", imgName, time.Since(start).Round(time.Millisecond))
	return imgName, nil
}

func findRepoDockerfiles(cwd, gitRoot string) ([]string, error) {
	files, err := findDirDockerfiles(cwd)
	if err != nil {
		return nil, err
	}
	if len(files) > 0 {
		return files, nil
	}

	path := cwd
	for path != gitRoot {
		path = filepath.Dir(path)
		files, err := findDirDockerfiles(path)
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			return files, nil
		}
	}
	return files, nil
}

// prioritizeDockerfiles returns the highest priority dockerfile from a list of candidates.
// Priority order: Dockerfile.sketch > Dockerfile > other Dockerfile.*
func prioritizeDockerfiles(candidates []string) string {
	if len(candidates) == 0 {
		return ""
	}
	if len(candidates) == 1 {
		return candidates[0]
	}

	// Look for Dockerfile.sketch first (case insensitive)
	for _, candidate := range candidates {
		basename := strings.ToLower(filepath.Base(candidate))
		if basename == "dockerfile.sketch" {
			return candidate
		}
	}

	// Look for Dockerfile second (case insensitive)
	for _, candidate := range candidates {
		basename := strings.ToLower(filepath.Base(candidate))
		if basename == "dockerfile" {
			return candidate
		}
	}

	// Return first remaining candidate
	return candidates[0]
}

// findDirDockerfiles finds all "Dockerfile*" files in a directory.
func findDirDockerfiles(root string) (res []string, err error) {
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && root != path {
			return filepath.SkipDir
		}
		name := strings.ToLower(info.Name())
		if name == "dockerfile" || strings.HasPrefix(name, "dockerfile.") {
			res = append(res, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

func checkForEmptyGitRepo(ctx context.Context, path string) error {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "-q", "--verify", "HEAD")
	cmd.Dir = path
	_, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sketch needs to run from within a git repo with at least one commit.\nRun: %s",
			"git commit --allow-empty -m 'initial commit'")
	}
	return nil
}

func findGitRoot(ctx context.Context, path string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-common-dir")
	cmd.Dir = path
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "not a git repository") {
			return "", fmt.Errorf(`sketch needs to run from within a git repo, but %s is not part of a git repo.
Consider one of the following options:
	- cd to a different dir that is already part of a git repo first, or
	- to create a new git repo from this directory (%s), run this command:

		git init . && git commit --allow-empty -m "initial commit"

and try running sketch again.
`, path, path)
		}
		return "", fmt.Errorf("git rev-parse --git-common-dir: %s: %w", out, err)
	}
	gitDir := strings.TrimSpace(string(out)) // location of .git dir, often as a relative path
	absGitDir := filepath.Join(path, gitDir)
	return filepath.Dir(absGitDir), err
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

// buildLinuxSketchBinWithDocker builds the Linux sketch binary using Docker when race detector is enabled.
// This avoids cross-compilation issues with CGO which is required for the race detector.
// Mounts host Go module cache and build cache for faster subsequent builds.
func buildLinuxSketchBinWithDocker(ctx context.Context, linuxGopath string) (string, error) {
	// Find the git repo root
	currentDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not get current directory: %w", err)
	}

	gitRoot, err := findGitRoot(ctx, currentDir)
	if err != nil {
		return "", fmt.Errorf("could not find git root, cannot build with race detector outside a git repo: %w", err)
	}

	// Get host Go cache directories to mount for faster builds
	goCacheDir, err := getHostGoCacheDir(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get host GOCACHE: %w", err)
	}
	goModCacheDir, err := getHostGoModCacheDir(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get host GOMODCACHE: %w", err)
	}

	slog.DebugContext(ctx, "building Linux sketch binary with race detector using Docker", "git_root", gitRoot, "gocache", goCacheDir, "gomodcache", goModCacheDir)

	// Use the published Docker image tag
	imageTag := dockerfileBaseHash()
	imgName := fmt.Sprintf("%s:%s", dockerImgName, imageTag)

	// Create destination directory for the binary
	destPath := filepath.Join(linuxGopath, "bin")
	if err := os.MkdirAll(destPath, 0o777); err != nil {
		return "", fmt.Errorf("failed to create destination directory: %w", err)
	}
	destFile := filepath.Join(destPath, "sketch")

	// Create a unique container name
	containerID := fmt.Sprintf("sketch-race-build-%d", time.Now().UnixNano())

	// Run a container with the repo mounted and Go caches for faster builds
	start := time.Now()
	slog.DebugContext(ctx, "running Docker container to build sketch with race detector")

	// Use explicit output path for clarity
	runArgs := []string{
		"run",
		"--name", containerID,
		"-v", gitRoot + ":/app",
		"-v", goCacheDir + ":/root/.cache/go-build",
		"-v", goModCacheDir + ":/go/pkg/mod",
		"-w", "/app",
		imgName,
		"sh", "-c", "cd /app && mkdir -p /tmp/sketch-out && go build -buildvcs=false -race -o /tmp/sketch-out/sketch sketch.dev/cmd/sketch",
	}

	out, err := combinedOutput(ctx, "docker", runArgs...)
	if err != nil {
		// Print the output to help with debugging
		slog.ErrorContext(ctx, "docker run for race build failed",
			slog.String("output", string(out)),
			slog.String("error", err.Error()))
		return "", fmt.Errorf("docker run failed: %s: %w", out, err)
	}

	slog.DebugContext(ctx, "built sketch with race detector in Docker", "elapsed", time.Since(start))

	// Copy the binary from the container using the explicit path
	out, err = combinedOutput(ctx, "docker", "cp", containerID+":/tmp/sketch-out/sketch", destFile)
	if err != nil {
		return "", fmt.Errorf("docker cp failed: %s: %w", out, err)
	}

	// Clean up the container
	if out, err := combinedOutput(ctx, "docker", "rm", containerID); err != nil {
		slog.WarnContext(ctx, "failed to remove container", "container", containerID, "error", err, "output", string(out))
	}

	// Make the binary executable
	if err := os.Chmod(destFile, 0o755); err != nil {
		return "", fmt.Errorf("failed to make binary executable: %w", err)
	}

	return destFile, nil
}

// getHostGoCacheDir returns the host's GOCACHE directory
func getHostGoCacheDir(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", "GOCACHE").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get GOCACHE: %s: %w", out, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// getHostGoModCacheDir returns the host's GOMODCACHE directory
func getHostGoModCacheDir(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "go", "env", "GOMODCACHE").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("failed to get GOMODCACHE: %s: %w", out, err)
	}
	return strings.TrimSpace(string(out)), nil
}
