package claudetool

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"log/slog"
	"os/exec"

	"sketch.dev/llm"
	"strings"
	"time"
)

// NmapRun represents the structure of the Nmap XML output.
type NmapRun struct {
	XMLName          xml.Name `xml:"nmaprun"`
	Scanner          string   `xml:"scanner,attr"`
	Args             string   `xml:"args,attr"`
	Start            string   `xml:"start,attr"`
	Version          string   `xml:"version,attr"`
	XMLOutputVersion string   `xml:"xmloutputversion,attr"`
	Hosts            []Host   `xml:"host"`
}

type Host struct {
	Status  Status    `xml:"status"`
	Address []Address `xml:"address"`
	Ports   []Port    `xml:"ports>port"`
}

type Status struct {
	State  string `xml:"state,attr"`
	Reason string `xml:"reason,attr"`
}

type Address struct {
	Addr     string `xml:"addr,attr"`
	AddrType string `xml:"addrtype,attr"`
}

type Port struct {
	Protocol string  `xml:"protocol,attr"`
	PortID   int     `xml:"portid,attr"`
	State    State   `xml:"state"`
	Service  Service `xml:"service"`
}

type State struct {
	State  string `xml:"state,attr"`
	Reason string `xml:"reason,attr"`
}

type Service struct {
	Name      string `xml:"name,attr"`
	Product   string `xml:"product,attr,omitempty"`
	Version   string `xml:"version,attr,omitempty"`
	ExtraInfo string `xml:"extrainfo,attr,omitempty"`
	Method    string `xml:"method,attr"`
	Conf      int    `xml:"conf,attr"`
}

// NmapTool is a tool for running Nmap scans.
type NmapTool struct{}

type NmapArgs struct {
	Args []string `json:"args"`
}

const (
	nmapInputSchema = `{
		"type": "object",
		"properties": {
			"args": {
				"type": "array",
				"items": {
					"type": "string"
				},
				"description": "Nmap command line arguments (e.g., [\"-sS\", \"-p\", \"80,443\", \"192.168.1.1\"])"
			}
		},
		"required": ["args"]
	}`
)

func (t *NmapTool) Tool() *llm.Tool {
	return &llm.Tool{
		Name:        "nmap",
		Description: "Run an Nmap scan with the given arguments. The output will be parsed from XML into a structured format.",
		InputSchema: llm.MustSchema(nmapInputSchema),
		Run:         t.Run,
	}
}

func (t *NmapTool) Run(ctx context.Context, input json.RawMessage) ([]llm.Content, error) {
	var args NmapArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("failed to unmarshal nmap args: %w", err)
	}

	slog.InfoContext(ctx, "running nmap tool", "args", args.Args)

	// Add non-interactive flags and XML output
	argsWithXML := append(args.Args, "-oX", "-")
	
	// Ensure non-interactive execution
	argsWithXML = t.ensureNonInteractive(argsWithXML)

	// Set appropriate timeout based on scan complexity
	timeout := t.calculateTimeout(argsWithXML)
	ctxWithTimeout, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctxWithTimeout, "nmap", argsWithXML...)

	out, err := cmd.Output()
	var result string
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			result = fmt.Sprintf("nmap command failed with exit code %d: %s", ee.ExitCode(), string(ee.Stderr))
		} else {
			result = fmt.Sprintf("failed to execute nmap command: %v", err)
		}
	} else {
		var nmapRun NmapRun
		if err := xml.Unmarshal(out, &nmapRun); err != nil {
			// If XML parsing fails, return the raw output with a warning.
			result = fmt.Sprintf("WARNING: could not parse nmap XML output. Error: %v\n\nRaw output:\n%s", err, string(out))
		} else {
			// For now, just return the raw XML. We can format this better later.
			result = string(out)
		}
	}

	return []llm.Content{{Text: result}}, nil
}

// ensureNonInteractive adds flags to ensure nmap runs non-interactively
func (t *NmapTool) ensureNonInteractive(args []string) []string {
	// Check if non-interactive flags are already present
	hasNoDNS := false
	
	for _, arg := range args {
		switch arg {
		case "-n", "--no-dns":
			hasNoDNS = true
		}
	}
	
	// Add non-interactive flags if not present
	if !hasNoDNS {
		// Disable DNS resolution to avoid delays and prompts
		args = append(args, "-n")
	}
	
	// Add verbose output for progress monitoring
	hasVerbose := false
	for _, arg := range args {
		if arg == "-v" || arg == "-vv" || arg == "-vvv" || arg == "--verbose" {
			hasVerbose = true
			break
		}
	}
	if !hasVerbose {
		args = append(args, "-v")
	}
	
	return args
}

// calculateTimeout determines appropriate timeout based on scan complexity
func (t *NmapTool) calculateTimeout(args []string) time.Duration {
	baseTimeout := 5 * time.Minute // Default timeout
	
	// Analyze arguments to estimate scan complexity
	for i, arg := range args {
		switch {
		case arg == "-sS" || arg == "-sT": // TCP scans
			baseTimeout = 10 * time.Minute
		case arg == "-sU": // UDP scan (slower)
			baseTimeout = 20 * time.Minute
		case arg == "-A": // Aggressive scan
			baseTimeout = 15 * time.Minute
		case arg == "-p" && i+1 < len(args): // Port specification
			// Check if scanning many ports
			portSpec := args[i+1]
			if strings.Contains(portSpec, "-") || strings.Contains(portSpec, ",") {
				baseTimeout = 15 * time.Minute
			}
		case strings.HasPrefix(arg, "--script"): // Script scanning
			baseTimeout = 20 * time.Minute
		case arg == "-O": // OS detection
			baseTimeout = 10 * time.Minute
		}
	}
	
	// Check for timing template
	for i, arg := range args {
		if arg == "-T" && i+1 < len(args) {
			timing := args[i+1]
			switch timing {
			case "0", "1": // Paranoid/Sneaky - very slow
				baseTimeout = 60 * time.Minute
			case "2": // Polite - slower
				baseTimeout = 30 * time.Minute
			case "4", "5": // Aggressive/Insane - faster
				baseTimeout = 3 * time.Minute
			}
		}
	}
	
	return baseTimeout
}
