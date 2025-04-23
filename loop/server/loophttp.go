// Package server provides HTTP server functionality for the sketch loop.
package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"sketch.dev/loop/server/gzhandler"

	"github.com/creack/pty"
	"sketch.dev/ant"
	"sketch.dev/loop"
	"sketch.dev/loop/webui"
)

// terminalSession represents a terminal session with its PTY and the event channel
type terminalSession struct {
	pty                *os.File
	eventsClients      map[chan []byte]bool
	lastEventClientID  int
	eventsClientsMutex sync.Mutex
	cmd                *exec.Cmd
}

// TerminalMessage represents a message sent from the client for terminal resize events
type TerminalMessage struct {
	Type string `json:"type"`
	Cols uint16 `json:"cols"`
	Rows uint16 `json:"rows"`
}

// TerminalResponse represents the response for a new terminal creation
type TerminalResponse struct {
	SessionID string `json:"sessionId"`
}

type State struct {
	MessageCount  int                  `json:"message_count"`
	TotalUsage    *ant.CumulativeUsage `json:"total_usage,omitempty"`
	InitialCommit string               `json:"initial_commit"`
	Title         string               `json:"title"`
	Hostname      string               `json:"hostname"`    // deprecated
	WorkingDir    string               `json:"working_dir"` // deprecated
	OS            string               `json:"os"`          // deprecated
	GitOrigin     string               `json:"git_origin,omitempty"`

	HostHostname      string `json:"host_hostname,omitempty"`
	RuntimeHostname   string `json:"runtime_hostname,omitempty"`
	HostOS            string `json:"host_os,omitempty"`
	RuntimeOS         string `json:"runtime_os,omitempty"`
	HostWorkingDir    string `json:"host_working_dir,omitempty"`
	RuntimeWorkingDir string `json:"runtime_working_dir,omitempty"`
}

type InitRequest struct {
	HostAddr          string `json:"host_addr"`
	GitRemoteAddr     string `json:"git_remote_addr"`
	Commit            string `json:"commit"`
	SSHAuthorizedKeys []byte `json:"ssh_authorized_keys"`
	SSHServerIdentity []byte `json:"ssh_server_identity"`
}

