package loop

import (
	"context"
	"encoding/json"

	"sketch.dev/claudetool/codereview"
	"sketch.dev/llm"
)

// makeDoneTool creates a tool that provides a checklist to the agent. There
// are some duplicative instructions here and in the system prompt, and it's
// not as reliable as it could be. Historically, we've found that Claude ignores
// the tool results here, so we don't tell the tool to say "hey, really check this"
// at the moment, though we've tried.
func makeDoneTool(codereview *codereview.CodeReviewer) *llm.Tool {
	return &llm.Tool{
		Name:        "done",
		Description: doneDescription,
		InputSchema: json.RawMessage(doneChecklistJSONSchema),
		Run: func(ctx context.Context, input json.RawMessage) llm.ToolOut {
			// Cannot be done with a messy git.
			if err := codereview.RequireNormalGitState(ctx); err != nil {
				return llm.ErrorToolOut(err)
			}
			if err := codereview.RequireNoUncommittedChanges(ctx); err != nil {
				return llm.ErrorToolOut(err)
			}
			// Ensure that the current commit has been reviewed.
			head, err := codereview.CurrentCommit(ctx)
			if err == nil {
				needsReview := !codereview.IsInitialCommit(head) && !codereview.HasReviewed(head)
				if needsReview {
					return llm.ErrorfToolOut("codereview tool has not been run for commit %v", head)
				}
			}
			return llm.ToolOut{LLMContent: llm.TextContent("Please ask the user to review your work. Be concise - users are more likely to read shorter comments.")}
		},
	}
}

// TODO: this is ugly, maybe JSON-encode a deeply nested map[string]any instead? also ugly.
const (
	doneDescription         = `Use this tool when you have achieved the user's goal. The parameters form a checklist which you should evaluate.`
	doneChecklistJSONSchema = `{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "Checklist",
  "description": "A schema for tracking checklist items with status and comments",
  "type": "object",
  "required": ["checklist_items"],
  "properties": {
    "checklist_items": {
      "type": "object",
      "description": "Collection of checklist items",
      "properties": {
        "checked_guidance": {
          "$ref": "#/definitions/checklistItem",
          "description": "I checked for and followed any directory-specific guidance files for all modified files."
        },
        "wrote_tests": {
          "$ref": "#/definitions/checklistItem",
          "description": "If code was changed, tests were written or updated."
        },
        "passes_tests": {
          "$ref": "#/definitions/checklistItem",
          "description": "If any commits were made, tests pass."
        },
        "code_reviewed": {
          "$ref": "#/definitions/checklistItem",
          "description": "If any commits were made, the codereview tool was run and its output was addressed."
        },
        "git_commit": {
          "$ref": "#/definitions/checklistItem",
          "description": "Create git commits for any code changes you made. A git hook will add Co-Authored-By and Change-ID trailers. The git user is already configured correctly."
        }
      },
      "additionalProperties": {
        "$ref": "#/definitions/checklistItem"
      }
    }
  },
  "definitions": {
    "checklistItem": {
      "type": "object",
      "required": ["status"],
      "properties": {
        "status": {
          "type": "string",
          "description": "Current status of the checklist item",
          "enum": ["yes", "no", "not applicable", "other"]
        },
        "description": {
          "type": "string",
          "description": "Description of what this checklist item verifies"
        },
        "comments": {
          "type": "string",
          "description": "Additional comments or context for this checklist item"
        }
      }
    }
  }
}`
)
