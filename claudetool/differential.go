package claudetool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/tools/go/packages"
	"sketch.dev/ant"
)

// This file does differential quality analysis of a commit relative to a base commit.

// Tool returns a tool spec for a CodeReview tool backed by r.
func (r *CodeReviewer) Tool() *ant.Tool {
	spec := &ant.Tool{
		Name:        "codereview",
		Description: `Run an automated code review.`,
		// If you modify this, update the termui template for prettier rendering.
		InputSchema: ant.MustSchema(`{"type": "object"}`),
		Run:         r.Run,
	}
	return spec
}

func (r *CodeReviewer) Run(ctx context.Context, m json.RawMessage) (string, error) {
	if err := r.RequireNormalGitState(ctx); err != nil {
		slog.DebugContext(ctx, "CodeReviewer.Run: failed to check for normal git state", "err", err)
		return "", err
	}
	if err := r.RequireNoUncommittedChanges(ctx); err != nil {
		slog.DebugContext(ctx, "CodeReviewer.Run: failed to check for uncommitted changes", "err", err)
		return "", err
	}

	// Check that the current commit is not the initial commit
	currentCommit, err := r.CurrentCommit(ctx)
	if err != nil {
		slog.DebugContext(ctx, "CodeReviewer.Run: failed to get current commit", "err", err)
		return "", err
	}
	if r.IsInitialCommit(currentCommit) {
		slog.DebugContext(ctx, "CodeReviewer.Run: current commit is initial commit, nothing to review")
		return "", fmt.Errorf("no new commits have been added, nothing to review")
	}

	// No matter what failures happen from here out, we will declare this to have been reviewed.
	// This should help avoid the model getting blocked by a broken code review tool.
	r.reviewed = append(r.reviewed, currentCommit)

	changedFiles, err := r.changedFiles(ctx, r.initialCommit, currentCommit)
	if err != nil {
		slog.DebugContext(ctx, "CodeReviewer.Run: failed to get changed files", "err", err)
		return "", err
	}

	// Prepare to analyze before/after for the impacted files.
	// We use the current commit to determine what packages exist and are impacted.
	// The packages in the initial commit may be different.
	// Good enough for now.
	// TODO: do better
	directPkgs, allPkgs, err := r.packagesForFiles(ctx, changedFiles)
	if err != nil {
		// TODO: log and skip to stuff that doesn't require packages
		slog.DebugContext(ctx, "CodeReviewer.Run: failed to get packages for files", "err", err)
		return "", err
	}
	allPkgList := slices.Collect(maps.Keys(allPkgs))
	directPkgList := slices.Collect(maps.Keys(directPkgs))

	var msgs []string

	testMsg, err := r.checkTests(ctx, allPkgList)
	if err != nil {
		slog.DebugContext(ctx, "CodeReviewer.Run: failed to check tests", "err", err)
		return "", err
	}
	if testMsg != "" {
		msgs = append(msgs, testMsg)
	}

	vetMsg, err := r.checkVet(ctx, directPkgList)
	if err != nil {
		slog.DebugContext(ctx, "CodeReviewer.Run: failed to check vet", "err", err)
		return "", err
	}
	if vetMsg != "" {
		msgs = append(msgs, vetMsg)
	}

	goplsMsg, err := r.checkGopls(ctx, changedFiles)
	if err != nil {
		slog.DebugContext(ctx, "CodeReviewer.Run: failed to check gopls", "err", err)
		return "", err
	}
	if goplsMsg != "" {
		msgs = append(msgs, goplsMsg)
	}

	if len(msgs) == 0 {
		slog.DebugContext(ctx, "CodeReviewer.Run: no issues found")
		return "OK", nil
	}
	slog.DebugContext(ctx, "CodeReviewer.Run: found issues", "issues", msgs)
	return strings.Join(msgs, "\n\n"), nil
}

func (r *CodeReviewer) initializeInitialCommitWorktree(ctx context.Context) error {
	if r.initialWorktree != "" {
		return nil
	}
	tmpDir, err := os.MkdirTemp("", "sketch-codereview-worktree")
	if err != nil {
		return err
	}
	worktreeCmd := exec.CommandContext(ctx, "git", "worktree", "add", "--detach", tmpDir, r.initialCommit)
	worktreeCmd.Dir = r.repoRoot
	out, err := worktreeCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("unable to create worktree for initial commit: %w\n%s", err, out)
	}
	r.initialWorktree = tmpDir
	return nil
}

