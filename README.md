# Sketch

Sketch is an AI-powered coding assistant that helps you write, analyze, and improve code, with a special focus on Go development.

## Features

- Interactive coding assistance in terminal or browser
- Specialized support for Go
- Git integration
- Auto-runs in Docker for secure, autonomous work
- go sketch(): easily spin up multiple sessions for concurrent coding assistants
- Budget limits to manage API usage costs

## Prerequisites

`brew install go docker colima npm`

That is:

- Go
- Docker
- Docker runner
- npm

## Installation

### Go install

```bash
go install sketch.dev/cmd/sketch@latest
```

### Clone and build

```bash
git clone https://github.com/boldsoftware/sketch.git
cd sketch
go install ./cmd/sketch
```

## Usage

```bash
# Run Sketch in Docker container
sketch

# Open browser automatically
sketch -open

# Run without Docker (not recommended)
sketch -unsafe
```

See `sketch -h` for more options.

## License

[Apache License 2.0](LICENSE)

## Third-Party Licenses

This project includes code derived from the following third-party packages:

- [rsc.io/edit](http://rsc.io/edit): BSD 3-Clause License (derived from Go standard library)
- [httprr](https://pkg.go.dev/rsc.io/gaby/internal/httprr): BSD 3-Clause License (derived from Go Authors)
- Various npm packages in the web UI: See individual license files in `loop/webui/node_modules/`
