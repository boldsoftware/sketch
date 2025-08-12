package bubbletea

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"

	"github.com/charmbracelet/lipgloss"
	"sketch.dev/loop"
)

// ToolTemplateRenderer handles rendering of tool usage messages
type ToolTemplateRenderer struct {
	template *template.Template
	emojiMap map[string]string
	styles   map[string]lipgloss.Style
}

// NewToolTemplateRenderer creates a new tool template renderer
func NewToolTemplateRenderer() *ToolTemplateRenderer {
	// Create the tool template
	tmpl, err := template.New("tool").Parse(toolUseTemplTxt)
	if err != nil {
		// Fall back to a simple template if parsing fails
		tmpl, _ = template.New("tool").Parse("{{.ToolName}}: {{.ToolInput}}")
	}

	// Create emoji map for different tool types
	emojiMap := map[string]string{
		"think":          "[THK]",  // Think indicator
		"bash":           "[SH]", // Shell indicator
		"patch":          "[PAT]", // Patch indicator
		"browser":        "[BR]",  // Browser indicator
		"browser_click":  "[CLK]", // Click indicator
		"browser_type":   "[TYP]", // Type indicator
		"browser_wait":   "[WAIT]",  // Wait indicator
		"codereview":     "[REV]",  // Review indicator
		"multiplechoice": "[MC]",  // Multiple choice
		"set-slug":       "[SLUG]",  // Slug indicator
		"error":          "[ERR]",  // Error indicator
		"default":        "[T]", // Tool indicator
	}

	// Create styles for different tool states
	styles := map[string]lipgloss.Style{
		"toolName": lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true),
		"toolInput": lipgloss.NewStyle().
			Foreground(lipgloss.Color("243")),
		"toolResult": lipgloss.NewStyle().
			Foreground(lipgloss.Color("252")),
		"toolError": lipgloss.NewStyle().
			Foreground(lipgloss.Color("196")).
			Bold(true),
		"toolSlow": lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Italic(true),
		"toolBackground": lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true),
	}

	return &ToolTemplateRenderer{
		template: tmpl,
		emojiMap: emojiMap,
		styles:   styles,
	}
}

// RenderTool renders a tool usage message
func (r *ToolTemplateRenderer) RenderTool(msg *loop.AgentMessage) string {
	if msg == nil {
		return ""
	}

	// Create template data
	data := map[string]interface{}{
		"ToolName":       msg.ToolName,
		"ToolInput":      msg.ToolInput,
		"ToolResult":     msg.ToolResult,
		"ToolError":      msg.ToolError,
		"ToolEmoji":      r.getToolEmoji(msg.ToolName),
		"ToolSlow":       r.isSlowTool(msg.ToolName),
		"ToolBackground": r.isBackgroundTool(msg.ToolName),
		"Styles":         r.styles,
	}

	// Execute template
	var buf bytes.Buffer
	if err := r.template.Execute(&buf, data); err != nil {
		// Fall back to simple rendering on error
		return fmt.Sprintf("%s %s: %s",
			r.getToolEmoji(msg.ToolName),
			r.styles["toolName"].Render(msg.ToolName),
			msg.ToolInput)
	}

	return buf.String()
}

// getToolEmoji returns the emoji for a tool type
func (r *ToolTemplateRenderer) getToolEmoji(toolName string) string {
	// Check for specific tool prefixes
	for prefix, emoji := range r.emojiMap {
		if strings.HasPrefix(strings.ToLower(toolName), prefix) {
			return emoji
		}
	}

	// Return default emoji if no match
	return r.emojiMap["default"]
}

// isSlowTool checks if a tool is known to be slow
func (r *ToolTemplateRenderer) isSlowTool(toolName string) bool {
	slowTools := []string{"codereview", "browser_wait", "bash"}
	toolNameLower := strings.ToLower(toolName)

	for _, slow := range slowTools {
		if strings.Contains(toolNameLower, slow) {
			return true
		}
	}

	return false
}

// isBackgroundTool checks if a tool runs in the background
func (r *ToolTemplateRenderer) isBackgroundTool(toolName string) bool {
	return strings.Contains(strings.ToLower(toolName), "background")
}

// Tool template text
const toolUseTemplTxt = `{{.ToolEmoji}} {{.Styles.toolName.Render .ToolName}}{{if .ToolSlow}} [SLOW]{{end}}{{if .ToolBackground}} [BG]{{end}}
{{.Styles.toolInput.Render .ToolInput}}
{{if .ToolError}}{{.Styles.toolError.Render "[ERR] Error:"}} {{.ToolResult}}{{else}}{{.Styles.toolResult.Render .ToolResult}}{{end}}`