// Server serves sketch HTTP. Server implements http.Handler.
type Server struct {
	mux      *http.ServeMux
	agent    loop.CodingAgent
	hostname string
	logFile  *os.File
	// Mutex to protect terminalSessions
	ptyMutex         sync.Mutex
	terminalSessions map[string]*terminalSession
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// New creates a new HTTP server.
func New(agent loop.CodingAgent, logFile *os.File) (*Server, error) {
	s := &Server{
		mux:              http.NewServeMux(),
		agent:            agent,
		hostname:         getHostname(),
		logFile:          logFile,
		terminalSessions: make(map[string]*terminalSession),
	}

	webBundle, err := webui.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build web bundle, did you run 'go generate sketch.dev/loop/...'?: %w", err)
	}

	s.mux.HandleFunc("/diff", func(w http.ResponseWriter, r *http.Request) {
		// Check if a specific commit hash was requested
		commit := r.URL.Query().Get("commit")

		// Get the diff, optionally for a specific commit
		var diff string
		var err error
		if commit != "" {
			// Validate the commit hash format
			if !isValidGitSHA(commit) {
				http.Error(w, fmt.Sprintf("Invalid git commit SHA format: %s", commit), http.StatusBadRequest)
				return
			}

			diff, err = agent.Diff(&commit)
		} else {
			diff, err = agent.Diff(nil)
		}

		if err != nil {
			http.Error(w, fmt.Sprintf("Error generating diff: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(diff))
	})

	// Handler for initialization called by host sketch binary when inside docker.
	s.mux.HandleFunc("/init", func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				slog.ErrorContext(r.Context(), "/init panic", slog.Any("recovered_err", err))

				// Return an error response to the client
				http.Error(w, fmt.Sprintf("panic: %v\n", err), http.StatusInternalServerError)
			}
		}()

		if r.Method != "POST" {
			http.Error(w, "POST required", http.StatusBadRequest)
			return
		}

		body, err := io.ReadAll(r.Body)
		r.Body.Close()
		if err != nil {
			http.Error(w, "failed to read request body: "+err.Error(), http.StatusBadRequest)
			return
		}

		m := &InitRequest{}
		if err := json.Unmarshal(body, m); err != nil {
			http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Start the SSH server if the init request included ssh keys.
		if len(m.SSHAuthorizedKeys) > 0 && len(m.SSHServerIdentity) > 0 {
			go func() {
				ctx := context.Background()
				if err := s.ServeSSH(ctx, m.SSHServerIdentity, m.SSHAuthorizedKeys); err != nil {
					slog.ErrorContext(r.Context(), "/init ServeSSH", slog.String("err", err.Error()))
				}
			}()
		}

		ini := loop.AgentInit{
			WorkingDir:    "/app",
			InDocker:      true,
			Commit:        m.Commit,
			GitRemoteAddr: m.GitRemoteAddr,
			HostAddr:      m.HostAddr,
		}
		if err := agent.Init(ini); err != nil {
			http.Error(w, "init failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "{}\n")
	})

	// Handler for /messages?start=N&end=M (start/end are optional)
	s.mux.HandleFunc("/messages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Extract query parameters for range
		var start, end int
		var err error

		currentCount := agent.MessageCount()

		startParam := r.URL.Query().Get("start")
		if startParam != "" {
			start, err = strconv.Atoi(startParam)
			if err != nil {
				http.Error(w, "Invalid 'start' parameter", http.StatusBadRequest)
				return
			}
		}

		endParam := r.URL.Query().Get("end")
		if endParam != "" {
			end, err = strconv.Atoi(endParam)
			if err != nil {
				http.Error(w, "Invalid 'end' parameter", http.StatusBadRequest)
				return
			}
		} else {
			end = currentCount
		}

		if start < 0 || start > end || end > currentCount {
			http.Error(w, fmt.Sprintf("Invalid range: start %d end %d currentCount %d", start, end, currentCount), http.StatusBadRequest)
			return
		}

		start = max(0, start)
		end = min(agent.MessageCount(), end)
		messages := agent.Messages(start, end)

		// Create a JSON encoder with indentation for pretty-printing
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ") // Two spaces for each indentation level

		err = encoder.Encode(messages)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	// Handler for /logs - displays the contents of the log file
	s.mux.HandleFunc("/logs", func(w http.ResponseWriter, r *http.Request) {
		if s.logFile == nil {
			http.Error(w, "log file not set", http.StatusNotFound)
			return
		}
		logContents, err := os.ReadFile(s.logFile.Name())
		if err != nil {
			http.Error(w, "error reading log file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, "<!DOCTYPE html>\n<html>\n<head>\n<title>Sketchy Log File</title>\n</head>\n<body>\n")
		fmt.Fprintf(w, "<pre>%s</pre>\n", html.EscapeString(string(logContents)))
		fmt.Fprintf(w, "</body>\n</html>")
	})

	// Handler for /download - downloads both messages and status as a JSON file
	s.mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		// Set headers for file download
		w.Header().Set("Content-Type", "application/octet-stream")

		// Generate filename with format: sketch-YYYYMMDD-HHMMSS.json
		timestamp := time.Now().Format("20060102-150405")
		filename := fmt.Sprintf("sketch-%s.json", timestamp)

		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))

		// Get all messages
		messageCount := agent.MessageCount()
		messages := agent.Messages(0, messageCount)

		// Get status information (usage and other metadata)
		totalUsage := agent.TotalUsage()
		hostname := getHostname()
		workingDir := getWorkingDir()

		// Create a combined structure with all information
		downloadData := struct {
			Messages     []loop.AgentMessage `json:"messages"`
			MessageCount int                 `json:"message_count"`
			TotalUsage   ant.CumulativeUsage `json:"total_usage"`
			Hostname     string              `json:"hostname"`
			WorkingDir   string              `json:"working_dir"`
			DownloadTime string              `json:"download_time"`
		}{
			Messages:     messages,
			MessageCount: messageCount,
			TotalUsage:   totalUsage,
			Hostname:     hostname,
			WorkingDir:   workingDir,
			DownloadTime: time.Now().Format(time.RFC3339),
		}

		// Marshal the JSON with indentation for better readability
		jsonData, err := json.MarshalIndent(downloadData, "", "  ")
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Write(jsonData)
	})

	// The latter doesn't return until the number of messages has changed (from seen
	// or from when this was called.)
	s.mux.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		pollParam := r.URL.Query().Get("poll")
		seenParam := r.URL.Query().Get("seen")

		// Get the client's current message count (if provided)
		clientMessageCount := -1
		var err error
		if seenParam != "" {
			clientMessageCount, err = strconv.Atoi(seenParam)
			if err != nil {
				http.Error(w, "Invalid 'seen' parameter", http.StatusBadRequest)
				return
			}
		}

		serverMessageCount := agent.MessageCount()

		// Let lazy clients not have to specify this.
		if clientMessageCount == -1 {
			clientMessageCount = serverMessageCount
		}

		if pollParam == "true" {
			ch := make(chan string)
			go func() {
				// This is your blocking operation
				agent.WaitForMessageCount(r.Context(), clientMessageCount)
				close(ch)
			}()
			select {
			case <-r.Context().Done():
				slog.DebugContext(r.Context(), "abandoned poll request")
				return
			case <-time.After(90 * time.Second):
				// Let the user call /state again to get the latest to limit how long our long polls hang out.
				slog.DebugContext(r.Context(), "longish poll request")
				break
			case <-ch:
				break
			}
		}

		serverMessageCount = agent.MessageCount()
		totalUsage := agent.TotalUsage()

		w.Header().Set("Content-Type", "application/json")

		state := State{
			MessageCount:      serverMessageCount,
			TotalUsage:        &totalUsage,
			Hostname:          s.hostname,
			WorkingDir:        getWorkingDir(),
			InitialCommit:     agent.InitialCommit(),
			Title:             agent.Title(),
			OS:                agent.OS(),
			HostHostname:      agent.HostHostname(),
			RuntimeHostname:   s.hostname,
			HostOS:            agent.HostOS(),
			RuntimeOS:         agent.OS(),
			HostWorkingDir:    agent.HostWorkingDir(),
			RuntimeWorkingDir: getWorkingDir(),
			GitOrigin:         agent.GitOrigin(),
		}

		// Create a JSON encoder with indentation for pretty-printing
		encoder := json.NewEncoder(w)
		encoder.SetIndent("", "  ") // Two spaces for each indentation level

		err = encoder.Encode(state)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
	})

	s.mux.Handle("/static/", http.StripPrefix("/static/", gzhandler.New(webBundle)))

	// Terminal WebSocket handler
	// Terminal endpoints - predefined terminals 1-9
	// TODO: The UI doesn't actually know how to use terminals 2-9!
	s.mux.HandleFunc("/terminal/events/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) < 4 {
			http.Error(w, "Invalid terminal ID", http.StatusBadRequest)
			return
		}

		sessionID := pathParts[3]
		// Validate that the terminal ID is between 1-9
		if len(sessionID) != 1 || sessionID[0] < '1' || sessionID[0] > '9' {
			http.Error(w, "Terminal ID must be between 1 and 9", http.StatusBadRequest)
			return
		}

		s.handleTerminalEvents(w, r, sessionID)
	})

	s.mux.HandleFunc("/terminal/input/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		pathParts := strings.Split(r.URL.Path, "/")
		if len(pathParts) < 4 {
			http.Error(w, "Invalid terminal ID", http.StatusBadRequest)
			return
		}
		sessionID := pathParts[3]
		s.handleTerminalInput(w, r, sessionID)
	})

	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Serve the sketch-app-shell.html file directly from the embedded filesystem
		data, err := fs.ReadFile(webBundle, "sketch-app-shell.html")
		if err != nil {
			http.Error(w, "File not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write(data)
	})

	// Handler for POST /chat
	s.mux.HandleFunc("/chat", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse the request body
		var requestBody struct {
			Message string `json:"message"`
		}

		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&requestBody); err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		if requestBody.Message == "" {
			http.Error(w, "Message cannot be empty", http.StatusBadRequest)
			return
		}

		agent.UserMessage(r.Context(), requestBody.Message)

		w.WriteHeader(http.StatusOK)
	})

	// Handler for /cancel - cancels the current inner loop in progress
	s.mux.HandleFunc("/cancel", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Parse the request body (optional)
		var requestBody struct {
			Reason     string `json:"reason"`
			ToolCallID string `json:"tool_call_id"`
		}

		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&requestBody); err != nil && err != io.EOF {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		cancelReason := "user requested cancellation"
		if requestBody.Reason != "" {
			cancelReason = requestBody.Reason
		}

		if requestBody.ToolCallID != "" {
			err := agent.CancelToolUse(requestBody.ToolCallID, fmt.Errorf("%s", cancelReason))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			// Return a success response
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"status":     "cancelled",
				"too_use_id": requestBody.ToolCallID,
				"reason":     cancelReason})
			return
		}
		// Call the CancelInnerLoop method
		agent.CancelInnerLoop(fmt.Errorf("%s", cancelReason))
		// Return a success response
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "cancelled", "reason": cancelReason})
	})

	debugMux := initDebugMux()
	s.mux.HandleFunc("/debug/", func(w http.ResponseWriter, r *http.Request) {
		debugMux.ServeHTTP(w, r)
	})

	return s, nil
}

