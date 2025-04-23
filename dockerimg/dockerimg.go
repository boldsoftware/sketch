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
	"time"

	"sketch.dev/loop/server"
	"sketch.dev/loop/webui"
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

	// AntURL is the URL of the LLM service.
	AntURL string

	// AntAPIKey is the API key for LLM service.
	AntAPIKey string

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

	// Public keys authorized to connect to the container's ssh server
	SSHAuthorizedKeys []byte

	// Private key used to identify the container's ssh server
	SSHServerIdentity []byte

	// Host information to pass to the container
	HostHostname   string
	HostOS         string
	HostWorkingDir string
}

// LaunchContainer creates a docker container for a project, installs sketch and opens a connection to it.
// It writes status to stdout.
func LaunchContainer(ctx context.Context, stdout, stderr io.Writer, config ContainerConfig) error {
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

	imgName, err := findOrBuildDockerImage(ctx, stdout, stderr, config.Path, gitRoot, config.AntURL, config.AntAPIKey, config.ForceRebuild)
	if err != nil {
		return err
	}

	linuxSketchBin := config.SketchBinaryLinux
	if linuxSketchBin == "" {
		linuxSketchBin, err = buildLinuxSketchBin(ctx, config.Path)
		if err != nil {
			return err
		}
		defer os.Remove(linuxSketchBin)
	}

	cntrName := imgName + "-" + config.SessionID
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

	// Create the sketch container
	if err := createDockerContainer(ctx, cntrName, hostPort, relPath, imgName, config); err != nil {
		return err
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

	fmt.Printf("starting container %s\ncommits made by the agent will be pushed to \033[1msketch/*\033[0m\n", cntrName)

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
		logLines := strings.Split(string(out), "\n")
		for _, logLine := range logLines {
			if !strings.HasPrefix(logLine, "structured logs:") {
				continue
			}
			logFile := strings.TrimSpace(strings.TrimPrefix(logLine, "structured logs:"))
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
	localAddr, err := getContainerPort(ctx, cntrName)
	if err != nil {
		return appendInternalErr(err)
	}

	// Tell the sketch container which git server port and commit to initialize with.
	go func() {
		// TODO: Why is this called in a goroutine? I have found that when I pull this out
		// of the goroutine and call it inline, then the terminal UI clears itself and all
		// the scrollback (which is not good, but also not fatal).  I can't see why it does this
		// though, since none of the calls in postContainerInitConfig obviously write to stdout
		// or stderr.
		if err := postContainerInitConfig(ctx, localAddr, commit, gitSrv.gitPort, gitSrv.pass, config.SSHServerIdentity, config.SSHAuthorizedKeys); err != nil {
			slog.ErrorContext(ctx, "LaunchContainer.postContainerInitConfig", slog.String("err", err.Error()))
			errCh <- appendInternalErr(err)
		}
	}()

	if config.OpenBrowser {
		OpenBrowser(ctx, "http://"+localAddr)
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
	// Really only needed for the "go build" command for the linux sketch binary
	cmd.Env = append(os.Environ(), "GOOS=linux", "CGO_ENABLED=0")
	start := time.Now()

	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, cmdName, slog.Duration("elapsed", time.Now().Sub(start)), slog.String("err", err.Error()), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	} else {
		slog.DebugContext(ctx, cmdName, slog.Duration("elapsed", time.Now().Sub(start)), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	}
	return out, err
}

func run(ctx context.Context, cmdName string, cmd *exec.Cmd) error {
	start := time.Now()
	err := cmd.Run()
	if err != nil {
		slog.ErrorContext(ctx, cmdName, slog.Duration("elapsed", time.Now().Sub(start)), slog.String("err", err.Error()), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	} else {
		slog.DebugContext(ctx, cmdName, slog.Duration("elapsed", time.Now().Sub(start)), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	}
	return err
}

type gitServer struct {
	gitLn   net.Listener
	gitPort string
	srv     *http.Server
	pass    string
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

	srv := http.Server{
		Handler: &gitHTTP{gitRepoRoot: gitRoot, pass: []byte(ret.pass)},
	}
	ret.srv = &srv

	_, gitPort, err := net.SplitHostPort(gitLn.Addr().String())
	if err != nil {
		return nil, fmt.Errorf("git port: %w", err)
	}
	ret.gitPort = gitPort
	return ret, nil
}

func createDockerContainer(ctx context.Context, cntrName, hostPort, relPath, imgName string, config ContainerConfig) error {
	//, config.SessionID, config.GitUsername, config.GitEmail, config.SkabandAddr
	// sessionID, gitUsername, gitEmail, skabandAddr string
	cmdArgs := []string{
		"create",
		"-it",
		"--name", cntrName,
		"-p", hostPort + ":80", // forward container port 80 to a host port
		"-e", "ANTHROPIC_API_KEY=" + config.AntAPIKey,
	}
	if config.AntURL != "" {
		cmdArgs = append(cmdArgs, "-e", "ANT_URL="+config.AntURL)
	}
	if config.SketchPubKey != "" {
		cmdArgs = append(cmdArgs, "-e", "SKETCH_PUB_KEY="+config.SketchPubKey)
	}
	if config.SSHPort != 0 {
		cmdArgs = append(cmdArgs, "-p", fmt.Sprintf("%d:2022", config.SSHPort)) // forward container ssh port to host ssh port
	}
	if relPath != "." {
		cmdArgs = append(cmdArgs, "-w", "/app/"+relPath)
	}
	// colima does this by default, but Linux docker seems to need this set explicitly
	cmdArgs = append(cmdArgs, "--add-host", "host.docker.internal:host-gateway")
	cmdArgs = append(
		cmdArgs,
		imgName,
		"/bin/sketch",
		"-unsafe",
		"-addr=:80",
		"-session-id="+config.SessionID,
		"-git-username="+config.GitUsername,
		"-git-email="+config.GitEmail,
		"-host-hostname="+config.HostHostname,
		"-host-os="+config.HostOS,
		"-host-working-dir="+config.HostWorkingDir,
	)
	if config.SkabandAddr != "" {
		cmdArgs = append(cmdArgs, "-skaband-addr="+config.SkabandAddr)
	}
	if out, err := combinedOutput(ctx, "docker", cmdArgs...); err != nil {
		return fmt.Errorf("docker create: %s, %w", out, err)
	}
	return nil
}

func buildLinuxSketchBin(ctx context.Context, path string) (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	linuxGopath := filepath.Join(homeDir, ".cache", "sketch", "linuxgo")
	if err := os.MkdirAll(linuxGopath, 0o777); err != nil {
		return "", err
	}

	verToInstall := "@latest"
	if out, err := exec.Command("go", "list", "-m").CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to run go list -m: %s: %v", out, err)
	} else {
		if strings.TrimSpace(string(out)) == "sketch.dev" {
			fmt.Printf("building linux agent from currently checked out module\n")
			verToInstall = ""
		}
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, "go", "install", "sketch.dev/cmd/sketch"+verToInstall)
	cmd.Env = append(
		os.Environ(),
		"GOOS=linux",
		"CGO_ENABLED=0",
		"GOTOOLCHAIN=auto",
		"GOPATH="+linuxGopath,
		"GOBIN=",
	)

	fmt.Printf("building linux agent binary...\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		slog.ErrorContext(ctx, "go", slog.Duration("elapsed", time.Now().Sub(start)), slog.String("err", err.Error()), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
		return "", fmt.Errorf("failed to build linux sketch binary: %s: %w", out, err)
	} else {
		slog.DebugContext(ctx, "go", slog.Duration("elapsed", time.Now().Sub(start)), slog.String("path", cmd.Path), slog.String("args", fmt.Sprintf("%v", skribe.Redact(cmd.Args))))
	}

	var src string
	if runtime.GOOS != "linux" {
		src = filepath.Join(linuxGopath, "bin", "linux_"+runtime.GOARCH, "sketch")
	} else {
		// If we are already on Linux, there's no extra platform name in the path
		src = filepath.Join(linuxGopath, "bin", "sketch")
	}

	dst := filepath.Join(path, "tmp-sketch-binary-linux")
	if err := moveFile(src, dst); err != nil {
		return "", err
	}

	fmt.Printf("built linux agent binary in %s\n", time.Since(start).Round(100*time.Millisecond))

	return dst, nil
}

func getContainerPort(ctx context.Context, cntrName string) (string, error) {
	localAddr := ""
	if out, err := combinedOutput(ctx, "docker", "port", cntrName, "80"); err != nil {
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
func postContainerInitConfig(ctx context.Context, localAddr, commit, gitPort, gitPass string, sshServerIdentity, sshAuthorizedKeys []byte) error {
	localURL := "http://" + localAddr

	initMsg, err := json.Marshal(
		server.InitRequest{
			Commit:            commit,
			GitRemoteAddr:     fmt.Sprintf("http://sketch:%s@host.docker.internal:%s/.git", gitPass, gitPort),
			HostAddr:          localAddr,
			SSHAuthorizedKeys: sshAuthorizedKeys,
			SSHServerIdentity: sshServerIdentity,
		})
	if err != nil {
		return fmt.Errorf("init msg: %w", err)
	}

	slog.DebugContext(ctx, "/init POST", slog.String("initMsg", string(initMsg)))

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
			// In addition to "connection refused", we also occasionally see "EOF" errors that can succeed on retries.
			if i < 100 && (strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "EOF")) {
				slog.DebugContext(ctx, "postContainerInitConfig retrying", slog.Int("retry", i), slog.String("err", err.Error()))
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

func findOrBuildDockerImage(ctx context.Context, stdout, stderr io.Writer, cwd, gitRoot, antURL, antAPIKey string, forceRebuild bool) (imgName string, err error) {
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

	// TODO: prefer a "Dockerfile.sketch" so users can tailor any env to this tool.
	if len(candidates) == 1 && strings.ToLower(filepath.Base(candidates[0])) == "dockerfile" {
		dockerfilePath = candidates[0]
		contents, err := os.ReadFile(dockerfilePath)
		if err != nil {
			return "", err
		}
		fmt.Printf("using %s as dev env\n", candidates[0])
		if hashInitFiles(map[string]string{dockerfilePath: string(contents)}) == curImgInitFilesHash && !forceRebuild {
			fmt.Printf("using existing docker image %s\n", imgName)
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
			fmt.Printf("using existing docker image %s\n", imgName)
			return imgName, nil
		}

		start := time.Now()
		dockerfile, err := createDockerfile(ctx, http.DefaultClient, antURL, antAPIKey, initFiles, subPathWorkingDir)
		if err != nil {
			return "", fmt.Errorf("create dockerfile: %w", err)
		}
		dockerfilePath = filepath.Join(cwd, "tmp-sketch-dockerfile")
		if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0o666); err != nil {
			return "", err
		}
		defer os.Remove(dockerfilePath)

		fmt.Fprintf(stderr, "generated Dockerfile in %s:\n\t%s\n\n", time.Since(start).Round(time.Millisecond), strings.Replace(dockerfile, "\n", "\n\t", -1))
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
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	fmt.Printf("building docker image %s...\n", imgName)

	err = run(ctx, "docker build", cmd)
	if err != nil {
		return "", fmt.Errorf("docker build failed: %v", err)
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

func OpenBrowser(ctx context.Context, url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "cmd", "/c", "start", url)
	default: // Linux and other Unix-like systems
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	if b, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to open browser: %v: %s\n", err, b)
	}
}

// moveFile is like Python's shutil.move, in that it tries a rename, and, if that fails,
// copies and deletes
func moveFile(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}

	stat, err := os.Stat(src)
	if err != nil {
		return err
	}

	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	if err != nil {
		return err
	}

	sourceFile.Close()
	destFile.Close()

	os.Chmod(dst, stat.Mode())

	return os.Remove(src)
}
