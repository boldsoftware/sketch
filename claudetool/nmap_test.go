package claudetool

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestNmapTool_ensureNonInteractive(t *testing.T) {
	tool := &NmapTool{}

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "basic args without non-interactive flags",
			input:    []string{"-sS", "192.168.1.1"},
			expected: []string{"-sS", "192.168.1.1", "-n", "-v"},
		},
		{
			name:     "args already have DNS disabled",
			input:    []string{"-sS", "-n", "192.168.1.1"},
			expected: []string{"-sS", "-n", "192.168.1.1", "-v"},
		},
		{
			name:     "args already have verbose enabled",
			input:    []string{"-sS", "-v", "192.168.1.1"},
			expected: []string{"-sS", "-v", "192.168.1.1", "-n"},
		},
		{
			name:     "args already have both flags",
			input:    []string{"-sS", "-n", "-v", "192.168.1.1"},
			expected: []string{"-sS", "-n", "-v", "192.168.1.1"},
		},
		{
			name:     "complex scan with multiple flags",
			input:    []string{"-sS", "-A", "-p", "80,443", "192.168.1.0/24"},
			expected: []string{"-sS", "-A", "-p", "80,443", "192.168.1.0/24", "-n", "-v"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.ensureNonInteractive(tt.input)
			
			// Check that all expected flags are present
			for _, expectedFlag := range tt.expected {
				found := false
				for _, resultFlag := range result {
					if resultFlag == expectedFlag {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected flag %s not found in result %v", expectedFlag, result)
				}
			}
		})
	}
}

func TestNmapTool_calculateTimeout(t *testing.T) {
	tool := &NmapTool{}

	tests := []struct {
		name     string
		args     []string
		expected time.Duration
	}{
		{
			name:     "basic scan",
			args:     []string{"-sS", "192.168.1.1"},
			expected: 10 * time.Minute,
		},
		{
			name:     "UDP scan",
			args:     []string{"-sU", "192.168.1.1"},
			expected: 20 * time.Minute,
		},
		{
			name:     "aggressive scan",
			args:     []string{"-A", "192.168.1.1"},
			expected: 15 * time.Minute,
		},
		{
			name:     "script scan",
			args:     []string{"--script", "vuln", "192.168.1.1"},
			expected: 20 * time.Minute,
		},
		{
			name:     "paranoid timing",
			args:     []string{"-T", "0", "192.168.1.1"},
			expected: 60 * time.Minute,
		},
		{
			name:     "aggressive timing",
			args:     []string{"-T", "4", "192.168.1.1"},
			expected: 3 * time.Minute,
		},
		{
			name:     "port range scan",
			args:     []string{"-p", "1-65535", "192.168.1.1"},
			expected: 15 * time.Minute,
		},
		{
			name:     "OS detection",
			args:     []string{"-O", "192.168.1.1"},
			expected: 10 * time.Minute,
		},
		{
			name:     "default timeout",
			args:     []string{"192.168.1.1"},
			expected: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tool.calculateTimeout(tt.args)
			if result != tt.expected {
				t.Errorf("Expected timeout %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestNmapTool_Run_InputValidation(t *testing.T) {
	tool := &NmapTool{}
	ctx := context.Background()

	tests := []struct {
		name        string
		input       string
		expectError bool
	}{
		{
			name:        "valid input",
			input:       `{"args": ["-sS", "192.168.1.1"]}`,
			expectError: false,
		},
		{
			name:        "invalid JSON",
			input:       `{"args": ["-sS", "192.168.1.1"`,
			expectError: true,
		},
		{
			name:        "missing args field",
			input:       `{"target": "192.168.1.1"}`,
			expectError: false, // args will be empty array
		},
		{
			name:        "empty args",
			input:       `{"args": []}`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := tool.Run(ctx, json.RawMessage(tt.input))
			
			if tt.expectError && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectError && err != nil && !strings.Contains(err.Error(), "nmap") {
				// Allow nmap execution errors (tool not found, etc.) but not parsing errors
				if strings.Contains(err.Error(), "unmarshal") {
					t.Errorf("Unexpected parsing error: %v", err)
				}
			}
		})
	}
}

func TestNmapTool_Tool(t *testing.T) {
	tool := &NmapTool{}
	llmTool := tool.Tool()

	if llmTool.Name != "nmap" {
		t.Errorf("Expected tool name 'nmap', got '%s'", llmTool.Name)
	}

	if llmTool.Description == "" {
		t.Error("Tool description should not be empty")
	}

	if llmTool.InputSchema == nil {
		t.Error("Tool input schema should not be nil")
	}

	if llmTool.Run == nil {
		t.Error("Tool run function should not be nil")
	}
}

func TestNmapArgs_Unmarshaling(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected NmapArgs
	}{
		{
			name:     "simple args",
			input:    `{"args": ["-sS", "192.168.1.1"]}`,
			expected: NmapArgs{Args: []string{"-sS", "192.168.1.1"}},
		},
		{
			name:     "complex args",
			input:    `{"args": ["-sS", "-A", "-p", "80,443", "--script", "vuln", "192.168.1.0/24"]}`,
			expected: NmapArgs{Args: []string{"-sS", "-A", "-p", "80,443", "--script", "vuln", "192.168.1.0/24"}},
		},
		{
			name:     "empty args",
			input:    `{"args": []}`,
			expected: NmapArgs{Args: []string{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var args NmapArgs
			err := json.Unmarshal([]byte(tt.input), &args)
			if err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			if len(args.Args) != len(tt.expected.Args) {
				t.Errorf("Expected %d args, got %d", len(tt.expected.Args), len(args.Args))
				return
			}

			for i, arg := range args.Args {
				if arg != tt.expected.Args[i] {
					t.Errorf("Expected arg[%d] = '%s', got '%s'", i, tt.expected.Args[i], arg)
				}
			}
		})
	}
}