func (r *CodeReviewer) checkTests(ctx context.Context, pkgList []string) (string, error) {
	goTestArgs := []string{"test", "-json", "-v"}
	goTestArgs = append(goTestArgs, pkgList...)

	afterTestCmd := exec.CommandContext(ctx, "go", goTestArgs...)
	afterTestCmd.Dir = r.repoRoot
	afterTestOut, afterTestErr := afterTestCmd.Output()
	if afterTestErr == nil {
		return "", nil // all tests pass, we're good!
	}

	err := r.initializeInitialCommitWorktree(ctx)
	if err != nil {
		return "", err
	}

	beforeTestCmd := exec.CommandContext(ctx, "go", goTestArgs...)
	beforeTestCmd.Dir = r.initialWorktree
	beforeTestOut, _ := beforeTestCmd.Output() // ignore error, interesting info is in the output

	// Parse the jsonl test results
	beforeResults, beforeParseErr := parseTestResults(beforeTestOut)
	if beforeParseErr != nil {
		return "", fmt.Errorf("unable to parse test results for initial commit: %w\n%s", beforeParseErr, beforeTestOut)
	}
	afterResults, afterParseErr := parseTestResults(afterTestOut)
	if afterParseErr != nil {
		return "", fmt.Errorf("unable to parse test results for current commit: %w\n%s", afterParseErr, afterTestOut)
	}

	testRegressions, err := r.compareTestResults(beforeResults, afterResults)
	if err != nil {
		return "", fmt.Errorf("failed to compare test results: %w", err)
	}
	// TODO: better output formatting?
	res := r.formatTestRegressions(testRegressions)
	return res, nil
}

// VetIssue represents a single issue found by go vet
type VetIssue struct {
	Position string `json:"posn"`
	Message  string `json:"message"`
	// Ignoring suggested_fixes for now as we don't need them for comparison
}

// VetResult represents the JSON output of go vet -json for a single package
type VetResult map[string][]VetIssue // category -> issues

// VetResults represents the full JSON output of go vet -json
type VetResults map[string]VetResult // package path -> result

// checkVet runs go vet on the provided packages in both the current and initial state,
// compares the results, and reports any new vet issues introduced in the current state.
func (r *CodeReviewer) checkVet(ctx context.Context, pkgList []string) (string, error) {
	if len(pkgList) == 0 {
		return "", nil // no packages to check
	}

	// Run vet on the current state with JSON output
	goVetArgs := []string{"vet", "-json"}
	goVetArgs = append(goVetArgs, pkgList...)

	afterVetCmd := exec.CommandContext(ctx, "go", goVetArgs...)
	afterVetCmd.Dir = r.repoRoot
	afterVetOut, afterVetErr := afterVetCmd.CombinedOutput() // ignore error, we'll parse the output regar
	if afterVetErr != nil {
		slog.WarnContext(ctx, "CodeReviewer.checkVet: (after) go vet failed", "err", afterVetErr, "output", string(afterVetOut))
		return "", nil // nothing more we can do here
	}

	// Parse the JSON output (even if vet returned an error, as it does when issues are found)
	afterVetResults, err := parseVetJSON(afterVetOut)
	if err != nil {
		return "", fmt.Errorf("failed to parse vet output for current state: %w", err)
	}

	// If no issues were found, we're done
	if len(afterVetResults) == 0 || !vetResultsHaveIssues(afterVetResults) {
		return "", nil
	}

	// Vet detected issues in the current state, check if they existed in the initial state
	err = r.initializeInitialCommitWorktree(ctx)
	if err != nil {
		return "", err
	}

	beforeVetCmd := exec.CommandContext(ctx, "go", goVetArgs...)
	beforeVetCmd.Dir = r.initialWorktree
	beforeVetOut, _ := beforeVetCmd.CombinedOutput() // ignore error, we'll parse the output anyway

	// Parse the JSON output for the initial state
	beforeVetResults, err := parseVetJSON(beforeVetOut)
	if err != nil {
		return "", fmt.Errorf("failed to parse vet output for initial state: %w", err)
	}

	// Find new issues that weren't present in the initial state
	vetRegressions := findVetRegressions(beforeVetResults, afterVetResults)
	if !vetResultsHaveIssues(vetRegressions) {
		return "", nil // no new issues
	}

	// Format the results
	return formatVetRegressions(vetRegressions), nil
}

