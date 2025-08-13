package bubbletea

import (
	"github.com/charmbracelet/lipgloss"
)

// Theme defines the color palette and styling for the UI
type Theme struct {
	// Core colors
	Primary   lipgloss.Color
	Secondary lipgloss.Color
	Accent    lipgloss.Color
	Success   lipgloss.Color
	Warning   lipgloss.Color
	Error     lipgloss.Color
	Info      lipgloss.Color

	// Background colors
	Background     lipgloss.Color
	Surface        lipgloss.Color
	SurfaceVariant lipgloss.Color

	// Text colors
	OnPrimary    lipgloss.Color
	OnSecondary  lipgloss.Color
	OnBackground lipgloss.Color
	OnSurface    lipgloss.Color
	Muted        lipgloss.Color

	// Border colors
	Border       lipgloss.Color
	BorderFocus  lipgloss.Color
	BorderActive lipgloss.Color
}

// MinimalTheme provides a clean, minimal color scheme
var MinimalTheme = Theme{
	// Core colors - subtle and professional
	Primary:   lipgloss.Color("#6366F1"), // Indigo
	Secondary: lipgloss.Color("#8B5CF6"), // Purple
	Accent:    lipgloss.Color("#06B6D4"), // Cyan
	Success:   lipgloss.Color("#10B981"), // Emerald
	Warning:   lipgloss.Color("#F59E0B"), // Amber
	Error:     lipgloss.Color("#EF4444"), // Red
	Info:      lipgloss.Color("#3B82F6"), // Blue

	// Background colors - dark but not harsh
	Background:     lipgloss.Color("#0F172A"), // Slate 900
	Surface:        lipgloss.Color("#1E293B"), // Slate 800
	SurfaceVariant: lipgloss.Color("#334155"), // Slate 700

	// Text colors - high contrast for readability
	OnPrimary:    lipgloss.Color("#FFFFFF"),
	OnSecondary:  lipgloss.Color("#FFFFFF"),
	OnBackground: lipgloss.Color("#F1F5F9"), // Slate 100
	OnSurface:    lipgloss.Color("#E2E8F0"), // Slate 200
	Muted:        lipgloss.Color("#94A3B8"), // Slate 400

	// Border colors - subtle but visible
	Border:      lipgloss.Color("#475569"), // Slate 600
	BorderFocus: lipgloss.Color("#6366F1"), // Indigo (same as primary)
}