// Utility functions
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

func getWorkingDir() string {
	wd, err := os.Getwd()
	if err != nil {
		return "unknown"
	}
	return wd
}

// createTerminalSession creates a new terminal session with the given ID
func (s *Server) createTerminalSession(sessionID string) (*terminalSession, error) {
	// Start a new shell process
	shellPath := getShellPath()
	cmd := exec.Command(shellPath)

	// Get working directory from the agent if possible
	workDir := getWorkingDir()
	cmd.Dir = workDir

	// Set up environment
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	// Start the command with a pty
	ptmx, err := pty.Start(cmd)
	if err != nil {
		slog.Error("Failed to start pty", "error", err)
		return nil, err
	}

	// Create the terminal session
	session := &terminalSession{
		pty:           ptmx,
		eventsClients: make(map[chan []byte]bool),
		cmd:           cmd,
	}

	// Start goroutine to read from pty and broadcast to all connected SSE clients
	go s.readFromPtyAndBroadcast(sessionID, session)

	return session, nil
} // handleTerminalEvents handles SSE connections for terminal output
func (s *Server) handleTerminalEvents(w http.ResponseWriter, r *http.Request, sessionID string) {
	// Check if the session exists, if not, create it
	s.ptyMutex.Lock()
	session, exists := s.terminalSessions[sessionID]

	if !exists {
		// Create a new terminal session
		var err error
		session, err = s.createTerminalSession(sessionID)
		if err != nil {
			s.ptyMutex.Unlock()
			http.Error(w, fmt.Sprintf("Failed to create terminal: %v", err), http.StatusInternalServerError)
			return
		}

		// Store the new session
		s.terminalSessions[sessionID] = session
	}
	s.ptyMutex.Unlock()

	// Set headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// Create a channel for this client
	events := make(chan []byte, 4096) // Buffer to prevent blocking

	// Register this client's channel
	session.eventsClientsMutex.Lock()
	clientID := session.lastEventClientID + 1
	session.lastEventClientID = clientID
	session.eventsClients[events] = true
	session.eventsClientsMutex.Unlock()

	// When the client disconnects, remove their channel
	defer func() {
		session.eventsClientsMutex.Lock()
		delete(session.eventsClients, events)
		close(events)
		session.eventsClientsMutex.Unlock()
	}()

	// Flush to send headers to client immediately
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	// Send events to the client as they arrive
	for {
		select {
		case <-r.Context().Done():
			return
		case data := <-events:
			// Format as SSE with base64 encoding
			fmt.Fprintf(w, "data: %s\n\n", base64.StdEncoding.EncodeToString(data))

			// Flush the data immediately
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}
	}
}

