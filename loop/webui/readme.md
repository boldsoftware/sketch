# Loop WebUI

A modern web interface for the CodingAgent loop.

The server in the sibling directory (../server) exposes an HTTP API for
the CodingAgent.

## Development

This module contains a TypeScript-based web UI for the Loop service. The TypeScript code is compiled into JavaScript using esbuild, and the resulting bundle is served by the Go server.

### Prerequisites

- Node.js and npm
- Go 1.20 or later

### Setup

```bash
# Install dependencies
make install

# Build the TypeScript code
make build

# Type checking only
make check
```

### Development Mode

For development, you can use watch mode:

```bash
make dev
```

This will rebuild the TypeScript files whenever they change.

## Integration with Go Server

The TypeScript code is bundled into JavaScript using esbuild and then served by the Go HTTP server. The integration happens through the `webui` package, which provides a function to retrieve the built bundle.

The server code accesses the built web UI through the `webui.GetBundle()` function, which returns a filesystem that can be used to serve the files.

## File Structure

- `src/`: TypeScript source files
- `dist/`: Generated JavaScript bundle
- `esbuild.go`: Go code for bundling TypeScript files
- `Makefile`: Build tasks

## Bundle Analysis

You can analyze the size and dependency structure of the TypeScript bundles:

```bash
# Generate bundle metafiles in a temporary directory
go run sketch.dev/cmd/bundle-analyzer
```

The tool generates metafiles that can be analyzed by dragging them onto the esbuild analyzer at https://esbuild.github.io/analyze/