// parseVetJSON parses the JSON output from go vet -json
func parseVetJSON(output []byte) (VetResults, error) {
	// The output contains multiple JSON objects, one per package
	// We need to parse them separately
	results := make(VetResults)

	// Process the output by collecting JSON chunks between # comment lines
	lines := strings.Split(string(output), "\n")
	currentChunk := strings.Builder{}

	// Helper function to process accumulated JSON chunks
	processChunk := func() {
		chunk := strings.TrimSpace(currentChunk.String())
		if chunk == "" || !strings.HasPrefix(chunk, "{") {
			return // Skip empty chunks or non-JSON chunks
		}

		// Try to parse the chunk as JSON
		var result VetResults
		if err := json.Unmarshal([]byte(chunk), &result); err != nil {
			return // Skip invalid JSON
		}

		// Merge with our results
		for pkg, issues := range result {
			results[pkg] = issues
		}

		// Reset the chunk builder
		currentChunk.Reset()
	}

	// Process lines
	for _, line := range lines {
		// If we hit a comment line, process the previous chunk and start a new one
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			processChunk()
			continue
		}

		// Add the line to the current chunk
		currentChunk.WriteString(line)
		currentChunk.WriteString("\n")
	}

	// Process the final chunk
	processChunk()

	return results, nil
}

// vetResultsHaveIssues checks if there are any actual issues in the vet results
func vetResultsHaveIssues(results VetResults) bool {
	for _, pkgResult := range results {
		for _, issues := range pkgResult {
			if len(issues) > 0 {
				return true
			}
		}
	}
	return false
}

// findVetRegressions identifies vet issues that are new in the after state
func findVetRegressions(before, after VetResults) VetResults {
	regressions := make(VetResults)

	// Go through all packages in the after state
	for pkgPath, afterPkgResults := range after {
		beforePkgResults, pkgExistedBefore := before[pkgPath]

		// Initialize package in regressions if it has issues
		if !pkgExistedBefore {
			// If the package didn't exist before, all issues are new
			regressions[pkgPath] = afterPkgResults
			continue
		}

		// Compare issues by category
		for category, afterIssues := range afterPkgResults {
			beforeIssues, categoryExistedBefore := beforePkgResults[category]

			if !categoryExistedBefore {
				// If this category didn't exist before, all issues are new
				if regressions[pkgPath] == nil {
					regressions[pkgPath] = make(VetResult)
				}
				regressions[pkgPath][category] = afterIssues
				continue
			}

			// Compare individual issues
			var newIssues []VetIssue
			for _, afterIssue := range afterIssues {
				if !issueExistsIn(afterIssue, beforeIssues) {
					newIssues = append(newIssues, afterIssue)
				}
			}

			// Add new issues to regressions
			if len(newIssues) > 0 {
				if regressions[pkgPath] == nil {
					regressions[pkgPath] = make(VetResult)
				}
				regressions[pkgPath][category] = newIssues
			}
		}
	}

	return regressions
}

// issueExistsIn checks if an issue already exists in a list of issues
// using a looser comparison that's resilient to position changes
func issueExistsIn(issue VetIssue, issues []VetIssue) bool {
	issueFile := extractFilePath(issue.Position)

	for _, existing := range issues {
		// Main comparison is by message content, which is likely stable
		if issue.Message == existing.Message {
			// If messages match exactly, consider it the same issue even if position changed
			return true
		}

		// As a secondary check, if the issue is in the same file and has similar message,
		// it's likely the same issue that might have been slightly reworded or relocated
		existingFile := extractFilePath(existing.Position)
		if issueFile == existingFile && messagesSimilar(issue.Message, existing.Message) {
			return true
		}
	}
	return false
}

// extractFilePath gets just the file path from a position string like "/path/to/file.go:10:15"
func extractFilePath(position string) string {
	parts := strings.Split(position, ":")
	if len(parts) >= 1 {
		return parts[0]
	}
	return position // fallback to the full position if we can't parse it
}

// messagesSimilar checks if two messages are similar enough to be considered the same issue
// This is a simple implementation that could be enhanced with more sophisticated text comparison
func messagesSimilar(msg1, msg2 string) bool {
	// For now, simple similarity check: if one is a substring of the other
	return strings.Contains(msg1, msg2) || strings.Contains(msg2, msg1)
}