// handleTerminalInput processes input to the terminal
func (s *Server) handleTerminalInput(w http.ResponseWriter, r *http.Request, sessionID string) {
	// Check if the session exists
	s.ptyMutex.Lock()
	session, exists := s.terminalSessions[sessionID]
	s.ptyMutex.Unlock()

	if !exists {
		http.Error(w, "Terminal session not found", http.StatusNotFound)
		return
	}

	// Read the request body (terminal input or resize command)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}

	// Check if it's a resize message
	if len(body) > 0 && body[0] == '{' {
		var msg TerminalMessage
		if err := json.Unmarshal(body, &msg); err == nil && msg.Type == "resize" {
			if msg.Cols > 0 && msg.Rows > 0 {
				pty.Setsize(session.pty, &pty.Winsize{
					Cols: msg.Cols,
					Rows: msg.Rows,
				})

				// Respond with success
				w.WriteHeader(http.StatusOK)
				return
			}
		}
	}

	// Regular terminal input
	_, err = session.pty.Write(body)
	if err != nil {
		slog.Error("Failed to write to pty", "error", err)
		http.Error(w, "Failed to write to terminal", http.StatusInternalServerError)
		return
	}

	// Respond with success
	w.WriteHeader(http.StatusOK)
}

// readFromPtyAndBroadcast reads output from the PTY and broadcasts it to all connected clients
func (s *Server) readFromPtyAndBroadcast(sessionID string, session *terminalSession) {
	buf := make([]byte, 4096)
	defer func() {
		// Clean up when done
		s.ptyMutex.Lock()
		delete(s.terminalSessions, sessionID)
		s.ptyMutex.Unlock()

		// Close the PTY
		session.pty.Close()

		// Ensure process is terminated
		if session.cmd.Process != nil {
			session.cmd.Process.Signal(syscall.SIGTERM)
			time.Sleep(100 * time.Millisecond)
			session.cmd.Process.Kill()
		}

		// Close all client channels
		session.eventsClientsMutex.Lock()
		for ch := range session.eventsClients {
			delete(session.eventsClients, ch)
			close(ch)
		}
		session.eventsClientsMutex.Unlock()
	}()

	for {
		n, err := session.pty.Read(buf)
		if err != nil {
			if err != io.EOF {
				slog.Error("Failed to read from pty", "error", err)
			}
			break
		}

		// Make a copy of the data for each client
		data := make([]byte, n)
		copy(data, buf[:n])

		// Broadcast to all connected clients
		session.eventsClientsMutex.Lock()
		for ch := range session.eventsClients {
			// Try to send, but don't block if channel is full
			select {
			case ch <- data:
			default:
				// Channel is full, drop the message for this client
			}
		}
		session.eventsClientsMutex.Unlock()
	}
}

