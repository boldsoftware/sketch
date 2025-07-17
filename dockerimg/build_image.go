package dockerimg

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// buildBaseDockerImage builds the base Docker image locally using the embedded Dockerfile
func buildBaseDockerImage(ctx context.Context, imageName string, verbose bool) error {
	// Create a temporary directory to store the Dockerfile
	tempDir, err := os.MkdirTemp("", "sketch-docker-build")
	if err != nil {
		return fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	// Write the Dockerfile to the temporary directory
	dockerfilePath := filepath.Join(tempDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfileBase), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	if verbose {
		fmt.Printf("Building Docker image from %s\n", dockerfilePath)
	}

	// Build the Docker image with platform specification and no cache to ensure proper build
	args := []string{
		"build",
		"--no-cache",
		"--platform", "linux/" + runtime.GOARCH,
		"-t", imageName,
		"-f", dockerfilePath,
		tempDir,
	}

	if verbose {
		// Show build output in verbose mode
		cmd := execCommand(ctx, "docker", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("docker build failed: %w", err)
		}
	} else {
		// Hide build output in non-verbose mode
		if out, err := combinedOutput(ctx, "docker", args...); err != nil {
			return fmt.Errorf("docker build failed: %s: %w", out, err)
		}
	}

	return nil
}

// execCommand is a wrapper around exec.CommandContext that allows for easier testing
var execCommand = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