// formatVetRegressions generates a human-readable summary of vet regressions
func formatVetRegressions(regressions VetResults) string {
	if !vetResultsHaveIssues(regressions) {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Go vet issues detected:\n\n")

	// Get sorted list of packages for deterministic output
	pkgPaths := make([]string, 0, len(regressions))
	for pkgPath := range regressions {
		pkgPaths = append(pkgPaths, pkgPath)
	}
	slices.Sort(pkgPaths)

	issueCount := 1
	for _, pkgPath := range pkgPaths {
		pkgResult := regressions[pkgPath]

		// Get sorted list of categories
		categories := make([]string, 0, len(pkgResult))
		for category := range pkgResult {
			categories = append(categories, category)
		}
		slices.Sort(categories)

		for _, category := range categories {
			issues := pkgResult[category]

			// Skip empty issue lists (shouldn't happen, but just in case)
			if len(issues) == 0 {
				continue
			}

			// Sort issues by position for deterministic output
			slices.SortFunc(issues, func(a, b VetIssue) int {
				return strings.Compare(a.Position, b.Position)
			})

			// Format each issue
			for _, issue := range issues {
				sb.WriteString(fmt.Sprintf("%d. [%s] %s: %s\n",
					issueCount,
					category,
					issue.Position,
					issue.Message))
				issueCount++
			}
		}
	}

	sb.WriteString("\nPlease fix these issues before proceeding.")
	return sb.String()
}

// GoplsIssue represents a single issue reported by gopls check
type GoplsIssue struct {
	Position string // File position in format "file:line:col-range"
	Message  string // Description of the issue
}

// checkGopls runs gopls check on the provided files in both the current and initial state,
// compares the results, and reports any new issues introduced in the current state.
func (r *CodeReviewer) checkGopls(ctx context.Context, changedFiles []string) (string, error) {
	if len(changedFiles) == 0 {
		return "", nil // no files to check
	}

	// Filter out non-Go files as gopls only works on Go files
	// and verify they still exist (not deleted)
	var goFiles []string
	for _, file := range changedFiles {
		if !strings.HasSuffix(file, ".go") {
			continue // not a Go file
		}

		// Check if the file still exists (not deleted)
		if _, err := os.Stat(file); os.IsNotExist(err) {
			continue // file doesn't exist anymore (deleted)
		}

		goFiles = append(goFiles, file)
	}

	if len(goFiles) == 0 {
		return "", nil // no Go files to check
	}

	// Run gopls check on the current state
	goplsArgs := append([]string{"check"}, goFiles...)

	afterGoplsCmd := exec.CommandContext(ctx, "gopls", goplsArgs...)
	afterGoplsCmd.Dir = r.repoRoot
	afterGoplsOut, err := afterGoplsCmd.CombinedOutput() // gopls returns non-zero if it finds issues
	if err != nil {
		// Check if the output looks like real gopls issues or if it's just error output
		if !looksLikeGoplsIssues(afterGoplsOut) {
			slog.WarnContext(ctx, "CodeReviewer.checkGopls: gopls check failed to run properly", "err", err, "output", string(afterGoplsOut))
			return "", nil // Skip rather than failing the entire code review
		}
		// Otherwise, proceed with parsing - it's likely just the non-zero exit code due to found issues
	}

	// Parse the output
	afterIssues := parseGoplsOutput(afterGoplsOut)

	// If no issues were found, we're done
	if len(afterIssues) == 0 {
		return "", nil
	}

	// Gopls detected issues in the current state, check if they existed in the initial state
	initErr := r.initializeInitialCommitWorktree(ctx)
	if initErr != nil {
		return "", err
	}

	// For each file that exists in the initial commit, run gopls check
	var initialFilesToCheck []string
	for _, file := range goFiles {
		// Get relative path for git operations
		relFile, err := filepath.Rel(r.repoRoot, file)
		if err != nil {
			slog.WarnContext(ctx, "CodeReviewer.checkGopls: failed to get relative path", "repo_root", r.repoRoot, "file", file, "err", err)
			continue
		}

		// Check if the file exists in the initial commit
		checkCmd := exec.CommandContext(ctx, "git", "cat-file", "-e", fmt.Sprintf("%s:%s", r.initialCommit, relFile))
		checkCmd.Dir = r.repoRoot
		if err := checkCmd.Run(); err == nil {
			// File exists in initial commit
			initialFilePath := filepath.Join(r.initialWorktree, relFile)
			initialFilesToCheck = append(initialFilesToCheck, initialFilePath)
		}
	}

	// Run gopls check on the files that existed in the initial commit
	beforeIssues := []GoplsIssue{}
	if len(initialFilesToCheck) > 0 {
		beforeGoplsArgs := append([]string{"check"}, initialFilesToCheck...)
		beforeGoplsCmd := exec.CommandContext(ctx, "gopls", beforeGoplsArgs...)
		beforeGoplsCmd.Dir = r.initialWorktree
		var beforeGoplsOut []byte
		var beforeCmdErr error
		beforeGoplsOut, beforeCmdErr = beforeGoplsCmd.CombinedOutput()
		if beforeCmdErr != nil && !looksLikeGoplsIssues(beforeGoplsOut) {
			// If gopls fails to run properly on the initial commit, log a warning and continue
			// with empty before issues - this will be conservative and report more issues
			slog.WarnContext(ctx, "CodeReviewer.checkGopls: gopls check failed on initial commit",
				"err", err, "output", string(beforeGoplsOut))
			// Continue with empty beforeIssues
		} else {
			beforeIssues = parseGoplsOutput(beforeGoplsOut)
		}
	}

	// Find new issues that weren't present in the initial state
	goplsRegressions := findGoplsRegressions(beforeIssues, afterIssues)
	if len(goplsRegressions) == 0 {
		return "", nil // no new issues
	}

	// Format the results
	return formatGoplsRegressions(goplsRegressions), nil
}

// parseGoplsOutput parses the text output from gopls check
// Each line has the format: '/path/to/file.go:448:22-26: unused parameter: path'
func parseGoplsOutput(output []byte) []GoplsIssue {
	var issues []GoplsIssue
	lines := strings.Split(string(output), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Skip lines that look like error messages rather than gopls issues
		if strings.HasPrefix(line, "Error:") ||
			strings.HasPrefix(line, "Failed:") ||
			strings.HasPrefix(line, "Warning:") ||
			strings.HasPrefix(line, "gopls:") {
			continue
		}

		// Find the first colon that separates the file path from the line number
		firstColonIdx := strings.Index(line, ":")
		if firstColonIdx < 0 {
			continue // Invalid format
		}

		// Verify the part before the first colon looks like a file path
		potentialPath := line[:firstColonIdx]
		if !strings.HasSuffix(potentialPath, ".go") {
			continue // Not a Go file path
		}

		// Find the position of the first message separator ': '
		// This separates the position info from the message
		messageStart := strings.Index(line, ": ")
		if messageStart < 0 || messageStart <= firstColonIdx {
			continue // Invalid format
		}

		// Extract position and message
		position := line[:messageStart]
		message := line[messageStart+2:] // Skip the ': ' separator

		// Verify position has the expected format (at least 2 colons for line:col)
		colonCount := strings.Count(position, ":")
		if colonCount < 2 {
			continue // Not enough position information
		}

		issues = append(issues, GoplsIssue{
			Position: position,
			Message:  message,
		})
	}

	return issues
}

// looksLikeGoplsIssues checks if the output appears to be actual gopls issues
// rather than error messages about gopls itself failing
func looksLikeGoplsIssues(output []byte) bool {
	// If output is empty, it's not valid issues
	if len(output) == 0 {
		return false
	}

	// Check if output has at least one line that looks like a gopls issue
	// A gopls issue looks like: '/path/to/file.go:123:45-67: message'
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// A gopls issue has at least two colons (file path, line number, column)
		// and contains a colon followed by a space (separating position from message)
		colonCount := strings.Count(line, ":")
		hasSeparator := strings.Contains(line, ": ")

		if colonCount >= 2 && hasSeparator {
			// Check if it starts with a likely file path (ending in .go)
			parts := strings.SplitN(line, ":", 2)
			if strings.HasSuffix(parts[0], ".go") {
				return true
			}
		}
	}
	return false
}

