package loop

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"time"

	"tailscale.com/portlist"
)

// TestPortMonitor_NewPortMonitor tests the creation of a new PortMonitor.
func TestPortMonitor_NewPortMonitor(t *testing.T) {
	agent := createTestAgent(t)
	interval := 2 * time.Second

	pm := NewPortMonitor(agent, interval)

	if pm == nil {
		t.Fatal("NewPortMonitor returned nil")
	}

	if pm.agent != agent {
		t.Errorf("expected agent %v, got %v", agent, pm.agent)
	}

	if pm.interval != interval {
		t.Errorf("expected interval %v, got %v", interval, pm.interval)
	}

	if pm.running {
		t.Error("expected monitor to not be running initially")
	}

	if pm.poller == nil {
		t.Error("expected poller to be initialized")
	}

	if !pm.poller.IncludeLocalhost {
		t.Error("expected IncludeLocalhost to be true")
	}
}

// TestPortMonitor_DefaultInterval tests that a default interval is set when invalid.
func TestPortMonitor_DefaultInterval(t *testing.T) {
	agent := createTestAgent(t)

	pm := NewPortMonitor(agent, 0)
	if pm.interval != 5*time.Second {
		t.Errorf("expected default interval 5s, got %v", pm.interval)
	}

	pm2 := NewPortMonitor(agent, -1*time.Second)
	if pm2.interval != 5*time.Second {
		t.Errorf("expected default interval 5s, got %v", pm2.interval)
	}
}

// TestPortMonitor_StartStop tests starting and stopping the monitor.
func TestPortMonitor_StartStop(t *testing.T) {
	agent := createTestAgent(t)
	pm := NewPortMonitor(agent, 100*time.Millisecond)

	// Test starting
	ctx := context.Background()
	err := pm.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start port monitor: %v", err)
	}

	if !pm.running {
		t.Error("expected monitor to be running after start")
	}

	// Test double start fails
	err = pm.Start(ctx)
	if err == nil {
		t.Error("expected error when starting already running monitor")
	}

	// Test stopping
	pm.Stop()
	if pm.running {
		t.Error("expected monitor to not be running after stop")
	}

	// Test double stop is safe
	pm.Stop() // should not panic
}

// TestPortMonitor_GetPorts tests getting the cached port list.
func TestPortMonitor_GetPorts(t *testing.T) {
	agent := createTestAgent(t)
	pm := NewPortMonitor(agent, 100*time.Millisecond)

	// Initially should be empty
	ports := pm.GetPorts()
	if len(ports) != 0 {
		t.Errorf("expected empty ports initially, got %d", len(ports))
	}

	// Start monitoring to populate ports
	ctx := context.Background()
	err := pm.Start(ctx)
	if err != nil {
		t.Fatalf("failed to start port monitor: %v", err)
	}
	defer pm.Stop()

	// Allow some time for initial scan
	time.Sleep(200 * time.Millisecond)

	// Should have some ports now (at least system ports)
	ports = pm.GetPorts()
	// We can't guarantee specific ports, but there should be at least some TCP ports
	// on most systems (like SSH, etc.)
	t.Logf("Found %d TCP ports", len(ports))

	// Verify all returned ports are TCP
	for _, port := range ports {
		if port.Proto != "tcp" {
			t.Errorf("expected TCP port, got %s", port.Proto)
		}
	}
}

// TestPortMonitor_FilterTCPPorts tests the TCP port filtering.
func TestPortMonitor_FilterTCPPorts(t *testing.T) {
	ports := []portlist.Port{
		{Proto: "tcp", Port: 80},
		{Proto: "udp", Port: 53},
		{Proto: "tcp", Port: 443},
		{Proto: "udp", Port: 123},
	}

	tcpPorts := filterTCPPorts(ports)

	if len(tcpPorts) != 2 {
		t.Errorf("expected 2 TCP ports, got %d", len(tcpPorts))
	}

	for _, port := range tcpPorts {
		if port.Proto != "tcp" {
			t.Errorf("expected TCP port, got %s", port.Proto)
		}
	}
}

