// Package onstart provides codebase analysis used to inform the initial system prompt.
package onstart

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"
)

// Codebase contains metadata about the codebase.
type Codebase struct {
	// ExtensionCounts tracks the number of files with each extension
	ExtensionCounts map[string]int
	// Total number of files analyzed
	TotalFiles int
	// BuildFiles contains paths to build and configuration files
	BuildFiles []string
	// DocumentationFiles contains paths to documentation files
	DocumentationFiles []string
	// GuidanceFiles contains paths to files that provide context and guidance to LLMs
	GuidanceFiles []string
	// InjectFiles contains paths to critical guidance files (like DEAR_LLM.md, claude.md, and cursorrules)
	// that need to be injected into the system prompt for highest visibility
	InjectFiles []string
	// InjectFileContents maps paths to file contents for critical inject files
	// to avoid requiring an extra file read during template rendering
	InjectFileContents map[string]string
}

// AnalyzeCodebase walks the codebase and analyzes the paths it finds.
func AnalyzeCodebase(ctx context.Context, repoPath string) (*Codebase, error) {
	// TODO: do a filesystem walk instead?
	// There's a balance: git ls-files skips node_modules etc,
	// but some guidance files might be locally .gitignored.
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = repoPath

	r, w := io.Pipe() // stream and scan rather than buffer
	cmd.Stdout = w

	err := cmd.Start()
	if err != nil {
		return nil, err
	}

	extCounts := make(map[string]int)
	var buildFiles []string
	var documentationFiles []string
	var guidanceFiles []string
	var injectFiles []string
	injectFileContents := make(map[string]string)
	var totalFiles int

	eg, _ := errgroup.WithContext(ctx)

	eg.Go(func() error {
		defer r.Close()

		scanner := bufio.NewScanner(r)
		scanner.Split(scanZero)
		for scanner.Scan() {
			file := scanner.Text()
			file = strings.TrimSpace(file)
			if file == "" {
				continue
			}
			totalFiles++
			ext := strings.ToLower(filepath.Ext(file))
			ext = cmp.Or(ext, "<no-extension>")
			extCounts[ext]++

			fileCategory := categorizeFile(file)
			// fmt.Println(file, "->", fileCategory)
			switch fileCategory {
			case "build":
				buildFiles = append(buildFiles, file)
			case "documentation":
				documentationFiles = append(documentationFiles, file)
			case "guidance":
				guidanceFiles = append(guidanceFiles, file)
			case "inject":
				injectFiles = append(injectFiles, file)
			}
		}
		return scanner.Err()
	})

	// Wait for the command to complete
	eg.Go(func() error {
		err := cmd.Wait()
		if err != nil {
			w.CloseWithError(err)
		} else {
			w.Close()
		}
		return err
	})

	if err := eg.Wait(); err != nil {
		return nil, err
	}

	// Read content of inject files
	for _, filePath := range injectFiles {
		absPath := filepath.Join(repoPath, filePath)
		content, err := os.ReadFile(absPath)
		if err != nil {
			fmt.Printf("Warning: Failed to read inject file %s: %v\n", filePath, err)
			continue
		}
		injectFileContents[filePath] = string(content)
	}

	return &Codebase{
		ExtensionCounts:    extCounts,
		TotalFiles:         totalFiles,
		BuildFiles:         buildFiles,
		DocumentationFiles: documentationFiles,
		GuidanceFiles:      guidanceFiles,
		InjectFiles:        injectFiles,
		InjectFileContents: injectFileContents,
	}, nil
}

// categorizeFile categorizes a file into one of four categories: build, documentation, guidance, or inject.
// Returns an empty string if the file doesn't belong to any of these categories.
// categorizeFile categorizes a file into one of four categories: build, documentation, guidance, or inject.
// Returns an empty string if the file doesn't belong to any of these categories.
// The path parameter is relative to the repository root as returned by git ls-files.
func categorizeFile(path string) string {
	filename := filepath.Base(path)
	lowerPath := strings.ToLower(path)
	lowerFilename := strings.ToLower(filename)

	// InjectFiles - critical guidance files that should be injected into the system prompt
	// These are repository root files only - files directly in the repo root, not in subdirectories
	// Since git ls-files returns paths relative to repo root, we just need to check for absence of path separators
	isRepoRootFile := !strings.Contains(path, "/")
	if isRepoRootFile {
		if (strings.HasPrefix(lowerFilename, "claude.") && strings.HasSuffix(lowerFilename, ".md")) ||
			strings.HasPrefix(lowerFilename, "dear_llm") ||
			(strings.HasPrefix(lowerFilename, "agent.") && strings.HasSuffix(lowerFilename, ".md")) ||
			strings.Contains(lowerFilename, "cursorrules") {
			return "inject"
		}
	}

	// GitHub Copilot: https://code.visualstudio.com/docs/copilot/copilot-customization
	if path == ".github/copilot-instructions.md" {
		return "inject"
	}

	// BuildFiles - build and configuration files
	if strings.HasPrefix(lowerFilename, "makefile") ||
		strings.HasSuffix(lowerPath, ".vscode/tasks.json") {
		return "build"
	}

	// DocumentationFiles - general documentation files
	if strings.HasPrefix(lowerFilename, "readme") ||
		strings.HasPrefix(lowerFilename, "contributing") {
		return "documentation"
	}

	// GuidanceFiles - other files that provide guidance but aren't critical enough to inject
	// Non-root directory claude.md files, and other guidance files
	if (strings.HasPrefix(lowerFilename, "claude.") && strings.HasSuffix(lowerFilename, ".md")) ||
		(strings.HasPrefix(lowerFilename, "agent.") && strings.HasSuffix(lowerFilename, ".md")) {
		return "guidance"
	}

	return ""
}

// TopExtensions returns the top 5 most common file extensions in the codebase
func (c *Codebase) TopExtensions() []string {
	type extCount struct {
		ext   string
		count int
	}
	pairs := make([]extCount, 0, len(c.ExtensionCounts))
	for ext, count := range c.ExtensionCounts {
		pairs = append(pairs, extCount{ext, count})
	}

	// Sort by count (descending), then by extension (ascending)
	slices.SortFunc(pairs, func(a, b extCount) int {
		return cmp.Or(
			-cmp.Compare(a.count, b.count),
			cmp.Compare(a.ext, b.ext),
		)
	})

	const nTop = 5
	count := min(nTop, len(pairs))
	result := make([]string, count)
	for i := range count {
		result[i] = fmt.Sprintf("%v: %v (%0.0f%%)", pairs[i].ext, pairs[i].count, 100*float64(pairs[i].count)/float64(c.TotalFiles))
	}

	return result
}

func scanZero(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	if i := bytes.IndexByte(data, 0); i >= 0 {
		// We have a full NUL line.
		return i + 1, data[0:i], nil
	}
	// If we're at EOF, we have a final, non-terminated line. Return it.
	if atEOF {
		return len(data), data, nil
	}
	// Request more data.
	return 0, nil, nil
}