// normalizeGoplsPosition extracts just the file path from a position string
func normalizeGoplsPosition(position string) string {
	// Extract just the file path by taking everything before the first colon
	parts := strings.Split(position, ":")
	if len(parts) < 1 {
		return position
	}
	return parts[0]
}

// findGoplsRegressions identifies gopls issues that are new in the after state
func findGoplsRegressions(before, after []GoplsIssue) []GoplsIssue {
	var regressions []GoplsIssue

	// Build map of before issues for easier lookup
	beforeIssueMap := make(map[string]map[string]bool) // file -> message -> exists
	for _, issue := range before {
		file := normalizeGoplsPosition(issue.Position)
		if _, exists := beforeIssueMap[file]; !exists {
			beforeIssueMap[file] = make(map[string]bool)
		}
		// Store both the exact message and the general issue type for fuzzy matching
		beforeIssueMap[file][issue.Message] = true

		// Extract the general issue type (everything before the first ':' in the message)
		generalIssue := issue.Message
		if colonIdx := strings.Index(issue.Message, ":"); colonIdx > 0 {
			generalIssue = issue.Message[:colonIdx]
		}
		beforeIssueMap[file][generalIssue] = true
	}

	// Check each after issue to see if it's new
	for _, afterIssue := range after {
		file := normalizeGoplsPosition(afterIssue.Position)
		isNew := true

		if fileIssues, fileExists := beforeIssueMap[file]; fileExists {
			// Check for exact message match
			if fileIssues[afterIssue.Message] {
				isNew = false
			} else {
				// Check for general issue type match
				generalIssue := afterIssue.Message
				if colonIdx := strings.Index(afterIssue.Message, ":"); colonIdx > 0 {
					generalIssue = afterIssue.Message[:colonIdx]
				}
				if fileIssues[generalIssue] {
					isNew = false
				}
			}
		}

		if isNew {
			regressions = append(regressions, afterIssue)
		}
	}

	// Sort regressions for deterministic output
	slices.SortFunc(regressions, func(a, b GoplsIssue) int {
		return strings.Compare(a.Position, b.Position)
	})

	return regressions
}