// TestPortMonitor_SortPorts tests the port sorting.
func TestPortMonitor_SortPorts(t *testing.T) {
	ports := []portlist.Port{
		{Proto: "tcp", Port: 443},
		{Proto: "tcp", Port: 80},
		{Proto: "tcp", Port: 8080},
		{Proto: "tcp", Port: 22},
	}

	sortPorts(ports)

	expected := []uint16{22, 80, 443, 8080}
	for i, port := range ports {
		if port.Port != expected[i] {
			t.Errorf("expected port %d at index %d, got %d", expected[i], i, port.Port)
		}
	}
}

// TestPortMonitor_FindAddedPorts tests finding added ports.
func TestPortMonitor_FindAddedPorts(t *testing.T) {
	previous := []portlist.Port{
		{Proto: "tcp", Port: 80},
		{Proto: "tcp", Port: 443},
	}

	current := []portlist.Port{
		{Proto: "tcp", Port: 80},
		{Proto: "tcp", Port: 443},
		{Proto: "tcp", Port: 8080},
		{Proto: "tcp", Port: 22},
	}

	added := findAddedPorts(previous, current)

	if len(added) != 2 {
		t.Errorf("expected 2 added ports, got %d", len(added))
	}

	addedPorts := make(map[uint16]bool)
	for _, port := range added {
		addedPorts[port.Port] = true
	}

	if !addedPorts[8080] || !addedPorts[22] {
		t.Errorf("expected ports 8080 and 22 to be added, got %v", added)
	}
}

// TestPortMonitor_FindRemovedPorts tests finding removed ports.
func TestPortMonitor_FindRemovedPorts(t *testing.T) {
	previous := []portlist.Port{
		{Proto: "tcp", Port: 80},
		{Proto: "tcp", Port: 443},
		{Proto: "tcp", Port: 8080},
		{Proto: "tcp", Port: 22},
	}

	current := []portlist.Port{
		{Proto: "tcp", Port: 80},
		{Proto: "tcp", Port: 443},
	}

	removed := findRemovedPorts(previous, current)

	if len(removed) != 2 {
		t.Errorf("expected 2 removed ports, got %d", len(removed))
	}

	removedPorts := make(map[uint16]bool)
	for _, port := range removed {
		removedPorts[port.Port] = true
	}

	if !removedPorts[8080] || !removedPorts[22] {
		t.Errorf("expected ports 8080 and 22 to be removed, got %v", removed)
	}
}

// TestPortMonitor_ShouldIgnoreProcess tests the shouldIgnoreProcess function.
func TestPortMonitor_ShouldIgnoreProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		// The implementation of shouldIgnoreProcess is specific to Linux (it uses /proc).
		// On macOS, ignoring SKETCH_IGNORE_PORTS simply won't work, because macOS doesn't expose other processes' environment variables.
		// This is OK (enough) because our primary operating environment is a Linux container.
		t.Skip("skipping test on non-Linux OS")
	}

	agent := createTestAgent(t)
	pm := NewPortMonitor(agent, 100*time.Millisecond)

	// Test with current process (should not be ignored)
	currentPid := os.Getpid()
	if pm.shouldIgnoreProcess(currentPid) {
		t.Errorf("current process should not be ignored")
	}

	// Test with invalid PID
	if pm.shouldIgnoreProcess(0) {
		t.Errorf("invalid PID should not be ignored")
	}
	if pm.shouldIgnoreProcess(-1) {
		t.Errorf("negative PID should not be ignored")
	}

	// Test with a process that has SKETCH_IGNORE_PORTS=1
	cmd := exec.Command("sleep", "5")
	cmd.Env = append(os.Environ(), "SKETCH_IGNORE_PORTS=1")
	err := cmd.Start()
	if err != nil {
		t.Fatalf("failed to start test process: %v", err)
	}
	defer cmd.Process.Kill()

	// Allow a moment for the process to start
	time.Sleep(100 * time.Millisecond)

	if !pm.shouldIgnoreProcess(cmd.Process.Pid) {
		t.Errorf("process with SKETCH_IGNORE_PORTS=1 should be ignored")
	}
}

// createTestAgent creates a minimal test agent for testing.
func createTestAgent(t *testing.T) *Agent {
	// Create a minimal agent for testing
	// We need to initialize the required fields for the PortMonitor to work
	agent := &Agent{
		subscribers: make([]chan *AgentMessage, 0),
	}
	return agent
}
