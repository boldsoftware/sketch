package claudetool

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"sketch.dev/ant"
)

// The Keyword tool provides keyword search.
// TODO: use an embedding model + re-ranker or otherwise do something nicer than this kludge.
// TODO: if we can get this fast enough, do it on the fly while the user is typing their prompt.
var Keyword = &ant.Tool{
	Name:        keywordName,
	Description: keywordDescription,
	InputSchema: ant.MustSchema(keywordInputSchema),
	Run:         keywordRun,
}

const (
	keywordName        = "keyword_search"
	keywordDescription = `
keyword_search locates files with a search-and-filter approach.
Use when navigating unfamiliar codebases with only conceptual understanding or vague user questions.

Effective use:
- Provide a detailed query for accurate relevance ranking
- Include extensive but uncommon keywords to ensure comprehensive results
- Order keywords by importance (most important first) - less important keywords may be dropped if there are too many results

IMPORTANT: Do NOT use this tool if you have precise information like log lines, error messages, filenames, symbols, or package names. Use direct approaches (grep, cat, go doc, etc.) instead.
`

	// If you modify this, update the termui template for prettier rendering.
	keywordInputSchema = `
{
  "type": "object",
  "required": [
    "query",
    "keywords"
  ],
  "properties": {
    "query": {
      "type": "string",
      "description": "A detailed statement of what you're trying to find or learn."
    },
    "keywords": {
      "type": "array",
      "items": {
        "type": "string"
      },
      "description": "List of keywords in descending order of importance."
    }
  }
}
`
)

type keywordInput struct {
	Query    string   `json:"query"`
	Keywords []string `json:"keywords"`
}

//go:embed keyword_system_prompt.txt
var keywordSystemPrompt string

// findRepoRoot attempts to find the git repository root from the current directory
func findRepoRoot(wd string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = wd
	out, err := cmd.Output()
	// todo: cwd here and throughout
	if err != nil {
		return "", fmt.Errorf("failed to find git repository root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func keywordRun(ctx context.Context, m json.RawMessage) (string, error) {
	var input keywordInput
	if err := json.Unmarshal(m, &input); err != nil {
		return "", err
	}
	wd := WorkingDir(ctx)
	root, err := findRepoRoot(wd)
	if err == nil {
		wd = root
	}
	slog.InfoContext(ctx, "keyword search input", "query", input.Query, "keywords", input.Keywords, "wd", wd)

	// first remove stopwords
	var keep []string
	for _, term := range input.Keywords {
		out, err := ripgrep(ctx, wd, []string{term})
		if err != nil {
			return "", err
		}
		if len(out) > 64*1024 {
			slog.InfoContext(ctx, "keyword search result too large", "term", term, "bytes", len(out))
			continue
		}
		keep = append(keep, term)
	}

	// peel off keywords until we get a result that fits in the query window
	var out string
	for {
		var err error
		out, err = ripgrep(ctx, wd, keep)
		if err != nil {
			return "", err
		}
		if len(out) < 128*1024 {
			break
		}
		keep = keep[:len(keep)-1]
	}

	info := ant.ToolCallInfoFromContext(ctx)
	convo := info.Convo.SubConvo()
	convo.SystemPrompt = strings.TrimSpace(keywordSystemPrompt)

	initialMessage := ant.Message{
		Role: ant.MessageRoleUser,
		Content: []ant.Content{
			ant.StringContent("<pwd>\n" + wd + "\n</pwd>"),
			ant.StringContent("<ripgrep_results>\n" + out + "\n</ripgrep_results>"),
			ant.StringContent("<query>\n" + input.Query + "\n</query>"),
		},
	}

	resp, err := convo.SendMessage(initialMessage)
	if err != nil {
		return "", fmt.Errorf("failed to send relevance filtering message: %w", err)
	}
	if len(resp.Content) != 1 {
		return "", fmt.Errorf("unexpected number of messages in relevance filtering response: %d", len(resp.Content))
	}

	filtered := resp.Content[0].Text

	slog.InfoContext(ctx, "keyword search results processed",
		"bytes", len(out),
		"lines", strings.Count(out, "\n"),
		"files", strings.Count(out, "\n\n"),
		"query", input.Query,
		"filtered", filtered,
	)

	return resp.Content[0].Text, nil
}

func ripgrep(ctx context.Context, wd string, terms []string) (string, error) {
	args := []string{"-C", "10", "-i", "--line-number", "--with-filename"}
	for _, term := range terms {
		args = append(args, "-e", term)
	}
	cmd := exec.CommandContext(ctx, "rg", args...)
	cmd.Dir = wd
	out, err := cmd.CombinedOutput()
	if err != nil {
		// ripgrep returns exit code 1 when no matches are found, which is not an error for us
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return "no matches found", nil
		}
		return "", fmt.Errorf("search failed: %v\n%s", err, out)
	}
	outStr := string(out)
	return outStr, nil
}