// formatGoplsRegressions generates a human-readable summary of gopls check regressions
func formatGoplsRegressions(regressions []GoplsIssue) string {
	if len(regressions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Gopls check issues detected:\n\n")

	// Format each issue
	for i, issue := range regressions {
		sb.WriteString(fmt.Sprintf("%d. %s: %s\n", i+1, issue.Position, issue.Message))
	}

	sb.WriteString("\nIMPORTANT: Only fix new gopls check issues in parts of the code that you have already edited. ")
	sb.WriteString("Do not change existing code that was not part of your current edits.")
	return sb.String()
}

func (r *CodeReviewer) HasReviewed(commit string) bool {
	return slices.Contains(r.reviewed, commit)
}

func (r *CodeReviewer) IsInitialCommit(commit string) bool {
	return commit == r.initialCommit
}

// packagesForFiles returns maps of packages related to the given files:
// 1. directPkgs: packages that directly contain the changed files
// 2. allPkgs: all packages that might be affected, including downstream packages that depend on the direct packages
// It may include false positives.
// Files must be absolute paths!
func (r *CodeReviewer) packagesForFiles(ctx context.Context, files []string) (directPkgs, allPkgs map[string]*packages.Package, err error) {
	for _, f := range files {
		if !filepath.IsAbs(f) {
			return nil, nil, fmt.Errorf("path %q is not absolute", f)
		}
	}
	cfg := &packages.Config{
		Mode:    packages.LoadImports | packages.NeedEmbedFiles,
		Context: ctx,
		// Logf: func(msg string, args ...any) {
		// 	slog.DebugContext(ctx, "loading go packages", "msg", fmt.Sprintf(msg, args...))
		// },
		// TODO: in theory, go.mod might not be in the repo root, and there might be multiple go.mod files.
		// We can cross that bridge when we get there.
		Dir:   r.repoRoot,
		Tests: true,
	}
	universe, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, nil, err
	}
	// Identify packages that directly contain the changed files
	directPkgs = make(map[string]*packages.Package) // import path -> package
	for _, pkg := range universe {
		// fmt.Println("pkg:", pkg.PkgPath)
		pkgFiles := allFiles(pkg)
		// fmt.Println("pkgFiles:", pkgFiles)
		for _, file := range files {
			if pkgFiles[file] {
				// prefer test packages, as they contain strictly more files (right?)
				prev := directPkgs[pkg.PkgPath]
				if prev == nil || prev.ForTest == "" {
					directPkgs[pkg.PkgPath] = pkg
				}
			}
		}
	}

	// Create a copy of directPkgs to expand with dependencies
	allPkgs = make(map[string]*packages.Package)
	for k, v := range directPkgs {
		allPkgs[k] = v
	}

	// Add packages that depend on the direct packages
	addDependentPackages(universe, allPkgs)
	return directPkgs, allPkgs, nil
}

// allFiles returns all files that might be referenced by the package.
// It may contain false positives.
func allFiles(p *packages.Package) map[string]bool {
	files := make(map[string]bool)
	add := [][]string{p.GoFiles, p.CompiledGoFiles, p.OtherFiles, p.EmbedFiles, p.IgnoredFiles}
	for _, extra := range add {
		for _, file := range extra {
			files[file] = true
		}
	}
	return files
}

// addDependentPackages adds to pkgs all packages from universe
// that directly or indirectly depend on any package already in pkgs.
func addDependentPackages(universe []*packages.Package, pkgs map[string]*packages.Package) {
	for {
		changed := false
		for _, p := range universe {
			if _, ok := pkgs[p.PkgPath]; ok {
				// already in pkgs
				continue
			}
			for importPath := range p.Imports {
				if _, ok := pkgs[importPath]; ok {
					// imports a package dependent on pkgs, add it
					pkgs[p.PkgPath] = p
					changed = true
					break
				}
			}
		}
		if !changed {
			break
		}
	}
}