// getShellPath returns the path to the shell to use
func getShellPath() string {
	// Try to use the user's preferred shell
	shell := os.Getenv("SHELL")
	if shell != "" {
		return shell
	}

	// Default to bash on Unix-like systems
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}

	// Fall back to sh
	return "/bin/sh"
}

func initDebugMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /debug/{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html>
			<html><head><title>sketch debug</title></head><body>
			<h1>sketch debug</h1>
			<ul>
					<li><a href="/debug/pprof/cmdline">pprof/cmdline</a></li>
					<li><a href="/debug/pprof/profile">pprof/profile</a></li>
					<li><a href="/debug/pprof/symbol">pprof/symbol</a></li>
					<li><a href="/debug/pprof/trace">pprof/trace</a></li>
					<li><a href="/debug/pprof/goroutine?debug=1">pprof/goroutine?debug=1</a></li>
					<li><a href="/debug/metrics">metrics</a></li>
			</ul>
			</body>
			</html>
			`)
	})
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("GET /debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("GET /debug/pprof/trace", pprof.Trace)
	return mux
}

// isValidGitSHA validates if a string looks like a valid git SHA hash.
// Git SHAs are hexadecimal strings of at least 4 characters but typically 7, 8, or 40 characters.
func isValidGitSHA(sha string) bool {
	// Git SHA must be a hexadecimal string with at least 4 characters
	if len(sha) < 4 || len(sha) > 40 {
		return false
	}

	// Check if the string only contains hexadecimal characters
	for _, char := range sha {
		if !(char >= '0' && char <= '9') && !(char >= 'a' && char <= 'f') && !(char >= 'A' && char <= 'F') {
			return false
		}
	}

	return true
}
