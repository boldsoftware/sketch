package bubbletea

import (
	"io/fs"
	"log/slog"
	"net/http"
)

// EmbeddedFS represents an embedded filesystem
type EmbeddedFS struct {
	FS fs.FS
}

// FileServer serves files
type FileServer struct {
	// The base directory
	baseDir string
	// Optional embedded filesystem
	fs fs.FS

	handler http.Handler
	// Logger for errors
	logger *slog.Logger
	// Fallback content for errors
	fallbackContent []byte
}

// FileServerOption contains options for creating a file server
type FileServerOption struct {
	// Base directory for serving files
	BaseDir string
	// Optional embedded filesystem
	EmbeddedFS *EmbeddedFS
	// Logger
	Logger *slog.Logger
	// Fallback content for errors (optional)
	FallbackContent []byte
}