// testJSON is a union of BuildEvent and TestEvent
type testJSON struct {
	// TestEvent only:
	// The Time field holds the time the event happened. It is conventionally omitted
	// for cached test results.
	Time time.Time `json:"Time"`
	// BuildEvent only:
	// The ImportPath field gives the package ID of the package being built.
	// This matches the Package.ImportPath field of go list -json and the
	// TestEvent.FailedBuild field of go test -json. Note that it does not
	// match TestEvent.Package.
	ImportPath string `json:"ImportPath"` // BuildEvent only
	// TestEvent only:
	// The Package field, if present, specifies the package being tested. When the
	// go command runs parallel tests in -json mode, events from different tests are
	// interlaced; the Package field allows readers to separate them.
	Package string `json:"Package"`
	// Action is used in both BuildEvent and TestEvent.
	// It is the key to distinguishing between them.
	// BuildEvent:
	// build-output or build-fail
	// TestEvent:
	// start, run, pause, cont, pass, bench, fail, output, skip
	Action string `json:"Action"`
	// TestEvent only:
	// The Test field, if present, specifies the test, example, or benchmark function
	// that caused the event. Events for the overall package test do not set Test.
	Test string `json:"Test"`
	// TestEvent only:
	// The Elapsed field is set for "pass" and "fail" events. It gives the time elapsed in seconds
	// for the specific test or the overall package test that passed or failed.
	Elapsed float64
	// TestEvent:
	// The Output field is set for Action == "output" and is a portion of the
	// test's output (standard output and standard error merged together). The
	// output is unmodified except that invalid UTF-8 output from a test is coerced
	// into valid UTF-8 by use of replacement characters. With that one exception,
	// the concatenation of the Output fields of all output events is the exact output
	// of the test execution.
	// BuildEvent:
	// The Output field is set for Action == "build-output" and is a portion of
	// the build's output. The concatenation of the Output fields of all output
	// events is the exact output of the build. A single event may contain one
	// or more lines of output and there may be more than one output event for
	// a given ImportPath. This matches the definition of the TestEvent.Output
	// field produced by go test -json.
	Output string `json:"Output"`
	// TestEvent only:
	// The FailedBuild field is set for Action == "fail" if the test failure was caused
	// by a build failure. It contains the package ID of the package that failed to
	// build. This matches the ImportPath field of the "go list" output, as well as the
	// BuildEvent.ImportPath field as emitted by "go build -json".
	FailedBuild string `json:"FailedBuild"`
}

// parseTestResults converts test output in JSONL format into a slice of testJSON objects
func parseTestResults(testOutput []byte) ([]testJSON, error) {
	var results []testJSON
	dec := json.NewDecoder(bytes.NewReader(testOutput))
	for {
		var event testJSON
		if err := dec.Decode(&event); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		results = append(results, event)
	}
	return results, nil
}

// testStatus represents the status of a test in a given commit
type testStatus int

const (
	testStatusUnknown testStatus = iota
	testStatusPass
	testStatusFail
	testStatusBuildFail
	testStatusSkip
)

// testInfo represents information about a specific test
type testInfo struct {
	Package string
	Test    string // empty for package tests
}

// String returns a human-readable string representation of the test
func (t testInfo) String() string {
	if t.Test == "" {
		return t.Package
	}
	return fmt.Sprintf("%s.%s", t.Package, t.Test)
}

// testRegression represents a test that regressed between commits
type testRegression struct {
	Info         testInfo
	BeforeStatus testStatus
	AfterStatus  testStatus
	Output       string // failure output in the after state
}

// collectTestStatuses processes a slice of test events and returns a map of test statuses
func collectTestStatuses(results []testJSON) map[testInfo]testStatus {
	statuses := make(map[testInfo]testStatus)
	failedBuilds := make(map[string]bool)      // track packages with build failures
	testOutputs := make(map[testInfo][]string) // collect output for failing tests

	// First pass: identify build failures
	for _, result := range results {
		if result.Action == "fail" && result.FailedBuild != "" {
			failedBuilds[result.FailedBuild] = true
		}
	}

	// Second pass: collect test statuses
	for _, result := range results {
		info := testInfo{Package: result.Package, Test: result.Test}

		// Skip output events for now, we'll process them in a separate pass
		if result.Action == "output" {
			if result.Test != "" { // only collect output for actual tests, not package messages
				testOutputs[info] = append(testOutputs[info], result.Output)
			}
			continue
		}

		// Handle BuildEvent output
		if result.Action == "build-fail" {
			// Mark all tests in this package as build failures
			for ti := range statuses {
				if ti.Package == result.ImportPath {
					statuses[ti] = testStatusBuildFail
				}
			}
			continue
		}

		// Check if the package has a build failure
		if _, hasBuildFailure := failedBuilds[result.Package]; hasBuildFailure {
			statuses[info] = testStatusBuildFail
			continue
		}

		// Handle test events
		switch result.Action {
		case "pass":
			statuses[info] = testStatusPass
		case "fail":
			statuses[info] = testStatusFail
		case "skip":
			statuses[info] = testStatusSkip
		}
	}

	return statuses
}

