// Package main provides a minimal command-line tool for generating bundle metafiles for analysis
// with the esbuild analyzer at https://esbuild.github.io/analyze/
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"sketch.dev/loop/webui"
)

func main() {
	// Use a temporary directory for output by default
	tmp, err := os.MkdirTemp("", "bundle-analysis-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating temp directory: %v\n", err)
		os.Exit(1)
	}

	// Parse command line flags
	outputDir := flag.String("output", tmp, "Directory where the bundle metafiles should be written")
	flag.Parse()

	// Ensure the output directory exists
	if err := os.MkdirAll(*outputDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output directory: %v\n", err)
		os.Exit(1)
	}

	// Get absolute path for better user feedback
	absOutDir, err := filepath.Abs(*outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error getting absolute path: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("Generating bundle metafiles...")

	// Generate the metafiles
	_, err = webui.GenerateBundleMetafile(*outputDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error generating metafiles: %v\n", err)
		os.Exit(1)
	}

	// Print the simple usage instructions
	fmt.Printf("\nBundle metafiles generated at: %s\n\n", absOutDir)
	fmt.Println("To analyze bundles:")
	fmt.Println("1. Go to https://esbuild.github.io/analyze/")
	fmt.Println("2. Drag and drop the generated metafiles onto the analyzer")
	fmt.Printf("   - %s/timeline.meta.json\n", absOutDir)
	fmt.Printf("   - %s/sketch-app-shell.meta.json\n", absOutDir)
}