// compareTestResults identifies tests that have regressed between commits
func (r *CodeReviewer) compareTestResults(beforeResults, afterResults []testJSON) ([]testRegression, error) {
	beforeStatuses := collectTestStatuses(beforeResults)
	afterStatuses := collectTestStatuses(afterResults)

	// Collect output for failing tests
	testOutputMap := make(map[testInfo]string)
	for _, result := range afterResults {
		if result.Action == "output" {
			info := testInfo{Package: result.Package, Test: result.Test}
			testOutputMap[info] += result.Output
		}
	}

	var regressions []testRegression

	// Look for tests that regressed
	for info, afterStatus := range afterStatuses {
		// Skip tests that are passing or skipped in the after state
		if afterStatus == testStatusPass || afterStatus == testStatusSkip {
			continue
		}

		// Get the before status (default to unknown if not present)
		beforeStatus, exists := beforeStatuses[info]
		if !exists {
			beforeStatus = testStatusUnknown
		}

		// Log warning if we encounter unexpected unknown status in the 'after' state
		if afterStatus == testStatusUnknown {
			slog.WarnContext(context.Background(), "Unexpected unknown test status encountered",
				"package", info.Package, "test", info.Test)
		}

		// Check for regressions
		if isRegression(beforeStatus, afterStatus) {
			regressions = append(regressions, testRegression{
				Info:         info,
				BeforeStatus: beforeStatus,
				AfterStatus:  afterStatus,
				Output:       testOutputMap[info],
			})
		}
	}

	// Sort regressions for consistent output
	slices.SortFunc(regressions, func(a, b testRegression) int {
		// First by package
		if c := strings.Compare(a.Info.Package, b.Info.Package); c != 0 {
			return c
		}
		// Then by test name
		return strings.Compare(a.Info.Test, b.Info.Test)
	})

	return regressions, nil
}

// badnessLevels maps test status to a badness level
// Higher values indicate worse status (more severe issues)
var badnessLevels = map[testStatus]int{
	testStatusBuildFail: 4, // Worst
	testStatusFail:      3,
	testStatusSkip:      2,
	testStatusPass:      1,
	testStatusUnknown:   0, // Least bad - avoids false positives
}

// isRegression determines if a test has regressed based on before and after status
// A regression is defined as an increase in badness level
func isRegression(before, after testStatus) bool {
	// Higher badness level means worse status
	return badnessLevels[after] > badnessLevels[before]
}

// formatTestRegressions generates a human-readable summary of test regressions
func (r *CodeReviewer) formatTestRegressions(regressions []testRegression) string {
	if len(regressions) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Test regressions detected between initial commit (%s) and HEAD:\n\n", r.initialCommit))

	for i, reg := range regressions {
		// Describe the regression
		sb.WriteString(fmt.Sprintf("%d. %s: ", i+1, reg.Info.String()))

		switch {
		case reg.BeforeStatus == testStatusUnknown && reg.AfterStatus == testStatusFail:
			sb.WriteString("New test is failing")
		case reg.BeforeStatus == testStatusUnknown && reg.AfterStatus == testStatusBuildFail:
			sb.WriteString("New test has build errors")
		case reg.BeforeStatus == testStatusPass && reg.AfterStatus == testStatusFail:
			sb.WriteString("Was passing, now failing")
		case reg.BeforeStatus == testStatusPass && reg.AfterStatus == testStatusBuildFail:
			sb.WriteString("Was passing, now has build errors")
		case reg.BeforeStatus == testStatusSkip && reg.AfterStatus == testStatusFail:
			sb.WriteString("Was skipped, now failing")
		case reg.BeforeStatus == testStatusSkip && reg.AfterStatus == testStatusBuildFail:
			sb.WriteString("Was skipped, now has build errors")
		default:
			sb.WriteString("Regression detected")
		}
		sb.WriteString("\n")

		// Add failure output with indentation for readability
		if reg.Output != "" {
			outputLines := strings.Split(strings.TrimSpace(reg.Output), "\n")
			// Limit output to first 10 lines to avoid overwhelming feedback
			shownLines := min(len(outputLines), 10)

			sb.WriteString("   Output:\n")
			for _, line := range outputLines[:shownLines] {
				sb.WriteString(fmt.Sprintf("   | %s\n", line))
			}
			if shownLines < len(outputLines) {
				sb.WriteString(fmt.Sprintf("   | ... (%d more lines)\n", len(outputLines)-shownLines))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Please fix these test failures before proceeding.")
	return sb.String()
}
